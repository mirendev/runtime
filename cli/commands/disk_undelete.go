package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// DiskUndelete restores a recently deleted disk from the soft-delete holding area.
func DiskUndelete(ctx *Context, opts struct {
	ConfigCentric
	Name     string `short:"n" long:"name" description:"Disk name to undelete" required:"true"`
	VolumeID string `short:"V" long:"volume-id" description:"Volume ID to restore (when multiple deleted disks share a name)"`
	DataPath string `long:"data-path" description:"Path to miren data directory" default:"/var/lib/miren"`
}) error {
	diskDataPath := filepath.Join(opts.DataPath, "disk-data")
	if _, err := os.Stat(diskDataPath); err != nil {
		return fmt.Errorf("data path %s not found — disk undelete must be run on the server", diskDataPath)
	}

	entries, err := diskio.ListDeletedVolumes(diskDataPath)
	if err != nil {
		return fmt.Errorf("listing deleted volumes: %w", err)
	}

	// Filter by name
	var matches []diskio.DeletedVolumeEntry
	for _, e := range entries {
		if e.Metadata.DiskName == opts.Name {
			if opts.VolumeID == "" || e.Metadata.VolumeID == opts.VolumeID {
				matches = append(matches, e)
			}
		}
	}

	if len(matches) == 0 {
		return fmt.Errorf("no deleted disk found with name %q", opts.Name)
	}

	if len(matches) > 1 {
		ctx.Info("Multiple deleted disks found with name %q:", opts.Name)
		for _, m := range matches {
			age := time.Since(m.Metadata.DeletedAt).Truncate(time.Minute)
			ctx.Info("  Volume ID: %s  (deleted %s ago, %d GB, %s)",
				m.Metadata.VolumeID, age, m.Metadata.SizeGb, m.Metadata.Filesystem)
		}
		return fmt.Errorf("specify --volume-id to select which disk to restore")
	}

	entry := matches[0]
	meta := entry.Metadata

	ctx.Info("Restoring deleted disk:")
	ctx.Info("  Name:       %s", meta.DiskName)
	ctx.Info("  Size:       %d GB", meta.SizeGb)
	ctx.Info("  Filesystem: %s", meta.Filesystem)
	ctx.Info("  Deleted:    %s", meta.DeletedAt.Format(time.RFC3339))

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)
	resolver := newEntityDiskResolver(eac, ec)

	// Check if a disk with this name already exists
	if _, err := resolver.FindDisk(context.Background(), meta.DiskName); err == nil {
		return fmt.Errorf("a disk named %q already exists — rename or delete it before restoring", meta.DiskName)
	}

	// Normalize filesystem
	filesystem := strings.TrimPrefix(strings.ToLower(meta.Filesystem), "filesystem.")

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

	// Create the disk entity in RESTORING state
	diskId := idgen.GenNS("disk")
	disk := &storage_v1alpha.Disk{
		Name:       meta.DiskName,
		SizeGb:     meta.SizeGb,
		Filesystem: fs,
		Status:     storage_v1alpha.RESTORING,
	}

	diskEntityId, err := ec.Create(context.Background(), diskId, disk)
	if err != nil {
		return fmt.Errorf("creating disk entity: %w", err)
	}

	ctx.Info("Created disk entity: %s", diskEntityId)

	// Move the volume directory back
	volId := meta.VolumeID
	destPath := filepath.Join(diskDataPath, "volumes", volId)

	if err := os.Rename(entry.Path, destPath); err != nil {
		// Clean up the disk entity we just created
		if _, derr := eac.Delete(context.Background(), string(diskEntityId)); derr != nil {
			ctx.Warn("Failed to clean up disk entity: %v", derr)
		}
		return fmt.Errorf("moving volume back to %s: %w", destPath, err)
	}

	// Deferred rollback: if we don't reach the final commit, move the
	// volume back to deleted-volumes (with its metadata intact) and clean
	// up any entities we created.
	committed := false
	defer func() {
		if committed {
			return
		}
		if rerr := os.Rename(destPath, entry.Path); rerr != nil {
			ctx.Warn("Failed to move volume back to deleted-volumes: %v", rerr)
		}
		if _, derr := eac.Delete(context.Background(), string(diskEntityId)); derr != nil {
			ctx.Warn("Failed to clean up disk entity: %v", derr)
		}
	}()

	// Find the node ID
	nodeId, err := resolver.findNodeId(context.Background())
	if err != nil {
		ctx.Warn("Failed to find node ID, using stored value: %v", err)
		nodeId = meta.NodeID.Id()
	}

	imagePath := filepath.Join(destPath, "disk.img")

	// Verify the disk image actually exists before creating entities
	if _, err := os.Stat(imagePath); err != nil {
		return fmt.Errorf("disk image not found at %s — deleted volume may be corrupted", imagePath)
	}

	// Create disk_volume entity
	volEntityId := entity.Id("disk_volume/" + volId)
	vol := &storage_v1alpha.DiskVolume{
		Name:         meta.DiskName,
		DiskId:       diskEntityId,
		VolumeId:     volId,
		SizeGb:       meta.SizeGb,
		Filesystem:   filesystem,
		VolumeMode:   storage_v1alpha.DiskVolumeVolumeMode(meta.VolumeMode),
		DesiredState: storage_v1alpha.DV_PRESENT,
		// Start PENDING, not READY. The runner's DiskVolumeController drives
		// the restored volume through the same mount-then-READY ordering as a
		// fresh create (skipping the reimage, since disk.img is already in
		// place). Advertising DV_READY here would let a lease bind against a
		// volume that isn't registered in the runner's in-memory state yet,
		// which lands the lease in a terminal FAILED (MIR-1469).
		ActualState: storage_v1alpha.DV_PENDING,
		ImagePath:   imagePath,
		NodeId:      nodeId,
	}

	_, err = eac.Create(context.Background(), entity.New(
		entity.DBId, volEntityId,
		vol.Encode,
	).Attrs())
	if err != nil {
		return fmt.Errorf("creating disk_volume entity: %w", err)
	}

	// Transition disk to PROVISIONING (not PROVISIONED). The DiskController
	// promotes it to PROVISIONED only once the disk_volume actually reaches
	// DV_READY — i.e. after the volume is registered and mounted on the node —
	// so the disk never reports "provisioned" (and thus leasable) ahead of a
	// real mount.
	_, err = eac.Patch(context.Background(), []entity.Attr{
		entity.Ref(entity.DBId, diskEntityId),
		entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusProvisioningId),
		entity.String(storage_v1alpha.DiskVolumeIdId, volId),
	}, 0)
	if err != nil {
		// Also clean up the volume entity
		if _, derr := eac.Delete(context.Background(), string(volEntityId)); derr != nil {
			ctx.Warn("Failed to clean up disk_volume entity: %v", derr)
		}
		return fmt.Errorf("updating disk to provisioned: %w", err)
	}

	// All entities committed — remove leftover metadata and disarm rollback
	os.Remove(filepath.Join(destPath, "metadata.json"))
	committed = true

	ctx.Info("Disk restored successfully")
	ctx.Info("  Disk ID:   %s", diskEntityId)
	ctx.Info("  Volume ID: %s", volId)
	ctx.Info("  Image:     %s", imagePath)

	return nil
}
