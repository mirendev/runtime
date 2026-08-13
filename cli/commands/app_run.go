package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/containerd/console"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/rpc/stream"
)

// runAttachPoll is how often the CLI checks whether a run's sandbox is up yet.
// The controller does the waiting that matters -- it has its own start deadline
// -- so this only decides how quickly a terminal appears.
const runAttachPoll = 250 * time.Millisecond

// AppRun creates a run and, unless detached, attaches to it.
//
// The command surface is nearly unchanged: no arguments still opens a shell,
// arguments still run that command, and the exit code still propagates. What
// changed underneath is that the invocation now leaves a durable record, which
// is what makes exit codes retrievable afterwards, --detach possible, and "who
// shelled into production" answerable.
//
// Once attached, Ctrl-P Ctrl-Q leaves the run running. Ctrl-C is not an exit:
// it reaches the command as an interrupt, which is what you want when the
// command is the workload rather than a child of your terminal.
func AppRun(ctx *Context, opts struct {
	AppCentric

	Task   string `long:"task" description:"Task to run; defaults to the console task"`
	Detach bool   `long:"detach" description:"Create the run without attaching a terminal"`

	Args []string `rest:"true"`
}) error {
	runs, err := runsClient(ctx)
	if err != nil {
		return err
	}

	// A pty is decided here because only the client knows whether a person is
	// watching, and containerd fixes it when the container's task is created --
	// so it cannot be settled later, on attach. A detached run never has a
	// terminal on the other end, and a piped one should keep stdout and stderr
	// separate for whatever is reading them.
	wantTTY := !opts.Detach && stdinIsTerminal()

	created, err := runs.CreateRun(ctx, opts.App, opts.Task, opts.Args, wantTTY)
	if err != nil {
		return err
	}

	runID := created.Id()

	if opts.Detach {
		// Just the id, so the common case -- a harness capturing it -- needs no
		// parsing.
		ctx.Printf("%s\n", runID)
		return nil
	}

	detached, err := attachToRun(ctx, runs, runID, created.SandboxName(), true)
	if err != nil {
		return err
	}
	if detached {
		return reportDetached(ctx, runID)
	}

	return reportRunExit(ctx, runs, runID)
}

// stdinIsTerminal reports whether a person is typing at this command.
func stdinIsTerminal() bool {
	_, err := console.ConsoleFromFile(os.Stdin)
	return err == nil
}

// reportDetached tells the user their run is still going and how to get back.
// Leaving silently would read as the run having ended.
func reportDetached(ctx *Context, runID string) error {
	ctx.Printf("\ndetached from %s; it is still running\n", runID)
	ctx.Printf("  reattach: miren app attach %s\n", runID)
	ctx.Printf("  stop it:  miren app runs cancel %s\n", runID)
	return nil
}

// AppAttach joins a run that is already going.
//
// Attaching and detaching are invisible to the run: it was started by the
// controller and outlives any client, so this can be called on a run that was
// started detached, or called again after a connection drops.
func AppAttach(ctx *Context, opts struct {
	AppCentric

	Run string `position:"0" usage:"Run to attach to" required:"true"`
}) error {
	if opts.Run == "" {
		return fmt.Errorf("a run id is required")
	}

	runs, err := runsClient(ctx)
	if err != nil {
		return err
	}

	got, err := runs.GetRun(ctx, opts.Run)
	if err != nil {
		return err
	}

	info := got.Run()
	if info == nil {
		return fmt.Errorf("run %s not found", opts.Run)
	}
	if info.Sandbox() == "" {
		return fmt.Errorf("run %s has not started yet", opts.Run)
	}

	detached, err := attachToRun(ctx, runs, info.Id(), info.Sandbox(), false)
	if err != nil {
		return err
	}
	if detached {
		return reportDetached(ctx, info.Id())
	}

	return reportRunExit(ctx, runs, info.Id())
}

