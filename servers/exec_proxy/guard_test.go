package execproxy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// The exec proxy is the load-bearing app-scoping guard for exec: it holds the
// caller's identity, whereas the downstream node exec server is reached over the
// coordinator cert. resolveSandboxApp is the part that determines which app a
// target belongs to; a wrong answer would either leak exec across apps or deny a
// legitimate one. The rpc.AllowApp call it feeds is the same audited pattern
// tested across the config-write handlers.
func TestResolveSandboxApp(t *testing.T) {
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	logger := testutils.TestLogger(t)
	server := &Server{Log: logger, EAC: inmem.EAC}

	mockCtrl := testutils.NewMockSandboxController(logger, inmem.EAC)
	require.NoError(t, mockCtrl.Start(ctx))
	defer mockCtrl.Stop()

	app := &core_v1alpha.App{ID: "app/victim"}
	ver := &core_v1alpha.AppVersion{
		ID:       "version/victim-v1",
		App:      app.ID,
		Version:  "v1",
		ImageUrl: "img:latest",
	}
	_, err := inmem.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{Name: "victim"}).Encode,
		entity.DBId, app.ID,
		app.Encode,
	).Attrs())
	require.NoError(t, err)

	// The resolution chain sandbox → version → app reads the AppVersion, so it
	// must be in the store too.
	_, err = inmem.EAC.Create(ctx, entity.New(
		entity.DBId, ver.ID,
		ver.Encode,
	).Attrs())
	require.NoError(t, err)

	sbEnt, cleanupFn, err := server.createEphemeralSandbox(ctx, app, ver, &core_v1alpha.ConfigSpec{StartDirectory: "/app"})
	require.NoError(t, err)
	defer cleanupFn()

	t.Run("resolves the owning app", func(t *testing.T) {
		assert.Equal(t, "victim", server.resolveSandboxApp(ctx, sbEnt))
	})

	// The resolved app drives rpc.AllowApp: a workload bound to another app is
	// refused, its own app is allowed, and an unscoped caller (cert/operator)
	// is unaffected.
	t.Run("guard denies another app", func(t *testing.T) {
		app := server.resolveSandboxApp(ctx, sbEnt)
		other := rpc.ContextWithIdentity(ctx, &rpc.Identity{
			Method:   rpc.AuthMethodWorkload,
			Metadata: map[string]any{"app": "other-app"},
		})
		assert.False(t, rpc.AllowApp(other, app))
	})

	t.Run("guard allows own app", func(t *testing.T) {
		app := server.resolveSandboxApp(ctx, sbEnt)
		own := rpc.ContextWithIdentity(ctx, &rpc.Identity{
			Method:   rpc.AuthMethodWorkload,
			Metadata: map[string]any{"app": "victim"},
		})
		assert.True(t, rpc.AllowApp(own, app))
	})

	t.Run("unresolvable target yields empty app (fail closed)", func(t *testing.T) {
		// A sandbox with no version can't be tied to an app → "". rpc.AllowApp
		// then refuses any app-scoped caller.
		ghost := entity.New(entity.DBId, entity.Id("sandbox/ghost"))
		app := server.resolveSandboxApp(ctx, ghost)
		assert.Empty(t, app)

		scoped := rpc.ContextWithIdentity(ctx, &rpc.Identity{
			Method:   rpc.AuthMethodWorkload,
			Metadata: map[string]any{"app": "victim"},
		})
		assert.False(t, rpc.AllowApp(scoped, app))
	})
}
