package diskresolve

import (
	"context"
	"fmt"
	"math"
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
// entity and hands the disk to the controller as PROVISIONING, which promotes
// it once the volume is ready.
func (r *Resolver) CreateDiskAndVolume(ctx context.Context, name string, sizeBytes int64, filesystem string, dataPath string) (*snapshot.RestoreTarget, error) {
	sizeGb, err := diskSizeGb(sizeBytes)
	if err != nil {
		return nil, err
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
		// The disk is already committed but the caller is about to get an
		// error and no RestoreTarget, so it has no Cleanup to call. Unwind it
		// here or it sits in RESTORING forever, and the next restore of the
		// same name finds that disk, skips creating one, and fails looking for
		// the volume it never got.
		if uerr := r.abandonDisk(ctx, diskEntityId); uerr != nil {
			return nil, fmt.Errorf(
				"finding node: %w (the half-created disk could not be unwound either, so %s is stuck in restoring: %v)",
				err, name, uerr)
		}
		return nil, fmt.Errorf("finding node: %w", err)
	}

	// Set once Finalize has committed the disk_volume, which is the moment the
	// image stops being ours to delete. Finalize and Cleanup are called in turn
	// by the restore handler on one goroutine, so a plain bool is enough.
	var volumeCommitted bool

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

			// Transition the disk to DELETING rather than deleting the
			// entity outright. A direct Delete bypasses the disk
			// controller's DELETING-driven handleDeletion path, which is
			// the only writer of disk_volume.desired_state=DV_ABSENT; a
			// disk_volume whose disk is gone has no reaper and is left
			// reconciled by the coordinator as a live mount / volume
			// directory / phantom cloud volume. Marking the disk DELETING
			// keeps it alive to drive that existing contract, which tears
			// the disk_volume down via the coordinator's
			// DiskVolumeController. Idempotent: patching an already-DELETING
			// disk is a no-op.
			//
			_, err := r.eac.Patch(cctx, []entity.Attr{
				entity.Ref(entity.DBId, diskEntityId),
				entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusDeletingId),
			}, 0)
			if err != nil {
				return fmt.Errorf("transitioning disk to deleting during cleanup: %w", err)
			}

			// Once the disk_volume exists, the image belongs to the teardown
			// above and not to us. That teardown is asynchronous: the patch
			// only marks the disk, and the controller unmounts, detaches and
			// soft-deletes the volume directory some time later. Removing the
			// image here would race it, and losing that race unlinks a file
			// whose loop device is still attached, which leaves the kernel
			// holding an inode nobody can reach instead of releasing it.
			//
			// Waiting for the teardown instead would mean polling the entity
			// store from an error path that is already handling a failure.
			// Letting the controller finish the job it already does is both
			// simpler and the thing that cannot race.
			if volumeCommitted {
				return nil
			}

			// No volume was ever created, which is the common case: most
			// cleanups run before Finalize. Nothing has opened this image, so
			// there is no teardown to wait for and no loop device to strand.
			// Tolerate "not present" — the restore may never have renamed the
			// image into place.
			if imageErr := os.Remove(imagePath); imageErr != nil && !os.IsNotExist(imageErr) {
				// Reported only once the authoritative step is done, so a
				// failure to reclaim the image never costs us the disk
				// rollback — a leftover image is disk space, a stuck
				// RESTORING disk blocks every same-name retry.
				return fmt.Errorf("removing restored image during cleanup: %w", imageErr)
			}
			return nil
		},
		Finalize: func(fctx context.Context) error {
			vol := &storage_v1alpha.DiskVolume{
				Name:       name,
				DiskId:     diskEntityId,
				VolumeId:   volId,
				SizeGb:     sizeGb,
				Filesystem: filesystem,
				VolumeMode: DetectVolumeMode(),

				DesiredState: storage_v1alpha.DV_PRESENT,
				// Start PENDING, not READY, and let the DiskVolumeController
				// drive it the rest of the way. The image is already written
				// and formatted, so it mounts what was restored rather than
				// reimaging.
				ActualState: storage_v1alpha.DV_PENDING,
				ImagePath:   imagePath,
				NodeId:      nodeId,
			}

			// The volume goes in first, and the disk moves to PROVISIONING
			// rather than PROVISIONED. Both halves of that matter, and the
			// reason is a window rather than a preference.
			//
			// DiskController ignores a RESTORING disk, so nothing watches
			// this disk until the patch below. Announcing PROVISIONED first
			// would end that truce while there was still no disk_volume to
			// find, and handleProvisioned answers a provisioned disk with no
			// volume by provisioning a blank one. The restore's own volume
			// then lands beside it: two DV_READY volumes on one disk, both
			// mounted, and the next backup fails on the ambiguity.
			//
			// This way the volume is already there when the disk becomes
			// visible, and the controller promotes it to PROVISIONED once the
			// volume reports ready. It is the same handshake a recovered disk
			// uses, so there is one path through this transition instead of
			// two.
			_, err := r.eac.Create(fctx, entity.New(
				entity.DBId, entity.Id("disk_volume/"+volId),
				vol.Encode,
			).Attrs())
			if err != nil {
				return fmt.Errorf("creating disk_volume entity: %w", err)
			}
			// From here the volume owns the image, so Cleanup must leave it to
			// the controller's teardown rather than unlinking it itself.
			volumeCommitted = true

			// A failure here leaves a RESTORING disk with a committed volume,
			// which Cleanup handles: it marks the disk DELETING, and the
			// controller tears the volume down from there.
			_, err = r.eac.Patch(fctx, []entity.Attr{
				entity.Ref(entity.DBId, diskEntityId),
				entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusProvisioningId),
				entity.String(storage_v1alpha.DiskVolumeIdId, volId),
			}, 0)
			if err != nil {
				return fmt.Errorf("updating disk to provisioning: %w", err)
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

// abandonDisk marks a disk DELETING when creation gave up partway through.
//
// It is the same unwind Cleanup performs, for the window before there is a
// RestoreTarget to hang a Cleanup on.
func (r *Resolver) abandonDisk(ctx context.Context, diskEntityId entity.Id) error {
	// The caller's context may already be cancelled, and unwinding is exactly
	// what still has to happen when it is.
	ctx = context.WithoutCancel(ctx)

	_, err := r.eac.Patch(ctx, []entity.Attr{
		entity.Ref(entity.DBId, diskEntityId),
		entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusDeletingId),
	}, 0)
	return err
}

