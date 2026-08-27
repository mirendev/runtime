//go:build linux

package server

import (
	"context"
	"os"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/units"
)

type coordinatorBootInputs struct {
	config coordinate.CoordinatorConfig
}

type coordinatorBootOutput struct {
	coordinator *coordinate.Coordinator
}

type coordinatorBoot struct {
	component     *readiness.Component
	inputs        coordinatorBootInputs
	ipDiscovery   *ipDiscoveryBoot
	registration  *registrationBoot
	identity      *workloadIdentityBoot
	etcd          *etcdBoot
	buildkit      *buildkitBoot
	observability *observabilityBoot
	result        coordinatorBootOutput
}

func coordinatorInputs(options StartOptions, graph *readiness.Graph, resolver netresolve.Resolver, secrets *secret.Registry, address string) coordinatorBootInputs {
	config := options.Config
	appVersionRetentionPeriod, err := units.ParseDuration(config.AppVersion.GetRetentionPeriod())
	if err != nil {
		options.Log.Warn("invalid app_version.retention_period, falling back to default",
			"value", config.AppVersion.GetRetentionPeriod(), "error", err)
	}

	secretKeyRotationPeriod, err := units.ParseDuration(config.Secrets.GetKeyRotationPeriod())
	if err != nil {
		options.Log.Warn("invalid secrets.key_rotation_period, falling back to default",
			"value", config.Secrets.GetKeyRotationPeriod(), "error", err)
		secretKeyRotationPeriod = -1
	}

	sagaRetentionPeriod, err := units.ParseDuration(config.Saga.GetRetentionPeriod())
	if err != nil || sagaRetentionPeriod < 0 {
		defaultSaga := serverconfig.DefaultSagaConfig()
		invalid := sagaRetentionPeriod
		sagaRetentionPeriod, _ = units.ParseDuration(defaultSaga.GetRetentionPeriod())

		reason := "negative duration"
		if err != nil {
			reason = err.Error()
		}
		options.Log.Warn("invalid saga.retention_period, falling back to default",
			"value", config.Saga.GetRetentionPeriod(),
			"parsed", invalid,
			"default", sagaRetentionPeriod,
			"error", reason)
	}

	return coordinatorBootInputs{config: coordinate.CoordinatorConfig{
		Address:                   address,
		EtcdEndpoints:             append([]string(nil), config.Etcd.Endpoints...),
		Prefix:                    config.Etcd.GetPrefix(),
		DataPath:                  config.Server.GetDataPath(),
		AdditionalNames:           append([]string(nil), config.TLS.AdditionalNames...),
		AcmeEmail:                 config.TLS.GetAcmeEmail(),
		AcmeDNSProvider:           config.TLS.GetAcmeDNSProvider(),
		Resolver:                  resolver,
		TempDir:                   os.TempDir(),
		HTTPRequestTimeout:        config.Server.HTTPRequestTimeoutDuration(),
		Secrets:                   secrets,
		Readiness:                 graph,
		AppVersionRetentionCount:  config.AppVersion.GetRetentionCount(),
		AppVersionRetentionPeriod: appVersionRetentionPeriod,
		SagaRetentionPeriod:       sagaRetentionPeriod,
		SecretKeyRotationPeriod:   secretKeyRotationPeriod,
	}}
}

func newCoordinatorBoot(inputs coordinatorBootInputs, ipDiscovery *ipDiscoveryBoot, registration *registrationBoot, identity *workloadIdentityBoot, etcd *etcdBoot, buildkit *buildkitBoot, observability *observabilityBoot) *coordinatorBoot {
	b := &coordinatorBoot{
		inputs:        inputs,
		ipDiscovery:   ipDiscovery,
		registration:  registration,
		identity:      identity,
		etcd:          etcd,
		buildkit:      buildkit,
		observability: observability,
	}
	b.component = readiness.NewComponent("coordinator", readiness.Spec{
		Dependencies: []readiness.Dependency{
			readiness.ReadyDep(ipDiscovery.component),
			readiness.ReadyDep(registration.component),
			readiness.ReadyDep(identity.component),
			readiness.ReadyDep(etcd.component),
			readiness.StartDep(buildkit.component),
			readiness.ReadyDep(observability.component),
		},
		Start:       b.start,
		Stop:        b.stop,
		StopTimeout: componentStopTimeout,
	})
	return b
}

func (b *coordinatorBoot) output() coordinatorBootOutput {
	return b.result
}

func (b *coordinatorBoot) start(ctx context.Context, _ readiness.Reporter) error {
	config := b.inputs.config
	config.IPs = b.ipDiscovery.output().ipSet
	config.CloudAuth = b.registration.output().cloudAuth
	config.WorkloadIssuer = b.identity.output().issuer
	etcd := b.etcd.output()
	buildkit := b.buildkit.output()
	observability := b.observability.output()
	config.EtcdEndpoints = etcd.endpoints
	config.BuildKit = buildkit.component
	if etcd.tls != nil {
		config.EtcdTLS = etcd.tls.ClientTLS
	}
	config.Mem = observability.memory
	config.Cpu = observability.cpu
	config.HTTP = observability.http
	config.Logs = observability.logs
	config.LogWriter = observability.logWriter
	config.VictoriametricsAddress = observability.victoriaMetricsAddress
	config.VictorialogsAddress = observability.victoriaLogsAddress

	b.result.coordinator = coordinate.NewCoordinator(observability.log, config)
	return b.result.coordinator.Start(ctx)
}

func (b *coordinatorBoot) stop(context.Context) error {
	if b.result.coordinator != nil {
		b.result.coordinator.Stop()
	}
	return nil
}
