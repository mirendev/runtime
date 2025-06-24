package testutils

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	cdcomp "miren.dev/runtime/components/containerd"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
)

func Registry(extra ...func(*asm.Registry)) (*asm.Registry, func()) {
	var r asm.Registry

	var usedClient *containerd.Client
	var usedComponent *cdcomp.ContainerdComponent

	tempDir, err := os.MkdirTemp("", "runtime-reg")
	if err != nil {
		panic(err)
	}

	ndb, err := netdb.New(filepath.Join(tempDir, "net.db"))
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
	r.Register("ip4-routable", subnet.Prefix())

	subnets := []netip.Prefix{
		netip.MustParsePrefix("10.10.0.0/16"),
		netip.MustParsePrefix("fd47:cafe:d00d::/64"),
	}

	r.Register("service-prefixes", subnets)

	r.Register("data-path", tempDir)
	r.Register("tempdir", tempDir)
	r.Register("subnet", subnet)
	r.Register("server_port", 10000)

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

	r.Provide(func(opts struct {
		Log *slog.Logger
	}) (*containerd.Client, error) {
		comp := cdcomp.NewContainerdComponent(opts.Log, tempDir)

		containerdPath, err := exec.LookPath("containerd")
		if err != nil {
			return nil, err
		}

		config := &cdcomp.Config{
			BinaryPath: containerdPath,
			BaseDir:    filepath.Join(tempDir, "containerd"),
			BinDir:     filepath.Dir(containerdPath),
		}

		if err := comp.Start(context.Background(), config); err != nil {
			return nil, err
		}

		cl, err := containerd.New(config.SocketPath)
		if err != nil {
			return nil, err
		}

		usedComponent = comp
		usedClient = cl

		return cl, nil
	})

	r.Provide(func() (*buildkit.Client, error) {
		return buildkit.New(context.TODO(), "")
	})

	ts := time.Now()

	namespace := fmt.Sprintf("runtime-%d", ts.UnixNano())

	r.Register("namespace", namespace)
	r.Register("org_id", uint64(1))

	log := slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	r.Register("log", log)

	r.Register("clickhouse-address", "clickhouse:9000")

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
			Debug: false,
			Debugf: func(format string, v ...interface{}) {
				opts.Log.Debug(fmt.Sprintf(format, v...))
			},
		})
	})

	res, hm := netresolve.NewLocalResolver()

	r.Provide(func() netresolve.Resolver {
		return res
	})

	r.Provide(func() netresolve.HostMapper {
		return hm
	})

	var prefix string

	r.ProvideName("etcd-prefix", func() string {
		prefix = "/" + idgen.Gen("p")
		return prefix
	})

	ctx, cancel := context.WithCancel(context.Background())

	r.Provide(func(opts struct {
		CPU    *metrics.CPUUsage
		Mem    *metrics.MemoryUsage
		Log    *slog.Logger
		Prefix string `asm:"etcd-prefix"`
	}) (*coordinate.Coordinator, error) {
		co := coordinate.NewCoordinator(opts.Log, coordinate.CoordinatorConfig{
			EtcdEndpoints: []string{"etcd:2379"},
			Prefix:        opts.Prefix,
			Resolver:      res,
			TempDir:       tempDir,
			Mem:           opts.Mem,
			Cpu:           opts.CPU,
		})

		err = co.Start(ctx)
		if err != nil {
			return nil, err
		}

		return co, nil
	})

	r.ProvideName("rpc-state", func() (*rpc.State, error) {
		return rpc.NewState(ctx, rpc.WithSkipVerify)
	})

	r.Provide(func(opts struct {
		State *rpc.State `asm:"rpc-state"`
		Co    *coordinate.Coordinator
	}) (*entityserver_v1alpha.EntityAccessClient, error) {
		client, err := opts.State.Connect(opts.Co.ListenAddress(), "entities")
		if err != nil {
			return nil, err
		}

		eac := entityserver_v1alpha.NewEntityAccessClient(client)
		return eac, nil
	})

	for _, f := range autoreg.All() {
		r.Provide(f.Interface())
	}

	for _, fn := range extra {
		fn(&r)
	}

	cleanup := func() {
		cancel()

		if usedClient != nil {
			NukeNamespace(usedClient, namespace)
		}

		if usedComponent != nil {
			// Use a fresh context for cleanup
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			usedComponent.Stop(cleanupCtx)
			cleanupCancel()
		}

		for _, cancel := range cancels {
			cancel()
		}

		ndb.ReleaseInterface(iface)
		mega.ReleaseSubnet(subnet.Prefix())

		ndb.Close()

		os.RemoveAll(tempDir)
	}

	return &r, cleanup
}
