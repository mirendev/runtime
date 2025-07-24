package commands

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"time"

	"github.com/mitchellh/cli"

	"github.com/ClickHouse/clickhouse-go/v2"
	containerd "github.com/containerd/containerd/v2/client"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/containerdx"
)

func addCommands(cmds map[string]cli.CommandFactory) {
	// Server command is now defined in commands.go (renamed from dev)
}

func (c *Context) setupServerComponents(ctx context.Context, reg *asm.Registry) {
	reg.Register("namespace", "runtime")
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

	if path, err := exec.LookPath("runsc-runtime"); err == nil && path != "" {
		reg.Register("runsc_binary", path)
	} else {
		reg.Register("runsc_binary", "runsc")
	}

	reg.Register("server-id", "runtime-server")

	reg.Register("data-path", "/var/lib/runtime")


	reg.Register("service-subnet", netip.MustParsePrefix("10.10.0.0/16"))

	reg.Register("clickhouse-address", "clickhouse:9000")
	reg.Register("clickhouse-debug", false)

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

	reg.Provide(func() observability.LogWriter {
		return &observability.PersistentLogWriter{}
	})

	reg.Provide(func() *observability.StatusMonitor {
		return &observability.StatusMonitor{}
	})

	for _, f := range autoreg.All() {
		reg.Provide(f.Interface())
	}
}
