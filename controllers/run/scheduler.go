package run

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/oncalendar"
)

// SchedulerInterval is how often the scheduler looks for ticks that have come
// due. It bounds how late a run can start, not how precisely ticks are
// computed: tick times come from the calendar expression, so a slow poll delays
// a run without shifting the schedule.
const SchedulerInterval = 15 * time.Second

// Scheduler creates runs for tasks whose calendar expression has come due.
//
// It holds no leader election and no external coordination. A tick is a pure
// function of the stored calendar expression, so every replica derives exactly
// the same firing times; the run's entity id is derived from the tick, which
// makes creating it a put-if-absent that etcd resolves to a single winner. The
// losers see the run already exists and move on.
//
// The consequence worth stating: the dedup guard is the run entity's existence.
// Deleting a tick's run resurrects that tick, which is why retention treats
// scheduled runs as a correctness constraint rather than a preference.
type Scheduler struct {
	Log *slog.Logger
	EC  *entityserver.Client
	EAC *entityserver_v1alpha.EntityAccessClient

	Interval time.Duration

	cancel func()

	// mu guards frontier, which records the newest tick already considered for
	// each (app, task). It is in-memory on purpose: on startup the frontier
	// begins at now, which is what makes missed ticks skipped rather than
	// backfilled.
	mu       sync.Mutex
	frontier map[string]time.Time

	// fire is the tick-firing step, indirected so a test can make it fail.
	// The frontier only advances past ticks that were actually handled, and
	// that rule is invisible unless a failure can be injected -- a sweep where
	// everything succeeds behaves identically whether the frontier moves before
	// or after firing.
	fire func(context.Context, *core_v1alpha.App, *core_v1alpha.AppVersion, string, core_v1alpha.ConfigSpecTasks, time.Time) error
}

func NewScheduler(log *slog.Logger, ec *entityserver.Client, eac *entityserver_v1alpha.EntityAccessClient) *Scheduler {
	s := &Scheduler{
		Log:      log.With("module", "run-scheduler"),
		EC:       ec,
		EAC:      eac,
		Interval: SchedulerInterval,
		frontier: make(map[string]time.Time),
	}
	s.fire = s.fireTick
	return s
}

func (s *Scheduler) Start(ctx context.Context) {
	s.Log.Info("starting run scheduler", "interval", s.Interval)

	ctx, s.cancel = context.WithCancel(ctx)
	go s.run(ctx)
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Scheduler) run(ctx context.Context) {
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.Log.Info("run scheduler stopped")
			return
		case <-ticker.C:
			if err := s.Sweep(ctx, time.Now()); err != nil {
				s.Log.Warn("scheduler sweep failed", "error", err)
			}
		}
	}
}

// Sweep fires every tick that has come due since the last sweep.
//
// now is a parameter so tests can drive it; in production it is time.Now.
func (s *Scheduler) Sweep(ctx context.Context, now time.Time) error {
	apps, err := s.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return fmt.Errorf("listing apps: %w", err)
	}

	for _, e := range apps.Values() {
		var app core_v1alpha.App
		app.Decode(e.Entity())
		app.ID = entity.Id(e.Id())

		if app.ActiveVersion == "" {
			continue
		}

		if err := s.sweepApp(ctx, &app, now); err != nil {
			s.Log.Warn("failed to sweep app for scheduled tasks", "app", app.ID, "error", err)
		}
	}

	return nil
}

func (s *Scheduler) sweepApp(ctx context.Context, app *core_v1alpha.App, now time.Time) error {
	var ver core_v1alpha.AppVersion
	if err := s.EC.GetById(ctx, app.ActiveVersion, &ver); err != nil {
		return fmt.Errorf("reading active version: %w", err)
	}

	cfgSpec, err := coreutil.ResolveConfig(ctx, s.EAC, &ver)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}

	var md core_v1alpha.Metadata
	if err := s.EC.GetById(ctx, app.ID, &md); err != nil {
		return fmt.Errorf("reading app metadata: %w", err)
	}

	for _, task := range cfgSpec.Tasks {
		if task.Schedule == "" {
			continue
		}
		if err := s.sweepTask(ctx, app, &ver, md.Name, task, now); err != nil {
			s.Log.Warn("failed to sweep task", "app", app.ID, "task", task.Name, "error", err)
		}
	}

	return nil
}

