package commands

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"miren.dev/runtime/pkg/cond"
)

func TestIsDeploymentCancelled(t *testing.T) {
	err := fmt.Errorf("build failed: %w", cond.RemoteError("deployment", "cancelled", "deployment cancelled"))
	if !isDeploymentCancelled(err) {
		t.Fatal("expected the deployment cancellation condition to survive wrapping")
	}
	if isDeploymentCancelled(cond.RemoteError("deployment", "conflict", "already active")) {
		t.Fatal("a different deployment condition must not look cancelled")
	}
}

func TestReconcileDeploymentCancellation(t *testing.T) {
	cancelErr := errors.New("cancellation rejected")

	tests := []struct {
		name        string
		status      string
		statusErr   error
		wantOutcome deploymentCancellationOutcome
		wantErr     bool
	}{
		{name: "cancelled confirms cancellation", status: "cancelled", wantOutcome: deploymentCancellationConfirmed},
		{name: "succeeded reports completion", status: "succeeded", wantOutcome: deploymentCancellationCompleted},
		{name: "legacy active reports completion", status: "active", wantOutcome: deploymentCancellationCompleted},
		{name: "in progress preserves cancellation error", status: "in_progress", wantOutcome: deploymentCancellationUnknown, wantErr: true},
		{name: "status read failure preserves cancellation error", statusErr: context.DeadlineExceeded, wantOutcome: deploymentCancellationUnknown, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getter := &mockStatusGetter{
				statuses: []string{tt.status},
				errors:   []error{tt.statusErr},
			}

			outcome, err := reconcileDeploymentCancellation(t.Context(), "dep-123", getter, cancelErr)
			if outcome != tt.wantOutcome {
				t.Fatalf("outcome = %v, want %v", outcome, tt.wantOutcome)
			}
			if tt.wantErr && !errors.Is(err, cancelErr) {
				t.Fatalf("error = %v, want wrapped cancellation error", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// mockStatusGetter is a test mock for deploymentStatusGetter
type mockStatusGetter struct {
	mu        sync.Mutex
	statuses  []string // sequence of statuses to return
	callCount int
	errors    []error // sequence of errors to return (nil for success)
}

func (m *mockStatusGetter) GetStatus(ctx context.Context, deploymentId string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.callCount
	m.callCount++

	if idx < len(m.errors) && m.errors[idx] != nil {
		return "", m.errors[idx]
	}

	if idx < len(m.statuses) {
		return m.statuses[idx], nil
	}

	// Default: return in_progress if no more statuses specified
	return "in_progress", nil
}

func (m *mockStatusGetter) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func TestCancellationPoller_DetectsCancellation(t *testing.T) {
	mock := &mockStatusGetter{
		statuses: []string{"in_progress", "in_progress", "cancelled"},
	}

	poller := newCancellationPoller("test-deployment", mock, 10*time.Millisecond)

	ctx := t.Context()

	var cancelCalled atomic.Bool

	done := make(chan struct{})
	go func() {
		poller.Start(ctx, func() {
			cancelCalled.Store(true)
		})
		close(done)
	}()

	// Wait for poller to finish
	select {
	case <-done:
		// Expected
	case <-time.After(time.Second):
		t.Fatal("poller did not stop after detecting cancellation")
	}

	if !poller.WasExternallyCancelled() {
		t.Error("expected WasExternallyCancelled to be true")
	}

	if !cancelCalled.Load() {
		t.Error("expected cancel function to be called")
	}

	if mock.CallCount() != 3 {
		t.Errorf("expected 3 calls, got %d", mock.CallCount())
	}
}

func TestCancellationPoller_StopsOnContextCancel(t *testing.T) {
	mock := &mockStatusGetter{
		// Never returns cancelled - poller should stop via context
		statuses: []string{"in_progress", "in_progress", "in_progress", "in_progress", "in_progress"},
	}

	poller := newCancellationPoller("test-deployment", mock, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	var cancelCalled atomic.Bool

	done := make(chan struct{})
	go func() {
		poller.Start(ctx, func() {
			cancelCalled.Store(true)
		})
		close(done)
	}()

	// Let it poll a few times
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for poller to finish
	select {
	case <-done:
		// Expected
	case <-time.After(time.Second):
		t.Fatal("poller did not stop after context cancellation")
	}

	if poller.WasExternallyCancelled() {
		t.Error("expected WasExternallyCancelled to be false")
	}

	if cancelCalled.Load() {
		t.Error("expected cancel function NOT to be called on context cancel")
	}
}

func TestCancellationPoller_ContinuesOnError(t *testing.T) {
	testErr := context.DeadlineExceeded

	mock := &mockStatusGetter{
		statuses: []string{"in_progress", "", "cancelled"},
		errors:   []error{nil, testErr, nil},
	}

	poller := newCancellationPoller("test-deployment", mock, 10*time.Millisecond)

	ctx := t.Context()

	var cancelCalled atomic.Bool

	done := make(chan struct{})
	go func() {
		poller.Start(ctx, func() {
			cancelCalled.Store(true)
		})
		close(done)
	}()

	// Wait for poller to finish
	select {
	case <-done:
		// Expected
	case <-time.After(time.Second):
		t.Fatal("poller did not stop after detecting cancellation")
	}

	if !poller.WasExternallyCancelled() {
		t.Error("expected WasExternallyCancelled to be true")
	}

	if !cancelCalled.Load() {
		t.Error("expected cancel function to be called")
	}

	if mock.CallCount() != 3 {
		t.Errorf("expected 3 calls (including error), got %d", mock.CallCount())
	}
}

func TestCancellationPoller_ImmediateCancellation(t *testing.T) {
	mock := &mockStatusGetter{
		statuses: []string{"cancelled"},
	}

	poller := newCancellationPoller("test-deployment", mock, 10*time.Millisecond)

	ctx := t.Context()

	var cancelCalled atomic.Bool

	done := make(chan struct{})
	go func() {
		poller.Start(ctx, func() {
			cancelCalled.Store(true)
		})
		close(done)
	}()

	// Wait for poller to finish
	select {
	case <-done:
		// Expected
	case <-time.After(time.Second):
		t.Fatal("poller did not stop after detecting cancellation")
	}

	if !poller.WasExternallyCancelled() {
		t.Error("expected WasExternallyCancelled to be true")
	}

	if !cancelCalled.Load() {
		t.Error("expected cancel function to be called")
	}

	if mock.CallCount() != 1 {
		t.Errorf("expected 1 call, got %d", mock.CallCount())
	}
}
