package deployment

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverreadiness"
)

const deploymentReadinessBudget = 30 * time.Second

type readinessGate struct {
	next   deployment_v1alpha.Deployment
	waiter readiness.Waiter
	log    *slog.Logger
}

// WithReadiness gates new deployment work without delaying reads, cancellation,
// or updates that belong to work already in flight.
func WithReadiness(next deployment_v1alpha.Deployment, waiter readiness.Waiter, log *slog.Logger) deployment_v1alpha.Deployment {
	if waiter == nil {
		return next
	}
	return &readinessGate{next: next, waiter: waiter, log: log.With("module", "deployment-readiness")}
}

func (g *readinessGate) await(ctx context.Context) {
	waitCtx, cancel := context.WithTimeout(ctx, deploymentReadinessBudget)
	defer cancel()
	if err := g.waiter.Await(waitCtx, serverreadiness.SandboxesReady); err != nil {
		g.log.Warn("accepting deployment before dependencies reported ready", "error", err)
	}
}

func (g *readinessGate) CreateDeployment(ctx context.Context, state *deployment_v1alpha.DeploymentCreateDeployment) error {
	g.await(ctx)
	return g.next.CreateDeployment(ctx, state)
}

func (g *readinessGate) UpdateDeploymentStatus(ctx context.Context, state *deployment_v1alpha.DeploymentUpdateDeploymentStatus) error {
	return g.next.UpdateDeploymentStatus(ctx, state)
}

func (g *readinessGate) UpdateDeploymentPhase(ctx context.Context, state *deployment_v1alpha.DeploymentUpdateDeploymentPhase) error {
	return g.next.UpdateDeploymentPhase(ctx, state)
}

func (g *readinessGate) UpdateFailedDeployment(ctx context.Context, state *deployment_v1alpha.DeploymentUpdateFailedDeployment) error {
	return g.next.UpdateFailedDeployment(ctx, state)
}

func (g *readinessGate) UpdateDeploymentAppVersion(ctx context.Context, state *deployment_v1alpha.DeploymentUpdateDeploymentAppVersion) error {
	return g.next.UpdateDeploymentAppVersion(ctx, state)
}

func (g *readinessGate) ListDeployments(ctx context.Context, state *deployment_v1alpha.DeploymentListDeployments) error {
	return g.next.ListDeployments(ctx, state)
}

func (g *readinessGate) GetDeploymentById(ctx context.Context, state *deployment_v1alpha.DeploymentGetDeploymentById) error {
	return g.next.GetDeploymentById(ctx, state)
}

func (g *readinessGate) GetActiveDeployment(ctx context.Context, state *deployment_v1alpha.DeploymentGetActiveDeployment) error {
	return g.next.GetActiveDeployment(ctx, state)
}

func (g *readinessGate) CancelDeployment(ctx context.Context, state *deployment_v1alpha.DeploymentCancelDeployment) error {
	return g.next.CancelDeployment(ctx, state)
}

func (g *readinessGate) DeployVersion(ctx context.Context, state *deployment_v1alpha.DeploymentDeployVersion) error {
	g.await(ctx)
	return g.next.DeployVersion(ctx, state)
}

func (g *readinessGate) SetEnvVars(ctx context.Context, state *deployment_v1alpha.DeploymentSetEnvVars) error {
	g.await(ctx)
	return g.next.SetEnvVars(ctx, state)
}

func (g *readinessGate) GetDeployLock(ctx context.Context, state *deployment_v1alpha.DeploymentGetDeployLock) error {
	return g.next.GetDeployLock(ctx, state)
}

func (g *readinessGate) DeleteEnvVars(ctx context.Context, state *deployment_v1alpha.DeploymentDeleteEnvVars) error {
	g.await(ctx)
	return g.next.DeleteEnvVars(ctx, state)
}
