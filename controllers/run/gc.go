package run

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

// GCConfig bounds how long runs are kept.
type GCConfig struct {
	// CheckInterval is how often a sweep happens.
	CheckInterval time.Duration

	// RetentionCount is how many terminal runs to keep per app regardless of
	// age. It is per app rather than per task on purpose: an app with two
	// hundred tasks would otherwise accumulate two hundred times this many.
	RetentionCount int

	// RetentionPeriod keeps runs newer than this regardless of count.
	RetentionPeriod time.Duration

	// ConsoleRetention is the shorter window for console runs. Their value is
	// an audit record and a recent scrollback, not a permanent log, and on an
	// app people debug regularly they would otherwise crowd out everything
	// worth reading.
	ConsoleRetention time.Duration

	// ScheduledFloor is the age below which a schedule-triggered run is never
	// deleted.
	//
	// This is a correctness constraint, not a tuning knob. The dedup guard for
	// a scheduled tick *is* the run entity's existence, so removing one while
	// any replica could still evaluate that tick -- one that was partitioned,
	// or restarted behind the others -- lets create-if-absent succeed a second
	// time and the job double-fires. The floor has to sit well beyond any
	// plausible partition, and scheduled runs are exempt from the count cap
	// entirely: a count applied naively to a busy app would evict same-day
	// ticks and silently reintroduce double-firing.
	ScheduledFloor time.Duration

	// OrphanSandbox is how long a terminal run's sandbox may linger before it
	// is torn down.
	OrphanSandbox time.Duration
}

func DefaultGCConfig() GCConfig {
	return GCConfig{
		CheckInterval:    15 * time.Minute,
		RetentionCount:   50,
		RetentionPeriod:  7 * 24 * time.Hour,
		ConsoleRetention: 24 * time.Hour,
		ScheduledFloor:   30 * 24 * time.Hour,
		OrphanSandbox:    5 * time.Minute,
	}
}

// GCResult reports what one sweep did.
type GCResult struct {
	DeletedRuns    int
	FailedRuns     int
	RetainedRuns   int
	StoppedSandbox int
	ReapedOrphans  int
	TotalScanned   int
}

func (r GCResult) didSomething() bool {
	return r.DeletedRuns > 0 || r.FailedRuns > 0 || r.StoppedSandbox > 0 || r.ReapedOrphans > 0
}

// GCController retires finished runs and the sandboxes they leave behind.
type GCController struct {
	Log    *slog.Logger
	EC     *entityserver.Client
	EAC    *entityserver_v1alpha.EntityAccessClient
	Config GCConfig

	cancel context.CancelFunc
}

func NewGCController(log *slog.Logger, ec *entityserver.Client, eac *entityserver_v1alpha.EntityAccessClient) *GCController {
	return &GCController{
		Log:    log.With("module", "run-gc"),
		EC:     ec,
		EAC:    eac,
		Config: DefaultGCConfig(),
	}
}

func (c *GCController) Start(ctx context.Context) {
	c.Log.Info("starting run GC controller",
		"check_interval", c.Config.CheckInterval,
		"retention_count", c.Config.RetentionCount,
		"scheduled_floor", c.Config.ScheduledFloor)

	ctx, c.cancel = context.WithCancel(ctx)
	go c.run(ctx)
}

func (c *GCController) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *GCController) run(ctx context.Context) {
	ticker := time.NewTicker(c.Config.CheckInterval)
	defer ticker.Stop()

	// Let the cluster settle before the first sweep.
	select {
	case <-time.After(30 * time.Second):
		c.sweepAndLog(ctx)
	case <-ctx.Done():
		c.Log.Info("run GC controller stopped")
		return
	}

	for {
		select {
		case <-ctx.Done():
			c.Log.Info("run GC controller stopped")
			return
		case <-ticker.C:
			c.sweepAndLog(ctx)
		}
	}
}

func (c *GCController) sweepAndLog(ctx context.Context) {
	sweepCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	result, err := c.RunGC(sweepCtx, time.Now())
	if err != nil {
		c.Log.Error("run GC sweep failed", "error", err)
		return
	}

	// Info only when something happened; a healthy quiet sweep is Debug.
	if result.didSomething() {
		c.Log.Info("run GC sweep complete",
			"deleted", result.DeletedRuns,
			"failed", result.FailedRuns,
			"retained", result.RetainedRuns,
			"sandboxes_stopped", result.StoppedSandbox,
			"orphans_reaped", result.ReapedOrphans)
		return
	}

	c.Log.Debug("run GC sweep complete, nothing to do", "scanned", result.TotalScanned)
}

// RunGC performs one sweep. now is a parameter so tests can drive it.
func (c *GCController) RunGC(ctx context.Context, now time.Time) (GCResult, error) {
	var result GCResult

	apps, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return result, err
	}

	for _, e := range apps.Values() {
		appID := entity.Id(e.Id())
		if err := c.sweepApp(ctx, appID, now, &result); err != nil {
			c.Log.Warn("failed to sweep app runs", "app", appID, "error", err)
		}
	}

	if err := c.reapOrphanSandboxes(ctx, &result); err != nil {
		c.Log.Warn("failed to reap orphaned run sandboxes", "error", err)
	}

	return result, nil
}

