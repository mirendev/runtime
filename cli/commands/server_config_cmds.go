package commands

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
	"miren.dev/runtime/pkg/serverconfig"
)

// ServerConfigGenerate generates a server configuration file
func ServerConfigGenerate(ctx *Context, opts struct {
	Output   string `short:"o" long:"output" description:"Output file path (defaults to stdout)"`
	Defaults bool   `short:"d" long:"defaults" description:"Generate config with default values"`
	// Kept so scripts passing --mode standalone keep working.
	Mode string `short:"m" long:"mode" description:"Deprecated: standalone is the only server mode" hidden:"yes"`
}) error {
	if opts.Mode != "" && opts.Mode != "standalone" {
		return fmt.Errorf("--mode %s is no longer supported; standalone is the only server mode", opts.Mode)
	}

	var cfg *serverconfig.Config

	if opts.Defaults {
		cfg = serverconfig.DefaultConfig()
	} else {
		cfg = &serverconfig.Config{}
	}

	// Marshal to TOML
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate TOML: %w", err)
	}

	// Write output
	if opts.Output != "" {
		if err := os.WriteFile(opts.Output, data, 0644); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}
		ctx.UILog.Info("Generated config file", "path", opts.Output)
	} else {
		fmt.Print(string(data))
	}

	return nil
}

// ServerConfigValidate validates a server configuration file
func ServerConfigValidate(ctx *Context, opts struct {
	ConfigFile string `short:"f" long:"file" description:"Configuration file to validate" required:"true"`
}) error {
	// Load the configuration
	cfg, err := serverconfig.Load(opts.ConfigFile, nil, ctx.Log)
	if err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}
	if err := cfg.ResolveDeprecatedConfig(); err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}
	if err := cfg.ValidateIngressCoherence(); err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}
	if err := cfg.ValidateTelemetryCoherence(); err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}
	cfg.WarnDeprecatedConfig(ctx.Log)

	ctx.UILog.Info("Configuration is valid", "file", opts.ConfigFile)

	if ctx.Verbose() {
		// Print the loaded configuration
		data, err := toml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}
		fmt.Println("\nLoaded configuration:")
		fmt.Print(string(data))
	}

	return nil
}
