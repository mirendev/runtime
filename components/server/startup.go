//go:build linux

package server

import (
	"context"
	"log/slog"

	"golang.org/x/sync/errgroup"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/serverconfig"
)

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
	containerd          *containerdBoot
	etcd                *etcdBoot
	victoriaLogs        *victoriaLogsBoot
	victoriaMetrics     *victoriaMetricsBoot
	buildkit            *buildkitBoot
	coordinator         *coordinatorBoot
	entityAccess        *entityAccessBoot
	network             *networkBoot
	runner              *runnerBoot
	ingress             *ingressBoot
	registryHostMapping *registryHostMappingBoot
	ociRegistry         *ociRegistryBoot
	buildSagaRecovery   *buildSagaRecoveryBoot
}

func newStartup(runtime *Runtime, options StartOptions) *startup {
	// These are shared in-process capabilities with no lifecycle of their own.
	// The composition root creates one instance of each and passes it directly to
	// the components that share it.
	resolver, hostMapper := netresolve.NewLocalResolver()
	secretRegistry := secret.NewRegistry()
	address := normalizedServerAddress(options.Log, options.Config.Server.GetAddress())

	// This is where we wire up the boot graph. Each component lives in a sibling
	// boot_*.go file. Its input helper narrows external config and shared
	// resources to what it needs. The remaining constructor arguments show its
	// references to upstream components. The readiness spec in each constructor
	// defines the startup ordering it requires. Every component whose output is
	// read must be a direct dependency; a constructor may vary those edges from
	// fixed config, but the graph does not change after startup begins.
	ipDiscovery := newIPDiscoveryBoot(ipDiscoveryInputs(options))
	registration := newRegistrationBoot(registrationInputs(options))
	workloadIdentity := newWorkloadIdentityBoot(workloadIdentityInputs(options), registration)
	tracing := newTracingBoot(tracingInputs(options), registration)
	observability := newObservabilityBoot(observabilityInputs(options), tracing)
	pprof := newPprofBoot(observability)
	containerd := newContainerdBoot(containerdInputs(options), observability)
	etcd := newEtcdBoot(etcdInputs(options), ipDiscovery, containerd, observability)
	victoriaLogs := newVictoriaLogsBoot(victoriaLogsInputs(options), containerd, observability)
	victoriaMetrics := newVictoriaMetricsBoot(victoriaMetricsInputs(options), containerd, observability)
	network := newNetworkBoot(networkInputs(options), etcd, observability)
	registryHostMapping := newRegistryHostMappingBoot(registryHostMappingInputs(hostMapper), network)
	buildkit := newBuildkitBoot(buildkitInputs(options), containerd, registryHostMapping, observability)
	coordinator := newCoordinatorBoot(
		coordinatorInputs(options, runtime.graph, resolver, secretRegistry, address),
		ipDiscovery,
		registration,
		workloadIdentity,
		etcd,
		buildkit,
		observability,
	)
	entityAccess := newEntityAccessBoot(entityAccessInputs(options), coordinator, observability)
	runner := newRunnerBoot(
		runnerInputs(options, resolver, secretRegistry, serverPort(options.Log, address)),
		registration,
		workloadIdentity,
		containerd,
		coordinator,
		entityAccess,
		network,
		observability,
	)
	ingress := newIngressBoot(ingressInputs(options), coordinator, observability)
	ociRegistry := newOCIRegistryBoot(ociRegistryInputs(options), workloadIdentity, entityAccess, registryHostMapping, observability)
	buildSagaRecovery := newBuildSagaRecoveryBoot(
		buildSagaRecoveryInputs(options),
		coordinator,
		buildkit,
		ociRegistry,
		registryHostMapping,
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
		entityAccess:        entityAccess,
		network:             network,
		runner:              runner,
		ingress:             ingress,
		registryHostMapping: registryHostMapping,
		ociRegistry:         ociRegistry,
		buildSagaRecovery:   buildSagaRecovery,
	}
}
