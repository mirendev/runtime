package execproxy

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

type Server struct {
	Log    *slog.Logger
	EAC    *entityserver_v1alpha.EntityAccessClient
	rs     *rpc.State
	AppAct activator.AppActivator
}

func NewServer(
	log *slog.Logger,
	eac *entityserver_v1alpha.EntityAccessClient,
	rs *rpc.State,
	appAct activator.AppActivator,
) *Server {
	return &Server{
		Log:    log,
		EAC:    eac,
		rs:     rs,
		AppAct: appAct,
	}
}

var _ exec_v1alpha.SandboxExec = (*Server)(nil)

func (s *Server) Exec(ctx context.Context, req *exec_v1alpha.SandboxExecExec) error {
	args := req.Args()

	var (
		id    string
		found *entity.Entity
	)

	switch args.Category() {
	case "id":
		id = args.Value()
		ret, err := s.EAC.Get(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to get entity %s: %w", id, err)
		}

		found = ret.Entity().Entity()

	case "app":
		name := args.Value()

		ent, err := s.EAC.Get(ctx, "app/"+name)
		if err != nil {
			return fmt.Errorf("failed to get entity %s: %w", name, err)
		}

		var appEnt core_v1alpha.App
		appEnt.Decode(ent.Entity().Entity())

		if appEnt.ActiveVersion == "" {
			return fmt.Errorf("app %s has no active version", name)
		}

		verEnt, err := s.EAC.Get(ctx, appEnt.ActiveVersion.String())
		if err != nil {
			return fmt.Errorf("failed to get app version %s: %w", appEnt.ActiveVersion, err)
		}

		var appVer core_v1alpha.AppVersion
		appVer.Decode(verEnt.Entity().Entity())

		lease, err := s.AppAct.AcquireLease(ctx, &appVer, "exec")
		if err != nil {
			return fmt.Errorf("failed to acquire lease for app %s: %w", name, err)
		}

		defer s.AppAct.ReleaseLease(ctx, lease)

		found = lease.SandboxEntity()
		id = found.Id().String()
	}

	if found == nil {
		return fmt.Errorf("no sandbox found with category=%s, value=%s", args.Category(), args.Value())
	}

	var sch compute_v1alpha.Schedule
	sch.Decode(found)

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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pargs := req.Args()

	r := stream.ToReader(ctx, args.Input())
	w := stream.ToWriter(ctx, args.Output())

	defer r.Close()
	defer w.Close()

	ch := make(chan *exec_v1alpha.WindowSize, 1)

	ws := stream.ChanReader(ch)

	if args.HasWindowUpdates() {
		stream.ChanWriter(ctx, args.WindowUpdates(), ch)
	}

	eret, err := ecl.Exec(ctx, "id", id, pargs.Command(), pargs.Options(), stream.ServeReader(ctx, r), stream.ServeWriter(ctx, w), ws)
	if err != nil {
		return fmt.Errorf("failed to exec on node %s: %w", node.ApiAddress, err)
	}

	req.Results().SetCode(eret.Code())

	return nil
}
