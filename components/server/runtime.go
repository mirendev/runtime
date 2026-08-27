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
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverreadiness"
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
	graph         *readiness.Graph
	once          sync.Once
	observability *observabilityBoot

	Coordinator *coordinate.Coordinator
	Runner      *runner.Runner

	stopErr error
}

// Start assembles, validates, and starts the server dependency graph.
func Start(options StartOptions) (*Runtime, error) {
	runtime := &Runtime{graph: readiness.NewGraph()}
	boot := newStartup(runtime, options)
	runtime.observability = boot.observability

	if err := boot.addComponents(); err != nil {
		return nil, err
	}
	if err := boot.declareConditions(); err != nil {
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
	boot.runner.enableShutdownCleanup()
	runtime.Coordinator = boot.coordinator.output().coordinator
	runtime.Runner = boot.runner.output().runner
	return runtime, nil
}

// Log returns the system-log-enabled logger produced during server boot.
func (r *Runtime) Log() *slog.Logger {
	return r.observability.output().log
}

func (s *startup) addComponents() error {
	components := []*readiness.Component{
		s.ipDiscovery.component,
		s.registration.component,
		s.workloadIdentity.component,
		s.tracing.component,
		s.observability.component,
		s.pprof.component,
		s.containerd.component,
		s.etcd.component,
		s.victoriaLogs.component,
		s.victoriaMetrics.component,
		s.buildkit.component,
		s.coordinator.component,
		s.entityAccess.component,
		s.network.component,
		s.runner.component,
		s.ingress.component,
		s.registryHostMapping.component,
		s.ociRegistry.component,
		s.buildSagaRecovery.component,
	}

	for _, component := range components {
		if err := s.runtime.graph.Add(component); err != nil {
			return fmt.Errorf("adding %s: %w", component, err)
		}
	}
	return nil
}

func (s *startup) declareConditions() error {
	// Conditions give consumers named readiness states without exposing the boot
	// graph. They do not add startup dependencies; a condition holds when every
	// listed provider is ready.
	conditions := []struct {
		condition readiness.Condition
		providers []*readiness.Component
	}{
		// We are ready to run builds when BuildKit, registry host mapping, and the OCI registry are ready.
		{serverreadiness.BuildReady, []*readiness.Component{s.buildkit.component, s.ociRegistry.component, s.registryHostMapping.component}},
		// We are ready to launch sandboxes when the container runtime, coordinator, network, and runner are ready.
		{serverreadiness.SandboxesReady, []*readiness.Component{s.containerd.component, s.coordinator.component, s.network.component, s.runner.component}},
		// We are ready to serve traffic when the coordinator and ingress are ready.
		{serverreadiness.ServeReady, []*readiness.Component{s.coordinator.component, s.ingress.component}},
	}

	for _, condition := range conditions {
		if err := s.runtime.graph.AddCondition(condition.condition, condition.providers...); err != nil {
			return fmt.Errorf("declaring %s: %w", condition.condition, err)
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
