package commands

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// DebugDiskCreate creates a new disk entity for testing
func DebugDiskCreate(ctx *Context, opts struct {
	ConfigCentric
	Name       string `short:"n" long:"name" description:"Name for the disk" required:"true"`
	Size       int64  `short:"s" long:"size" description:"Size of disk in GB" default:"10"`
	Filesystem string `short:"f" long:"filesystem" description:"Filesystem type (ext4, xfs, btrfs)" default:"ext4"`
	CreatedBy  string `short:"c" long:"created-by" description:"Creator ID for the disk"`
	RemoteOnly bool   `short:"r" long:"remote-only" description:"Store disk only in remote storage (no local replica)"`
}) error {
	// Use the context's RPC client
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Create entity access client
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)

	// Generate disk ID
	diskId := idgen.Gen("disk")

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
		SizeGb:     opts.Size,
		Filesystem: fs,
		Status:     storage_v1alpha.PROVISIONING,
		RemoteOnly: opts.RemoteOnly,
	}

	// Set created by if provided
	if opts.CreatedBy != "" {
		disk.CreatedBy = entity.Id(opts.CreatedBy)
	}

	// Create the disk entity
	ctx.Info("Creating disk entity...")
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
	if opts.CreatedBy != "" {
		ctx.Info("Created By: %s", opts.CreatedBy)
	}

	return nil
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
		if disk.CreatedBy != "" {
			ctx.Info("  Created By: %s", disk.CreatedBy)
		}
		if disk.LsvdVolumeId != "" {
			ctx.Info("  LSVD Volume ID: %s", disk.LsvdVolumeId)
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

	// Validate disk ID format
	diskId := entity.Id(opts.ID)
	if len(opts.ID) < 5 || string(diskId)[:5] != "disk/" {
		diskId = entity.Id("disk/" + opts.ID)
	}

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

	// Validate disk ID format
	diskId := entity.Id(opts.ID)
	if len(opts.ID) < 5 || string(diskId)[:5] != "disk/" {
		diskId = entity.Id("disk/" + opts.ID)
	}

	// Get the disk entity
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

	if disk.CreatedBy != "" {
		ctx.Info("  Created By: %s", disk.CreatedBy)
	}

	if disk.LsvdVolumeId != "" {
		ctx.Info("  LSVD Volume ID: %s", disk.LsvdVolumeId)
	}

	// Check for mount path in attributes
	// Note: Custom attributes like mount_path would need to be retrieved from the entity attributes
	// This is left as a placeholder for future implementation

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

		// Only show LSVD volume devices (runtime-managed disks)
		if !strings.Contains(device, "lsvd-vol") {
			continue
		}

		ctx.Info("%s on %s type %s", device, mountPoint, fsType)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading /proc/mounts: %w", err)
	}

	return nil
}
