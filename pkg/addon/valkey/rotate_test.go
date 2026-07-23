package valkey

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/saga"
)

func TestRegisterRotateDedicatedSaga(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	err := RegisterRotateDedicatedSaga(registry, fw, rc)
	require.NoError(t, err)

	def, ok := registry.Get("rotate-dedicated-valkey")
	require.True(t, ok)
	assert.Equal(t, "rotate-dedicated-valkey", def.Name)
	assert.Len(t, def.Actions, 8)
}

// TestRotateDedicatedSagaOrder pins the safety-critical ordering: the disk is
// single-attach, so the old pool must scale down before the new one is created,
// and the old pool must not be deleted until the new one is confirmed ready.
// Both are enforced by saga edges, not data dependencies.
func TestRotateDedicatedSagaOrder(t *testing.T) {
	registry := saga.NewRegistry()
	fw := &addon.ProviderFramework{}
	rc := &rotateCapture{}

	err := RegisterRotateDedicatedSaga(registry, fw, rc)
	require.NoError(t, err)

	def, ok := registry.Get("rotate-dedicated-valkey")
	require.True(t, ok)

	order := def.ExecutionOrder()
	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		t.Fatalf("action %q not found in order %v", name, order)
		return -1
	}

	assert.Less(t, indexOf("decode-valkey-server-ref"), indexOf("load-valkey-rotation-state"),
		"must decode the server ref before loading it")
	assert.Less(t, indexOf("load-valkey-rotation-state"), indexOf("scale-down-old-valkey-pool"),
		"must load the pool ref before scaling it down")
	assert.Less(t, indexOf("scale-down-old-valkey-pool"), indexOf("swap-valkey-pool"),
		"must free the single-attach disk before the new pool attaches it")
	assert.Less(t, indexOf("swap-valkey-pool"), indexOf("wait-valkey-pool-ready"),
		"must create the new pool before waiting on it")
	assert.Less(t, indexOf("wait-valkey-pool-ready"), indexOf("delete-old-valkey-pool"),
		"must confirm the new pool is ready before deleting the old one")
}

func newRotateTestFramework(t *testing.T) (context.Context, *addon.ProviderFramework) {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	fw := &addon.ProviderFramework{
		EC:  entityserver.NewClient(slog.Default(), inmem.EAC),
		EAC: inmem.EAC,
		Log: slog.Default(),
	}
	return context.Background(), fw
}

func serverPoolRef(t *testing.T, ctx context.Context, fw *addon.ProviderFramework, serverID entity.Id) entity.Id {
	t.Helper()
	var server addon_v1alpha.ValkeyServer
	require.NoError(t, fw.EC.GetById(ctx, serverID, &server))
	return server.SandboxPool
}

func poolCommand(t *testing.T, ctx context.Context, fw *addon.ProviderFramework, poolID entity.Id) string {
	t.Helper()
	var pool compute_v1alpha.SandboxPool
	require.NoError(t, fw.EC.GetById(ctx, poolID, &pool))
	require.NotEmpty(t, pool.SandboxSpec.Container)
	return pool.SandboxSpec.Container[0].Command
}

// TestSwapValkeyPoolCreatesThenAdopts is the pool-leak regression: the first
// swap stands up the new pool with a deterministic id, and a re-run (as would
// happen when a crash lands between the create and the repoint) adopts that same
// pool instead of leaking it and creating a second one.
func TestSwapValkeyPoolCreatesThenAdopts(t *testing.T) {
	ctx, fw := newRotateTestFramework(t)

	oldPoolID, err := fw.CreateSandboxPool(ctx, addon.CreateSandboxPoolSpec{
		DesiredInstances: 1,
		Image:            "valkey",
		Command:          valkeyCommand("oldpw"),
	})
	require.NoError(t, err)

	serverID, err := fw.EC.Create(ctx, "vk", &addon_v1alpha.ValkeyServer{
		Password:    "oldpw",
		SandboxPool: oldPoolID,
	})
	require.NoError(t, err)

	in := SwapValkeyPoolIn{
		RotateServerID:    serverID,
		RotateOldPoolID:   oldPoolID,
		RotateNewPassword: "newpw",
	}

	// First swap: creates the new pool at its deterministic id and repoints.
	out1, err := swapValkeyPool(ctx, fw, in)
	require.NoError(t, err)
	newPoolID := out1.RotateNewPoolID
	require.NotEmpty(t, newPoolID)
	assert.NotEqual(t, oldPoolID, newPoolID)
	assert.Equal(t, entity.Id("pool/"+rotationPoolName(serverID, oldPoolID)), newPoolID)
	assert.Equal(t, newPoolID, serverPoolRef(t, ctx, fw, serverID), "server should point at the new pool")
	assert.Equal(t, valkeyCommand("newpw"), poolCommand(t, ctx, fw, newPoolID))

	// Simulate a crash between create and repoint: the repoint is undone, so the
	// server points back at the old pool while the new pool lingers.
	require.NoError(t, fw.EC.Patch(ctx, serverID, 0,
		entity.Ref(addon_v1alpha.ValkeyServerSandboxPoolId, oldPoolID)))

	// Second swap: must adopt the lingering pool. If adoption were broken it would
	// try to create at the same deterministic id and fail with a conflict, so a
	// clean success returning the same id proves adoption (no duplicate leaked).
	out2, err := swapValkeyPool(ctx, fw, in)
	require.NoError(t, err)
	assert.Equal(t, newPoolID, out2.RotateNewPoolID, "should adopt the existing pool, not leak a duplicate")
	assert.Equal(t, newPoolID, serverPoolRef(t, ctx, fw, serverID), "server should be repointed at the adopted pool")
}

