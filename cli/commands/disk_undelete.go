package commands

import (
	"miren.dev/runtime/api/disk/disk_v1alpha"
)

// DiskUndelete recovers a recently deleted disk, asking the server to move the
// data back and recreate the entities, so it works from anywhere.
func DiskUndelete(ctx *Context, opts struct {
	ConfigCentric
	Name     string `short:"n" long:"name" description:"Disk name to undelete" required:"true"`
	VolumeID string `short:"V" long:"volume-id" description:"Volume ID to recover (when several deleted disks share a name)"`
}) error {
	client, err := ctx.RPCClient(diskBackupService)
	if err != nil {
		return err
	}

	ctx.Info("Recovering deleted disk %q", opts.Name)

	res, err := disk_v1alpha.NewDiskBackupClient(client).Undelete(ctx, opts.Name, opts.VolumeID)
	if err != nil {
		return err
	}

	out := res.Result()
	ctx.Info("Disk restored successfully")
	if out == nil {
		return nil
	}
	ctx.Info("  Disk ID:   %s", out.DiskId())
	ctx.Info("  Volume ID: %s", out.VolumeId())
	ctx.Info("  Image:     %s", out.ImagePath())

	return nil
}
