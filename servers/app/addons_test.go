package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

type mockProvider struct{}

func (m *mockProvider) LocalityMode() addon.LocalityMode {
	return addon.OnCluster
}

func (m *mockProvider) Provision(ctx context.Context, app addon.App, variant addon.Variant) (*addon.ProvisionResult, error) {
	return &addon.ProvisionResult{}, nil
}

func (m *mockProvider) AdjustEnvVars(ctx context.Context, result *addon.ProvisionResult, assoc addon.AddonAssociation, collisions []string) ([]addon.Variable, error) {
	return nil, nil
}

func (m *mockProvider) Deprovision(ctx context.Context, assoc addon.AddonAssociation) error {
	return nil
}

func setupAddonsTest(t *testing.T) (context.Context, *app_v1alpha.AddonsClient, *entityserver.Client) {
	t.Helper()

	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	return ctx, newAddonsClient(t, ctx, ec), ec
}

// newAddonsClient wires an AddonsServer (with a single registered mock provider)
// over the given entity client. Shared by the mock-backed unit tests and the
// etcd-backed concurrency test so both exercise the same handler.
func newAddonsClient(t *testing.T, ctx context.Context, ec *entityserver.Client) *app_v1alpha.AddonsClient {
	t.Helper()

	registry := addon.NewRegistry()
	registry.Register("miren-postgresql", &mockProvider{}, addon.AddonDefinition{
		Name:           "miren-postgresql",
		DisplayName:    "PostgreSQL",
		DefaultVariant: "small",
		Variants: []addon.VariantDefinition{
			{Name: "small", Description: "Small"},
			{Name: "shared", Description: "Shared"},
		},
	})

	// Ensure addon entities exist
	require.NoError(t, registry.EnsureEntities(ctx, ec))

	server := NewAddonsServer(slog.Default(), ec, registry, nil)

	return &app_v1alpha.AddonsClient{
		Client: rpc.LocalClient(app_v1alpha.AdaptAddons(server)),
	}
}

func TestAddonsCreateInstance(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	// Create test app
	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "myapp", app)
	require.NoError(t, err)

	// Create addon instance
	result, err := client.CreateInstance(ctx, "test", "miren-postgresql", "small", "myapp", "")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Id())
}

func TestAddonsCreateInstanceDefaultVariant(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "myapp", app)
	require.NoError(t, err)

	// Create with empty variant — should use default
	result, err := client.CreateInstance(ctx, "test", "miren-postgresql", "", "myapp", "")
	require.NoError(t, err)
	assert.NotEmpty(t, result.Id())
}

func TestAddonsCreateInstanceDuplicatePrevented(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "myapp", app)
	require.NoError(t, err)

	// Create first instance
	_, err = client.CreateInstance(ctx, "test", "miren-postgresql", "small", "myapp", "")
	require.NoError(t, err)

	// Attempt duplicate
	_, err = client.CreateInstance(ctx, "test2", "miren-postgresql", "small", "myapp", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already attached")
}

func TestAddonsCreateInstanceUnknownAddon(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "myapp", app)
	require.NoError(t, err)

	_, err = client.CreateInstance(ctx, "test", "miren-redis", "small", "myapp", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown addon")
}

func TestAddonsListInstances(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "myapp", app)
	require.NoError(t, err)

	// List with no addons
	result, err := client.ListInstances(ctx, "myapp")
	require.NoError(t, err)
	assert.Empty(t, result.Addons())

	// Create an addon instance
	_, err = client.CreateInstance(ctx, "test", "miren-postgresql", "small", "myapp", "")
	require.NoError(t, err)

	// List again
	result, err = client.ListInstances(ctx, "myapp")
	require.NoError(t, err)
	assert.Len(t, result.Addons(), 1)
	assert.Equal(t, "miren-postgresql", result.Addons()[0].Name())
	assert.Equal(t, "small", result.Addons()[0].Variant())
}

func TestAddonsDeleteInstance(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	app := &core_v1alpha.App{}
	appID, err := ec.Create(ctx, "myapp", app)
	require.NoError(t, err)

	// Create an addon instance
	createResult, err := client.CreateInstance(ctx, "test", "miren-postgresql", "small", "myapp", "")
	require.NoError(t, err)

	// Delete it
	_, err = client.DeleteInstance(ctx, "myapp", "miren-postgresql")
	require.NoError(t, err)

	// Verify status changed to deprovisioning
	var assoc addon_v1alpha.AddonAssociation
	err = ec.GetById(ctx, entity.Id(createResult.Id()), &assoc)
	require.NoError(t, err)
	assert.Equal(t, "deprovisioning", assoc.Status)

	// Verify it's still associated with the app
	assert.Equal(t, appID, assoc.App)
}

func TestAddonsDeleteInstanceNotFound(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	app := &core_v1alpha.App{}
	_, err := ec.Create(ctx, "myapp", app)
	require.NoError(t, err)

	_, err = client.DeleteInstance(ctx, "myapp", "miren-postgresql")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not attached")
}

