package commands

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/components/diskio"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// DebugDiskCreate creates a new disk entity for testing
func DebugDiskCreate(ctx *Context, opts struct {
	ConfigCentric
	Name       string `short:"n" long:"name" description:"Name for the disk" required:"true"`
	Size       int    `short:"s" long:"size" description:"Size of disk in GB" default:"10"`
	Filesystem string `short:"f" long:"filesystem" description:"Filesystem type (ext4, xfs, btrfs)" default:"ext4"`
	CreatedBy  string `short:"c" long:"created-by" description:"Creator ID for the disk"`
	RemoteOnly bool   `short:"r" long:"remote-only" description:"Store disk only in remote storage (no local replica)"`
	VolumeID   string `short:"V" long:"volume-id" description:"Attach to existing volume instead of creating new one"`
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)

	// Generate disk ID (format: disk-<base58>)
	diskId := idgen.GenNS("disk")

	// Determine filesystem type
	var fs storage_v1alpha.DiskFilesystem
	switch opts.Filesystem {
	case "ext4":
		fs = storage_v1alpha.EXT4
	case "xfs":
		fs = storage_v1alpha.XFS
	case "btrfs":
		fs = storage_v1alpha.BTRFS
	default:
		return fmt.Errorf("unsupported filesystem type: %s (use ext4, xfs, or btrfs)", opts.Filesystem)
	}

	// Create disk entity
	disk := &storage_v1alpha.Disk{
		Name:       opts.Name,
		SizeGb:     int64(opts.Size),
		Filesystem: fs,
		Status:     storage_v1alpha.PROVISIONING,
		RemoteOnly: opts.RemoteOnly,
	}

	// Set volume ID if attaching to existing volume
	if opts.VolumeID != "" {
		disk.VolumeId = opts.VolumeID
	}

	// Set created by if provided
	if opts.CreatedBy != "" {
		disk.CreatedBy = entity.Id(opts.CreatedBy)
	}

	// Create the disk entity
	if opts.VolumeID != "" {
		ctx.Info("Creating disk entity (attaching to existing volume)...")
	} else {
		ctx.Info("Creating disk entity (provisioning new volume)...")
	}
	result, err := ec.Create(ctx, diskId, disk)
	if err != nil {
		return fmt.Errorf("failed to create disk entity: %w", err)
	}

	ctx.Info("Disk created successfully")
	ctx.Info("Disk ID: %s", result)
	ctx.Info("Name: %s", opts.Name)
	ctx.Info("Size: %d GB", opts.Size)
	ctx.Info("Filesystem: %s", opts.Filesystem)
	ctx.Info("Remote Only: %v", opts.RemoteOnly)
	if opts.VolumeID != "" {
		ctx.Info("Volume ID (attach): %s", opts.VolumeID)
	}
	if opts.CreatedBy != "" {
		ctx.Info("Created By: %s", opts.CreatedBy)
	}

	return nil
}

// DebugDiskAdopt creates a disk bound to an existing miren.cloud volume, such
// as one written by another (possibly lost) cluster in the same organization.
// Its first mount rebuilds the image from the cloud's protected checkpoint and
// log tail and continues that volume's stream.
//
// --takeover declares the volume's current writer gone: the mount breaks a
// cloud lease granted before now, fencing that writer, instead of waiting for
// a release that will never come. Use it only when the old cluster is lost.
func DebugDiskAdopt(ctx *Context, opts struct {
	ConfigCentric
	Name        string `short:"n" long:"name" description:"Name for the new disk" required:"true"`
	CloudVolume string `long:"cloud-volume" description:"miren.cloud volume ID to adopt (vol-...)" required:"true"`
	Size        int    `short:"s" long:"size" description:"Size of the cloud volume in GB" required:"true"`
	Filesystem  string `short:"f" long:"filesystem" description:"Filesystem type the volume holds" default:"ext4"`
	Node        string `long:"node" description:"Node the volume lives on" default:"node/miren"`
	Takeover    bool   `long:"takeover" description:"The volume's previous writer is lost: break any cloud lease granted before now, fencing it"`
}) error {
	var takeover string
	if opts.Takeover {
		takeover = time.Now().UTC().Format(time.RFC3339Nano)
	}
	diskId, err := createAcceleratorDisk(ctx, opts.Name, opts.Size, opts.Filesystem, opts.Node, func(v *storage_v1alpha.DiskVolume) {
		v.CloudVolumeId = opts.CloudVolume
		v.LeaseTakeoverBefore = takeover
	})
	if err != nil {
		return err
	}
	ctx.Info("Disk %s (%s) adopts cloud volume %s on %s", opts.Name, diskId, opts.CloudVolume, opts.Node)
	if takeover != "" {
		ctx.Info("Its mount will take over any cloud lease granted before %s", takeover)
	}
	return nil
}

