package sandbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestAcquireDiskLease(t *testing.T) {
	t.Run("creates new lease when none exists", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node"),
		}

		// Setup: Create a disk entity
		diskID := entity.Id("disk/test-disk")
		disk := &storage.Disk{
			ID:       diskID,
			Name:     "test-disk",
			SizeGb:   10,
			Status:   storage.PROVISIONED,
			VolumeId: "vol-123",
		}
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, diskID,
			disk.Encode,
		).Attrs())
		r.NoError(err)

		// Act: Request a lease when none exists
		sandboxID := entity.Id("sandbox/new-sandbox")
		nodeID := compute.NewNodeId("test-node").Id()
		appID := entity.Id("app/test-app")

		leaseID, err := controller.acquireDiskLease(ctx, diskID, nodeID, sandboxID, appID, "/data", false)
		r.NoError(err)
		r.NotEmpty(leaseID, "should create a new lease")

		// Verify the created lease
		leaseResp, err := es.EAC.Get(ctx, leaseID.String())
		r.NoError(err)
		var createdLease storage.DiskLease
		createdLease.Decode(leaseResp.Entity().Entity())

		r.Equal(diskID, createdLease.DiskId)
		r.Equal(sandboxID, createdLease.SandboxId)
		r.Equal(nodeID, createdLease.NodeId)
		r.Equal(storage.PENDING, createdLease.Status)
		r.Equal("/data", createdLease.Mount.Path)
	})

	t.Run("returns existing lease if owned by same sandbox", func(t *testing.T) {
		// This tests the retry case where sandbox creation is retried
		// and the lease already exists from a previous attempt

		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node"),
		}

		// Setup: Create a disk entity
		diskID := entity.Id("disk/test-disk")
		disk := &storage.Disk{
			ID:       diskID,
			Name:     "test-disk",
			SizeGb:   10,
			Status:   storage.PROVISIONED,
			VolumeId: "vol-123",
		}
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, diskID,
			disk.Encode,
		).Attrs())
		r.NoError(err)

		// Setup: Create an existing lease owned by our sandbox
		sandboxID := entity.Id("sandbox/my-sandbox")
		nodeID := compute.NewNodeId("test-node").Id()
		existingLeaseID := entity.Id("disk-lease/existing-lease")

		existingLease := &storage.DiskLease{
			ID:        existingLeaseID,
			DiskId:    diskID,
			SandboxId: sandboxID,
			NodeId:    nodeID,
			Status:    storage.BOUND,
			Mount: storage.Mount{
				Path:     "/data",
				Options:  "rw",
				ReadOnly: false,
			},
		}
		_, err = es.EAC.Create(ctx, entity.New(
			entity.DBId, existingLeaseID,
			existingLease.Encode,
		).Attrs())
		r.NoError(err)

		// Act: Request a lease for the same disk/node/sandbox
		appID := entity.Id("app/test-app")
		foundLeaseID, err := controller.acquireDiskLease(ctx, diskID, nodeID, sandboxID, appID, "/data", false)
		r.NoError(err)
		r.Equal(existingLeaseID, foundLeaseID, "should return the existing lease")
	})

	t.Run("fails if lease is active for another sandbox", func(t *testing.T) {
		// If another sandbox has a PENDING or BOUND lease, we shouldn't
		// create a new one (indicates that sandbox hasn't been cleaned up)

		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node"),
		}

		// Setup: Create a disk entity
		diskID := entity.Id("disk/test-disk")
		disk := &storage.Disk{
			ID:       diskID,
			Name:     "test-disk",
			SizeGb:   10,
			Status:   storage.PROVISIONED,
			VolumeId: "vol-123",
		}
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, diskID,
			disk.Encode,
		).Attrs())
		r.NoError(err)

		nodeID := compute.NewNodeId("test-node").Id()
		otherSandboxID := entity.Id("sandbox/other-sandbox")
		mySandboxID := entity.Id("sandbox/my-sandbox")
		appID := entity.Id("app/test-app")

		// Test both PENDING and BOUND states block new leases
		for _, status := range []storage.DiskLeaseStatus{storage.PENDING, storage.BOUND} {
			// Create an existing lease owned by another sandbox
			existingLeaseID := entity.Id("disk-lease/other-lease-" + string(status))
			existingLease := &storage.DiskLease{
				ID:        existingLeaseID,
				DiskId:    diskID,
				SandboxId: otherSandboxID,
				NodeId:    nodeID,
				Status:    status,
				Mount: storage.Mount{
					Path:     "/data",
					Options:  "rw",
					ReadOnly: false,
				},
			}
			_, err = es.EAC.Create(ctx, entity.New(
				entity.DBId, existingLeaseID,
				existingLease.Encode,
			).Attrs())
			r.NoError(err)

			// Try to acquire a lease - should fail
			_, err = controller.acquireDiskLease(ctx, diskID, nodeID, mySandboxID, appID, "/data", false)
			r.Error(err, "should fail when another sandbox has a %s lease", status)
			r.Contains(err.Error(), "active lease")

			// Clean up for next iteration
			_, _ = es.EAC.Delete(ctx, existingLeaseID.String())
		}
	})

	t.Run("creates new lease if existing lease is released", func(t *testing.T) {
		// This tests the case where an old sandbox's lease was released
		// and a new sandbox should be able to create a fresh lease

		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node"),
		}

		// Setup: Create a disk entity
		diskID := entity.Id("disk/test-disk")
		disk := &storage.Disk{
			ID:       diskID,
			Name:     "test-disk",
			SizeGb:   10,
			Status:   storage.PROVISIONED,
			VolumeId: "vol-123",
		}
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, diskID,
			disk.Encode,
		).Attrs())
		r.NoError(err)

		// Setup: Create an existing RELEASED lease (from a dead sandbox)
		oldSandboxID := entity.Id("sandbox/old-sandbox")
		nodeID := compute.NewNodeId("test-node").Id()
		releasedLeaseID := entity.Id("disk-lease/released-lease")

		releasedLease := &storage.DiskLease{
			ID:        releasedLeaseID,
			DiskId:    diskID,
			SandboxId: oldSandboxID,
			NodeId:    nodeID,
			Status:    storage.RELEASED, // Already released
			Mount: storage.Mount{
				Path:     "/data",
				Options:  "rw",
				ReadOnly: false,
			},
		}
		_, err = es.EAC.Create(ctx, entity.New(
			entity.DBId, releasedLeaseID,
			releasedLease.Encode,
		).Attrs())
		r.NoError(err)

		// Act: A new sandbox should be able to create a new lease
		newSandboxID := entity.Id("sandbox/new-sandbox")
		appID := entity.Id("app/test-app")
		newLeaseID, err := controller.acquireDiskLease(ctx, diskID, nodeID, newSandboxID, appID, "/data", false)

		r.NoError(err)
		r.NotEqual(releasedLeaseID, newLeaseID, "should create a new lease, not reuse released one")

		// Verify it's a new lease owned by the new sandbox
		leaseResp, err := es.EAC.Get(ctx, newLeaseID.String())
		r.NoError(err)
		var newLease storage.DiskLease
		newLease.Decode(leaseResp.Entity().Entity())

		r.Equal(newSandboxID, newLease.SandboxId)
		r.Equal(storage.PENDING, newLease.Status)
	})

	t.Run("does not reuse lease from different node", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node-2"),
		}

		// Setup: Create a disk entity
		diskID := entity.Id("disk/test-disk")
		disk := &storage.Disk{
			ID:       diskID,
			Name:     "test-disk",
			SizeGb:   10,
			Status:   storage.PROVISIONED,
			VolumeId: "vol-123",
		}
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, diskID,
			disk.Encode,
		).Attrs())
		r.NoError(err)

		// Setup: Create an existing lease on a DIFFERENT node
		sandbox1ID := entity.Id("sandbox/sandbox-on-node1")
		node1ID := compute.NewNodeId("test-node-1").Id()
		existingLeaseID := entity.Id("disk-lease/node1-lease")

		existingLease := &storage.DiskLease{
			ID:        existingLeaseID,
			DiskId:    diskID,
			SandboxId: sandbox1ID,
			NodeId:    node1ID, // Different node
			Status:    storage.BOUND,
			Mount: storage.Mount{
				Path:     "/data",
				Options:  "rw",
				ReadOnly: false,
			},
		}
		_, err = es.EAC.Create(ctx, entity.New(
			entity.DBId, existingLeaseID,
			existingLease.Encode,
		).Attrs())
		r.NoError(err)

		// Act: Request a lease on node2 (controller.NodeId = "test-node-2")
		sandbox2ID := entity.Id("sandbox/sandbox-on-node2")
		node2ID := compute.NewNodeId("test-node-2").Id()
		appID := entity.Id("app/test-app")

		newLeaseID, err := controller.acquireDiskLease(ctx, diskID, node2ID, sandbox2ID, appID, "/data", false)
		r.NoError(err)

		// Should have created a NEW lease, not affected by the one from node1
		r.NotEqual(existingLeaseID, newLeaseID, "should create new lease for different node")

		// Verify the new lease
		leaseResp, err := es.EAC.Get(ctx, newLeaseID.String())
		r.NoError(err)
		var createdLease storage.DiskLease
		createdLease.Decode(leaseResp.Entity().Entity())

		r.Equal(diskID, createdLease.DiskId)
		r.Equal(sandbox2ID, createdLease.SandboxId)
		r.Equal(node2ID, createdLease.NodeId)
	})
}

