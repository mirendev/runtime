package keyrotation

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	esv1 "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/etcdtest"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/secret/cluster"
	"miren.dev/runtime/pkg/secret/keyring"
	entityserversrv "miren.dev/runtime/servers/entityserver"
)

// newTestController stands the controller up over a real etcd-backed store,
// since rotation is defined by indexed queries over stored versions.
func newTestController(t *testing.T) (*Controller, *cluster.Backend, *entityserver.Client, string) {
	t.Helper()

	log := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := entity.NewEtcdStore(ctx, log, client, prefix)
	require.NoError(t, err)
	require.NoError(t, schema.Apply(ctx, store))

	eac := esv1.NewEntityAccessClient(rpc.LocalClient(
		esv1.AdaptEntityAccess(&entityserversrv.EntityServer{Log: log, Store: store}),
	))
	ec := entityserver.NewClient(log, eac)

	dataPath := t.TempDir()
	ring, err := keyring.Ensure(log, dataPath)
	require.NoError(t, err)

	backend := cluster.NewBackend(log, ec, ring)

	return &Controller{
		Log:      log,
		EC:       ec,
		Backend:  backend,
		DataPath: dataPath,
		Config:   Config{CheckInterval: time.Hour, MaxKeyAge: 90 * 24 * time.Hour},
	}, backend, ec, dataPath
}

// drive runs ticks until the rotation finishes, so a test asserts the end state
// rather than the number of passes it took to get there.
func drive(t *testing.T, c *Controller, ctx context.Context) {
	t.Helper()
	for range 50 {
		c.tick(ctx)
		active, err := c.activeRotation(ctx)
		require.NoError(t, err)
		if active == nil {
			return
		}
	}
	t.Fatal("rotation did not finish")
}

func TestRotationRewrapsEveryVersionAndRetiresTheOldKey(t *testing.T) {
	c, backend, _, dataPath := newTestController(t)
	ctx := t.Context()

	before := backend.Keyring().CurrentID()

	values := map[string]string{
		"payments/stripe-key": "sk_live",
		"registry/npm-token":  "npm_tok",
		"auth/session-key":    "sess",
	}
	for path, v := range values {
		_, _, err := backend.Put(ctx, path, []byte(v))
		require.NoError(t, err)
	}

	require.NoError(t, c.Begin(ctx))
	assert.NotEqual(t, before, backend.Keyring().CurrentID(), "writes move to the new key immediately")

	drive(t, c, ctx)

	// Nothing references the old key, and it is gone from the ring.
	remaining, err := backend.CountOnKey(ctx, before)
	require.NoError(t, err)
	assert.Zero(t, remaining)

	for _, k := range backend.Keyring().Keys() {
		assert.NotEqual(t, before, k.ID, "the retired key should be out of the ring")
	}

	// Every value still reads, which is the whole point.
	for path, want := range values {
		got, err := backend.Resolve(ctx, path)
		require.NoError(t, err, path)
		assert.Equal(t, want, string(got.Bytes), path)
	}

	// And the retired ring is what a restart would load.
	reloaded, err := keyring.Ensure(slog.New(slog.DiscardHandler), dataPath)
	require.NoError(t, err)
	assert.Equal(t, backend.Keyring().CurrentID(), reloaded.CurrentID())
	assert.Len(t, reloaded.Keys(), 1)
}

