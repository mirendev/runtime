package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/snapshot"
)

// entityDiskResolver implements snapshot.DiskResolver using the entity
// access RPC client.
type entityDiskResolver struct {
	eac *entityserver_v1alpha.EntityAccessClient
	ec  *entityserver.Client
}

func newEntityDiskResolver(eac *entityserver_v1alpha.EntityAccessClient, ec *entityserver.Client) *entityDiskResolver {
	return &entityDiskResolver{eac: eac, ec: ec}
}

func (r *entityDiskResolver) FindDisk(ctx context.Context, name string) (*snapshot.DiskState, error) {
	ref := entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk)
	results, err := r.eac.List(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("listing disks: %w", err)
	}

	var matches []snapshot.DiskState
	for _, e := range results.Values() {
		var disk storage_v1alpha.Disk
		disk.Decode(e.Entity())
		if disk.Name == name {
			matches = append(matches, snapshot.DiskState{
				ID:         string(disk.ID),
				Name:       disk.Name,
				Status:     string(disk.Status),
				Filesystem: strings.TrimPrefix(string(disk.Filesystem), "filesystem."),
			})
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("disk %q not found", name)
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("multiple disks found with name %q (%d matches)", name, len(matches))
	}
}

func (r *entityDiskResolver) FindVolume(ctx context.Context, diskID string) (*snapshot.VolumeState, error) {
	resp, err := r.eac.List(ctx, entity.Ref(storage_v1alpha.DiskVolumeDiskIdId, entity.Id(diskID)))
	if err != nil {
		return nil, fmt.Errorf("listing disk volumes: %w", err)
	}

	values := resp.Values()
	if len(values) == 0 {
		return nil, fmt.Errorf("no disk volume found for disk %s", diskID)
	}
	if len(values) > 1 {
		return nil, fmt.Errorf("multiple disk volumes found for disk %s (%d matches)", diskID, len(values))
	}

	var vol storage_v1alpha.DiskVolume
	vol.Decode(values[0].Entity())
	return &snapshot.VolumeState{
		VolumeID: vol.VolumeId,
		// Where the disk controller records the volume's id in miren.cloud
		// once it has registered it there.
		CloudVolumeID: vol.CloudVolumeId,
		ImagePath:     vol.ImagePath,
	}, nil
}

