// Package run reconciles Run entities: one sandbox, one command, one exit
// code, then teardown.
//
// The controller is the only writer of Run.Status. Everything else -- the exit
// code the sandbox reports, a cancellation request from the CLI, a deadline
// passing -- arrives as an input it reads, never as a status another component
// writes. Splitting that ownership would be a race with no lock available to
// fix it, since the entity store has no cross-entity transaction.
package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	runapi "miren.dev/runtime/api/run"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/appspec"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

const (
	// DefaultTimeout bounds a run whose task didn't set one. Unbounded is the
	// wrong default: a forgotten run holds a concurrency slot indefinitely.
	DefaultTimeout = 8 * time.Hour

	// StartDeadline bounds how long a run may sit pending before its sandbox is
	// running. It covers image pulls and an unschedulable cluster, and replaces
	// the client-side two-minute wait the exec proxy used to impose -- which
	// only applied while a client was connected.
	StartDeadline = 5 * time.Minute

	// SweepInterval is how often deadlines are checked. The sweep does two
	// indexed lookups and enqueues; it transitions nothing.
	SweepInterval = 10 * time.Second
)

// Controller reconciles Run entities.
type Controller struct {
	Log *slog.Logger
	EC  *entityserver.Client
	EAC *entityserver_v1alpha.EntityAccessClient

	// RC is this controller's own reconcile controller, used by the deadline
	// sweep and the sandbox bridge to enqueue work. Set after construction
	// because the reconcile controller needs the handler that wraps this.
	RC *controller.ReconcileController
}

func NewController(log *slog.Logger, ec *entityserver.Client, eac *entityserver_v1alpha.EntityAccessClient) *Controller {
	return &Controller{
		Log: log.With("module", "run"),
		EC:  ec,
		EAC: eac,
	}
}

func (c *Controller) Init(ctx context.Context) error { return nil }

// Reconcile drives a run through its lifecycle. Every step is idempotent and
// re-entrant: the framework logs and drops handler errors without requeueing,
// so recovery comes from the next watch event or the deadline sweep rather than
// from retry bookkeeping here.
func (c *Controller) Reconcile(ctx context.Context, r *run_v1alpha.Run, meta *entity.Meta) error {
	if isTerminal(r.Status) {
		return c.ensureTornDown(ctx, r)
	}

	// A cancellation request outranks everything else, including a run that has
	// not started: someone asking for it to stop should not first have to wait
	// for it to be admitted.
	if !r.CancelRequestedAt.IsZero() {
		return c.finish(ctx, r, run_v1alpha.CANCELED, nil)
	}

	switch r.Status {
	case "", run_v1alpha.PENDING:
		return c.start(ctx, r)
	case run_v1alpha.RUNNING:
		return c.observe(ctx, r)
	case run_v1alpha.SUCCEEDED, run_v1alpha.FAILED, run_v1alpha.TIMED_OUT,
		run_v1alpha.CANCELED, run_v1alpha.SKIPPED:
		// Unreachable: isTerminal above returns early for these.
		return nil
	default:
		return nil
	}
}

// start admits a pending run and creates its sandbox.
func (c *Controller) start(ctx context.Context, r *run_v1alpha.Run) error {
	// Stamp the queue time on first sight. The start deadline measures from
	// here: a run held back by admission never reaches RUNNING, so measuring
	// from StartedAt would leave it queued with no bound -- behind a
	// max_concurrent = 1 holder that runs to DefaultTimeout, for hours.
	if r.QueuedAt.IsZero() {
		r.QueuedAt = time.Now()
		if err := c.patchRun(ctx, r.ID, &run_v1alpha.Run{QueuedAt: r.QueuedAt}); err != nil {
			return fmt.Errorf("stamping queue time: %w", err)
		}
	}

	if deadline := c.startDeadline(r); !deadline.IsZero() && time.Now().After(deadline) {
		c.Log.Info("run timed out before it started", "run", r.ID, "task", r.Task)
		return c.finish(ctx, r, run_v1alpha.TIMED_OUT, nil)
	}

	admitted, err := c.admit(ctx, r)
	if err != nil {
		return fmt.Errorf("admitting run: %w", err)
	}
	if !admitted {
		// Stay pending. The sweep re-enqueues, so the run is queued rather than
		// rejected -- which is what lets deploy- and schedule-triggered runs use
		// the same gate as a manual invoke without any of them having to poll.
		c.Log.Debug("run not admitted yet, staying pending", "run", r.ID, "task", r.Task)
		return nil
	}

	sandboxID, err := c.ensureSandbox(ctx, r)
	if err != nil {
		// Stay pending rather than failing outright. A read failure, a config
		// resolution failure, or a transient create failure is not the task's
		// fault, and failing here would spend a run's whole retry budget on a
		// blip in the store -- with no exit code to explain it. The sweep
		// retries, and the start deadline bounds how long that can go on.
		c.Log.Warn("failed to create sandbox for run, staying pending",
			"run", r.ID, "task", r.Task, "error", err)
		return nil
	}

	attempt := r.Attempt
	if attempt < 1 {
		attempt = 1
	}

	return c.patchRun(ctx, r.ID, &run_v1alpha.Run{
		Sandbox:   sandboxID,
		Attempt:   attempt,
		StartedAt: time.Now(),
		Status:    run_v1alpha.RUNNING,
	})
}