func TestReleaseDiskLeases(t *testing.T) {
	t.Run("releases leases owned by sandbox", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node"),
		}

		// Setup: Create a sandbox and its disk lease
		sandboxID := entity.Id("sandbox/dying-sandbox")
		nodeID := compute.NewNodeId("test-node").Id()
		diskID := entity.Id("disk/test-disk")
		leaseID := entity.Id("disk-lease/to-be-released")

		lease := &storage.DiskLease{
			ID:        leaseID,
			DiskId:    diskID,
			SandboxId: sandboxID,
			NodeId:    nodeID,
			Status:    storage.BOUND,
			Mount: storage.Mount{
				Path:     "/data",
				Options:  "rw",
				ReadOnly: false,
			},
		}
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, leaseID,
			lease.Encode,
		).Attrs())
		r.NoError(err)

		// Verify lease is initially BOUND
		leaseResp, err := es.EAC.Get(ctx, leaseID.String())
		r.NoError(err)
		var storedLease storage.DiskLease
		storedLease.Decode(leaseResp.Entity().Entity())
		r.Equal(storage.BOUND, storedLease.Status)

		// Act: Release disk leases for the sandbox
		err = controller.ReleaseDiskLeases(ctx, sandboxID)
		r.NoError(err)

		// Assert: Lease should now be RELEASED
		leaseResp, err = es.EAC.Get(ctx, leaseID.String())
		r.NoError(err)
		var releasedLease storage.DiskLease
		releasedLease.Decode(leaseResp.Entity().Entity())
		r.Equal(storage.RELEASED, releasedLease.Status,
			"lease should be transitioned to RELEASED status")
	})

	t.Run("does not affect leases owned by other sandboxes", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node"),
		}

		// Setup: Create two sandboxes with their own leases
		sandbox1ID := entity.Id("sandbox/sandbox-1")
		sandbox2ID := entity.Id("sandbox/sandbox-2")
		nodeID := compute.NewNodeId("test-node").Id()

		lease1ID := entity.Id("disk-lease/lease-1")
		lease1 := &storage.DiskLease{
			ID:        lease1ID,
			DiskId:    entity.Id("disk/disk-1"),
			SandboxId: sandbox1ID,
			NodeId:    nodeID,
			Status:    storage.BOUND,
		}
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, lease1ID,
			lease1.Encode,
		).Attrs())
		r.NoError(err)

		lease2ID := entity.Id("disk-lease/lease-2")
		lease2 := &storage.DiskLease{
			ID:        lease2ID,
			DiskId:    entity.Id("disk/disk-2"),
			SandboxId: sandbox2ID,
			NodeId:    nodeID,
			Status:    storage.BOUND,
		}
		_, err = es.EAC.Create(ctx, entity.New(
			entity.DBId, lease2ID,
			lease2.Encode,
		).Attrs())
		r.NoError(err)

		// Act: Release leases for sandbox1 only
		err = controller.ReleaseDiskLeases(ctx, sandbox1ID)
		r.NoError(err)

		// Assert: sandbox1's lease should be RELEASED
		leaseResp, err := es.EAC.Get(ctx, lease1ID.String())
		r.NoError(err)
		var updatedLease1 storage.DiskLease
		updatedLease1.Decode(leaseResp.Entity().Entity())
		r.Equal(storage.RELEASED, updatedLease1.Status)

		// Assert: sandbox2's lease should still be BOUND
		leaseResp, err = es.EAC.Get(ctx, lease2ID.String())
		r.NoError(err)
		var updatedLease2 storage.DiskLease
		updatedLease2.Decode(leaseResp.Entity().Entity())
		r.Equal(storage.BOUND, updatedLease2.Status,
			"other sandbox's lease should not be affected")
	})
}

