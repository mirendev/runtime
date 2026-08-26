//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/appmetrics"
	"miren.dev/runtime/pkg/boot"
)

type appMetricsBootInputs struct {
	config                appmetrics.Config
	configuredClusterName string
	dataPath              string
}

type appMetricsBoot struct {
	component        *boot.Component
	inputs           appMetricsBootInputs
	managed          *appmetrics.Component
	disabledReporter *appmetrics.DisabledReporter
}

func appMetricsInputs(options StartOptions) appMetricsBootInputs {
	return appMetricsBootInputs{
		config: appmetrics.Config{
			RemoteWriteURL: options.Config.Metrics.RemoteWrite.GetURL(),
			Audience:       options.Config.Metrics.RemoteWrite.GetWorkloadIdentityAudience(),
		},
		configuredClusterName: options.Config.Server.GetConfigClusterName(),
		dataPath:              options.Config.Server.GetDataPath(),
	}
}

func newAppMetricsBoot(
	inputs appMetricsBootInputs,
	containerd boot.Output[containerdBootOutput],
	registration boot.Output[registrationBootOutput],
	identity boot.Output[workloadIdentityBootOutput],
	entityAccess boot.Output[entityAccessBootOutput],
	observability boot.Output[observabilityBootOutput],
) *appMetricsBoot {
	b := &appMetricsBoot{
		inputs: inputs,
	}
	b.component = boot.Run5(
		"app-metrics",
		containerd,
		registration,
		identity,
		entityAccess,
		observability,
		b.start,
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *appMetricsBoot) start(
	ctx context.Context,
	containerd containerdBootOutput,
	registration registrationBootOutput,
	identity workloadIdentityBootOutput,
	entityAccess entityAccessBootOutput,
	observability observabilityBootOutput,
) error {
	log := observability.log
	eac := entityAccess.access
	if b.inputs.config.RemoteWriteURL == "" {
		log.Info("managed application metrics disabled: no remote-write destination configured")
		reporter := appmetrics.NewDisabledReporter(log, eac)
		if err := reporter.Start(ctx); err != nil {
			log.Error("failed to watch for application metrics without a destination", "error", err)
			return nil
		}
		b.disabledReporter = reporter
		return nil
	}

	config := b.inputs.config
	config.ClusterID = managedMetricsClusterLabel(
		registration.cloudAuth.ClusterID,
		b.inputs.configuredClusterName,
	)
	managed := appmetrics.New(log, containerd.client, containerd.namespace, b.inputs.dataPath, eac, identity.issuer)
	if err := managed.Start(ctx, config); err != nil {
		// Metrics are optional application telemetry. A broken destination or
		// scraper must be visible, but must not take the application control
		// plane down with it.
		log.Error("managed application metrics failed to start", "error", err)
		return nil
	}
	b.managed = managed
	return nil
}

func (b *appMetricsBoot) stop(ctx context.Context) error {
	if b.disabledReporter != nil {
		b.disabledReporter.Stop()
		b.disabledReporter = nil
	}
	if b.managed == nil {
		return nil
	}
	err := b.managed.Stop(ctx)
	b.managed = nil
	return err
}

func managedMetricsClusterLabel(cloudID, configuredName string) string {
	if cloudID != "" {
		return cloudID
	}
	if configuredName != "" {
		return configuredName
	}
	return "local"
}
