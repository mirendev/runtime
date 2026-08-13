package compute

import (
	"testing"

	"miren.dev/runtime/api/core/core_v1alpha"
)

func v(key, value, source string) core_v1alpha.ConfigSpecVariables {
	return core_v1alpha.ConfigSpecVariables{Key: key, Value: value, Source: source}
}

func varsByKey(vars []core_v1alpha.ConfigSpecVariables) map[string]core_v1alpha.ConfigSpecVariables {
	out := make(map[string]core_v1alpha.ConfigSpecVariables, len(vars))
	for _, item := range vars {
		out[item.Key] = item
	}
	return out
}

func TestCarryForwardVars(t *testing.T) {
	tests := []struct {
		name        string
		target      []core_v1alpha.ConfigSpecVariables
		prev        []core_v1alpha.ConfigSpecVariables
		wantChanged bool
		want        map[string]core_v1alpha.ConfigSpecVariables
	}{
		{
			// MIR-1579: the version being re-activated predates the addon, so it
			// has no DATABASE_URL at all. Activating it verbatim used to leave
			// the app permanently without its database credentials.
			name:        "addon var missing from target is carried forward",
			target:      []core_v1alpha.ConfigSpecVariables{v("DB_POOL_SIZE", "5", SourceConfig)},
			prev:        []core_v1alpha.ConfigSpecVariables{v("DB_POOL_SIZE", "5", SourceConfig), v("DATABASE_URL", "postgres://live", SourceAddon)},
			wantChanged: true,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"DB_POOL_SIZE": v("DB_POOL_SIZE", "5", SourceConfig),
				"DATABASE_URL": v("DATABASE_URL", "postgres://live", SourceAddon),
			},
		},
		{
			// Deprovision only strips keys whose source is exactly "addon"
			// (controllers/addon/controller.go). Relabelling would leak the var.
			name:        "carried addon var keeps its addon source",
			target:      nil,
			prev:        []core_v1alpha.ConfigSpecVariables{v("PGPASSWORD", "hunter2", SourceAddon)},
			wantChanged: true,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"PGPASSWORD": v("PGPASSWORD", "hunter2", SourceAddon),
			},
		},
		{
			name:        "rotated addon value overrides the stale one in target",
			target:      []core_v1alpha.ConfigSpecVariables{v("PGPASSWORD", "old", SourceAddon)},
			prev:        []core_v1alpha.ConfigSpecVariables{v("PGPASSWORD", "rotated", SourceAddon)},
			wantChanged: true,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"PGPASSWORD": v("PGPASSWORD", "rotated", SourceAddon),
			},
		},
		{
			name:        "manual var set after the target version is carried forward",
			target:      []core_v1alpha.ConfigSpecVariables{v("API_TOKEN", "old", SourceManual)},
			prev:        []core_v1alpha.ConfigSpecVariables{v("API_TOKEN", "new", SourceManual)},
			wantChanged: true,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"API_TOKEN": v("API_TOKEN", "new", SourceManual),
			},
		},
		{
			// app.toml belongs to the build, so the pinned version's value wins.
			name:        "config var stays with the target version",
			target:      []core_v1alpha.ConfigSpecVariables{v("LOG_LEVEL", "debug", SourceConfig)},
			prev:        []core_v1alpha.ConfigSpecVariables{v("LOG_LEVEL", "info", SourceConfig)},
			wantChanged: false,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"LOG_LEVEL": v("LOG_LEVEL", "debug", SourceConfig),
			},
		},
		{
			name:        "config var absent from prev is not added",
			target:      []core_v1alpha.ConfigSpecVariables{v("LOG_LEVEL", "debug", SourceConfig)},
			prev:        []core_v1alpha.ConfigSpecVariables{v("FEATURE_X", "1", SourceConfig)},
			wantChanged: false,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"LOG_LEVEL": v("LOG_LEVEL", "debug", SourceConfig),
			},
		},
		{
			// Additive only: re-activating an old version must not be the thing
			// that deletes a variable.
			name:        "target-only manual var is not deleted",
			target:      []core_v1alpha.ConfigSpecVariables{v("LEGACY_FLAG", "1", SourceManual)},
			prev:        []core_v1alpha.ConfigSpecVariables{v("DATABASE_URL", "postgres://live", SourceAddon)},
			wantChanged: true,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"LEGACY_FLAG":  v("LEGACY_FLAG", "1", SourceManual),
				"DATABASE_URL": v("DATABASE_URL", "postgres://live", SourceAddon),
			},
		},
		{
			name:        "legacy empty source is treated as manual",
			target:      nil,
			prev:        []core_v1alpha.ConfigSpecVariables{v("OLD_VAR", "value", "")},
			wantChanged: true,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"OLD_VAR": v("OLD_VAR", "value", ""),
			},
		},
		{
			// The caller skips minting a derived version when nothing changed,
			// so an identical spec must report false.
			name:        "identical specs report no change",
			target:      []core_v1alpha.ConfigSpecVariables{v("DATABASE_URL", "postgres://live", SourceAddon), v("LOG_LEVEL", "info", SourceConfig)},
			prev:        []core_v1alpha.ConfigSpecVariables{v("DATABASE_URL", "postgres://live", SourceAddon), v("LOG_LEVEL", "info", SourceConfig)},
			wantChanged: false,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"DATABASE_URL": v("DATABASE_URL", "postgres://live", SourceAddon),
				"LOG_LEVEL":    v("LOG_LEVEL", "info", SourceConfig),
			},
		},
		{
			name:        "app with no addons and no manual vars is untouched",
			target:      []core_v1alpha.ConfigSpecVariables{v("LOG_LEVEL", "info", SourceConfig)},
			prev:        []core_v1alpha.ConfigSpecVariables{v("LOG_LEVEL", "info", SourceConfig)},
			wantChanged: false,
			want: map[string]core_v1alpha.ConfigSpecVariables{
				"LOG_LEVEL": v("LOG_LEVEL", "info", SourceConfig),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &core_v1alpha.ConfigSpec{Variables: tt.target}
			prev := &core_v1alpha.ConfigSpec{Variables: tt.prev}

			changed := CarryForwardVars(target, prev)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}

			got := varsByKey(target.Variables)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d variables %v, want %d", len(got), target.Variables, len(tt.want))
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("variable %q = %+v, want %+v", key, got[key], want)
				}
			}
		})
	}
}

