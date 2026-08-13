package deployment

import (
	"context"
	"log/slog"
	"testing"

	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/secret"
)

// envFixture builds an app with two versions in the in-memory store: a target
// version (the one a -V deploy names) and a live version that is currently
// active. Both get a real ConfigVersion, the way every version minted since the
// config-version split does.
type envFixture struct {
	ctx     context.Context
	inmem   *testutils.InMemEntityServer
	server  *DeploymentServer
	client  *deployment_v1alpha.DeploymentClient
	appName string
	appID   entity.Id
}

func newEnvFixture(t *testing.T, appName string, targetVars, liveVars []core_v1alpha.ConfigSpecVariables) *envFixture {
	t.Helper()

	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	if err != nil {
		t.Fatalf("failed to create deployment server: %v", err)
	}

	appID, err := inmem.Client.Create(ctx, appName, &core_v1alpha.App{})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	f := &envFixture{
		ctx:     ctx,
		inmem:   inmem,
		server:  server,
		client:  &deployment_v1alpha.DeploymentClient{Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server))},
		appName: appName,
		appID:   appID,
	}

	f.createVersion(t, appName+"-target", targetVars)
	liveID := f.createVersion(t, appName+"-live", liveVars)

	if err := inmem.Client.Update(ctx, &core_v1alpha.App{ID: appID, ActiveVersion: liveID}); err != nil {
		t.Fatalf("failed to set active version: %v", err)
	}

	return f
}

func (f *envFixture) createVersion(t *testing.T, name string, vars []core_v1alpha.ConfigSpecVariables) entity.Id {
	t.Helper()

	cvID, err := f.inmem.Client.Create(f.ctx, name+"-cfg", &core_v1alpha.ConfigVersion{
		App:  f.appID,
		Spec: core_v1alpha.ConfigSpec{Entrypoint: "/app/server", Variables: vars},
	})
	if err != nil {
		t.Fatalf("failed to create config version %s: %v", name, err)
	}

	id, err := f.inmem.Client.Create(f.ctx, name, &core_v1alpha.AppVersion{
		App:           f.appID,
		Version:       name,
		ImageUrl:      "registry.example/" + name,
		ConfigVersion: cvID,
	})
	if err != nil {
		t.Fatalf("failed to create app version %s: %v", name, err)
	}
	return id
}

// deploy runs the -V path against the target version and returns the config the
// app ends up running, plus the version name that actually went active.
func (f *envFixture) deploy(t *testing.T, envVars []*deployment_v1alpha.EnvironmentVariable) (*core_v1alpha.ConfigSpec, string) {
	t.Helper()

	result, err := f.client.DeployVersion(f.ctx, f.appName, "cluster1", f.appName+"-target", false, envVars, "", "")
	if err != nil {
		t.Fatalf("DeployVersion failed: %v", err)
	}
	if result.HasError() && result.Error() != "" {
		t.Fatalf("DeployVersion returned error: %s", result.Error())
	}

	var app core_v1alpha.App
	if err := f.inmem.Client.Get(f.ctx, f.appName, &app); err != nil {
		t.Fatalf("failed to re-read app: %v", err)
	}
	if app.ActiveVersion == "" {
		t.Fatal("app has no active version after deploy")
	}

	var active core_v1alpha.AppVersion
	if err := f.inmem.Client.GetById(f.ctx, app.ActiveVersion, &active); err != nil {
		t.Fatalf("failed to read active version: %v", err)
	}

	spec, err := coreutil.ResolveConfig(f.ctx, f.inmem.EAC, &active)
	if err != nil {
		t.Fatalf("failed to resolve active config: %v", err)
	}
	return spec, active.Version
}

func cfgVar(key, value, source string) core_v1alpha.ConfigSpecVariables {
	return core_v1alpha.ConfigSpecVariables{Key: key, Value: value, Source: source}
}

