package commands

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/clientconfig"
)

// InfoConfig shows configuration file information
func InfoConfig(ctx *Context, opts struct {
	ConfigCentric
}) error {
	configPath := clientconfig.GetActiveConfigPath()
	configDir := clientconfig.GetConfigDirPath()

	cfg, err := opts.LoadConfig()
	if err != nil && err != clientconfig.ErrNoConfig {
		return err
	}

	ctx.Printf("%s\n", infoBold.Render("Configuration"))
	ctx.Printf("%s\n", infoGray.Render("============="))
	ctx.Printf("%s  %s\n", infoLabel.Render("Config file:"), configPath)
	ctx.Printf("%s  %s\n", infoLabel.Render("Config dir:"), configDir)
	ctx.Printf("%s  %s\n", infoLabel.Render("Format:"), "YAML")

	if cfg != nil {
		if leafConfigs := cfg.GetLeafConfigNames(); len(leafConfigs) > 0 {
			formatted := make([]string, len(leafConfigs))
			for i, name := range leafConfigs {
				formatted[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(fmt.Sprintf("clientconfig.d/%s.yaml", name))
			}
			ctx.Printf("%s  %s\n", infoLabel.Render("Leaf configs:"), strings.Join(formatted, ", "))
		}
	}

	return nil
}
