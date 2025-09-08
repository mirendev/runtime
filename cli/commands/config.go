package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
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
	FormatOptions
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		return err
	}

	// Prepare structured data
	type ClusterInfo struct {
		Name     string `json:"name"`
		Address  string `json:"address"`
		Identity string `json:"identity"`
		Active   bool   `json:"active"`
	}

	var clusters []ClusterInfo
	var rows []ui.Row
	headers := []string{"", "CLUSTER", "ADDRESS", "IDENTITY"}

	err = cfg.IterateClusters(func(name string, ccfg *clientconfig.ClusterConfig) error {
		// Determine if this is the active cluster
		isActive := false
		if opts.Cluster != "" {
			isActive = (name == opts.Cluster)
		} else {
			isActive = (name == cfg.ActiveCluster())
		}

		// Use a star for active cluster
		prefix := " "
		if isActive {
			prefix = "*"
		}

		// Get identity info if present
		identity := ccfg.Identity
		if identity == "" {
			identity = "-"
		}

		// Build structured data for JSON
		clusterInfo := ClusterInfo{
			Name:     name,
			Address:  ccfg.Hostname,
			Identity: identity,
			Active:   isActive,
		}
		clusters = append(clusters, clusterInfo)

		// Build table row with formatting
		if !opts.IsJSON() {
			// Format address - color port portion gray for table display
			address := ccfg.Hostname
			// Check if it has a port specified
			if strings.Contains(address, ":") {
				// Find the last colon to handle IPv6 addresses properly
				lastColon := strings.LastIndex(address, ":")
				host := address[:lastColon]
				port := address[lastColon+1:]

				// Color the port gray
				grayPort := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(":" + port)
				address = host + grayPort
			}

			rows = append(rows, ui.Row{
				prefix,
				name,
				address,
				identity,
			})
		}
		return nil
	})

	if err != nil {
		return err
	}

	if len(clusters) == 0 {
		if opts.IsJSON() {
			return PrintJSON([]interface{}{})
		}
		ctx.Printf("No clusters configured\n")
		return nil
	}

	// Output based on format
	if opts.IsJSON() {
		return PrintJSON(clusters)
	}

	// Create and render the table
	columns := ui.AutoSizeColumns(headers, rows)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}
