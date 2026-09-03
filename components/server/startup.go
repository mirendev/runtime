//go:build linux

package server

import (
	"context"
	"log/slog"
	"path/filepath"

	"golang.org/x/sync/errgroup"
	containerdcomp "miren.dev/runtime/components/containerd"
	"miren.dev/runtime/components/netresolve"
	runnercomp "miren.dev/runtime/components/runner"
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
	runtime               *Runtime
	ipDiscovery           *ipDiscoveryBoot
	registration          *registrationBoot
	workloadIdentity      *workloadIdentityBoot
	tracing               *tracingBoot
	observability         *observabilityBoot
	pprof                 *pprofBoot
	containerd            *containerdcomp.Boot
	etcd                  *etcdBoot
	victoriaLogs          *victoriaLogsBoot
	victoriaMetrics       *victoriaMetricsBoot
	buildkit              *buildkitBoot
	foundation            *foundationBoot
	appData               *appDataBoot
	secretStore           *secretStoreBoot
	runnerEndpoints       *runnerEndpointsBoot
	clusterAccess         *clusterAccessBoot
	nodeStorage           *nodeStorageBoot
	workloadControl       *workloadControlBoot
	applicationManagement *applicationManagementBoot
	maintenance           *entityMaintenanceBoot
	cloudControl          *cloudControlBoot
	deploymentAttempts    *deploymentAttemptMigrationBoot
	cloudUplink           *cloudUplinkBoot
	entityAccess          *entityAccessBoot
	appMetrics            *appMetricsBoot
	network               *networkBoot
	sandboxHost           *sandboxHostBoot
	storageAgent          *runnercomp.CapabilityBoot[*runnercomp.StorageAgent]
	sandboxAgent          *runnercomp.CapabilityBoot[*runnercomp.SandboxAgent]
	nodePresence          *runnercomp.CapabilityBoot[*runnercomp.NodePresence]
	ingress               *ingressBoot
	admin                 *adminBoot
	registryHostMapping   *registryHostMappingBoot
	ociRegistry           *ociRegistryBoot
	workAdmission         *workAdmissionBoot
	buildSagaRecovery     *buildSagaRecoveryBoot
}

