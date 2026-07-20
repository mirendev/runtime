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
