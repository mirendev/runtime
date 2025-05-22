package commands

import (
	"miren.dev/runtime/components/coordinate"
)

func AuthGenerate(ctx *Context, opts struct {
	DataPath   string `short:"d" long:"data-path" description:"Data path" default:"/var/lib/miren"`
	ConfigPath string `short:"c" long:"config-path" description:"Path to the config file" default:"clientconfig.yaml"`
	Name       string `short:"n" long:"name" description:"Name of the client certificate" require:"true"`
}) error {
	co := coordinate.NewCoordinator(ctx.Log, coordinate.CoordinatorConfig{
		DataPath: opts.DataPath,
	})

	err := co.LoadCA(ctx)
	if err != nil {
		return err
	}

	err = co.LoadAPICert(ctx)
	if err != nil {
		return err
	}

	lcfg, err := co.NamedConfig(opts.Name)
	if err != nil {
		return err
	}

	return lcfg.SaveTo(opts.ConfigPath)
}
