package commands

import (
	"errors"
	"fmt"

	"miren.dev/runtime/clientconfig"
)

type ConfigCentric struct {
	Config  string `long:"config" description:"Path to the config file"`
	Cluster string `short:"C" long:"cluster" description:"Cluster name"`

	cfg *clientconfig.Config
}

var ErrNoConfig = errors.New("no cluster config")

func (c *ConfigCentric) LoadConfig() (*clientconfig.Config, error) {
	if c.cfg != nil {
		return c.cfg, nil
	}

	var (
		cfg *clientconfig.Config
		err error
	)

	if c.Config != "" {
		cfg, err = clientconfig.LoadConfigFrom(c.Config)
	} else {
		cfg, err = clientconfig.LoadConfig()
	}

	if err != nil {
		return nil, err
	}

	if cfg == nil {
		return nil, ErrNoConfig
	}

	c.cfg = cfg

	return c.cfg, nil
}

func (c *ConfigCentric) SaveConfig() error {
	if c.cfg == nil {
		return nil
	}

	return c.cfg.Save()
}

func (c *ConfigCentric) LoadCluster() (*clientconfig.ClusterConfig, string, error) {
	cfg, err := c.LoadConfig()
	if err != nil {
		return nil, "", err
	}

	var (
		name string
	)

	if c.Cluster == "" {
		name = cfg.ActiveCluster()
		if name == "" {
			return nil, "", fmt.Errorf("no cluster specified and no active cluster set")
		}
	} else {
		name = c.Cluster
	}

	cc, err := cfg.GetCluster(name)
	if err != nil {
		return nil, "", err
	}

	if cc == nil {
		return nil, "", ErrNoConfig
	}

	return cc, name, nil
}

func ConfigInfo(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		return err
	}

	return cfg.IterateClusters(func(name string, ccfg *clientconfig.ClusterConfig) error {
		prefix := " "
		if opts.Cluster != "" {
			if name == opts.Cluster {
				prefix = "*"
			}
		} else if name == cfg.ActiveCluster() {
			prefix = "*"
		}
		ctx.Printf("%s %s at %s\n", prefix, name, ccfg.Hostname)
		return nil
	})
}