// A no-op merge must not touch the caller's slice; the deploy path relies on
// that to decide it can activate the existing version rather than mint a new one.
func TestCarryForwardVarsLeavesTargetSliceAloneWhenUnchanged(t *testing.T) {
	original := []core_v1alpha.ConfigSpecVariables{v("LOG_LEVEL", "info", SourceConfig)}
	target := &core_v1alpha.ConfigSpec{Variables: original}
	prev := &core_v1alpha.ConfigSpec{Variables: []core_v1alpha.ConfigSpecVariables{v("LOG_LEVEL", "info", SourceConfig)}}

	if CarryForwardVars(target, prev) {
		t.Fatal("expected no change")
	}
	if &target.Variables[0] != &original[0] {
		t.Error("target slice was reallocated despite no change")
	}
}

// Carrying a variable in must not mutate the spec it was read from — the deploy
// path resolves prev from the app's live ConfigVersion.
func TestCarryForwardVarsDoesNotMutatePrev(t *testing.T) {
	target := &core_v1alpha.ConfigSpec{Variables: []core_v1alpha.ConfigSpecVariables{v("A", "1", SourceConfig)}}
	prevVars := []core_v1alpha.ConfigSpecVariables{v("DATABASE_URL", "postgres://live", SourceAddon)}
	prev := &core_v1alpha.ConfigSpec{Variables: prevVars}

	if !CarryForwardVars(target, prev) {
		t.Fatal("expected a change")
	}
	if len(prev.Variables) != 1 || prev.Variables[0] != prevVars[0] {
		t.Errorf("prev was mutated: %+v", prev.Variables)
	}
}

func TestCarryForwardVarsServiceEnv(t *testing.T) {
	env := func(key, value, source string) core_v1alpha.ConfigSpecServicesEnv {
		return core_v1alpha.ConfigSpecServicesEnv{Key: key, Value: value, Source: source}
	}

	target := &core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{
			{Name: "web", Env: []core_v1alpha.ConfigSpecServicesEnv{env("PORT", "8080", SourceConfig)}},
			{Name: "worker", Env: []core_v1alpha.ConfigSpecServicesEnv{env("QUEUE", "default", SourceConfig)}},
		},
	}
	prev := &core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{
			{Name: "web", Env: []core_v1alpha.ConfigSpecServicesEnv{
				env("PORT", "9090", SourceConfig),
				env("WEB_SECRET", "s3cret", SourceManual),
			}},
			// A service the target version doesn't have is skipped entirely.
			{Name: "cron", Env: []core_v1alpha.ConfigSpecServicesEnv{env("SCHEDULE", "* * * * *", SourceManual)}},
		},
	}

	if !CarryForwardVars(target, prev) {
		t.Fatal("expected a change")
	}

	web := target.Services[0]
	if len(web.Env) != 2 {
		t.Fatalf("web env = %+v, want 2 entries", web.Env)
	}
	if web.Env[0] != env("PORT", "8080", SourceConfig) {
		t.Errorf("config-sourced PORT should stay with the target version, got %+v", web.Env[0])
	}
	if web.Env[1] != env("WEB_SECRET", "s3cret", SourceManual) {
		t.Errorf("manual WEB_SECRET not carried, got %+v", web.Env[1])
	}

	if len(target.Services) != 2 {
		t.Errorf("services = %d, want 2 (cron exists only in prev and must not be added)", len(target.Services))
	}
	if worker := target.Services[1]; len(worker.Env) != 1 || worker.Env[0] != env("QUEUE", "default", SourceConfig) {
		t.Errorf("worker env changed unexpectedly: %+v", worker.Env)
	}
}
