package commands

import (
	"errors"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/mapx"
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

func (c *ConfigCentric) LoadCluster() (*clientconfig.ClusterConfig, error) {
	cfg, err := c.LoadConfig()
	if err != nil {
		return nil, err
	}

	if c.Cluster == "" {
		return cfg.GetActiveCluster()
	}

	return cfg.GetCluster(c.Cluster)
}

func ConfigInfo(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		return err
	}

	for name, ccfg := range mapx.StableOrder(cfg.Clusters) {
		prefix := " "
		if opts.Cluster != "" {
			if name == opts.Cluster {
				prefix = "*"
			}
		} else if name == cfg.ActiveCluster {
			prefix = "*"
		}
		ctx.Printf("%s %s at %s\n", prefix, name, ccfg.Hostname)
	}

	return nil
}
