//go:build linux

package distributedrunner

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/components/netresolve"
	runnercomponent "miren.dev/runtime/components/runner"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/workloadidentity"
)

type runnerBootInputs struct {
	log             *slog.Logger
	group           *errgroup.Group
	clientConfig    *clientconfig.Config
	runnerID        string
	name            string
	listenAddress   string
	dataPath        string
	diskMode        string
	etcdEndpoints   []string
	etcdPrefix      string
	coordinator     string
	clientCert      string
	clientKey       string
	caCert          string
	servicePrefixes []netip.Prefix
	newRunner       func(*slog.Logger, runnercomponent.RunnerDeps, runnercomponent.RunnerConfig) (runnerRuntime, error)
}

type runnerRuntime interface {
	Start(context.Context, ...*errgroup.Group) error
	Close() error
	WorkloadIssuer() workloadidentity.TokenIssuer
}

type runnerBoot struct {
	component *boot.Component
	inputs    runnerBootInputs
	runner    runnerRuntime
}

func runnerInputs(options StartOptions) runnerBootInputs {
	return runnerBootInputs{
		log:             options.Log,
		group:           options.Group,
		clientConfig:    options.ClientConfig,
		runnerID:        options.Config.RunnerID,
		name:            options.Config.Name,
		listenAddress:   options.ListenAddr,
		dataPath:        options.DataPath,
		diskMode:        options.Config.DiskMode,
		etcdEndpoints:   append([]string(nil), options.Config.EtcdEndpoints...),
		etcdPrefix:      options.Config.EtcdPrefix,
		coordinator:     options.Config.CoordinatorAddress,
		clientCert:      options.Config.ClientCert,
		clientKey:       options.Config.ClientKey,
		caCert:          options.Config.CACert,
		servicePrefixes: serviceNetworkPrefixes(),
		newRunner: func(log *slog.Logger, deps runnercomponent.RunnerDeps, config runnercomponent.RunnerConfig) (runnerRuntime, error) {
			return runnercomponent.NewRunner(log, deps, config)
		},
	}
}

func newRunnerBoot(inputs runnerBootInputs, containerd boot.Output[containerdBootOutput], telemetry boot.Output[telemetryBootOutput]) *runnerBoot {
	b := &runnerBoot{inputs: inputs}
	b.component = boot.Run2("runner", containerd, telemetry, b.start,
		boot.WithStop(b.stop, 0))
	return b
}

func (b *runnerBoot) start(ctx context.Context, containerd containerdBootOutput, telemetry telemetryBootOutput) error {
	config := runnercomponent.RunnerConfig{
		Id:            b.inputs.runnerID,
		Name:          b.inputs.name,
		ListenAddress: b.inputs.listenAddress,
		Workers:       runnercomponent.DefaulWorkers,
		DataPath:      b.inputs.dataPath,
		Config:        b.inputs.clientConfig,
		DiskMode:      b.inputs.diskMode,
	}
	dependencies := runnercomponent.RunnerDeps{
		CC:        containerd.Client,
		Namespace: containerd.Namespace,
		Bridge:    "rt0",
		Tempdir:   os.TempDir(),

		DisableLocalNet: true,
		LogsMaintainer:  observability.NewLogsMaintainer(),
		LogWriter:       telemetry.logWriter,
		StatusMon:       observability.NewStatusMonitor(b.inputs.log),
		SandboxMetrics:  telemetry.sandboxMetrics,
		ServicePrefixes: b.inputs.servicePrefixes,

		EtcdEndpoints: append([]string(nil), b.inputs.etcdEndpoints...),
		EtcdPrefix:    b.inputs.etcdPrefix,
	}
	if err := b.prepareNetworkDeps(&dependencies); err != nil {
		return err
	}

	runner, err := b.inputs.newRunner(b.inputs.log, dependencies, config)
	if err != nil {
		return fmt.Errorf("creating runner: %w", err)
	}
	b.runner = runner
	if err := runner.Start(ctx, b.inputs.group); err != nil {
		return fmt.Errorf("starting runner: %w", err)
	}
	if telemetry.tokenSource != nil {
		if issuer := runner.WorkloadIssuer(); issuer != nil {
			telemetry.tokenSource.SetIssuer(issuer)
		} else {
			b.inputs.log.Error("no workload identity issuer available; telemetry cannot be shipped")
		}
	}
	b.inputs.log.Info("runner started successfully",
		"runner_id", b.inputs.runnerID,
		"listen_address", b.inputs.listenAddress)
	return nil
}

