package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/davecgh/go-spew/spew"
	"github.com/opencontainers/runtime-spec/specs-go"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/app"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
)

type debugWriter struct {
	w io.Writer
}

func (d *debugWriter) Write(p []byte) (n int, err error) {
	spew.Printf("Writing data: %v\n", p)
	return d.w.Write(p)
}

type debugReader struct {
	r io.Reader
}

func (d *debugReader) Read(p []byte) (n int, err error) {
	n, err = d.r.Read(p)

	spew.Printf("Read data: %v\n", p[:n])

	return n, err
}

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

	input := stream.ToReader(ctx, args.Input())
	output := stream.ToWriter(ctx, args.Output())

	if args.HasWindowUpdates() {
		ch := make(chan *exec_v1alpha.WindowSize)
		stream.ChanWriter(ctx, args.WindowUpdates(), ch)
	}

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

	es, err := proc.Wait(ctx)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		args.Input().Close()
		args.Output().Close()
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

func (e *Server) spec(opts *exec_v1alpha.ShellOptions, spec *oci.Spec, ver *core_v1alpha.AppVersion) (*specs.Process, error) {
	proc := &specs.Process{
		Cwd:  spec.Process.Cwd,
		Env:  spec.Process.Env,
		User: spec.Process.User,
	}

	var cfg app.Configuration

	if ver != nil && ver.Configuration != nil {
		err := json.Unmarshal(ver.Configuration, &cfg)
		if err != nil {
			return nil, err
		}
	}

	ep := cfg.Entrypoint()

	args := opts.Command()

	if len(args) == 0 {
		if con := cfg.CommandFor("console"); con != "" {
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
