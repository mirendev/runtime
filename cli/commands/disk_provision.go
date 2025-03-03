package commands

import (
	"path/filepath"

	"miren.dev/runtime/disk"
)

func DiskProvision(ctx *Context, opts struct {
	DataDir string `long:"data" description:"Directory containing disk data"`
	Dir     string `short:"d" long:"dir" description:"Directory to maintain disk access info"`
	Name    string `short:"n" long:"name" description:"Name of the disk"`
}) error {
	var dp disk.Provisioner

	err := ctx.Server.Populate(&dp)
	if err != nil {
		return err
	}

	err = dp.Provision(ctx, disk.ProvisionConfig{
		Name:      opts.Name,
		DataDir:   opts.DataDir,
		AccessDir: opts.Dir,
		LogFile:   filepath.Join(opts.Dir, "disk.log"),
	})

	return err
}