// TestSwapValkeyPoolReplacesStalePool covers the orphan left by an earlier
// rotation that crashed and was abandoned: a pool sits at the deterministic slot
// but launches with a different (older) password. A fresh rotation toward a new
// secret must not adopt it; it tears the stale pool down and rebuilds the slot.
func TestSwapValkeyPoolReplacesStalePool(t *testing.T) {
	ctx, fw := newRotateTestFramework(t)

	oldPoolID, err := fw.CreateSandboxPool(ctx, addon.CreateSandboxPoolSpec{
		DesiredInstances: 1,
		Image:            "valkey",
		Command:          valkeyCommand("oldpw"),
	})
	require.NoError(t, err)

	serverID, err := fw.EC.Create(ctx, "vk", &addon_v1alpha.ValkeyServer{
		Password:    "oldpw",
		SandboxPool: oldPoolID,
	})
	require.NoError(t, err)

	// A prior rotation toward "stalepw" stood up the slot pool and then was
	// abandoned, leaving it behind.
	_, err = swapValkeyPool(ctx, fw, SwapValkeyPoolIn{
		RotateServerID:    serverID,
		RotateOldPoolID:   oldPoolID,
		RotateNewPassword: "stalepw",
	})
	require.NoError(t, err)
	require.NoError(t, fw.EC.Patch(ctx, serverID, 0,
		entity.Ref(addon_v1alpha.ValkeyServerSandboxPoolId, oldPoolID)))

	// A fresh rotation toward "freshpw" finds the stale pool at the same slot
	// (same server + old pool), must reject it on the command mismatch, and
	// rebuild the slot for the new secret.
	out, err := swapValkeyPool(ctx, fw, SwapValkeyPoolIn{
		RotateServerID:    serverID,
		RotateOldPoolID:   oldPoolID,
		RotateNewPassword: "freshpw",
	})
	require.NoError(t, err)

	slotID := entity.Id("pool/" + rotationPoolName(serverID, oldPoolID))
	assert.Equal(t, slotID, out.RotateNewPoolID, "should reuse the deterministic slot")
	assert.Equal(t, valkeyCommand("freshpw"), poolCommand(t, ctx, fw, out.RotateNewPoolID),
		"slot pool should launch with the fresh password, not the stale one")
	assert.Equal(t, out.RotateNewPoolID, serverPoolRef(t, ctx, fw, serverID))
}

// TestCreateRotationPoolAdoptsOnConflict covers the create losing a race: if a
// concurrent attempt already created the pool at the deterministic id, a
// conflict is adopted when the launch command matches and refused when it does
// not.
func TestCreateRotationPoolAdoptsOnConflict(t *testing.T) {
	ctx, fw := newRotateTestFramework(t)

	basePoolID, err := fw.CreateSandboxPool(ctx, addon.CreateSandboxPoolSpec{
		DesiredInstances: 1,
		Image:            "valkey",
		Command:          valkeyCommand("oldpw"),
	})
	require.NoError(t, err)
	var basePool compute_v1alpha.SandboxPool
	require.NoError(t, fw.EC.GetById(ctx, basePoolID, &basePool))

	name := "valkey-rot-conflict"
	poolID := entity.Id("pool/" + name)

	// A concurrent attempt already stood the pool up at the deterministic id,
	// targeting the same secret.
	_, err = fw.CreateSandboxPool(ctx, addon.CreateSandboxPoolSpec{
		Name:             name,
		DesiredInstances: 1,
		Image:            "valkey",
		Command:          valkeyCommand("newpw"),
	})
	require.NoError(t, err)

	// Matching command: the conflict is adopted.
	require.NoError(t, createRotationPool(ctx, fw, &basePool, poolID, name, valkeyCommand("newpw")))

	// Different command at the same id: refuse to adopt.
	err = createRotationPool(ctx, fw, &basePool, poolID, name, valkeyCommand("different"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different launch command")
}
