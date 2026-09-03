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
	"miren.dev/runtime/image"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	build "miren.dev/runtime/pkg/buildkit"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
)

// TestDeps holds all test dependencies explicitly for use in tests.
type TestDeps struct {
	// Containerd
	CC        *containerd.Client
	Namespace string

	// Network
	Bridge          string
	Subnet          *netdb.Subnet
	IPv4Routable    netip.Prefix
	ServicePrefixes []netip.Prefix
	NetServ         *network.ServiceManager
	Resolver        netresolve.Resolver

	// Paths
	DataPath string
	TempDir  string

	// Buildkit
	Buildkit *build.Buildkit

	// Metrics
	Writer      *metrics.VictoriaMetricsWriter
	Reader      *metrics.VictoriaMetricsReader
	CPU         *metrics.CPUUsage
	Mem         *metrics.MemoryUsage
	HTTPMetrics *metrics.HTTPMetrics

	// Observability
	LogsMaintainer      *observability.LogsMaintainer
	StatusMon           *observability.StatusMonitor
	LogWriter           observability.LogWriter
	PersistentLogReader *observability.PersistentLogReader
	Logs                *observability.LogReader

	// Coordinator and Entity Access
	Coordinator *coordinate.Coordinator
	EAC         *entityserver_v1alpha.EntityAccessClient
	RPCState    *rpc.State

	// Logger
	Log *slog.Logger

	// Context
	Ctx    context.Context
	Cancel context.CancelFunc

	// Internal for cleanup
	netdb       *netdb.NetDB
	megaSubnet  *netdb.Subnet
	bridgeIface string
}

// NewTestDeps creates a TestDeps with explicit dependencies.
// This is the preferred way to set up test dependencies.
func NewTestDeps() (*TestDeps, func()) {
	tempDir, err := os.MkdirTemp("", "miren-test")
	if err != nil {
		panic(err)
	}

	log := slog.New(slogfmt.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

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
	// We exclude 10 to avoid overlap with service-prefixes (10.10.0.0/16 below).
	secondOctet, err := rand.Int(rand.Reader, big.NewInt(255))
	if err != nil {
		panic(err)
	}
	octet := secondOctet.Int64()
	if octet >= 10 {
		octet++ // Skip 10 to avoid collision with service-prefixes
	}
	megaSubnet := fmt.Sprintf("10.%d.0.0/16", octet)

	mega, err := ndb.Subnet(megaSubnet)
	if err != nil {
		panic(err)
	}

	subnet, err := mega.ReserveSubnet(24, idgen.Gen("test"))
	if err != nil {
		panic(err)
	}

	// Setup network bridge
	_, err = network.SetupBridge(&network.BridgeConfig{
		Name:      iface,
		Addresses: []netip.Prefix{subnet.Router()},
	})
	if err != nil {
		panic(err)
	}

	// Create containerd client
	cc, err := containerd.New(containerdx.DefaultSocket)
	if err != nil {
		panic(err)
	}

	ts := time.Now()
	namespace := fmt.Sprintf("miren-%d", ts.UnixNano())

	ctx, cancel := context.WithCancel(context.Background())

	// Create buildkit client
	bkClient, err := buildkit.New(ctx, "")
	if err != nil {
		panic(fmt.Errorf("failed to create buildkit client: %w", err))
	}
	bk := &build.Buildkit{
		Client: bkClient,
		Log:    log,
	}

	// Create network service manager and resolver
	netServ := network.NewServiceManager(log, nil)
	resolver, _ := netresolve.NewLocalResolver()

	// Create metrics components
	endpoint := "victoriametrics:8428"
	writer := metrics.NewVictoriaMetricsWriter(log, endpoint, 30*time.Second)
	writer.Start()
	reader := metrics.NewVictoriaMetricsReader(log, endpoint, 30*time.Second)
	cpu := metrics.NewCPUUsage(log, writer, reader)
	mem := metrics.NewMemoryUsage(log, writer, reader)
	httpMetrics := metrics.NewHTTPMetrics(log, writer, reader)

	// Create observability components
	logsMaintainer := observability.NewLogsMaintainer()
	statusMon := observability.NewStatusMonitor(log)
	// Use PersistentLogWriter to write to VictoriaLogs so logs can be read back in tests
	logWriter := observability.NewPersistentLogWriter("victorialogs:9428", 30*time.Second)
	persistentLogReader := observability.NewPersistentLogReader("victorialogs:9428", 30*time.Second)
	logs := observability.NewLogReader("victorialogs:9428", 30*time.Second)

	// Create coordinator
	prefix := "/" + idgen.Gen("p")
	coord := coordinate.NewCoordinator(log, coordinate.CoordinatorConfig{
		EtcdEndpoints: []string{"etcd:2379"},
		Prefix:        prefix,
		Resolver:      resolver,
		TempDir:       tempDir,
		DataPath:      filepath.Join(tempDir, "coordinator"),
		Mem:           mem,
		Cpu:           cpu,
		NoAuth:        true, // Disable authentication for tests
	})

	err = coord.Start(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to start coordinator: %w", err))
	}

	// Create RPC state and entity access client
	rpcState, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		panic(fmt.Errorf("failed to create rpc state: %w", err))
	}

	client, err := rpcState.Connect(coord.ListenAddress(), "entities")
	if err != nil {
		panic(fmt.Errorf("failed to connect to entities: %w", err))
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	deps := &TestDeps{
		CC:        cc,
		Namespace: namespace,

		Bridge:       iface,
		Subnet:       subnet,
		IPv4Routable: subnet.Prefix(),
		ServicePrefixes: []netip.Prefix{
			netip.MustParsePrefix("10.10.0.0/16"),
			netip.MustParsePrefix("fd47:cafe:d00d::/64"),
		},
		NetServ:  netServ,
		Resolver: resolver,

		DataPath: tempDir,
		TempDir:  tempDir,

		Buildkit: bk,

		Writer:      writer,
		Reader:      reader,
		CPU:         cpu,
		Mem:         mem,
		HTTPMetrics: httpMetrics,

		LogsMaintainer:      logsMaintainer,
		StatusMon:           statusMon,
		LogWriter:           logWriter,
		PersistentLogReader: persistentLogReader,
		Logs:                logs,

		Coordinator: coord,
		EAC:         eac,
		RPCState:    rpcState,

		Log:    log,
		Ctx:    ctx,
		Cancel: cancel,

		netdb:       ndb,
		megaSubnet:  mega,
		bridgeIface: iface,
	}

	cleanup := func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := coord.Stop(shutdownCtx); err != nil {
			log.Error("failed to stop test coordinator", "error", err)
		}
		shutdownCancel()

		if netServ != nil {
			netServ.ShutdownAll()
		}

		NukeNamespace(cc, namespace)

		network.TeardownBridge(iface)

		ndb.ReleaseInterface(iface)
		mega.ReleaseSubnet(subnet.Prefix())

		ndb.Close()

		os.RemoveAll(tempDir)
	}

	return deps, cleanup
}

// NewImageImporter creates an ImageImporter using the test dependencies.
func (d *TestDeps) NewImageImporter() image.ImageImporter {
	return image.ImageImporter{
		CC:        d.CC,
		Namespace: d.Namespace,
	}
}
