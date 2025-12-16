package commands

import (
	"errors"
	"fmt"

	"miren.dev/runtime/clientconfig"
)

// DoctorConfig shows configuration file information
func DoctorConfig(ctx *Context, opts struct {
	ConfigCentric
}) error {
	configPath := clientconfig.GetActiveConfigPath()
	configDir := clientconfig.GetConfigDirPath()

	cfg, err := opts.LoadConfig()
	if err != nil && !errors.Is(err, clientconfig.ErrNoConfig) {
		return err
	}

	ctx.Printf("%s\n", infoBold.Render("Configuration"))
	ctx.Printf("%s\n", infoGray.Render("============="))
	ctx.Printf("%s  %s\n", infoLabel.Render("Config file:"), configPath)
	ctx.Printf("%s  %s\n", infoLabel.Render("Config dir:"), configDir)
	ctx.Printf("%s  %s\n", infoLabel.Render("Format:"), "YAML")

	if cfg != nil {
		// Show all configured clusters
		activeCluster := cfg.ActiveCluster()
		ctx.Printf("\n%s\n", infoLabel.Render("Clusters:"))
		cfg.IterateClusters(func(name string, cluster *clientconfig.ClusterConfig) error {
			if name == activeCluster {
				ctx.Printf("  %s %s\n", infoGreen.Render(name), infoGray.Render("(active)"))
			} else {
				ctx.Printf("  %s\n", name)
			}
			ctx.Printf("    %s %s\n", infoGray.Render("Address:"), cluster.Hostname)
			return nil
		})

		if leafConfigs := cfg.GetLeafConfigNames(); len(leafConfigs) > 0 {
			ctx.Printf("\n%s\n", infoLabel.Render("Leaf configs:"))
			for _, name := range leafConfigs {
				filename := fmt.Sprintf("clientconfig.d/%s.yaml", name)
				ctx.Printf("  %s\n", infoGray.Render(filename))
			}
		}
	}

	return nil
}