func newStartup(runtime *Runtime, options StartOptions) *startup {
	// These are shared in-process capabilities with no lifecycle of their own.
	// The composition root creates one instance of each and passes it directly to
	// the components that share it.
	resolver, hostMapper := netresolve.NewLocalResolver()
	secretRegistry := secret.NewRegistry()
	address := NormalizeServerAddress(options.Log, options.Config.Server.GetAddress())

	// This is where we wire up the boot graph. Each component lives in a sibling
	// boot_*.go file. Its input helper narrows external config and shared
	// resources to what it needs. The remaining constructor arguments are typed
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
	observability := newObservabilityBoot(observabilityInputs(options), tracing.component, victoriaLogs.output, victoriaMetrics.output)
	pprof := newPprofBoot(observability.output)
	etcd := newEtcdBoot(etcdInputs(options), ipDiscovery.output, containerd.Output, observability.output)
	network := newNetworkBoot(networkInputs(options), etcd.output, observability.output)
	registryHostMapping := newRegistryHostMappingBoot(registryHostMappingInputs(hostMapper), network.output)
	buildkit := newBuildkitBoot(buildkitInputs(options), containerd.Output, registryHostMapping.output, network.output, observability.output)
	foundation := newFoundationBoot(
		foundationConfig(options, resolver, secretRegistry, address),
		ipDiscovery.output,
		registration.output,
		workloadIdentity.output,
		etcd.output,
		buildkit.output,
		observability.output,
	)
	appData := newAppDataBoot(foundation.output)
	secretStore := newSecretStoreBoot(foundation.output)
	runnerEndpoints := newRunnerEndpointsBoot(foundation.output, secretStore.component)
	deploymentAttempts := newDeploymentAttemptMigrationBoot(foundation.output, appData.component)
	entityAccess := newEntityAccessBoot(entityAccessInputs(options), foundation.output, observability.output)
	appMetrics := newAppMetricsBoot(
		appMetricsInputs(options),
		containerd.Output,
		registration.output,
		workloadIdentity.output,
		entityAccess.output,
		observability.output,
	)
	clusterAccess := newClusterAccessBoot(
		clusterAccessBootInputs{config: options.Config.Server},
		foundation.output,
		workloadIdentity.output,
		secretStore.output,
		observability.output,
		runnerEndpoints.component,
	)
	nodeStorage := newNodeStorageBoot(clusterAccess.output, registration.output)
	sandboxHost := newSandboxHostBoot(
		sandboxHostInputs(options, resolver, serverPort(options.Log, address)),
		clusterAccess.output,
		nodeStorage.output,
		containerd.Output,
		network.output,
		observability.output,
	)
	storageAgent := runnercomp.NewStorageAgentBoot(nodeStorage.output, sandboxHost.component, componentStopTimeout)
	applicationManagement := newApplicationManagementBoot(foundation.output, secretStore.output, appData.component)
	workloadControl := newWorkloadControlBoot(foundation.output, applicationManagement.output, sandboxHost.component)
	sandboxAgent := runnercomp.NewSandboxAgentBoot(sandboxHost.output, componentStopTimeout, workloadControl.component)
	nodePresence := runnercomp.NewNodePresenceBoot(sandboxHost.output, storageAgent.Component, sandboxAgent.Component, componentStopTimeout)
	maintenance := newEntityMaintenanceBoot(foundation.output, appData.component)
	cloudControl := newCloudControlBoot(foundation.output, applicationManagement.component, maintenance.component, workloadControl.component)
	ingress := newIngressBoot(ingressInputs(options), workloadControl.output, nodePresence.Component, workloadIdentity.output, entityAccess.output, observability.output)
	adminAPI := newAdminBoot(foundation.output, entityAccess.output, ingress.output, observability.output)
	cloudUplink := newCloudUplinkBoot(cloudControl.output, deploymentAttempts.output, ingress.output)
	ociRegistry := newOCIRegistryBoot(ociRegistryInputs(options), workloadIdentity.output, entityAccess.output, registryHostMapping.component, observability.output)
	workAdmission := newWorkAdmissionBoot(applicationManagement.output, workloadControl.component, nodePresence.Component, buildkit.component, ociRegistry.component, registryHostMapping.component)
	buildSagaRecovery := newBuildSagaRecoveryBoot(
		buildSagaRecoveryInputs(options),
		applicationManagement.output,
		workAdmission.component,
	)

	return &startup{
		runtime:               runtime,
		ipDiscovery:           ipDiscovery,
		registration:          registration,
		workloadIdentity:      workloadIdentity,
		tracing:               tracing,
		observability:         observability,
		pprof:                 pprof,
		containerd:            containerd,
		etcd:                  etcd,
		victoriaLogs:          victoriaLogs,
		victoriaMetrics:       victoriaMetrics,
		buildkit:              buildkit,
		foundation:            foundation,
		appData:               appData,
		secretStore:           secretStore,
		runnerEndpoints:       runnerEndpoints,
		clusterAccess:         clusterAccess,
		nodeStorage:           nodeStorage,
		workloadControl:       workloadControl,
		applicationManagement: applicationManagement,
		maintenance:           maintenance,
		cloudControl:          cloudControl,
		deploymentAttempts:    deploymentAttempts,
		cloudUplink:           cloudUplink,
		entityAccess:          entityAccess,
		appMetrics:            appMetrics,
		network:               network,
		sandboxHost:           sandboxHost,
		storageAgent:          storageAgent,
		sandboxAgent:          sandboxAgent,
		nodePresence:          nodePresence,
		ingress:               ingress,
		admin:                 adminAPI,
		registryHostMapping:   registryHostMapping,
		ociRegistry:           ociRegistry,
		workAdmission:         workAdmission,
		buildSagaRecovery:     buildSagaRecovery,
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
