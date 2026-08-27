package build

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverreadiness"
)

const buildReadinessBudget = 30 * time.Second

type readinessGate struct {
	next   build_v1alpha.Builder
	waiter readiness.Waiter
	log    *slog.Logger
}

// WithReadiness gates build admission on the dependencies needed by fresh and
// recovered builds. A timeout deliberately fails open, preserving the old
// behavior while making the degraded attempt visible.
func WithReadiness(next build_v1alpha.Builder, waiter readiness.Waiter, log *slog.Logger) build_v1alpha.Builder {
	if waiter == nil {
		return next
	}
	return &readinessGate{next: next, waiter: waiter, log: log.With("module", "build-readiness")}
}

func (g *readinessGate) await(ctx context.Context, conditions ...readiness.Condition) {
	waitCtx, cancel := context.WithTimeout(ctx, buildReadinessBudget)
	defer cancel()
	for _, condition := range conditions {
		if err := g.waiter.Await(waitCtx, condition); err != nil {
			g.log.Warn("accepting build before dependencies reported ready", "condition", condition, "error", err)
		}
	}
}

func (g *readinessGate) BuildFromTar(ctx context.Context, state *build_v1alpha.BuilderBuildFromTar) error {
	conditions := []readiness.Condition{serverreadiness.BuildReady}
	if state != nil && state.Args().HasDeployment() {
		conditions = append(conditions, serverreadiness.SandboxesReady)
	}
	g.await(ctx, conditions...)
	return g.next.BuildFromTar(ctx, state)
}

func (g *readinessGate) BuildFromPrepared(ctx context.Context, state *build_v1alpha.BuilderBuildFromPrepared) error {
	conditions := []readiness.Condition{serverreadiness.BuildReady}
	if state != nil && state.Args().HasDeployment() {
		conditions = append(conditions, serverreadiness.SandboxesReady)
	}
	g.await(ctx, conditions...)
	return g.next.BuildFromPrepared(ctx, state)
}

func (g *readinessGate) PrepareUpload(ctx context.Context, state *build_v1alpha.BuilderPrepareUpload) error {
	return g.next.PrepareUpload(ctx, state)
}

func (g *readinessGate) AnalyzeApp(ctx context.Context, state *build_v1alpha.BuilderAnalyzeApp) error {
	return g.next.AnalyzeApp(ctx, state)
}
