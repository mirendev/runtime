package workload

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrDraining    = errors.New("workload: sandbox is draining")
	ErrUnavailable = errors.New("workload: Session is not accepting work")
)

// StopFunc returns only once all resources have closed. Failure prevents deletion
// acknowledgment and is retried. It must tolerate repeated calls.
type StopFunc func(context.Context) error

// StartFunc starts an independent loop. Its context is canceled on removal or
// shutdown. The returned StopFunc waits for cleanup, including any in-flight work.
type StartFunc func(context.Context, Session) (StopFunc, error)

type assignment struct {
	ctx       context.Context
	cancel    context.CancelFunc
	stop      StopFunc
	work      int
	accepting bool
}

// Host manages assignments, cleanup, activity, and shutdown admission. Create
// one per sandbox and call Run once. It does not persist application state.
type Host struct {
	client      *Client
	mu          sync.Mutex
	assignments map[string]*assignment
	running     bool
	shutdownAt  time.Time
	draining    chan struct{}
	wake        chan struct{}
	reportMu    sync.Mutex
}

func NewHost(cfg Config) (*Host, error) {
	c, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Host{client: c, assignments: map[string]*assignment{}, draining: make(chan struct{}), wake: make(chan struct{}, 1)}, nil
}

// Draining closes once shutdown is advertised. It never reopens, even if a
// later response omits the notice. Already accepted work may finish.
func (h *Host) Draining() <-chan struct{} { return h.draining }

func (h *Host) ShutdownAt() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shutdownAt
}

func (h *Host) wakeReporter() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *Host) report(ctx context.Context) error {
	h.reportMu.Lock()
	defer h.reportMu.Unlock()
	return h.reportLocked(ctx)
}

func (h *Host) reportLocked(ctx context.Context) error {
	h.mu.Lock()
	active := false
	type activityReport struct {
		active     bool
		assignment *assignment
	}
	reports := make(map[string]activityReport)
	for id, a := range h.assignments {
		active = active || a.work > 0
		if a.accepting {
			reports[id] = activityReport{a.work > 0, a}
		}
	}
	h.mu.Unlock()
	deadline, err := h.client.ReportActivity(ctx, active)
	if err != nil {
		return err
	}
	if !deadline.IsZero() {
		h.mu.Lock()
		if h.shutdownAt.IsZero() {
			close(h.draining)
		}
		h.shutdownAt = deadline
		h.mu.Unlock()
	}
	for id, report := range reports {
		if err := h.client.ReportSessionActivity(ctx, id, report.active); err != nil {
			var status *HTTPError
			if errors.As(err, &status) && (status.StatusCode == 409 || status.StatusCode == 403) {
				h.mu.Lock()
				if a := h.assignments[id]; a == report.assignment {
					a.accepting = false
				}
				h.mu.Unlock()
				continue // Only this assignment was withdrawn, not its neighbours.
			}
			return err
		}
	}
	return nil
}

// Begin reserves activity before accepting work, including queued work. It
// reports active synchronously and refuses work if the reply advertises shutdown
// or cannot be obtained. Always call the returned, idempotent release function.
func (h *Host) Begin(ctx context.Context, id string) (func(), error) {
	h.reportMu.Lock()
	defer h.reportMu.Unlock()
	h.mu.Lock()
	if !h.shutdownAt.IsZero() {
		h.mu.Unlock()
		return nil, ErrDraining
	}
	a := h.assignments[id]
	if a == nil || !a.accepting || a.ctx.Err() != nil {
		h.mu.Unlock()
		return nil, ErrUnavailable
	}
	a.work++
	h.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			h.mu.Lock()
			a.work--
			h.mu.Unlock()
			h.wakeReporter()
		})
	}
	err := h.reportLocked(ctx)
	h.mu.Lock()
	if err == nil && !h.shutdownAt.IsZero() {
		err = ErrDraining
	}
	if err == nil && (!a.accepting || a.ctx.Err() != nil || h.assignments[id] != a) {
		err = ErrUnavailable
	}
	h.mu.Unlock()
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func retryable(err error) bool {
	var status *HTTPError
	return !errors.As(err, &status) || status.StatusCode >= 500 || status.StatusCode == 429
}

func pause(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(time.Second):
		return true
	}
}

