//go:build linux

package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/autotls"
	"miren.dev/runtime/components/buildkit"
	containerdcomp "miren.dev/runtime/components/containerd"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/components/ipalloc"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/ocireg"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/components/victorialogs"
	"miren.dev/runtime/components/victoriametrics"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/grunge"
	"miren.dev/runtime/pkg/ipdiscovery"
	"miren.dev/runtime/pkg/labs"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/units"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/version"
)

// pprofAddr is the localhost-only address for the diagnostic pprof endpoint.
//
// NOTE: hard-coded, and this assumes a single miren instance per host. If a
// second instance runs on the same host (e.g. a blue/green deploy) its bind
// will fail and it will run without pprof, logging an error at startup.
const pprofAddr = "127.0.0.1:6060"

// startPprofServer starts the diagnostic net/http/pprof endpoint on pprofAddr so
// a heap profile can be pulled live during a memory balloon without restarting
// the process.
//
// The pprof handlers are registered on a private ServeMux, not the global
// http.DefaultServeMux, so nothing else in the process can inadvertently expose
// handlers on this listener. The server shuts down cleanly when ctx is
// cancelled, matching the rest of the server's lifecycle.
//
// pprof is diagnostic-only, so a bind failure is logged as an error (operators
// need to know it is unavailable) but does not abort server startup.
func startPprofServer(ctx context.Context, log *slog.Logger) {
	mux := http.NewServeMux()
	// Mirror exactly what net/http/pprof's init would register on the default
	// mux: Index serves the index and the named profiles (heap, goroutine, ...),
	// the other four are the endpoints Index does not handle.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	ln, err := net.Listen("tcp", pprofAddr)
	if err != nil {
		log.Error("pprof debug server not started", "addr", pprofAddr, "err", err)
		return
	}

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("pprof debug server exited", "err", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warn("pprof debug server shutdown", "err", err)
		}
	}()
}

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
	if err := cfg.ValidateIngressCoherence(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	cfg.WarnDeprecatedConfig(ctx.Log)

	// Initialize Miren Labs feature flags
	labs.Init(ctx.Log, cfg.Labs)

	versionInfo := version.GetInfo()
	ctx.UILog.Info("starting miren server", "version", versionInfo.Version, "commit", versionInfo.Commit)

	// Discover local and public IPs early so they're available for TLS cert SANs.
	// IPs are tagged with whether they are user-configured (explicit) or
	// auto-discovered from interfaces. The coordinator uses this to decide
	// which IPs to prune (discovered) vs always include (explicit).
	ipSet := coordinate.NewIPSet()
	discovery, err := ipdiscovery.DiscoverWithTimeout(10*time.Second, ctx.Log, ipdiscovery.Options{
		NetcheckURL: coordinate.DefaultCloudURL,
	})
	if err != nil {
		ctx.Log.Warn("failed to discover IPs", "error", err)
	} else {
		for _, addr := range discovery.Addresses {
			ip := net.ParseIP(addr.IP)
			if ip != nil && !ip.IsLinkLocalUnicast() {
				ipSet.AddDiscovered(ip)
			}
		}
		ctx.Log.Info("discovered IPs", "addresses", len(discovery.Addresses))
	}

	// Parse explicit AdditionalIPs up front so they're included in TLS cert
	// SANs everywhere — both the API cert and the etcd cert (used by
	// distributed runners), the latter of which is built earlier in startup.
	for _, ip := range cfg.TLS.AdditionalIPs {
		addr := net.ParseIP(ip)
		if addr == nil {
			ctx.Log.Error("failed to parse additional IP", "ip", ip)
			return fmt.Errorf("failed to parse additional IP %s", ip)
		}
		ipSet.AddExplicit(addr)
	}

	switch cfg.GetMode() {
	case "standalone":
		// Mode defaults are already applied by serverconfig.Load
		// No need to manually set StartEmbedded flags here

		// Determine release path
		if cfg.Server.GetReleasePath() == "" {
			if releasePath := FindReleasePath(); releasePath != "" {
				cfg.Server.SetReleasePath(releasePath)
				ctx.Log.Info("using release path", "path", releasePath)
			} else {
				// No release directory found, try to download one
				ctx.Log.Info("no release directory found, downloading release")

				// Determine where to download based on permissions
				downloadGlobal := false
				downloadPath := ""

				// Get user release path for potential fallback
				var userReleasePath string
				if homeDir, err := getUserHomeDir(); err == nil {
					userReleasePath = filepath.Join(homeDir, ".miren", "release")
				}

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

				branch := "latest"
				if br := version.Branch(); br != "" {
					branch = br
				}

				// Download the release
				if err := PerformDownloadRelease(ctx, DownloadReleaseOptions{
					Branch: branch,
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

		ctx.ServerState.ContainerdSocket = containerdComponent.SocketPath()
	} else {
		// Use existing containerd with provided or default socket path
		if cfg.Containerd.GetSocketPath() != "" {
			ctx.ServerState.ContainerdSocket = cfg.Containerd.GetSocketPath()
		}
		// else keep the default from NewServerState()
	}

	// Initialize containerd client
	if err := ctx.ServerState.InitContainerd(); err != nil {
		ctx.Log.Error("failed to create containerd client", "error", err)
		return err
	}

	// etcdTLSSetup holds etcd TLS configuration when distributed runners is enabled
	var etcdTLSSetup *coordinate.EtcdTLSSetupResult

	// etcdComponent is hoisted so the metrics writer can be attached later, once the
	// metrics subsystem is initialized (which happens after etcd has started).
	var etcdComponent *etcd.EtcdComponent

	// Start embedded etcd server if requested
	if cfg.Etcd.GetStartEmbedded() {
		ctx.Log.Info("starting embedded etcd server", "client-port", cfg.Etcd.GetClientPort(), "peer-port", cfg.Etcd.GetPeerPort())

		etcdComponent = etcd.NewEtcdComponent(ctx.Log, ctx.ServerState.CC, ctx.ServerState.Namespace, cfg.Server.GetDataPath())

		etcdConfig := etcd.EtcdConfig{
			Name:              "miren-etcd",
			ClientPort:        cfg.Etcd.GetClientPort(),
			HTTPClientPort:    cfg.Etcd.GetHTTPClientPort(),
			PeerPort:          cfg.Etcd.GetPeerPort(),
			ClusterState:      "new",
			QuotaBackendBytes: int64(cfg.Etcd.GetQuotaBackendBytes()),
		}

		// Set up etcd TLS when distributed runners feature is enabled
		if labs.DistributedRunners() {
			ctx.Log.Info("setting up etcd mTLS for distributed runners")

			// SetupEtcdTLS reads the CA from disk and requires it to already
			// exist. On a fresh install the coordinator's own create-if-missing
			// LoadCA doesn't run until much later (Coordinator.Start), so ensure
			// the CA is materialized here first. Otherwise the first boot fails
			// "CA certificate not found" and crash-loops until an out-of-band
			// auth generate creates it.
			if _, err := coordinate.EnsureCA(ctx.Log, cfg.Server.GetDataPath()); err != nil {
				ctx.Log.Error("failed to ensure CA for etcd TLS", "error", err)
				return err
			}

			var err error
			etcdTLSSetup, err = coordinate.SetupEtcdTLS(ctx.Log, cfg.Server.GetDataPath(), cfg.TLS.AdditionalNames, ipSet.RawIPs())
			if err != nil {
				ctx.Log.Error("failed to set up etcd TLS", "error", err)
				return err
			}
			etcdConfig.TLS = &etcd.TLSConfig{
				CertsDir: etcdTLSSetup.CertsDir,
			}
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

	// Start embedded VictoriaLogs server if requested
	if cfg.Victorialogs.GetStartEmbedded() {
		ctx.Log.Info("starting embedded victorialogs server", "http-port", cfg.Victorialogs.GetHTTPPort())

		victoriaLogsComponent := victorialogs.NewVictoriaLogsComponent(ctx.Log, ctx.ServerState.CC, ctx.ServerState.Namespace, cfg.Server.GetDataPath())

		victoriaLogsConfig := victorialogs.VictoriaLogsConfig{
			HTTPPort:        cfg.Victorialogs.GetHTTPPort(),
			RetentionPeriod: cfg.Victorialogs.GetRetentionPeriod(),
		}

		err = victoriaLogsComponent.Start(sub, victoriaLogsConfig)
		if err != nil {
			ctx.Log.Error("failed to start victorialogs component", "error", err)
			return err
		}

		ctx.Log.Info("embedded victorialogs started", "http-endpoint", victoriaLogsComponent.HTTPEndpoint())

		// Update ServerState with embedded VictoriaLogs address
		ctx.ServerState.VictorialogsAddress = victoriaLogsComponent.HTTPEndpoint()
		ctx.ServerState.VictorialogsTimeout = 30 * time.Second

		// Ensure cleanup on exit
		defer func() {
			ctx.Log.Info("stopping embedded victorialogs")
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := victoriaLogsComponent.Stop(stopCtx); err != nil {
				ctx.Log.Error("failed to stop victorialogs component", "error", err)
			}
		}()
	} else {
		if cfg.Victorialogs.GetAddress() == "" {
			ctx.Log.Error("victorialogs address not specified and embedded victorialogs not started")
			return fmt.Errorf("victorialogs address not specified and embedded victorialogs not started")
		}

		// Update ServerState with external VictoriaLogs address
		ctx.Log.Info("using external victorialogs", "address", cfg.Victorialogs.GetAddress())
		ctx.ServerState.VictorialogsAddress = cfg.Victorialogs.GetAddress()
		ctx.ServerState.VictorialogsTimeout = 30 * time.Second
	}

	// Start embedded VictoriaMetrics server if requested
	if cfg.Victoriametrics.GetStartEmbedded() {
		ctx.Log.Info("starting embedded victoriametrics server", "http-port", cfg.Victoriametrics.GetHTTPPort())

		victoriaMetricsComponent := victoriametrics.NewVictoriaMetricsComponent(ctx.Log, ctx.ServerState.CC, ctx.ServerState.Namespace, cfg.Server.GetDataPath())

		victoriaMetricsConfig := victoriametrics.VictoriaMetricsConfig{
			HTTPPort:        cfg.Victoriametrics.GetHTTPPort(),
			RetentionPeriod: cfg.Victoriametrics.GetRetentionPeriod(),
		}

		err = victoriaMetricsComponent.Start(sub, victoriaMetricsConfig)
		if err != nil {
			ctx.Log.Error("failed to start victoriametrics component", "error", err)
			return err
		}

		ctx.Log.Info("embedded victoriametrics started", "http-endpoint", victoriaMetricsComponent.HTTPEndpoint())

		// Update ServerState with embedded VictoriaMetrics address
		ctx.ServerState.VictoriametricsAddress = victoriaMetricsComponent.HTTPEndpoint()
		ctx.ServerState.VictoriametricsTimeout = 30 * time.Second

		// Ensure cleanup on exit
		defer func() {
			ctx.Log.Info("stopping embedded victoriametrics")

			// First, close the writer to flush any remaining metrics
			if ctx.ServerState.Writer != nil {
				ctx.Log.Info("flushing and closing victoriametrics writer")
				if err := ctx.ServerState.Writer.Close(); err != nil {
					ctx.Log.Error("failed to close victoriametrics writer", "error", err)
				}
			}

			// Then stop the VictoriaMetrics component
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := victoriaMetricsComponent.Stop(stopCtx); err != nil {
				ctx.Log.Error("failed to stop victoriametrics component", "error", err)
			}
		}()
	} else {
		if cfg.Victoriametrics.GetAddress() == "" {
			ctx.Log.Error("victoriametrics address not specified and embedded victoriametrics not started")
			return fmt.Errorf("victoriametrics address not specified and embedded victoriametrics not started")
		}

		// Update ServerState with external VictoriaMetrics address
		ctx.Log.Info("using external victoriametrics", "address", cfg.Victoriametrics.GetAddress())
		ctx.ServerState.VictoriametricsAddress = cfg.Victoriametrics.GetAddress()
		ctx.ServerState.VictoriametricsTimeout = 30 * time.Second
	}

	// BuildKit component (nil if not configured)
	var buildkitComponent *buildkit.Component

	// Start embedded BuildKit daemon if requested
	if cfg.Buildkit.GetStartEmbedded() {
		ctx.Log.Info("starting embedded buildkit daemon", "socket-dir", cfg.Buildkit.GetSocketDir())

		buildkitComponent = buildkit.NewComponent(ctx.Log, ctx.ServerState.CC, ctx.ServerState.Namespace, cfg.Server.GetDataPath())

		// Parse GC settings
		gcStorage, _ := units.ParseData(cfg.Buildkit.GetGcKeepStorage())
		gcDuration, _ := units.ParseDuration(cfg.Buildkit.GetGcKeepDuration())

		// Default socket directory to data_path/buildkit/socket if not set
		socketDir := cfg.Buildkit.GetSocketDir()
		if socketDir == "" {
			socketDir = filepath.Join(cfg.Server.GetDataPath(), "buildkit", "socket")
		}

		buildkitConfig := buildkit.Config{
			SocketDir:      socketDir,
			GCKeepStorage:  int64(gcStorage.Bytes()),
			GCKeepDuration: int64(gcDuration.Seconds()),
			RegistryHost:   "cluster.local:5000",
		}

		err = buildkitComponent.Start(sub, buildkitConfig)
		if err != nil {
			ctx.Log.Error("failed to start buildkit component", "error", err)
			return err
		}

		ctx.Log.Info("embedded buildkit started", "socket-path", buildkitComponent.SocketPath())

		// Ensure cleanup on exit
		defer func() {
			ctx.Log.Info("stopping embedded buildkit")
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := buildkitComponent.Stop(stopCtx); err != nil {
				ctx.Log.Error("failed to stop buildkit component", "error", err)
			}
		}()
	} else if cfg.Buildkit.GetSocketPath() != "" {
		// Use external BuildKit daemon
		ctx.Log.Info("using external buildkit daemon", "socket", cfg.Buildkit.GetSocketPath())

		buildkitComponent = buildkit.NewExternalComponent(ctx.Log, cfg.Buildkit.GetSocketPath())

		err := buildkitComponent.Start(sub, buildkit.Config{})
		if err != nil {
			ctx.Log.Error("failed to connect to external buildkit", "error", err)
			return err
		}

		ctx.Log.Info("connected to external buildkit", "socket-path", buildkitComponent.SocketPath())
	}

	klog.SetLogger(logr.FromSlogHandler(ctx.Log.With("module", "global").Handler()))

	res, hm := netresolve.NewLocalResolver()

	// Initialize metrics components via ServerState
	ctx.ServerState.InitMetricsWriter(ctx.Log)
	ctx.ServerState.InitMetricsReader(ctx.Log)

	// The metrics writer only exists now (it needs the VictoriaMetrics address), after
	// etcd and its maintenance loop have already started; attach it so the loop can emit
	// etcd health gauges. The loop reads the writer atomically each tick.
	if etcdComponent != nil {
		etcdComponent.SetMetricsWriter(ctx.ServerState.Writer)
	}

	// Create CPU and memory usage monitors
	cpu := metrics.NewCPUUsage(ctx.Log, ctx.ServerState.Writer, ctx.ServerState.Reader)
	ctx.ServerState.CPU = cpu
	mem := metrics.NewMemoryUsage(ctx.Log, ctx.ServerState.Writer, ctx.ServerState.Reader)
	ctx.ServerState.Mem = mem

	// Control-process runtime memory metrics. The coordinator process is not
	// covered by any per-sandbox cgroup, so this is the only visibility into
	// its own heap/RSS growth. Pushes go_mem_* + process_resident_memory_bytes
	// (entity="miren/control") through the same writer as the sandbox metrics.
	runtimeMem := metrics.NewRuntimeMemory(ctx.Log, ctx.ServerState.Writer)
	go runtimeMem.Monitor(sub)

	// Diagnostic pprof endpoint, bound to localhost only so it is reachable
	// on-box / via SSH tunnel but never publicly. Lets us pull a heap profile
	// during a memory balloon to attribute Go-heap growth to an allocation site.
	startPprofServer(sub, ctx.Log)

	// Create log writer and reader
	logWriter := observability.NewPersistentLogWriter(ctx.ServerState.VictorialogsAddress, ctx.ServerState.VictorialogsTimeout)
	ctx.ServerState.LogWriter = logWriter
	logs := observability.NewLogReader(ctx.ServerState.VictorialogsAddress, ctx.ServerState.VictorialogsTimeout)
	ctx.ServerState.Logs = logs

	// Wait for VictoriaLogs to be ready before teeing server logs to it.
	// VictoriaLogs Start() is async, so it may not be accepting HTTP yet.
	if !observability.WaitForVictoriaLogs(sub, ctx.ServerState.VictorialogsAddress, 30*time.Second) {
		ctx.Log.Warn("VictoriaLogs not ready after 30s, system log ingestion may be delayed")
	}

	// Tee server logs to VictoriaLogs so they're queryable via `miren logs system`.
	// Batch writes to reduce the number of HTTP POSTs under high log volume.
	batchWriter := observability.NewBatchLogWriter(logWriter)
	defer batchWriter.Close()
	systemHandler := observability.NewSystemLogHandler(ctx.Log.Handler(), batchWriter)
	ctx.Log = slog.New(systemHandler)

	// Create HTTP metrics
	httpMetrics := metrics.NewHTTPMetrics(ctx.Log, ctx.ServerState.Writer, ctx.ServerState.Reader)
	ctx.ServerState.HTTPMetrics = httpMetrics

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
		cloudAuthConfig.DNSHostname = reg.DNSHostname
	} else if reg != nil && reg.Status == "pending" {
		ctx.Log.Info("found pending cluster registration",
			"cluster-name", reg.ClusterName,
			"registration-id", reg.RegistrationID,
			"expires-at", reg.ExpiresAt)
	} else {
		ctx.Log.Info("no cluster registration found")
	}

	// Initialize workload identity issuer for sandbox OIDC tokens
	var workloadIssuer *workloadidentity.Issuer
	{
		var issuerURL string
		if cloudAuthConfig.DNSHostname != "" {
			issuerURL = "https://" + cloudAuthConfig.DNSHostname
		} else if len(cfg.TLS.AdditionalNames) > 0 {
			issuerURL = "https://" + cfg.TLS.AdditionalNames[0]
		}
		if issuerURL != "" {
			workloadIssuer, err = workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
				DataPath:       cfg.Server.GetDataPath(),
				IssuerURL:      issuerURL,
				OrganizationID: cloudAuthConfig.Tags["organization_id"],
				ClusterID:      cloudAuthConfig.ClusterID,
			})
			if err != nil {
				ctx.Log.Warn("failed to initialize workload identity issuer", "error", err)
			} else {
				ctx.Log.Info("workload identity issuer initialized", "issuer", issuerURL)
			}
		}
	}

	// Auto-enable OTel tracing when the standard OTLP endpoint env var is set.
	// Placed after registration loading so we can identify the cluster in traces.
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		// Determine cluster identity: registration name → first DNS name → config cluster name
		clusterName := cfg.Server.GetConfigClusterName()
		if cloudAuthConfig.Enabled {
			clusterName = cloudAuthConfig.Tags["cluster_name"]
		} else if len(cfg.TLS.AdditionalNames) > 0 {
			clusterName = cfg.TLS.AdditionalNames[0]
		}

		tracingShutdown, err := rpc.SetupTracing(sub,
			attribute.String("miren.cluster.name", clusterName),
		)
		if err != nil {
			ctx.Log.Error("failed to set up tracing", "error", err)
			return err
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tracingShutdown(shutdownCtx); err != nil {
				ctx.Log.Error("failed to shut down tracing", "error", err)
			}
		}()
		ctx.Log.Info("OTel tracing enabled", "endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), "cluster", clusterName)
	}

	srvaddr := cfg.Server.GetAddress()
	if !strings.HasPrefix(srvaddr, ":") {
		if _, _, err := net.SplitHostPort(srvaddr); err != nil {
			// ok, add the default port.
			srvaddr = net.JoinHostPort(srvaddr, strconv.Itoa(8443))
			ctx.Log.Debug("no port specified in server address, using default 8443", "address", srvaddr)
		}
	}

	// A malformed retention period parses to 0, which the coordinator treats as
	// "use the default" rather than "retain nothing". Warn so the operator knows
	// their configured value was ignored.
	appVersionRetentionPeriod, err := units.ParseDuration(cfg.AppVersion.GetRetentionPeriod())
	if err != nil {
		ctx.Log.Warn("invalid app_version.retention_period, falling back to default",
			"value", cfg.AppVersion.GetRetentionPeriod(), "error", err)
	}

	// Build coordinator config
	coordConfig := coordinate.CoordinatorConfig{
		Address:                   srvaddr,
		EtcdEndpoints:             cfg.Etcd.Endpoints,
		Prefix:                    cfg.Etcd.GetPrefix(),
		DataPath:                  cfg.Server.GetDataPath(),
		AdditionalNames:           cfg.TLS.AdditionalNames,
		IPs:                       ipSet,
		AcmeEmail:                 cfg.TLS.GetAcmeEmail(),
		AcmeDNSProvider:           cfg.TLS.GetAcmeDNSProvider(),
		Resolver:                  res,
		TempDir:                   os.TempDir(),
		CloudAuth:                 cloudAuthConfig,
		Mem:                       mem,
		Cpu:                       cpu,
		HTTP:                      httpMetrics,
		Logs:                      logs,
		LogWriter:                 logWriter,
		BuildKit:                  buildkitComponent,
		HTTPRequestTimeout:        cfg.Server.HTTPRequestTimeoutDuration(),
		VictoriametricsAddress:    ctx.ServerState.VictoriametricsAddress,
		VictorialogsAddress:       ctx.ServerState.VictorialogsAddress,
		WorkloadIssuer:            workloadIssuer,
		AppVersionRetentionCount:  cfg.AppVersion.GetRetentionCount(),
		AppVersionRetentionPeriod: appVersionRetentionPeriod,
	}

	// Pass etcd TLS config when distributed runners is enabled
	if etcdTLSSetup != nil {
		coordConfig.EtcdTLS = etcdTLSSetup.ClientTLS
	}

	// Create coordinator
	co := coordinate.NewCoordinator(ctx.Log, coordConfig)

	err = co.Start(sub)
	if err != nil {
		ctx.Log.Error("failed to start coordinator", "error", err)
		return err
	}

	time.Sleep(time.Second)

	// Set service prefixes in ServerState
	ctx.ServerState.ServicePrefixes = []netip.Prefix{
		netip.MustParsePrefix("10.10.0.0/16"),
		netip.MustParsePrefix("fd47:cafe:d00d::/64"),
	}

	// Initialize netdb before flannel so we can read the previous subnet lease
	dataPath := cfg.Server.GetDataPath()
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dataPath, err)
	}

	ndb, err := netdb.New(filepath.Join(dataPath, "net.db"))
	if err != nil {
		return fmt.Errorf("failed to open netdb: %w", err)
	}

	prevSubnet, err := ndb.GetLeasedSubnet()
	if err != nil {
		return fmt.Errorf("failed to read leased subnet from netdb: %w", err)
	}

	grungeOpts := grunge.NetworkOptions{
		EtcdEndpoints: cfg.Etcd.Endpoints,
		EtcdPrefix:    cfg.Etcd.GetPrefix() + "/sub/flannel",
		PrevIPv4:      prevSubnet,
	}

	// Add TLS config when distributed runners is enabled
	if etcdTLSSetup != nil {
		grungeOpts.TLSCertFile = etcdTLSSetup.ClientCertFile
		grungeOpts.TLSKeyFile = etcdTLSSetup.ClientKeyFile
		grungeOpts.TLSCAFile = etcdTLSSetup.CAFile
	}

	gn, err := grunge.NewNetwork(ctx.Log, grungeOpts)
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
	ctx.ServerState.IPv4Routable = lease.IPv4()

	ctx.Log.Info("leased IP prefixes", "ipv4", lease.IPv4().String(), "ipv6", lease.IPv6().String())

	// Persist the leased subnet so we can pass it as PrevIPv4 on next restart
	if err := ndb.SetLeasedSubnet(lease.IPv4()); err != nil {
		return fmt.Errorf("failed to persist leased subnet: %w", err)
	}

	ctx.ServerState.Subnet, err = ndb.Subnet(lease.IPv4().String())
	if err != nil {
		return fmt.Errorf("failed to create subnet: %w", err)
	}

	// Get router address from subnet
	routerAddr := ctx.ServerState.Subnet.Router().Addr()

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

	ipa := ipalloc.NewAllocator(ctx.Log, ctx.ServerState.ServicePrefixes)
	eg.Go(func() error {
		return ipa.Watch(sub, eac)
	})

	// Get httpingress from coordinator (created there for admin server)
	hs := co.HttpIngress()

	// Initialize remaining ServerState components
	ctx.ServerState.InitNetServ(ctx.Log, eac)
	ctx.ServerState.InitLogsMaintainer()
	ctx.ServerState.InitStatusMonitor(ctx.Log)
	ctx.ServerState.InitSandboxMetrics(ctx.Log)

	var rc runner.RunnerConfig

	rc.Id = cfg.Server.GetRunnerID()
	rc.ListenAddress = cfg.Server.GetRunnerAddress()
	rc.Workers = runner.DefaulWorkers

	// Use RunnerConfig to get a certificate with proper SANs for the runner's listen address
	rcfg, err := co.RunnerConfig(rc.ListenAddress)
	if err != nil {
		return err
	}
	rc.Config = rcfg

	// Pass cloud authentication config if available
	if cloudAuthConfig.Enabled {
		rc.CloudAuth = &cloudAuthConfig
	} else {
		rc.CloudAuth = &coordinate.CloudAuthConfig{}
	}

	// Build RunnerDeps from ServerState (all dependencies already initialized)
	deps := runner.RunnerDeps{
		CC:              ctx.ServerState.CC,
		Namespace:       ctx.ServerState.Namespace,
		Bridge:          ctx.ServerState.Bridge,
		Tempdir:         ctx.ServerState.Tempdir,
		Subnet:          ctx.ServerState.Subnet,
		NetServ:         ctx.ServerState.NetServ,
		LogsMaintainer:  ctx.ServerState.LogsMaintainer,
		LogWriter:       logWriter,
		StatusMon:       ctx.ServerState.StatusMon,
		IPv4Routable:    ctx.ServerState.IPv4Routable,
		ServicePrefixes: ctx.ServerState.ServicePrefixes,
		DisableLocalNet: false,
		Resolver:        res,
		SandboxMetrics:  ctx.ServerState.SandboxMetrics,
		IsCoordinator:   true,
	}

	// Assign only when non-nil: storing a typed-nil *Issuer into the
	// TokenIssuer interface field would make it compare != nil, defeating the
	// nil guards in the sandbox controller and panicking on first use.
	if workloadIssuer != nil {
		deps.WorkloadIssuer = workloadIssuer
	}

	rc.DataPath = cfg.Server.GetDataPath()
	rc.DiskMode = cfg.Server.GetDiskMode()

	r, err := runner.NewRunner(ctx.Log, deps, rc)
	if err != nil {
		ctx.Log.Error("failed to create runner", "error", err)
		return err
	}

	// Start runner (pass errgroup for network background tasks)
	err = r.Start(sub, eg)
	if err != nil {
		return err
	}

	defer func() {
		if err := r.Close(); err != nil {
			ctx.Log.Error("failed to close runner", "error", err)
		}
	}()

	switch mode := cfg.Ingress.GetMode(); mode {
	case serverconfig.IngressModeAutoprovision:
		if cfg.TLS.GetSelfSigned() {
			if err := autotls.ServeTLSSelfSigned(sub, ctx.Log, hs); err != nil {
				ctx.Log.Error("failed to enable self-signed TLS", "error", err)
				return err
			}
		} else {
			certProvider := co.CertificateProvider()
			if certProvider == nil {
				return fmt.Errorf("no certificate provider available")
			}
			if err := autotls.ServeTLSWithController(sub, ctx.Log, certProvider, hs); err != nil {
				return err
			}
			// Signal autocert controller that port 80 is ready for ACME challenges
			if readyFn := co.AutocertReadySignal(); readyFn != nil {
				readyFn()
			}
		}

	case serverconfig.IngressModeBehindProxyHTTPS:
		addr := cfg.Ingress.GetAddress()
		if addr == "" {
			addr = "127.0.0.1:443"
		}
		if cfg.TLS.GetSelfSigned() {
			if err := autotls.ServeTLSSelfSignedOnAddr(sub, ctx.Log, hs, addr); err != nil {
				ctx.Log.Error("failed to enable self-signed TLS on custom address", "error", err)
				return err
			}
		} else {
			certProvider := co.CertificateProvider()
			if certProvider == nil {
				return fmt.Errorf("no certificate provider available")
			}
			if err := autotls.ServeTLSWithControllerOnAddr(sub, ctx.Log, certProvider, hs, addr); err != nil {
				return err
			}
		}

	case serverconfig.IngressModeBehindProxyHTTP:
		addr := cfg.Ingress.GetAddress()
		if addr == "" {
			addr = "127.0.0.1:80"
		}
		httpSrv := &http.Server{Addr: addr, Handler: hs}
		eg.Go(func() error {
			ctx.Log.Info("starting HTTP server", "addr", addr)
			if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("failed to start HTTP server on %s: %w", addr, err)
			}
			return nil
		})
		eg.Go(func() error {
			<-sub.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return httpSrv.Shutdown(shutdownCtx)
		})

	default:
		return fmt.Errorf("unrecognized ingress.mode %q (should have been caught by config validation)", mode)
	}

	registry := ocireg.NewRegistry(cfg.Server.GetDataPath(), ctx.Log, ec)

	if err := registry.Start(ctx, ":5000"); err != nil {
		ctx.Log.Error("failed to start registry", "error", err)
		return err
	}

	ctx.Log.Info("OCI registry URL", "url", routerAddr)

	if err := hm.SetHost("cluster.local", routerAddr); err != nil {
		ctx.Log.Error("failed to set host", "error", err)
		return err
	}

	// Update BuildKit's hosts file with the registry IP
	if buildkitComponent != nil {
		if err := buildkitComponent.SetRegistryIP(routerAddr.String()); err != nil {
			ctx.Log.Warn("failed to update buildkit hosts file", "error", err)
		}
	}

	// The registry is now listening and cluster.local resolves to it, so
	// it's safe to resume any build sagas interrupted by a previous crash.
	// Recovery runs here, not inside co.Start, because a resumed image push
	// needs these dependencies that Start does not yet have (MIR-1285). It
	// runs in the background on sub (the errgroup/shutdown context) so a
	// recovered build, which re-runs to completion and can take minutes,
	// doesn't block boot but is still cancelled on server shutdown.
	go co.RecoverBuildSagas(sub)

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

	// Set up signal handling for graceful drain (SIGUSR2) and restart mode (SIGUSR1)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR1, syscall.SIGUSR2)

	eg.Go(func() error {
		for {
			select {
			case <-sub.Done():
				return nil
			case sig := <-sigChan:
				switch sig {
				case syscall.SIGUSR1:
					// Restart mode
					ctx.Log.Info("SIGUSR1 received - restart mode")
					r.SetRestartMode(true)
					return fmt.Errorf("restart requested via SIGUSR1")

				case syscall.SIGUSR2:
					// Drain mode - stop all sandboxes before shutdown
					ctx.Log.Info("SIGUSR2 received - draining runner")

					drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()

					if err := r.Drain(drainCtx); err != nil {
						ctx.Log.Error("failed to drain runner", "error", err)
						return err
					}

					ctx.Log.Info("runner drained successfully, shutting down")
					return fmt.Errorf("runner drained, shutting down")
				}
			}
		}
	})

	// Wait for all goroutines to complete or context to be cancelled
	err = eg.Wait()
	if err != nil && err != context.Canceled {
		ctx.Log.Error("error during execution", "error", err)
	}

	ctx.Log.Info("miren server shutting down, cleaning up resources")

	// Stop all sandboxes if requested
	if cfg.Server.GetStopSandboxesOnShutdown() {
		// First close the runner to stop accepting new work
		ctx.Log.Info("closing runner before stopping sandboxes")
		if err := r.Close(); err != nil {
			ctx.Log.Error("failed to close runner during shutdown", "error", err)
		}

		// Stop all sandbox containers via containerd using ServerState
		if ctx.ServerState.CC != nil {
			if err := stopAllSandboxContainers(context.Background(), ctx.Log, ctx.ServerState.CC); err != nil {
				ctx.Log.Error("failed to stop all sandbox containers during shutdown", "error", err)
			}
		}
	}

	co.Stop()
	return err
}

