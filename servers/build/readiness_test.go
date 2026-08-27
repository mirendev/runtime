package build

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/pkg/readiness"
	"miren.dev/runtime/pkg/serverreadiness"
)

type recordingBuilder struct {
	mu     sync.Mutex
	calls  []string
	called chan struct{}
}

func newRecordingBuilder() *recordingBuilder {
	return &recordingBuilder{called: make(chan struct{}, 1)}
}

func (b *recordingBuilder) record(name string) {
	b.mu.Lock()
	b.calls = append(b.calls, name)
	b.mu.Unlock()
	b.called <- struct{}{}
}

func (b *recordingBuilder) BuildFromTar(context.Context, *build_v1alpha.BuilderBuildFromTar) error {
	b.record("tar")
	return nil
}

func (b *recordingBuilder) BuildFromPrepared(context.Context, *build_v1alpha.BuilderBuildFromPrepared) error {
	b.record("prepared")
	return nil
}

func (b *recordingBuilder) PrepareUpload(context.Context, *build_v1alpha.BuilderPrepareUpload) error {
	b.record("prepare")
	return nil
}

func (b *recordingBuilder) AnalyzeApp(context.Context, *build_v1alpha.BuilderAnalyzeApp) error {
	b.record("analyze")
	return nil
}

type blockingWaiter struct {
	conditions chan readiness.Condition
	release    chan struct{}
}

func (w *blockingWaiter) Await(ctx context.Context, condition readiness.Condition) error {
	w.conditions <- condition
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.release:
		return nil
	}
}

func TestReadinessGateBlocksBuildAdmissionUntilBuildReady(t *testing.T) {
	next := newRecordingBuilder()
	waiter := &blockingWaiter{
		conditions: make(chan readiness.Condition, 1),
		release:    make(chan struct{}),
	}
	gate := WithReadiness(next, waiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	done := make(chan error, 1)
	go func() {
		done <- gate.BuildFromTar(t.Context(), nil)
	}()

	require.Equal(t, serverreadiness.BuildReady, <-waiter.conditions)
	select {
	case <-next.called:
		t.Fatal("build reached the handler before BuildReady")
	default:
	}

	close(waiter.release)
	require.NoError(t, <-done)
	<-next.called
}

func TestReadinessGateDoesNotBlockPreparation(t *testing.T) {
	next := newRecordingBuilder()
	waiter := &blockingWaiter{
		conditions: make(chan readiness.Condition, 1),
		release:    make(chan struct{}),
	}
	gate := WithReadiness(next, waiter, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, gate.PrepareUpload(t.Context(), nil))
	<-next.called
	select {
	case condition := <-waiter.conditions:
		t.Fatalf("preparation unexpectedly waited for %s", condition)
	default:
	}
}

func TestReadinessGateFailsOpenWhenRequestContextEnds(t *testing.T) {
	next := newRecordingBuilder()
	waiter := &blockingWaiter{
		conditions: make(chan readiness.Condition, 1),
		release:    make(chan struct{}),
	}
	gate := WithReadiness(next, waiter, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.NoError(t, gate.BuildFromPrepared(ctx, nil))
	require.Equal(t, serverreadiness.BuildReady, <-waiter.conditions)
	<-next.called
}

func TestDeploymentBuildWaitsForBuildAndSandboxConditions(t *testing.T) {
	waiter := &blockingWaiter{
		conditions: make(chan readiness.Condition, 2),
		release:    make(chan struct{}),
	}
	gate := WithReadiness(
		newRecordingBuilder(),
		waiter,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).(*readinessGate)

	close(waiter.release)
	gate.await(t.Context(), serverreadiness.BuildReady, serverreadiness.SandboxesReady)
	require.Equal(t, serverreadiness.BuildReady, <-waiter.conditions)
	require.Equal(t, serverreadiness.SandboxesReady, <-waiter.conditions)
}