func lookupVar(t *testing.T, spec *core_v1alpha.ConfigSpec, key string) core_v1alpha.ConfigSpecVariables {
	t.Helper()
	for _, v := range spec.Variables {
		if v.Key == key {
			return v
		}
	}
	t.Fatalf("variable %q missing from resolved config %+v", key, spec.Variables)
	return core_v1alpha.ConfigSpecVariables{}
}

func hasVar(spec *core_v1alpha.ConfigSpec, key string) bool {
	for _, v := range spec.Variables {
		if v.Key == key {
			return true
		}
	}
	return false
}

// MIR-1579. An addon injects its variables by minting a successor version, so
// the version a failed first deploy names has never contained DATABASE_URL.
// Re-activating it with `miren deploy -V` used to leave the app permanently
// without its database credentials, recoverable only by destroying the addon.
func TestDeployVersionPreservesAddonEnvVars(t *testing.T) {
	f := newEnvFixture(t, "addonapp",
		[]core_v1alpha.ConfigSpecVariables{cfgVar("DB_POOL_SIZE", "5", coreutil.SourceConfig)},
		[]core_v1alpha.ConfigSpecVariables{
			cfgVar("DB_POOL_SIZE", "5", coreutil.SourceConfig),
			cfgVar("DATABASE_URL", "postgres://live/db", coreutil.SourceAddon),
			cfgVar("PGPASSWORD", "hunter2", coreutil.SourceAddon),
		})

	spec, activeVersion := f.deploy(t, nil)

	dbURL := lookupVar(t, spec, "DATABASE_URL")
	if dbURL.Value != "postgres://live/db" {
		t.Errorf("DATABASE_URL = %q, want the live value", dbURL.Value)
	}
	// Deprovision only strips keys whose source is exactly "addon"; relabelling
	// would leak the variable when the addon is later removed.
	if dbURL.Source != coreutil.SourceAddon {
		t.Errorf("DATABASE_URL source = %q, want %q", dbURL.Source, coreutil.SourceAddon)
	}
	if !hasVar(spec, "PGPASSWORD") {
		t.Error("PGPASSWORD was not carried forward")
	}

	if activeVersion == "addonapp-target" {
		t.Error("expected a derived version to be activated, got the target version verbatim")
	}
}

func TestDeployVersionPreservesManualEnvVars(t *testing.T) {
	f := newEnvFixture(t, "manualapp",
		[]core_v1alpha.ConfigSpecVariables{cfgVar("API_TOKEN", "old", coreutil.SourceManual)},
		[]core_v1alpha.ConfigSpecVariables{cfgVar("API_TOKEN", "rotated", coreutil.SourceManual)})

	spec, _ := f.deploy(t, nil)

	if got := lookupVar(t, spec, "API_TOKEN"); got.Value != "rotated" {
		t.Errorf("API_TOKEN = %q, want the current live value %q", got.Value, "rotated")
	}
}

// app.toml belongs to the build, so a -V deploy gets the pinned version's value.
func TestDeployVersionKeepsConfigVarsFromTargetVersion(t *testing.T) {
	f := newEnvFixture(t, "configapp",
		[]core_v1alpha.ConfigSpecVariables{cfgVar("LOG_LEVEL", "debug", coreutil.SourceConfig)},
		[]core_v1alpha.ConfigSpecVariables{
			cfgVar("LOG_LEVEL", "info", coreutil.SourceConfig),
			cfgVar("DATABASE_URL", "postgres://live/db", coreutil.SourceAddon),
		})

	spec, _ := f.deploy(t, nil)

	if got := lookupVar(t, spec, "LOG_LEVEL"); got.Value != "debug" {
		t.Errorf("LOG_LEVEL = %q, want the target version's value %q", got.Value, "debug")
	}
	if !hasVar(spec, "DATABASE_URL") {
		t.Error("DATABASE_URL was not carried forward alongside the config var")
	}
}

