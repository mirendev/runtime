//go:build linux

package commands

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/distributedrunner"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/runnerconfig"
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

	containerdBinary, containerdBinDir, err := resolveRunnerContainerd(opts.ContainerdSocket)
	if err != nil {
		return err
	}

	// Set up signal handling
	sigCtx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Create errgroup for background tasks
	eg, egCtx := errgroup.WithContext(sigCtx)

	runtime, err := distributedrunner.Start(distributedrunner.StartOptions{
		Log:              ctx.Log,
		Context:          egCtx,
		Group:            eg,
		Config:           cfg,
		ClientConfig:     clientCfg,
		ListenAddr:       listenAddr,
		DataPath:         opts.DataPath,
		ContainerdSocket: opts.ContainerdSocket,
		ContainerdBinary: containerdBinary,
		ContainerdBinDir: containerdBinDir,
	})
	if err != nil {
		return fmt.Errorf("failed to start distributed runner dependency graph: %w", err)
	}
	stop := func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), distributedrunner.ShutdownTimeout)
		defer stopCancel()
		if err := runtime.Stop(stopCtx); err != nil {
			ctx.Log.Error("failed to stop distributed runner dependency graph", "error", err)
		}
	}

	// Wait for shutdown signal or error
	<-egCtx.Done()
	ctx.Log.Info("shutting down runner")
	stop()

	// Wait for background tasks to complete
	if err := eg.Wait(); err != nil && err != context.Canceled {
		ctx.Log.Error("background task error", "error", err)
	}

	ctx.Log.Info("runner stopped")
	return nil
}

// resolveRunnerContainerd applies the command's executable-selection policy.
// Lifecycle ownership begins only after these fixed graph inputs are known.
func resolveRunnerContainerd(externalSocket string) (binaryPath, binDir string, err error) {
	if externalSocket != "" {
		return "", "", nil
	}
	if releasePath := FindReleasePath(); releasePath != "" {
		candidate := filepath.Join(releasePath, "containerd")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, releasePath, nil
		}
	}
	binaryPath, err = exec.LookPath("containerd")
	if err != nil {
		return "", "", fmt.Errorf("containerd binary not found in PATH or release directory: %w", err)
	}
	return binaryPath, "", nil
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

	if slices.Contains(cert.DNSNames, host) {
		return true, nil
	}
	return false, nil
}
