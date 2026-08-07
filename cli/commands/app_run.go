package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
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

	created, err := runs.CreateRun(ctx, opts.App, opts.Task, opts.Args)
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

	if err := attachToRun(ctx, runID, created.SandboxName()); err != nil {
		return err
	}

	return reportRunExit(ctx, runs, runID)
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

	if err := attachToRun(ctx, info.Id(), info.Sandbox()); err != nil {
		return err
	}

	return reportRunExit(ctx, runs, info.Id())
}

// attachToRun streams a run's terminal until the client disconnects or the run
// ends.
func attachToRun(ctx *Context, runID, sandboxName string) error {
	opt := new(exec_v1alpha.ShellOptions)
	in, out, winUpdates, cleanup := setupExecIO(ctx, opt)
	defer cleanup()

	cl, err := ctx.RPCClient("dev.miren.runtime/exec")
	if err != nil {
		return err
	}

	sec := exec_v1alpha.NewSandboxExecClient(cl)

	// The sandbox exists only once the controller has admitted the run, so retry
	// briefly rather than failing on a race the user cannot see.
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, err = sec.Attach(
			ctx,
			sandboxName, "",
			stream.ServeReader(ctx, in),
			stream.ServeWriter(ctx, out),
			stream.ChanReader(winUpdates),
		)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("attaching to run %s: %w", runID, err)
		}
		time.Sleep(runAttachPoll)
	}
}

// reportRunExit propagates the run's exit code to the caller's shell.
//
// The run may still be going -- detaching is not cancelling -- in which case
// there is no code to report and the command exits successfully, having done
// what it was asked.
func reportRunExit(ctx *Context, runs *app_v1alpha.RunsClient, runID string) error {
	got, err := runs.GetRun(ctx, runID)
	if err != nil {
		return nil
	}

	info := got.Run()
	if info == nil || !info.HasExitCode() {
		return nil
	}

	ctx.SetExitCode(int(info.ExitCode()))
	return nil
}

func runsClient(ctx *Context) (*app_v1alpha.RunsClient, error) {
	cl, err := ctx.RPCClient("dev.miren.runtime/app-runs")
	if err != nil {
		return nil, err
	}
	return app_v1alpha.NewRunsClient(cl), nil
}