// observe checks whether a running run's sandbox has finished.
func (c *Controller) observe(ctx context.Context, r *run_v1alpha.Run) error {
	if deadline := c.runDeadline(r); !deadline.IsZero() && time.Now().After(deadline) {
		c.Log.Info("run exceeded its timeout", "run", r.ID, "task", r.Task, "timeout", r.Timeout)
		return c.finish(ctx, r, run_v1alpha.TIMED_OUT, nil)
	}

	// A result already reported -- by the exec path, say -- is authoritative and
	// saves reading the sandbox at all.
	if !r.Result.At.IsZero() {
		return c.finishWithCode(ctx, r, r.Result.Code)
	}

	if r.Sandbox == "" {
		// Admitted but the sandbox reference never landed. Treat it as not
		// started and let start() rebuild it; the deterministic sandbox name
		// makes that safe to repeat.
		return c.patchRun(ctx, r.ID, &run_v1alpha.Run{Status: run_v1alpha.PENDING})
	}

	sb, err := c.getSandbox(ctx, r.Sandbox)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// The sandbox was deleted out from under a running run. Nothing will
			// ever report an exit, so resolve it now rather than waiting for the
			// timeout to notice.
			c.Log.Warn("sandbox for running run has disappeared", "run", r.ID, "sandbox", r.Sandbox)
			return c.finish(ctx, r, run_v1alpha.FAILED, nil)
		}
		return fmt.Errorf("reading run sandbox: %w", err)
	}

	// A reported exit is authoritative, whatever the status says. Status is a
	// lifecycle flag several components write -- the boot path marks a sandbox
	// RUNNING once it finishes its own bookkeeping, which for a command that
	// exits in milliseconds can land after the exit was already recorded. The
	// exit component has exactly one writer and is only ever set once.
	if !sb.Exit.Empty() {
		return c.finishWithCode(ctx, r, sb.Exit.Code)
	}

	switch sb.Status {
	case compute.PENDING, compute.NOT_READY, compute.RUNNING:
		// Still going; nothing to decide until it stops.
		return nil
	case compute.STOPPED, compute.DEAD:
		// Stopped with no exit recorded: the runner went away mid-run, or the
		// sandbox was retired by policy. A failed attempt, but not an exit code.
		c.Log.Info("run sandbox ended without reporting an exit", "run", r.ID, "sandbox", sb.ID)
		return c.finish(ctx, r, run_v1alpha.FAILED, nil)
	default:
		return nil
	}
}

// finishWithCode resolves a run from an observed exit code, retrying if the
// task has attempts left.
func (c *Controller) finishWithCode(ctx context.Context, r *run_v1alpha.Run, code int64) error {
	if code == 0 {
		return c.finish(ctx, r, run_v1alpha.SUCCEEDED, &code)
	}

	if c.canRetry(r) {
		return c.retry(ctx, r, code)
	}
	return c.finish(ctx, r, run_v1alpha.FAILED, &code)
}

func (c *Controller) canRetry(r *run_v1alpha.Run) bool {
	// Retries exist for triggers nobody is watching. A manual run that fails
	// just fails, and the caller decides what to do about it.
	if r.Trigger == run_v1alpha.MANUAL {
		return false
	}
	attempt := r.Attempt
	if attempt < 1 {
		attempt = 1
	}
	return attempt < r.MaxAttempts
}

