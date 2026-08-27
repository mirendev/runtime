//go:build linux

package server

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/etcd"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/labs"
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
	component     *boot.Component
	inputs        etcdBootInputs
	observability observabilityBootOutput
	server        *etcd.EtcdComponent
	result        etcdBootOutput
	output        boot.Output[etcdBootOutput]
}

func etcdInputs(options StartOptions) etcdBootInputs {
	return etcdBootInputs{
		config:      options.Config.Etcd,
		tls:         options.Config.TLS,
		dataPath:    options.Config.Server.GetDataPath(),
		distributed: labs.DistributedRunners(),
	}
}

func newEtcdBoot(inputs etcdBootInputs, ipDiscovery boot.Output[ipDiscoveryBootOutput], containerd boot.Output[containerdBootOutput], observability boot.Output[observabilityBootOutput]) *etcdBoot {
	b := &etcdBoot{inputs: inputs}
	stop := boot.WithStop(b.stop, componentStopTimeout)
	if inputs.config.GetStartEmbedded() {
		b.component, b.output = boot.Provide3("etcd", ipDiscovery, containerd, observability, b.startEmbedded, stop)
	} else {
		b.component, b.output = boot.Provide0("etcd", b.startExternal, stop)
	}
	return b
}

func (b *etcdBoot) startExternal(ctx context.Context) (etcdBootOutput, error) {
	endpoints := append([]string(nil), b.inputs.config.Endpoints...)
	if len(endpoints) == 0 {
		return etcdBootOutput{}, fmt.Errorf("etcd endpoints not specified and embedded etcd not started")
	}
	client, err := clientv3.New(clientv3.Config{Endpoints: endpoints, DialTimeout: 5 * time.Second})
	if err != nil {
		return etcdBootOutput{}, fmt.Errorf("creating external etcd client: %w", err)
	}
	defer client.Close()

	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := client.Get(readyCtx, b.inputs.config.GetPrefix(), clientv3.WithLimit(1)); err != nil {
		return etcdBootOutput{}, fmt.Errorf("checking external etcd readiness: %w", err)
	}
	return etcdBootOutput{endpoints: endpoints}, nil
}

func (b *etcdBoot) startEmbedded(ctx context.Context, ipDiscovery ipDiscoveryBootOutput, containerd containerdBootOutput, observability observabilityBootOutput) (etcdBootOutput, error) {
	b.observability = observability
	b.result.endpoints = append([]string(nil), b.inputs.config.Endpoints...)
	log := observability.log
	log.Info("starting embedded etcd server", "client-port", b.inputs.config.GetClientPort(), "peer-port", b.inputs.config.GetPeerPort())
	b.server = etcd.NewEtcdComponent(log, containerd.client, containerd.namespace, b.inputs.dataPath)
	b.server.SetMetricsWriter(observability.metricsWriter)
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
			return etcdBootOutput{}, fmt.Errorf("ensuring CA for etcd TLS: %w", err)
		}
		var err error
		b.result.tls, err = coordinate.SetupEtcdTLS(log, b.inputs.dataPath, b.inputs.tls.AdditionalNames, ipDiscovery.ipSet.RawIPs())
		if err != nil {
			return etcdBootOutput{}, fmt.Errorf("setting up etcd TLS: %w", err)
		}
		config.TLS = &etcd.TLSConfig{CertsDir: b.result.tls.CertsDir}
	}

	if err := b.server.Start(ctx, config); err != nil {
		return etcdBootOutput{}, err
	}
	b.result.endpoints = []string{b.server.ClientEndpoint()}
	log.Info("using embedded etcd", "endpoint", b.server.ClientEndpoint())
	return b.result, nil
}

func (b *etcdBoot) stop(ctx context.Context) error {
	if b.server == nil {
		return nil
	}
	b.observability.log.Info("stopping embedded etcd")
	return b.server.Stop(ctx)
}
