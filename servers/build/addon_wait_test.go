package build

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

const testAddonRef = entity.Id("addon/miren-postgresql")

var expectPostgres = []expectedAddon{{Name: "miren-postgresql", Variant: "small"}}

// addonWaitFixture is a Builder against an in-memory store, whose mock store
// delivers live index events so the watch path is exercised for real. The
// ceiling is shrunk so the timeout test runs in milliseconds.
type addonWaitFixture struct {
	b     *Builder
	inmem *testutils.InMemEntityServer
	appID entity.Id
}

func newAddonWaitFixture(t *testing.T, ceiling time.Duration) *addonWaitFixture {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	origCeiling := addonWaitCeiling
	addonWaitCeiling = ceiling
	t.Cleanup(func() { addonWaitCeiling = origCeiling })

	return &addonWaitFixture{
		b:     &Builder{Log: testutils.TestLogger(t), EAS: inmem.EAC},
		inmem: inmem,
		appID: entity.Id("app/demo"),
	}
}

func (f *addonWaitFixture) association(t *testing.T, name string, assoc *addon_v1alpha.AddonAssociation) entity.Id {
	t.Helper()
	assoc.App = f.appID
	if assoc.Addon == "" {
		assoc.Addon = testAddonRef
	}
	id, err := f.inmem.Client.Create(context.Background(), name, assoc)
	require.NoError(t, err)
	return id
}

func (f *addonWaitFixture) setStatus(t *testing.T, id entity.Id, status string) {
	t.Helper()
	err := f.inmem.Client.Patch(context.Background(), id, 0,
		entity.String(addon_v1alpha.AddonAssociationStatusId, status))
	require.NoError(t, err)
}

func (f *addonWaitFixture) await(expected []expectedAddon, sender StatusSender) error {
	return f.b.awaitAddons(context.Background(), "demo", f.appID, expected, sender)
}

// The wait has to release once the addon controller flips the association,
// and the user watching the deploy has to see both ends of it.
func TestAwaitAddonsReturnsWhenAssociationGoesActive(t *testing.T) {
	f := newAddonWaitFixture(t, 5*time.Second)
	id := f.association(t, "assoc-pending", &addon_v1alpha.AddonAssociation{
		Variant: "small",
		Status:  "pending",
	})

	go func() {
		time.Sleep(50 * time.Millisecond)
		f.setStatus(t, id, "active")
	}()

	sender := &recordingSender{}
	require.NoError(t, f.await(expectPostgres, sender))

	assert.Equal(t, []string{
		"Provisioning addon miren-postgresql (small)...",
		"Addon miren-postgresql ready",
	}, sender.Messages)
}

// The deploy declared the addon, so the wait must hold even before the
// association record is visible: a slow index must not let the deploy
// through as though there were nothing to wait for.
func TestAwaitAddonsWaitsForAnAssociationThatDoesNotExistYet(t *testing.T) {
	f := newAddonWaitFixture(t, 5*time.Second)

	go func() {
		time.Sleep(50 * time.Millisecond)
		f.association(t, "assoc-late", &addon_v1alpha.AddonAssociation{
			Variant: "small",
			Status:  "active",
		})
	}()

	sender := &recordingSender{}
	require.NoError(t, f.await(expectPostgres, sender))

	assert.Equal(t, []string{
		"Provisioning addon miren-postgresql (small)...",
		"Addon miren-postgresql ready",
	}, sender.Messages)
}

// An addon that was already active is not news; a redeploy of an app whose
// addons are all up must return at once and say nothing.
func TestAwaitAddonsIsSilentWhenAlreadyActive(t *testing.T) {
	f := newAddonWaitFixture(t, time.Second)
	f.association(t, "assoc-active", &addon_v1alpha.AddonAssociation{Status: "active"})

	sender := &recordingSender{}
	require.NoError(t, f.await(expectPostgres, sender))
	assert.Empty(t, sender.Messages)
}