func (c *GCController) sweepApp(ctx context.Context, appID entity.Id, now time.Time, result *GCResult) error {
	results, err := c.EAC.List(ctx, entity.Ref(run_v1alpha.RunAppId, appID))
	if err != nil {
		return err
	}

	type candidate struct {
		run     run_v1alpha.Run
		endedAt time.Time
	}

	var capped []candidate
	for _, e := range results.Values() {
		var r run_v1alpha.Run
		r.Decode(e.Entity())
		r.ID = entity.Id(e.Id())

		result.TotalScanned++

		// Never touch a run that hasn't finished.
		if !isTerminal(r.Status) {
			result.RetainedRuns++
			continue
		}

		if err := c.stopFinishedSandbox(ctx, &r, now, result); err != nil {
			c.Log.Debug("failed to stop sandbox for terminal run", "run", r.ID, "error", err)
		}

		endedAt := r.EndedAt
		if endedAt.IsZero() {
			endedAt = r.StartedAt
		}
		age := now.Sub(endedAt)

		switch {
		case r.Trigger == run_v1alpha.SCHEDULE:
			// Exempt from the count cap; only age retires a tick, and only well
			// past any plausible partition. See ScheduledFloor.
			if endedAt.IsZero() || age < c.Config.ScheduledFloor {
				result.RetainedRuns++
				continue
			}
			c.deleteRun(ctx, r.ID, result)

		case r.Task == consoleTaskName:
			if endedAt.IsZero() || age < c.Config.ConsoleRetention {
				result.RetainedRuns++
				continue
			}
			c.deleteRun(ctx, r.ID, result)

		default:
			capped = append(capped, candidate{run: r, endedAt: endedAt})
		}
	}

	// Newest first, keep RetentionCount, then retire what is also past the age
	// floor. Both have to be satisfied: the count alone would evict a burst of
	// recent runs, and the age alone would let a busy app grow without bound.
	slices.SortFunc(capped, func(a, b candidate) int {
		return b.endedAt.Compare(a.endedAt)
	})

	for i, cand := range capped {
		if i < c.Config.RetentionCount {
			result.RetainedRuns++
			continue
		}
		if cand.endedAt.IsZero() || now.Sub(cand.endedAt) < c.Config.RetentionPeriod {
			result.RetainedRuns++
			continue
		}
		c.deleteRun(ctx, cand.run.ID, result)
	}

	return nil
}

// consoleTaskName mirrors the server's console convention. Duplicated rather
// than imported to keep the controller from depending on the app server.
const consoleTaskName = "console"

func (c *GCController) deleteRun(ctx context.Context, id entity.Id, result *GCResult) {
	if _, err := c.EAC.Delete(ctx, id.String()); err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			c.Log.Warn("failed to delete run", "run", id, "error", err)
			result.FailedRuns++
			return
		}
	}
	result.DeletedRuns++
}

// stopFinishedSandbox tears down a sandbox left running by a finished run.
//
// The run controller does this on the transition; this is the backstop for when
// that write was lost, which the framework makes possible by dropping handler
// errors without requeueing.
func (c *GCController) stopFinishedSandbox(ctx context.Context, r *run_v1alpha.Run, now time.Time, result *GCResult) error {
	if r.Sandbox == "" {
		return nil
	}
	if !r.EndedAt.IsZero() && now.Sub(r.EndedAt) < c.Config.OrphanSandbox {
		return nil
	}

	resp, err := c.EAC.Get(ctx, r.Sandbox.String())
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil
		}
		return err
	}

	var sb compute.Sandbox
	sb.Decode(resp.Entity().Entity())
	if sb.Status == compute.STOPPED || sb.Status == compute.DEAD {
		return nil
	}

	c.Log.Info("stopping sandbox left behind by a finished run", "run", r.ID, "sandbox", r.Sandbox)
	_, err = c.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, r.Sandbox),
		(&compute.Sandbox{Status: compute.STOPPED}).Encode,
	).Attrs(), 0)
	if err != nil {
		return err
	}

	result.StoppedSandbox++
	return nil
}

// reapOrphanSandboxes stops any run sandbox whose run no longer exists.
//
// This is what makes the old exec-sandbox leak un-leakable rather than merely
// less likely. Previously teardown was a deferred call in a request handler, so
// a crash stranded a running sandbox with nothing that would ever collect it.
// Now the run owns the lifecycle -- and this catches the case where the run
// itself is gone, whether garbage collected or deleted with its app.
func (c *GCController) reapOrphanSandboxes(ctx context.Context, result *GCResult) error {
	sandboxes, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
	if err != nil {
		return err
	}

	for _, e := range sandboxes.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())
		sb.ID = entity.Id(e.Id())

		runID := runIDFor(&sb)
		if runID == "" {
			continue
		}
		if sb.Status == compute.DEAD || sb.Status == compute.STOPPED {
			continue
		}

		if _, err := c.EAC.Get(ctx, runID.String()); err == nil {
			continue
		} else if !errors.Is(err, cond.ErrNotFound{}) {
			continue
		}

		c.Log.Info("reaping sandbox whose run no longer exists", "sandbox", sb.ID, "run", runID)
		if _, err := c.EAC.Patch(ctx, entity.New(
			entity.Ref(entity.DBId, sb.ID),
			(&compute.Sandbox{Status: compute.STOPPED}).Encode,
		).Attrs(), 0); err != nil {
			c.Log.Warn("failed to reap orphaned sandbox", "sandbox", sb.ID, "error", err)
			continue
		}

		result.ReapedOrphans++
	}

	return nil
}