// gib is the unit disks are sized in.
const gib = 1 << 30

// diskSizeGb converts an image size into the capacity to record on the disk.
//
// It rounds up, because a disk claiming less capacity than the image it is
// about to be given is wrong on its face: a 1.5 GiB image would land on a disk
// reporting 1 GiB.
//
// The size comes from a snapshot header, which is a file the caller handed us,
// so it is checked rather than trusted. The rounding is done with a remainder
// rather than by adding gib-1 so that a size near the top of the range cannot
// overflow into a negative capacity, and the upper bound is where the
// controller's own conversion back to bytes would overflow.
func diskSizeGb(sizeBytes int64) (int64, error) {
	switch {
	case sizeBytes < 0:
		return 0, fmt.Errorf("snapshot reports a negative image size (%d bytes)", sizeBytes)
	case sizeBytes > math.MaxInt64-gib+1:
		// The ceiling is the largest size whose rounded-up capacity still
		// converts back to bytes without overflowing, which is the whole GiB
		// just below the top of the range.
		return 0, fmt.Errorf("snapshot reports an image size too large to be real (%d bytes)", sizeBytes)
	}

	sizeGb := sizeBytes / gib
	if sizeBytes%gib != 0 {
		sizeGb++
	}
	// Even an empty image gets a disk, and a disk has to be at least 1 GB.
	if sizeGb == 0 {
		sizeGb = 1
	}
	return sizeGb, nil
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
