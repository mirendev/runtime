package testutils

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	buildkit "github.com/moby/buildkit/client"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
)

func Registry(extra ...func(*asm.Registry)) (*asm.Registry, func()) {
	var r asm.Registry

	var usedClient *containerd.Client
	var netServ *network.ServiceManager

	tempDir, err := os.MkdirTemp("", "miren-reg")
	if err != nil {
		panic(err)
	}

	ndb, err := netdb.New(filepath.Join(tempDir, "net.db"))
	if err != nil {
		panic(err)
	}

	// Generate a unique interface prefix to avoid conflicts with parallel tests.
	// Each test has its own netdb, so without unique prefixes, multiple tests
	// could all get "mt1" and conflict when creating the actual Linux bridge.
	// Use a short random suffix to keep interface name within Linux's 15-char limit.
	ifaceSuffix, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		panic(err)
	}
	ifacePrefix := fmt.Sprintf("mt%d", ifaceSuffix.Int64())

	iface, err := ndb.ReserveInterface(ifacePrefix)
	if err != nil {
		panic(err)
	}

	// Use a random /16 within 10.0.0.0/8 to avoid conflicts with parallel tests.
	// Each test gets its own netdb, so we need different base subnets to prevent
	// IP address collisions when multiple tests run concurrently.
	secondOctet, err := rand.Int(rand.Reader, big.NewInt(256))
	if err != nil {
		panic(err)
	}
	megaSubnet := fmt.Sprintf("10.%d.0.0/16", secondOctet.Int64())

	mega, err := ndb.Subnet(megaSubnet)
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

	r.Register("node-id", "test")

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

	r.Provide(func() (*containerd.Client, error) {
		cl, err := containerd.New(containerdx.DefaultSocket)
		if err != nil {
			return nil, err
		}

		usedClient = cl

		return cl, nil
	})

	r.Provide(func(opts struct {
		Log *slog.Logger
	}) (*buildkit.Client, error) {
		opts.Log.Debug("creating buildkit client for tests with default address")
		client, err := buildkit.New(context.TODO(), "")
		if err != nil {
			opts.Log.Error("failed to create buildkit client for tests", "error", err)
		} else {
			opts.Log.Info("buildkit client created for tests")
		}
		return client, err
	})

	ts := time.Now()

	namespace := fmt.Sprintf("miren-%d", ts.UnixNano())

	r.Register("namespace", namespace)
	r.Register("org_id", uint64(1))

	log := slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	r.Register("log", log)

	r.Register("victorialogs-address", "victorialogs:9428")
	r.Register("victorialogs-timeout", 30*time.Second)

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
			DataPath:      filepath.Join(tempDir, "coordinator"),
			Mem:           opts.Mem,
			Cpu:           opts.CPU,
			NoAuth:        true, // Disable authentication for tests
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

	endpoint := "victoriametrics:8428"

	// Provide a test VictoriaMetrics writer
	r.ProvideName("victoriametrics-writer", func(opts struct {
		Log *slog.Logger
	}) *metrics.VictoriaMetricsWriter {
		log := opts.Log
		if log == nil {
			log = slog.Default()
		}
		writer := metrics.NewVictoriaMetricsWriter(log, endpoint, 30*time.Second)
		writer.Start()
		return writer
	})

	// Provide a test VictoriaMetrics reader
	r.ProvideName("victoriametrics-reader", func(opts struct {
		Log *slog.Logger
	}) *metrics.VictoriaMetricsReader {
		log := opts.Log
		if log == nil {
			log = slog.Default()
		}
		return metrics.NewVictoriaMetricsReader(log, endpoint, 30*time.Second)
	})

	for _, f := range autoreg.All() {
		r.Provide(f.Interface())
	}

	for _, fn := range extra {
		fn(&r)
	}

	cleanup := func() {
		cancel()

		// Try to get ServiceManager and shut it down before cleaning up containerd
		// This is done lazily at cleanup time rather than during setup
		if err := r.Populate(&netServ); err == nil && netServ != nil {
			if err := netServ.ShutdownAll(); err != nil {
				log.Error("failed to shutdown network services during test cleanup", "error", err)
			}
		}

		if usedClient != nil {
			NukeNamespace(usedClient, namespace)
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
