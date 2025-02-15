package commands

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/mitchellh/cli"

	"github.com/ClickHouse/clickhouse-go/v2"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/jackc/pgx/v5/pgxpool"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/app"
	"miren.dev/runtime/build"
	"miren.dev/runtime/build/launch"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/health"
	"miren.dev/runtime/image"
	"miren.dev/runtime/ingress"
	"miren.dev/runtime/lease"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/run"
	"miren.dev/runtime/server"
	"miren.dev/runtime/shell"
)

func addCommands(cmds map[string]cli.CommandFactory) {
	cmds["server"] = func() (cli.Command, error) {
		return Infer("server", "Start the server", Server), nil
	}
}

func (c *Context) setupServerComponents(ctx context.Context, reg *asm.Registry) {
	reg.Register("namespace", "runtime")
	reg.Register("top_context", ctx)

	reg.Provide(func(opts struct {
		Namespace string `asm:"namespace"`
	}) (*containerd.Client, error) {
		return containerd.New("/run/containerd/containerd.sock",
			containerd.WithDefaultNamespace(opts.Namespace))
	})

	reg.Provide(func(opts struct {
		CC *containerd.Client
	}) (*buildkit.Client, error) {
		return buildkit.New(ctx, "")
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

	reg.ProvideName("subnet", func(opts struct {
		TempDir string `asm:"tempdir"`
		Id      string `asm:"server-id"`
	}) (*netdb.Subnet, error) {
		ndb, err := netdb.New(filepath.Join(opts.TempDir, "net.db"))
		if err != nil {
			return nil, err
		}

		mega, err := ndb.Subnet("10.8.0.0/16")
		if err != nil {
			return nil, err
		}

		return mega.ReserveSubnet(24, opts.Id)
	})

	reg.Register("clickhouse-address", "clickhouse:9000")
	reg.Register("postgres-address", "postgres:5432")
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

	findMigrations := func() (string, error) {
		dir, err := os.Getwd()
		if err != nil {
			return "", err
		}

		for {
			if _, err := os.Stat(dir + "/db"); err == nil {
				return dir + "/db", nil
			}

			if dir == "/" {
				return "", os.ErrNotExist
			}

			dir = filepath.Dir(dir)
		}
	}
	reg.Register("postgres-db", "runtime_prod")

	reg.ProvideName("postgres", func(opts struct {
		Log     *slog.Logger
		Address string `asm:"postgres-address"`
		DB      string `asm:"postgres-db"`
	}) (*pgxpool.Pool, error) {
		pool, err := pgxpool.New(ctx,
			fmt.Sprintf("postgres://runtime:runtime@%s/%s", opts.Address, opts.DB),
		)
		if err != nil {
			return nil, err
		}

		dir, err := findMigrations()
		if err != nil {
			return nil, err
		}

		err = runMigrations(ctx, dir, pool)
		if err != nil {
			return nil, err
		}

		opts.Log.Debug("connected to postgres", "addr", opts.Address)

		return pool, nil
	})
	reg.Provide(func() observability.LogWriter {
		return &observability.PersistentLogWriter{}
	})

	reg.Provide(func() *observability.StatusMonitor {
		return &observability.StatusMonitor{}
	})

	reg.Provide(func() *build.Buildkit {
		return &build.Buildkit{}
	})

	reg.Provide(func() *build.RPCBuilder {
		return &build.RPCBuilder{}
	})

	reg.Provide(func() *launch.LaunchBuildkit {
		return &launch.LaunchBuildkit{}
	})

	reg.Provide(func() *run.ContainerRunner {
		return &run.ContainerRunner{}
	})

	reg.Provide(func() *app.AppAccess {
		return &app.AppAccess{}
	})

	reg.Provide(func() *app.RPCCrud {
		return &app.RPCCrud{}
	})

	reg.Provide(func() *image.ImageImporter {
		return &image.ImageImporter{}
	})

	reg.Provide(func() *shell.RPCShell {
		return &shell.RPCShell{}
	})

	reg.Provide(func() *lease.LaunchContainer {
		return &lease.LaunchContainer{}
	})

	reg.Provide(func() *image.ImagePruner {
		return &image.ImagePruner{}
	})

	reg.Provide(func() *discovery.Containerd {
		return &discovery.Containerd{}
	})

	reg.Provide(func() *health.ContainerMonitor {
		return &health.ContainerMonitor{}
	})

	reg.Provide(func() *ingress.LeaseHTTP {
		return &ingress.LeaseHTTP{}
	})

	reg.Provide(func() *observability.RunSCMonitor {
		return &observability.RunSCMonitor{}
	})

	reg.Provide(func() *server.RPCAppInfo {
		return &server.RPCAppInfo{}
	})

	for _, f := range autoreg.All() {
		reg.Provide(f.Interface())
	}
}
