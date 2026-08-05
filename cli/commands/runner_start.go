//go:build linux

package commands

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/clientconfig"
	containerdcomp "miren.dev/runtime/components/containerd"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/runnerconfig"
	"miren.dev/runtime/servers/runnertelemetry"
)

func RunnerStart(ctx *Context, opts struct {
	ConfigPath       string `long:"config" description:"Path to runner config" default:"/var/lib/miren/runner/config.yaml"`
	DataPath         string `long:"data-path" description:"Path to store runner data" default:"/var/lib/miren/runner"`
	ContainerdSocket string `long:"containerd-socket" description:"Path to containerd socket"`
	ListenAddr       string `short:"l" long:"listen" description:"Address this runner will listen on (overrides config)"`
}) error {
	// Load saved runner configuration
	cfg, err := runnerconfig.Load(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load runner config: %w (did you run 'miren runner join' first?)", err)
	}

	ctx.Log.Info("starting distributed runner",
		"runner_id", cfg.RunnerID,
		"coordinator", cfg.CoordinatorAddress,
		"etcd_endpoints", cfg.EtcdEndpoints)

	// Determine listen address. If no explicit address is given, discover the
	// machine's outbound IP (the one that would route to the coordinator) and
	// advertise that so the coordinator knows how to reach this runner.
	listenAddr := opts.ListenAddr
	if listenAddr == "" {
		port := "8444"
		ip, err := discoverOutboundIP(cfg.CoordinatorAddress)
		if err != nil {
			return fmt.Errorf("could not discover outbound IP for listen address (use --listen to set manually): %w", err)
		}
		listenAddr = net.JoinHostPort(ip.String(), port)
		ctx.Log.Info("discovered listen address", "addr", listenAddr)
	}

	// The runner's certificate is persisted in the config (often on a disk that
	// outlives the VM). Before serving, reconcile it: refresh it if the listen
	// address isn't covered (fatal — a stale cert leaves the runner unreachable),
	// proactively rotate it if it's past its renewal threshold, and warn if its
	// CommonName has drifted from the runner_id. Rotation takes effect on this
	// start; the cert baked into the serving stack below is the reconciled one.
	if err := reconcileRunnerCertificate(ctx, cfg, opts.ConfigPath, listenAddr); err != nil {
		return err
	}

	// Create clientconfig from saved certs for RPC authentication
	clientCfg := clientconfig.NewConfig()
	clientCfg.SetCluster("coordinator", &clientconfig.ClusterConfig{
		Hostname:   cfg.CoordinatorAddress,
		CACert:     cfg.CACert,
		ClientCert: cfg.ClientCert,
		ClientKey:  cfg.ClientKey,
	})
	clientCfg.SetActiveCluster("coordinator")

	var cc *containerd.Client

	if opts.ContainerdSocket != "" {
		// Explicit socket provided — connect to external containerd
		ctx.Log.Info("connecting to external containerd", "socket", opts.ContainerdSocket)
		cc, err = containerd.New(opts.ContainerdSocket,
			containerd.WithDefaultNamespace("miren"))
		if err != nil {
			return fmt.Errorf("failed to connect to containerd: %w", err)
		}
		defer cc.Close()
	} else {
		// Start embedded containerd (same as server standalone mode)
		var (
			containerdBinary string
			binDir           string
		)

		if releasePath := FindReleasePath(); releasePath != "" {
			candidate := filepath.Join(releasePath, "containerd")
			if _, err := os.Stat(candidate); err == nil {
				binDir = releasePath
				containerdBinary = candidate
			}
		}
		if containerdBinary == "" {
			var err error
			containerdBinary, err = exec.LookPath("containerd")
			if err != nil {
				return fmt.Errorf("containerd binary not found in PATH or release directory: %w", err)
			}
		}

		containerdComponent := containerdcomp.NewContainerdComponent(ctx.Log, opts.DataPath)

		envPath := os.Getenv("PATH")
		if binDir != "" {
			envPath = binDir + ":" + envPath
		}

		baseDir := filepath.Join(opts.DataPath, "containerd")
		containerdConfig := &containerdcomp.Config{
			BinaryPath: containerdBinary,
			BaseDir:    baseDir,
			BinDir:     binDir,
			SocketPath: filepath.Join(baseDir, "containerd.sock"),
			Env:        []string{"PATH=" + envPath},
		}

		if err := containerdComponent.Start(ctx, containerdConfig); err != nil {
			return fmt.Errorf("failed to start embedded containerd: %w", err)
		}
		defer func() {
			ctx.Log.Info("stopping embedded containerd")
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := containerdComponent.Stop(stopCtx); err != nil {
				ctx.Log.Error("failed to stop embedded containerd", "error", err)
			}
		}()

		cc, err = containerd.New(containerdComponent.SocketPath(),
			containerd.WithDefaultNamespace("miren"))
		if err != nil {
			return fmt.Errorf("failed to connect to embedded containerd: %w", err)
		}
		defer cc.Close()

		ctx.Log.Info("embedded containerd started", "socket", containerdComponent.SocketPath())
	}

	// Ensure data directory exists
	if err := os.MkdirAll(opts.DataPath, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Set up signal handling
	sigCtx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create errgroup for background tasks
	eg, egCtx := errgroup.WithContext(sigCtx)

	// Build runner configuration
	runnerCfg := runner.RunnerConfig{
		Id:            cfg.RunnerID,
		Name:          cfg.Name,
		ListenAddress: listenAddr,
		Workers:       runner.DefaulWorkers,
		DataPath:      opts.DataPath,
		Config:        clientCfg,
		DiskMode:      cfg.DiskMode,
	}

	// Create resolver for network operations and map cluster.local to the
	// coordinator so the runner can pull images from the coordinator's registry.
	resolver, hostMapper := netresolve.NewLocalResolver()
	coordinatorHost, coordinatorPort, _ := net.SplitHostPort(cfg.CoordinatorAddress)
	coordinatorAddr, resolveErr := resolveHost(coordinatorHost)
	if resolveErr != nil {
		ctx.Log.Warn("could not resolve coordinator address", "host", coordinatorHost, "error", resolveErr)
	} else {
		hostMapper.SetHost("cluster.local", coordinatorAddr)
		ctx.Log.Info("mapped cluster.local to coordinator", "hostname", coordinatorHost, "addr", coordinatorAddr)
	}

	// Build runner dependencies
	// Note: Some deps like NetServ require entity access client which we get after connecting
	// The runner will set up its own entity client connection via RPC
	deps := runner.RunnerDeps{
		CC:        cc,
		Namespace: "miren",
		Bridge:    "rt0",
		Tempdir:   os.TempDir(),

		// Distributed runner doesn't use local network for sandboxes
		DisableLocalNet: true,

		LogsMaintainer: observability.NewLogsMaintainer(),
		StatusMon:      observability.NewStatusMonitor(ctx.Log),

		// Resolver for network operations
		Resolver: resolver,

		// Service prefixes - same as coordinator
		// TODO: These should come from join response
		ServicePrefixes: []netip.Prefix{
			netip.MustParsePrefix("10.10.0.0/16"),
			netip.MustParsePrefix("fd47:cafe:d00d::/64"),
		},

		// Flannel network configuration
		EtcdEndpoints: cfg.EtcdEndpoints,
		EtcdPrefix:    cfg.EtcdPrefix,
	}

	// Sandboxes here reach the API on the coordinator, not on this host, so
	// point them at the coordinator rather than the local bridge router (which
	// fronts only this runner's own services). It must be an IP: sandbox DNS
	// resolves app.miren names and nothing else, so the coordinator's hostname
	// would not resolve inside the container.
	if resolveErr == nil && coordinatorPort != "" {
		deps.ApiAddress = net.JoinHostPort(coordinatorAddr.String(), coordinatorPort)
		deps.CACert = []byte(cfg.CACert)
		ctx.Log.Info("sandboxes will reach the cluster API at", "address", deps.ApiAddress)
	} else {
		ctx.Log.Warn("in-cluster API access disabled: no usable coordinator address",
			"coordinator", cfg.CoordinatorAddress)
	}

	// Write etcd TLS certs to disk for flannel (which requires file paths)
	if cfg.ClientCert != "" && cfg.ClientKey != "" && cfg.CACert != "" {
		etcdCertsDir := filepath.Join(opts.DataPath, "etcd-certs")
		if err := os.MkdirAll(etcdCertsDir, 0700); err != nil {
			return fmt.Errorf("failed to create etcd certs directory: %w", err)
		}
		certFile := filepath.Join(etcdCertsDir, "client.crt")
		keyFile := filepath.Join(etcdCertsDir, "client.key")
		caFile := filepath.Join(etcdCertsDir, "ca.crt")
		if err := os.WriteFile(certFile, []byte(cfg.ClientCert), 0644); err != nil {
			return fmt.Errorf("failed to write etcd client cert: %w", err)
		}
		if err := os.WriteFile(keyFile, []byte(cfg.ClientKey), 0600); err != nil {
			return fmt.Errorf("failed to write etcd client key: %w", err)
		}
		if err := os.WriteFile(caFile, []byte(cfg.CACert), 0644); err != nil {
			return fmt.Errorf("failed to write etcd CA cert: %w", err)
		}
		deps.EtcdTLSCertFile = certFile
		deps.EtcdTLSKeyFile = keyFile
		deps.EtcdTLSCAFile = caFile
	}

	// Initialize observability subsystems. Telemetry goes to the coordinator's
	// ingest endpoints rather than to VictoriaMetrics and VictoriaLogs
	// directly, so neither of those has to listen anywhere this runner can
	// reach. The addresses the coordinator sends at Join are no longer dialed;
	// their presence is only the signal that the cluster records telemetry at
	// all.
	//
	// The token source is armed after Start, because a distributed runner mints
	// through the coordinator and has no issuer until it has connected. Writes
	// before then fail loudly rather than dropping on the floor; in practice
	// the first flush is seconds away and the connection is up well before it.
	tokenSource := runnertelemetry.NewIssuerTokenSource()

	var telemetryClient *runnertelemetry.Client
	if cfg.VictoriametricsAddress != "" || cfg.VictorialogsAddress != "" {
		telemetryClient, err = runnertelemetry.NewClient(runnertelemetry.ClientConfig{
			ClientCertPEM: []byte(cfg.ClientCert),
			ClientKeyPEM:  []byte(cfg.ClientKey),
			CACertPEM:     []byte(cfg.CACert),
			TokenSource:   tokenSource,
			Timeout:       30 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("failed to build telemetry client: %w", err)
		}
		defer func() {
			if err := telemetryClient.Close(); err != nil {
				ctx.Log.Error("failed to close telemetry client", "error", err)
			}
		}()
	}

	var metricsWriter *metrics.VictoriaMetricsWriter
	if telemetryClient != nil && cfg.VictoriametricsAddress != "" {
		metricsURL := runnertelemetry.MetricsURL(cfg.CoordinatorAddress)
		metricsWriter = metrics.NewVictoriaMetricsWriter(ctx.Log, metricsURL, 30*time.Second,
			metrics.WithHTTPClient(telemetryClient.HTTP))
		metricsWriter.Start()
		defer func() {
			if err := metricsWriter.Close(); err != nil {
				ctx.Log.Error("failed to close metrics writer", "error", err)
			}
		}()
		ctx.Log.Info("metrics writer started", "endpoint", metricsURL)
	} else {
		ctx.Log.Warn("no VictoriaMetrics address configured, sandbox metrics will not be recorded")
	}

	sbMetrics := sandbox.NewMetrics()
	sbMetrics.Log = ctx.Log
	sbMetrics.CPUUsage = metrics.NewCPUUsage(ctx.Log, metricsWriter, nil)
	sbMetrics.MemUsage = metrics.NewMemoryUsage(ctx.Log, metricsWriter, nil)
	deps.SandboxMetrics = sbMetrics

	if telemetryClient != nil && cfg.VictorialogsAddress != "" {
		logsURL := runnertelemetry.LogsURL(cfg.CoordinatorAddress)
		logWriter := observability.NewPersistentLogWriter(logsURL, 30*time.Second,
			observability.WithHTTPClient(telemetryClient.HTTP))

		// Batch on this path. One HTTP/3 round trip per log line to the
		// coordinator costs far more than the unbatched POST to a local
		// VictoriaLogs did, and a failure here is worth hearing about: the
		// runner's own log goes to the journal, not through this writer, so
		// reporting it cannot feed back into the buffer it is reporting on.
		batchWriter := observability.NewBatchLogWriter(logWriter,
			observability.WithBatchErrorHandler(func(err error, dropped int) {
				ctx.Log.Error("failed to ship sandbox logs", "error", err, "dropped", dropped)
			}))
		defer batchWriter.Close()

		deps.LogWriter = batchWriter
		ctx.Log.Info("log writer started", "endpoint", logsURL)
	} else {
		deps.LogWriter = observability.NewDebugLogWriter(ctx.Log)
		ctx.Log.Warn("no VictoriaLogs address configured, sandbox logs will only be written to debug output")
	}

	// Create runner
	r, err := runner.NewRunner(ctx.Log, deps, runnerCfg)
	if err != nil {
		return fmt.Errorf("failed to create runner: %w", err)
	}

	// Start runner with errgroup for background network tasks
	if err := r.Start(egCtx, eg); err != nil {
		return fmt.Errorf("failed to start runner: %w", err)
	}

	// Start acquired the coordinator-backed issuer, so telemetry can now mint.
	if issuer := r.WorkloadIssuer(); issuer != nil {
		tokenSource.SetIssuer(issuer)
	} else if telemetryClient != nil {
		ctx.Log.Error("no workload identity issuer available; telemetry cannot be shipped")
	}

	ctx.Log.Info("runner started successfully",
		"runner_id", cfg.RunnerID,
		"listen_address", listenAddr)

	// Wait for shutdown signal or error
	<-egCtx.Done()
	ctx.Log.Info("shutting down runner")

	// Close runner
	if err := r.Close(); err != nil {
		ctx.Log.Error("error closing runner", "error", err)
	}

	// Wait for background tasks to complete
	if err := eg.Wait(); err != nil && err != context.Canceled {
		ctx.Log.Error("background task error", "error", err)
	}

	ctx.Log.Info("runner stopped")
	return nil
}

// certRenewalFraction is the fraction of a certificate's total validity after
// which the runner proactively rotates it. Rotating at two-thirds of the lifetime
// leaves roughly a third of the validity as runway to authenticate the renewal
// (which uses the still-valid current cert), keeping the runner clear of the hard
// expiry cliff where it can no longer refresh itself.
const certRenewalFraction = 2.0 / 3.0

// reconcileRunnerCertificate inspects the persisted runner certificate at startup
// and brings it into a good state before the runner serves. It handles three
// conditions: a listen address the cert's SANs don't cover (refresh, fatal on
// failure because a stale cert leaves the runner unreachable), a cert past its
// renewal threshold (proactive rotation, best-effort since a still-valid cert
// isn't worth failing startup over), and a CommonName that has drifted from the
// runner_id (warn — workload tokens will be denied until the runner is
// re-provisioned).
//
// An already-expired certificate is fatal: it can't be refreshed (the expired
// cert is what would authenticate the refresh), so the runner can't recover in
// place and must be re-provisioned. Rotation and refresh use RefreshCertificate,
// which preserves the cert's CommonName, so a drifted CommonName can't be repaired
// here either and is surfaced as a warning.
func reconcileRunnerCertificate(ctx *Context, cfg *runnerconfig.Config, configPath, listenAddr string) error {
	if cfg.ClientCert == "" {
		return nil
	}

	warnIfCertCommonNameDrifted(ctx, cfg)

	expired, err := certExpired(cfg.ClientCert)
	if err != nil {
		return fmt.Errorf("failed to inspect runner certificate: %w", err)
	}
	if expired {
		return fmt.Errorf("runner certificate has expired and cannot be rotated in place; "+
			"re-provision with 'miren runner remove' then 'miren runner join --runner-id %s'", cfg.RunnerID)
	}

	covered, err := certCoversListenAddr(cfg.ClientCert, listenAddr)
	if err != nil {
		return fmt.Errorf("failed to inspect runner certificate: %w", err)
	}

	if !covered {
		ctx.Log.Info("runner certificate does not cover listen address; refreshing",
			"listen_addr", listenAddr)
		// Fatal on failure: serving a cert that doesn't cover our address leaves
		// the runner silently unreachable.
		return refreshRunnerCertificate(ctx, cfg, configPath, listenAddr)
	}

	needsRotation, err := certPastRenewalThreshold(cfg.ClientCert)
	if err != nil {
		ctx.Log.Warn("could not evaluate runner certificate expiry; skipping rotation", "error", err)
		return nil
	}
	if needsRotation {
		ctx.Log.Info("runner certificate is past its renewal threshold; rotating",
			"listen_addr", listenAddr)
		// Best-effort: the cert is still valid, so a momentarily unavailable
		// coordinator shouldn't block startup. We'll try again on the next start.
		if err := refreshRunnerCertificate(ctx, cfg, configPath, listenAddr); err != nil {
			ctx.Log.Warn("proactive certificate rotation failed; will retry on next start", "error", err)
		}
	}

	return nil
}

// refreshRunnerCertificate asks the coordinator to re-issue the runner's
// certificate (preserving its CommonName) with SANs for listenAddr, then writes
// the new material back to the config so it survives restarts.
func refreshRunnerCertificate(ctx *Context, cfg *runnerconfig.Config, configPath, listenAddr string) error {
	cs, err := rpc.NewState(ctx,
		rpc.WithLogger(ctx.Log),
		rpc.WithBindAddr("[::]:0"),
		rpc.WithCertPEMs([]byte(cfg.ClientCert), []byte(cfg.ClientKey)),
		rpc.WithCertificateVerification([]byte(cfg.CACert)),
	)
	if err != nil {
		return fmt.Errorf("failed to create RPC state for certificate refresh: %w", err)
	}
	defer cs.Close()

	client, err := cs.Connect(cfg.CoordinatorAddress, rpc.ServiceRunner)
	if err != nil {
		return fmt.Errorf("failed to connect to coordinator for certificate refresh: %w", err)
	}
	defer client.Close()

	rc := runner_v1alpha.NewRunnerRegistrationClient(client)
	res, err := rc.RefreshCertificate(ctx, listenAddr)
	if err != nil {
		return fmt.Errorf("certificate refresh request failed: %w", err)
	}
	if res.Error() != "" {
		return fmt.Errorf("certificate refresh rejected by coordinator: %s", res.Error())
	}
	if len(res.CertPem()) == 0 || len(res.KeyPem()) == 0 {
		return fmt.Errorf("coordinator returned an empty certificate")
	}

	cfg.ClientCert = string(res.CertPem())
	cfg.ClientKey = string(res.KeyPem())
	if len(res.CaPem()) > 0 {
		cfg.CACert = string(res.CaPem())
	}

	if err := cfg.Save(configPath); err != nil {
		return fmt.Errorf("failed to save refreshed certificate: %w", err)
	}

	ctx.Log.Info("runner certificate refreshed", "listen_addr", listenAddr)
	return nil
}

// parseLeafCertificate decodes the first PEM block in certPEM and returns the
// parsed leaf certificate.
func parseLeafCertificate(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate: %w", err)
	}
	return cert, nil
}

// certExpired reports whether the leaf certificate in certPEM is past its
// NotAfter. An expired cert can't authenticate a refresh, so the runner can't
// recover it in place.
func certExpired(certPEM string) (bool, error) {
	cert, err := parseLeafCertificate(certPEM)
	if err != nil {
		return false, err
	}
	return time.Now().After(cert.NotAfter), nil
}

// certPastRenewalThreshold reports whether the leaf certificate in certPEM is past
// certRenewalFraction of its total validity and should be proactively rotated.
func certPastRenewalThreshold(certPEM string) (bool, error) {
	cert, err := parseLeafCertificate(certPEM)
	if err != nil {
		return false, err
	}

	lifetime := cert.NotAfter.Sub(cert.NotBefore)
	if lifetime <= 0 {
		return false, nil
	}
	renewAfter := cert.NotBefore.Add(time.Duration(float64(lifetime) * certRenewalFraction))
	return time.Now().After(renewAfter), nil
}

// warnIfCertCommonNameDrifted logs a loud warning when the persisted certificate's
// CommonName no longer matches runner-<runner_id>. This is the state that silently
// broke workload identity on older runners (MIR-1228): the coordinator authorizes
// workload-token requests by the cert CommonName, so a drifted name is denied.
// In-place rotation preserves the CommonName and so can't repair a drifted one, so
// we direct the operator to re-provision the runner, which mints a fresh cert with
// the current naming scheme while keeping the same runner_id.
func warnIfCertCommonNameDrifted(ctx *Context, cfg *runnerconfig.Config) {
	if cfg.RunnerID == "" {
		return
	}
	cn, err := certCommonName(cfg.ClientCert)
	if err != nil {
		return
	}

	// Mirrors the coordinator's runnerCertName scheme (runner-<full runner_id>).
	want := "runner-" + cfg.RunnerID
	if cn != want {
		ctx.Log.Warn("runner certificate CommonName does not match runner_id; workload identity tokens will be denied. Re-provision this runner ('miren runner remove' then 'miren runner join' reusing its runner_id) to mint a cert with the current naming scheme.",
			"cert_common_name", cn,
			"expected_common_name", want,
			"runner_id", cfg.RunnerID)
	}
}

// certCommonName returns the CommonName of the leaf certificate in certPEM.
func certCommonName(certPEM string) (string, error) {
	cert, err := parseLeafCertificate(certPEM)
	if err != nil {
		return "", err
	}
	return cert.Subject.CommonName, nil
}

// certCoversListenAddr reports whether the leaf certificate in certPEM carries a
// SAN matching the host of listenAddr: an IP SAN for an IP host, or a DNS SAN
// for a hostname. This mirrors how the coordinator builds the certificate's SANs
// from the listen address.
func certCoversListenAddr(certPEM, listenAddr string) (bool, error) {
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil {
		host = listenAddr
	}
	if host == "" {
		return true, nil
	}

	cert, err := parseLeafCertificate(certPEM)
	if err != nil {
		return false, err
	}

	if ip := net.ParseIP(host); ip != nil {
		for _, certIP := range cert.IPAddresses {
			if certIP.Equal(ip) {
				return true, nil
			}
		}
		return false, nil
	}

	for _, name := range cert.DNSNames {
		if name == host {
			return true, nil
		}
	}
	return false, nil
}

// resolveHost turns a host that may be either a literal IP or a DNS name into
// an address. Callers need a literal IP: it feeds the local resolver and the
// API address handed to sandboxes, and sandbox DNS answers only app.miren
// names, so a hostname would not resolve inside a container.
func resolveHost(host string) (netip.Addr, error) {
	if host == "" {
		return netip.Addr{}, fmt.Errorf("empty host")
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		return addr, nil
	}

	addrs, err := net.LookupHost(host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("resolving %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return netip.Addr{}, fmt.Errorf("resolving %q: no addresses", host)
	}

	addr, err := netip.ParseAddr(addrs[0])
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parsing resolved address %q for %q: %w", addrs[0], host, err)
	}

	return addr, nil
}