// The merge is additive: re-activating an old version must never be the thing
// that deletes a variable.
func TestDeployVersionDoesNotDeleteTargetOnlyVars(t *testing.T) {
	f := newEnvFixture(t, "additiveapp",
		[]core_v1alpha.ConfigSpecVariables{cfgVar("LEGACY_FLAG", "1", coreutil.SourceManual)},
		[]core_v1alpha.ConfigSpecVariables{cfgVar("DATABASE_URL", "postgres://live/db", coreutil.SourceAddon)})

	spec, _ := f.deploy(t, nil)

	if !hasVar(spec, "LEGACY_FLAG") {
		t.Error("LEGACY_FLAG was deleted; the carry-forward merge must be additive")
	}
	if !hasVar(spec, "DATABASE_URL") {
		t.Error("DATABASE_URL was not carried forward")
	}
}

// An app whose config has not moved on should keep its exact version identity —
// blackbox/rollback_test.go asserts the active version id flips back precisely.
func TestDeployVersionActivatesVerbatimWhenNothingToCarry(t *testing.T) {
	vars := []core_v1alpha.ConfigSpecVariables{cfgVar("LOG_LEVEL", "info", coreutil.SourceConfig)}
	f := newEnvFixture(t, "plainapp", vars, vars)

	spec, activeVersion := f.deploy(t, nil)

	if activeVersion != "plainapp-target" {
		t.Errorf("active version = %q, want the target version activated verbatim", activeVersion)
	}
	if len(spec.Variables) != 1 {
		t.Errorf("variables = %+v, want just the one", spec.Variables)
	}
}

// `deploy -V <ver> -e KEY=VAL` used to clone the version without its
// ConfigVersion and merge the CLI vars into the (empty) legacy inline config,
// wiping every variable, service, entrypoint, port and disk the app had.
func TestDeployVersionWithCliEnvVarsKeepsFullConfig(t *testing.T) {
	f := newEnvFixture(t, "cliapp",
		[]core_v1alpha.ConfigSpecVariables{cfgVar("LOG_LEVEL", "debug", coreutil.SourceConfig)},
		[]core_v1alpha.ConfigSpecVariables{
			cfgVar("LOG_LEVEL", "debug", coreutil.SourceConfig),
			cfgVar("DATABASE_URL", "postgres://live/db", coreutil.SourceAddon),
		})

	ev := &deployment_v1alpha.EnvironmentVariable{}
	ev.SetKey("FEATURE_FLAG")
	ev.SetValue("on")

	spec, _ := f.deploy(t, []*deployment_v1alpha.EnvironmentVariable{ev})

	if got := lookupVar(t, spec, "FEATURE_FLAG"); got.Value != "on" || got.Source != coreutil.SourceManual {
		t.Errorf("FEATURE_FLAG = %+v, want value \"on\" with source %q", got, coreutil.SourceManual)
	}
	if !hasVar(spec, "LOG_LEVEL") {
		t.Error("LOG_LEVEL lost — the derived version dropped the target's config")
	}
	if !hasVar(spec, "DATABASE_URL") {
		t.Error("DATABASE_URL lost — the derived version dropped the carried addon vars")
	}
	if spec.Entrypoint != "/app/server" {
		t.Errorf("entrypoint = %q, want the target version's config to survive", spec.Entrypoint)
	}
}

// A -e/-s that shadows an app.toml declaration must not blank the metadata that
// declaration carries: `miren env list` shows the description, and the next
// build's validateRequiredVars reads Required.
func TestDeployVersionCliEnvVarsKeepDeclaredMetadata(t *testing.T) {
	declared := core_v1alpha.ConfigSpecVariables{
		Key:         "API_TOKEN",
		Value:       "",
		Source:      coreutil.SourceConfig,
		Required:    true,
		Description: "Shared secret for the write endpoints",
	}
	f := newEnvFixture(t, "metaapp",
		[]core_v1alpha.ConfigSpecVariables{declared},
		[]core_v1alpha.ConfigSpecVariables{declared})

	ev := &deployment_v1alpha.EnvironmentVariable{}
	ev.SetKey("API_TOKEN")
	ev.SetValue("supplied")
	ev.SetSensitive(true)

	spec, _ := f.deploy(t, []*deployment_v1alpha.EnvironmentVariable{ev})

	got := lookupVar(t, spec, "API_TOKEN")
	if got.Value != "supplied" || got.Source != coreutil.SourceManual || !got.Sensitive {
		t.Errorf("API_TOKEN = %+v, want the supplied value as a sensitive manual var", got)
	}
	if !got.Required {
		t.Error("Required was dropped; the next build would treat a declared-required var as optional")
	}
	if got.Description != declared.Description {
		t.Errorf("Description = %q, want it carried from the app.toml declaration", got.Description)
	}
}

