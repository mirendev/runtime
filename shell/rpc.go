package shell

import (
	"context"
	"log/slog"
	"strings"

	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/app"
	"miren.dev/runtime/lease"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg shell -input rpc.yml -output rpc.gen.go

type RPCShell struct {
	Log       *slog.Logger
	Lease     *lease.LaunchContainer
	Apps      *app.AppAccess
	Namespace string `asm:"namespace"`
}

var _ ShellAccess = (*RPCShell)(nil)

func (r *RPCShell) Open(ctx context.Context, state *ShellAccessOpen) error {
	args := state.Args()

	name := args.Application()

	ctx = namespaces.WithNamespace(ctx, r.Namespace)
	opts := args.Options()

	pool := opts.Pool()
	if pool == "" {
		pool = "shell"
	}

	ac, err := r.Apps.LoadApp(ctx, name)
	if err != nil {
		return err
	}

	lc, err := r.Lease.Lease(ctx, ac.Xid, lease.DontWaitNetwork(), lease.Pool(pool), lease.Command("sleep 48h"))
	if err != nil {
		return err
	}

	defer r.Lease.ReleaseLease(ctx, lc)

	cc, err := lc.Obj(ctx)
	if err != nil {
		return err
	}

	task, err := cc.Task(ctx, nil)
	if err != nil {
		return err
	}

	input := stream.ToReader(ctx, args.Input())
	output := stream.ToWriter(ctx, args.Output())

	copts := []cio.Opt{cio.WithStreams(input, output, output)}

	spec := r.spec(opts, lc)
	if spec.Terminal {
		r.Log.Debug("terminal shell")
		copts = append(copts, cio.WithTerminal)
	} else {
		r.Log.Debug("batch shell")
	}

	cstreams := cio.NewCreator(copts...)

	proc, err := task.Exec(ctx, idgen.Gen("e"), spec, cstreams)
	if err != nil {
		return err
	}

	err = proc.Start(ctx)
	if err != nil {
		return err
	}

	es, err := proc.Wait(ctx)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		args.Input().Close()
		args.Output().Close()
		state.Results().SetStatus(int32(130))
		proc.Kill(context.Background(), unix.SIGKILL)
		return ctx.Err()
	case status := <-es:
		proc.IO().Wait()

		err = status.Error()
		if err != nil {
			return err
		}

		state.Results().SetStatus(int32(status.ExitCode()))
	}

	return nil
}

func (r *RPCShell) spec(opts *ShellOptions, lc *lease.LeasedContainer) *specs.Process {
	spec := lc.Spec()

	proc := &specs.Process{
		Cwd:  "/app",
		Env:  spec.Process.Env,
		User: spec.Process.User,
	}

	ep := lc.Configuration.Entrypoint()

	args := opts.Command()

	if len(args) == 0 {
		if con := lc.Configuration.CommandFor("console"); con != "" {
			// CommandFor already prepends the entrypoint
			args = []string{"/bin/sh", "-c", "exec " + con}
		} else if ep != "" {
			args = []string{"/bin/sh", "-c", "exec " + ep + " /bin/sh"}
		} else {
			args = []string{"/bin/sh"}
		}
	} else if ep != "" {
		args = []string{"/bin/sh", "-c", "exec " + ep + " " + strings.Join(args, " ")}
	}

	proc.Args = args

	winsize := opts.WinSize()
	if winsize != nil {
		proc.Terminal = true
		proc.ConsoleSize = &specs.Box{
			Height: uint(winsize.Height()),
			Width:  uint(winsize.Width()),
		}
	}

	return proc
}
