package execproxy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
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
		id      string
		found   *entity.Entity
		cleanup func()
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

		// Create ephemeral sandbox for this console session
		sbEnt, cleanupFn, err := s.createEphemeralSandbox(ctx, &appEnt, &appVer)
		if err != nil {
			return fmt.Errorf("failed to create ephemeral sandbox: %w", err)
		}
		cleanup = cleanupFn

		found = sbEnt
		id = found.Id().String()
	}

	// Ensure cleanup runs when we're done
	if cleanup != nil {
		defer cleanup()
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

// createEphemeralSandbox creates a new sandbox for a console session.
// The sandbox is deleted when the returned cleanup function is called.
func (s *Server) createEphemeralSandbox(
	ctx context.Context,
	app *core_v1alpha.App,
	ver *core_v1alpha.AppVersion,
) (*entity.Entity, func(), error) {
	// Build sandbox spec
	spec, err := s.buildSandboxSpec(ctx, app, ver)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build sandbox spec: %w", err)
	}

	// Get app metadata for labels
	appResp, err := s.EAC.Get(ctx, app.ID.String())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get app metadata: %w", err)
	}

	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	// Create sandbox entity
	sbName := idgen.GenNS(fmt.Sprintf("%s-web", appMD.Name))
	sbID := entity.Id("sandbox/" + sbName)

	sb := &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.PENDING,
		Spec:   *spec,
	}

	s.Log.Info("creating ephemeral sandbox", "id", sbID, "app", appMD.Name)

	_, err = s.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name: sbName,
			Labels: types.LabelSet(
				"app", appMD.Name,
				"service", "web",
				"ephemeral", "true",
			),
		}).Encode,
		entity.DBId, sbID,
		sb.Encode,
	).Attrs())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create sandbox entity: %w", err)
	}

	// Wait for sandbox to become RUNNING
	sbEnt, err := s.waitForSandboxRunning(ctx, sbID)
	if err != nil {
		// Cleanup on failure
		s.deleteSandbox(sbID)
		return nil, nil, fmt.Errorf("sandbox failed to start: %w", err)
	}

	// Return cleanup function that deletes the sandbox
	cleanup := func() {
		s.Log.Info("cleaning up ephemeral sandbox", "id", sbID)
		s.deleteSandbox(sbID)
	}

	return sbEnt, cleanup, nil
}

// waitForSandboxRunning watches the sandbox entity until it reaches RUNNING status.
func (s *Server) waitForSandboxRunning(ctx context.Context, sbID entity.Id) (*entity.Entity, error) {
	// Create a timeout context (2 minutes should be enough for sandbox to boot)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	type result struct {
		ent *entity.Entity
		err error
	}

	resultCh := make(chan result, 1)

	// Helper to check sandbox status and send result if terminal
	checkStatus := func(ent *entity.Entity) bool {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent)

		switch sb.Status {
		case compute_v1alpha.RUNNING:
			s.Log.Info("ephemeral sandbox is running", "id", sbID)
			resultCh <- result{ent: ent}
			return true
		case compute_v1alpha.DEAD, compute_v1alpha.STOPPED:
			resultCh <- result{err: fmt.Errorf("sandbox failed to start, status: %s", sb.Status)}
			return true
		default:
			// PENDING or NOT_READY - keep waiting
			s.Log.Debug("sandbox not ready yet", "id", sbID, "status", sb.Status)
			return false
		}
	}

	// Watch for entity updates in background
	go func() {
		_, err := s.EAC.WatchEntity(ctx, sbID.String(), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
			if !op.HasEntity() {
				// Entity was deleted
				resultCh <- result{err: fmt.Errorf("sandbox entity was deleted")}
				return nil
			}

			checkStatus(op.Entity().Entity())
			return nil
		}))
		if err != nil && ctx.Err() == nil {
			resultCh <- result{err: fmt.Errorf("failed to watch sandbox: %w", err)}
		}
	}()

	// Check current status in case sandbox became RUNNING while watch was being set up
	resp, err := s.EAC.Get(ctx, sbID.String())
	if err == nil {
		if checkStatus(resp.Entity().Entity()) {
			// Already in terminal state, return immediately
			res := <-resultCh
			return res.ent, res.err
		}
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout waiting for sandbox to become running")
	case res := <-resultCh:
		return res.ent, res.err
	}
}

// deleteSandbox deletes a sandbox entity, triggering cleanup by the sandbox controller.
func (s *Server) deleteSandbox(sbID entity.Id) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := s.EAC.Delete(ctx, sbID.String())
	if err != nil {
		s.Log.Error("failed to delete ephemeral sandbox", "id", sbID, "error", err)
	}
}

// buildSandboxSpec creates a SandboxSpec for the given service.
// Adapted from controllers/deployment/launcher.go:buildSandboxSpec
func (s *Server) buildSandboxSpec(
	ctx context.Context,
	app *core_v1alpha.App,
	ver *core_v1alpha.AppVersion,
) (*compute_v1alpha.SandboxSpec, error) {
	// Get app metadata
	appResp, err := s.EAC.Get(ctx, app.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get app: %w", err)
	}

	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	spec := &compute_v1alpha.SandboxSpec{
		Version:      ver.ID,
		LogEntity:    app.ID.String(),
		LogAttribute: types.LabelSet("stage", "console", "service", "web"),
	}

	// Determine start directory, defaulting to /app
	startDir := ver.Config.StartDirectory
	if startDir == "" {
		startDir = "/app"
	}

	image := ver.ImageUrl

	appCont := compute_v1alpha.SandboxSpecContainer{
		Name:  "app",
		Image: image,
		Env: []string{
			"MIREN_APP=" + appMD.Name,
			"MIREN_VERSION=" + ver.Version,
		},
		Directory: startDir,
	}

	// Add global config env vars
	envMap := make(map[string]string)
	for _, x := range ver.Config.Variable {
		envMap[x.Key] = x.Value
	}

	// Convert map to env var slice
	for k, v := range envMap {
		appCont.Env = append(appCont.Env, k+"="+v)
	}

	appCont.Command = "/bin/sh"
	appCont.Tty = true
	appCont.Stdin = true

	spec.Container = []compute_v1alpha.SandboxSpecContainer{appCont}

	return spec, nil
}