// A CLI-supplied value is the operator being explicit, so it beats the value
// currently live on the app.
func TestDeployVersionCliEnvVarsOverrideCarriedVars(t *testing.T) {
	f := newEnvFixture(t, "overrideapp",
		[]core_v1alpha.ConfigSpecVariables{cfgVar("API_TOKEN", "old", coreutil.SourceManual)},
		[]core_v1alpha.ConfigSpecVariables{cfgVar("API_TOKEN", "live", coreutil.SourceManual)})

	ev := &deployment_v1alpha.EnvironmentVariable{}
	ev.SetKey("API_TOKEN")
	ev.SetValue("explicit")

	spec, _ := f.deploy(t, []*deployment_v1alpha.EnvironmentVariable{ev})

	if got := lookupVar(t, spec, "API_TOKEN"); got.Value != "explicit" {
		t.Errorf("API_TOKEN = %q, want the CLI-supplied value", got.Value)
	}
}

// An ephemeral -V deploy must mint exactly one AppVersion: the ephemeral one.
// Deriving a full version pair first left the intermediate AppVersion orphaned —
// never activated, never referenced, but still consuming one of the retention
// GC's RetentionCount slots on every preview deploy.
func TestDeployVersionEphemeralDoesNotOrphanADerivedVersion(t *testing.T) {
	f := newEnvFixture(t, "ephapp",
		[]core_v1alpha.ConfigSpecVariables{cfgVar("LOG_LEVEL", "info", coreutil.SourceConfig)},
		[]core_v1alpha.ConfigSpecVariables{
			cfgVar("LOG_LEVEL", "info", coreutil.SourceConfig),
			cfgVar("DATABASE_URL", "postgres://live/db", coreutil.SourceAddon),
		})

	before := countAppVersions(t, f)

	result, err := f.client.DeployVersion(f.ctx, f.appName, "cluster1", f.appName+"-target", false, nil, "preview", "1h")
	if err != nil {
		t.Fatalf("DeployVersion failed: %v", err)
	}
	if result.HasError() && result.Error() != "" {
		t.Fatalf("DeployVersion returned error: %s", result.Error())
	}

	if got := countAppVersions(t, f) - before; got != 1 {
		t.Errorf("created %d AppVersions, want exactly 1 (the ephemeral one)", got)
	}

	// The ephemeral version still has to carry the app's addon bindings.
	eph := ephemeralVersion(t, f, "preview")
	spec, err := coreutil.ResolveConfig(f.ctx, f.inmem.EAC, eph)
	if err != nil {
		t.Fatalf("failed to resolve ephemeral config: %v", err)
	}
	if !hasVar(spec, "DATABASE_URL") {
		t.Errorf("ephemeral version missing carried addon var: %+v", spec.Variables)
	}
	if !hasVar(spec, "LOG_LEVEL") {
		t.Errorf("ephemeral version lost the target version's config: %+v", spec.Variables)
	}
}