// retry records the failed attempt and returns the same entity to pending.
//
// Reusing the entity is not an optimization. A scheduled run's id is derived
// from its tick, and that derivation is the single-execution guarantee; a
// second entity for attempt two would carry a name that isn't derived from the
// tick and would forfeit it.
func (c *Controller) retry(ctx context.Context, r *run_v1alpha.Run, code int64) error {
	c.Log.Info("retrying failed run",
		"run", r.ID, "task", r.Task, "attempt", r.Attempt, "max_attempts", r.MaxAttempts, "exit_code", code)

	if err := c.tearDownSandbox(ctx, r); err != nil {
		c.Log.Warn("failed to tear down sandbox before retry", "run", r.ID, "error", err)
	}

	attempt := r.Attempt
	if attempt < 1 {
		attempt = 1
	}

	// Sandbox is left pointing at the failed attempt rather than cleared: a
	// cardinality-one ref cannot be unset by a patch, and start() overwrites it
	// with the next attempt's deterministic name before anything reads it.
	return c.patchRun(ctx, r.ID, &run_v1alpha.Run{
		AttemptRecord: []run_v1alpha.AttemptRecord{{
			Attempt:   attempt,
			Sandbox:   r.Sandbox,
			ExitCode:  code,
			Status:    string(run_v1alpha.FAILED),
			StartedAt: r.StartedAt,
			EndedAt:   time.Now(),
		}},
		Attempt: attempt + 1,
		// A retry re-enters the queue, so it gets a fresh start window rather
		// than inheriting the previous attempt's expired one.
		QueuedAt: time.Now(),
		Status:   run_v1alpha.PENDING,
	})
}

// finish moves a run terminal and tears its sandbox down.
//
// code is nil when no exit was observed -- a timeout, a cancellation, a runner
// that vanished. Nothing is recorded in that case: a fabricated code would be
// indistinguishable from one the process actually returned, and the status
// already carries the meaning.
func (c *Controller) finish(ctx context.Context, r *run_v1alpha.Run, status run_v1alpha.RunStatus, code *int64) error {
	if err := c.tearDownSandbox(ctx, r); err != nil {
		c.Log.Warn("failed to tear down sandbox", "run", r.ID, "error", err)
	}

	attempt := r.Attempt
	if attempt < 1 {
		attempt = 1
	}

	rec := &run_v1alpha.AttemptRecord{
		Attempt:   attempt,
		Sandbox:   r.Sandbox,
		Status:    string(status),
		StartedAt: r.StartedAt,
		EndedAt:   time.Now(),
	}

	update := &run_v1alpha.Run{
		Status:  status,
		EndedAt: time.Now(),
	}

	if code != nil {
		rec.ExitCode = *code
		update.Result = run_v1alpha.Result{Code: *code, At: time.Now()}
	}

	update.AttemptRecord = []run_v1alpha.AttemptRecord{*rec}

	if err := c.patchRun(ctx, r.ID, update); err != nil {
		return err
	}

	// Release after the status is durable. Releasing first would let another run
	// be admitted while this one still reads as running, which is exactly the
	// double-execution the slot exists to prevent.
	c.release(ctx, r)

	c.Log.Info("run finished",
		"run", r.ID, "app", r.App, "task", r.Task, "trigger", r.Trigger, "status", status,
		"has_exit_code", code != nil)
	return nil
}

// ensureTornDown is the terminal-state reconcile: it exists so a run whose
// status landed but whose sandbox teardown didn't is repaired on the next
// event, rather than leaking a sandbox until the GC notices.
func (c *Controller) ensureTornDown(ctx context.Context, r *run_v1alpha.Run) error {
	if r.Sandbox == "" {
		return nil
	}

	sb, err := c.getSandbox(ctx, r.Sandbox)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// Already gone, which is the outcome this wanted.
			return nil
		}
		// Anything else means we could not tell whether a sandbox is still
		// live. Return so the framework retries rather than leaving one running.
		return fmt.Errorf("reading sandbox for terminal run: %w", err)
	}
	if sb.Status == compute.DEAD || sb.Status == compute.STOPPED {
		return nil
	}

	c.Log.Info("tearing down sandbox for terminal run", "run", r.ID, "sandbox", r.Sandbox)
	return c.tearDownSandbox(ctx, r)
}

