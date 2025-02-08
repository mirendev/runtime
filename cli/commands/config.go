package commands

import (
	"errors"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/mapx"
)

type ConfigCentric struct {
	Config  string `long:"config" description:"Path to the config file"`
	Cluster string `long:"cluster" description:"Cluster name"`

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

	return clientconfig.SaveConfig(c.cfg)
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
		if name == cfg.ActiveCluster {
			ctx.Printf("* %s at %s\n", name, ccfg.Hostname)
		} else {
			ctx.Printf("  %s as %s\n", name, ccfg.Hostname)
		}
	}

	return nil
}
