package testutils

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/davecgh/go-spew/spew"
	"github.com/jackc/pgx/v5/pgxpool"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/slogfmt"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
)

func Registry(extra ...func(*asm.Registry)) (*asm.Registry, func()) {
	var r asm.Registry

	var usedClient *containerd.Client

	ndb, err := netdb.New(filepath.Join(os.TempDir(), "net.db"))
	if err != nil {
		panic(err)
	}

	iface, err := ndb.ReserveInterface("mt")
	if err != nil {
		panic(err)
	}

	mega, err := ndb.Subnet("10.8.0.0/16")
	if err != nil {
		panic(err)
	}

	subnet, err := mega.ReserveSubnet(24, idgen.Gen("test"))
	if err != nil {
		panic(err)
	}

	r.Register("tempdir", os.TempDir())
	r.Register("subnet", subnet)

	var cancels []func()

	r.ProvideName("bridge-iface", func() (string, error) {
		_, err = network.SetupBridge(&network.BridgeConfig{
			Name:      iface,
			Addresses: []netip.Prefix{subnet.Router()},
		})
		if err != nil {
			return "", err
		}
		cancels = append(cancels, func() {
			network.TeardownBridge(iface)
		})
		return iface, nil
	})

	r.Provide(func() (*containerd.Client, error) {
		cl, err := containerd.New("/run/containerd.sock")
		if err != nil {
			return nil, err
		}

		usedClient = cl

		return cl, nil
	})

	r.Provide(func() (*buildkit.Client, error) {
		return buildkit.New(context.TODO(), "")
	})

	ts := time.Now()

	namespace := fmt.Sprintf("miren-%d", ts.UnixNano())

	r.Register("namespace", namespace)
	r.Register("org_id", uint64(1))

	log := slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	r.Register("log", log)

	r.ProvideName("clickhouse", func(opts struct {
		Log *slog.Logger
	}) *sql.DB {
		return clickhouse.OpenDB(&clickhouse.Options{
			Addr: []string{"clickhouse:9000"},
			Auth: clickhouse.Auth{
				Database: "default",
				Username: "default",
				Password: "default",
			},
			DialTimeout: time.Second * 30,
			Compression: &clickhouse.Compression{
				Method: clickhouse.CompressionLZ4,
			},
			Debug: true,
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

	for _, f := range autoreg.All() {
		r.Provide(f.Interface())
	}

	for _, fn := range extra {
		fn(&r)
	}

	cleanup := func() {
		if usedClient != nil {
			NukeNamespace(usedClient, namespace)
		}

		for _, cancel := range cancels {
			cancel()
		}

		ndb.ReleaseInterface(iface)
		mega.ReleaseSubnet(subnet.Prefix())

		ndb.Close()
	}

	return &r, cleanup
}
