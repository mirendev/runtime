//go:build linux

package server

import (
	"context"
	"os"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/serverconfig"
	"miren.dev/runtime/pkg/units"
)

type foundationBootOutput struct {
	foundation *coordinate.Foundation
}

type foundationBoot struct {
	component *boot.Component
	config    coordinate.CoordinatorConfig
	value     *coordinate.Foundation
	output    boot.Output[foundationBootOutput]
}

func newFoundationBoot(config coordinate.CoordinatorConfig, ipDiscovery boot.Output[ipDiscoveryBootOutput], registration boot.Output[registrationBootOutput], identity boot.Output[workloadIdentityBootOutput], etcd boot.Output[etcdBootOutput], buildkit boot.Output[buildkitBootOutput], observability boot.Output[observabilityBootOutput]) *foundationBoot {
	b := &foundationBoot{config: config}
	b.component, b.output = boot.Provide6(
		"cluster-foundation", ipDiscovery, registration, identity, etcd, buildkit, observability,
		b.start, boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *foundationBoot) start(ctx context.Context, ipDiscovery ipDiscoveryBootOutput, registration registrationBootOutput, identity workloadIdentityBootOutput, etcd etcdBootOutput, buildkit buildkitBootOutput, observability observabilityBootOutput) (foundationBootOutput, error) {
	config := b.config
	config.IPs = ipDiscovery.ipSet
	config.CloudAuth = registration.cloudAuth
	config.WorkloadIssuer = identity.issuer
	config.EtcdEndpoints = etcd.endpoints
	config.BuildKit = buildkit.component
	if etcd.tls != nil {
		config.EtcdTLS = etcd.tls.ClientTLS
	}
	config.Mem = observability.memory
	config.Cpu = observability.cpu
	config.HTTP = observability.http
	config.MetricsWriter = observability.metricsWriter
	config.Logs = observability.logs
	config.LogWriter = observability.logWriter
	config.VictoriametricsAddress = observability.victoriaMetricsAddress
	config.VictorialogsAddress = observability.victoriaLogsAddress

	b.value = coordinate.NewFoundation(observability.log, config)
	if err := b.value.Start(ctx); err != nil {
		return foundationBootOutput{}, err
	}
	return foundationBootOutput{foundation: b.value}, nil
}

func (b *foundationBoot) stop(ctx context.Context) error {
	if b.value == nil {
		return nil
	}
	return b.value.Stop(ctx)
}

func foundationConfig(options StartOptions, resolver netresolve.Resolver, secrets *secret.Registry, address string) coordinate.CoordinatorConfig {
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

	return coordinate.CoordinatorConfig{
		Address:                   address,
		EtcdEndpoints:             append([]string(nil), config.Etcd.Endpoints...),
		Prefix:                    config.Etcd.GetPrefix(),
		DataPath:                  config.Server.GetDataPath(),
		AdditionalNames:           append([]string(nil), config.TLS.AdditionalNames...),
		AcmeEmail:                 config.TLS.GetAcmeEmail(),
		AcmeDNSProvider:           config.TLS.GetAcmeDNSProvider(),
		Resolver:                  resolver,
		TempDir:                   os.TempDir(),
		Secrets:                   secrets,
		AppVersionRetentionCount:  config.AppVersion.GetRetentionCount(),
		AppVersionRetentionPeriod: appVersionRetentionPeriod,
		SagaRetentionPeriod:       sagaRetentionPeriod,
		SecretKeyRotationPeriod:   secretKeyRotationPeriod,
	}
}
