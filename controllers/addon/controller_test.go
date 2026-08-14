package addon

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/saga"
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

// testProvider is a configurable mock for testing controller behavior.
type testProvider struct {
	localityMode   addon.LocalityMode
	provisionFn    func(ctx context.Context, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error)
	deprovisionFn  func(ctx context.Context, assoc addon.AddonAssociation) error
	deprovisionErr error
	adjustErr      error

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
	if p.adjustErr != nil {
		return nil, p.adjustErr
	}
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

	// Tests that care reach this back through ctrl.sagaStorage.
	ctrl := NewController(slog.Default(), ec, inmem.EAC, registry, saga.NewMemoryStorage())

	return ctx, ctrl, ec, provider
}

// Naming a teardown after its association makes a failed one permanent unless
// something retires the record: Execute would replay the recorded error on
// every later pass and never call the provider again, so `addon destroy` would
// be one-shot. Every undo in these sagas is a no-op, so a failed run left its
// resources in place and a retry has only more to do.
func TestDeprovisionRetriesAfterAFailedTeardownSaga(t *testing.T) {
	ctx, ctrl, ec, provider := setupControllerTest(t)

	appID := createAppWithVars(t, ctx, ec, "myapp", nil)
	addonID, err := ec.Create(ctx, "miren-postgresql", &addon_v1alpha.Addon{
		Name: "miren-postgresql",
	})
	require.NoError(t, err)

	assocID, err := ec.Create(ctx, "test-assoc", &addon_v1alpha.AddonAssociation{
		App:     appID,
		Addon:   addonID,
		Variant: "small",
		Status:  "deprovisioning",
	})
	require.NoError(t, err)

	// What an earlier teardown that gave up leaves behind.
	require.NoError(t, ctrl.sagaStorage.Save(ctx, &saga.Execution{
		ID:             addon.DeprovisionExecutionID(assocID),
		DefinitionName: "deprovision-dedicated-postgresql",
		Status:         saga.StatusFailed,
		Error:          "engine unreachable",
	}))

	var assoc addon_v1alpha.AddonAssociation
	meta, err := getMeta(ctx, ec, assocID, &assoc)
	require.NoError(t, err)

	require.NoError(t, ctrl.Reconcile(ctx, &assoc, meta))

	// The load-bearing assertion. The provider here is a mock that never runs a
	// saga, so it would be called either way; what decides whether a real one
	// re-runs is whether the failed record is still sitting under that name.
	_, err = ctrl.sagaStorage.Get(ctx, addon.DeprovisionExecutionID(assocID))
	assert.ErrorIs(t, err, saga.ErrExecutionNotFound,
		"a failed teardown record must be retired so the next Execute runs the saga")

	assert.True(t, provider.deprovisionCalled, "and the teardown still reaches the provider")
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

	// The app already sets DATABASE_URL, which is what testProvider contributes,
	// so completeProvision reaches AdjustEnvVars — a step that runs after
	// Provision has already created resources, which is what makes compensation
	// necessary.
	appID := createAppWithVars(t, ctx, ec, "myapp", []core_v1alpha.Variable{
		{Key: "DATABASE_URL", Value: "postgres://existing", Source: "manual"},
	})
	provider.adjustErr = errors.New("cannot adjust")

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

// MIR-1524 itself. provision() writes "provisioning" before the work starts, so
// a coordinator that dies partway leaves the association sitting on it. The
// switch had no case for that value, so every later pass fell through and
// returned in silence, and the association was never picked back up.
func TestReconcileResumesAnInterruptedProvision(t *testing.T) {
	ctx, ctrl, ec, provider := setupControllerTest(t)

	appID := createAppWithVars(t, ctx, ec, "myapp", nil)

	addonID, err := ec.Create(ctx, "miren-postgresql", &addon_v1alpha.Addon{
		Name: "miren-postgresql",
	})
	require.NoError(t, err)

	// Exactly what a crash mid-provision leaves behind.
	assocID, err := ec.Create(ctx, "test-assoc", &addon_v1alpha.AddonAssociation{
		App:     appID,
		Addon:   addonID,
		Variant: "small",
		Status:  "provisioning",
	})
	require.NoError(t, err)

	var assoc addon_v1alpha.AddonAssociation
	meta, err := getMeta(ctx, ec, assocID, &assoc)
	require.NoError(t, err)

	require.NoError(t, ctrl.Reconcile(ctx, &assoc, meta))

	assert.True(t, provider.provisionCalled,
		"an interrupted provision must be picked back up, not skipped in silence")

	// Read the status off meta rather than the store: the controller framework
	// flushes a handler's writes once it returns, so calling Reconcile directly
	// leaves them staged here.
	var staged addon_v1alpha.AddonAssociation
	staged.Decode(meta.Entity)
	assert.Equal(t, "active", staged.Status, "and run through to a settled status")
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

	// Delete the app. Deprovision no longer touches the app's config at all,
	// so a missing app is simply not its problem and teardown still completes.
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