func (b *runnerBoot) prepareNetworkDeps(deps *runnercomponent.RunnerDeps) error {
	if err := os.MkdirAll(b.inputs.dataPath, 0755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	resolver, hostMapper := netresolve.NewLocalResolver()
	deps.Resolver = resolver
	coordinatorHost, coordinatorPort, splitErr := net.SplitHostPort(b.inputs.coordinator)
	if splitErr != nil {
		b.inputs.log.Warn("in-cluster API access disabled: coordinator address has no usable host and port",
			"coordinator", b.inputs.coordinator, "error", splitErr)
	} else if coordinatorAddr, err := resolveHost(coordinatorHost); err != nil {
		b.inputs.log.Warn("could not resolve coordinator address", "host", coordinatorHost, "error", err)
	} else {
		// Sandboxes reach the API on the coordinator rather than the local bridge
		// router. This must be an IP because sandbox DNS resolves app.miren names
		// and nothing else, so the coordinator hostname would not resolve there.
		hostMapper.SetHost("cluster.local", coordinatorAddr)
		deps.ApiAddress = net.JoinHostPort(coordinatorAddr.String(), coordinatorPort)
		deps.CACert = []byte(b.inputs.caCert)
		b.inputs.log.Info("mapped cluster.local to coordinator", "hostname", coordinatorHost, "addr", coordinatorAddr)
		b.inputs.log.Info("sandboxes will reach the cluster API at", "address", deps.ApiAddress)
	}

	if b.inputs.clientCert == "" || b.inputs.clientKey == "" || b.inputs.caCert == "" {
		return nil
	}
	etcdCertsDir := filepath.Join(b.inputs.dataPath, "etcd-certs")
	if err := os.MkdirAll(etcdCertsDir, 0700); err != nil {
		return fmt.Errorf("creating etcd certs directory: %w", err)
	}
	deps.EtcdTLSCertFile = filepath.Join(etcdCertsDir, "client.crt")
	deps.EtcdTLSKeyFile = filepath.Join(etcdCertsDir, "client.key")
	deps.EtcdTLSCAFile = filepath.Join(etcdCertsDir, "ca.crt")
	if err := os.WriteFile(deps.EtcdTLSCertFile, []byte(b.inputs.clientCert), 0644); err != nil {
		return fmt.Errorf("writing etcd client cert: %w", err)
	}
	if err := os.WriteFile(deps.EtcdTLSKeyFile, []byte(b.inputs.clientKey), 0600); err != nil {
		return fmt.Errorf("writing etcd client key: %w", err)
	}
	if err := os.WriteFile(deps.EtcdTLSCAFile, []byte(b.inputs.caCert), 0644); err != nil {
		return fmt.Errorf("writing etcd CA cert: %w", err)
	}
	return nil
}

func (b *runnerBoot) stop(context.Context) error {
	if b.runner == nil {
		return nil
	}
	return b.runner.Close()
}

func serviceNetworkPrefixes() []netip.Prefix {
	// TODO: Receive these from the coordinator's join response.
	return []netip.Prefix{
		netip.MustParsePrefix("10.10.0.0/16"),
		netip.MustParsePrefix("fd47:cafe:d00d::/64"),
	}
}

// resolveHost turns a host that may be either a literal IP or a DNS name into
// an address. Callers need a literal IP for the local resolver and sandbox API
// config because sandbox DNS resolves app.miren names and nothing else.
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
