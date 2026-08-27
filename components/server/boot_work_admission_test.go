//go:build linux

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
)

func TestWorkAdmissionWaitsForBuildAndSandboxCapabilities(t *testing.T) {
	graph := boot.NewGraph()
	coordinatorComponent, coordinatorOutput := boot.Provide0("coordinator", func(context.Context) (coordinatorBootOutput, error) {
		return coordinatorBootOutput{coordinator: new(coordinate.Coordinator)}, nil
	})

	runnerStarted := make(chan struct{})
	releaseRunner := make(chan struct{})
	runnerComponent, runnerOutput := boot.Provide0("runner", func(ctx context.Context) (runnerBootOutput, error) {
		close(runnerStarted)
		select {
		case <-ctx.Done():
			return runnerBootOutput{}, ctx.Err()
		case <-releaseRunner:
			return runnerBootOutput{}, nil
		}
	})

	buildkitStarted := make(chan struct{})
	releaseBuildkit := make(chan struct{})
	buildkitComponent, buildkitOutput := boot.Provide0("buildkit", func(ctx context.Context) (buildkitBootOutput, error) {
		close(buildkitStarted)
		select {
		case <-ctx.Done():
			return buildkitBootOutput{}, ctx.Err()
		case <-releaseBuildkit:
			return buildkitBootOutput{}, nil
		}
	})

	ociRegistryComponent, ociRegistryOutput := boot.Provide0("oci-registry", func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	})
	hostMappingComponent, hostMappingOutput := boot.Provide0("registry-host-mapping", func(context.Context) (registryHostMappingBootOutput, error) {
		return registryHostMappingBootOutput{}, nil
	})
	admission := newWorkAdmissionBoot(coordinatorOutput, runnerOutput, buildkitOutput, ociRegistryOutput, hostMappingOutput)

	for _, component := range []*boot.Component{
		coordinatorComponent,
		runnerComponent,
		buildkitComponent,
		ociRegistryComponent,
		hostMappingComponent,
		admission.component,
	} {
		require.NoError(t, graph.Add(component))
	}

	done := make(chan error, 1)
	go func() { done <- graph.Start(t.Context()) }()
	require.Eventually(t, func() bool {
		select {
		case <-runnerStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case <-buildkitStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	assertStillBooting := func() {
		t.Helper()
		select {
		case err := <-done:
			t.Fatalf("work admission ran before its inputs resolved: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	assertStillBooting()
	close(releaseRunner)
	assertStillBooting()
	close(releaseBuildkit)
	require.ErrorContains(t, <-done, "work services are not initialized")
}
