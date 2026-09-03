//go:build linux

package distributedrunner

import (
	"fmt"

	containerdcomp "miren.dev/runtime/components/containerd"
	runnercomp "miren.dev/runtime/components/runner"
	"miren.dev/runtime/pkg/boot"
)

// startup is the composition root. Each component owns its inputs, resources,
// outputs, and cleanup. The typed outputs below are also the graph edges.
type startup struct {
	runtime       *Runtime
	containerd    *containerdcomp.Boot
	clusterAccess *clusterAccessBoot
	nodeStorage   *nodeStorageBoot
	telemetry     *telemetryBoot
	sandboxHost   *sandboxHostBoot
	storageAgent  *runnercomp.CapabilityBoot[*runnercomp.StorageAgent]
	sandboxAgent  *runnercomp.CapabilityBoot[*runnercomp.SandboxAgent]
	nodePresence  *runnercomp.CapabilityBoot[*runnercomp.NodePresence]
}

func newStartup(runtime *Runtime, options StartOptions) *startup {
	containerd := containerdcomp.NewBoot("containerd", containerdBootConfig(options))
	clusterAccess := newClusterAccessBoot(clusterAccessInputs(options))
	nodeStorage := newNodeStorageBoot(clusterAccess.output)
	telemetry := newTelemetryBoot(telemetryInputs(options), clusterAccess.output)
	sandboxHost := newSandboxHostBoot(
		sandboxHostInputs(options),
		clusterAccess.output,
		nodeStorage.output,
		containerd.Output,
		telemetry.output,
	)
	storageAgent := runnercomp.NewStorageAgentBoot(nodeStorage.output, sandboxHost.component, 0)
	sandboxAgent := runnercomp.NewSandboxAgentBoot(sandboxHost.output, 0)
	nodePresence := runnercomp.NewNodePresenceBoot(sandboxHost.output, storageAgent.Component, sandboxAgent.Component, 0)

	return &startup{
		runtime:       runtime,
		containerd:    containerd,
		clusterAccess: clusterAccess,
		nodeStorage:   nodeStorage,
		telemetry:     telemetry,
		sandboxHost:   sandboxHost,
		storageAgent:  storageAgent,
		sandboxAgent:  sandboxAgent,
		nodePresence:  nodePresence,
	}
}

func (s *startup) addComponents() error {
	components := []*boot.Component{
		s.containerd.Component,
		s.clusterAccess.component,
		s.nodeStorage.component,
		s.telemetry.component,
		s.sandboxHost.component,
		s.storageAgent.Component,
		s.sandboxAgent.Component,
		s.nodePresence.Component,
	}
	for _, component := range components {
		if err := s.runtime.graph.Add(component); err != nil {
			return fmt.Errorf("adding %s: %w", component, err)
		}
	}
	return nil
}