// tearDownSandbox stops a run's sandbox.
//
// It patches STOPPED rather than deleting the entity, so the sandbox controller
// honors the container's shutdown timeout on the same path a normal exit takes
// -- graceful, then killed. Deleting would skip that, and would also destroy
// the record before anyone could look at it.
func (c *Controller) tearDownSandbox(ctx context.Context, r *run_v1alpha.Run) error {
	if r.Sandbox == "" {
		return nil
	}

	sb, err := c.getSandbox(ctx, r.Sandbox)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil
		}
		return err
	}
	if sb.Status == compute.STOPPED || sb.Status == compute.DEAD {
		return nil
	}

	_, err = c.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, r.Sandbox),
		(&compute.Sandbox{Status: compute.STOPPED}).Encode,
	).Attrs(), 0)
	if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	return nil
}

// ensureSandbox creates the sandbox for the current attempt.
//
// The name is derived from the run and the attempt, so Create is idempotent:
// the store returns success for a create whose attributes are byte-identical,
// which means a lost reply cannot produce two sandboxes running the command.
func (c *Controller) ensureSandbox(ctx context.Context, r *run_v1alpha.Run) (entity.Id, error) {
	attempt := r.Attempt
	if attempt < 1 {
		attempt = 1
	}

	sandboxID := runapi.SandboxName(r.ID, attempt)
	name := strings.TrimPrefix(sandboxID.String(), "sandbox/")

	if existing, err := c.getSandbox(ctx, sandboxID); err == nil {
		return existing.ID, nil
	} else if !errors.Is(err, cond.ErrNotFound{}) {
		return "", err
	}

	spec, appName, err := c.buildSpec(ctx, r)
	if err != nil {
		return "", err
	}

	sb := compute.Sandbox{Status: compute.PENDING, Spec: *spec}

	_, err = c.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name: name,
			Labels: types.LabelSet(
				"app", appName,
				"run", r.ID.String(),
				"task", r.Task,
			),
		}).Encode,
		entity.DBId, sandboxID,
		sb.Encode,
	).Attrs())
	if err != nil {
		return "", fmt.Errorf("creating sandbox: %w", err)
	}

	c.Log.Info("created sandbox for run",
		"run", r.ID, "sandbox", sandboxID, "task", r.Task, "attempt", attempt)
	return sandboxID, nil
}

// buildSpec resolves the app's deployed image and config into a sandbox spec.
func (c *Controller) buildSpec(ctx context.Context, r *run_v1alpha.Run) (*compute.SandboxSpec, string, error) {
	appResp, err := c.EAC.Get(ctx, r.App.String())
	if err != nil {
		return nil, "", fmt.Errorf("reading app: %w", err)
	}
	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	verResp, err := c.EAC.Get(ctx, r.Version.String())
	if err != nil {
		return nil, "", fmt.Errorf("reading app version: %w", err)
	}
	var ver core_v1alpha.AppVersion
	ver.Decode(verResp.Entity().Entity())

	cfgSpec, err := coreutil.ResolveRuntimeConfig(ctx, c.EAC, &ver)
	if err != nil {
		return nil, "", fmt.Errorf("resolving config: %w", err)
	}

	spec, err := appspec.Build(c.Log, appspec.Options{
		AppID:   r.App,
		AppName: appMD.Name,
		Version: &ver,
		Config:  cfgSpec,
		Image:   ver.ImageUrl,
		Command: r.Command,

		// Carries [tasks.<name>.env] into the container. A run has no service,
		// so this is the only path that env has.
		Task: r.Task,

		// A run declares no ports and touches no disks. Both are load-bearing:
		// a declared port would have the sandbox controller kill the run for
		// failing to bind one, and a miren disk is single-writer and would
		// contend with the service already holding its lease.
		SkipPorts: true,
		SkipDisks: true,

		// Attachable whether or not anyone attaches now. containerd fixes the
		// stdin FIFO at task creation, so a run started detached could never be
		// attached later otherwise -- and "start detached, attach later" is the
		// point of separating the two.
		Stdin: true,

		// A pty only when a person asked for one. Stdin without it gives a
		// shell no job control, no line editing, and no prompt; a pty on a
		// deploy task would instead merge its stderr into stdout and rewrite
		// newlines in the log a machine reads.
		Tty: r.Tty,

		// One command, at most once: a sandbox whose containers vanish must be
		// retired rather than rebooted.
		RestartPolicy: compute.SandboxSpecNEVER,

		LogAttrs: types.LabelSet(
			"miren.stage", "run",
			"miren.run", r.ID.String(),
			"miren.task", r.Task,
			"miren.trigger", string(r.Trigger),
		),
	})
	if err != nil {
		return nil, "", err
	}

	return spec, appMD.Name, nil
}

