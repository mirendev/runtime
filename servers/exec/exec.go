package exec

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
)

type Server struct {
	Log *slog.Logger
	CC  *containerd.Client

	EAC *entityserver_v1alpha.EntityAccessClient

	Namespace string `asm:"namespace"`
}

var _ exec_v1alpha.SandboxExec = (*Server)(nil)

func (s *Server) Exec(ctx context.Context, req *exec_v1alpha.SandboxExecExec) error {
	args := req.Args()

	if args.Category() != "id" {
		return fmt.Errorf("invalid category %s", args.Category())
	}

	id := args.Value()

	containers, err := s.CC.Containers(ctx, `labels."runtime.computer/entity-id"==`+id)
	if err != nil {
		return err
	}

	if len(containers) == 0 {
		return fmt.Errorf("no container found for %s", id)
	}

	s.Log.Debug("found containers", "count", len(containers))

	var (
		firstContainer containerd.Container
		verId          string
	)

	// Find the first non-sandbox container
	for _, container := range containers {
		lbls, err := container.Labels(ctx)
		if err != nil {
			continue
		}

		if lbls["runtime.computer/container-kind"] != "sandbox" {
			verId = lbls["runtime.computer/version-entity"]
			firstContainer = container
			break
		}
	}

	if firstContainer == nil {
		return fmt.Errorf("no non-sandbox container found for %s", id)
	}

	s.Log.Debug("found container", "id", firstContainer.ID())

	// TODO support specifying which container to exec into

	task, err := firstContainer.Task(ctx, nil)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	input := stream.ToReader(ctx, args.Input())
	output := stream.ToWriter(ctx, args.Output())

	defer input.Close()
	defer output.Close()

	spec, err := firstContainer.Spec(ctx)
	if err != nil {
		return err
	}

	var ver *core_v1alpha.AppVersion

	if verId != "" {
		res, err := s.EAC.Get(ctx, verId)
		if err != nil {
			return err
		}

		var v core_v1alpha.AppVersion
		v.Decode(res.Entity().Entity())

		s.Log.Debug("found version", "id", verId)

		ver = &v
	}

	pspec, err := s.spec(args.Options(), spec, ver)
	if err != nil {
		return err
	}

	copts := []cio.Opt{cio.WithStreams(input, output, output)}

	if pspec.Terminal {
		copts = append(copts, cio.WithTerminal)
	}

	cstreams := cio.NewCreator(copts...)

	proc, err := task.Exec(ctx,
		idgen.Gen("t"),
		pspec,
		cstreams,
	)
	if err != nil {
		return err
	}

	err = proc.Start(ctx)
	if err != nil {
		return err
	}

	// Handle window resize events
	if args.HasWindowUpdates() {
		winCh := make(chan *exec_v1alpha.WindowSize)
		stream.ChanWriter(ctx, args.WindowUpdates(), winCh)

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case ws, ok := <-winCh:
					if !ok {
						return
					}
					if err := proc.Resize(ctx, uint32(ws.Width()), uint32(ws.Height())); err != nil {
						s.Log.Debug("failed to resize terminal", "error", err)
					}
				}
			}
		}()
	}

	es, err := proc.Wait(ctx)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		req.Results().SetCode(int32(130))
		proc.Kill(context.Background(), 9)
		return ctx.Err()
	case status := <-es:
		proc.IO().Wait()

		err = status.Error()
		if err != nil {
			return nil
		}

		req.Results().SetCode(int32(status.ExitCode()))
	}

	return nil
}

func (e *Server) command(ver *core_v1alpha.AppVersion, service string) string {
	for _, cmd := range ver.Config.Commands {
		if cmd.Service == service && cmd.Command != "" {
			if ver.Config.Entrypoint != "" {
				return ver.Config.Entrypoint + " " + cmd.Command
			}
			return cmd.Command
		}
	}

	return ""
}

func (e *Server) spec(opts *exec_v1alpha.ShellOptions, spec *oci.Spec, ver *core_v1alpha.AppVersion) (*specs.Process, error) {
	proc := &specs.Process{
		Cwd:  spec.Process.Cwd,
		Env:  spec.Process.Env,
		User: spec.Process.User,
	}

	ep := ver.Config.Entrypoint

	args := opts.Command()

	if len(args) == 0 {
		if con := e.command(ver, "console"); con != "" {
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

	return proc, nil
}
