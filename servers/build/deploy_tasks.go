package build

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

const (
	// deployTaskPoll is how often the gate re-reads its runs. The run controller
	// does the work; this only decides how promptly the deploy notices.
	deployTaskPoll = 2 * time.Second

	// deployTaskCeiling bounds the gate as a whole, independently of any task's
	// own timeout. A task with no timeout would otherwise be able to hold a
	// deploy open forever.
	deployTaskCeiling = 2 * time.Hour
)

// runDeployTasks creates a Run for every task triggered by deploy and waits for
// all of them.
//
// It sits between addons being ready and ActiveVersion flipping, which is the
// only hook point in the deploy and deliberately so. Gating here means the runs
// complete or fail while the previous version is still the only thing serving:
// there is no partial rollout to unwind, no traffic to shift back, and no
// compensation step. A failed deploy task is a failed deploy, not a
// half-promoted one, and that holds for every rollout strategy because the gate
// sits ahead of all of them rather than inside any.
//
// The cost is that a strategy cannot gate per stage -- "run the backfill after
// the canary validates" is not expressible. That is worth having eventually,
// but the right shape for it is a step in the strategy's sequence that
// references a task by name, not another field on the task: the task says what
// to run, the strategy says when. Keeping that split means adding per-stage
// hooks later doesn't invalidate this.
//
// What it deliberately does not do is undo anything. If a task succeeded and a
// later step failed, its effects stay; the platform does not run
// down-migrations, and reversibility is the app's problem.
func (b *Builder) runDeployTasks(
	ctx context.Context,
	appName string,
	appID entity.Id,
	versionID entity.Id,
	cfgSpec *core_v1alpha.ConfigSpec,
	report func(format string, args ...any),
) error {
	tasks := deployTriggeredTasks(cfgSpec)
	if len(tasks) == 0 {
		return nil
	}

	runIDs := make(map[entity.Id]string, len(tasks))
	for _, task := range tasks {
		id, err := b.createDeployRun(ctx, appName, appID, versionID, task)
		if err != nil {
			return fmt.Errorf("creating run for task %q: %w", task.Name, err)
		}
		runIDs[id] = task.Name

		report("Running deploy task %s", task.Name)
	}

	return b.awaitDeployRuns(ctx, runIDs, report)
}

// deployTriggeredTasks returns the tasks a deploy must run, in a stable order.
//
// Ordering between them is not expressed: RFD-79 put task dependencies out of
// scope, and RFD-88's readiness graph is the right home if we ever want them.
// They run concurrently; sorting only keeps the reported order stable.
func deployTriggeredTasks(cfgSpec *core_v1alpha.ConfigSpec) []core_v1alpha.ConfigSpecTasks {
	if cfgSpec == nil {
		return nil
	}

	var tasks []core_v1alpha.ConfigSpecTasks
	for _, t := range cfgSpec.Tasks {
		if t.Trigger == string(deployTrigger) {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// deployTrigger is the app.toml value, not the entity enum: config stores the
// bare word while the entity stores a namespaced id.
const deployTrigger = "deploy"

func (b *Builder) createDeployRun(
	ctx context.Context,
	appName string,
	appID entity.Id,
	versionID entity.Id,
	task core_v1alpha.ConfigSpecTasks,
) (entity.Id, error) {
	maxAttempts := int64(1)
	if task.Retries > 0 {
		maxAttempts = task.Retries + 1
	}

	name := fmt.Sprintf("%s-%s-%s", appName, task.Name, idgen.Gen(""))
	return b.ec.Create(ctx, name, &run_v1alpha.Run{
		App:         appID,
		Version:     versionID,
		Task:        task.Name,
		Trigger:     run_v1alpha.DEPLOY,
		Command:     task.Command,
		Status:      run_v1alpha.PENDING,
		Timeout:     task.Timeout,
		MaxAttempts: maxAttempts,
	})
}

// awaitDeployRuns blocks until every run finishes, and fails the deploy if any
// of them did.
func (b *Builder) awaitDeployRuns(
	ctx context.Context,
	runIDs map[entity.Id]string,
	report func(format string, args ...any),
) error {
	deadline := time.Now().Add(deployTaskCeiling)
	pending := make(map[entity.Id]string, len(runIDs))
	for id, task := range runIDs {
		pending[id] = task
	}

	for len(pending) > 0 {
		if time.Now().After(deadline) {
			var names []string
			for _, task := range pending {
				names = append(names, task)
			}
			return fmt.Errorf("deploy tasks did not finish within %s: %s",
				deployTaskCeiling, strings.Join(names, ", "))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(deployTaskPoll):
		}

		for id, task := range pending {
			var run run_v1alpha.Run
			if err := b.ec.GetById(ctx, id, &run); err != nil {
				// A run that isn't visible yet is a race with its own creation,
				// and the next poll picks it up. Anything else -- a permission
				// or transport failure -- will not fix itself, and waiting out
				// the ceiling would turn it into a two-hour hang with a
				// misleading message.
				if errors.Is(err, cond.ErrNotFound{}) {
					continue
				}
				return fmt.Errorf("reading deploy task run %s: %w", task, err)
			}

			switch run.Status {
			case run_v1alpha.SUCCEEDED:
				delete(pending, id)
				report("Deploy task %s succeeded", task)

			case run_v1alpha.FAILED, run_v1alpha.TIMED_OUT, run_v1alpha.CANCELED:
				// Surface where to look. A failing migration is a deploy
				// failure and the user is already reading this output, so the
				// message has to carry the run id rather than making them go
				// find it.
				return fmt.Errorf("deploy task %s %s (exit code %s); see: miren logs run %s",
					task, terminalWord(run.Status), exitCodeWord(&run), run.ID)

			case run_v1alpha.PENDING, run_v1alpha.RUNNING:
				// Still going.

			case run_v1alpha.SKIPPED:
				// Only a scheduled tick is ever skipped, so a deploy-triggered
				// run reaching here means something is wrong. Fail the deploy:
				// the gate exists to prove every task ran, and a task that was
				// skipped did not.
				return fmt.Errorf("deploy task %s was skipped without running; see: miren logs run %s",
					task, run.ID)

			default:
			}
		}
	}

	return nil
}

func terminalWord(s run_v1alpha.RunStatus) string {
	switch s {
	case run_v1alpha.TIMED_OUT:
		return "timed out"
	case run_v1alpha.CANCELED:
		return "was canceled"
	case run_v1alpha.PENDING, run_v1alpha.RUNNING, run_v1alpha.SUCCEEDED,
		run_v1alpha.FAILED, run_v1alpha.SKIPPED:
		return "failed"
	default:
		return "failed"
	}
}

// exitCodeWord renders the exit code, or says none was observed rather than
// printing a 0 that would read as a clean exit.
func exitCodeWord(run *run_v1alpha.Run) string {
	if run.Result.At.IsZero() {
		return "none reported"
	}
	return fmt.Sprintf("%d", run.Result.Code)
}
