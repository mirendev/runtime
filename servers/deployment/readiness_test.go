package deployment

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverreadiness"
)

type recordingDeployment struct {
	deployment_v1alpha.Deployment
	called chan string
}

func (d *recordingDeployment) DeployVersion(context.Context, *deployment_v1alpha.DeploymentDeployVersion) error {
	d.called <- "deploy-version"
	return nil
}

func (d *recordingDeployment) ListDeployments(context.Context, *deployment_v1alpha.DeploymentListDeployments) error {
	d.called <- "list"
	return nil
}

type deploymentWaiter struct {
	conditions chan readiness.Condition
	release    chan struct{}
}

func (w *deploymentWaiter) Await(ctx context.Context, condition readiness.Condition) error {
	w.conditions <- condition
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.release:
		return nil
	}
}

func TestReadinessGateBlocksNewDeploymentUntilSandboxesReady(t *testing.T) {
	next := &recordingDeployment{called: make(chan string, 1)}
	waiter := &deploymentWaiter{
		conditions: make(chan readiness.Condition, 1),
		release:    make(chan struct{}),
	}
	gate := WithReadiness(next, waiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	done := make(chan error, 1)
	go func() { done <- gate.DeployVersion(t.Context(), nil) }()
	require.Equal(t, serverreadiness.SandboxesReady, <-waiter.conditions)
	select {
	case <-next.called:
		t.Fatal("deployment reached the handler before SandboxesReady")
	default:
	}

	close(waiter.release)
	require.NoError(t, <-done)
	require.Equal(t, "deploy-version", <-next.called)
}

func TestReadinessGateDoesNotBlockDeploymentReads(t *testing.T) {
	next := &recordingDeployment{called: make(chan string, 1)}
	waiter := &deploymentWaiter{
		conditions: make(chan readiness.Condition, 1),
		release:    make(chan struct{}),
	}
	gate := WithReadiness(next, waiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, gate.ListDeployments(t.Context(), nil))
	require.Equal(t, "list", <-next.called)
	select {
	case condition := <-waiter.conditions:
		t.Fatalf("read unexpectedly waited for %s", condition)
	default:
	}
}
