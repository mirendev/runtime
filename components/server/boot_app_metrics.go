//go:build linux

package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"miren.dev/runtime/components/appmetrics"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/servers/metricspush"
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

	// push is the coordinator's metrics push ingest, armed while vmagent runs.
	push *metricspush.Ingest
}

func appMetricsInputs(options StartOptions) appMetricsBootInputs {
	return appMetricsBootInputs{
		config: appmetrics.Config{
			RemoteWriteURL: options.Config.Telemetry.Metrics.GetRemoteWriteURL(),
			Audience:       options.Config.Telemetry.Metrics.GetWorkloadIdentityAudience(),
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
	foundation boot.Output[foundationBootOutput],
) *appMetricsBoot {
	b := &appMetricsBoot{
		inputs: inputs,
	}
	b.component = boot.Run6(
		"app-metrics",
		containerd,
		registration,
		identity,
		entityAccess,
		observability,
		foundation,
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
	foundation foundationBootOutput,
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
		// Push was advertised from config; stop, since nothing will arm it.
		if push := foundation.foundation.MetricsPush(); push != nil {
			push.Fail()
		}
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
	b.shipWriter = metrics.NewVictoriaMetricsWriter(log, managed.ImportURL(), 0,
		metrics.WithHTTPClient(&http.Client{Timeout: 30 * time.Second, Transport: managed.ImportTransport(nil)}))
	b.shipWriter.Start()
	b.attachShipping(ctx, log, observability, b.shipWriter, identityLabels)
	log.Info("runtime operational metrics shipping through managed metrics",
		"cluster", config.ClusterID, "runner", b.inputs.runnerID)

	if push := foundation.foundation.MetricsPush(); push != nil {
		push.Arm(metricspush.Backend{
			ImportURL: managed.ImportURL(),
			Transport: managed.ImportTransport(nil),
			ClusterID: config.ClusterID,
			Resolver:  metricspush.NewEntityResolver(eac),
		})
		b.push = push
		log.Info("workload metrics push enabled", "cluster", config.ClusterID)
	}
	return nil
}

// attachShipping joins sink to the operational fanout, labeled with the
// cluster identity, and then re-emits the process identity series through
// it. The order matters: the identity sample the process pushed at boot only
// reached the embedded store, since this sink did not exist yet, so the emit
// has to come after Attach for a process that dies before its next tick to
// record its start time and build where the restart and skew rules can see it.
func (b *appMetricsBoot) attachShipping(
	ctx context.Context,
	log *slog.Logger,
	observability observabilityBootOutput,
	sink metrics.PointWriter,
	identityLabels map[string]string,
) {
	b.shipping = &metrics.Labeled{Sink: sink, Labels: identityLabels}
	b.operational = observability.operationalMetrics
	b.operational.Attach(b.shipping)
	if err := observability.processInfo.Emit(ctx); err != nil {
		log.Warn("failed to ship control-process identity after attaching shipping sink", "error", err)
	}
	// Recovery at boot can count before this sink exists, and a counter whose
	// first shipped sample is already nonzero gives increase() nothing to
	// measure from.
	if observability.sagaCounts != nil {
		observability.sagaCounts.ResendBaselines()
		if err := observability.sagaCounts.Emit(ctx, time.Now()); err != nil {
			log.Warn("failed to ship saga count baselines after attaching shipping sink", "error", err)
		}
	}
}

func (b *appMetricsBoot) stop(ctx context.Context) error {
	if b.push != nil {
		b.push.Disarm()
		b.push = nil
	}
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
