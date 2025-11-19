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
			return nil, "", nil
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
	FormatOptions
	ConfigCentric
}) error {
	// Get the config file paths
	configPath := clientconfig.GetActiveConfigPath()
	configDir := clientconfig.GetConfigDirPath()

	// Load config to get some stats
	cfg, err := opts.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		return err
	}

	// Prepare structured data
	type ConfigInfo struct {
		MainConfigFile  string   `json:"main_config_file"`
		ConfigDirectory string   `json:"config_directory"`
		Format          string   `json:"format"`
		ActiveCluster   string   `json:"active_cluster,omitempty"`
		ClusterCount    int      `json:"cluster_count"`
		IdentityCount   int      `json:"identity_count"`
		LeafConfigs     []string `json:"leaf_configs,omitempty"`
	}

	info := ConfigInfo{
		MainConfigFile:  configPath,
		ConfigDirectory: configDir,
		Format:          "YAML",
	}

	if cfg != nil {
		info.ActiveCluster = cfg.ActiveCluster()

		// Count clusters
		clusterCount := 0
		cfg.IterateClusters(func(name string, ccfg *clientconfig.ClusterConfig) error {
			clusterCount++
			return nil
		})
		info.ClusterCount = clusterCount

		// Count identities
		info.IdentityCount = len(cfg.GetIdentityNames())

		// List leaf config files
		if leafConfigs := cfg.GetLeafConfigNames(); len(leafConfigs) > 0 {
			// Create a new slice to avoid mutating the original
			formatted := make([]string, len(leafConfigs))
			for i, name := range leafConfigs {
				formatted[i] = fmt.Sprintf("clientconfig.d/%s.yaml", name)
			}
			info.LeafConfigs = formatted
		}
	}

	// Output based on format
	if opts.IsJSON() {
		return PrintJSON(info)
	}

	// Text output
	ctx.Printf("Miren Configuration\n")
	ctx.Printf("===================\n\n")

	ctx.Printf("Configuration Files:\n")
	ctx.Printf("  Main config:    %s\n", configPath)
	ctx.Printf("  Config dir:     %s\n", configDir)
	ctx.Printf("  Format:         %s\n", "YAML")
	ctx.Printf("\n")

	if cfg != nil {
		ctx.Printf("Current State:\n")
		if info.ActiveCluster != "" {
			ctx.Printf("  Active cluster: %s\n", info.ActiveCluster)
		}
		ctx.Printf("  Clusters:       %d configured\n", info.ClusterCount)
		ctx.Printf("  Identities:     %d configured\n", info.IdentityCount)

		if len(info.LeafConfigs) > 0 {
			ctx.Printf("\nCluster Configs:\n")
			for _, leaf := range info.LeafConfigs {
				ctx.Printf("  - %s\n", leaf)
			}
		}
	} else {
		ctx.Printf("\nNo configuration found.\n")
		ctx.Printf("\nGet started with:\n")
		ctx.Printf("  miren login        # Set up an identity\n")
		ctx.Printf("  miren cluster add  # Add a cluster\n")
	}

	ctx.Printf("\nTip: Use 'miren cluster list' to see configured clusters\n")

	return nil
}