// SweepDeadlines re-enqueues runs whose start deadline or timeout has passed.
//
// It transitions nothing. Every status change stays inside Reconcile, which the
// framework serializes per entity -- a sweep that wrote statuses directly could
// race a watch-driven reconcile of the same run with no lock to prevent it.
func (c *Controller) SweepDeadlines(ctx context.Context) error {
	if c.RC == nil {
		return nil
	}

	for _, status := range []entity.Id{
		run_v1alpha.RunStatusPendingId,
		run_v1alpha.RunStatusRunningId,
	} {
		ids, err := c.EAC.List(ctx, entity.Ref(run_v1alpha.RunStatusId, status))
		if err != nil {
			c.Log.Warn("failed to list runs for deadline sweep", "status", status, "error", err)
			continue
		}

		for _, e := range ids.Values() {
			var r run_v1alpha.Run
			r.Decode(e.Entity())

			// Enqueue anything past a deadline, plus anything still pending:
			// a run held back by admission has no other event coming, so the
			// sweep is what retries it.
			if !c.pastDeadline(&r) && r.Status != run_v1alpha.PENDING {
				continue
			}

			c.RC.Enqueue(controller.Event{
				Type:   controller.EventUpdated,
				Id:     entity.Id(e.Id()),
				Entity: e.Entity(),
			})
		}
	}

	return nil
}

func (c *Controller) pastDeadline(r *run_v1alpha.Run) bool {
	now := time.Now()
	if d := c.startDeadline(r); !d.IsZero() && now.After(d) {
		return true
	}
	if d := c.runDeadline(r); !d.IsZero() && now.After(d) {
		return true
	}
	return false
}

// startDeadline bounds how long a run may stay pending before its sandbox is
// running, whether it is waiting on admission or on a sandbox that will not
// come up.
func (c *Controller) startDeadline(r *run_v1alpha.Run) time.Time {
	if r.Status != run_v1alpha.PENDING && r.Status != "" {
		return time.Time{}
	}
	// Measured from when the run was queued, not from StartedAt: StartedAt is
	// only written once a run is running, so a run that never gets admitted has
	// none and would never time out.
	if r.QueuedAt.IsZero() {
		return time.Time{}
	}
	return r.QueuedAt.Add(StartDeadline)
}

func (c *Controller) runDeadline(r *run_v1alpha.Run) time.Time {
	if r.Status != run_v1alpha.RUNNING || r.StartedAt.IsZero() {
		return time.Time{}
	}

	timeout := DefaultTimeout
	if r.Timeout != "" {
		d, err := time.ParseDuration(r.Timeout)
		if err != nil {
			c.Log.Warn("run has an unparseable timeout, using the default",
				"run", r.ID, "timeout", r.Timeout, "error", err)
		} else if d == 0 {
			// An explicit zero means unbounded, which is how a task opts out.
			return time.Time{}
		} else {
			timeout = d
		}
	}

	return r.StartedAt.Add(timeout)
}

func (c *Controller) getSandbox(ctx context.Context, id entity.Id) (*compute.Sandbox, error) {
	resp, err := c.EAC.Get(ctx, id.String())
	if err != nil {
		return nil, err
	}
	var sb compute.Sandbox
	sb.Decode(resp.Entity().Entity())
	return &sb, nil
}

// patchRun applies a partial update. Zero-valued fields on the struct are
// skipped by the generated encoder, so this sets exactly what the caller filled
// in. Revision 0 means no OCC, matching how the reconcile framework writes back.
func (c *Controller) patchRun(ctx context.Context, id entity.Id, update *run_v1alpha.Run) error {
	_, err := c.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, id),
		update.Encode,
	).Attrs(), 0)
	if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	return nil
}

func isTerminal(s run_v1alpha.RunStatus) bool {
	switch s {
	case run_v1alpha.SUCCEEDED, run_v1alpha.FAILED, run_v1alpha.TIMED_OUT,
		run_v1alpha.CANCELED, run_v1alpha.SKIPPED:
		return true
	case run_v1alpha.PENDING, run_v1alpha.RUNNING:
		return false
	}
	return false
}
