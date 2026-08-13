package execproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
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

	var (
		id    string
		found *entity.Entity
	)

	// Only the "id" category remains. Reaching an app by name used to mean
	// creating an ephemeral sandbox here, in a request handler whose death
	// leaked it -- that is now a Run, owned by the run controller, and reached
	// with Attach rather than Exec.
	switch args.Category() {
	case "id":
		id = args.Value()
		ret, err := s.EAC.Get(ctx, id)
		if err != nil {
			// Retry a bare name as a sandbox id, but only when it is actually
			// bare: re-prefixing an id that already carries one produces
			// "sandbox/sandbox/run-..." in the error a client is shown, which
			// reads as corruption rather than a missing entity.
			if errors.Is(err, cond.ErrNotFound{}) && !strings.HasPrefix(id, "sandbox/") {
				id = "sandbox/" + id
				ret, err = s.EAC.Get(ctx, id)
			}

			if err != nil {
				return fmt.Errorf("failed to get entity %s: %w", id, err)
			}
		}

		found = ret.Entity().Entity()
		id = found.Id().String()

		// Confine an app-scoped caller (e.g. an app-debugger workload) to its
		// own app. This is the load-bearing guard: exec is unguarded downstream,
		// and the proxy forwards to the node's exec server over the coordinator
		// cert, which would lose the caller's identity. Resolve the target's app
		// from its own version rather than trusting anything caller-sent; an
		// unscoped caller (cert/operator) is unaffected.
		if app := s.resolveSandboxApp(ctx, found); !rpc.AllowApp(ctx, app) {
			return rpc.AppAccessError(ctx, app)
		}

	case "app":
		return fmt.Errorf("exec by app name is no longer supported; use `miren app run` (which creates a run) or `miren sandbox exec` for an existing sandbox")
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

	s.Log.Debug("passing exec to node", "address", node.ApiAddress, "node", node.ID, "id", id)

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

// resolveSandboxApp returns the app a sandbox entity belongs to, or "" if it
// cannot be determined. A "" result denies an app-scoped caller (rpc.AllowApp
// fails closed against an empty app), which is the safe outcome — we never let a
// workload exec into a target whose ownership we can't verify.
func (s *Server) resolveSandboxApp(ctx context.Context, sandbox *entity.Entity) string {
	var sb compute_v1alpha.Sandbox
	sb.Decode(sandbox)
	if sb.Spec.Version == "" {
		return ""
	}

	verResp, err := s.EAC.Get(ctx, sb.Spec.Version.String())
	if err != nil {
		return ""
	}

	var ver core_v1alpha.AppVersion
	ver.Decode(verResp.Entity().Entity())
	if ver.App == "" {
		return ""
	}

	appResp, err := s.EAC.Get(ctx, ver.App.String())
	if err != nil {
		return ""
	}

	var meta core_v1alpha.Metadata
	meta.Decode(appResp.Entity().Entity())
	return meta.Name
}