// DebugDiskRewind creates a new disk holding another accelerator cloud
// volume's contents as of a point in time. Its first mount rebuilds the image
// from that volume's checkpoint and log up to the time, crash-consistently,
// and publishes it as the new disk's own first checkpoint. The source volume
// is only read.
func DebugDiskRewind(ctx *Context, opts struct {
	ConfigCentric
	Name       string `short:"n" long:"name" description:"Name for the new disk" required:"true"`
	From       string `long:"from" description:"miren.cloud accelerator volume ID to restore from (vol-...)" required:"true"`
	At         string `long:"at" description:"Point in time to restore to (RFC 3339); omit for the latest mirrored state"`
	Size       int    `short:"s" long:"size" description:"Size of the source volume in GB" required:"true"`
	Filesystem string `short:"f" long:"filesystem" description:"Filesystem type the volume holds" default:"ext4"`
	Node       string `long:"node" description:"Node to create the disk on" default:"node/miren"`
}) error {
	var until string
	if opts.At != "" {
		at, err := time.Parse(time.RFC3339Nano, opts.At)
		if err != nil {
			return fmt.Errorf("--at: %w", err)
		}
		until = diskio.LabelAt(at)
	}
	diskId, err := createAcceleratorDisk(ctx, opts.Name, opts.Size, opts.Filesystem, opts.Node, func(v *storage_v1alpha.DiskVolume) {
		v.RestoreFromVolume = opts.From
		v.RestoreUntil = until
	})
	if err != nil {
		return err
	}
	ctx.Info("Disk %s (%s) will be restored from %s as of %s (label %s)", opts.Name, diskId, opts.From, opts.At, until)
	return nil
}

func createAcceleratorDisk(ctx *Context, name string, sizeGB int, filesystem, node string, bind func(*storage_v1alpha.DiskVolume)) (entity.Id, error) {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return "", err
	}
	ec := entityserver.NewClient(ctx.Log, entityserver_v1alpha.NewEntityAccessClient(client))

	var fs storage_v1alpha.DiskFilesystem
	switch filesystem {
	case "ext4":
		fs = storage_v1alpha.EXT4
	case "xfs":
		fs = storage_v1alpha.XFS
	case "btrfs":
		fs = storage_v1alpha.BTRFS
	default:
		return "", fmt.Errorf("unsupported filesystem type: %s", filesystem)
	}
	// RESTORING keeps the disk controller from provisioning a fresh volume
	// until the bound one exists; the volume must reference an existing disk.
	diskId, err := ec.Create(ctx, idgen.GenNS("disk"), &storage_v1alpha.Disk{
		Name:       name,
		SizeGb:     int64(sizeGB),
		Filesystem: fs,
		Status:     storage_v1alpha.RESTORING,
		Mode:       storage_v1alpha.ACCELERATOR,
	})
	if err != nil {
		return "", fmt.Errorf("creating disk: %w", err)
	}
	volume := &storage_v1alpha.DiskVolume{
		Name:         name,
		DiskId:       diskId,
		SizeGb:       int64(sizeGB),
		Filesystem:   filesystem,
		VolumeMode:   storage_v1alpha.VM_ACCELERATOR,
		DesiredState: storage_v1alpha.DV_PRESENT,
		ActualState:  storage_v1alpha.DV_PENDING,
		NodeId:       entity.Id(node),
	}
	bind(volume)
	if _, err := ec.Create(ctx, idgen.GenNS("disk-vol"), volume); err != nil {
		return "", fmt.Errorf("creating disk_volume: %w", err)
	}
	if err := ec.UpdateAttrs(ctx, diskId, entity.Ref(storage_v1alpha.DiskStatusId, storage_v1alpha.DiskStatusProvisioningId)); err != nil {
		return "", fmt.Errorf("releasing disk to provisioning: %w", err)
	}
	return diskId, nil
}

