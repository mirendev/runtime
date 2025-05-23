package commands

import (
	"errors"

	"miren.dev/runtime/clientconfig"
)

func ConfigLoad(ctx *Context, opts struct {
	Config    string `long:"config" description:"Path to the config file to update"`
	Input     string `short:"i" long:"input" description:"Path to the input config file to add" required:"true"`
	Force     bool   `short:"f" long:"force" description:"Force the update"`
	SetActive bool   `short:"a" long:"set-active" description:"Set the active cluster"`
}) error {
	var (
		cfg *clientconfig.Config
		err error
	)

	if opts.Config != "" {
		cfg, err = clientconfig.LoadConfigFrom(opts.Config)
	} else {
		cfg, err = clientconfig.LoadConfig()
	}

	if err != nil {
		if errors.Is(err, clientconfig.ErrNoConfig) {
			cfg = clientconfig.NewConfig()
		} else {
			return err
		}
	}

	input, err := clientconfig.LoadConfigFrom(opts.Input)
	if err != nil {
		return err
	}

	err = cfg.Merge(input, opts.SetActive, opts.Force)
	if err != nil {
		return err
	}

	err = cfg.Save()
	if err != nil {
		return err
	}

	for name, cluster := range input.Clusters {
		ctx.Printf("Added cluster %s: %s\n", name, cluster.Hostname)
	}

	return nil
}
