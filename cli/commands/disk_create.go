package commands

import (
	"github.com/google/uuid"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/pkg/units"
)

func DiskCreate(ctx *Context, opts struct {
	Dir  string `short:"d" long:"dir" description:"Directory to create the disk in"`
	Name string `short:"n" long:"name" description:"Name of the disk"`
	Size string `short:"s" long:"size" description:"Size of the disk (use si units as suffix)"`
}) error {
	sa := &lsvd.LocalFileAccess{Dir: opts.Dir, Log: ctx.Log}

	sz, err := units.ParseData(opts.Size)
	if err != nil {
		return err
	}

	vi, err := sa.GetVolumeInfo(ctx, opts.Name)
	if err == nil {
		ctx.Info("Disk already exists. name=%s, size=%s", opts.Name, vi.Size.Short())
		return nil
	}

	err = sa.InitVolume(ctx, &lsvd.VolumeInfo{
		Name: opts.Name,
		Size: sz.Bytes(),
		UUID: uuid.NewString(),
	})
	if err != nil {
		return err
	}

	ctx.Info("Disk created")

	return err
}
