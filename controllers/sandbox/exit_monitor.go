package sandbox

import (
	"context"
	"fmt"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
)

// Bounds on the backoff monitorTaskExit uses when containerd fails to report a
// task's exit. Vars rather than consts so tests can shrink them.
var (
	exitWaitRetryBase = 500 * time.Millisecond
	exitWaitRetryMax  = 30 * time.Second
)

// taskIODrainTimeout bounds how long an exited task's output may take to finish
// copying out of containerd. The backlog is at most a few pipe buffers, so this
// is generous; it exists so a stuck copy cannot hold up recording the exit.
// A variable so tests can shorten it.
var taskIODrainTimeout = 30 * time.Second

// waitForTaskIO blocks until containerd has copied all of an exited task's
// output to its writers, or the drain timeout passes.
func (c *SandboxController) waitForTaskIO(sandboxID entity.Id, containerID string, task containerd.Task) {
	tio := task.IO()
	if tio == nil {
		return
	}

	copied := make(chan struct{})
	go func() {
		tio.Wait()
		close(copied)
	}()

	select {
	case <-copied:
	case <-time.After(taskIODrainTimeout):
		c.Log.Warn("container output was still copying after its process exited; attached clients may miss the end of it",
			"sandbox", sandboxID, "container", containerID, "timeout", taskIODrainTimeout)
	}
}

