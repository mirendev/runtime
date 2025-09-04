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

	// Merge clusters from input config
	err = input.IterateClusters(func(name string, cluster *clientconfig.ClusterConfig) error {
		if cfg.HasCluster(name) && !opts.Force {
			return errors.New("cluster \"" + name + "\" already exists in current config, use --force to overwrite")
		}
		cfg.SetCluster(name, cluster)
		return nil
	})
	if err != nil {
		return err
	}

	// Merge identities from input config
	for _, identityName := range input.GetIdentityNames() {
		if cfg.HasIdentity(identityName) && !opts.Force {
			return errors.New("identity \"" + identityName + "\" already exists in current config, use --force to overwrite")
		}
		identity, err := input.GetIdentity(identityName)
		if err != nil {
			return err
		}
		cfg.SetIdentity(identityName, identity)
	}

	// Update active cluster if requested and input config has one
	if cfg.ActiveCluster() == "" || (opts.SetActive && input.ActiveCluster() != "") {
		if input.ActiveCluster() != "" {
			err = cfg.SetActiveCluster(input.ActiveCluster())
			if err != nil {
				return err
			}
		}
	}

	err = cfg.Save()
	if err != nil {
		return err
	}

	// Report which clusters were added
	err = input.IterateClusters(func(name string, cluster *clientconfig.ClusterConfig) error {
		ctx.Printf("Added cluster %s: %s\n", name, cluster.Hostname)
		return nil
	})

	return err
}