func (s *Scheduler) sweepTask(
	ctx context.Context,
	app *core_v1alpha.App,
	ver *core_v1alpha.AppVersion,
	appName string,
	task core_v1alpha.ConfigSpecTasks,
	now time.Time,
) error {
	expr, err := oncalendar.Parse(task.Schedule)
	if err != nil {
		// Config validation rejects unparseable schedules, so reaching here
		// means a version that predates that check. The error is returned and
		// logged by the caller on every sweep, which is noisy but honest: a
		// task the platform cannot schedule should keep saying so rather than
		// going quiet and looking healthy.
		return fmt.Errorf("parsing schedule %q: %w", task.Schedule, err)
	}

	key := app.ID.String() + "/" + task.Name

	s.mu.Lock()
	from, seen := s.frontier[key]
	if !seen {
		// First sight of this task: start the frontier at now, so ticks that
		// came due while the cluster was down are skipped rather than
		// backfilled. Replaying an outage's worth of cleanups on recovery is a
		// good way to turn an outage into an incident, and a job that needs
		// catch-up can be made idempotent over a window instead.
		from = now
		s.frontier[key] = now
	}
	s.mu.Unlock()

	// Only fire ticks this replica's own clock has passed. Skew therefore makes
	// a fast replica fire early rather than twice, which matters for a
	// nine-o'clock report and not for a six-hourly cleanup.
	var fired []time.Time
	cursor := from
	for {
		next, ok := expr.Next(cursor)
		if !ok || next.After(now) {
			break
		}
		fired = append(fired, next)
		cursor = next

		// A pathological expression plus a long outage could otherwise produce
		// an unbounded list; the frontier advance below still moves past them.
		if len(fired) >= 64 {
			break
		}
	}

	// Advance the frontier only past ticks that were actually handled. Moving
	// it first would drop any tick whose fire failed -- permanently, since the
	// frontier is what decides a tick is behind us.
	for _, tick := range fired {
		if err := s.fire(ctx, app, ver, appName, task, tick); err != nil {
			return err
		}

		s.mu.Lock()
		s.frontier[key] = tick
		s.mu.Unlock()
	}

	return nil
}

// fireTick creates the run for one tick, or records that it was skipped.
func (s *Scheduler) fireTick(
	ctx context.Context,
	app *core_v1alpha.App,
	ver *core_v1alpha.AppVersion,
	appName string,
	task core_v1alpha.ConfigSpecTasks,
	tick time.Time,
) error {
	name := tickRunName(appName, task.Name, tick)

	// Ticks do not queue and do not overlap. A tick whose predecessor is still
	// going is skipped -- and recorded as skipped, because "my job stopped
	// running" and "my job got slower than its interval" are indistinguishable
	// from the outside otherwise.
	status := run_v1alpha.PENDING
	busy, err := s.taskIsBusy(ctx, app.ID, task.Name)
	if err != nil {
		return err
	}
	if busy {
		status = run_v1alpha.SKIPPED
	}

	maxAttempts := int64(1)
	if task.Retries > 0 {
		maxAttempts = task.Retries + 1
	}

	run := &run_v1alpha.Run{
		App:         app.ID,
		Version:     ver.ID,
		Task:        task.Name,
		Trigger:     run_v1alpha.SCHEDULE,
		Command:     task.Command,
		Status:      status,
		Timeout:     task.Timeout,
		MaxAttempts: maxAttempts,
		Tick:        tick.UTC().Format(time.RFC3339),
	}

	// A skipped tick is born terminal, so it never passes through the
	// controller that would otherwise stamp its timestamps. Retention is
	// measured from EndedAt, and a scheduled run with none is retained
	// unconditionally -- so without this, skipped ticks accumulate forever on
	// exactly the busy task that produces the most of them.
	if status == run_v1alpha.SKIPPED {
		run.StartedAt = tick
		run.EndedAt = tick
	}

	// Create is put-if-absent on a name derived from the tick, which is the
	// entire single-execution guarantee: every replica derives the same name,
	// etcd admits one, and the rest learn the tick is taken.
	id, err := s.EC.Create(ctx, name, run)
	if err != nil {
		if errors.Is(err, cond.ErrConflict{}) {
			s.Log.Debug("tick already claimed by another replica",
				"app", app.ID, "task", task.Name, "tick", run.Tick)
			return nil
		}
		return fmt.Errorf("creating run for tick %s: %w", run.Tick, err)
	}

	if status == run_v1alpha.SKIPPED {
		// Warn, not Info: the task did not run. That is a degraded handled
		// event, and an operator watching a schedule wants it to stand out
		// from the ticks that fired normally.
		s.Log.Warn("skipped scheduled tick, previous run still going",
			"run", id, "app", app.ID, "task", task.Name, "tick", run.Tick)
		return nil
	}

	s.Log.Info("scheduled tick fired",
		"run", id, "app", app.ID, "task", task.Name, "tick", run.Tick)
	return nil
}

// taskIsBusy reports whether a run of this task is still going.
func (s *Scheduler) taskIsBusy(ctx context.Context, appID entity.Id, task string) (bool, error) {
	results, err := s.EAC.List(ctx, entity.Ref(run_v1alpha.RunAppId, appID))
	if err != nil {
		return false, fmt.Errorf("listing runs: %w", err)
	}

	for _, e := range results.Values() {
		var other run_v1alpha.Run
		other.Decode(e.Entity())
		if other.Task == task && !isTerminal(other.Status) {
			return true, nil
		}
	}

	return false, nil
}

// tickRunName derives the run's name from the tick, which is what makes
// creating it a deduplicating operation.
//
// The timestamp is hashed rather than embedded: entity names are a single clean
// segment, and an RFC 3339 string carries colons and a plus. The hash is
// truncated because it only has to separate ticks of one task on one app, not
// resist collision by an adversary.
func tickRunName(appName, task string, tick time.Time) string {
	sum := sha256.Sum256([]byte(tick.UTC().Format(time.RFC3339)))
	digest := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:6]))
	return fmt.Sprintf("%s-%s-%s", appName, task, digest)
}
