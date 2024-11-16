package testutils

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
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

	for _, fn := range extra {
		fn(&r)
	}

	return &r
}
