package commands

import (
	"time"

	"miren.dev/runtime/api/disk/disk_v1alpha"
)

// DiskListDeleted lists disks whose data is still recoverable, asking the
// server rather than reading its data directory, so it works from anywhere.
func DiskListDeleted(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient(diskBackupService)
	if err != nil {
		return err
	}

	res, err := disk_v1alpha.NewDiskBackupClient(client).ListDeleted(ctx)
	if err != nil {
		return err
	}

	disks := res.Disks()
	retentionDays := int(res.RetentionDays())

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
		for _, d := range disks {
			items = append(items, deletedDiskJSON{
				DiskName:      d.DiskName(),
				VolumeID:      d.VolumeId(),
				SizeGB:        d.SizeGb(),
				Filesystem:    d.Filesystem(),
				DeletedAt:     rfc3339(d.HasDeletedAt(), d.DeletedAt()),
				ExpiresAt:     rfc3339(d.HasExpiresAt(), d.ExpiresAt()),
				RetentionDays: retentionDays,
			})
		}

		return PrintJSON(items)
	}

	if len(disks) == 0 {
		ctx.Info("No deleted disks found")
		return nil
	}

	ctx.Info("Deleted disks available for recovery:")
	ctx.Info("")

	for _, d := range disks {
		deletedAt := goTime(d.HasDeletedAt(), d.DeletedAt())
		expiresAt := goTime(d.HasExpiresAt(), d.ExpiresAt())

		ctx.Info("Name: %s", d.DiskName())
		ctx.Info("  Volume ID:  %s", d.VolumeId())
		ctx.Info("  Size:       %d GB", d.SizeGb())
		ctx.Info("  Filesystem: %s", d.Filesystem())

		if deletedAt.IsZero() {
			ctx.Info("  Deleted:    unknown")
		} else {
			age := time.Since(deletedAt)
			ctx.Info("  Deleted:    %s (%s ago)", deletedAt.Format(time.RFC3339), age.Truncate(time.Minute))
		}

		switch {
		case expiresAt.IsZero():
			ctx.Info("  Expires in: unknown")
		case time.Until(expiresAt) > 0:
			ctx.Info("  Expires in: %s", time.Until(expiresAt).Truncate(time.Minute))
		default:
			ctx.Info("  Expires in: imminent (past retention period)")
		}
		ctx.Info("")
	}

	ctx.Info("To recover: miren disk undelete --name <disk-name>")

	return nil
}