// A key must not be retired while anything still names it: those versions
// would have nothing able to unwrap them, and there is no recovering from it.
func TestRotationWillNotRetireAKeyStillInUse(t *testing.T) {
	c, backend, ec, _ := newTestController(t)
	ctx := t.Context()

	oldKey := backend.Keyring().CurrentID()
	_, _, err := backend.Put(ctx, "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)

	require.NoError(t, c.Begin(ctx))

	// Jump straight to retiring, as a crash between the two steps would.
	active, err := c.activeRotation(ctx)
	require.NoError(t, err)
	require.NoError(t, ec.UpdateAttrs(ctx, active.ID,
		entity.Ref(core_v1alpha.KeyRotationStatusId, core_v1alpha.KeyRotationStatusRetiringId)))

	// The retire step must notice the version still on the old key and go back
	// rather than dropping it.
	c.tick(ctx)

	found := false
	for _, k := range backend.Keyring().Keys() {
		if k.ID == oldKey {
			found = true
		}
	}
	assert.True(t, found, "the old key must survive while a version still names it")

	got, err := backend.Resolve(ctx, "payments/stripe-key")
	require.NoError(t, err)
	assert.Equal(t, "sk_live", string(got.Bytes))

	// Driving it the rest of the way converges normally.
	drive(t, c, ctx)
	remaining, err := backend.CountOnKey(ctx, oldKey)
	require.NoError(t, err)
	assert.Zero(t, remaining)
}

// The backfill's state is the query, not a cursor, so interrupting it and
// starting over converges rather than losing or double-counting work.
func TestRotationResumesAfterInterruption(t *testing.T) {
	c, backend, _, _ := newTestController(t)
	ctx := t.Context()

	oldKey := backend.Keyring().CurrentID()
	for i := range 5 {
		_, _, err := backend.Put(ctx, "app/secret-"+string(rune('a'+i)), []byte("value"))
		require.NoError(t, err)
	}

	require.NoError(t, c.Begin(ctx))

	// One partial pass, then pick up with a fresh controller over the same
	// store and ring, as a restart would.
	moved, err := backend.RewrapBatch(ctx, oldKey, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, moved)

	resumed := &Controller{
		Log: c.Log, EC: c.EC, Backend: backend, DataPath: c.DataPath, Config: c.Config,
	}
	drive(t, resumed, ctx)

	remaining, err := backend.CountOnKey(ctx, oldKey)
	require.NoError(t, err)
	assert.Zero(t, remaining)

	for i := range 5 {
		got, err := backend.Resolve(ctx, "app/secret-"+string(rune('a'+i)))
		require.NoError(t, err)
		assert.Equal(t, "value", string(got.Bytes))
	}
}

// A destroyed version keeps its kek_id but has no payload to move. Left alone
// it would hold the count above zero forever and block retirement.
func TestRotationClearsDestroyedVersionsOffTheOldKey(t *testing.T) {
	c, backend, _, _ := newTestController(t)
	ctx := t.Context()

	oldKey := backend.Keyring().CurrentID()

	version, _, err := backend.Put(ctx, "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)
	require.NoError(t, backend.SetState(ctx,
		secret.FormatRef("payments/stripe-key", version), secret.StateDestroyed))

	require.NoError(t, c.Begin(ctx))
	drive(t, c, ctx)

	remaining, err := backend.CountOnKey(ctx, oldKey)
	require.NoError(t, err)
	assert.Zero(t, remaining, "a destroyed version must not pin a key forever")
}

func TestRotationRefusesToStackConcurrentRotations(t *testing.T) {
	c, _, _, _ := newTestController(t)
	ctx := t.Context()

	require.NoError(t, c.Begin(ctx))
	err := c.Begin(ctx)
	assert.Error(t, err, "a second rotation would try to retire a key the first still needs")
}

func TestAutomaticRotationTriggersOnKeyAge(t *testing.T) {
	c, _, _, _ := newTestController(t)
	ctx := t.Context()

	// A fresh key is nowhere near the policy.
	due, err := c.rotationDue()
	require.NoError(t, err)
	assert.False(t, due)

	c.tick(ctx)
	active, err := c.activeRotation(ctx)
	require.NoError(t, err)
	assert.Nil(t, active, "a young key must not rotate")

	// Past the policy, the next tick starts one on its own.
	c.Config.MaxKeyAge = time.Nanosecond
	due, err = c.rotationDue()
	require.NoError(t, err)
	assert.True(t, due)

	c.tick(ctx)
	active, err = c.activeRotation(ctx)
	require.NoError(t, err)
	require.NotNil(t, active, "an aged-out key should rotate without an operator")
}

func TestAutomaticRotationCanBeDisabled(t *testing.T) {
	c, _, _, _ := newTestController(t)

	c.Config.MaxKeyAge = 0
	due, err := c.rotationDue()
	require.NoError(t, err)
	assert.False(t, due, "zero max age leaves rotation to the operator")
}

// A key with no recorded time predates key timestamps. Treating it as new would
// leave it unrotated forever, so it reads as old enough to rotate.
func TestKeyWithNoRecordedTimeIsTreatedAsDue(t *testing.T) {
	k := keyring.Key{ID: "legacy"}
	assert.Greater(t, k.Age(time.Now()), 100*365*24*time.Hour)
}

// A rotation interrupted by a restart must resume on its own. Waiting for the
// first tick would stall a half-finished rotation for the whole interval, which
// on the default hourly tick means an old key lingering long after an operator
// believes it is gone.
func TestRotationResumesShortlyAfterStartup(t *testing.T) {
	c, backend, _, _ := newTestController(t)
	ctx := t.Context()

	oldKey := backend.Keyring().CurrentID()
	_, _, err := backend.Put(ctx, "payments/stripe-key", []byte("sk_live"))
	require.NoError(t, err)

	// Begin without a running loop, so the rotation is left in flight exactly
	// as a restart mid-rotation would leave it.
	require.NoError(t, c.Begin(ctx))
	active, err := c.activeRotation(ctx)
	require.NoError(t, err)
	require.NotNil(t, active, "precondition: a rotation is in flight")

	// A fresh controller over the same store and ring, as the next boot builds.
	resumed := &Controller{
		Log: c.Log, EC: c.EC, Backend: backend, DataPath: c.DataPath,
		Config: Config{CheckInterval: time.Hour, MaxKeyAge: 90 * 24 * time.Hour},
	}
	resumed.Start(ctx)
	t.Cleanup(resumed.Stop)

	require.Eventually(t, func() bool {
		remaining, err := backend.CountOnKey(ctx, oldKey)
		return err == nil && remaining == 0
	}, startupDelay+10*time.Second, 50*time.Millisecond,
		"an in-flight rotation should resume without waiting for a tick")

	got, err := backend.Resolve(ctx, "payments/stripe-key")
	require.NoError(t, err)
	assert.Equal(t, "sk_live", string(got.Bytes))
}

// An operator rotating during an incident should not have to wait out a tick
// interval to see the backfill even begin.
func TestBeginDrivesTheBackfillWithoutWaitingForATick(t *testing.T) {
	c, backend, _, _ := newTestController(t)
	ctx := t.Context()

	// A tick interval long enough that a timer-driven pass cannot be what
	// finishes this.
	c.Config.CheckInterval = time.Hour
	oldKey := backend.Keyring().CurrentID()

	for i := range 3 {
		_, _, err := backend.Put(ctx, "app/secret-"+string(rune('a'+i)), []byte("value"))
		require.NoError(t, err)
	}

	c.Start(ctx)
	t.Cleanup(c.Stop)

	require.NoError(t, c.Begin(ctx))

	require.Eventually(t, func() bool {
		remaining, err := backend.CountOnKey(ctx, oldKey)
		return err == nil && remaining == 0
	}, 10*time.Second, 20*time.Millisecond, "the backfill should run off the nudge, not the tick")

	require.Eventually(t, func() bool {
		for _, k := range backend.Keyring().Keys() {
			if k.ID == oldKey {
				return false
			}
		}
		return true
	}, 10*time.Second, 20*time.Millisecond, "the old key should retire once nothing references it")

	for i := range 3 {
		got, err := backend.Resolve(ctx, "app/secret-"+string(rune('a'+i)))
		require.NoError(t, err)
		assert.Equal(t, "value", string(got.Bytes))
	}
}
