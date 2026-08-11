package addon

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestAddonNameFromRef(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"addon/miren-postgresql", "miren-postgresql"},
		{"miren-postgresql", "miren-postgresql"},
		{"addon/ns/miren-postgresql", "miren-postgresql"},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := addon.NameFromRef(entity.Id("dev.miren.addon/" + tt.ref))
			assert.Equal(t, tt.want, got)
		})
	}

	// Direct test without prefix
	assert.Equal(t, "miren-postgresql", addon.NameFromRef(entity.Id("addon/miren-postgresql")))
}

func TestFindCollisions(t *testing.T) {
	existing := []core_v1alpha.Variable{
		{Key: "DATABASE_URL", Value: "old", Source: "manual"},
		{Key: "PGHOST", Value: "old", Source: "config"},
		{Key: "OTHER_VAR", Value: "val", Source: "manual"},
	}

	addonVars := []addon.Variable{
		{Key: "DATABASE_URL", Value: "new"},
		{Key: "PGHOST", Value: "new"},
		{Key: "PGPORT", Value: "5432"},
	}

	collisions := findCollisions(existing, addonVars)
	assert.ElementsMatch(t, []string{"DATABASE_URL", "PGHOST"}, collisions)
}

func TestFindCollisionsNoOverlap(t *testing.T) {
	existing := []core_v1alpha.Variable{
		{Key: "OTHER_VAR", Value: "val"},
	}

	addonVars := []addon.Variable{
		{Key: "DATABASE_URL", Value: "new"},
	}

	collisions := findCollisions(existing, addonVars)
	assert.Empty(t, collisions)
}

func TestMergeAddonVars(t *testing.T) {
	existing := []core_v1alpha.Variable{
		{Key: "MANUAL_VAR", Value: "manual_val", Source: "manual"},
		{Key: "CONFIG_VAR", Value: "config_val", Source: "config"},
	}

	addonVars := []addon.Variable{
		{Key: "DATABASE_URL", Value: "postgres://...", Sensitive: true},
		{Key: "PGHOST", Value: "host.addon.app.miren"},
		{Key: "MANUAL_VAR", Value: "should_not_override"},
		{Key: "CONFIG_VAR", Value: "should_override"},
	}

	result := mergeAddonVars(existing, addonVars)

	varMap := make(map[string]core_v1alpha.Variable, len(result))
	for _, v := range result {
		varMap[v.Key] = v
	}

	// Manual var should NOT be overridden
	assert.Equal(t, "manual_val", varMap["MANUAL_VAR"].Value)
	assert.Equal(t, "manual", varMap["MANUAL_VAR"].Source)

	// Config var should be overridden by addon
	assert.Equal(t, "should_override", varMap["CONFIG_VAR"].Value)
	assert.Equal(t, "addon", varMap["CONFIG_VAR"].Source)

	// New addon vars should be added
	assert.Equal(t, "postgres://...", varMap["DATABASE_URL"].Value)
	assert.True(t, varMap["DATABASE_URL"].Sensitive)
	assert.Equal(t, "addon", varMap["DATABASE_URL"].Source)

	assert.Equal(t, "host.addon.app.miren", varMap["PGHOST"].Value)
	assert.Equal(t, "addon", varMap["PGHOST"].Source)
}

func TestMergeAddonVarsEmptySource(t *testing.T) {
	// Vars with empty source should be treated as manual (backward compat)
	existing := []core_v1alpha.Variable{
		{Key: "DATABASE_URL", Value: "old", Source: ""},
	}

	addonVars := []addon.Variable{
		{Key: "DATABASE_URL", Value: "new"},
	}

	result := mergeAddonVars(existing, addonVars)

	varMap := make(map[string]core_v1alpha.Variable, len(result))
	for _, v := range result {
		varMap[v.Key] = v
	}

	// Empty source treated as manual — should NOT be overridden
	assert.Equal(t, "old", varMap["DATABASE_URL"].Value)
}

func TestMergeAddonVarsEmpty(t *testing.T) {
	result := mergeAddonVars(nil, nil)
	assert.Empty(t, result)
}

