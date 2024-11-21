package testutils

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/davecgh/go-spew/spew"
	"github.com/jackc/pgx/v5/pgxpool"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/slogfmt"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
)

func Registry(extra ...func(*asm.Registry)) *asm.Registry {
	var r asm.Registry

	r.Provide(func() (*containerd.Client, error) {
		return containerd.New("/run/containerd.sock")
	})

	r.Provide(func() (*buildkit.Client, error) {
		return buildkit.New(context.TODO(), "")
	})

	r.Register("namespace", "miren-test")
	r.Register("org_id", uint64(1))

	log := slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	r.Register("log", log)

	r.ProvideName("clickhouse", func() *sql.DB {
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

	r.ProvideName("postgres", func() (*pgxpool.Pool, error) {
		ctx := context.Background()

		pool, err := pgxpool.New(ctx, "postgres://postgres@postgres:5432/miren_test")
		if err != nil {
			return nil, err
		}

		dir, err := findMigrations()
		if err != nil {
			return nil, err
		}

		err = RunMigartions(ctx, dir, pool)
		if err != nil {
			spew.Dump(dir, err)
			return nil, err
		}

		return pool, nil
	})

	for _, fn := range extra {
		fn(&r)
	}

	return &r
}