// Error is terminal in the addon controller and a redeploy cannot heal it, so
// the deploy must fail here, carrying the controller's reason and the fix,
// rather than 90 seconds later in the CLI's health wait with "0 of 0 ready".
func TestAwaitAddonsFailsOnErrorWithMessageAndGuidance(t *testing.T) {
	f := newAddonWaitFixture(t, time.Second)
	f.association(t, "assoc-error", &addon_v1alpha.AddonAssociation{
		Status:       "error",
		ErrorMessage: "pool never became ready",
	})

	sender := &recordingSender{}
	err := f.await(expectPostgres, sender)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "miren-postgresql")
	assert.Contains(t, err.Error(), "pool never became ready")
	assert.Contains(t, err.Error(), "miren addon destroy miren-postgresql -a demo")
	require.Len(t, sender.Errors, 1, "the reason must reach the deploy stream too")
	assert.Contains(t, sender.Errors[0], "failed to provision")
}

// A declared addon that is being removed was destroyed out from under this
// deploy and will not come back on its own; say so instead of waiting out
// the ceiling.
func TestAwaitAddonsFailsWhenExpectedAddonIsDeprovisioning(t *testing.T) {
	f := newAddonWaitFixture(t, time.Second)
	f.association(t, "assoc-leaving", &addon_v1alpha.AddonAssociation{Status: "deprovisioning"})

	err := f.await(expectPostgres, &recordingSender{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "being removed")
}

func TestAwaitAddonsFailsAtCeiling(t *testing.T) {
	f := newAddonWaitFixture(t, 30*time.Millisecond)
	f.association(t, "assoc-stuck", &addon_v1alpha.AddonAssociation{Status: "provisioning"})

	err := f.await(expectPostgres, &recordingSender{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not become ready")
	assert.Contains(t, err.Error(), "miren-postgresql")
}

// Only the declared addons matter. One the deploy did not ask for, whatever
// its state, must neither hold the deploy nor fail it.
func TestAwaitAddonsIgnoresUndeclaredAssociations(t *testing.T) {
	f := newAddonWaitFixture(t, time.Second)
	f.association(t, "assoc-other", &addon_v1alpha.AddonAssociation{
		Addon:  entity.Id("addon/miren-valkey"),
		Status: "error",
	})
	f.association(t, "assoc-pg", &addon_v1alpha.AddonAssociation{Status: "active"})

	sender := &recordingSender{}
	require.NoError(t, f.await(expectPostgres, sender))
	assert.Empty(t, sender.Messages)
}

// With nothing declared there is nothing to wait for, and the store must not
// even be consulted: this is every deploy of an app without addons.
func TestAwaitAddonsReturnsImmediatelyWhenNothingExpected(t *testing.T) {
	f := newAddonWaitFixture(t, time.Second)
	f.b.EAS = nil // any touch of the store would panic

	sender := &recordingSender{}
	require.NoError(t, f.await(nil, sender))
	assert.Empty(t, sender.Messages)
}

// A cancelled deploy must stop waiting rather than run out the ceiling.
func TestAwaitAddonsHonorsCancellation(t *testing.T) {
	f := newAddonWaitFixture(t, time.Minute)
	f.association(t, "assoc-slow", &addon_v1alpha.AddonAssociation{Status: "pending"})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := f.b.awaitAddons(ctx, "demo", f.appID, expectPostgres, &recordingSender{})
	require.ErrorIs(t, err, context.Canceled)
}

// The expected set comes from app.toml. The key is the addon name, a
// ":variant" suffix on the key is honored the way CreateInstance honors it,
// and an explicit variant field wins over the suffix.
func TestExpectedAddonsFromConfig(t *testing.T) {
	assert.Nil(t, expectedAddons(nil))
	assert.Empty(t, expectedAddons(&appconfig.AppConfig{}))

	got := expectedAddons(&appconfig.AppConfig{Addons: map[string]*appconfig.AddonConfig{
		"miren-valkey":           nil,
		"miren-postgresql":       {Variant: "shared"},
		"miren-mysql:small":      {},
		"miren-rabbitmq:default": {Variant: "small"},
	}})
	assert.Equal(t, []expectedAddon{
		{Name: "miren-mysql", Variant: "small"},
		{Name: "miren-postgresql", Variant: "shared"},
		{Name: "miren-rabbitmq", Variant: "small"},
		{Name: "miren-valkey", Variant: ""},
	}, got)
}