// testProvider is a configurable mock for testing controller behavior.
type testProvider struct {
	localityMode   addon.LocalityMode
	provisionFn    func(ctx context.Context, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error)
	deprovisionFn  func(ctx context.Context, assoc addon.AddonAssociation) error
	deprovisionErr error

	provisionCalled   bool
	deprovisionCalled bool
}

func (p *testProvider) LocalityMode() addon.LocalityMode {
	if p.localityMode == "" {
		return addon.OnCluster
	}
	return p.localityMode
}

func (p *testProvider) Provision(ctx context.Context, _ addon.AddonAssociation, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	p.provisionCalled = true
	if p.provisionFn != nil {
		return p.provisionFn(ctx, app, variant)
	}
	return &addon.ProvisionResult{
		EnvVars: []addon.Variable{
			{Key: "DATABASE_URL", Value: "postgres://test", Sensitive: true},
		},
	}, nil
}

func (p *testProvider) AdjustEnvVars(ctx context.Context, result *addon.ProvisionResult, assoc addon.AddonAssociation, collisions []string) ([]addon.Variable, error) {
	return result.EnvVars, nil
}

func (p *testProvider) Deprovision(ctx context.Context, assoc addon.AddonAssociation) error {
	p.deprovisionCalled = true
	if p.deprovisionFn != nil {
		return p.deprovisionFn(ctx, assoc)
	}
	if p.deprovisionErr != nil {
		return p.deprovisionErr
	}
	return nil
}

func setupControllerTest(t *testing.T) (context.Context, *Controller, *entityserver.Client, *testProvider) {
	t.Helper()

	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	provider := &testProvider{}
	registry := addon.NewRegistry()
	registry.Register("miren-postgresql", provider, addon.AddonDefinition{
		Name:           "miren-postgresql",
		DisplayName:    "PostgreSQL",
		DefaultVariant: "small",
		Variants: []addon.VariantDefinition{
			{Name: "small", Description: "Small"},
		},
	})

	ctrl := NewController(slog.Default(), ec, inmem.EAC, registry)

	return ctx, ctrl, ec, provider
}

// getMeta fetches an entity by ID and returns both the decoded struct and a Meta
// suitable for use in controller Reconcile calls.
func getMeta(ctx context.Context, ec *entityserver.Client, id entity.Id, sc entityserver.SchemaEncoder) (*entity.Meta, error) {
	ent, err := ec.GetByIdWithEntity(ctx, id, sc)
	if err != nil {
		return nil, err
	}
	e := ent.Entity()
	return &entity.Meta{
		Entity:   e,
		Revision: e.GetRevision(),
	}, nil
}

func TestProvisionCompensatesOnPostProvisionFailure(t *testing.T) {
	ctx, ctrl, ec, provider := setupControllerTest(t)

	// Create an app entity without an active version — this will cause
	// createVersionWithAddonVars to fail with "app has no active version",
	// triggering the compensation path.
	appID, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)

	addonID, err := ec.Create(ctx, "miren-postgresql", &addon_v1alpha.Addon{
		Name: "miren-postgresql",
	})
	require.NoError(t, err)

	assocID, err := ec.Create(ctx, "test-assoc", &addon_v1alpha.AddonAssociation{
		App:     appID,
		Addon:   addonID,
		Variant: "small",
		Status:  "pending",
	})
	require.NoError(t, err)

	var assoc addon_v1alpha.AddonAssociation
	meta, err := getMeta(ctx, ec, assocID, &assoc)
	require.NoError(t, err)

	_ = ctrl.Reconcile(ctx, &assoc, meta)

	assert.True(t, provider.provisionCalled, "Provision should have been called")
	assert.True(t, provider.deprovisionCalled, "Deprovision should have been called as compensation after post-provision failure")
}