func (h *Host) remove(ctx context.Context, id string) error {
	h.mu.Lock()
	a := h.assignments[id]
	if a != nil {
		a.accepting = false
		a.cancel()
	}
	h.mu.Unlock()
	if a == nil {
		return nil
	}
	cleanup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := a.stop(cleanup); err != nil {
		return fmt.Errorf("workload: Session cleanup failed: %w", err)
	}
	h.mu.Lock()
	delete(h.assignments, id)
	h.mu.Unlock()
	h.wakeReporter()
	return nil
}

// Run watches assignments and renews activity every ten seconds. Start failures
// and permanent HTTP errors are returned; transport, server, cleanup, and ack
// failures are retried. Cancellation stops every loop before Run returns.
// Start callbacks must not block waiting for their loop to finish.
func (h *Host) Run(ctx context.Context, start StartFunc) (result error) {
	if start == nil {
		return fmt.Errorf("workload: start callback is required")
	}
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return fmt.Errorf("workload: Run may only be called once")
	}
	h.running = true
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	reporterDone := make(chan struct{})
	reporterError := make(chan error, 1)
	go func() {
		defer close(reporterDone)
		for ctx.Err() == nil {
			if err := h.report(ctx); err != nil {
				if !retryable(err) {
					reporterError <- err
					cancel()
					return
				}
				if !pause(ctx) {
					return
				}
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-h.wake:
			case <-time.After(10 * time.Second):
			}
		}
	}()
	defer func() {
		cancel()
		<-reporterDone
		select {
		case err := <-reporterError:
			result = err
		default:
		}
		h.mu.Lock()
		var ids []string
		for id, a := range h.assignments {
			a.accepting = false
			a.cancel()
			ids = append(ids, id)
		}
		h.mu.Unlock()
		for _, id := range ids {
			result = errors.Join(result, h.remove(context.Background(), id))
		}
	}()
	version := ""
	for ctx.Err() == nil {
		snapshot, err := h.client.Sessions(ctx, version)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if !retryable(err) {
				return err
			}
			if !pause(ctx) {
				break
			}
			continue
		}
		if snapshot == nil {
			continue
		}
		assigned := make(map[string]bool, len(snapshot.Sessions))
		for _, id := range snapshot.Sessions {
			assigned[id] = true
			h.mu.Lock()
			exists := h.assignments[id] != nil
			draining := !h.shutdownAt.IsZero()
			h.mu.Unlock()
			if exists || draining {
				continue
			}
			session, ok := snapshot.Details[id]
			if !ok {
				return fmt.Errorf("workload: assignment lacks Session details")
			}
			loopCtx, stopLoop := context.WithCancel(ctx)
			stop, err := start(loopCtx, session)
			if err != nil {
				stopLoop()
				return err
			}
			if stop == nil {
				stopLoop()
				return fmt.Errorf("workload: start callback returned no cleanup function")
			}
			h.mu.Lock()
			h.assignments[id] = &assignment{ctx: loopCtx, cancel: stopLoop, stop: stop, accepting: true}
			h.mu.Unlock()
			h.wakeReporter()
		}
		h.mu.Lock()
		var removed []string
		for id, a := range h.assignments {
			if !assigned[id] {
				a.accepting = false
				a.cancel()
				removed = append(removed, id)
			}
		}
		h.mu.Unlock()
		complete := true
		for _, id := range removed {
			if err := h.remove(ctx, id); err != nil {
				complete = false
			}
		}
		for _, id := range snapshot.Deleted {
			if err := h.remove(ctx, id); err != nil {
				complete = false
				continue
			}
			if err := h.client.AcknowledgeDeletion(ctx, id); err != nil {
				if ctx.Err() == nil && !retryable(err) {
					return err
				}
				complete = false
			}
		}
		for id, detachedAt := range snapshot.Detached {
			if err := h.remove(ctx, id); err != nil {
				complete = false
				continue
			}
			if err := h.client.AcknowledgeDetachment(ctx, id, detachedAt); err != nil {
				if ctx.Err() == nil && !retryable(err) {
					return err
				}
				complete = false
			}
		}
		if complete {
			version = snapshot.Version
		} else {
			version = ""
			if !pause(ctx) {
				break
			}
		}
	}
	return nil
}
