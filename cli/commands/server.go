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
	"strconv"
	"strings"
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
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/servers/httpingress"
)

func Server(ctx *Context, opts struct {
	ConfigFile                string   `long:"config" description:"Path to configuration file"`
	Mode                      string   `short:"m" long:"mode" description:"Server mode (standalone, distributed)" default:"standalone"`
	Address                   string   `short:"a" long:"address" description:"Address to listen on" default:"localhost:8443"`
	RunnerAddress             string   `long:"runner-address" description:"Address to listen on" default:"localhost:8444"`
	EtcdEndpoints             []string `short:"e" long:"etcd" description:"Etcd endpoints" default:"http://etcd:2379"`
	EtcdPrefix                string   `short:"p" long:"etcd-prefix" description:"Etcd prefix" default:"/miren"`
	RunnerId                  string   `short:"r" long:"runner-id" description:"Runner ID" default:"miren"`
	DataPath                  string   `short:"d" long:"data-path" description:"Data path" default:"/var/lib/miren" asm:"data-path"`
	ReleasePath               string   `long:"release-path" description:"Path to release directory containing binaries"`
	AdditionalNames           []string `long:"dns-names" description:"Additional DNS names assigned to the server cert"`
	AdditionalIPs             []string `long:"ips" description:"Additional IPs assigned to the server cert"`
	StandardTLS               bool     `long:"serve-tls" description:"Expose the http ingress on standard TLS ports"`
	HTTPRequestTimeout        int      `long:"http-request-timeout" description:"HTTP request timeout in seconds" default:"60"`
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
	ConfigClusterName         string   `short:"C" long:"config-cluster-name" description:"Name of the cluster in client config" default:"local"`
}) error {
	eg, sub := errgroup.WithContext(ctx)

	// Load configuration from all sources with precedence:
	// CLI flags > Environment variables > Config file > Defaults
	cliFlags := &serverconfig.CLIFlags{
		Mode:                      opts.Mode,
		Address:                   opts.Address,
		RunnerAddress:             opts.RunnerAddress,
		EtcdEndpoints:             opts.EtcdEndpoints,
		EtcdPrefix:                opts.EtcdPrefix,
		RunnerID:                  opts.RunnerId,
		DataPath:                  opts.DataPath,
		ReleasePath:               opts.ReleasePath,
		AdditionalNames:           opts.AdditionalNames,
		AdditionalIPs:             opts.AdditionalIPs,
		StandardTLS:               opts.StandardTLS,
		HTTPRequestTimeout:        opts.HTTPRequestTimeout,
		StartEtcd:                 opts.StartEtcd,
		EtcdClientPort:            opts.EtcdClientPort,
		EtcdPeerPort:              opts.EtcdPeerPort,
		EtcdHTTPClientPort:        opts.EtcdHTTPClientPort,
		StartClickHouse:           opts.StartClickHouse,
		ClickHouseHTTPPort:        opts.ClickHouseHTTPPort,
		ClickHouseNativePort:      opts.ClickHouseNativePort,
		ClickHouseInterServerPort: opts.ClickHouseInterServerPort,
		ClickHouseAddress:         opts.ClickHouseAddress,
		StartContainerd:           opts.StartContainerd,
		ContainerdBinary:          opts.ContainerdBinary,
		ContainerdSocketPath:      opts.ContainerdSocketPath,
		SkipClientConfig:          opts.SkipClientConfig,
		ConfigClusterName:         opts.ConfigClusterName,
		SetFlags:                  make(map[string]bool),
	}

	// Track which flags were explicitly provided on the CLI (independent of value)
	argv := os.Args[1:]
	if flagProvided("mode", "m", argv) {
		cliFlags.SetFlags["mode"] = true
	}
	if flagProvided("address", "a", argv) {
		cliFlags.SetFlags["address"] = true
	}
	if flagProvided("runner-address", "", argv) {
		cliFlags.SetFlags["runner-address"] = true
	}
	if flagProvided("etcd", "e", argv) {
		cliFlags.SetFlags["etcd"] = true
	}
	if flagProvided("etcd-prefix", "p", argv) {
		cliFlags.SetFlags["etcd-prefix"] = true
	}
	if flagProvided("runner-id", "r", argv) {
		cliFlags.SetFlags["runner-id"] = true
	}
	if flagProvided("data-path", "d", argv) {
		cliFlags.SetFlags["data-path"] = true
	}
	if flagProvided("release-path", "", argv) {
		cliFlags.SetFlags["release-path"] = true
	}
	if flagProvided("dns-names", "", argv) {
		cliFlags.SetFlags["dns-names"] = true
	}
	if flagProvided("ips", "", argv) {
		cliFlags.SetFlags["ips"] = true
	}
	if flagProvided("serve-tls", "", argv) {
		cliFlags.SetFlags["serve-tls"] = true
	}
	if flagProvided("http-request-timeout", "", argv) {
		cliFlags.SetFlags["http-request-timeout"] = true
	}
	if flagProvided("start-etcd", "", argv) {
		cliFlags.SetFlags["start-etcd"] = true
	}
	if flagProvided("etcd-client-port", "", argv) {
		cliFlags.SetFlags["etcd-client-port"] = true
	}
	if flagProvided("etcd-peer-port", "", argv) {
		cliFlags.SetFlags["etcd-peer-port"] = true
	}
	if flagProvided("etcd-http-client-port", "", argv) {
		cliFlags.SetFlags["etcd-http-client-port"] = true
	}
	if flagProvided("start-clickhouse", "", argv) {
		cliFlags.SetFlags["start-clickhouse"] = true
	}
	if flagProvided("clickhouse-http-port", "", argv) {
		cliFlags.SetFlags["clickhouse-http-port"] = true
	}
	if flagProvided("clickhouse-native-port", "", argv) {
		cliFlags.SetFlags["clickhouse-native-port"] = true
	}
	if flagProvided("clickhouse-interserver-port", "", argv) {
		cliFlags.SetFlags["clickhouse-interserver-port"] = true
	}
	if flagProvided("clickhouse-addr", "", argv) {
		cliFlags.SetFlags["clickhouse-addr"] = true
	}
	if flagProvided("start-containerd", "", argv) {
		cliFlags.SetFlags["start-containerd"] = true
	}
	if flagProvided("containerd-binary", "", argv) {
		cliFlags.SetFlags["containerd-binary"] = true
	}
	if flagProvided("containerd-socket", "", argv) {
		cliFlags.SetFlags["containerd-socket"] = true
	}
	if flagProvided("skip-client-config", "", argv) {
		cliFlags.SetFlags["skip-client-config"] = true
	}
	if flagProvided("config-cluster-name", "C", argv) {
		cliFlags.SetFlags["config-cluster-name"] = true
	}

	sourcedConfig, err := serverconfig.Load(opts.ConfigFile, cliFlags, ctx.Log)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	cfg := &sourcedConfig.Config
	opts.Mode = cfg.Mode
	opts.Address = cfg.Server.Address
	opts.RunnerAddress = cfg.Server.RunnerAddress
	opts.DataPath = cfg.Server.DataPath
	opts.RunnerId = cfg.Server.RunnerID
	opts.ReleasePath = cfg.Server.ReleasePath
	opts.ConfigClusterName = cfg.Server.ConfigClusterName
	opts.SkipClientConfig = cfg.Server.SkipClientConfig
	opts.HTTPRequestTimeout = cfg.Server.HTTPRequestTimeout
	opts.AdditionalNames = cfg.TLS.AdditionalNames
	opts.AdditionalIPs = cfg.TLS.AdditionalIPs
	opts.StandardTLS = cfg.TLS.StandardTLS
	opts.EtcdEndpoints = cfg.Etcd.Endpoints
	opts.EtcdPrefix = cfg.Etcd.Prefix
	opts.StartEtcd = cfg.Etcd.StartEmbedded
	opts.EtcdClientPort = cfg.Etcd.ClientPort
	opts.EtcdPeerPort = cfg.Etcd.PeerPort
	opts.EtcdHTTPClientPort = cfg.Etcd.HTTPClientPort
	opts.StartClickHouse = cfg.ClickHouse.StartEmbedded
	opts.ClickHouseHTTPPort = cfg.ClickHouse.HTTPPort
	opts.ClickHouseNativePort = cfg.ClickHouse.NativePort
	opts.ClickHouseInterServerPort = cfg.ClickHouse.InterServerPort
	opts.ClickHouseAddress = cfg.ClickHouse.Address
	opts.StartContainerd = cfg.Containerd.StartEmbedded
	opts.ContainerdBinary = cfg.Containerd.BinaryPath
	opts.ContainerdSocketPath = cfg.Containerd.SocketPath

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

		envPath := os.Getenv("PATH")
		if binDir != "" {
			envPath = binDir + ":" + envPath
		}
		containerdConfig := &containerdcomp.Config{
			BinaryPath: containerdPath,
			BaseDir:    filepath.Join(opts.DataPath, "containerd"),
			BinDir:     binDir,
			SocketPath: containerdSocketPath,
			Env:        []string{"PATH=" + envPath},
		}

		err = containerdComponent.Start(sub, containerdConfig)
		if err != nil {
			ctx.Log.Error("failed to start containerd component", "error", err)
			return err
		}

		ctx.Log.Info("embedded containerd started", "socket", containerdComponent.SocketPath())

		// Ensure cleanup on exit
		defer func() {
			ctx.Log.Info("stopping embedded containerd")
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := containerdComponent.Stop(stopCtx); err != nil {
				ctx.Log.Error("failed to stop containerd component", "error", err)
			}
		}()

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
		etcdComponent := etcd.NewEtcdComponent(ctx.Log, cc, "miren", opts.DataPath)

		etcdConfig := etcd.EtcdConfig{
			Name:           "miren-etcd",
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
		defer func() {
			ctx.Log.Info("stopping embedded etcd")
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := etcdComponent.Stop(stopCtx); err != nil {
				ctx.Log.Error("failed to stop etcd component", "error", err)
			}
		}()
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

		clickhouseComponent := clickhouse.NewClickHouseComponent(ctx.Log, cc, "miren", opts.DataPath)

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
		defer func() {
			ctx.Log.Info("stopping embedded clickhouse")
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := clickhouseComponent.Stop(stopCtx); err != nil {
				ctx.Log.Error("failed to stop clickhouse component", "error", err)
			}
		}()
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

	err = ctx.Server.Populate(&mem)
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
	seen := make(map[string]struct{})
	for _, ip := range opts.AdditionalIPs {
		addr := net.ParseIP(ip)
		if addr == nil {
			ctx.Log.Error("failed to parse additional IP", "ip", ip)
			return fmt.Errorf("failed to parse additional IP %s", ip)
		}
		key := addr.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		additionalIps = append(additionalIps, addr)
	}

	// Create HTTP metrics
	var httpMetrics metrics.HTTPMetrics
	err = ctx.Server.Populate(&httpMetrics)
	if err != nil {
		ctx.Log.Error("failed to populate HTTP metrics", "error", err)
		return err
	}

	// Load registration if it exists
	var cloudAuthConfig coordinate.CloudAuthConfig
	registrationDir := filepath.Join(opts.DataPath, "server")
	if reg, err := registration.LoadRegistration(registrationDir); err != nil {
		ctx.Log.Warn("failed to load registration", "error", err, "dir", registrationDir)
	} else if reg != nil && reg.Status == "approved" {
		// Only use approved registrations
		ctx.Log.Info("loaded cluster registration",
			"cluster-id", reg.ClusterID,
			"cluster-name", reg.ClusterName,
			"org-id", reg.OrganizationID,
			"cloud-url", reg.CloudURL)

		if reg.Tags == nil {
			reg.Tags = make(map[string]string)
		}

		reg.Tags["cluster_id"] = reg.ClusterID
		reg.Tags["cluster_name"] = reg.ClusterName
		reg.Tags["organization_id"] = reg.OrganizationID

		// Configure cloud authentication from registration
		cloudAuthConfig.Enabled = true
		cloudAuthConfig.CloudURL = reg.CloudURL
		cloudAuthConfig.PrivateKey = filepath.Join(registrationDir, "service-account.key")
		cloudAuthConfig.Tags = reg.Tags
		cloudAuthConfig.ClusterID = reg.ClusterID
	} else if reg != nil && reg.Status == "pending" {
		ctx.Log.Info("found pending cluster registration",
			"cluster-name", reg.ClusterName,
			"registration-id", reg.RegistrationID,
			"expires-at", reg.ExpiresAt)
	} else {
		ctx.Log.Info("no cluster registration found")
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
		CloudAuth:       cloudAuthConfig,
		Mem:             &mem,
		Cpu:             &cpu,
		HTTP:            &httpMetrics,
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
		if err := os.MkdirAll(opts.Dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", opts.Dir, err)
		}

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

	if opts.HTTPRequestTimeout <= 0 {
		ctx.Log.Warn("invalid http-request-timeout; using default 60s", "value", opts.HTTPRequestTimeout)
		opts.HTTPRequestTimeout = 60
	}

	ingressConfig := httpingress.IngressConfig{
		RequestTimeout: time.Duration(opts.HTTPRequestTimeout) * time.Second,
	}
	hs := httpingress.NewServer(ctx, ctx.Log, ingressConfig, client, aa, &httpMetrics)

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

	defer func() {
		if err := r.Close(); err != nil {
			ctx.Log.Error("failed to close runner", "error", err)
		}
	}()

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
		if err := autotls.ServeTLS(sub, ctx.Log, opts.DataPath, hs); err != nil {
			ctx.Log.Error("failed to enable standard TLS", "error", err)
		}
	}

	var registry ocireg.Registry
	err = reg.Populate(&registry)
	if err != nil {
		ctx.Log.Error("failed to populate OCI registry", "error", err)
		return err
	}

	if err := registry.Start(ctx, ":5000"); err != nil {
		ctx.Log.Error("failed to start registry", "error", err)
		return err
	}

	var regAddr netip.Addr

	err = reg.ResolveNamed(&regAddr, "router-address")
	if err != nil {
		ctx.Log.Error("failed to resolve router address", "error", err)
		return err
	}

	ctx.Log.Info("OCI registry URL", "url", regAddr)

	if err := hm.SetHost("cluster.local", regAddr); err != nil {
		ctx.Log.Error("failed to set host", "error", err)
		return err
	}

	cert, err := co.IssueCertificate("miren-server")
	if err != nil {
		ctx.Log.Error("failed to issue server certificate", "error", err)
		return fmt.Errorf("failed to issue server certificate: %w", err)
	}

	if opts.ConfigClusterName == "" {
		opts.ConfigClusterName = "local"
	}

	// Write local cluster config to clientconfig.d
	if opts.Mode == "standalone" && !opts.SkipClientConfig {
		if err := writeLocalClusterConfig(ctx, cert, opts.Address, opts.ConfigClusterName); err != nil {
			ctx.Log.Warn("failed to write local cluster config", "error", err)
			// Don't fail the whole command if we can't write the config
		}
	}

	ctx.UILog.Info("Miren server started", "address", opts.Address, "etcd_endpoints", opts.EtcdEndpoints, "etcd_prefix", opts.EtcdPrefix, "runner_id", opts.RunnerId)

	ctx.Info("Miren server started successfully! You can now connect to the cluster using `-C %s`\n", opts.ConfigClusterName)
	ctx.Info("For example: cd my-app && miren deploy -C %s", opts.ConfigClusterName)

	// Wait for all goroutines to complete or context to be cancelled
	err = eg.Wait()
	if err != nil && err != context.Canceled {
		ctx.Log.Error("error during execution", "error", err)
	}

	ctx.Log.Info("miren server shutting down, cleaning up resources")
	return err
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

// writeLocalClusterConfig writes a client config file for the local cluster
func writeLocalClusterConfig(ctx *Context, cc *caauth.ClientCertificate, address, clusterName string) error {
	// Determine the config.d directory path, respecting SUDO_USER
	homeDir, err := getUserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	configDirPath := filepath.Join(homeDir, ".config/miren/clientconfig.d")

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(configDirPath, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Fix directory ownership if running under sudo
	// Fix ownership for all parent directories that we may have created
	dirsToFix := []string{
		filepath.Join(homeDir, ".config"),
		filepath.Join(homeDir, ".config/miren"),
		configDirPath,
	}

	for _, dir := range dirsToFix {
		if err := fixOwnershipIfSudo(ctx, dir); err != nil {
			ctx.Log.Warn("failed to fix directory ownership", "dir", dir, "error", err)
		}
	}

	// Load or create the main client config
	mainConfig, err := clientconfig.LoadConfig()
	if err != nil {
		// If no config exists, create a new one
		if err == clientconfig.ErrNoConfig {
			mainConfig = clientconfig.NewConfig()
		} else {
			return fmt.Errorf("failed to load client config: %w", err)
		}
	}

	// Create the local cluster config data
	leafConfigData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			clusterName: {
				Hostname:   address,
				CACert:     string(cc.CACert),
				ClientCert: string(cc.CertPEM),
				ClientKey:  string(cc.KeyPEM),
			},
		},
	}

	// Add as a leaf config (this will be saved to clientconfig.d/50-local.yaml)
	mainConfig.SetLeafConfig("50-local", leafConfigData)

	// Save the main config (which will also save the leaf config)
	if err := mainConfig.Save(); err != nil {
		return fmt.Errorf("failed to save local cluster config: %w", err)
	}

	// Fix file ownership for the created files if running under sudo
	localConfigPath := filepath.Join(configDirPath, "50-local.yaml")
	// Ensure file is user-readable only since it contains client key
	if err := os.Chmod(localConfigPath, 0600); err != nil {
		ctx.Log.Warn("failed to set config file permissions", "error", err)
	}
	if err := fixOwnershipIfSudo(ctx, localConfigPath); err != nil {
		ctx.Log.Warn("failed to fix config file ownership", "error", err)
	}

	ctx.Log.Info("wrote local cluster config", "path", localConfigPath, "name", clusterName, "address", address)
	return nil
}

// fixOwnershipIfSudo fixes file/directory ownership when running under sudo
func fixOwnershipIfSudo(ctx *Context, path string) error {
	if os.Geteuid() != 0 {
		// Not running as root, nothing to do
		return nil
	}

	// Check if running under sudo
	sudoUID := os.Getenv("SUDO_UID")
	sudoGID := os.Getenv("SUDO_GID")

	if sudoUID == "" || sudoGID == "" {
		// Not running under sudo, nothing to do
		return nil
	}

	// Parse UID and GID
	uid, err := strconv.Atoi(sudoUID)
	if err != nil {
		return fmt.Errorf("failed to parse SUDO_UID: %w", err)
	}

	gid, err := strconv.Atoi(sudoGID)
	if err != nil {
		return fmt.Errorf("failed to parse SUDO_GID: %w", err)
	}

	// Change ownership
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("failed to chown %s to %d:%d: %w", path, uid, gid, err)
	}

	ctx.Log.Debug("fixed ownership for", "path", path, "uid", uid, "gid", gid)

	return nil
}

// flagProvided reports whether a flag was present in argv (handles --long, --long=val, -s, -s=val, and -s val forms).
func flagProvided(long, short string, argv []string) bool {
	prefixLong := "--" + long
	shortFlag := "-" + short
	for i, a := range argv {
		if strings.HasPrefix(a, prefixLong) { // matches --flag and --flag=val
			return true
		}
		if short != "" {
			if a == shortFlag {
				// covers: -s val
				return true
			}
			if strings.HasPrefix(a, shortFlag+"=") {
				// covers: -s=val
				return true
			}
			// Note: combined short flags (e.g., -abc) aren't used here.
		}
		_ = i
	}
	return false
}
