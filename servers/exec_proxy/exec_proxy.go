package execproxy

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

type Server struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient
	rs  *rpc.State
}

func NewServer(
	log *slog.Logger,
	eac *entityserver_v1alpha.EntityAccessClient,
	rs *rpc.State,
) *Server {
	return &Server{
		Log: log,
		EAC: eac,
		rs:  rs,
	}
}

var _ exec_v1alpha.SandboxExec = (*Server)(nil)

func (s *Server) Exec(ctx context.Context, req *exec_v1alpha.SandboxExecExec) error {
	args := req.Args()

	id := args.Id()

	ret, err := s.EAC.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get entity %s: %w", id, err)
	}

	var sch compute_v1alpha.Schedule
	sch.Decode(ret.Entity().Entity())

	var node compute_v1alpha.Node

	nret, err := s.EAC.Get(ctx, string(sch.Key.Node))
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", sch.Key.Node, err)
	}

	node.Decode(nret.Entity().Entity())

	s.Log.Debug("passing exec to done", "address", node.ApiAddress, "node", node.ID, "id", id)

	rcl, err := s.rs.Connect(node.ApiAddress, "dev.miren.runtime/exec")
	if err != nil {
		return fmt.Errorf("failed to connect to node %s: %w", node.ApiAddress, err)
	}

	ecl := &exec_v1alpha.SandboxExecClient{Client: rcl}

	pargs := req.Args()

	r := stream.ToReader(ctx, args.Input())
	w := stream.ToWriter(ctx, args.Output())

	eret, err := ecl.Exec(ctx, pargs.Id(), pargs.Command(), stream.ServeReader(ctx, r), stream.ServeWriter(ctx, w))
	if err != nil {
		return fmt.Errorf("failed to exec on node %s: %w", node.ApiAddress, err)
	}

	req.Results().SetCode(eret.Code())

	return nil
}
