// Package sagagc periodically drives saga retention, deleting executions that
// have been in a terminal state longer than the retention window.
//
// Saga executions are durable by design: the executor persists one on every
// state transition so a crashed process can resume or roll back. Nothing ever
// removed them, so a cluster accumulated one entity per sandbox creation and
// per build forever, each carrying a JSON blob of every action's output. Garden
// reached roughly 4,900 in a month of gentle use, and a single app that fails to
// bind its declared port retries on a loop and writes thousands a day (MIR-1519).
//
// The policy itself lives in pkg/saga, which owns how executions are stored.
// This package is the schedule: when to sweep, how much to do at once, and what
// to tell an operator afterwards.
package sagagc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/saga"
)

const (
	// initialDelay keeps the first sweep clear of the boot storm, and in
	// particular clear of saga recovery, which is walking incomplete executions
	// and transitioning them to terminal states as we start up.
	initialDelay = 2 * time.Minute
)

// GCConfig tunes the retention sweep.
type GCConfig struct {
	// Retention is how long a terminal execution is kept after it finished.
	// Zero disables deletion entirely.
	Retention time.Duration

	// CheckInterval is how often to run a sweep.
	CheckInterval time.Duration

	// MaxDeletesPerSweep caps deletions per sweep so an accumulated backlog
	// drains over several passes. Zero means unbounded.
	MaxDeletesPerSweep int

	// SweepTimeout bounds a single sweep. A truncated sweep is fine; the next
	// one picks up where this left off, since the pass is idempotent.
	SweepTimeout time.Duration
}

// DefaultGCConfig returns the default configuration. The seven-day retention is
// what RFD-35 committed to. The per-sweep cap and interval together drain about
// 48,000 executions a day, comfortably ahead of the worst observed write rate.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		Retention:          7 * 24 * time.Hour,
		CheckInterval:      30 * time.Minute,
		MaxDeletesPerSweep: 1000,
		SweepTimeout:       10 * time.Minute,
	}
}

// GCController periodically deletes expired saga executions. It runs on the
// coordinator, so exactly one process is sweeping and it never contends with
// runners writing saga state through the entity-access client.
type GCController struct {
	Log     *slog.Logger
	Storage saga.Storage
	Config  GCConfig

	cancel context.CancelFunc
}

// Start begins the periodic sweep.
func (c *GCController) Start(ctx context.Context) {
	if c.Config.Retention <= 0 {
		c.Log.Info("saga retention GC disabled, executions will accumulate",
			"reason", "retention is zero")
		return
	}

	c.Log.Info("starting saga retention GC controller",
		"retention", c.Config.Retention,
		"check_interval", c.Config.CheckInterval,
		"max_deletes_per_sweep", c.Config.MaxDeletesPerSweep)

	ctx, c.cancel = context.WithCancel(ctx)
	go c.run(ctx)
}

// Stop gracefully stops the controller.
func (c *GCController) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *GCController) run(ctx context.Context) {
	// Construct the ticker after the initial delay so the first interval is a
	// true CheckInterval rather than a back-to-back double fire.
	select {
	case <-time.After(initialDelay):
		c.sweep(ctx)
	case <-ctx.Done():
		c.Log.Info("saga retention GC controller stopped")
		return
	}

	ticker := time.NewTicker(c.Config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.sweep(ctx)
		case <-ctx.Done():
			c.Log.Info("saga retention GC controller stopped")
			return
		}
	}
}

// sweep runs one bounded pass. It is best-effort: errors are logged, never
// propagated, and never block a foreground operation.
func (c *GCController) sweep(ctx context.Context) {
	sweepCtx := ctx
	if c.Config.SweepTimeout > 0 {
		var cancel context.CancelFunc
		sweepCtx, cancel = context.WithTimeout(ctx, c.Config.SweepTimeout)
		defer cancel()
	}

	result, err := saga.RunRetention(sweepCtx, c.Storage, saga.RetentionConfig{
		Retention:  c.Config.Retention,
		MaxDeletes: c.Config.MaxDeletesPerSweep,
	}, c.Log)
	if err != nil {
		// A truncated sweep is two different events wearing one error. Hitting
		// our own deadline or shutting down is expected and resumes next tick.
		// Anything else means the store would not answer, which is the platform
		// failing at a job it owns, and retention silently stops converging
		// until someone notices.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			c.Log.Warn("saga retention sweep ended early", "error", err,
				"scanned", result.Scanned, "deleted", result.Deleted)
		} else {
			c.Log.Error("saga retention sweep failed", "error", err,
				"scanned", result.Scanned, "deleted", result.Deleted)
		}
		return
	}

	if result.Deleted > 0 || result.Failed > 0 {
		c.Log.Info("saga retention sweep complete",
			"deleted", result.Deleted,
			"failed", result.Failed,
			"skipped", result.Skipped,
			"scanned", result.Scanned,
			"capped", result.Capped)
	} else {
		c.Log.Debug("saga retention sweep complete, nothing expired",
			"scanned", result.Scanned)
	}
}
