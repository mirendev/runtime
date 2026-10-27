//go:build linux

package server

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverconfig"
)

type etcdBootInputs struct {
	log      *slog.Logger
	config   serverconfig.EtcdConfig
	tls      serverconfig.TLSConfig
	dataPath string
}

type etcdBootOutput struct {
	endpoints []string
	tls       *coordinate.EtcdTLSSetupResult
}

type etcdBoot struct {
	component     *boot.Component
	inputs        etcdBootInputs
	observability observabilityBootOutput
	server        *etcd.EtcdComponent
	cancel        context.CancelFunc
	result        etcdBootOutput
	output        boot.Output[etcdBootOutput]
}

func etcdInputs(options StartOptions) etcdBootInputs {
	return etcdBootInputs{
		log:      options.Log,
		config:   options.Config.Etcd,
		tls:      options.Config.TLS,
		dataPath: options.Config.Server.GetDataPath(),
	}
}

// embeddedEtcdConfig is the member identity and ports the embedded server
// runs with. A restore builds its data directory for the same identity.
func embeddedEtcdConfig(config serverconfig.EtcdConfig) etcd.EtcdConfig {
	return etcd.EtcdConfig{
		Name:              "miren-etcd",
		ClientPort:        config.GetClientPort(),
		HTTPClientPort:    config.GetHTTPClientPort(),
		PeerPort:          config.GetPeerPort(),
		ClusterState:      "new",
		QuotaBackendBytes: int64(config.GetQuotaBackendBytes()),
	}
}

func newEtcdBoot(inputs etcdBootInputs, ipDiscovery boot.Output[ipDiscoveryBootOutput], containerd boot.Output[containerdBootOutput], observability boot.Output[observabilityBootOutput], dataRestore *boot.Component) *etcdBoot {
	b := &etcdBoot{inputs: inputs}
	stop := boot.WithStop(b.stop, componentStopTimeout)
	// The order-only edge on dataRestore is what makes a rollback's
	// data restore land before etcd opens the data directory.
	b.component, b.output = boot.Provide3("etcd", ipDiscovery, containerd, observability, b.startEmbedded, stop, boot.DependsOn(dataRestore))
	return b
}

func (b *etcdBoot) prepareMetadata(ctx context.Context, log *slog.Logger, result etcdBootOutput) error {
	config := coordinate.CoordinatorConfig{DataPath: b.inputs.dataPath, Prefix: b.inputs.config.GetPrefix(), EtcdEndpoints: result.endpoints}
	if result.tls != nil {
		config.EtcdTLS = result.tls.ClientTLS
	}
	return coordinate.NewFoundation(log, config).PrepareMetadata(ctx)
}

func (b *etcdBoot) startEmbedded(ctx context.Context, ipDiscovery ipDiscoveryBootOutput, containerd containerdBootOutput, observability observabilityBootOutput) (etcdBootOutput, error) {
	b.observability = observability
	log := observability.log
	log.Info("starting embedded etcd server", "client-port", b.inputs.config.GetClientPort(), "peer-port", b.inputs.config.GetPeerPort())
	b.server = etcd.NewEtcdComponent(log, containerd.Client, containerd.Namespace, b.inputs.dataPath)
	b.server.SetMetricsWriter(observability.operationalMetrics)
	config := embeddedEtcdConfig(b.inputs.config)

	log.Info("setting up etcd mTLS")
	if _, err := coordinate.EnsureCA(log, b.inputs.dataPath); err != nil {
		return etcdBootOutput{}, fmt.Errorf("ensuring CA for etcd TLS: %w", err)
	}
	tls, err := coordinate.SetupEtcdTLS(log, b.inputs.dataPath, b.inputs.tls.AdditionalNames, ipDiscovery.ipSet.RawIPs())
	if err != nil {
		return etcdBootOutput{}, fmt.Errorf("setting up etcd TLS: %w", err)
	}
	b.result.tls = tls
	config.TLS = &etcd.TLSConfig{CertsDir: tls.CertsDir}

	runtimeCtx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	if err := b.server.Start(runtimeCtx, config); err != nil {
		return etcdBootOutput{}, err
	}
	b.result.endpoints = []string{b.server.ClientEndpoint()}
	if err := b.prepareMetadata(ctx, log, b.result); err != nil {
		return etcdBootOutput{}, err
	}
	// A snapshot restored into an empty directory bootstraps with a temporary
	// transport CA. Switch to the restored authority before publishing etcd to
	// networking clients or runners, not on the following server restart.
	ca, err := coordinate.EnsureCA(log, b.inputs.dataPath)
	if err != nil {
		return etcdBootOutput{}, err
	}
	if !bytes.Equal(ca.GetCACertificate(), tls.ClientTLS.CACert) {
		err := b.server.Stop(ctx)
		b.cancel()
		if err != nil {
			return etcdBootOutput{}, err
		}
		tls, err = coordinate.SetupEtcdTLS(log, b.inputs.dataPath, b.inputs.tls.AdditionalNames, ipDiscovery.ipSet.RawIPs())
		if err != nil {
			return etcdBootOutput{}, err
		}
		b.result.tls = tls
		runtimeCtx, b.cancel = context.WithCancel(ctx)
		if err := b.server.Start(runtimeCtx, config); err != nil {
			return etcdBootOutput{}, err
		}
	}
	log.Info("using embedded etcd", "endpoint", b.server.ClientEndpoint())
	return b.result, nil
}

func (b *etcdBoot) stop(ctx context.Context) error {
	if b.cancel != nil {
		defer b.cancel()
	}
	if b.server == nil {
		return nil
	}
	b.observability.log.Info("stopping embedded etcd")
	return b.server.Stop(ctx)
}
