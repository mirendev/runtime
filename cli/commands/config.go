package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
)

type ConfigCentric struct {
	Config  string `long:"config" description:"Path to the config file"`
	Cluster string `short:"C" long:"cluster" description:"Cluster name" env:"MIREN_CLUSTER"`

	cfg *clientconfig.Config
}

var ErrNoConfig = errors.New("no cluster config")

// RequestedCluster returns the cluster the user explicitly asked for with -C or
// MIREN_CLUSTER, or "" when they didn't ask for one. Failing to honor an
// explicit request has to be fatal; falling back to a default is only
// acceptable when no particular cluster was named.
func (c *ConfigCentric) RequestedCluster() string { return c.Cluster }

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
		if errors.Is(err, clientconfig.ErrNoConfig) && c.Cluster != "" {
			c.cfg = clientconfig.NewConfig()
			return c.cfg, nil
		}
		return nil, err
	}

	if cfg == nil {
		if c.Cluster != "" {
			c.cfg = clientconfig.NewConfig()
			return c.cfg, nil
		}
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

	if c.Cluster != "" {
		// Cluster came from -C or MIREN_CLUSTER (env:"MIREN_CLUSTER" on the field).
		// Prefer an exact name match (including any literal ";sha1:..." suffix),
		// otherwise parse as "address;sha1:fingerprint" and probe.
		cc, err := cfg.GetCluster(c.Cluster)
		if err == nil && cc != nil {
			return cc, c.Cluster, nil
		}

		address := c.Cluster
		var fingerprint string
		if idx := strings.Index(c.Cluster, ";"); idx >= 0 {
			address = c.Cluster[:idx]
			fingerprint = c.Cluster[idx+1:]
		}

		cc, name, err := setupClusterFromAddress(cfg, address, fingerprint)
		if err != nil {
			return nil, "", fmt.Errorf("cluster %q: failed to connect to %q: %w", c.Cluster, address, err)
		}
		return cc, name, nil
	}

	// Check per-app state if we're in an app directory.
	if ac, err := appconfig.LoadAppConfig(); err != nil {
		printConfigWarning(err)
	} else if ac != nil && ac.Name != "" {
		state, err := appconfig.LoadAppState(ac.Name)
		if err == nil && state != nil && state.Cluster != "" {
			cc, err := cfg.GetCluster(state.Cluster)
			if err == nil && cc != nil {
				return cc, state.Cluster, nil
			}
			// State references unknown cluster; fall through to global default.
		}
	}

	name = cfg.ActiveCluster()
	if name == "" {
		return nil, "", nil
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

// checkFingerprint verifies that actual matches expected.
// The expected value may carry a "sha1:" prefix, which is stripped before comparison.
// An empty expected string means no verification (returns nil).
// Comparison is case-insensitive.
func checkFingerprint(expected, actual string) error {
	if expected == "" {
		return nil
	}
	expected = strings.TrimPrefix(expected, "sha1:")
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("TLS certificate fingerprint mismatch: got %s, expected %s", actual, expected)
	}
	return nil
}

// setupClusterFromAddress probes an address via QUIC to extract its TLS certificate,
// then creates an in-memory cluster config and adds it to the config.
// If expectedFingerprint is non-empty, the probed certificate's SHA1 fingerprint
// is verified against it (with optional "sha1:" prefix).
func setupClusterFromAddress(cfg *clientconfig.Config, address, expectedFingerprint string) (*clientconfig.ClusterConfig, string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cert, fingerprint, err := extractTLSCertificate(ctx, address)
	if err != nil {
		return nil, "", err
	}

	if err := checkFingerprint(expectedFingerprint, fingerprint); err != nil {
		return nil, "", err
	}

	cc := &clientconfig.ClusterConfig{
		Hostname: address,
		CACert:   cert,
	}

	leafData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			address: cc,
		},
	}
	cfg.SetLeafConfig(address, leafData)

	return cc, address, nil
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
		ClusterConfigs  []string `json:"cluster_configs,omitempty"`
		IdentityConfigs []string `json:"identity_configs,omitempty"`
		KeyConfigs      []string `json:"key_configs,omitempty"`
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

		// Categorize leaf config files by type
		if leafConfigs := cfg.GetLeafConfigNames(); len(leafConfigs) > 0 {
			for _, name := range leafConfigs {
				filename := fmt.Sprintf("clientconfig.d/%s.yaml", name)
				switch {
				case strings.HasPrefix(name, "identity-"):
					info.IdentityConfigs = append(info.IdentityConfigs, filename)
				case strings.HasPrefix(name, "key-"):
					info.KeyConfigs = append(info.KeyConfigs, filename)
				default:
					info.ClusterConfigs = append(info.ClusterConfigs, filename)
				}
			}
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

		if len(info.ClusterConfigs) > 0 {
			ctx.Printf("\nCluster Configs:\n")
			for _, cfg := range info.ClusterConfigs {
				ctx.Printf("  - %s\n", cfg)
			}
		}

		if len(info.IdentityConfigs) > 0 {
			ctx.Printf("\nIdentity Configs:\n")
			for _, cfg := range info.IdentityConfigs {
				ctx.Printf("  - %s\n", cfg)
			}
		}

		if len(info.KeyConfigs) > 0 {
			ctx.Printf("\nKey Configs:\n")
			for _, cfg := range info.KeyConfigs {
				ctx.Printf("  - %s\n", cfg)
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
