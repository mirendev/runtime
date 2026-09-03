//go:build linux

package distributedrunner

import (
	"fmt"

	containerdcomp "miren.dev/runtime/components/containerd"
	"miren.dev/runtime/pkg/boot"
)

// startup is the composition root. Each component owns its inputs, resources,
// outputs, and cleanup. The typed outputs below are also the graph edges.
type startup struct {
	runtime    *Runtime
	containerd *containerdcomp.Boot
	telemetry  *telemetryBoot
	runner     *runnerBoot
}

func newStartup(runtime *Runtime, options StartOptions) *startup {
	containerd := containerdcomp.NewBoot("containerd", containerdBootConfig(options))
	telemetry := newTelemetryBoot(telemetryInputs(options))
	runner := newRunnerBoot(
		runnerInputs(options),
		containerd.Output,
		telemetry.output,
	)

	return &startup{
		runtime:    runtime,
		containerd: containerd,
		telemetry:  telemetry,
		runner:     runner,
	}
}

func (s *startup) addComponents() error {
	components := []*boot.Component{
		s.containerd.Component,
		s.telemetry.component,
		s.runner.component,
	}
	for _, component := range components {
		if err := s.runtime.graph.Add(component); err != nil {
			return fmt.Errorf("adding %s: %w", component, err)
		}
	}
	return nil
}
