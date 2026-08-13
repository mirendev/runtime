package compute

import (
	"miren.dev/runtime/api/core/core_v1alpha"
)

// Variable sources. The field is a free-form string in the schema; these are the
// values the platform actually writes.
const (
	// SourceConfig marks a variable declared in .miren/app.toml. It belongs to
	// the version that was built from that app.toml.
	SourceConfig = "config"

	// SourceManual marks a variable an operator set (miren env set, deploy -e/-s).
	SourceManual = "manual"

	// SourceAddon marks a variable an addon injected at provision or rotation
	// time. Deprovision only strips keys whose source is exactly this, so the
	// value must never be rewritten to something else.
	SourceAddon = "addon"
)

// normalizeSource maps the legacy empty source to manual, matching the merge
// rules in servers/build/build.go. Versions written before the source field
// existed carry an empty string and have always been treated as operator-owned.
func normalizeSource(source string) string {
	if source == "" {
		return SourceManual
	}
	return source
}

// carriedForward reports whether a variable with this source belongs to the app
// rather than to a particular build: addon bindings and operator-set values.
func carriedForward(source string) bool {
	switch normalizeSource(source) {
	case SourceAddon, SourceManual:
		return true
	}
	return false
}

// CarryForwardVars merges the variables an app owns — addon-injected bindings
// and operator-set values — from prev into target, and reports whether target
// changed.
//
// It exists for the paths that activate an already-built AppVersion rather than
// building a new one (deploy -V, app rollback). Those versions are snapshots
// from an earlier moment, and a snapshot predating an addon has no DATABASE_URL
// in it. Activating it verbatim used to strand the app without its database
// credentials permanently, because the addon controller only injects at
// provision time and never re-injects (MIR-1579).
//
// The rule mirrors what the build path already does in mergeVariablesFromAppConfig:
// addon and manual variables follow the app, while config variables (from
// .miren/app.toml) stay with the version that was built from that file.
//
// The merge is additive. A carried variable overwrites the same key in target,
// but a key present only in target is left alone — re-activating an old version
// should never be the thing that deletes a variable. Each carried variable is
// copied whole, so Source, Backend and the app.toml metadata survive intact.
func CarryForwardVars(target, prev *core_v1alpha.ConfigSpec) bool {
	if target == nil || prev == nil {
		return false
	}

	changed := false

	vars, varsChanged := carryVars(target.Variables, prev.Variables,
		func(v core_v1alpha.ConfigSpecVariables) (string, string) { return v.Key, v.Source })
	if varsChanged {
		target.Variables = vars
		changed = true
	}

	// Addons only inject globals today, but `miren env set --service` writes
	// per-service manual vars, and those deserve the same treatment. Task env
	// is left alone: buildTasksConfig rebuilds spec.Tasks wholesale from
	// app.toml and stamps every entry source="config", and nothing else writes
	// it, so a task has no app-owned variables to carry.
	for i := range target.Services {
		prevSvc := findService(prev.Services, target.Services[i].Name)
		if prevSvc == nil {
			continue
		}

		env, envChanged := carryVars(target.Services[i].Env, prevSvc.Env,
			func(e core_v1alpha.ConfigSpecServicesEnv) (string, string) { return e.Key, e.Source })
		if envChanged {
			target.Services[i].Env = env
			changed = true
		}
	}

	return changed
}

func findService(services []core_v1alpha.ConfigSpecServices, name string) *core_v1alpha.ConfigSpecServices {
	for i := range services {
		if services[i].Name == name {
			return &services[i]
		}
	}
	return nil
}

// carryVars merges the app-owned entries of prev into target, preserving
// target's ordering and appending anything new in prev's order. keySource
// extracts the key and source from an entry; the two ConfigSpec variable types
// are field-identical but distinct, so the accessor is what unifies them.
//
// T must be a flat value type. The == below is what decides whether anything
// actually changed, and the caller skips minting a whole version when it says
// nothing did. The comparable constraint already rules out slice and map fields,
// but a pointer field would satisfy it while comparing addresses rather than
// contents, quietly reporting "changed" for equal values. Both instantiations
// today are plain string/bool structs.
func carryVars[T comparable](target, prev []T, keySource func(T) (string, string)) ([]T, bool) {
	positions := make(map[string]int, len(target))
	for i, v := range target {
		key, _ := keySource(v)
		positions[key] = i
	}

	// Copy lazily: a no-op merge must leave the caller's slice untouched so the
	// caller can decide not to mint a new version at all.
	merged := target
	changed := false
	copied := false
	ensureCopy := func() {
		if !copied {
			merged = append(make([]T, 0, len(target)+len(prev)), target...)
			copied = true
		}
	}

	for _, pv := range prev {
		key, source := keySource(pv)
		if !carriedForward(source) {
			continue
		}

		if i, ok := positions[key]; ok {
			if merged[i] == pv {
				continue
			}
			ensureCopy()
			merged[i] = pv
			changed = true
			continue
		}

		ensureCopy()
		positions[key] = len(merged)
		merged = append(merged, pv)
		changed = true
	}

	return merged, changed
}