// A legacy version stores its config inline with no ConfigVersion. When there is
// nothing to carry forward, an ephemeral deploy must leave that inline config
// alone rather than blanking it alongside a ConfigVersion it never minted.
func TestDeployVersionEphemeralKeepsLegacyInlineConfig(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	if err != nil {
		t.Fatalf("failed to create deployment server: %v", err)
	}

	appID, err := inmem.Client.Create(ctx, "legacyapp", &core_v1alpha.App{})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// No ConfigVersion — config lives in the inline Config field.
	versionID, err := inmem.Client.Create(ctx, "legacyapp-target", &core_v1alpha.AppVersion{
		App:     appID,
		Version: "legacyapp-target",
		Config: core_v1alpha.Config{
			Entrypoint: "/app/legacy",
			Variable:   []core_v1alpha.Variable{{Key: "LEGACY_VAR", Value: "kept", Source: coreutil.SourceConfig}},
		},
	})
	if err != nil {
		t.Fatalf("failed to create app version: %v", err)
	}
	if err := inmem.Client.Update(ctx, &core_v1alpha.App{ID: appID, ActiveVersion: versionID}); err != nil {
		t.Fatalf("failed to set active version: %v", err)
	}

	f := &envFixture{
		ctx:     ctx,
		inmem:   inmem,
		server:  server,
		client:  &deployment_v1alpha.DeploymentClient{Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server))},
		appName: "legacyapp",
		appID:   appID,
	}

	result, err := f.client.DeployVersion(ctx, "legacyapp", "cluster1", "legacyapp-target", false, nil, "preview", "1h")
	if err != nil {
		t.Fatalf("DeployVersion failed: %v", err)
	}
	if result.HasError() && result.Error() != "" {
		t.Fatalf("DeployVersion returned error: %s", result.Error())
	}

	eph := ephemeralVersion(t, f, "preview")
	spec, err := coreutil.ResolveConfig(ctx, inmem.EAC, eph)
	if err != nil {
		t.Fatalf("failed to resolve ephemeral config: %v", err)
	}
	if !hasVar(spec, "LEGACY_VAR") {
		t.Errorf("inline config was blanked: %+v", spec.Variables)
	}
	if spec.Entrypoint != "/app/legacy" {
		t.Errorf("entrypoint = %q, want the legacy inline value", spec.Entrypoint)
	}
}

func countAppVersions(t *testing.T, f *envFixture) int {
	t.Helper()
	resp, err := f.inmem.EAC.List(f.ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindAppVersion))
	if err != nil {
		t.Fatalf("failed to list app versions: %v", err)
	}
	return len(resp.Values())
}

func ephemeralVersion(t *testing.T, f *envFixture, label string) *core_v1alpha.AppVersion {
	t.Helper()
	resp, err := f.inmem.EAC.List(f.ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindAppVersion))
	if err != nil {
		t.Fatalf("failed to list app versions: %v", err)
	}
	for _, e := range resp.Values() {
		var av core_v1alpha.AppVersion
		av.Decode(e.Entity())
		if av.EphemeralLabel == label {
			return &av
		}
	}
	t.Fatalf("no ephemeral version with label %q", label)
	return nil
}

// ReplaceExisting deletes the preview currently holding the label. Resolving the
// config after that point would mean a resolution failure leaves the operator
// with neither the old preview nor a new one, so the resolve has to come first.
func TestDeployVersionEphemeralKeepsOldPreviewWhenResolveFails(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	server, err := newTestDeploymentServer(t, slog.Default(), inmem)
	if err != nil {
		t.Fatalf("failed to create deployment server: %v", err)
	}

	appID, err := inmem.Client.Create(ctx, "previewapp", &core_v1alpha.App{})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// A target version pointing at a ConfigVersion that does not exist, so
	// resolving its config fails.
	if _, err := inmem.Client.Create(ctx, "previewapp-target", &core_v1alpha.AppVersion{
		App:           appID,
		Version:       "previewapp-target",
		ConfigVersion: entity.Id("missing-config-version"),
	}); err != nil {
		t.Fatalf("failed to create app version: %v", err)
	}

	// The preview that must survive the failed deploy.
	existingID, err := inmem.Client.Create(ctx, "previewapp-eph-old", &core_v1alpha.AppVersion{
		App:            appID,
		Version:        "previewapp-eph-old",
		EphemeralLabel: "preview",
		EphemeralTtl:   "24h",
	})
	if err != nil {
		t.Fatalf("failed to create existing ephemeral version: %v", err)
	}

	client := &deployment_v1alpha.DeploymentClient{Client: rpc.LocalClient(deployment_v1alpha.AdaptDeployment(server))}
	result, err := client.DeployVersion(ctx, "previewapp", "cluster1", "previewapp-target", false, nil, "preview", "1h")
	if err != nil {
		t.Fatalf("DeployVersion returned a transport error: %v", err)
	}
	if !result.HasError() || result.Error() == "" {
		t.Fatal("expected the deploy to fail on the unresolvable config")
	}

	var survivor core_v1alpha.AppVersion
	if err := inmem.Client.GetById(ctx, existingID, &survivor); err != nil {
		t.Fatalf("the existing preview was deleted before the config failed to resolve: %v", err)
	}
	if survivor.EphemeralLabel != "preview" {
		t.Errorf("existing preview lost its label: %+v", survivor)
	}
}

