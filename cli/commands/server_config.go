package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"miren.dev/runtime/pkg/serverconfig"
)

// ServerConfig provides server configuration management subcommands
func ServerConfig(ctx *Context, opts struct{}) error {
	return fmt.Errorf("please specify a subcommand: generate, validate")
}

// ServerConfigGenerate generates a server config file from current settings
func ServerConfigGenerate(ctx *Context, opts struct {
	Output string `short:"o" long:"output" description:"Output file path (default: stdout)"`
	Mode   string `short:"m" long:"mode" description:"Server mode (standalone, distributed)" default:"standalone"`

	// All the server flags to capture current configuration
	Address                   string   `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	RunnerAddress             string   `long:"runner-address" description:"Address to listen on" default:"localhost:8444"`
	EtcdEndpoints             []string `short:"e" long:"etcd" description:"Etcd endpoints" default:"http://etcd:2379"`
	EtcdPrefix                string   `short:"p" long:"etcd-prefix" description:"Etcd prefix" default:"/miren"`
	RunnerId                  string   `short:"r" long:"runner-id" description:"Runner ID" default:"miren"`
	DataPath                  string   `short:"d" long:"data-path" description:"Data path" default:"/var/lib/miren"`
	ReleasePath               string   `long:"release-path" description:"Path to release directory containing binaries"`
	AdditionalNames           []string `long:"dns-names" description:"Additional DNS names assigned to the server cert"`
	AdditionalIPs             []string `long:"ips" description:"Additional IPs assigned to the server cert"`
	StandardTLS               bool     `long:"serve-tls" description:"Expose the http ingress on standard TLS ports"`
	HTTPRequestTimeout        int      `long:"http-request-timeout" description:"HTTP request timeout in seconds" default:"60"`
	StartEtcd                 bool     `long:"start-etcd" description:"Start embedded etcd server"`
	EtcdClientPort            int      `long:"etcd-client-port" description:"Etcd client port" default:"12379"`
	EtcdPeerPort              int      `long:"etcd-peer-port" description:"Etcd peer port" default:"12380"`
	EtcdHTTPClientPort        int      `long:"etcd-http-client-port" description:"Etcd client port" default:"12381"`
	StartClickHouse           bool     `long:"start-clickhouse" description:"Start embedded ClickHouse server"`
	ClickHouseHTTPPort        int      `long:"clickhouse-http-port" description:"ClickHouse HTTP port" default:"8223"`
	ClickHouseNativePort      int      `long:"clickhouse-native-port" description:"ClickHouse native port" default:"9009"`
	ClickHouseInterServerPort int      `long:"clickhouse-interserver-port" description:"ClickHouse inter-server port" default:"9010"`
	ClickHouseAddress         string   `long:"clickhouse-addr" description:"ClickHouse address (when not using embedded)"`
	StartContainerd           bool     `long:"start-containerd" description:"Start embedded containerd daemon"`
	ContainerdBinary          string   `long:"containerd-binary" description:"Path to containerd binary" default:"containerd"`
	ContainerdSocketPath      string   `long:"containerd-socket" description:"Path to containerd socket"`
	SkipClientConfig          bool     `long:"skip-client-config" description:"Skip writing client config file to clientconfig.d"`
	ConfigClusterName         string   `short:"C" long:"config-cluster-name" description:"Name of the cluster in client config" default:"local"`
}) error {
	// Build config from provided flags
	cfg := &serverconfig.Config{
		Mode: opts.Mode,
		Server: serverconfig.ServerConfig{
			Address:            opts.Address,
			RunnerAddress:      opts.RunnerAddress,
			DataPath:           opts.DataPath,
			RunnerID:           opts.RunnerId,
			ReleasePath:        opts.ReleasePath,
			ConfigClusterName:  opts.ConfigClusterName,
			SkipClientConfig:   opts.SkipClientConfig,
			HTTPRequestTimeout: opts.HTTPRequestTimeout,
		},
		TLS: serverconfig.TLSConfig{
			AdditionalNames: opts.AdditionalNames,
			AdditionalIPs:   opts.AdditionalIPs,
			StandardTLS:     opts.StandardTLS,
		},
		Etcd: serverconfig.EtcdConfig{
			Endpoints:      opts.EtcdEndpoints,
			Prefix:         opts.EtcdPrefix,
			StartEmbedded:  opts.StartEtcd,
			ClientPort:     opts.EtcdClientPort,
			PeerPort:       opts.EtcdPeerPort,
			HTTPClientPort: opts.EtcdHTTPClientPort,
		},
		ClickHouse: serverconfig.ClickHouseConfig{
			StartEmbedded:   opts.StartClickHouse,
			HTTPPort:        opts.ClickHouseHTTPPort,
			NativePort:      opts.ClickHouseNativePort,
			InterServerPort: opts.ClickHouseInterServerPort,
			Address:         opts.ClickHouseAddress,
		},
		Containerd: serverconfig.ContainerdConfig{
			StartEmbedded: opts.StartContainerd,
			BinaryPath:    opts.ContainerdBinary,
			SocketPath:    opts.ContainerdSocketPath,
		},
	}

	// Generate TOML
	tomlData, err := serverconfig.GenerateTOML(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate TOML: %w", err)
	}

	// Add header comment
	header := `# Miren Server Configuration File
# Generated from current server settings
# 
# Configuration Precedence (highest to lowest):
# 1. CLI flags (operational overrides)
# 2. Environment variables (container deployments)
# 3. This config file (baseline configuration)
# 4. Built-in defaults

`

	finalData := append([]byte(header), tomlData...)

	// Write to output or stdout
	if opts.Output != "" {
		// Create directory if needed
		dir := filepath.Dir(opts.Output)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}

		if err := os.WriteFile(opts.Output, finalData, 0644); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}
		ctx.Info("Generated server configuration file: %s\n", opts.Output)
		ctx.Info("You can now start the server with: miren server --config %s\n", opts.Output)
	} else {
		// Write to stdout
		fmt.Print(string(finalData))
	}

	return nil
}

// ServerConfigValidate validates a server configuration file
func ServerConfigValidate(ctx *Context, opts struct {
	ConfigFile string `long:"config" description:"Path to configuration file to validate" required:"true"`
	Verbose    bool   `short:"v" long:"verbose" description:"Show detailed validation information"`
}) error {
	// Check if file exists
	if _, err := os.Stat(opts.ConfigFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s", opts.ConfigFile)
		}
		return fmt.Errorf("failed to access config file: %w", err)
	}

	// Load and validate the configuration
	sourcedConfig, err := serverconfig.Load(opts.ConfigFile, nil, ctx.Log)
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	cfg := &sourcedConfig.Config

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration is invalid: %w", err)
	}

	ctx.Info("✓ Configuration file is valid")

	if opts.Verbose {
		ctx.Info("\nConfiguration Summary:\n")
		ctx.Info("  Mode: %s\n", cfg.Mode)
		ctx.Info("  Server Address: %s\n", cfg.Server.Address)
		ctx.Info("  Runner Address: %s\n", cfg.Server.RunnerAddress)
		ctx.Info("  Data Path: %s\n", cfg.Server.DataPath)
		ctx.Info("  Etcd Endpoints: %v\n", cfg.Etcd.Endpoints)
		ctx.Info("  Etcd Prefix: %s\n", cfg.Etcd.Prefix)

		if cfg.Mode == "standalone" {
			ctx.Info("\n  Embedded Services (automatically enabled in standalone mode):\n")
			ctx.Info("    - Etcd\n")
			ctx.Info("    - ClickHouse\n")
			ctx.Info("    - Containerd\n")
		} else {
			ctx.Info("\n  Embedded Services:\n")
			if cfg.Etcd.StartEmbedded {
				ctx.Info("    - Etcd (ports: %d, %d, %d)\n",
					cfg.Etcd.ClientPort, cfg.Etcd.PeerPort, cfg.Etcd.HTTPClientPort)
			}
			if cfg.ClickHouse.StartEmbedded {
				ctx.Info("    - ClickHouse (ports: %d, %d, %d)\n",
					cfg.ClickHouse.HTTPPort, cfg.ClickHouse.NativePort, cfg.ClickHouse.InterServerPort)
			} else if cfg.ClickHouse.Address != "" {
				ctx.Info("    - ClickHouse (external): %s\n", cfg.ClickHouse.Address)
			}
			if cfg.Containerd.StartEmbedded {
				ctx.Info("    - Containerd (binary: %s)\n", cfg.Containerd.BinaryPath)
			} else if cfg.Containerd.SocketPath != "" {
				ctx.Info("    - Containerd (socket): %s\n", cfg.Containerd.SocketPath)
			}
		}

		if len(cfg.TLS.AdditionalNames) > 0 || len(cfg.TLS.AdditionalIPs) > 0 {
			ctx.Info("\n  TLS Configuration:\n")
			if len(cfg.TLS.AdditionalNames) > 0 {
				ctx.Info("    Additional Names: %v\n", cfg.TLS.AdditionalNames)
			}
			if len(cfg.TLS.AdditionalIPs) > 0 {
				ctx.Info("    Additional IPs: %v\n", cfg.TLS.AdditionalIPs)
			}
			if cfg.TLS.StandardTLS {
				ctx.Info("    Standard TLS: enabled (port 443)\n")
			}
		}

		// Show configuration sources if debug logging is enabled
		ctx.Info("\n  Configuration Sources:\n")
		for key, source := range sourcedConfig.Sources {
			if source != serverconfig.SourceDefault {
				ctx.Info("    %s: %s\n", key, source)
			}
		}
	}

	return nil
}
