package commands

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/build"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/asm"
)

func (c *Context) setupServerComponents(ctx context.Context, reg *asm.Registry) {
	reg.Provide(func() (*containerd.Client, error) {
		return containerd.New("/run/containerd/containerd.sock")
	})

	reg.Provide(func(opts struct {
		CC *containerd.Client
	}) (*buildkit.Client, error) {
		return buildkit.New(ctx, "")
	})

	reg.Register("namespace", "miren-test")
	reg.Register("org_id", uint64(1))

	reg.Register("log", c.Log)

	reg.Register("clickhouse-address", "clickhouse:9000")
	reg.Register("postgres-address", "postgres:5432")

	reg.ProvideName("clickhouse", func() *sql.DB {
		return clickhouse.OpenDB(&clickhouse.Options{
			Addr: []string{"clickhouse:9000"},
			Auth: clickhouse.Auth{
				Database: "default",
				Username: "default",
				Password: "",
			},
			DialTimeout: time.Second * 30,
			Compression: &clickhouse.Compression{
				Method: clickhouse.CompressionLZ4,
			},
			Debug: true,
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

	reg.ProvideName("postgres", func(opts struct {
		Log     *slog.Logger
		Address string `asm:"postgres-address"`
	}) (*pgxpool.Pool, error) {
		pool, err := pgxpool.New(ctx,
			fmt.Sprintf("postgres://miren:miren@%s/miren_dev", opts.Address),
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
}

func runMigrations(ctx context.Context, dir string, pool *pgxpool.Pool) error {
	c, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}

	defer c.Release()

	m, err := migrate.NewMigrator(ctx, c.Conn(), "schema_versions")
	if err != nil {
		return err
	}

	err = m.LoadMigrations(os.DirFS(dir))
	if err != nil {
		return err
	}

	return m.Migrate(ctx)
}
