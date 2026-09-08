//go:build linux

package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

const (
	// ShutdownTimeout is the overall ceiling for stopping the server graph.
	// Each component also gets its own smaller budget so one slow stop cannot
	// consume the time reserved for components behind it.
	ShutdownTimeout            = 6 * time.Minute
	componentStopTimeout       = 30 * time.Second
	runnerComponentStopTimeout = 2*time.Minute + componentStopTimeout
)

// Runtime owns the started server graph and its top-level services.
type Runtime struct {
	graph         *boot.Graph
	once          sync.Once
	observability boot.Output[observabilityBootOutput]

	ControlPlane *coordinate.ControlPlane
	Runner       *runner.Runner

	stopErr error
}

// Start assembles, validates, and starts the server dependency graph.
func Start(options StartOptions) (*Runtime, error) {
	runtime := &Runtime{graph: boot.NewGraph()}
	components := newStartup(runtime, options)
	runtime.observability = components.observability.output

	if err := components.addComponents(); err != nil {
		return nil, err
	}
	if err := runtime.graph.Validate(); err != nil {
		return nil, err
	}
	if err := runtime.graph.Start(options.Context); err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
		defer cancel()
		_ = runtime.Stop(stopCtx)
		return nil, err
	}
	components.sandboxHost.enableShutdownCleanup()
	runtime.ControlPlane = coordinate.NewControlPlane(
		components.foundation.output.Value().foundation,
		coordinate.ControlPlaneParts{
			Secrets:         components.secretStore.output.Value().secretStore,
			RunnerEndpoints: components.runnerEndpoints.output.Value().endpoints,
			Workloads:       components.workloadControl.output.Value().workloadControl,
			Applications:    components.applicationManagement.output.Value().applications,
			Maintenance:     components.maintenance.output.Value().maintenance,
			Cloud:           components.cloudControl.output.Value().cloud,
		},
	)
	runtime.Runner = &runner.Runner{
		Access:       components.clusterAccess.output.Value().access,
		Storage:      components.nodeStorage.output.Value(),
		Host:         components.sandboxHost.output.Value(),
		StorageAgent: components.storageAgent.Output.Value(),
		SandboxAgent: components.sandboxAgent.Output.Value(),
		Presence:     components.nodePresence.Output.Value(),
	}
	return runtime, nil
}

// Log returns the system-log-enabled logger produced during server boot.
func (r *Runtime) Log() *slog.Logger {
	return r.observability.Value().log
}

func (s *startup) addComponents() error {
	components := []*boot.Component{
		s.ipDiscovery.component,
		s.registration.component,
		s.workloadIdentity.component,
		s.tracing.component,
		s.observability.component,
		s.pprof.component,
		s.containerd.Component,
		s.etcd.component,
		s.victoriaLogs.component,
		s.victoriaMetrics.component,
		s.buildkit.component,
		s.foundation.component,
		s.appData.component,
		s.secretStore.component,
		s.runnerEndpoints.component,
		s.clusterAccess.component,
		s.nodeStorage.component,
		s.deploymentAttempts.component,
		s.entityAccess.component,
		s.appMetrics.component,
		s.network.component,
		s.sandboxHost.component,
		s.storageAgent.Component,
		s.sandboxAgent.Component,
		s.nodePresence.Component,
		s.workloadControl.component,
		s.applicationManagement.component,
		s.maintenance.component,
		s.cloudControl.component,
		s.ingress.component,
		s.admin.component,
		s.registryHostMapping.component,
		s.ociRegistry.component,
		s.workAdmission.component,
		s.buildSagaRecovery.component,
		s.cloudUplink.component,
	}

	for _, component := range components {
		if err := s.runtime.graph.Add(component); err != nil {
			return fmt.Errorf("adding %s: %w", component, err)
		}
	}
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	r.once.Do(func() {
		r.stopErr = r.graph.Stop(ctx)
	})
	return r.stopErr
}
