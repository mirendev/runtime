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
// The same tick also drives the stalled-saga sweep, which handles the executions
// retention cannot: ones still in flight that nothing will ever resume, so
// nothing will ever move them into a state retention collects (MIR-1788).
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

	// StaleAfter is how long an in-flight execution may sit without changing
	// state before it is declared stranded and forced to failed. Zero disables
	// the stalled sweep, leaving in-flight executions untouched at any age.
	//
	// There is no server config field behind this. The one reason an operator
	// pauses saga GC is to freeze the store while they investigate, and that
	// intent is already spelled saga.retention_period = 0, which the
	// coordinator passes through to both windows. A second knob would ask them
	// to know what an in-flight saga is in order to answer a question they had
	// already answered.
	StaleAfter time.Duration

	// MaxForcesPerSweep caps stalled transitions per sweep, on the same
	// reasoning as MaxDeletesPerSweep. Zero means unbounded.
	MaxForcesPerSweep int
}

// DefaultGCConfig returns the default configuration. The seven-day retention is
// what RFD-35 committed to. The per-sweep cap and interval together drain about
// 48,000 executions a day, comfortably ahead of the worst observed write rate.
//
// StaleAfter reuses the same seven days, which is enormously generous against
// what it is actually measuring: an execution writes a timestamp on every
// transition and every action completion, so the gap this has to clear is one
// action, not one saga. A forced execution then waits out Retention before its
// bytes go, so a stranded record takes about a fortnight to disappear entirely.
// That is the right trade for a population that has already been sitting for
// months: the slow path costs nothing, and it leaves a week in which an
// operator can still see what got forced and why.
//
// Nothing here is tuned separately in practice. The coordinator sets both
// windows from one config field, so these two numbers move together or not at
// all; they are separate fields because the sweeps are separate policies, not
// because an operator is expected to play them against each other.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		Retention:          7 * 24 * time.Hour,
		CheckInterval:      30 * time.Minute,
		MaxDeletesPerSweep: 1000,
		SweepTimeout:       10 * time.Minute,
		StaleAfter:         7 * 24 * time.Hour,
		MaxForcesPerSweep:  1000,
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
//
// Zeroing a window disables that sweep, and there is only a controller to run
// at all if at least one of them is on. An operator reaches this through one
// config field that zeroes both together; the gate reads them separately
// because a caller that wires only one of them is still a caller worth serving,
// and because reading Retention alone here would silently take the stalled
// sweep down with it.
func (c *GCController) Start(ctx context.Context) {
	if c.Config.Retention <= 0 && c.Config.StaleAfter <= 0 {
		c.Log.Info("saga GC disabled, executions will accumulate",
			"reason", "retention and stale window are both zero")
		return
	}

	c.Log.Info("starting saga GC controller",
		"retention", c.Config.Retention,
		"stale_after", c.Config.StaleAfter,
		"check_interval", c.Config.CheckInterval,
		"max_deletes_per_sweep", c.Config.MaxDeletesPerSweep,
		"max_forces_per_sweep", c.Config.MaxForcesPerSweep)

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
		c.Log.Info("saga GC controller stopped")
		return
	}

	ticker := time.NewTicker(c.Config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.sweep(ctx)
		case <-ctx.Done():
			c.Log.Info("saga GC controller stopped")
			return
		}
	}
}

// sweep runs one bounded pass of each policy under a single deadline. It is
// best-effort: errors are logged, never propagated, and never block a
// foreground operation.
//
// The two share one deadline, so a slow retention pass can leave the stalled
// sweep no time and it says so at Warn rather than running past the bound. That
// is the acceptable direction: both passes are idempotent and resume from where
// they stopped, retention is capped at MaxDeletesPerSweep so it cannot run away
// on a backlog, and a stranded execution that waits another half hour has
// already waited months.
func (c *GCController) sweep(ctx context.Context) {
	sweepCtx := ctx
	if c.Config.SweepTimeout > 0 {
		var cancel context.CancelFunc
		sweepCtx, cancel = context.WithTimeout(ctx, c.Config.SweepTimeout)
		defer cancel()
	}

	c.sweepRetention(sweepCtx)
	c.sweepStalled(sweepCtx)
}

func (c *GCController) sweepRetention(ctx context.Context) {
	result, err := saga.RunRetention(ctx, c.Storage, saga.RetentionConfig{
		Retention:  c.Config.Retention,
		MaxDeletes: c.Config.MaxDeletesPerSweep,
	}, c.Log)
	if err != nil {
		c.logSweepError(err, "saga retention sweep",
			"scanned", result.Scanned, "deleted", result.Deleted)
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

// sweepStalled forces stranded in-flight executions to failed.
//
// A sweep that forces anything logs at Info rather than Debug, and says how
// many. On a cluster running v0.14.0 or later the expected number is zero: the
// generated-name addon retries that created these stopped happening when
// convergent IDs shipped. So a line here is either the historical backlog
// draining, which ends, or a saga getting stranded by something we have not
// found yet, which does not. An operator cannot tell those apart without seeing
// the count, and cannot see the count if it never gets logged.
func (c *GCController) sweepStalled(ctx context.Context) {
	result, err := saga.RunStalledSweep(ctx, c.Storage, saga.StalledConfig{
		StaleAfter: c.Config.StaleAfter,
		MaxForces:  c.Config.MaxForcesPerSweep,
	}, c.Log)
	if err != nil {
		c.logSweepError(err, "saga stalled sweep",
			"scanned", result.Scanned, "forced", result.Forced)
		return
	}

	if result.Forced > 0 || result.Failed > 0 {
		c.Log.Info("saga stalled sweep complete",
			"forced", result.Forced,
			"failed", result.Failed,
			"skipped", result.Skipped,
			"recovered", result.Recovered,
			"scanned", result.Scanned,
			"capped", result.Capped)
	} else {
		c.Log.Debug("saga stalled sweep complete, nothing stranded",
			"scanned", result.Scanned)
	}
}

// logSweepError separates the two different events a truncated sweep wears as
// one error. Hitting our own deadline or shutting down is expected and resumes
// next tick. Anything else means the store would not answer, which is the
// platform failing at a job it owns, and GC silently stops converging until
// someone notices.
func (c *GCController) logSweepError(err error, what string, counts ...any) {
	args := append([]any{"error", err}, counts...)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		c.Log.Warn(what+" ended early", args...)
	} else {
		c.Log.Error(what+" failed", args...)
	}
}
