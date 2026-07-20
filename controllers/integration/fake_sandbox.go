package integration

import (
	"context"
	"fmt"
	"log/slog"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// FakeSandboxController replicates the disk management logic from
// controllers/sandbox/volume.go without requiring containerd or real networking.
// It creates/acquires disks and leases through the entity store, then relies on
// the harness's ReconcileAll to drive the disk controllers to completion.
type FakeSandboxController struct {
	Log    *slog.Logger
	EAC    *entityserver_v1alpha.EntityAccessClient
	NodeId compute.NodeId
}

// NewFakeSandboxController creates a new fake sandbox controller.
func NewFakeSandboxController(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, nodeId compute.NodeId) *FakeSandboxController {
	return &FakeSandboxController{
		Log:    log.With("module", "fake-sandbox"),
		EAC:    eac,
		NodeId: nodeId,
	}
}

// EnsureDisk looks up or creates a Disk entity by name.
// Mirrors controllers/sandbox/volume.go:ensureDisk.
func (f *FakeSandboxController) EnsureDisk(ctx context.Context, diskName string, sizeGB int64, filesystem string) (entity.Id, error) {
	// Search for existing disk by name
	listResp, err := f.EAC.List(ctx, entity.String(storage.DiskNameId, diskName))
	if err != nil {
		return "", fmt.Errorf("failed to query disks by name: %w", err)
	}

	if len(listResp.Values()) > 0 {
		e := listResp.Values()[0]
		var disk storage.Disk
		disk.Decode(e.Entity())
		f.Log.Info("found existing disk", "disk", disk.ID, "name", diskName)
		return disk.ID, nil
	}

	if sizeGB <= 0 {
		return "", fmt.Errorf("disk %q does not exist and no size specified", diskName)
	}

	var fs storage.DiskFilesystem
	switch filesystem {
	case "ext4":
		fs = storage.EXT4
	case "xfs":
		fs = storage.XFS
	default:
		fs = storage.EXT4
	}

	disk := &storage.Disk{
		Name:       diskName,
		SizeGb:     sizeGB,
		Filesystem: fs,
		Status:     storage.PROVISIONING,
	}

	name := idgen.GenNS("disk")
	diskID := entity.Id("disk/" + name)
	putResp, err := f.EAC.Create(ctx, entity.New(
		entity.DBId, diskID,
		disk.Encode,
	).Attrs())
	if err != nil {
		return "", fmt.Errorf("failed to create disk entity: %w", err)
	}

	f.Log.Info("created disk", "disk", diskID, "resp_id", putResp.Id(), "name", diskName)
	return diskID, nil
}

// AcquireDiskLease creates a new DiskLease entity for the given sandbox.
// Mirrors controllers/sandbox/volume.go:acquireDiskLease.
func (f *FakeSandboxController) AcquireDiskLease(ctx context.Context, diskID, sandboxID, appID entity.Id, mountPath string, readOnly bool) (entity.Id, error) {
	// Check for existing lease for this sandbox
	listResp, err := f.EAC.List(ctx, entity.Ref(entity.EntityKind, storage.KindDiskLease))
	if err != nil {
		return "", fmt.Errorf("failed to list disk leases: %w", err)
	}

	nodeID := f.NodeId.Id()

	for _, e := range listResp.Values() {
		var lease storage.DiskLease
		lease.Decode(e.Entity())

		if lease.DiskId == diskID && f.NodeId.Matches(lease.NodeId) {
			if lease.SandboxId == sandboxID {
				f.Log.Info("found existing lease", "lease", lease.ID)
				return lease.ID, nil
			}

			if lease.Status == storage.PENDING || lease.Status == storage.BOUND {
				return "", fmt.Errorf("disk %s has active lease (%s) for sandbox %s", diskID, lease.Status, lease.SandboxId)
			}
		}
	}

	// Create new lease
	lease := &storage.DiskLease{
		DiskId:    diskID,
		SandboxId: sandboxID,
		AppId:     appID,
		Status:    storage.PENDING,
		Mount: storage.Mount{
			Path:     mountPath,
			ReadOnly: readOnly,
			Options:  "rw",
		},
		NodeId: nodeID,
	}

	if readOnly {
		lease.Mount.Options = "ro"
	}

	name := idgen.GenNS("disk-lease")
	leaseID := entity.Id("disk-lease/" + name)
	_, err = f.EAC.Create(ctx, entity.New(
		entity.DBId, leaseID,
		lease.Encode,
	).Attrs())
	if err != nil {
		return "", fmt.Errorf("failed to create disk lease: %w", err)
	}

	f.Log.Info("created disk lease", "lease", leaseID, "disk", diskID, "sandbox", sandboxID)
	return leaseID, nil
}

// ReleaseDiskLeases releases all disk leases owned by the given sandbox.
// Mirrors controllers/sandbox/sandbox.go:releaseDiskLeases.
func (f *FakeSandboxController) ReleaseDiskLeases(ctx context.Context, sandboxID entity.Id) error {
	listResp, err := f.EAC.List(ctx, entity.Ref(entity.EntityKind, storage.KindDiskLease))
	if err != nil {
		return fmt.Errorf("failed to list disk leases: %w", err)
	}

	for _, e := range listResp.Values() {
		var lease storage.DiskLease
		lease.Decode(e.Entity())

		if lease.SandboxId != sandboxID {
			continue
		}

		if lease.Status == storage.RELEASED {
			continue
		}

		f.Log.Info("releasing disk lease", "lease", lease.ID, "disk", lease.DiskId, "sandbox", sandboxID)

		_, err := f.EAC.Patch(ctx, entity.New(
			entity.DBId, lease.ID,
			(&storage.DiskLease{
				Status: storage.RELEASED,
			}).Encode,
		).Attrs(), 0)
		if err != nil {
			f.Log.Error("failed to release lease", "lease", lease.ID, "error", err)
		}
	}

	return nil
}