// DebugDiskList lists all disk entities
func DebugDiskList(ctx *Context, opts struct {
	ConfigCentric
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// List disk entities
	ref := entity.Ref(entity.EntityKind, storage_v1alpha.KindDisk)
	results, err := eac.List(ctx, ref)
	if err != nil {
		return fmt.Errorf("failed to list disk entities: %w", err)
	}

	entities := results.Values()
	if len(entities) == 0 {
		ctx.Info("No disk entities found")
		return nil
	}

	ctx.Info("Disk entities:")
	ctx.Info("")

	for _, e := range entities {
		// Decode disk entity
		var disk storage_v1alpha.Disk
		disk.Decode(e.Entity())

		ctx.Info("ID: %s", disk.ID)
		ctx.Info("  Name: %s", disk.Name)
		ctx.Info("  Size: %d GB", disk.SizeGb)
		ctx.Info("  Filesystem: %s", disk.Filesystem)
		ctx.Info("  Status: %s", disk.Status)
		ctx.Info("  Remote Only: %v", disk.RemoteOnly)
		if disk.Mode != "" {
			ctx.Info("  Mode: %s", disk.Mode)
		}
		if disk.CreatedBy != "" {
			ctx.Info("  Created By: %s", disk.CreatedBy)
		}
		if disk.VolumeId != "" {
			ctx.Info("  Volume ID: %s", disk.VolumeId)
		}
		ctx.Info("")
	}

	return nil
}

// DebugDiskDelete deletes a disk entity
func DebugDiskDelete(ctx *Context, opts struct {
	ConfigCentric
	ID string `short:"i" long:"id" description:"Disk ID to delete" required:"true"`
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(slog.Default(), eac)

	diskId := entity.Id(opts.ID)

	// First, update the disk status to DELETING
	ctx.Info("Marking disk for deletion...")
	disk := &storage_v1alpha.Disk{
		Status: storage_v1alpha.DELETING,
	}

	err = ec.UpdateAttrs(context.Background(), diskId, disk.Encode)
	if err != nil {
		// If update fails, try direct deletion
		ctx.Warn("Failed to update disk status, attempting direct deletion: %v", err)
		err = ec.Delete(context.Background(), diskId)
		if err != nil {
			return fmt.Errorf("failed to delete disk entity: %w", err)
		}
	}

	ctx.Info("Disk marked for deletion: %s", diskId)
	ctx.Info("The disk controller will handle cleanup of the underlying volume")

	return nil
}

// DebugDiskStatus shows the status of a specific disk
func DebugDiskStatus(ctx *Context, opts struct {
	ConfigCentric
	ID string `short:"i" long:"id" description:"Disk ID to check" required:"true"`
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// Get the disk entity
	diskId := entity.Id(opts.ID)
	result, err := eac.Get(context.Background(), string(diskId))
	if err != nil {
		return fmt.Errorf("failed to get disk entity: %w", err)
	}

	// Decode disk entity
	var disk storage_v1alpha.Disk
	if result.Entity() != nil {
		disk.Decode(result.Entity().Entity())
	} else {
		return fmt.Errorf("disk entity not found: %s", diskId)
	}

	// Display disk information
	ctx.Info("Disk Details:")
	ctx.Info("  ID: %s", disk.ID)
	ctx.Info("  Name: %s", disk.Name)
	ctx.Info("  Size: %d GB", disk.SizeGb)
	ctx.Info("  Filesystem: %s", disk.Filesystem)
	ctx.Info("  Status: %s", disk.Status)
	ctx.Info("  Remote Only: %v", disk.RemoteOnly)

	if disk.Mode != "" {
		ctx.Info("  Mode: %s", disk.Mode)
	}

	if disk.CreatedBy != "" {
		ctx.Info("  Created By: %s", disk.CreatedBy)
	}

	if disk.VolumeId != "" {
		ctx.Info("  Volume ID: %s", disk.VolumeId)
	}

	return nil
}

// DebugDiskMounts lists all mounted runtime-managed disks by reading /proc/mounts
func DebugDiskMounts(ctx *Context, opts struct{}) error {
	file, err := os.Open("/proc/mounts")
	if err != nil {
		return fmt.Errorf("failed to open /proc/mounts: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)

		if len(fields) < 3 {
			continue
		}

		device := fields[0]
		mountPoint := fields[1]
		fsType := fields[2]

		// Only show runtime-managed disk mounts (loop devices under /var/lib/miren/disks)
		if !strings.HasPrefix(mountPoint, "/var/lib/miren/disks") {
			continue
		}

		ctx.Info("%s on %s type %s", device, mountPoint, fsType)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading /proc/mounts: %w", err)
	}

	return nil
}
