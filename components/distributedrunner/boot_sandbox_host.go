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
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/boot"
)

type sandboxHostBootInputs struct {
	log             *slog.Logger
	group           *errgroup.Group
	dataPath        string
	etcdEndpoints   []string
	etcdPrefix      string
	coordinator     string
	clientCert      string
	clientKey       string
	caCert          string
	servicePrefixes []netip.Prefix
}

type sandboxHostBoot struct {
	component *boot.Component
	inputs    sandboxHostBootInputs
	value     *runner.SandboxHost
	output    boot.Output[*runner.SandboxHost]
}

func sandboxHostInputs(options StartOptions) sandboxHostBootInputs {
	return sandboxHostBootInputs{
		log:             options.Log,
		group:           options.Group,
		dataPath:        options.DataPath,
		etcdEndpoints:   append([]string(nil), options.Config.EtcdEndpoints...),
		etcdPrefix:      options.Config.EtcdPrefix,
		coordinator:     options.Config.CoordinatorAddress,
		clientCert:      options.Config.ClientCert,
		clientKey:       options.Config.ClientKey,
		caCert:          options.Config.CACert,
		servicePrefixes: serviceNetworkPrefixes(),
	}
}

func newSandboxHostBoot(
	inputs sandboxHostBootInputs,
	access boot.Output[clusterAccessBootOutput],
	storage boot.Output[*runner.NodeStorage],
	containerd boot.Output[containerdBootOutput],
	telemetry boot.Output[telemetryBootOutput],
) *sandboxHostBoot {
	b := &sandboxHostBoot{inputs: inputs}
	b.component, b.output = boot.Provide4(
		"sandbox-host", access, storage, containerd, telemetry, b.start,
		boot.WithStop(b.stop, 0),
	)
	return b
}

func (b *sandboxHostBoot) start(
	ctx context.Context,
	access clusterAccessBootOutput,
	storage *runner.NodeStorage,
	containerd containerdBootOutput,
	telemetry telemetryBootOutput,
) (*runner.SandboxHost, error) {
	dependencies := runner.RunnerDeps{
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
		return nil, err
	}

	var err error
	b.value, err = runner.NewSandboxHost(access.access, storage, dependencies, access.config)
	if err != nil {
		return nil, fmt.Errorf("creating sandbox host: %w", err)
	}
	if err := b.value.Start(ctx, b.inputs.group); err != nil {
		return nil, fmt.Errorf("starting sandbox host: %w", err)
	}
	return b.value, nil
}

func (b *sandboxHostBoot) prepareNetworkDeps(deps *runner.RunnerDeps) error {
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

func (b *sandboxHostBoot) stop(context.Context) error {
	if b.value == nil {
		return nil
	}
	return b.value.Close()
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
