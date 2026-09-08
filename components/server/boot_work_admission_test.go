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

func TestWorkAdmissionWaitsForExecutionCapabilities(t *testing.T) {
	graph := boot.NewGraph()
	applicationsComponent, applicationsOutput := boot.Provide0("application-management", func(context.Context) (applicationManagementBootOutput, error) {
		return applicationManagementBootOutput{applications: coordinate.NewApplicationManagement(new(coordinate.Foundation), nil)}, nil
	})
	workloadsStarted := make(chan struct{})
	releaseWorkloads := make(chan struct{})
	workloadsComponent, _ := boot.Provide0("workload-control", func(ctx context.Context) (workloadControlBootOutput, error) {
		close(workloadsStarted)
		select {
		case <-ctx.Done():
			return workloadControlBootOutput{}, ctx.Err()
		case <-releaseWorkloads:
			return workloadControlBootOutput{}, nil
		}
	})

	nodePresenceStarted := make(chan struct{})
	releaseNodePresence := make(chan struct{})
	nodePresenceComponent, _ := boot.Provide0("node-presence", func(ctx context.Context) (struct{}, error) {
		close(nodePresenceStarted)
		select {
		case <-ctx.Done():
			return struct{}{}, ctx.Err()
		case <-releaseNodePresence:
			return struct{}{}, nil
		}
	})

	buildkitStarted := make(chan struct{})
	releaseBuildkit := make(chan struct{})
	buildkitComponent, _ := boot.Provide0("buildkit", func(ctx context.Context) (buildkitBootOutput, error) {
		close(buildkitStarted)
		select {
		case <-ctx.Done():
			return buildkitBootOutput{}, ctx.Err()
		case <-releaseBuildkit:
			return buildkitBootOutput{}, nil
		}
	})

	ociRegistryComponent, _ := boot.Provide0("oci-registry", func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	})
	hostMappingComponent, _ := boot.Provide0("registry-host-mapping", func(context.Context) (registryHostMappingBootOutput, error) {
		return registryHostMappingBootOutput{}, nil
	})
	admission := newWorkAdmissionBoot(applicationsOutput, workloadsComponent, nodePresenceComponent, buildkitComponent, ociRegistryComponent, hostMappingComponent)

	for _, component := range []*boot.Component{
		applicationsComponent,
		workloadsComponent,
		nodePresenceComponent,
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
		case <-nodePresenceStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case <-workloadsStarted:
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
	close(releaseNodePresence)
	assertStillBooting()
	close(releaseBuildkit)
	assertStillBooting()
	close(releaseWorkloads)
	require.ErrorContains(t, <-done, "build and deployment APIs are not initialized")
}
