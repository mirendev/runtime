package commands

import (
	"context"
	"log/slog"
	"net/netip"
	"os"
	"time"

	"miren.dev/mflags"

	containerd "github.com/containerd/containerd/v2/client"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/containerdx"
)

func addCommands(d *mflags.Dispatcher) {
	// Server command is now defined in commands.go (renamed from dev)

	// Cloud registration commands
	d.Dispatch("server register", Infer("server register", "Register this cluster with miren.cloud", Register))

	d.Dispatch("server register status", Infer("server register status", "Show cluster registration status", RegisterStatus))

	// Server management commands
	d.Dispatch("server install", Infer("server install", "Install systemd service for miren server", ServerInstall))

	d.Dispatch("server uninstall", Infer("server uninstall", "Remove systemd service for miren server", ServerUninstall))

	d.Dispatch("server status", Infer("server status", "Show miren service status", ServerStatus))
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
