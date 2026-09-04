package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/components/diskio"
)

// DebugDiskListDeleted reads the soft-delete holding area directly, without
// going through the server.
//
// Break-glass; `miren disk list-deleted` is the supported command.
func DebugDiskListDeleted(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
	DataPath string `long:"data-path" description:"Path to miren data directory" default:"/var/lib/miren"`
}) error {
	diskDataPath := filepath.Join(opts.DataPath, "disk-data")
	if _, err := os.Stat(diskDataPath); err != nil {
		return fmt.Errorf("data path %s not found — this command must be run on the server", diskDataPath)
	}

	entries, err := diskio.ListDeletedVolumes(diskDataPath)
	if err != nil {
		return fmt.Errorf("listing deleted volumes: %w", err)
	}

	retentionDays := diskio.DefaultDeletedVolumeGCConfig().RetentionDays

	if opts.IsJSON() {
		type deletedDiskJSON struct {
			DiskName      string `json:"disk_name"`
			VolumeID      string `json:"volume_id"`
			SizeGB        int64  `json:"size_gb"`
			Filesystem    string `json:"filesystem"`
			DeletedAt     string `json:"deleted_at"`
			ExpiresAt     string `json:"expires_at"`
			RetentionDays int    `json:"retention_days"`
		}

		var items []deletedDiskJSON
		for _, e := range entries {
			meta := e.Metadata
			expiresAt := meta.DeletedAt.Add(time.Duration(retentionDays) * 24 * time.Hour)
			items = append(items, deletedDiskJSON{
				DiskName:      meta.DiskName,
				VolumeID:      meta.VolumeID,
				SizeGB:        meta.SizeGb,
				Filesystem:    meta.Filesystem,
				DeletedAt:     meta.DeletedAt.Format(time.RFC3339),
				ExpiresAt:     expiresAt.Format(time.RFC3339),
				RetentionDays: retentionDays,
			})
		}

		return PrintJSON(items)
	}

	if len(entries) == 0 {
		ctx.Info("No deleted disks found")
		return nil
	}

	ctx.Info("Deleted disks available for recovery:")
	ctx.Info("")

	for _, e := range entries {
		meta := e.Metadata
		age := time.Since(meta.DeletedAt)
		remaining := time.Duration(retentionDays)*24*time.Hour - age

		ctx.Info("Name: %s", meta.DiskName)
		ctx.Info("  Volume ID:  %s", meta.VolumeID)
		ctx.Info("  Size:       %d GB", meta.SizeGb)
		ctx.Info("  Filesystem: %s", meta.Filesystem)
		ctx.Info("  Deleted:    %s (%s ago)", meta.DeletedAt.Format(time.RFC3339), age.Truncate(time.Minute))
		if remaining > 0 {
			ctx.Info("  Expires in: %s", remaining.Truncate(time.Minute))
		} else {
			ctx.Info("  Expires in: imminent (past retention period)")
		}
		ctx.Info("")
	}

	ctx.Info("To restore: miren disk undelete --name <disk-name>")

	return nil
}