// A -e/-s literal replaces a secret reference on that key outright: the value is
// inline now, so the old backend pointer must not survive and turn it back into
// a reference. A caller that does supply a backend keeps it, so the reference
// still reaches createConfigVersion to be pinned.
func TestDeployVersionCliEnvVarBackendHandling(t *testing.T) {
	referenced := core_v1alpha.ConfigSpecVariables{
		Key:       "API_TOKEN",
		Value:     "prod/api-token",
		Backend:   "vault",
		Sensitive: true,
		Source:    coreutil.SourceManual,
	}

	t.Run("literal clears the backend it replaces", func(t *testing.T) {
		f := newEnvFixture(t, "litapp",
			[]core_v1alpha.ConfigSpecVariables{referenced},
			[]core_v1alpha.ConfigSpecVariables{referenced})

		ev := &deployment_v1alpha.EnvironmentVariable{}
		ev.SetKey("API_TOKEN")
		ev.SetValue("inline-literal")

		spec, _ := f.deploy(t, []*deployment_v1alpha.EnvironmentVariable{ev})

		got := lookupVar(t, spec, "API_TOKEN")
		if got.Value != "inline-literal" {
			t.Errorf("value = %q, want the literal", got.Value)
		}
		if got.Backend != "" {
			t.Errorf("backend = %q, want it cleared so the literal is not read as a reference", got.Backend)
		}
	})

	t.Run("supplied backend reference is preserved and pinned", func(t *testing.T) {
		f := newEnvFixture(t, "refapp",
			[]core_v1alpha.ConfigSpecVariables{cfgVar("API_TOKEN", "old", coreutil.SourceManual)},
			[]core_v1alpha.ConfigSpecVariables{cfgVar("API_TOKEN", "old", coreutil.SourceManual)})
		f.server.Secrets = pinningResolver{}

		ev := &deployment_v1alpha.EnvironmentVariable{}
		ev.SetKey("API_TOKEN")
		ev.SetValue("prod/api-token")
		ev.SetBackend("vault")
		ev.SetSensitive(true)

		spec, _ := f.deploy(t, []*deployment_v1alpha.EnvironmentVariable{ev})

		got := lookupVar(t, spec, "API_TOKEN")
		if got.Backend != "vault" {
			t.Errorf("backend = %q, want it carried through so the reference can be pinned", got.Backend)
		}
		// Reaching the resolver at all is the point: a dropped backend would
		// have left this an unpinned literal.
		if got.Value != "prod/api-token@v1" {
			t.Errorf("value = %q, want the reference pinned to a concrete version", got.Value)
		}
	})
}

// pinningResolver stands in for a cluster secret backend, resolving any ref to a
// fixed version so a pinned ConfigVersion can be asserted.
type pinningResolver struct{}

func (pinningResolver) ResolveRef(_ context.Context, _, ref string) (secret.SecretValue, error) {
	return secret.SecretValue{Ref: ref + "@v1", Bytes: []byte("value")}, nil
}
