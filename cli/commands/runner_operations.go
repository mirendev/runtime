package commands

import (
	"errors"
	"fmt"

	"miren.dev/runtime/pkg/runnerlifecycle"
	"miren.dev/runtime/pkg/serverlifecycle"
)

// The runner's operations commands read the ledger on this host directly.
// The coordinator reaches a runner's ledger over RPC; the runner's own host
// has the files, and they stay readable while an operation restarts the
// runner that would otherwise answer.

// RunnerOperationsRun is the executor entry point the transient systemd unit
// invokes for a runner operation.
func RunnerOperationsRun(ctx *Context, opts struct {
	Operation string `long:"operation" required:"true" description:"Operation id to execute or resume"`
	Dir       string `long:"dir" description:"Operation directory" default:"/var/lib/miren/runner/lifecycle"`
	Config    string `long:"config" description:"Runner config, for reaching the runner to verify readiness" default:"/var/lib/miren/runner/config.yaml"`
}) error {
	store, err := serverlifecycle.NewStore(opts.Dir)
	if err != nil {
		return err
	}
	ex := serverlifecycle.NewExecutor(store, serverlifecycle.RunnerOptions(), ctx.Log)
	ex.WithProber(&runnerlifecycle.Prober{ConfigPath: opts.Config, Log: ctx.Log})

	op, err := ex.Run(ctx, opts.Operation)
	if err != nil {
		return err
	}
	if !op.Succeeded() {
		return fmt.Errorf("operation %s %s: %s", op.ID, op.Phase, op.Error)
	}
	return nil
}

func RunnerOperationsList(ctx *Context, opts struct {
	FormatOptions
	Dir string `long:"dir" description:"Operation directory" default:"/var/lib/miren/runner/lifecycle"`
}) error {
	store, err := openStore(opts.Dir)
	if err != nil {
		return err
	}
	ops, err := store.List()
	if err != nil {
		return err
	}
	return printOperationList(ctx, opts.FormatOptions, ops)
}

func RunnerOperationsShow(ctx *Context, opts struct {
	FormatOptions
	Dir string `long:"dir" description:"Operation directory" default:"/var/lib/miren/runner/lifecycle"`
	ID  string `position:"0" usage:"Operation id" required:"true"`
}) error {
	store, err := openStore(opts.Dir)
	if err != nil {
		return err
	}
	op, err := store.Get(opts.ID)
	if err != nil {
		return err
	}
	return printOperation(ctx, opts.FormatOptions, op)
}

// RunnerOperationsAbandon ends an operation nobody will finish, such as an
// executor that died mid-operation. A runner has no data restore to settle.
func RunnerOperationsAbandon(ctx *Context, opts struct {
	Dir string `long:"dir" description:"Operation directory" default:"/var/lib/miren/runner/lifecycle"`
	ID  string `position:"0" usage:"Operation id" required:"true"`
}) error {
	store, err := serverlifecycle.NewStore(opts.Dir)
	if err != nil {
		return err
	}
	op, err := serverlifecycle.Abandon(store, opts.ID)
	if err != nil {
		if errors.Is(err, serverlifecycle.ErrLocked) {
			return fmt.Errorf("%w; wait for it to finish or stop unit %s first", err, serverlifecycle.UnitName(opts.ID))
		}
		return err
	}
	ctx.Info("Abandoned %s operation %s (%s)", op.Action, op.ID, op.Phase)
	return nil
}