// attachToRun streams a run's terminal until the run ends or the user detaches.
//
// Reports whether the user detached, which the caller needs in order to decide
// what to say about the exit code: a detached run has not produced one yet,
// while an attach that ended on its own means the container is gone and a code
// is on its way.
//
// cancelIfNeverStarts belongs to the caller because the two callers mean
// different things by giving up. `miren app run` created the run, so a run that
// never started is a command that never executed and leaving it pending would
// let it execute later, after this process told the caller it did not start.
// `miren app attach` only ever asked to watch: cancelling there would kill
// someone else's run for the crime of pulling a large image slowly.
func attachToRun(ctx *Context, runs *app_v1alpha.RunsClient, runID, sandboxName string, cancelIfNeverStarts bool) (bool, error) {
	opt := new(exec_v1alpha.ShellOptions)
	in, out, winUpdates, cleanup := setupExecIO(ctx, opt)
	defer cleanup()

	cl, err := ctx.RPCClient("dev.miren.runtime/exec")
	if err != nil {
		return false, err
	}

	sec := exec_v1alpha.NewSandboxExecClient(cl)

	// The detach sequence has to cancel the call rather than just close the
	// input: the server keeps serving output until the container ends, which is
	// the whole point, so an input that merely stops would leave the client
	// attached with no way to type.
	attachCtx, cancelAttach := context.WithCancel(ctx.Context)
	defer cancelAttach()

	detach := newDetachReader(in, cancelAttach)

	// The sandbox exists only once the controller has admitted the run, so retry
	// while it is simply absent. Anything else -- a denial, a transport failure,
	// a container that was never made attachable -- is reported immediately:
	// waiting out two minutes and then printing one generic message would lose
	// the distinction between "not ready yet" and "not allowed".
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, err = sec.Attach(
			attachCtx,
			sandboxName, "",
			stream.ServeReader(attachCtx, detach),
			stream.ServeWriter(attachCtx, out),
			stream.ChanReader(winUpdates),
		)
		if detach.Detached() {
			return true, nil
		}
		if err == nil {
			return false, nil
		}
		if !attachTargetMissing(err) {
			return false, fmt.Errorf("attaching to run %s: %w", runID, err)
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}

		// A short task can finish before the terminal ever attaches, taking its
		// container -- and the thing to attach to -- with it. That is not a
		// failure: the run did what it was asked, and its exit code is
		// recorded. Stop waiting and let the caller report it.
		if runIsOver(ctx, runs, runID) {
			return false, nil
		}

		if time.Now().After(deadline) {
			if !cancelIfNeverStarts {
				return false, fmt.Errorf("run %s has not started within 2m; it is still going and can be attached again: %w", runID, err)
			}

			// Cancel rather than abandon. The run is still pending, so leaving
			// it would let it execute later, after this command has already
			// told the caller it did not start -- and someone who retries on
			// that message would get the command executed twice.
			if _, cerr := runs.CancelRun(ctx, runID); cerr != nil {
				ctx.Log.Warn("could not cancel a run that never started",
					"run", runID, "error", cerr)
			}
			return false, fmt.Errorf("run %s did not start within 2m and was canceled: %w", runID, err)
		}
		time.Sleep(runAttachPoll)
	}
}

// attachTargetMissing reports whether an attach failed because the sandbox is
// not there yet, as opposed to a reason waiting will not fix.
func attachTargetMissing(err error) bool {
	if errors.Is(err, cond.ErrNotFound{}) {
		return true
	}
	// The error crosses an RPC boundary, so the typed value does not survive and
	// the text is what there is to match on. These are the runner and proxy's
	// three ways of saying "not yet": no sandbox entity, no node assigned, and
	// no container booted.
	msg := err.Error()
	return strings.Contains(msg, "failed to find sandbox") ||
		strings.Contains(msg, "not scheduled to a node yet") ||
		strings.Contains(msg, "is not running yet")
}

// runIsOver reports whether the run has already reached a terminal state. A
// read failure answers no: the caller keeps waiting, which its own deadline
// bounds, rather than abandoning an attach over a transient blip.
func runIsOver(ctx *Context, runs *app_v1alpha.RunsClient, runID string) bool {
	got, err := runs.GetRun(ctx, runID)
	if err != nil {
		return false
	}

	info := got.Run()
	if info == nil {
		return false
	}

	return isTerminalRunStatus(info.Status())
}

// runSettleTimeout bounds how long the CLI waits for a run to record its
// outcome after the container is gone. The two are written by different actors
// -- the container ends, then the runner reports the exit to the run -- so a
// small gap is expected and is not the run still working.
const runSettleTimeout = 15 * time.Second

// reportRunExit propagates the run's exit code to the caller's shell.
//
// Called once the attach has ended with the container, so the run is finishing
// rather than running; it waits for the status to settle instead of reading
// once. Reading once is a coin flip on a fast task -- the exit reaches the run
// entity just after the container's stdio closes -- and losing it means a
// failing task reports success to whatever script invoked it.
//
// A *failure* to read the run is different from a run with no code: the task's
// outcome is unknown, and exiting 0 would tell a script it succeeded.
func reportRunExit(ctx *Context, runs *app_v1alpha.RunsClient, runID string) error {
	deadline := time.Now().Add(runSettleTimeout)

	var lastErr error
	for {
		got, err := runs.GetRun(ctx, runID)
		if err != nil {
			lastErr = err
		} else if info := got.Run(); info != nil {
			if info.HasExitCode() {
				ctx.SetExitCode(int(info.ExitCode()))
				return nil
			}
			// Terminal without a code: canceled and timed-out runs report the
			// teardown's exit rather than the task's, so there is nothing
			// honest to hand back to the shell.
			if isTerminalRunStatus(info.Status()) {
				return nil
			}
			lastErr = nil
		}

		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("reading run %s to report its exit code: %w", runID, lastErr)
			}
			// Still running after the container went away. Nothing to report,
			// and inventing a 0 would be the lie this function exists to avoid.
			ctx.Log.Warn("run had not recorded an outcome when its terminal closed", "run", runID)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(runAttachPoll)
	}
}

func isTerminalRunStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "timed_out", "canceled", "skipped":
		return true
	default:
		return false
	}
}

func runsClient(ctx *Context) (*app_v1alpha.RunsClient, error) {
	cl, err := ctx.RPCClient("dev.miren.runtime/app-runs")
	if err != nil {
		return nil, err
	}
	return app_v1alpha.NewRunsClient(cl), nil
}
