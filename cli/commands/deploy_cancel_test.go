package commands

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
