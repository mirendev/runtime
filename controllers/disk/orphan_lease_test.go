package disk

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// TestReconcileOrphanLeasesReleasesDeadSandboxLease verifies that a BOUND
// lease whose owning sandbox has status DEAD gets transitioned to RELEASED
// at boot. This catches leases stranded after a SIGKILL that bypassed the
// normal StopSandbox → ReleaseDiskLeases path.
func TestReconcileOrphanLeasesReleasesDeadSandboxLease(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	// Create a dead sandbox.
	deadSandboxId := entity.Id("sandbox/dead-postgres")
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, deadSandboxId,
		(&compute.Sandbox{
			ID:     deadSandboxId,
			Status: compute.DEAD,
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	// Create a BOUND lease owned by it.
	leaseId := entity.Id("disk-lease/orphan-bound")
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, leaseId,
		(&storage_v1alpha.DiskLease{
			ID:         leaseId,
			DiskId:     entity.Id("disk/pg-shared"),
			SandboxId:  deadSandboxId,
			Status:     storage_v1alpha.BOUND,
			AcquiredAt: time.Now(),
			NodeId:     compute.NewNodeId("test-node").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	// A second lease owned by a LIVE sandbox must NOT be released.
	liveSandboxId := entity.Id("sandbox/live-web")
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, liveSandboxId,
		(&compute.Sandbox{
			ID:     liveSandboxId,
			Status: compute.RUNNING,
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	liveLeaseId := entity.Id("disk-lease/live-bound")
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, liveLeaseId,
		(&storage_v1alpha.DiskLease{
			ID:         liveLeaseId,
			DiskId:     entity.Id("disk/web-data"),
			SandboxId:  liveSandboxId,
			Status:     storage_v1alpha.BOUND,
			AcquiredAt: time.Now(),
			NodeId:     compute.NewNodeId("test-node").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	leaseController := NewDiskLeaseController(log, es.EAC, compute.NewNodeId("test-node"), "")
	require.NoError(t, leaseController.Init(ctx))

	// Orphan lease should now be RELEASED.
	resp, err := es.EAC.Get(ctx, leaseId.String())
	require.NoError(t, err)
	var orphan storage_v1alpha.DiskLease
	orphan.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.RELEASED, orphan.Status,
		"lease for a DEAD sandbox must be released by the orphan sweep")

	// Live lease should be untouched.
	resp, err = es.EAC.Get(ctx, liveLeaseId.String())
	require.NoError(t, err)
	var live storage_v1alpha.DiskLease
	live.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.BOUND, live.Status,
		"lease for a LIVE sandbox must not be touched")
}

// TestReconcileOrphanLeasesReleasesLeaseForMissingSandbox verifies that
// if the sandbox entity has been deleted entirely (not just marked
// DEAD), the dangling lease gets cleaned up.
func TestReconcileOrphanLeasesReleasesLeaseForMissingSandbox(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	leaseId := entity.Id("disk-lease/orphan-missing")
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, leaseId,
		(&storage_v1alpha.DiskLease{
			ID:         leaseId,
			DiskId:     entity.Id("disk/some-disk"),
			SandboxId:  entity.Id("sandbox/does-not-exist"),
			Status:     storage_v1alpha.BOUND,
			AcquiredAt: time.Now(),
			NodeId:     compute.NewNodeId("test-node").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	leaseController := NewDiskLeaseController(log, es.EAC, compute.NewNodeId("test-node"), "")
	require.NoError(t, leaseController.Init(ctx))

	resp, err := es.EAC.Get(ctx, leaseId.String())
	require.NoError(t, err)
	var lease storage_v1alpha.DiskLease
	lease.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.RELEASED, lease.Status,
		"lease for a missing sandbox must be released by the orphan sweep")
}

// TestReconcileOrphanLeasesRecurring verifies the sweep works as a recurring
// entry point, not just at Init. A sandbox can die (SIGKILL, boot failure)
// while the controller is already running; the periodic tick must release the
// stranded lease, and calling it again must be a harmless no-op.
func TestReconcileOrphanLeasesRecurring(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	leaseController := NewDiskLeaseController(log, es.EAC, compute.NewNodeId("test-node"), "")
	require.NoError(t, leaseController.Init(ctx))

	// After the controller is already up, a sandbox dies holding a BOUND lease.
	deadSandboxId := entity.Id("sandbox/late-dead")
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, deadSandboxId,
		(&compute.Sandbox{
			ID:     deadSandboxId,
			Status: compute.DEAD,
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	leaseId := entity.Id("disk-lease/late-orphan")
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, leaseId,
		(&storage_v1alpha.DiskLease{
			ID:         leaseId,
			DiskId:     entity.Id("disk/late-data"),
			SandboxId:  deadSandboxId,
			Status:     storage_v1alpha.BOUND,
			AcquiredAt: time.Now(),
			NodeId:     compute.NewNodeId("test-node").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	// The Init sweep already ran before this lease existed; the recurring tick
	// is what must catch it. Zero grace here to assert the core release logic.
	require.NoError(t, leaseController.ReconcileOrphanLeases(ctx, 0))

	resp, err := es.EAC.Get(ctx, leaseId.String())
	require.NoError(t, err)
	var lease storage_v1alpha.DiskLease
	lease.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.RELEASED, lease.Status,
		"recurring sweep must release a lease whose sandbox died after Init")

	// Running the sweep again is a harmless no-op.
	require.NoError(t, leaseController.ReconcileOrphanLeases(ctx, 0))
	resp, err = es.EAC.Get(ctx, leaseId.String())
	require.NoError(t, err)
	lease.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.RELEASED, lease.Status)
}

// TestReconcileOrphanLeasesGracePeriod verifies the recurring sweep leaves a
// freshly created lease alone even when its sandbox already reads as DEAD, so a
// just-booted sandbox or an in-flight teardown isn't raced by the sweep. A zero
// grace (the boot-time path) still reclaims it immediately.
func TestReconcileOrphanLeasesGracePeriod(t *testing.T) {
	ctx := context.Background()
	log := slog.Default()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	deadSandboxId := entity.Id("sandbox/grace-dead")
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, deadSandboxId,
		(&compute.Sandbox{
			ID:     deadSandboxId,
			Status: compute.DEAD,
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	leaseId := entity.Id("disk-lease/grace-young")
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, leaseId,
		(&storage_v1alpha.DiskLease{
			ID:         leaseId,
			DiskId:     entity.Id("disk/grace-data"),
			SandboxId:  deadSandboxId,
			Status:     storage_v1alpha.BOUND,
			AcquiredAt: time.Now(),
			NodeId:     compute.NewNodeId("test-node").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	leaseController := NewDiskLeaseController(log, es.EAC, compute.NewNodeId("test-node"), "")

	// With a long grace, the just-created lease must be left BOUND even though
	// its sandbox is DEAD.
	require.NoError(t, leaseController.ReconcileOrphanLeases(ctx, time.Hour))
	resp, err := es.EAC.Get(ctx, leaseId.String())
	require.NoError(t, err)
	var lease storage_v1alpha.DiskLease
	lease.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.BOUND, lease.Status,
		"grace period must protect a freshly created lease from the recurring sweep")

	// With zero grace (boot-time recovery), the same lease is reclaimed.
	require.NoError(t, leaseController.ReconcileOrphanLeases(ctx, 0))
	resp, err = es.EAC.Get(ctx, leaseId.String())
	require.NoError(t, err)
	lease.Decode(resp.Entity().Entity())
	assert.Equal(t, storage_v1alpha.RELEASED, lease.Status,
		"zero grace must reclaim the orphaned lease")
}
