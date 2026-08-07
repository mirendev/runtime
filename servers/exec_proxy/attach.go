package execproxy

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

// Attach forwards a client's attach request to the node running the sandbox.
//
// Unlike the exec path this proxy creates nothing and deletes nothing. The
// sandbox it attaches to is owned by whatever created it -- for a task run,
// the run controller -- and outlives any particular client. That is what makes
// disconnecting safe: there is no cleanup closure here whose absence could
// strand a sandbox, and no request handler whose death takes one with it.
func (s *Server) Attach(ctx context.Context, req *exec_v1alpha.SandboxExecAttach) error {
	args := req.Args()

	id := args.Sandbox()
	if id == "" {
		return fmt.Errorf("attach: sandbox is required")
	}

	ent, err := s.EAC.Get(ctx, id)
	if err != nil {
		// Accept a bare name as well as a full id, matching Exec's resolution.
		var retryErr error
		ent, retryErr = s.EAC.Get(ctx, "sandbox/"+id)
		if retryErr != nil {
			return fmt.Errorf("failed to find sandbox %s: %w", id, err)
		}
		id = "sandbox/" + id
	}

	found := ent.Entity().Entity()

	// Confine an app-scoped caller to its own app, exactly as Exec does.
	// Attaching hands the caller the container's stdio, so it needs the same
	// guard: the proxy forwards to the node over the coordinator cert, which
	// loses the caller's identity, and nothing downstream re-checks. The app is
	// resolved from the sandbox's own version rather than anything caller-sent.
	if app := s.resolveSandboxApp(ctx, found); !rpc.AllowApp(ctx, app) {
		return rpc.AppAccessError(ctx, app)
	}

	var sch compute_v1alpha.Schedule
	sch.Decode(found)

	if sch.Key.Node == "" {
		return fmt.Errorf("sandbox %s is not scheduled to a node yet", id)
	}

	nret, err := s.EAC.Get(ctx, string(sch.Key.Node))
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", sch.Key.Node, err)
	}

	var node compute_v1alpha.Node
	node.Decode(nret.Entity().Entity())

	s.Log.Debug("passing attach to node", "address", node.ApiAddress, "node", node.ID, "sandbox", id)

	rcl, err := s.rs.Connect(node.ApiAddress, "dev.miren.runtime/exec")
	if err != nil {
		return fmt.Errorf("failed to connect to node %s: %w", node.ApiAddress, err)
	}

	ecl := &exec_v1alpha.SandboxExecClient{Client: rcl}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	r := stream.ToReader(ctx, args.Input())
	w := stream.ToWriter(ctx, args.Output())

	defer r.Close()
	defer w.Close()

	ch := make(chan *exec_v1alpha.WindowSize, 1)
	ws := stream.ChanReader(ch)

	if args.HasWindowUpdates() {
		stream.ChanWriter(ctx, args.WindowUpdates(), ch)
	}

	if _, err := ecl.Attach(ctx, id, args.Container(),
		stream.ServeReader(ctx, r), stream.ServeWriter(ctx, w), ws); err != nil {
		return fmt.Errorf("failed to attach on node %s: %w", node.ApiAddress, err)
	}

	return nil
}