// writeLocalClusterConfig writes a client config file for the local cluster
func writeLocalClusterConfig(ctx *Context, cc *caauth.ClientCertificate, address, clusterName string) error {
	config, err := clientconfig.LoadConfig()
	if err != nil {
		if !errors.Is(err, clientconfig.ErrNoConfig) {
			return fmt.Errorf("failed to load existing client config: %w", err)
		}

		ctx.Log.Warn("error loading existing client config, creating new one", "error", err)
		config = clientconfig.NewConfig()
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
	config.SetLeafConfig("50-local", leafConfigData)

	if config.ActiveCluster() == "" {
		// Set the active cluster to the local one if none is set
		config.SetActiveCluster(clusterName)
	}

	if err := config.Save(); err != nil {
		return fmt.Errorf("failed to save local cluster leaf config: %w", err)
	}

	spath := config.SourcePath()

	var pathsToFix []string

	if spath == "" {
		ctx.Log.Warn("client config source path is empty, cannot fix ownership or permissions")
	} else {
		// Fix directory ownership if running under sudo
		// Fix ownership for all parent directories that we may have created
		pathsToFix = []string{
			filepath.Dir(filepath.Dir(spath)),
			filepath.Dir(spath),
			filepath.Join(filepath.Dir(spath), "clientconfig.d"),
			filepath.Join(filepath.Dir(spath), "clientconfig.d", "50-local.yaml"),
			spath,
		}
	}

	for _, entry := range pathsToFix {
		if err := fixOwnershipIfSudo(entry); err != nil {
			ctx.Log.Warn("failed to fix directory ownership", "dir", entry, "error", err)
		}

		fi, err := os.Stat(entry)
		if err != nil {
			ctx.Log.Warn("failed to stat directory for permission fix", "dir", entry, "error", err)
			continue
		}

		if fi.IsDir() {
			continue
		}

		if err := os.Chmod(entry, 0600); err != nil {
			ctx.Log.Warn("failed to set main config file permissions", "error", err)
		}
	}

	ctx.Log.Info("wrote local cluster config", "path", spath, "name", clusterName, "address", address)
	return nil
}

// fixOwnershipIfSudo fixes file/directory ownership when running under sudo
func fixOwnershipIfSudo(path string) error {
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

	return nil
}

// stopAllSandboxContainers stops all running sandbox containers via containerd during server shutdown
func stopAllSandboxContainers(ctx context.Context, log *slog.Logger, cc *containerd.Client) error {
	log.Info("stopping all sandbox containers via containerd")

	// Create a context with timeout for the entire cleanup
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Use the miren namespace
	ctx = namespaces.WithNamespace(ctx, "miren")

	// List all containers in the namespace
	containerList, err := cc.Containers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	stoppedCount := 0
	for _, container := range containerList {
		containerID := container.ID()

		// Get the task (if it exists and is running)
		task, err := container.Task(ctx, nil)
		if err != nil {
			// No task running, skip
			continue
		}

		log.Info("stopping container", "container", containerID)

		// Send SIGTERM to gracefully stop the task
		if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
			log.Debug("failed to send SIGTERM to task", "container", containerID, "error", err)
		} else {
			log.Debug("sent SIGTERM to task", "container", containerID)
		}

		// Wait a bit for graceful shutdown
		time.Sleep(100 * time.Millisecond)

		// Delete the task (will force kill if still running)
		_, err = task.Delete(ctx, containerd.WithProcessKill)
		if err != nil {
			log.Debug("failed to delete task", "container", containerID, "error", err)
		} else {
			stoppedCount++
		}
	}

	log.Info("stopped sandbox containers", "count", stoppedCount)
	return nil
}