// testSchedule returns a Schedule encoder that assigns sandboxes to the
// test node, matching the node-scoped index used by Periodic.
func testSchedule() compute.Schedule {
	return compute.Schedule{
		Key: compute.Key{
			Kind: compute.KindSandbox,
			Node: compute.NewNodeId("test-node").Id(),
		},
	}
}

func TestPeriodicReleasesDiskLeases(t *testing.T) {
	t.Run("releases disk leases before deleting dead sandbox", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node"),
		}

		sandboxID := entity.Id("sandbox/dead-with-lease")
		diskID := entity.Id("disk/test-disk")
		leaseID := entity.Id("disk-lease/orphan-candidate")

		// Create a DEAD sandbox entity
		schedule := testSchedule()
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, sandboxID,
			(&compute.Sandbox{
				ID:     sandboxID,
				Status: compute.DEAD,
			}).Encode,
			schedule.Encode,
		).Attrs())
		r.NoError(err)

		// Create a BOUND disk lease pointing at the sandbox
		_, err = es.EAC.Create(ctx, entity.New(
			entity.DBId, leaseID,
			(&storage.DiskLease{
				ID:        leaseID,
				DiskId:    diskID,
				SandboxId: sandboxID,
				NodeId:    compute.NewNodeId("test-node").Id(),
				Status:    storage.BOUND,
			}).Encode,
		).Attrs())
		r.NoError(err)

		// Use a negative time horizon so the cutoff is slightly in the future,
		// avoiding a race where updatedAt ≈ now causes Before(cutoff) to fail.
		err = controller.Periodic(ctx, -time.Millisecond)
		r.NoError(err)

		// Verify the disk lease was released
		leaseResp, err := es.EAC.Get(ctx, leaseID.String())
		r.NoError(err)
		var releasedLease storage.DiskLease
		releasedLease.Decode(leaseResp.Entity().Entity())
		r.Equal(storage.RELEASED, releasedLease.Status,
			"disk lease should be released before sandbox deletion")

		// Verify the sandbox entity was deleted
		resp, err := es.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
		r.NoError(err)
		r.Empty(resp.Values(), "dead sandbox should have been deleted")
	})

	t.Run("still deletes sandbox when no disk leases exist", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node"),
		}

		sandboxID := entity.Id("sandbox/dead-no-lease")

		// Create a DEAD sandbox entity with no disk leases
		schedule := testSchedule()
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, sandboxID,
			(&compute.Sandbox{
				ID:     sandboxID,
				Status: compute.DEAD,
			}).Encode,
			schedule.Encode,
		).Attrs())
		r.NoError(err)

		// Use a negative time horizon so the cutoff is slightly in the future,
		// avoiding a race where updatedAt ≈ now causes Before(cutoff) to fail.
		err = controller.Periodic(ctx, -time.Millisecond)
		r.NoError(err)

		resp, err := es.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
		r.NoError(err)
		r.Empty(resp.Values(), "dead sandbox should have been deleted")
	})

	t.Run("does not release leases for non-dead sandboxes", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()

		es, cleanup := testutils.NewInMemEntityServer(t)
		defer cleanup()

		log := testutils.TestLogger(t)

		controller := &SandboxController{
			Log:    log,
			EAC:    es.EAC,
			NodeId: compute.NewNodeId("test-node"),
		}

		sandboxID := entity.Id("sandbox/running-with-lease")
		diskID := entity.Id("disk/test-disk")
		leaseID := entity.Id("disk-lease/should-stay-bound")

		// Create a RUNNING sandbox
		schedule := testSchedule()
		_, err := es.EAC.Create(ctx, entity.New(
			entity.DBId, sandboxID,
			(&compute.Sandbox{
				ID:     sandboxID,
				Status: compute.RUNNING,
			}).Encode,
			schedule.Encode,
		).Attrs())
		r.NoError(err)

		// Create a BOUND disk lease for the running sandbox
		_, err = es.EAC.Create(ctx, entity.New(
			entity.DBId, leaseID,
			(&storage.DiskLease{
				ID:        leaseID,
				DiskId:    diskID,
				SandboxId: sandboxID,
				NodeId:    compute.NewNodeId("test-node").Id(),
				Status:    storage.BOUND,
			}).Encode,
		).Attrs())
		r.NoError(err)

		// Periodic should skip running sandboxes
		err = controller.Periodic(ctx, 0)
		r.NoError(err)

		// Verify lease is still BOUND
		leaseResp, err := es.EAC.Get(ctx, leaseID.String())
		r.NoError(err)
		var lease storage.DiskLease
		lease.Decode(leaseResp.Entity().Entity())
		r.Equal(storage.BOUND, lease.Status,
			"lease for running sandbox should not be released")

		// Verify sandbox still exists
		resp, err := es.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
		r.NoError(err)
		r.Len(resp.Values(), 1, "running sandbox should not be deleted")
	})
}