// createActiveAddon attaches an addon and promotes the association to "active"
// so RotateCredential will admit it. The provisioning controller that would
// normally flip the status isn't running in these unit tests.
func createActiveAddon(t *testing.T, ctx context.Context, client *app_v1alpha.AddonsClient, ec *entityserver.Client, appName string) entity.Id {
	t.Helper()

	res, err := client.CreateInstance(ctx, "test", "miren-postgresql", "small", appName, "")
	require.NoError(t, err)

	assocID := entity.Id(res.Id())
	require.NoError(t, ec.Patch(ctx, assocID, 0,
		entity.String(addon_v1alpha.AddonAssociationStatusId, "active")))
	return assocID
}

// countRotationRequests returns how many rotation_request entities exist for the
// association — the singleton gate should keep this at exactly one.
func countRotationRequests(t *testing.T, ctx context.Context, ec *entityserver.Client, assocID entity.Id) int {
	t.Helper()

	reqs, err := ec.List(ctx, entity.Ref(addon_v1alpha.RotationRequestAssociationId, assocID))
	require.NoError(t, err)

	count := 0
	for reqs.Next() {
		count++
	}
	return count
}

func TestRotateCredentialAdmitsFirstRotation(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	_, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)
	assocID := createActiveAddon(t, ctx, client, ec, "myapp")

	res, err := client.RotateCredential(ctx, "myapp", "miren-postgresql", "app")
	require.NoError(t, err)
	require.NotEmpty(t, res.Id())

	var req addon_v1alpha.RotationRequest
	require.NoError(t, ec.GetById(ctx, entity.Id(res.Id()), &req))
	assert.Equal(t, "pending", req.Status)
	assert.Equal(t, "app", req.Credential)
	assert.Equal(t, assocID, req.Association)
	assert.Equal(t, 1, countRotationRequests(t, ctx, ec, assocID))
}

func TestRotateCredentialRejectsWhileInFlight(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	_, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)
	assocID := createActiveAddon(t, ctx, client, ec, "myapp")

	res, err := client.RotateCredential(ctx, "myapp", "miren-postgresql", "app")
	require.NoError(t, err)
	reqID := entity.Id(res.Id())

	// A "pending" request already holds the association's slot.
	_, err = client.RotateCredential(ctx, "myapp", "miren-postgresql", "app")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in progress")

	// The controller moves it to "rotating" mid-flight; still no new admission.
	require.NoError(t, ec.Patch(ctx, reqID, 0,
		entity.String(addon_v1alpha.RotationRequestStatusId, "rotating")))
	_, err = client.RotateCredential(ctx, "myapp", "miren-postgresql", "app")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in progress")

	assert.Equal(t, 1, countRotationRequests(t, ctx, ec, assocID))
}

func TestRotateCredentialReclaimsAfterDone(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	_, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)
	assocID := createActiveAddon(t, ctx, client, ec, "myapp")

	res1, err := client.RotateCredential(ctx, "myapp", "miren-postgresql", "app")
	require.NoError(t, err)
	reqID := entity.Id(res1.Id())

	// Simulate the controller finishing, leaving a stale secret behind to prove
	// reclaim clears it.
	require.NoError(t, ec.Patch(ctx, reqID, 0,
		entity.String(addon_v1alpha.RotationRequestStatusId, "done"),
		entity.String(addon_v1alpha.RotationRequestNewSecretId, "leftover")))

	res2, err := client.RotateCredential(ctx, "myapp", "miren-postgresql", "root")
	require.NoError(t, err)
	// Singleton per association: the terminal request is reclaimed in place.
	assert.Equal(t, reqID, entity.Id(res2.Id()))

	var req addon_v1alpha.RotationRequest
	require.NoError(t, ec.GetById(ctx, reqID, &req))
	assert.Equal(t, "pending", req.Status)
	assert.Equal(t, "root", req.Credential)
	assert.Empty(t, req.NewSecret)
	assert.Equal(t, 1, countRotationRequests(t, ctx, ec, assocID))
}

func TestRotateCredentialReclaimsAfterError(t *testing.T) {
	ctx, client, ec := setupAddonsTest(t)

	_, err := ec.Create(ctx, "myapp", &core_v1alpha.App{})
	require.NoError(t, err)
	assocID := createActiveAddon(t, ctx, client, ec, "myapp")

	res1, err := client.RotateCredential(ctx, "myapp", "miren-postgresql", "app")
	require.NoError(t, err)
	reqID := entity.Id(res1.Id())

	// A prior rotation failed terminally with a recorded message.
	require.NoError(t, ec.Patch(ctx, reqID, 0,
		entity.String(addon_v1alpha.RotationRequestStatusId, "error"),
		entity.String(addon_v1alpha.RotationRequestErrorMessageId, "boom")))

	res2, err := client.RotateCredential(ctx, "myapp", "miren-postgresql", "app")
	require.NoError(t, err)
	assert.Equal(t, reqID, entity.Id(res2.Id()))

	var req addon_v1alpha.RotationRequest
	require.NoError(t, ec.GetById(ctx, reqID, &req))
	assert.Equal(t, "pending", req.Status)
	assert.Empty(t, req.ErrorMessage)
	assert.Equal(t, 1, countRotationRequests(t, ctx, ec, assocID))
}
