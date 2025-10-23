package commands

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"time"

	"github.com/mitchellh/cli"

	"github.com/ClickHouse/clickhouse-go/v2"
	containerd "github.com/containerd/containerd/v2/client"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/containerdx"
)

func addCommands(cmds map[string]cli.CommandFactory) {
	// Server command is now defined in commands.go (renamed from dev)

	// Cloud registration commands
	cmds["register"] = func() (cli.Command, error) {
		return Infer("register", "Register this cluster with miren.cloud", Register), nil
	}

	cmds["register status"] = func() (cli.Command, error) {
		return Infer("register status", "Show cluster registration status", RegisterStatus), nil
	}

	// Server management commands
	cmds["server install"] = func() (cli.Command, error) {
		return Infer("server install", "Install systemd service for miren server", ServerInstall), nil
	}

	cmds["server uninstall"] = func() (cli.Command, error) {
		return Infer("server uninstall", "Remove systemd service for miren server", ServerUninstall), nil
	}

	cmds["server status"] = func() (cli.Command, error) {
		return Infer("server status", "Show miren service status", ServerStatus), nil
	}
}

func (c *Context) setupServerComponents(ctx context.Context, reg *asm.Registry) {
	reg.Register("namespace", "miren")
	reg.Register("top_context", ctx)
	reg.Register("containerd-socket", containerdx.DefaultSocket)

	reg.Provide(func(opts struct {
		Namespace string `asm:"namespace"`
		Socket    string `asm:"containerd-socket"`
	}) (*containerd.Client, error) {
		return containerd.New(opts.Socket,
			containerd.WithDefaultNamespace(opts.Namespace))
	})

	reg.Provide(func(opts struct {
		CC  *containerd.Client
		Log *slog.Logger `asm:"log"`
	}) (*buildkit.Client, error) {
		// When address is empty, buildkit.New will try to detect the daemon using:
		// 1. $BUILDKIT_HOST environment variable
		// 2. $DOCKER_HOST environment variable (for Docker Buildx)
		// 3. Default unix socket locations
		opts.Log.Debug("creating buildkit client with default (empty) address")
		client, err := buildkit.New(ctx, "")
		if err != nil {
			opts.Log.Error("failed to create buildkit client with default address", "error", err)
		} else {
			opts.Log.Info("buildkit client created with default address")
		}
		return client, err
	})

	reg.Register("org_id", uint64(1))

	reg.Register("log", c.Log)

	reg.Register("bridge-iface", "rt0")

	reg.Register("tempdir", os.TempDir())

	reg.Register("server-id", "miren-server")

	reg.Register("data-path", "/var/lib/miren")

	reg.Register("service-subnet", netip.MustParsePrefix("10.10.0.0/16"))

	// Configure ClickHouse address from environment variables or default
	clickhouseHost := os.Getenv("CLICKHOUSE_HOST")
	if clickhouseHost == "" {
		clickhouseHost = "clickhouse"
	}
	clickhousePort := os.Getenv("CLICKHOUSE_PORT")
	if clickhousePort == "" {
		clickhousePort = "9000"
	}
	reg.Register("clickhouse-address", fmt.Sprintf("%s:%s", clickhouseHost, clickhousePort))
	reg.Register("clickhouse-debug", false)

	// VictoriaLogs configuration
	victoriaLogsAddr := os.Getenv("VICTORIALOGS_ADDR")
	if victoriaLogsAddr == "" {
		victoriaLogsAddr = "localhost:9428"
	}
	reg.Register("victorialogs-address", victoriaLogsAddr)
	reg.Register("victorialogs-timeout", 30*time.Second)

	// VictoriaMetrics configuration
	victoriaMetricsAddr := os.Getenv("VICTORIAMETRICS_ADDR")
	if victoriaMetricsAddr == "" {
		victoriaMetricsAddr = "localhost:8428"
	}
	reg.Register("victoriametrics-address", victoriaMetricsAddr)
	reg.Register("victoriametrics-timeout", 30*time.Second)

	reg.Register("container_idle_timeout", time.Minute)

	reg.Register("http_domain", "local.miren.run")
	reg.Register("lookup_timeout", 5*time.Minute)

	reg.Register("rollback_window", 2)

	reg.ProvideName("clickhouse", func(opts struct {
		Log     *slog.Logger
		Address string `asm:"clickhouse-address"`
		Debug   bool   `asm:"clickhouse-debug"`
	}) *sql.DB {
		return clickhouse.OpenDB(&clickhouse.Options{
			Addr: []string{opts.Address},
			Auth: clickhouse.Auth{
				Database: "default",
				Username: "default",
				Password: "default",
			},
			DialTimeout: time.Second * 30,
			Compression: &clickhouse.Compression{
				Method: clickhouse.CompressionLZ4,
			},
			Debug: opts.Debug,
			Debugf: func(format string, v ...interface{}) {
				opts.Log.Debug(fmt.Sprintf(format, v...))
			},
		})
	})

	// VictoriaMetrics writer provider
	reg.ProvideName("victoriametrics-writer", func(opts struct {
		Log     *slog.Logger
		Address string        `asm:"victoriametrics-address"`
		Timeout time.Duration `asm:"victoriametrics-timeout"`
	}) *metrics.VictoriaMetricsWriter {
		writer := metrics.NewVictoriaMetricsWriter(opts.Log, opts.Address, opts.Timeout)
		writer.Start()
		return writer
	})

	// VictoriaMetrics reader provider
	reg.ProvideName("victoriametrics-reader", func(opts struct {
		Log     *slog.Logger
		Address string        `asm:"victoriametrics-address"`
		Timeout time.Duration `asm:"victoriametrics-timeout"`
	}) *metrics.VictoriaMetricsReader {
		return metrics.NewVictoriaMetricsReader(opts.Log, opts.Address, opts.Timeout)
	})

	reg.Provide(func(opts struct {
		Log *slog.Logger
	}) *observability.StatusMonitor {
		log := opts.Log
		if log == nil {
			log = slog.Default()
		}
		return &observability.StatusMonitor{
			Log: log,
		}
	})

	for _, f := range autoreg.All() {
		reg.Provide(f.Interface())
	}
}
