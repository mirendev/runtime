package exec

import (
	"context"
	"fmt"
	"log/slog"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/opencontainers/runtime-spec/specs-go"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/shellwords"
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

	id := args.Id()

	containers, err := s.CC.Containers(ctx, `labels."runtime.computer/entity-id"==`+id)
	if err != nil {
		return err
	}

	if len(containers) == 0 {
		return fmt.Errorf("no container found for %s", id)
	}

	s.Log.Debug("found containers", "count", len(containers))

	var firstContainer containerd.Container

	// Find the first non-sandbox container
	for _, container := range containers {
		lbls, err := container.Labels(ctx)
		if err != nil {
			continue
		}

		if lbls["runtime.computer/container-kind"] != "sandbox" {
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

	r := stream.ToReader(ctx, args.Input())
	w := stream.ToWriter(ctx, args.Output())

	cmd, err := shellwords.Split(args.Command())
	if err != nil {
		return err
	}

	spec, err := firstContainer.Spec(ctx)
	if err != nil {
		return err
	}

	proc, err := task.Exec(ctx,
		idgen.Gen("t"),
		&specs.Process{
			Args: cmd,
			Env:  spec.Process.Env,
			Cwd:  spec.Process.Cwd,
		},
		cio.NewCreator(cio.WithStreams(r, w, w)),
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