// TestProvisionSkipsWhenAssociationNoLongerPending verifies the pre-flight
// re-read: if a stale Reconcile event routes through provision() but the
// association has since moved to "deprovisioning" (e.g. on startup resync
// after the user ran `addon destroy`), provision() must be a no-op so the
// subsequent deprovisioning event can run.
func TestProvisionSkipsWhenAssociationNoLongerPending(t *testing.T) {
	ctx, ctrl, ec, provider := setupControllerTest(t)

	appID, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)

	addonID, err := ec.Create(ctx, "miren-postgresql", &addon_v1alpha.Addon{
		Name: "miren-postgresql",
	})
	require.NoError(t, err)

	assocID, err := ec.Create(ctx, "test-assoc", &addon_v1alpha.AddonAssociation{
		App:     appID,
		Addon:   addonID,
		Variant: "small",
		Status:  "pending",
	})
	require.NoError(t, err)

	// Capture a stale "pending" view of the association.
	var stale addon_v1alpha.AddonAssociation
	meta, err := getMeta(ctx, ec, assocID, &stale)
	require.NoError(t, err)

	// In the meantime, the user destroys the addon and the status flips.
	require.NoError(t, ec.Patch(ctx, assocID, 0,
		(&addon_v1alpha.AddonAssociation{Status: "deprovisioning"}).Encode()...,
	))

	// Now reconcile the stale event. provision() should re-read, see the
	// association is no longer "pending", and skip without calling the provider.
	require.NoError(t, ctrl.Reconcile(ctx, &stale, meta))

	assert.False(t, provider.provisionCalled, "Provision should NOT be called when association is no longer pending")

	// The on-disk status should remain "deprovisioning" — provision() must not
	// have overwritten it.
	var current addon_v1alpha.AddonAssociation
	require.NoError(t, ec.GetById(ctx, assocID, &current))
	assert.Equal(t, "deprovisioning", current.Status)
}

// createAppWithVars creates an app with an active AppVersion whose inline config
// carries the given variables, returning the app's entity ID.
func createAppWithVars(t *testing.T, ctx context.Context, ec *entityserver.Client, name string, vars []core_v1alpha.Variable) entity.Id {
	t.Helper()

	cfgVars := make([]core_v1alpha.Variable, len(vars))
	copy(cfgVars, vars)

	verID, err := ec.Create(ctx, name+"-v0", &core_v1alpha.AppVersion{
		Version: name + "-v0",
		Config:  core_v1alpha.Config{Variable: cfgVars},
	})
	require.NoError(t, err)

	appID, err := ec.Create(ctx, name, &core_v1alpha.App{ActiveVersion: verID})
	require.NoError(t, err)

	return appID
}

// activeVars resolves the variables on an app's current active version, keyed by name.
func activeVars(t *testing.T, ctx context.Context, ctrl *Controller, appID entity.Id) map[string]core_v1alpha.ConfigSpecVariables {
	t.Helper()

	var app core_v1alpha.App
	require.NoError(t, ctrl.ec.GetById(ctx, appID, &app))

	var ver core_v1alpha.AppVersion
	require.NoError(t, ctrl.ec.GetById(ctx, app.ActiveVersion, &ver))

	spec, err := coreutil.ResolveConfig(ctx, ctrl.eac, &ver)
	require.NoError(t, err)

	out := make(map[string]core_v1alpha.ConfigSpecVariables, len(spec.Variables))
	for _, v := range spec.Variables {
		out[v.Key] = v
	}
	return out
}

// TestUpdateAppActiveVersionRetriesOnConcurrentSwing is the MIR-1458 regression
// test. Two addon associations for the same app reconcile concurrently and each
// swings the app's active version. Without optimistic concurrency control the
// second write clobbers the first, silently dropping one addon's variables.
//
// The race is reproduced deterministically without goroutines: a competing addon
// (memcache) swings the active version exactly once, during our transform's first
// invocation — i.e. after we captured the app revision but before our own Patch.
// Our Patch must then conflict, the read-modify-write must retry against the
// competitor's version, and the final active config must carry BOTH addons' vars.
func TestUpdateAppActiveVersionRetriesOnConcurrentSwing(t *testing.T) {
	ctx, ctrl, ec, _ := setupControllerTest(t)

	appID := createAppWithVars(t, ctx, ec, "myapp", []core_v1alpha.Variable{
		{Key: "BASE", Value: "1", Source: "config"},
	})

	competed := false
	err := updateAppActiveVersion(ctx, ctrl.log, ctrl.ec, ctrl.eac, appID, func(spec *core_v1alpha.ConfigSpec) error {
		if !competed {
			competed = true
			// A competing addon (memcache) wins the active-version swing while
			// we are mid-flight, bumping the app revision under us.
			require.NoError(t, ctrl.createVersionWithAddonVars(ctx, appID, []addon.Variable{
				{Key: "MEMCACHE_URL", Value: "memcache://x"},
			}))
		}
		spec.Variables = append(spec.Variables, core_v1alpha.ConfigSpecVariables{
			Key: "DATABASE_URL", Value: "postgres://y", Sensitive: true, Source: "addon",
		})
		return nil
	})
	require.NoError(t, err)
	require.True(t, competed, "the competing swing must have run")

	vars := activeVars(t, ctx, ctrl, appID)
	assert.Equal(t, "1", vars["BASE"].Value, "base config var must be preserved")
	assert.Equal(t, "memcache://x", vars["MEMCACHE_URL"].Value,
		"competing addon's var must survive the retry (would be clobbered without OCC)")
	assert.Equal(t, "postgres://y", vars["DATABASE_URL"].Value, "our addon's var must be present")

	// The version pair minted on the losing first attempt must have been deleted,
	// not left to accumulate: base + competitor + final activated = 3 AppVersions.
	assert.Equal(t, 3, countKind(t, ctx, ec, core_v1alpha.KindAppVersion),
		"the superseded version from the lost CAS race must be cleaned up")
}

