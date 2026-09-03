//go:build linux

package server

import (
	"context"
	"log/slog"
	"path/filepath"

	"golang.org/x/sync/errgroup"
	containerdcomp "miren.dev/runtime/components/containerd"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/serverconfig"
)

type containerdBootOutput = containerdcomp.Capability

// StartOptions contains the resolved inputs used to assemble one fixed server
// boot graph.
type StartOptions struct {
	Log     *slog.Logger
	Context context.Context
	Group   *errgroup.Group
	Config  *serverconfig.Config
}

// startup is the composition root. It owns the component inventory, while
// each component owns its own inputs, dependencies, resources, and outputs.
type startup struct {
	runtime             *Runtime
	ipDiscovery         *ipDiscoveryBoot
	registration        *registrationBoot
	workloadIdentity    *workloadIdentityBoot
	tracing             *tracingBoot
	observability       *observabilityBoot
	pprof               *pprofBoot
	containerd          *containerdcomp.Boot
	etcd                *etcdBoot
	victoriaLogs        *victoriaLogsBoot
	victoriaMetrics     *victoriaMetricsBoot
	buildkit            *buildkitBoot
	coordinator         *coordinatorBoot
	deploymentAttempts  *deploymentAttemptMigrationBoot
	cloudUplink         *cloudUplinkBoot
	entityAccess        *entityAccessBoot
	appMetrics          *appMetricsBoot
	network             *networkBoot
	runner              *runnerBoot
	ingress             *ingressBoot
	registryHostMapping *registryHostMappingBoot
	ociRegistry         *ociRegistryBoot
	workAdmission       *workAdmissionBoot
	buildSagaRecovery   *buildSagaRecoveryBoot
}

func newStartup(runtime *Runtime, options StartOptions) *startup {
	// These are shared in-process capabilities with no lifecycle of their own.
	// The composition root creates one instance of each and passes it directly to
	// the components that share it.
	resolver, hostMapper := netresolve.NewLocalResolver()
	secretRegistry := secret.NewRegistry()
	address := NormalizeServerAddress(options.Log, options.Config.Server.GetAddress())

	// This is where we wire up the boot graph. Server-specific components live
	// in sibling boot_*.go files; reusable adapters live with the component they
	// manage. Input helpers narrow external config and shared resources to what
	// each component needs. The remaining constructor arguments are typed
	// upstream outputs: they show both the values delivered to the component and
	// the dataflow edges that determine startup and shutdown order. The graph
	// does not change after startup begins.
	ipDiscovery := newIPDiscoveryBoot(ipDiscoveryInputs(options))
	registration := newRegistrationBoot(registrationInputs(options))
	workloadIdentity := newWorkloadIdentityBoot(workloadIdentityInputs(options), registration.output)
	tracing := newTracingBoot(tracingInputs(options), registration.output)
	containerd := containerdcomp.NewBoot("containerd", containerdBootConfig(options))
	victoriaLogs := newVictoriaLogsBoot(victoriaLogsInputs(options), containerd.Output)
	victoriaMetrics := newVictoriaMetricsBoot(victoriaMetricsInputs(options), containerd.Output)
	observability := newObservabilityBoot(observabilityInputs(options), tracing.output, victoriaLogs.output, victoriaMetrics.output)
	pprof := newPprofBoot(observability.output)
	etcd := newEtcdBoot(etcdInputs(options), ipDiscovery.output, containerd.Output, observability.output)
	network := newNetworkBoot(networkInputs(options), etcd.output, observability.output)
	registryHostMapping := newRegistryHostMappingBoot(registryHostMappingInputs(hostMapper), network.output)
	buildkit := newBuildkitBoot(buildkitInputs(options), containerd.Output, registryHostMapping.output, network.output, observability.output)
	coordinator := newCoordinatorBoot(
		coordinatorInputs(options, resolver, secretRegistry, address),
		ipDiscovery.output,
		registration.output,
		workloadIdentity.output,
		etcd.output,
		buildkit.output,
		observability.output,
	)
	deploymentAttempts := newDeploymentAttemptMigrationBoot(coordinator.output)
	cloudUplink := newCloudUplinkBoot(coordinator.output, deploymentAttempts.output)
	entityAccess := newEntityAccessBoot(entityAccessInputs(options), coordinator.output, observability.output)
	appMetrics := newAppMetricsBoot(
		appMetricsInputs(options),
		containerd.Output,
		registration.output,
		workloadIdentity.output,
		entityAccess.output,
		observability.output,
	)
	runner := newRunnerBoot(
		runnerInputs(options, resolver, secretRegistry, serverPort(options.Log, address)),
		registration.output,
		workloadIdentity.output,
		containerd.Output,
		coordinator.output,
		entityAccess.output,
		network.output,
		observability.output,
	)
	ingress := newIngressBoot(ingressInputs(options), coordinator.output, observability.output)
	ociRegistry := newOCIRegistryBoot(ociRegistryInputs(options), workloadIdentity.output, entityAccess.output, registryHostMapping.output, observability.output)
	workAdmission := newWorkAdmissionBoot(coordinator.output, runner.output, buildkit.output, ociRegistry.output, registryHostMapping.output)
	buildSagaRecovery := newBuildSagaRecoveryBoot(
		buildSagaRecoveryInputs(options),
		coordinator.output,
		buildkit.output,
		ociRegistry.output,
		registryHostMapping.output,
	)

	return &startup{
		runtime:             runtime,
		ipDiscovery:         ipDiscovery,
		registration:        registration,
		workloadIdentity:    workloadIdentity,
		tracing:             tracing,
		observability:       observability,
		pprof:               pprof,
		containerd:          containerd,
		etcd:                etcd,
		victoriaLogs:        victoriaLogs,
		victoriaMetrics:     victoriaMetrics,
		buildkit:            buildkit,
		coordinator:         coordinator,
		deploymentAttempts:  deploymentAttempts,
		cloudUplink:         cloudUplink,
		entityAccess:        entityAccess,
		appMetrics:          appMetrics,
		network:             network,
		runner:              runner,
		ingress:             ingress,
		registryHostMapping: registryHostMapping,
		ociRegistry:         ociRegistry,
		workAdmission:       workAdmission,
		buildSagaRecovery:   buildSagaRecovery,
	}
}

func containerdBootConfig(options StartOptions) containerdcomp.BootConfig {
	config := options.Config.Containerd
	dataPath := options.Config.Server.GetDataPath()
	socketPath := config.GetSocketPath()
	if socketPath == "" {
		socketPath = filepath.Join(dataPath, "containerd", "containerd.sock")
	}

	var bootConfig containerdcomp.BootConfig
	if config.GetStartEmbedded() {
		bootConfig = containerdcomp.EmbeddedBootConfig(options.Log, dataPath,
			config.GetBinaryPath(), options.Config.Server.GetReleasePath(), socketPath)
	} else {
		bootConfig = containerdcomp.ExternalBootConfig(options.Log, dataPath, socketPath)
	}
	bootConfig.StopTimeout = componentStopTimeout
	return bootConfig
}
