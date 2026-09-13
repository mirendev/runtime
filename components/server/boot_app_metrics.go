//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/appmetrics"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/boot"
)

type appMetricsBootInputs struct {
	config                appmetrics.Config
	configuredClusterName string
	runnerID              string
	dataPath              string
}

type appMetricsBoot struct {
	component        *boot.Component
	inputs           appMetricsBootInputs
	managed          *appmetrics.Component
	disabledReporter *appmetrics.DisabledReporter

	// shipping pushes the runtime's operational series into vmagent. It is
	// attached to the observability fanout for as long as vmagent runs.
	shipping    *metrics.Labeled
	shipWriter  *metrics.VictoriaMetricsWriter
	operational *metrics.Fanout
}

func appMetricsInputs(options StartOptions) appMetricsBootInputs {
	return appMetricsBootInputs{
		config: appmetrics.Config{
			RemoteWriteURL: options.Config.Metrics.RemoteWrite.GetURL(),
			Audience:       options.Config.Metrics.RemoteWrite.GetWorkloadIdentityAudience(),
		},
		configuredClusterName: options.Config.Server.GetConfigClusterName(),
		runnerID:              options.Config.Server.GetRunnerID(),
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
	managed := appmetrics.New(log, containerd.Client, containerd.Namespace, b.inputs.dataPath, eac, identity.issuer)
	if err := managed.Start(ctx, config); err != nil {
		// Metrics are optional application telemetry. A broken destination or
		// scraper must be visible, but must not take the application control
		// plane down with it.
		log.Error("managed application metrics failed to start", "error", err)
		return nil
	}
	b.managed = managed

	// The runtime's own operational series ride the same vmagent. Pushed
	// samples skip fileSD relabeling, so the cluster identity the scrape path
	// gets from targets.json is stamped here from the same clusterID. The
	// embedded VictoriaMetrics keeps receiving the unlabeled series it always
	// has; only the shipped copy carries the identity, since only at a shared
	// destination do series from different clusters need telling apart.
	identityLabels := map[string]string{"miren_cluster": config.ClusterID}
	if b.inputs.runnerID != "" {
		identityLabels["miren_runner"] = b.inputs.runnerID
	}
	// A zero timeout takes the writer's 30s default; vmagent is on loopback.
	b.shipWriter = metrics.NewVictoriaMetricsWriter(log, managed.ImportURL(), 0)
	b.shipWriter.Start()
	b.shipping = &metrics.Labeled{Sink: b.shipWriter, Labels: identityLabels}
	b.operational = observability.operationalMetrics
	b.operational.Attach(b.shipping)
	log.Info("runtime operational metrics shipping through managed metrics",
		"cluster", config.ClusterID, "runner", b.inputs.runnerID)
	return nil
}

func (b *appMetricsBoot) stop(ctx context.Context) error {
	if b.disabledReporter != nil {
		b.disabledReporter.Stop()
		b.disabledReporter = nil
	}
	if b.shipping != nil {
		b.operational.Detach(b.shipping)
		b.shipping = nil
		b.operational = nil
	}
	if b.shipWriter != nil {
		// Close flushes what it can into vmagent, whose own queue outlives this
		// process; a final send that fails because vmagent is already stopping
		// is reported by the writer and is not a shutdown error.
		_ = b.shipWriter.Close()
		b.shipWriter = nil
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