func (c *SandboxController) monitorTaskExit(
	sb *compute.Sandbox,
	containerID string,
	containerName string,
	task containerd.Task,
	exitCh <-chan containerd.ExitStatus,
) {
	c.Log.Debug("monitoring task for exit", "sandbox", sb.ID, "container", containerID)

	var (
		failures int
		waitedAt = time.Now()
	)

	for {
		select {
		case exitStatus := <-exitCh:
			// Check if the exit status contains an error. Per containerd docs:
			// "ExitCode() is only valid if Error() returns nil" and
			// "If an error is returned, the process may still be running."
			// This can happen when re-establishing task monitoring after server restart,
			// where containerd returns an ExitStatus with UnknownExitStatus (255) and
			// zero exit time because the task state is uncertain.
			if err := exitStatus.Error(); err != nil {
				if grpcstatus.Code(err) == codes.NotFound {
					c.Log.Warn("task no longer exists, stopping exit monitoring",
						"sandbox", sb.ID,
						"container", containerID,
					)
					return
				}

				// task.Wait reports RPC failures on the channel rather than
				// returning them, so while containerd is unreachable every
				// retry fails instantly. Back off, and keep retrying: the
				// process may well outlive the outage, and giving up would
				// lose its exit for good. A wait that held for a while means
				// the last outage ended, so a new one starts the count over.
				if time.Since(waitedAt) > exitWaitRetryMax {
					failures = 0
				}
				failures++
				delay := exitWaitRetryMax
				if failures <= 16 {
					delay = min(exitWaitRetryBase<<(failures-1), exitWaitRetryMax)
				}

				if failures == 1 {
					c.Log.Warn("received exit status with error, retrying monitoring with backoff",
						"sandbox", sb.ID,
						"container", containerID,
						"error", err,
					)
				} else {
					c.Log.Debug("exit monitoring still failing",
						"sandbox", sb.ID,
						"container", containerID,
						"attempt", failures,
						"backoff", delay,
						"error", err,
					)
				}

				select {
				case <-time.After(delay):
				case <-c.topCtx.Done():
					return
				}

				waitedAt = time.Now()
				newExitCh, waitErr := task.Wait(c.topCtx)
				if waitErr != nil {
					c.Log.Warn("failed to re-establish task monitoring after error status",
						"sandbox", sb.ID,
						"container", containerID,
						"error", waitErr,
					)
					return
				}
				exitCh = newExitCh
				continue
			}

			c.Log.Info("container process exited",
				"sandbox", sb.ID,
				"container", containerID,
				"exit_code", exitStatus.ExitCode(),
				"exit_time", exitStatus.ExitTime(),
			)
			c.setProcessExited(containerID)

			// The process exiting does not mean its output has arrived. What it
			// wrote last can still be sitting in the stdout pipe and FIFO, behind
			// a log consumer that ingests line by line, when the exit event
			// fires. Everything below leads to the Hub closing -- directly, and
			// through the STOPPED patch waking teardown -- and a closed Hub
			// discards what containerd copies into it afterwards, so an attached
			// `miren app run` lost the end of its output and still exited 0
			// (MIR-2012). Wait for the copy to finish first.
			if c.hubs.Get(sb.ID, containerName) != nil {
				c.waitForTaskIO(sb.ID, containerID, task)
			}

			// We don't delete the task here so that our destroySubContainers function
			// has a consistent view of the state of containers and tasks.

			// Record the exit in the same patch as the STOPPED transition, so
			// anything woken by the stop already has the code. Splitting them
			// would leave a window where a run's sandbox reads stopped with no
			// result to report.
			//
			// ExitTime can be zero even on a clean exit, and an Exit carrying a
			// zero code and a zero time is Empty() -- it would encode to nothing
			// and the code would silently vanish. Fall back to now.
			exitAt := exitStatus.ExitTime()
			if exitAt.IsZero() {
				exitAt = time.Now()
			}

			// Update sandbox status to STOPPED using Patch (only updating the
			// fields set here). STOPPED triggers cleanup in reconciliation
			// (stopSandbox), which:
			// - Releases IPs immediately
			// - Cleans up containers
			// - Marks as DEAD afterward
			//
			// The DEAD patch that follows leaves Exit intact: it is a struct
			// literal with an empty Exit, which the generated encoder skips.
			ctx := context.Background()
			result, err := c.recordExit(c.topCtx, sb.ID, compute.Exit{
				Code:      int64(exitStatus.ExitCode()),
				At:        exitAt,
				Container: containerName,
			})
			if err != nil {
				if !errors.Is(err, cond.ErrNotFound{}) {
					c.Log.Error("failed to update sandbox status to STOPPED",
						"sandbox", sb.ID,
						"error", err,
					)
				}
				return
			}
			if c.writeTracker != nil && result != nil && result.HasRevision() {
				c.writeTracker.RecordWrite(result.Revision())
			}

			// If this sandbox belongs to a task run, report the exit to the run
			// as well. That is not redundancy: the sandbox entity has several
			// writers, so the exit recorded there can lose a write to a
			// concurrent stale patch, while the run has exactly one writer plus
			// this input and nothing to contend with.
			c.reportRunExit(ctx, sb, exitStatus.ExitCode(), exitAt)

			// Release this container's stdio now that its process is gone.
			//
			// StopSandbox does this too, but it runs from reconciliation, and
			// nothing schedules a reconcile at this moment -- so it lands on
			// the controller's next resync, anywhere from zero to a full period
			// away. An attached client blocks on the Hub closing, so waiting
			// for that pass put a uniform 0-60s tail on every `miren app run`:
			// the command's output arrived at once and the terminal then sat
			// there, long after the container had exited (MIR-1769).
			//
			// Closing a Hub also closes the container's stdin, which is why
			// only teardown may normally do it. It is safe here for the same
			// reason it is safe there: the process this stdin belonged to has
			// already exited. The Hub lingers after closing, so a client that
			// arrives late still gets the replayed output.
			c.hubs.Remove(sb.ID, containerName)

			c.Log.Info("marked sandbox as STOPPED due to process exit, cleanup will be triggered by reconciliation", "sandbox", sb.ID)
			return

		case <-c.topCtx.Done():
			c.Log.Debug("task monitoring cancelled", "sandbox", sb.ID, "container", containerID)
			return
		}
	}
}

