//go:build linux

package server

import (
	"context"
	"fmt"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/labs"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverconfig"
)

type etcdBootInputs struct {
	config      serverconfig.EtcdConfig
	tls         serverconfig.TLSConfig
	dataPath    string
	distributed bool
}

type etcdBootOutput struct {
	endpoints []string
	tls       *coordinate.EtcdTLSSetupResult
}

type etcdBoot struct {
	component     *readiness.Component
	inputs        etcdBootInputs
	ipDiscovery   *ipDiscoveryBoot
	containerd    *containerdBoot
	observability *observabilityBoot
	server        *etcd.EtcdComponent
	result        etcdBootOutput
}

func etcdInputs(options StartOptions) etcdBootInputs {
	return etcdBootInputs{
		config:      options.Config.Etcd,
		tls:         options.Config.TLS,
		dataPath:    options.Config.Server.GetDataPath(),
		distributed: labs.DistributedRunners(),
	}
}

func newEtcdBoot(inputs etcdBootInputs, ipDiscovery *ipDiscoveryBoot, containerd *containerdBoot, observability *observabilityBoot) *etcdBoot {
	b := &etcdBoot{inputs: inputs, ipDiscovery: ipDiscovery, containerd: containerd, observability: observability}
	b.component = readiness.NewComponent("etcd", readiness.Spec{
		Dependencies: []readiness.Dependency{
			readiness.ReadyDep(ipDiscovery.component),
			readiness.ReadyDep(containerd.component),
			readiness.ReadyDep(observability.component),
		},
		Start:       b.start,
		Stop:        b.stop,
		StopTimeout: componentStopTimeout,
	})
	return b
}

func (b *etcdBoot) output() etcdBootOutput {
	return b.result
}

func (b *etcdBoot) start(ctx context.Context, _ readiness.Reporter) error {
	b.result.endpoints = append([]string(nil), b.inputs.config.Endpoints...)
	if !b.inputs.config.GetStartEmbedded() {
		return nil
	}

	log := b.observability.output().log
	log.Info("starting embedded etcd server", "client-port", b.inputs.config.GetClientPort(), "peer-port", b.inputs.config.GetPeerPort())
	containerd := b.containerd.output()
	b.server = etcd.NewEtcdComponent(log, containerd.client, containerd.namespace, b.inputs.dataPath)
	b.server.SetMetricsWriter(b.observability.output().metricsWriter)
	config := etcd.EtcdConfig{
		Name:              "miren-etcd",
		ClientPort:        b.inputs.config.GetClientPort(),
		HTTPClientPort:    b.inputs.config.GetHTTPClientPort(),
		PeerPort:          b.inputs.config.GetPeerPort(),
		ClusterState:      "new",
		QuotaBackendBytes: int64(b.inputs.config.GetQuotaBackendBytes()),
	}

	if b.inputs.distributed {
		log.Info("setting up etcd mTLS for distributed runners")
		if _, err := coordinate.EnsureCA(log, b.inputs.dataPath); err != nil {
			return fmt.Errorf("ensuring CA for etcd TLS: %w", err)
		}
		var err error
		b.result.tls, err = coordinate.SetupEtcdTLS(log, b.inputs.dataPath, b.inputs.tls.AdditionalNames, b.ipDiscovery.output().ipSet.RawIPs())
		if err != nil {
			return fmt.Errorf("setting up etcd TLS: %w", err)
		}
		config.TLS = &etcd.TLSConfig{CertsDir: b.result.tls.CertsDir}
	}

	if err := b.server.Start(ctx, config); err != nil {
		return err
	}
	b.result.endpoints = []string{b.server.ClientEndpoint()}
	log.Info("using embedded etcd", "endpoint", b.server.ClientEndpoint())
	return nil
}

func (b *etcdBoot) stop(ctx context.Context) error {
	if b.server == nil {
		return nil
	}
	b.observability.output().log.Info("stopping embedded etcd")
	return b.server.Stop(ctx)
}
