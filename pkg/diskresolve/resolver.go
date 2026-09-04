package diskresolve

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

// Resolver implements snapshot.DiskResolver using the entity
// access RPC client.
type Resolver struct {
	eac *entityserver_v1alpha.EntityAccessClient
	ec  *entityserver.Client
}

func New(eac *entityserver_v1alpha.EntityAccessClient, ec *entityserver.Client) *Resolver {
	return &Resolver{eac: eac, ec: ec}
}

func (r *Resolver) FindDisk(ctx context.Context, name string) (*snapshot.DiskState, error) {
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
		// Typed, so callers can tell "no such disk" from "the lookup failed".
		// PrepareRestore creates a disk on the first and must not on the second.
		return nil, snapshot.DiskNotFoundError{Name: name}
	case 1:
		return &matches[0], nil
	default:
		return nil, fmt.Errorf("multiple disks found with name %q (%d matches)", name, len(matches))
	}
}

func (r *Resolver) FindVolume(ctx context.Context, diskID string) (*snapshot.VolumeState, error) {
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
func (r *Resolver) CreateDiskAndVolume(ctx context.Context, name string, sizeBytes int64, filesystem string, dataPath string) (*snapshot.RestoreTarget, error) {
	// Round up. Truncating would hand back a disk that claims less capacity
	// than the image it is about to be given, so a 1.5 GiB image would land on
	// a disk reporting 1 GiB.
	sizeGb := (sizeBytes + (1 << 30) - 1) / (1 << 30)
	if sizeGb == 0 {
		sizeGb = 1
	}

	// Normalize filesystem string — strip enum prefix if present
	filesystem = strings.TrimPrefix(strings.ToLower(filesystem), "filesystem.")
	fs := ParseFilesystem(filesystem)

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

	nodeId, err := r.FindNodeId(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding node: %w", err)
	}

	return &snapshot.RestoreTarget{
		Name:      name,
		ImagePath: imagePath,
		Created:   true,
		Cleanup: func(cctx context.Context) error {
			// The restore's own context is already cancelled when the
			// operator interrupts the restore, and the rollback is exactly
			// what has to run then. Detach so cleanup is not defeated by
			// the failure it is cleaning up after.
			cctx = context.WithoutCancel(cctx)

			// Finalize is the last fallible step of a restore, so cleanup
			// only ever runs when it did not complete — and its
			// disk_volume Create is its last write. Nothing owns the image
			// at this point, and the restore already renamed it into
			// place, so the temp-file removal in disk_restore.go is a
			// no-op for it. Remove it before touching the disk: a
			// PROVISIONED disk still carrying its VolumeId can be
			// self-healed into a disk_volume by the controller, and there
			// is no reason to let that adopt an image on its way to being
			// torn down. Tolerate "not present" — the rename may never
			// have happened.
			imageErr := os.Remove(imagePath)
			if os.IsNotExist(imageErr) {
				imageErr = nil
			}

			// Transition the disk to DELETING rather than deleting the
			// entity outright. A direct Delete bypasses the disk
			// controller's DELETING-driven handleDeletion path, which is
			// the only writer of disk_volume.desired_state=DV_ABSENT; once
			// the disk is gone, a disk_volume the controller self-healed
			// from the PROVISIONED disk has no reaper and is left
			// reconciled by the coordinator as a live mount / volume
			// directory / phantom cloud volume. Marking the disk DELETING
			// keeps it alive to drive that existing contract, which tears
			// the disk_volume down via the coordinator's
			// DiskVolumeController. Idempotent: patching an already-DELETING
			// disk is a no-op.
			_, err := r.eac.Patch(cctx, []entity.Attr{
				entity.Ref(entity.DBId, diskEntityId),
				entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusDeletingId),
			}, 0)
			if err != nil {
				return fmt.Errorf("transitioning disk to deleting during cleanup: %w", err)
			}

			// Reported only once the authoritative step is done, so a
			// failure to reclaim the image never costs us the disk
			// rollback — a leftover image is disk space, a stuck
			// RESTORING disk blocks every same-name retry.
			if imageErr != nil {
				return fmt.Errorf("removing restored image during cleanup: %w", imageErr)
			}
			return nil
		},
		Finalize: func(fctx context.Context) error {
			vol := &storage_v1alpha.DiskVolume{
				Name:         name,
				DiskId:       diskEntityId,
				VolumeId:     volId,
				SizeGb:       sizeGb,
				Filesystem:   filesystem,
				VolumeMode:   DetectVolumeMode(),
				DesiredState: storage_v1alpha.DV_PRESENT,
				ActualState:  storage_v1alpha.DV_READY,
				ImagePath:    imagePath,
				NodeId:       nodeId,
			}

			// Transition the disk to PROVISIONED before creating the
			// disk_volume. These are two independent, non-transactional
			// writes, so the order matters: if the disk_volume were
			// created first and the Patch then failed, the deferred
			// Cleanup would orphan the committed disk_volume by deleting
			// its parent disk out from under it. Patching first means a
			// Create failure leaves no disk_volume behind, and a surviving
			// PROVISIONED disk drives the existing DELETING-based cleanup
			// and self-healing paths.
			_, err := r.eac.Patch(fctx, []entity.Attr{
				entity.Ref(entity.DBId, diskEntityId),
				entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusProvisionedId),
				entity.String(storage_v1alpha.DiskVolumeIdId, volId),
			}, 0)
			if err != nil {
				return fmt.Errorf("updating disk to provisioned: %w", err)
			}

			_, err = r.eac.Create(fctx, entity.New(
				entity.DBId, entity.Id("disk_volume/"+volId),
				vol.Encode,
			).Attrs())
			if err != nil {
				return fmt.Errorf("creating disk_volume entity: %w", err)
			}

			return nil
		},
	}, nil
}

// FindNodeId finds the coordinator node. Stateful sandboxes (those with
// disk volumes) run on the coordinator, so disk_volume entities must
// reference it.
func (r *Resolver) FindNodeId(ctx context.Context) (entity.Id, error) {
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

func (r *Resolver) FindLeases(ctx context.Context, diskID string) ([]snapshot.LeaseState, error) {
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

// ParseFilesystem maps a filesystem name onto the disk enum, tolerating the
// "filesystem." prefix the enum renders with. Anything unrecognized becomes
// ext4, which is the default a disk gets when it does not ask for one.
func ParseFilesystem(fs string) storage_v1alpha.DiskFilesystem {
	switch strings.TrimPrefix(strings.ToLower(fs), "filesystem.") {
	case "xfs":
		return storage_v1alpha.XFS
	case "btrfs":
		return storage_v1alpha.BTRFS
	default:
		return storage_v1alpha.EXT4
	}
}

func DetectVolumeMode() storage_v1alpha.DiskVolumeVolumeMode {
	if mode := os.Getenv("MIREN_DISK_MODE"); mode == "accelerator" {
		return storage_v1alpha.VM_ACCELERATOR
	}
	if _, err := exec.LookPath("lbdctl"); err == nil {
		return storage_v1alpha.VM_ACCELERATOR
	}
	return storage_v1alpha.VM_UNIVERSAL
}