// recordExit writes a container's exit alongside the STOPPED transition, under
// optimistic concurrency.
//
// The revision guard is the whole point. PatchEntity is read-modify-write, and
// the rest of the controller patches with revision 0, so two writers computed
// from the same snapshot do not merge -- the second simply overwrites the first
// wholesale. The boot path is exactly such a writer: it decides to mark a
// sandbox RUNNING from a snapshot taken before the task started, and for a
// command that exits in milliseconds that write can land after the exit. The
// result is not a clobbered field but a lost update, taking the exit code with
// it and restoring RUNNING on a sandbox whose process is already gone.
//
// Retrying against the current revision makes the exit the last write instead.
// The cap is high rather than small: losing a task's exit code is not something
// to give up on after a couple of attempts, and contention here is bounded by
// the handful of writers a single sandbox has.
func (c *SandboxController) recordExit(
	ctx context.Context,
	id entity.Id,
	exit compute.Exit,
) (*entityserver_v1alpha.EntityAccessClientPatchResults, error) {
	const maxAttempts = 100

	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			// Back off rather than hammering the store. Without this a
			// contended sandbox issues up to 200 immediate round trips, and a
			// shutdown could not interrupt them.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(min(time.Duration(attempt)*10*time.Millisecond, 500*time.Millisecond)):
			}
		}

		resp, err := c.EAC.Get(ctx, id.String())
		if err != nil {
			return nil, err
		}

		var current compute.Sandbox
		current.Decode(resp.Entity().Entity())
		if current.Status == compute.DEAD {
			// Do not advance UpdatedAt on an already-counted failure.
			return nil, nil
		}
		stopped := &compute.Sandbox{Status: compute.STOPPED, Exit: exit}
		if current.StopRequestedAt.IsZero() && current.Status == compute.STOPPED && current.Exit.Empty() && current.StartupOutcome != compute.STARTUP_FAILED {
			stopped.StopRequestedAt = time.Now()
		}
		if current.StartupOutcome == "" {
			switch current.Status {
			case compute.RUNNING:
				stopped.StartupOutcome = compute.STARTUP_RUNNING
			case compute.PENDING:
				stopped.StartupOutcome = compute.STARTUP_FAILED
			case compute.NOT_READY, compute.STOPPED, compute.DEAD:
				// No lifecycle conclusion from these states alone.
			}
		}
		patchAttrs := entity.New(
			entity.Ref(entity.DBId, id),
			stopped.Encode,
		)

		result, err := c.EAC.Patch(ctx, patchAttrs.Attrs(), resp.Entity().Revision())
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return nil, err
		}

		lastErr = err
		c.Log.Debug("retrying exit record after a concurrent sandbox write",
			"sandbox", id, "attempt", attempt+1)
	}

	return nil, fmt.Errorf("recording exit for %s: %w", id, lastErr)
}

// reportRunExit records a container's exit against the task run that owns it.
//
// RFD-97 models the reported exit as an *input* to the run controller rather
// than a state the reporter owns, with a source saying where it came from. This
// is that input for the ordinary case where the command is the container's own
// process; the exec path supplies the same field when a client is attached.
//
// Reporting here rather than leaving the run to read it off the sandbox is what
// makes the exit code reliable. The sandbox entity is written by the boot path,
// the exit monitor, and teardown, all with revision 0 -- so a read-modify-write
// computed from a pre-exit snapshot can land afterwards and take the exit with
// it. The run entity has one writer and this input, so there is no race to lose.
func (c *SandboxController) reportRunExit(
	ctx context.Context,
	sb *compute.Sandbox,
	code uint32,
	at time.Time,
) {
	var runID entity.Id
	for _, l := range sb.Spec.LogAttribute {
		if l.Key == "miren.run" {
			runID = entity.Id(l.Value)
			break
		}
	}
	if runID == "" {
		return
	}

	_, err := c.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, runID),
		(&run_v1alpha.Run{
			Result: run_v1alpha.Result{
				Code:   int64(code),
				At:     at,
				Source: run_v1alpha.TASK,
			},
		}).Encode,
	).Attrs(), 0)
	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			c.Log.Warn("failed to report exit to run", "run", runID, "sandbox", sb.ID, "error", err)
		}
		return
	}

	c.Log.Debug("reported exit to run", "run", runID, "sandbox", sb.ID, "exit_code", code)
}
