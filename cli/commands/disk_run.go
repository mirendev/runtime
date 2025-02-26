package commands

import (
	"miren.dev/runtime/disk"
	"miren.dev/runtime/lsvd"
)

func DiskRun(ctx *Context, opts struct {
	DataDir string `long:"data" description:"Directory containing disk data"`
	Dir     string `short:"d" long:"dir" description:"Directory to maintain disk access info"`
	Name    string `short:"n" long:"name" description:"Name of the disk"`
}) error {
	sa := &lsvd.LocalFileAccess{Dir: opts.DataDir, Log: ctx.Log}

	vi, err := sa.GetVolumeInfo(ctx, opts.Name)
	if err != nil {
		ctx.Info("Error loading volume info on %s", opts.Name)
		return err
	}

	ctx.Log.Info("Starting volume", "name", opts.Name, "size", vi.Size.Short())

	runner, err := disk.NewRunner(sa, opts.Dir, ctx.Log)
	if err != nil {
		return err
	}

	return runner.Run(ctx, opts.Name)
}
