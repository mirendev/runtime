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
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	"miren.dev/runtime/pkg/nbd"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/servers/httpingress"
)

func Server(ctx *Context, opts serverconfig.CLIFlags) error {
	eg, sub := errgroup.WithContext(ctx)

	// Load configuration from all sources with precedence:
	// CLI flags > Environment variables > Config file > Defaults
	configFile := ""
	if opts.ConfigFile != nil {
		configFile = *opts.ConfigFile
	}
	cfg, err := serverconfig.Load(configFile, &opts, ctx.Log)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	switch cfg.GetMode() {
	case "standalone":
		// Mode defaults are already applied by serverconfig.Load
		// No need to manually set StartEmbedded flags here

		// Determine release path
		if cfg.Server.GetReleasePath() == "" {
			// Check for user's home directory first, respecting SUDO_USER
			homeDir, err := getUserHomeDir()
			if err != nil {
				ctx.Log.Warn("failed to determine home directory", "error", err)
				homeDir = ""
			}

			// Try ~/.miren/release if home dir is available
			var userReleasePath string
			if homeDir != "" {
				userReleasePath = filepath.Join(homeDir, ".miren", "release")
				if _, err := os.Stat(userReleasePath); err == nil {
					cfg.Server.SetReleasePath(userReleasePath)
					ctx.Log.Info("using user release path", "path", userReleasePath)
				}
			}

			if cfg.Server.GetReleasePath() == "" {
				ctx.Log.Info("user release path not found, trying system path")
				// Try /var/lib/miren/release
				systemReleasePath := "/var/lib/miren/release"
				if _, err := os.Stat(systemReleasePath); err == nil {
					cfg.Server.SetReleasePath(systemReleasePath)
					ctx.Log.Info("using system release path", "path", systemReleasePath)
				} else {
					// No release directory found, try to download one
					ctx.Log.Info("no release directory found, downloading release")

					// Determine where to download based on permissions
					downloadGlobal := false
					downloadPath := ""

					// Check if we can write to /var/lib/miren
					// First ensure the directory exists
					if err := os.MkdirAll("/var/lib/miren", 0755); err == nil {
						// Test actual writability by creating a temp file
						tempFile := fmt.Sprintf("/var/lib/miren/.test_%d_%d", os.Getpid(), time.Now().UnixNano())
						f, err := os.OpenFile(tempFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
						if err == nil {
							// Successfully created temp file, clean it up
							f.Close()
							os.Remove(tempFile)
							// We have write permission to system path
							downloadGlobal = true
							downloadPath = systemReleasePath
						} else if userReleasePath != "" {
							// Can't write to system path, fall back to user path
							downloadPath = userReleasePath
						} else {
							return fmt.Errorf("unable to write to /var/lib/miren and no user path available: %w", err)
						}
					} else if userReleasePath != "" {
						// Can't create system directory, use user's home directory
						downloadPath = userReleasePath
					} else {
						return fmt.Errorf("unable to create /var/lib/miren and no user path available: %w", err)
					}

					// Download the release
					if err := PerformDownloadRelease(ctx, DownloadReleaseOptions{
						Branch: "main",
						Global: downloadGlobal,
						Force:  false,
						Output: downloadPath,
					}); err != nil {
						return fmt.Errorf("failed to download release: %w", err)
					}

					// Set the release path to the downloaded location
					cfg.Server.SetReleasePath(downloadPath)
					ctx.Log.Info("using downloaded release", "path", downloadPath)
				}
			}

			// In standalone mode, automatically start all components
			ctx.UILog.Info("running in standalone mode - starting all components", "release-path", cfg.Server.GetReleasePath())
		} else {
			path, err := filepath.Abs(cfg.Server.GetReleasePath())
			if err != nil {
				return fmt.Errorf("failed to resolve absolute release path: %w", err)
			}

			cfg.Server.SetReleasePath(path)

			// Verify the provided release path exists
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("release path does not exist: %s", path)
			}
		}
	case "distributed":
		// In distributed mode, use the flags as provided
		ctx.UILog.Info("running in distributed mode")
	default:
		return fmt.Errorf("unknown mode: %s (valid modes: standalone, distributed)", cfg.GetMode())
	}

	// Initialize NBD kernel module for disk provisioning
	if err := nbd.InitializeNBDModule(ctx.Log); err != nil {
		ctx.Log.Warn("Failed to initialize NBD module (disk provisioning may not work)", "error", err)
		// Don't fail server startup if NBD isn't available
	}

	// Determine containerd socket path
	if cfg.Containerd.GetSocketPath() == "" {
		cfg.Containerd.SetSocketPath(filepath.Join(cfg.Server.GetDataPath(), "containerd", "containerd.sock"))
	}

	// Start embedded containerd if requested
	if cfg.Containerd.GetStartEmbedded() {
		ctx.Log.Info("starting embedded containerd", "binary", cfg.Containerd.GetBinaryPath(), "release-path", cfg.Server.GetReleasePath(), "socket", cfg.Containerd.GetSocketPath())

		var (
			containerdPath string
			binDir         string
			err            error
		)

		if cfg.Server.GetReleasePath() == "" {
			containerdPath, err = exec.LookPath(cfg.Containerd.GetBinaryPath())
			if err != nil {
				ctx.Log.Error("containerd binary not found in PATH", "binary", cfg.Containerd.GetBinaryPath(), "error", err)
				return fmt.Errorf("containerd binary not found: %s", cfg.Containerd.GetBinaryPath())
			}
		} else {
			// Get directory containing binaries from release path
			binDir = cfg.Server.GetReleasePath()

			// Use containerd from release path
			containerdPath = filepath.Join(binDir, "containerd")
		}

		// Verify the binary exists
		if _, err := os.Stat(containerdPath); err != nil {
			ctx.Log.Error("containerd binary not found", "path", containerdPath, "error", err)
			return fmt.Errorf("containerd binary not found at %s: %w", containerdPath, err)
		}

		containerdComponent := containerdcomp.NewContainerdComponent(ctx.Log, cfg.Server.GetDataPath())

		envPath := os.Getenv("PATH")
		if binDir != "" {
			envPath = binDir + ":" + envPath
		}
		containerdConfig := &containerdcomp.Config{
			BinaryPath: containerdPath,
			BaseDir:    filepath.Join(cfg.Server.GetDataPath(), "containerd"),
			BinDir:     binDir,
			SocketPath: cfg.Containerd.GetSocketPath(),
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
		if cfg.Containerd.GetSocketPath() != "" {
			defaultSocket = cfg.Containerd.GetSocketPath()
		}

		ctx.Server.Override("containerd-socket", defaultSocket)
	}

	// Start embedded etcd server if requested
	if cfg.Etcd.GetStartEmbedded() {
		ctx.Log.Info("starting embedded etcd server", "client-port", cfg.Etcd.GetClientPort(), "peer-port", cfg.Etcd.GetPeerPort())

		// Get containerd client from registry
		var cc *containerd.Client
		err := ctx.Server.Resolve(&cc)
		if err != nil {
			ctx.Log.Error("failed to get containerd client for etcd", "error", err)
			return err
		}

		// TODO figure out why I can't use ResolveNamed to pull out the namespace from ctx.Server
		etcdComponent := etcd.NewEtcdComponent(ctx.Log, cc, "miren", cfg.Server.GetDataPath())

		etcdConfig := etcd.EtcdConfig{
			Name:           "miren-etcd",
			ClientPort:     cfg.Etcd.GetClientPort(),
			HTTPClientPort: cfg.Etcd.GetHTTPClientPort(),
			PeerPort:       cfg.Etcd.GetPeerPort(),
			ClusterState:   "new",
		}

		err = etcdComponent.Start(sub, etcdConfig)
		if err != nil {
			ctx.Log.Error("failed to start etcd component", "error", err)
			return err
		}

		// Update etcd endpoints to use local etcd
		cfg.Etcd.Endpoints = []string{etcdComponent.ClientEndpoint()}
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
	if cfg.Clickhouse.GetStartEmbedded() {
		ctx.Log.Info("starting embedded clickhouse server",
			"http-port", cfg.Clickhouse.GetHTTPPort(),
			"native-port", cfg.Clickhouse.GetNativePort(),
			"interserver-port", cfg.Clickhouse.GetInterserverPort())

		// Get containerd client from registry
		var cc *containerd.Client
		err := ctx.Server.Resolve(&cc)
		if err != nil {
			ctx.Log.Error("failed to get containerd client for clickhouse", "error", err)
			return err
		}

		clickhouseComponent := clickhouse.NewClickHouseComponent(ctx.Log, cc, "miren", cfg.Server.GetDataPath())

		clickhouseConfig := clickhouse.ClickHouseConfig{
			HTTPPort:        cfg.Clickhouse.GetHTTPPort(),
			NativePort:      cfg.Clickhouse.GetNativePort(),
			InterServerPort: cfg.Clickhouse.GetInterserverPort(),
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
	} else if cfg.Clickhouse.GetAddress() != "" {
		// Override ClickHouse address if provided (for external ClickHouse)
		ctx.Log.Info("using external clickhouse", "address", cfg.Clickhouse.GetAddress())
		ctx.Server.Override("clickhouse-address", cfg.Clickhouse.GetAddress())
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
				cfg.TLS.AdditionalIPs = append(cfg.TLS.AdditionalIPs, addr.IP)
			}
		}

		// Add public IP if available
		if discovery.PublicIP != "" {
			cfg.TLS.AdditionalIPs = append(cfg.TLS.AdditionalIPs, discovery.PublicIP)
		}

		ctx.Log.Info("discovered IPs", "local-addresses", len(discovery.Addresses), "public", discovery.PublicIP)
	}

	var additionalIps []net.IP
	seen := make(map[string]struct{})
	for _, ip := range cfg.TLS.AdditionalIPs {
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
	registrationDir := filepath.Join(cfg.Server.GetDataPath(), "server")
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
		cloudAuthConfig.PrivateKey = reg.PrivateKey // Use the actual private key content from registration
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

	srvaddr := cfg.Server.GetAddress()
	if !strings.HasPrefix(srvaddr, ":") {
		if _, _, err := net.SplitHostPort(srvaddr); err != nil {
			// ok, add the default port.
			srvaddr = net.JoinHostPort(srvaddr, strconv.Itoa(8443))
			ctx.Log.Debug("no port specified in server address, using default 8443", "address", srvaddr)
		}
	}

	// Create coordinator
	co := coordinate.NewCoordinator(ctx.Log, coordinate.CoordinatorConfig{
		Address:         srvaddr,
		EtcdEndpoints:   cfg.Etcd.Endpoints,
		Prefix:          cfg.Etcd.GetPrefix(),
		DataPath:        cfg.Server.GetDataPath(),
		AdditionalNames: cfg.TLS.AdditionalNames,
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
		EtcdEndpoints: cfg.Etcd.Endpoints,
		EtcdPrefix:    cfg.Etcd.GetPrefix() + "/sub/flannel",
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

	// HttpRequestTimeout is already validated by cfg.Validate() to be >= 1
	// No need for additional checks here

	ingressConfig := httpingress.IngressConfig{
		RequestTimeout: cfg.Server.HTTPRequestTimeoutDuration(),
	}
	hs := httpingress.NewServer(ctx, ctx.Log, ingressConfig, client, aa, &httpMetrics)

	reg.Register("app-activator", aa)
	reg.Register("resolver", res)

	rcfg, err := co.NamedConfig("runner")
	if err != nil {
		return err
	}

	var rc runner.RunnerConfig

	rc.Id = cfg.Server.GetRunnerID()
	rc.ListenAddress = cfg.Server.GetRunnerAddress()
	rc.Workers = runner.DefaulWorkers
	rc.Config = rcfg

	// Pass cloud authentication config if available
	if cloudAuthConfig.Enabled {
		rc.CloudAuth = &cloudAuthConfig
	} else {
		rc.CloudAuth = &coordinate.CloudAuthConfig{}
	}

	err = ctx.Server.Populate(&rc)
	if err != nil {
		ctx.Log.Error("failed to populate runner config", "error", err)
		return err
	}

	r, err := runner.NewRunner(ctx.Log, ctx.Server, rc)
	if err != nil {
		ctx.Log.Error("failed to create runner", "error", err)
		return err
	}

	// Start runner
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

	if cfg.TLS.GetStandardTLS() {
		if err := autotls.ServeTLS(sub, ctx.Log, cfg.Server.GetDataPath(), hs); err != nil {
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

	if cfg.Server.GetConfigClusterName() == "" {
		cfg.Server.SetConfigClusterName("local")
	}

	// Write local cluster config to clientconfig.d
	if cfg.GetMode() == "standalone" && !cfg.Server.GetSkipClientConfig() {
		srvaddr := cfg.Server.GetAddress()
		if strings.HasPrefix(srvaddr, ":") {
			srvaddr = "127.0.0.1" + srvaddr
		} else if strings.HasPrefix(srvaddr, "0.0.0.0:") {
			// Replace 0.0.0.0 with 127.0.0.1 for local connections
			srvaddr = strings.Replace(srvaddr, "0.0.0.0:", "127.0.0.1:", 1)
		}
		ctx.Log.Info("writing local cluster config", "cluster-name", cfg.Server.GetConfigClusterName(), "server-address", srvaddr)
		if err := writeLocalClusterConfig(ctx, cert, srvaddr, cfg.Server.GetConfigClusterName()); err != nil {
			ctx.Log.Warn("failed to write local cluster config", "error", err)
			// Don't fail the whole command if we can't write the config
		}
	}

	ctx.UILog.Info("Miren server started", "address", cfg.Server.GetAddress(), "etcd_endpoints", cfg.Etcd.Endpoints, "etcd_prefix", cfg.Etcd.GetPrefix(), "runner_id", cfg.Server.GetRunnerID())

	ctx.Info("Miren server started successfully! You can now connect to the cluster using `-C %s`\n", cfg.Server.GetConfigClusterName())
	ctx.Info("For example: cd my-app && miren deploy -C %s", cfg.Server.GetConfigClusterName())

	// Set up signal handling for graceful drain on SIGUSR2
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR2)

	eg.Go(func() error {
		select {
		case <-sub.Done():
			return nil
		case sig := <-sigChan:
			ctx.Log.Info("received signal, draining runner", "signal", sig)

			// Drain the runner
			drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			if err := r.Drain(drainCtx); err != nil {
				ctx.Log.Error("failed to drain runner", "error", err)
				return err
			}

			ctx.Log.Info("runner drained successfully, shutting down")
			return fmt.Errorf("runner drained, shutting down")
		}
	})

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
	if err := mainConfig.SaveTo(filepath.Join(filepath.Dir(configDirPath), "clientconfig.yaml")); err != nil {
		return fmt.Errorf("failed to save local cluster config: %w", err)
	}

	// Fix file ownership for the created files if running under sudo
	// Fix main config file
	mainConfigPath := filepath.Join(homeDir, ".config", "miren", "clientconfig.yaml")
	if _, err := os.Stat(mainConfigPath); err == nil {
		// Main config file exists, fix its permissions and ownership
		if err := os.Chmod(mainConfigPath, 0600); err != nil {
			ctx.Log.Warn("failed to set main config file permissions", "error", err)
		}
		if err := fixOwnershipIfSudo(ctx, mainConfigPath); err != nil {
			ctx.Log.Warn("failed to fix main config file ownership", "error", err)
		}
	}

	// Fix leaf config file
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