// CreateDiskAndVolume creates a new disk entity in RESTORING state so the disk
// controller ignores it while restore writes the image. The returned
// RestoreTarget includes a Finalize callback that creates the disk_volume
// entity and transitions the disk to PROVISIONED.
func (r *entityDiskResolver) CreateDiskAndVolume(ctx context.Context, name string, sizeBytes int64, filesystem string, dataPath string) (*snapshot.RestoreTarget, error) {
	sizeGb := sizeBytes / (1 << 30)
	if sizeGb == 0 {
		sizeGb = 1
	}

	// Normalize filesystem string — strip enum prefix if present
	filesystem = strings.TrimPrefix(strings.ToLower(filesystem), "filesystem.")

	var fs storage_v1alpha.DiskFilesystem
	switch filesystem {
	case "ext4":
		fs = storage_v1alpha.EXT4
	case "xfs":
		fs = storage_v1alpha.XFS
	case "btrfs":
		fs = storage_v1alpha.BTRFS
	default:
		fs = storage_v1alpha.EXT4
	}

	diskId := idgen.GenNS("disk")
	volId := idgen.GenNS("disk-vol")
	imagePath := filepath.Join(dataPath, "disk-data", "volumes", volId, "disk.img")

	disk := &storage_v1alpha.Disk{
		Name:       name,
		SizeGb:     sizeGb,
		Filesystem: fs,
		Status:     storage_v1alpha.RESTORING,
	}

	diskEntityId, err := r.ec.Create(ctx, diskId, disk)
	if err != nil {
		return nil, fmt.Errorf("creating disk entity: %w", err)
	}

	nodeId, err := r.findNodeId(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding node: %w", err)
	}

	return &snapshot.RestoreTarget{
		Name:      name,
		ImagePath: imagePath,
		Created:   true,
		Cleanup: func(cctx context.Context) error {
			// Rollback must run even when the operator cancelled the restore
			// (SIGINT during Finalize). The request context is already gone
			// by then, so the entity RPCs use a non-cancellable context.
			rollbackCtx := context.WithoutCancel(cctx)

			// Finalize runs after os.Rename commits the image to ImagePath, so
			// the temp-file defer's os.Remove(tmpPath) is a no-op once cleanup
			// runs. Remove the final image; best-effort — a leftover is a
			// reclaimable disk-space leak, not a correctness hazard, and must
			// not block the entity rollback below. Tolerate "not present":
			// Finalize may have failed before the rename committed.
			if rerr := os.Remove(imagePath); rerr != nil && !os.IsNotExist(rerr) {
				_ = rerr
			}

			// Delete a disk_volume Finalize may have created before failing.
			// eac.Delete is idempotent (the store treats a missing entity as a
			// successful delete), so this is safe whether or not Finalize got
			// past its Create. Best-effort: a failure here must not block the
			// disk entity rollback, which is the authoritative step.
			r.eac.Delete(rollbackCtx, "disk_volume/"+volId)

			// The disk entity is the authoritative rollback: leaving it stuck
			// in RESTORING blocks same-name retries, so this is the one error
			// surfaced to the caller.
			if _, err := r.eac.Delete(rollbackCtx, string(diskEntityId)); err != nil {
				return fmt.Errorf("deleting disk entity during cleanup: %w", err)
			}
			return nil
		},
		Finalize: func(fctx context.Context) error {
			// Create disk_volume now that the image is written.
			vol := &storage_v1alpha.DiskVolume{
				Name:         name,
				DiskId:       diskEntityId,
				VolumeId:     volId,
				SizeGb:       sizeGb,
				Filesystem:   filesystem,
				VolumeMode:   detectVolumeMode(),
				DesiredState: storage_v1alpha.DV_PRESENT,
				ActualState:  storage_v1alpha.DV_READY,
				ImagePath:    imagePath,
				NodeId:       nodeId,
			}

			_, err := r.eac.Create(fctx, entity.New(
				entity.DBId, entity.Id("disk_volume/"+volId),
				vol.Encode,
			).Attrs())
			if err != nil {
				return fmt.Errorf("creating disk_volume entity: %w", err)
			}

			// Transition disk to PROVISIONED with volume ID.
			_, err = r.eac.Patch(fctx, []entity.Attr{
				entity.Ref(entity.DBId, diskEntityId),
				entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusProvisionedId),
				entity.String(storage_v1alpha.DiskVolumeIdId, volId),
			}, 0)
			if err != nil {
				return fmt.Errorf("updating disk to provisioned: %w", err)
			}

			return nil
		},
	}, nil
}

// findNodeId finds the coordinator node. Stateful sandboxes (those with
// disk volumes) run on the coordinator, so disk_volume entities must
// reference it.
func (r *entityDiskResolver) findNodeId(ctx context.Context) (entity.Id, error) {
	resp, err := r.eac.List(ctx, entity.Ref(entity.EntityKind, compute.KindNode))
	if err != nil {
		return "", fmt.Errorf("listing nodes: %w", err)
	}

	values := resp.Values()
	if len(values) == 0 {
		return "", fmt.Errorf("no nodes found")
	}

	// If there's only one node, use it.
	if len(values) == 1 {
		return entity.Id(values[0].Entity().Id()), nil
	}

	// Multiple nodes — find the coordinator (role=coordinator constraint).
	for _, v := range values {
		var node compute.Node
		node.Decode(v.Entity())
		if role, _ := node.Constraints.Get("role"); role == "coordinator" {
			return node.ID, nil
		}
	}

	return "", fmt.Errorf("multiple nodes found but none has role=coordinator")
}

func (r *entityDiskResolver) FindLeases(ctx context.Context, diskID string) ([]snapshot.LeaseState, error) {
	resp, err := r.eac.List(ctx, entity.Ref(storage_v1alpha.DiskLeaseDiskIdId, entity.Id(diskID)))
	if err != nil {
		return nil, fmt.Errorf("listing disk leases: %w", err)
	}

	var leases []snapshot.LeaseState
	for _, e := range resp.Values() {
		var lease storage_v1alpha.DiskLease
		lease.Decode(e.Entity())
		leases = append(leases, snapshot.LeaseState{
			ID:     string(lease.ID),
			Status: string(lease.Status),
		})
	}

	return leases, nil
}

func detectVolumeMode() storage_v1alpha.DiskVolumeVolumeMode {
	if mode := os.Getenv("MIREN_DISK_MODE"); mode == "accelerator" {
		return storage_v1alpha.VM_ACCELERATOR
	}
	if _, err := exec.LookPath("lbdctl"); err == nil {
		return storage_v1alpha.VM_ACCELERATOR
	}
	return storage_v1alpha.VM_UNIVERSAL
}
