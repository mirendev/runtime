package disk

import (
	"context"
	"log/slog"
	"testing"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestDiskLeaseController_New(t *testing.T) {
	log := slog.Default()
	controller := NewDiskLeaseController(log, nil, compute.NewNodeId("test-node"), "")

	assert.NotNil(t, controller)
	assert.NotNil(t, controller.Log)
	assert.Equal(t, "/var/lib/miren/disks", controller.mountBasePath)
	assert.Equal(t, compute.NewNodeId("test-node"), controller.NodeId)
}

func TestDiskLeaseController_LeaseConflict(t *testing.T) {
	log := slog.Default()
	dlc := NewDiskLeaseController(log, nil, compute.NewNodeId("test-node"), "")

	// Simulate existing lease for the disk
	dlc.activeLeases["disk/test-disk"] = "disk-lease/existing-lease"

	conflictingLease := &storage_v1alpha.DiskLease{
		ID:        entity.Id("disk-lease/conflicting-lease"),
		DiskId:    entity.Id("disk/test-disk"),
		SandboxId: entity.Id("sandbox/another-sandbox"),
		Status:    storage_v1alpha.PENDING,
		Mount: storage_v1alpha.Mount{
			Path: "/data",
		},
	}

	// Process the conflicting lease
	meta := &entity.Meta{}
	err := dlc.Create(context.Background(), conflictingLease, meta)
	require.NoError(t, err)

	// Should stay PENDING for retry (not FAILED), since the existing lease
	// may be in the process of being released
	assert.Equal(t, storage_v1alpha.PENDING, conflictingLease.Status, "Conflicting lease should stay PENDING for retry")
}

func TestDiskLeaseController_Delete(t *testing.T) {
	log := slog.Default()
	dlc := NewDiskLeaseController(log, nil, compute.NewNodeId("test-node"), "")

	// Setup active lease and lease details
	dlc.activeLeases["disk/test-disk"] = "disk-lease/test-lease"
	dlc.leaseDetails["disk-lease/test-lease"] = &leaseInfo{
		leaseId:   "disk-lease/test-lease",
		diskId:    "disk/test-disk",
		sandboxId: "sandbox/test-sandbox",
	}

	// Process the deletion
	err := dlc.Delete(context.Background(), entity.Id("disk-lease/test-lease"), nil)
	require.NoError(t, err)

	// Should remove from active leases
	_, exists := dlc.activeLeases["disk/test-disk"]
	assert.False(t, exists, "Should remove lease from active leases")

	// Should also remove from lease details
	_, detailsExist := dlc.leaseDetails["disk-lease/test-lease"]
	assert.False(t, detailsExist, "Should remove lease from lease details")
}

func TestDiskLeaseController_Release(t *testing.T) {
	log := slog.Default()
	dlc := NewDiskLeaseController(log, nil, compute.NewNodeId("test-node"), "")

	// Setup active lease
	dlc.activeLeases["disk/test-disk"] = "disk-lease/test-lease"
	dlc.leaseDetails["disk-lease/test-lease"] = &leaseInfo{
		leaseId:   "disk-lease/test-lease",
		diskId:    "disk/test-disk",
		sandboxId: "sandbox/test-sandbox",
	}

	releasedLease := &storage_v1alpha.DiskLease{
		ID:        entity.Id("disk-lease/test-lease"),
		DiskId:    entity.Id("disk/test-disk"),
		SandboxId: entity.Id("sandbox/test-sandbox"),
		Status:    storage_v1alpha.RELEASED,
	}

	// Process the release
	meta := &entity.Meta{}
	err := dlc.Update(context.Background(), releasedLease, meta)
	require.NoError(t, err)

	// Should remove from active leases
	_, exists := dlc.activeLeases["disk/test-disk"]
	assert.False(t, exists, "Should remove released lease from active leases")

	// Should remove from lease details
	_, detailsExist := dlc.leaseDetails["disk-lease/test-lease"]
	assert.False(t, detailsExist, "Should remove from lease details")
}

func TestDiskLeaseController_ReleaseIdempotent(t *testing.T) {
	log := slog.Default()
	dlc := NewDiskLeaseController(log, nil, compute.NewNodeId("test-node"), "")

	// Setup: No active lease (already released)
	releasedLease := &storage_v1alpha.DiskLease{
		ID:        entity.Id("disk-lease/already-released"),
		DiskId:    entity.Id("disk/already-released"),
		SandboxId: entity.Id("sandbox/test-sandbox"),
		Status:    storage_v1alpha.RELEASED,
	}

	meta := &entity.Meta{}

	// First call - lease already not active
	err := dlc.Update(context.Background(), releasedLease, meta)
	require.NoError(t, err)

	// Second call - should still work without errors
	err = dlc.Update(context.Background(), releasedLease, meta)
	require.NoError(t, err)

	// Verify no lease is tracked
	_, exists := dlc.activeLeases["disk/already-released"]
	assert.False(t, exists, "No lease should be tracked")
}

func TestDiskLeaseController_CleanupOldReleasedLeases(t *testing.T) {
	log := slog.Default()
	dlc := NewDiskLeaseController(log, nil, compute.NewNodeId("test-node"), "")

	// Since we don't have a real EAC, we test the logic in isolation
	// The controller should skip cleanup when EAC is nil (test mode)
	ctx := context.Background()
	err := dlc.CleanupOldReleasedLeases(ctx)

	// Should not error even with no EAC
	assert.NoError(t, err)
}

// newOwnershipFixture creates a PROVISIONED disk and its disk_volume, stamped
// with volumeNode (pass "" for a legacy volume that predates the node stamp).
// The lease ownership tests all need this same pair and differ only in who
// owns what, so the shape lives here rather than in each of them.
func newOwnershipFixture(t *testing.T, ctx context.Context, name string, volumeNode entity.Id) (*testutils.InMemEntityServer, entity.Id) {
	t.Helper()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	diskId := entity.Id("disk/" + name)
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, diskId,
		(&storage_v1alpha.Disk{
			ID:       diskId,
			Name:     name,
			Status:   storage_v1alpha.PROVISIONED,
			VolumeId: "vol-" + name,
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	volumeId := entity.Id("disk_volume/vol-" + name)
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, volumeId,
		(&storage_v1alpha.DiskVolume{
			ID:          volumeId,
			DiskId:      diskId,
			VolumeId:    "vol-" + name,
			ActualState: storage_v1alpha.DV_READY,
			NodeId:      volumeNode,
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	return es, diskId
}

// TestDiskLeaseController_SkipsVolumeOwnedByAnotherNode covers a lease with no
// NodeId, which every node reconciles. Only the node holding the volume may
// mount it: a non-owner that creates a disk_mount produces one that can never
// mount, and the lease then binds or fails depending on which mount the index
// happens to return first (MIR-1469).
func TestDiskLeaseController_SkipsVolumeOwnedByAnotherNode(t *testing.T) {
	ctx := context.Background()

	es, diskId := newOwnershipFixture(t, ctx, "shared", compute.NewNodeId("coordinator").Id())

	dlc := NewDiskLeaseController(slog.Default(), es.EAC, compute.NewNodeId("runner1"), "")

	lease := &storage_v1alpha.DiskLease{
		ID:     entity.Id("disk_lease/unpinned"),
		DiskId: diskId,
		Status: storage_v1alpha.PENDING,
		Mount:  storage_v1alpha.Mount{Path: "/data"},
	}

	require.NoError(t, dlc.handlePendingLease(ctx, lease))

	assert.Equal(t, storage_v1alpha.PENDING, lease.Status,
		"non-owner must leave the lease alone, not fail it")
	assert.Empty(t, lease.ErrorMessage)

	mounts, err := es.EAC.List(ctx, entity.Ref(storage_v1alpha.DiskMountDiskLeaseIdId, lease.ID))
	require.NoError(t, err)
	assert.Empty(t, mounts.Values(), "non-owner must not create a disk_mount")
}

// TestDiskLeaseController_PicksOwnDiskMount covers the lookup side: when a
// lease already carries mounts from more than one node, the controller must
// read its own rather than whichever the index returns first.
func TestDiskLeaseController_PicksOwnDiskMount(t *testing.T) {
	ctx := context.Background()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	leaseId := entity.Id("disk_lease/two-mounts")

	ownId := entity.Id("disk_mount/from-runner1")
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, ownId,
		(&storage_v1alpha.DiskMount{
			ID:          ownId,
			DiskLeaseId: leaseId,
			VolumeId:    entity.Id("disk_volume/vol-shared"),
			ActualState: storage_v1alpha.DM_MOUNTED,
			NodeId:      compute.NewNodeId("runner1").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	foreignId := entity.Id("disk_mount/from-coordinator")
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, foreignId,
		(&storage_v1alpha.DiskMount{
			ID:          foreignId,
			DiskLeaseId: leaseId,
			VolumeId:    entity.Id("disk_volume/vol-shared"),
			ActualState: storage_v1alpha.DM_ERROR,
			NodeId:      compute.NewNodeId("coordinator").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	dlc := NewDiskLeaseController(slog.Default(), es.EAC, compute.NewNodeId("runner1"), "")

	mount, _, err := dlc.getDiskMountForLease(ctx, leaseId)
	require.NoError(t, err)
	require.NotNil(t, mount)
	assert.Equal(t, ownId, mount.ID)
	assert.Equal(t, storage_v1alpha.DM_MOUNTED, mount.ActualState)

	// The coordinator, holding no mount of its own for this lease, must not
	// adopt runner1's. Index order decides nothing here — a node either has a
	// mount or it doesn't.
	coordinator := NewDiskLeaseController(slog.Default(), es.EAC, compute.NewNodeId("coordinator"), "")

	own, foreign, err := coordinator.getDiskMountForLease(ctx, entity.Id("disk_lease/runner-only"))
	require.NoError(t, err)
	assert.Nil(t, own)
	assert.False(t, foreign, "no mounts at all is not a foreign mount")

	runnerOnlyId := entity.Id("disk_mount/runner-only-mount")
	_, err = es.EAC.Create(ctx, entity.New(
		entity.DBId, runnerOnlyId,
		(&storage_v1alpha.DiskMount{
			ID:          runnerOnlyId,
			DiskLeaseId: entity.Id("disk_lease/runner-only"),
			VolumeId:    entity.Id("disk_volume/vol-shared"),
			ActualState: storage_v1alpha.DM_ERROR,
			NodeId:      compute.NewNodeId("runner1").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	own, foreign, err = coordinator.getDiskMountForLease(ctx, entity.Id("disk_lease/runner-only"))
	require.NoError(t, err)
	assert.Nil(t, own, "another node's mount is not ours to act on")
	assert.True(t, foreign, "must report that some other node holds the mount")
}

// TestDiskLeaseController_NonOwnerIgnoresItsOwnStaleErrorMount covers the state
// this change has to repair, not just prevent: an unpinned lease that already
// carries a failed mount from a non-owner. Ownership has to be settled before
// the existing-mount state machine runs, or the non-owner reads its own
// DM_ERROR mount and drives the shared lease to a terminal FAILED.
func TestDiskLeaseController_NonOwnerIgnoresItsOwnStaleErrorMount(t *testing.T) {
	ctx := context.Background()

	es, diskId := newOwnershipFixture(t, ctx, "legacy", compute.NewNodeId("coordinator").Id())
	volumeId := entity.Id("disk_volume/vol-legacy")

	leaseId := entity.Id("disk_lease/legacy-unpinned")

	// Left behind by an earlier reconcile on this node, before the guard existed.
	staleId := entity.Id("disk_mount/stale-runner1")
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, staleId,
		(&storage_v1alpha.DiskMount{
			ID:           staleId,
			DiskLeaseId:  leaseId,
			VolumeId:     volumeId,
			ActualState:  storage_v1alpha.DM_ERROR,
			ErrorMessage: "volume vol-legacy not found in state",
			NodeId:       compute.NewNodeId("runner1").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	dlc := NewDiskLeaseController(slog.Default(), es.EAC, compute.NewNodeId("runner1"), "")

	lease := &storage_v1alpha.DiskLease{
		ID:     leaseId,
		DiskId: diskId,
		Status: storage_v1alpha.PENDING,
		Mount:  storage_v1alpha.Mount{Path: "/data"},
	}

	require.NoError(t, dlc.handlePendingLease(ctx, lease))

	assert.Equal(t, storage_v1alpha.PENDING, lease.Status,
		"a non-owner must not fail a lease from its own stale mount")
	assert.Empty(t, lease.ErrorMessage)
}

// TestDiskLeaseController_BoundLeaseSurvivesNonOwnerReconcile covers the other
// side of an unpinned lease: every node reconciles it in BOUND too. A node that
// holds no mount of its own must not knock it back to PENDING, or the owner and
// non-owner flap the lease between BOUND and PENDING forever.
func TestDiskLeaseController_BoundLeaseSurvivesNonOwnerReconcile(t *testing.T) {
	ctx := context.Background()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	leaseId := entity.Id("disk_lease/bound-unpinned")

	ownerMountId := entity.Id("disk_mount/owner-mount")
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, ownerMountId,
		(&storage_v1alpha.DiskMount{
			ID:          ownerMountId,
			DiskLeaseId: leaseId,
			VolumeId:    entity.Id("disk_volume/vol-bound"),
			ActualState: storage_v1alpha.DM_MOUNTED,
			NodeId:      compute.NewNodeId("coordinator").Id(),
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	// runner1 holds no mount for this lease.
	dlc := NewDiskLeaseController(slog.Default(), es.EAC, compute.NewNodeId("runner1"), "")

	lease := &storage_v1alpha.DiskLease{
		ID:     leaseId,
		DiskId: entity.Id("disk/bound"),
		Status: storage_v1alpha.BOUND,
		Mount:  storage_v1alpha.Mount{Path: "/data"},
	}

	require.NoError(t, dlc.handleBoundLease(ctx, lease))

	assert.Equal(t, storage_v1alpha.BOUND, lease.Status,
		"a node without its own mount must leave a bound lease bound")
}

// TestDiskLeaseController_PinnedLeaseWithForeignVolumeFails covers the case the
// unpinned guard must not swallow: a lease pinned to this node whose volume
// lives elsewhere. Every other node bails at the NodeId filter, so if this node
// also steps aside the lease sits in PENDING forever with nothing explaining
// why. Fail it, and name the split.
func TestDiskLeaseController_PinnedLeaseWithForeignVolumeFails(t *testing.T) {
	ctx := context.Background()

	es, diskId := newOwnershipFixture(t, ctx, "misplaced", compute.NewNodeId("coordinator").Id())

	dlc := NewDiskLeaseController(slog.Default(), es.EAC, compute.NewNodeId("runner1"), "")

	lease := &storage_v1alpha.DiskLease{
		ID:     entity.Id("disk_lease/pinned-elsewhere"),
		DiskId: diskId,
		Status: storage_v1alpha.PENDING,
		Mount:  storage_v1alpha.Mount{Path: "/data"},
		NodeId: compute.NewNodeId("runner1").Id(),
	}

	require.NoError(t, dlc.handlePendingLease(ctx, lease))

	assert.Equal(t, storage_v1alpha.FAILED, lease.Status,
		"a lease nobody else will pick up must fail loudly, not stall")
	assert.Contains(t, lease.ErrorMessage, "node/coordinator")
}

// TestDiskLeaseController_MissingVolumeIsRetryable covers the window where a
// PROVISIONED disk has no disk_volume yet: DiskController recreates it on its
// next pass, so the lease must wait rather than burn itself on a state that
// resolves on its own.
func TestDiskLeaseController_MissingVolumeIsRetryable(t *testing.T) {
	ctx := context.Background()

	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	diskId := entity.Id("disk/no-volume-yet")
	_, err := es.EAC.Create(ctx, entity.New(
		entity.DBId, diskId,
		(&storage_v1alpha.Disk{
			ID:       diskId,
			Name:     "no-volume-yet",
			Status:   storage_v1alpha.PROVISIONED,
			VolumeId: "vol-pending",
		}).Encode,
	).Attrs())
	require.NoError(t, err)

	dlc := NewDiskLeaseController(slog.Default(), es.EAC, compute.NewNodeId("miren"), "")

	lease := &storage_v1alpha.DiskLease{
		ID:     entity.Id("disk_lease/waiting-on-volume"),
		DiskId: diskId,
		Status: storage_v1alpha.PENDING,
		Mount:  storage_v1alpha.Mount{Path: "/data"},
		NodeId: compute.NewNodeId("miren").Id(),
	}

	require.NoError(t, dlc.handlePendingLease(ctx, lease))

	assert.Equal(t, storage_v1alpha.PENDING, lease.Status,
		"a volume that hasn't been created yet is a wait, not a terminal failure")
	assert.Empty(t, lease.ErrorMessage)
}

// TestDiskLeaseController_UnstampedVolume covers volumes predating the node_id
// stamp. With no owner recorded on the volume, the lease's own pin is the only
// signal left: pinned to us we proceed, pinned to nobody we wait rather than
// let every node guess and fight over the mount.
func TestDiskLeaseController_UnstampedVolume(t *testing.T) {
	ctx := context.Background()

	t.Run("lease pinned to us proceeds", func(t *testing.T) {
		es, diskId := newOwnershipFixture(t, ctx, "unstamped-pinned", "")
		dlc := NewDiskLeaseController(slog.Default(), es.EAC, compute.NewNodeId("miren"), "")

		lease := &storage_v1alpha.DiskLease{
			ID:     entity.Id("disk_lease/pinned-to-us"),
			DiskId: diskId,
			Status: storage_v1alpha.PENDING,
			Mount:  storage_v1alpha.Mount{Path: "/data"},
			NodeId: compute.NewNodeId("miren").Id(),
		}

		require.NoError(t, dlc.handlePendingLease(ctx, lease))

		mounts, err := es.EAC.List(ctx, entity.Ref(storage_v1alpha.DiskMountDiskLeaseIdId, lease.ID))
		require.NoError(t, err)
		assert.Len(t, mounts.Values(), 1,
			"the designated node should still mount an unstamped volume")
	})

	t.Run("lease pinned to nobody waits", func(t *testing.T) {
		es, diskId := newOwnershipFixture(t, ctx, "unstamped-unpinned", "")
		dlc := NewDiskLeaseController(slog.Default(), es.EAC, compute.NewNodeId("miren"), "")

		lease := &storage_v1alpha.DiskLease{
			ID:     entity.Id("disk_lease/pinned-to-nobody"),
			DiskId: diskId,
			Status: storage_v1alpha.PENDING,
			Mount:  storage_v1alpha.Mount{Path: "/data"},
		}

		require.NoError(t, dlc.handlePendingLease(ctx, lease))

		assert.Equal(t, storage_v1alpha.PENDING, lease.Status)
		mounts, err := es.EAC.List(ctx, entity.Ref(storage_v1alpha.DiskMountDiskLeaseIdId, lease.ID))
		require.NoError(t, err)
		assert.Empty(t, mounts.Values(),
			"with no owner recorded anywhere, no node should claim the mount")
	})
}
