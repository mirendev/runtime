//go:build linux
// +build linux

package commands

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/autotls"
	"miren.dev/runtime/components/clickhouse"
	containerdcomp "miren.dev/runtime/components/containerd"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/components/ipalloc"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/components/scheduler"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/grunge"
	"miren.dev/runtime/pkg/ipdiscovery"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/httpingress"
)

func Dev(ctx *Context, opts struct {
	Mode                      string   `short:"m" long:"mode" description:"Development mode (standalone, distributed)" default:"standalone"`
	Address                   string   `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	RunnerAddress             string   `long:"runner-address" description:"Address to listen on" default:"localhost:8444"`
	EtcdEndpoints             []string `short:"e" long:"etcd" description:"Etcd endpoints" default:"http://etcd:2379"`
	EtcdPrefix                string   `short:"p" long:"etcd-prefix" description:"Etcd prefix" default:"/miren"`
	RunnerId                  string   `short:"r" long:"runner-id" description:"Runner ID" default:"dev"`
	DataPath                  string   `short:"d" long:"data-path" description:"Data path" default:"/var/lib/miren"`
	ReleasePath               string   `long:"release-path" description:"Path to release directory containing binaries"`
	AdditionalNames           []string `long:"dns-names" description:"Additional DNS names assigned to the server cert"`
	AdditionalIPs             []string `long:"ips" description:"Additional IPs assigned to the server cert"`
	StandardTLS               bool     `long:"serve-tls" description:"Expose the http ingress on standard TLS ports"`
	StartEtcd                 bool     `long:"start-etcd" description:"Start embedded etcd server"`
	EtcdClientPort            int      `long:"etcd-client-port" description:"Etcd client port" default:"12379"`
	EtcdPeerPort              int      `long:"etcd-peer-port" description:"Etcd peer port" default:"12380"`
	EtcdHTTPClientPort        int      `long:"etcd-http-client-port" description:"Etcd client port" default:"12381"`
	StartClickHouse           bool     `long:"start-clickhouse" description:"Start embedded ClickHouse server"`
	ClickHouseHTTPPort        int      `long:"clickhouse-http-port" description:"ClickHouse HTTP port" default:"8223"`
	ClickHouseNativePort      int      `long:"clickhouse-native-port" description:"ClickHouse native port" default:"9009"`
	ClickHouseInterServerPort int      `long:"clickhouse-interserver-port" description:"ClickHouse inter-server port" default:"9010"`
	ClickHouseAddress         string   `long:"clickhouse-addr" description:"ClickHouse address (when not using embedded)"`
	StartContainerd           bool     `long:"start-containerd" description:"Start embedded containerd daemon"`
	ContainerdBinary          string   `long:"containerd-binary" description:"Path to containerd binary" default:"containerd"`
	ContainerdSocketPath      string   `long:"containerd-socket" description:"Path to containerd socket"`
	SkipClientConfig          bool     `long:"skip-client-config" description:"Skip writing client config file to clientconfig.d"`
	ConfigClusterName         string   `short:"C" long:"config-cluster-name" description:"Name of the cluster in client config" default:"dev"`
}) error {
	eg, sub := errgroup.WithContext(ctx)

	// Handle mode configuration
	switch opts.Mode {
	case "standalone":
		opts.StartContainerd = true
		opts.StartEtcd = true
		opts.StartClickHouse = true

		// Determine release path
		if opts.ReleasePath == "" {
			// Check for user's home directory first, respecting SUDO_USER
			homeDir, err := getUserHomeDir()
			if err != nil {
				ctx.Log.Warn("failed to determine home directory", "error", err)
				homeDir = ""
			}

			// Try ~/.miren/release
			userReleasePath := filepath.Join(homeDir, ".miren", "release")
			if _, err := os.Stat(userReleasePath); err == nil {
				opts.ReleasePath = userReleasePath
				ctx.Log.Info("using user release path", "path", opts.ReleasePath)
			} else {
				ctx.Log.Info("user release path not found, trying system path")
				// Try /var/lib/miren/release
				systemReleasePath := "/var/lib/miren/release"
				if _, err := os.Stat(systemReleasePath); err == nil {
					opts.ReleasePath = systemReleasePath
					ctx.Log.Info("using system release path", "path", opts.ReleasePath)
				} else {
					return fmt.Errorf("no release directory found (tried %s and %s)", userReleasePath, systemReleasePath)
				}
			}

			// In standalone mode, automatically start all components
			ctx.UILog.Info("running in standalone mode - starting all components", "release-path", opts.ReleasePath)
		} else {
			path, err := filepath.Abs(opts.ReleasePath)
			if err != nil {
				return fmt.Errorf("failed to resolve absolute release path: %w", err)
			}

			opts.ReleasePath = path

			// Verify the provided release path exists
			if _, err := os.Stat(opts.ReleasePath); err != nil {
				return fmt.Errorf("release path does not exist: %s", opts.ReleasePath)
			}
		}
	case "distributed":
		// In distributed mode, use the flags as provided
		ctx.UILog.Info("running in distributed mode")
	default:
		return fmt.Errorf("unknown mode: %s (valid modes: standalone, distributed)", opts.Mode)
	}

	// Determine containerd socket path
	containerdSocketPath := opts.ContainerdSocketPath
	if containerdSocketPath == "" {
		containerdSocketPath = filepath.Join(opts.DataPath, "containerd", "containerd.sock")
	}

	// Start embedded containerd if requested
	if opts.StartContainerd {
		ctx.Log.Info("starting embedded containerd", "binary", opts.ContainerdBinary, "release-path", opts.ReleasePath, "socket", containerdSocketPath)

		var (
			containerdPath string
			binDir         string
			err            error
		)

		if opts.ReleasePath == "" {
			containerdPath, err = exec.LookPath(opts.ContainerdBinary)
			if err != nil {
				ctx.Log.Error("containerd binary not found in PATH", "binary", opts.ContainerdBinary, "error", err)
				return fmt.Errorf("containerd binary not found: %s", opts.ContainerdBinary)
			}
		} else {
			// Get directory containing binaries from release path
			binDir = opts.ReleasePath

			// Use containerd from release path
			containerdPath = filepath.Join(binDir, "containerd")
		}

		// Verify the binary exists
		if _, err := os.Stat(containerdPath); err != nil {
			ctx.Log.Error("containerd binary not found", "path", containerdPath, "error", err)
			return fmt.Errorf("containerd binary not found at %s: %w", containerdPath, err)
		}

		containerdComponent := containerdcomp.NewContainerdComponent(ctx.Log, opts.DataPath)

		containerdConfig := &containerdcomp.Config{
			BinaryPath: containerdPath,
			BaseDir:    filepath.Join(opts.DataPath, "containerd"),
			BinDir:     binDir,
			SocketPath: containerdSocketPath,
			Env:        []string{"PATH=" + binDir + ":" + os.Getenv("PATH")},
		}

		err = containerdComponent.Start(sub, containerdConfig)
		if err != nil {
			ctx.Log.Error("failed to start containerd component", "error", err)
			return err
		}

		ctx.Log.Info("embedded containerd started", "socket", containerdComponent.SocketPath())

		// Ensure cleanup on exit
		defer containerdComponent.Stop(context.Background())

		ctx.Server.Override("containerd-socket", containerdComponent.SocketPath())
	} else {
		// Use existing containerd with provided or default socket path
		defaultSocket := containerdx.DefaultSocket
		if containerdSocketPath != "" {
			defaultSocket = containerdSocketPath
		}

		ctx.Server.Override("containerd-socket", defaultSocket)
	}

	// Start embedded etcd server if requested
	if opts.StartEtcd {
		ctx.Log.Info("starting embedded etcd server", "client-port", opts.EtcdClientPort, "peer-port", opts.EtcdPeerPort)

		// Get containerd client from registry
		var cc *containerd.Client
		err := ctx.Server.Resolve(&cc)
		if err != nil {
			ctx.Log.Error("failed to get containerd client for etcd", "error", err)
			return err
		}

		// TODO figure out why I can't use ResolveNamed to pull out the namespace from ctx.Server
		etcdComponent := etcd.NewEtcdComponent(ctx.Log, cc, "runtime", opts.DataPath)

		etcdConfig := etcd.EtcdConfig{
			Name:           "dev-etcd",
			ClientPort:     opts.EtcdClientPort,
			HTTPClientPort: opts.EtcdHTTPClientPort,
			PeerPort:       opts.EtcdPeerPort,
			ClusterState:   "new",
		}

		err = etcdComponent.Start(sub, etcdConfig)
		if err != nil {
			ctx.Log.Error("failed to start etcd component", "error", err)
			return err
		}

		// Update etcd endpoints to use local etcd
		opts.EtcdEndpoints = []string{etcdComponent.ClientEndpoint()}
		ctx.Log.Info("using embedded etcd", "endpoint", etcdComponent.ClientEndpoint())

		// Ensure cleanup on exit
		defer etcdComponent.Stop(context.Background())
	}

	// Start embedded ClickHouse server if requested
	if opts.StartClickHouse {
		ctx.Log.Info("starting embedded clickhouse server",
			"http-port", opts.ClickHouseHTTPPort,
			"native-port", opts.ClickHouseNativePort,
			"interserver-port", opts.ClickHouseInterServerPort)

		// Get containerd client from registry
		var cc *containerd.Client
		err := ctx.Server.Resolve(&cc)
		if err != nil {
			ctx.Log.Error("failed to get containerd client for clickhouse", "error", err)
			return err
		}

		clickhouseComponent := clickhouse.NewClickHouseComponent(ctx.Log, cc, "runtime", opts.DataPath)

		clickhouseConfig := clickhouse.ClickHouseConfig{
			HTTPPort:        opts.ClickHouseHTTPPort,
			NativePort:      opts.ClickHouseNativePort,
			InterServerPort: opts.ClickHouseInterServerPort,
			User:            "default",
			Password:        "default",
		}

		err = clickhouseComponent.Start(sub, clickhouseConfig)
		if err != nil {
			ctx.Log.Error("failed to start clickhouse component", "error", err)
			return err
		}

		ctx.Log.Info("embedded clickhouse started",
			"http-endpoint", clickhouseComponent.HTTPEndpoint(),
			"native-endpoint", clickhouseComponent.NativeEndpoint())

		// Register ClickHouse component in the registry for other components to use
		ctx.Server.Override("clickhouse-address", clickhouseComponent.NativeEndpoint())

		// Ensure cleanup on exit
		defer clickhouseComponent.Stop(context.Background())
	} else if opts.ClickHouseAddress != "" {
		// Override ClickHouse address if provided (for external ClickHouse)
		ctx.Log.Info("using external clickhouse", "address", opts.ClickHouseAddress)
		ctx.Server.Override("clickhouse-address", opts.ClickHouseAddress)
	}

	klog.SetLogger(logr.FromSlogHandler(ctx.Log.With("module", "global").Handler()))

	res, hm := netresolve.NewLocalResolver()

	var (
		cpu  metrics.CPUUsage
		mem  metrics.MemoryUsage
		logs observability.LogReader
	)

	err := ctx.Server.Populate(&mem)
	if err != nil {
		ctx.Log.Error("failed to populate memory usage", "error", err)
		return err
	}

	err = ctx.Server.Populate(&cpu)
	if err != nil {
		ctx.Log.Error("failed to populate CPU usage", "error", err)
		return err
	}

	err = ctx.Server.Populate(&logs)
	if err != nil {
		ctx.Log.Error("failed to populate log reader", "error", err)
		return err
	}

	// Discover local IPs using ipdiscovery
	discovery, err := ipdiscovery.DiscoverWithTimeout(5*time.Second, ctx.Log)
	if err != nil {
		ctx.Log.Warn("failed to discover local IPs", "error", err)
		// Don't fail if IP discovery fails, just log it
	} else {
		// Add discovered IPs to the additional IPs list
		for _, addr := range discovery.Addresses {
			// Skip IPv6 link-local addresses
			ip := net.ParseIP(addr.IP)
			if ip != nil && !ip.IsLinkLocalUnicast() {
				opts.AdditionalIPs = append(opts.AdditionalIPs, addr.IP)
			}
		}

		// Add public IP if available
		if discovery.PublicIP != "" {
			opts.AdditionalIPs = append(opts.AdditionalIPs, discovery.PublicIP)
		}

		ctx.Log.Info("discovered IPs", "local-addresses", len(discovery.Addresses), "public", discovery.PublicIP)
	}

	var additionalIps []net.IP
	for _, ip := range opts.AdditionalIPs {
		addr := net.ParseIP(ip)
		if addr == nil {
			ctx.Log.Error("failed to parse additional IP", "ip", ip)
			return fmt.Errorf("failed to parse additional IP %s", ip)
		}
		additionalIps = append(additionalIps, addr)
	}

	co := coordinate.NewCoordinator(ctx.Log, coordinate.CoordinatorConfig{
		Address:         opts.Address,
		EtcdEndpoints:   opts.EtcdEndpoints,
		Prefix:          opts.EtcdPrefix,
		DataPath:        opts.DataPath,
		AdditionalNames: opts.AdditionalNames,
		AdditionalIPs:   additionalIps,
		Resolver:        res,
		TempDir:         os.TempDir(),
		Mem:             &mem,
		Cpu:             &cpu,
		Logs:            &logs,
	})

	err = co.Start(sub)
	if err != nil {
		ctx.Log.Error("failed to start coordinator", "error", err)
		return err
	}

	time.Sleep(time.Second)

	subnets := []netip.Prefix{
		netip.MustParsePrefix("10.10.0.0/16"),
		netip.MustParsePrefix("fd47:cafe:d00d::/64"),
	}

	reg := ctx.Server

	ctx.Server.Register("service-prefixes", subnets)

	gn, err := grunge.NewNetwork(ctx.Log, grunge.NetworkOptions{
		EtcdEndpoints: opts.EtcdEndpoints,
		EtcdPrefix:    opts.EtcdPrefix + "/sub/flannel",
	})
	if err != nil {
		ctx.Log.Error("failed to create grunge network", "error", err)
		return err
	}

	err = gn.SetupConfig(ctx,
		netip.MustParsePrefix("10.8.0.0/16"),
		netip.MustParsePrefix("fd47:ace::/64"),
	)
	if err != nil {
		ctx.Log.Error("failed to setup grunge network", "error", err)
		return err
	}

	err = gn.Start(sub, eg)
	if err != nil {
		ctx.Log.Error("failed to start grunge network", "error", err)
		return err
	}

	lease := gn.Lease()

	ctx.Log.Info("leased IP prefixes", "ipv4", lease.IPv4().String(), "ipv6", lease.IPv6().String())

	reg.Register("ip4-routable", lease.IPv4())

	reg.ProvideName("subnet", func(opts struct {
		Dir    string       `asm:"data-path"`
		Id     string       `asm:"server-id"`
		Prefix netip.Prefix `asm:"ip4-routable"`
	}) (*netdb.Subnet, error) {
		os.MkdirAll(opts.Dir, 0755)
		ndb, err := netdb.New(filepath.Join(opts.Dir, "net.db"))
		if err != nil {
			return nil, fmt.Errorf("failed to open netdb: %w", err)
		}

		sub, err := ndb.Subnet(opts.Prefix.String())
		if err != nil {
			return nil, err
		}

		return sub, nil
	})

	reg.ProvideName("router-address", func(opts struct {
		Sub *netdb.Subnet `asm:"subnet"`
	}) (netip.Addr, error) {
		return opts.Sub.Router().Addr(), nil
	})

	// Run the runner!

	scfg, err := co.ServiceConfig()
	if err != nil {
		ctx.Log.Error("failed to get service config", "error", err)
		return err
	}

	// Create RPC client to interact with coordinator
	rs, err := scfg.State(ctx, rpc.WithLogger(ctx.Log))
	if err != nil {
		ctx.Log.Error("failed to create RPC client", "error", err)
		return err
	}

	client, err := rs.Client("entities")
	if err != nil {
		ctx.Log.Error("failed to connect to RPC server", "error", err)
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	ec := entityserver.NewClient(ctx.Log, eac)

	reg.Register("hl-entity-client", ec)

	ipa := ipalloc.NewAllocator(ctx.Log, subnets)
	eg.Go(func() error {
		return ipa.Watch(sub, eac)
	})

	aa := co.Activator()

	hs := httpingress.NewServer(ctx, ctx.Log, client, aa)

	reg.Register("app-activator", aa)
	reg.Register("resolver", res)

	rcfg, err := co.NamedConfig("runner")
	if err != nil {
		return err
	}

	r := runner.NewRunner(ctx.Log, ctx.Server, runner.RunnerConfig{
		Id:            opts.RunnerId,
		ListenAddress: opts.RunnerAddress,
		Workers:       runner.DefaulWorkers,
		Config:        rcfg,
	})

	err = r.Start(sub)
	if err != nil {
		return err
	}

	defer r.Close()

	sch, err := scheduler.NewScheduler(sub, ctx.Log, eac)
	if err != nil {
		ctx.Log.Error("failed to create scheduler", "error", err)
		return err
	}

	eg.Go(func() error {
		return sch.Watch(sub, eac)
	})

	go func() {
		err := http.ListenAndServe(":8989", hs)
		if err != nil {
			ctx.Log.Error("failed to start HTTP server", "error", err)
		}
	}()

	if opts.StandardTLS {
		autotls.ServeTLS(sub, ctx.Log, opts.DataPath, hs)
	}

	var registry ocireg.Registry
	err = reg.Populate(&registry)
	if err != nil {
		ctx.Log.Error("failed to populate OCI registry", "error", err)
		return err
	}

	registry.Start(ctx, ":5000")

	var regAddr netip.Addr

	err = reg.ResolveNamed(&regAddr, "router-address")
	if err != nil {
		ctx.Log.Error("failed to resolve router address", "error", err)
		return err
	}

	ctx.Log.Info("OCI registry URL", "url", regAddr)

	hm.SetHost("cluster.local", regAddr)

	cert, err := co.IssueCertificate("dev")
	if err != nil {
		ctx.Log.Error("failed to issue dev certificate", "error", err)
		return fmt.Errorf("failed to issue dev certificate: %w", err)
	}

	if opts.ConfigClusterName == "" {
		opts.ConfigClusterName = "dev"
	}

	// Write dev cluster config to clientconfig.d
	if opts.Mode == "standalone" && !opts.SkipClientConfig {
		if err := writeDevClusterConfig(ctx, cert, opts.Address, opts.ConfigClusterName); err != nil {
			ctx.Log.Warn("failed to write dev cluster config", "error", err)
			// Don't fail the whole command if we can't write the config
		}
	}

	ctx.UILog.Info("Started in dev mode", "address", opts.Address, "etcd_endpoints", opts.EtcdEndpoints, "etcd_prefix", opts.EtcdPrefix, "runner_id", opts.RunnerId)

	ctx.Info("Dev mode started successfully! You can now connect to the cluster using `-C %s`\n", opts.ConfigClusterName)
	ctx.Info("For example `cd my-app; runtime deploy -C dev`")

	return eg.Wait()
}

// getUserHomeDir returns the user's home directory, respecting SUDO_USER if running under sudo
func getUserHomeDir() (string, error) {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		// Running under sudo, get the original user's home
		u, err := user.Lookup(sudoUser)
		if err == nil {
			return u.HomeDir, nil
		}
		// Fallback to HOME env var
		if homeDir := os.Getenv("HOME"); homeDir != "" {
			return homeDir, nil
		}
	} else {
		// Not running under sudo
		if homeDir := os.Getenv("HOME"); homeDir != "" {
			return homeDir, nil
		}
		u, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return u.HomeDir, nil
	}
	return "", fmt.Errorf("could not determine home directory")
}

// writeDevClusterConfig writes a client config file for the dev cluster
func writeDevClusterConfig(ctx *Context, cc *caauth.ClientCertificate, address, clusterName string) error {
	// Determine the config.d directory path, respecting SUDO_USER
	homeDir, err := getUserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	configDirPath := filepath.Join(homeDir, ".config/runtime/clientconfig.d")

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(configDirPath, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create the dev cluster config
	devConfigPath := filepath.Join(configDirPath, "50-dev.yaml")

	lcfg := &clientconfig.Config{
		Clusters: map[string]*clientconfig.ClusterConfig{
			clusterName: {
				Hostname:   address,
				CACert:     string(cc.CACert),
				ClientCert: string(cc.CertPEM),
				ClientKey:  string(cc.KeyPEM),
			},
		},
	}

	if err := lcfg.SaveTo(devConfigPath); err != nil {
		return fmt.Errorf("failed to save dev cluster config: %w", err)
	}

	ctx.Log.Info("wrote dev cluster config", "path", devConfigPath, "name", clusterName, "address", address)
	return nil
}