// countKind returns the number of entities of the given kind in the store.
func countKind(t *testing.T, ctx context.Context, ec *entityserver.Client, kind entity.Id) int {
	t.Helper()
	res, err := ec.List(ctx, entity.Ref(entity.EntityKind, kind))
	require.NoError(t, err)
	n := 0
	for res.Next() {
		n++
	}
	return n
}

// TestCreateVersionWithAddonVarsComposesSequentially verifies that attaching two
// addons one after another accumulates both var sets (the non-racing baseline).
func TestCreateVersionWithAddonVarsComposesSequentially(t *testing.T) {
	ctx, ctrl, ec, _ := setupControllerTest(t)

	appID := createAppWithVars(t, ctx, ec, "myapp", []core_v1alpha.Variable{
		{Key: "BASE", Value: "1", Source: "config"},
	})

	require.NoError(t, ctrl.createVersionWithAddonVars(ctx, appID, []addon.Variable{
		{Key: "DATABASE_URL", Value: "postgres://y", Sensitive: true},
	}))
	require.NoError(t, ctrl.createVersionWithAddonVars(ctx, appID, []addon.Variable{
		{Key: "MEMCACHE_URL", Value: "memcache://x"},
	}))

	vars := activeVars(t, ctx, ctrl, appID)
	assert.Equal(t, "1", vars["BASE"].Value)
	assert.Equal(t, "postgres://y", vars["DATABASE_URL"].Value)
	assert.Equal(t, "memcache://x", vars["MEMCACHE_URL"].Value)
}

func TestDeprovisionCompletesWhenAppDeleted(t *testing.T) {
	ctx, ctrl, ec, provider := setupControllerTest(t)

	appID, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)

	addonID, err := ec.Create(ctx, "miren-postgresql", &addon_v1alpha.Addon{
		Name: "miren-postgresql",
	})
	require.NoError(t, err)

	assocID, err := ec.Create(ctx, "test-assoc", &addon_v1alpha.AddonAssociation{
		App:     appID,
		Addon:   addonID,
		Variant: "small",
		Status:  "deprovisioning",
		Variables: []addon_v1alpha.Variables{
			{Key: "DATABASE_URL", Value: "postgres://test", Sensitive: true},
		},
	})
	require.NoError(t, err)

	// Delete the app — removeEnvVars should treat this as a no-op
	// so that deprovision can complete and the association is cleaned up.
	require.NoError(t, ec.Delete(ctx, appID))

	var assoc addon_v1alpha.AddonAssociation
	meta, err := getMeta(ctx, ec, assocID, &assoc)
	require.NoError(t, err)

	reconcileErr := ctrl.Reconcile(ctx, &assoc, meta)
	require.NoError(t, reconcileErr, "deprovision should succeed when app is already deleted")

	// Provider.Deprovision should have been called
	assert.True(t, provider.deprovisionCalled)

	// The association should have been deleted (cleanup completed)
	var gone addon_v1alpha.AddonAssociation
	err = ec.GetById(ctx, assocID, &gone)
	require.Error(t, err, "association should be deleted after successful deprovision")
}
