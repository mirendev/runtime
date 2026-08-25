package execproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	run_v1alpha "miren.dev/runtime/api/run/run_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/stream"
)

// RunManager creates and cancels durable runs in-process. The exec proxy uses
// it on the legacy exec-by-app compatibility path so a run is created (and, when
// abandoned, canceled) with the caller's own identity rather than the
// coordinator's. *app.AppInfo satisfies it.
type RunManager interface {
	// CreateRunEntity records a run and returns its id and the name its first
	// attempt's sandbox will have.
	CreateRunEntity(ctx context.Context, app, task string, command []string, tty bool) (entity.Id, string, error)
	// CancelRunEntity requests cancellation of a run, reporting whether the
	// request was recorded (false if it was already terminal).
	CancelRunEntity(ctx context.Context, runID string) (bool, error)
}

type Server struct {
	Log  *slog.Logger
	EAC  *entityserver_v1alpha.EntityAccessClient
	rs   *rpc.State
	runs RunManager
}

func NewServer(
	log *slog.Logger,
	eac *entityserver_v1alpha.EntityAccessClient,
	rs *rpc.State,
	runs RunManager,
) *Server {
	return &Server{
		Log:  log,
		EAC:  eac,
		rs:   rs,
		runs: runs,
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
		// Compatibility window: a pre-durable-runs client (<= v0.13) reaches an
		// app by name here, expecting the old ephemeral-sandbox exec. Rather
		// than restore that leak-prone path, translate the request onto a
		// durable run and attach to it, so the run controller owns the sandbox's
		// lifecycle. Remove this once v0.13 is out of the compatibility window.
		return s.execByApp(ctx, req)
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

// execByAppTimeout bounds how long the legacy exec-by-app path waits for its
// run's sandbox to come up before giving up. It matches the client's own attach
// deadline (cli attachToRun waits 2m), so a slow image pull is tolerated while a
// run that never starts does not pin the request handler open forever.
const execByAppTimeout = 2 * time.Minute

// execByAppPoll is how often the wait loop re-checks the entity store.
const execByAppPoll = 250 * time.Millisecond

// execByApp serves a legacy exec-by-app request by creating a durable run and
// attaching to it. It exists only for the compatibility window (see the "app"
// case in Exec) and reproduces the old synchronous behavior a v0.13 client
// expects: run the app's console command, stream its terminal, return its exit
// code.
//
// The run is created with the caller's identity -- CreateRunEntity gates on
// rpc.AllowApp with this ctx -- which is stricter than the v0.13 exec-by-app
// path that had no app guard at all, and is the safe direction. Unlike the old
// ephemeral sandbox this replaces, an abandoned run stays owned by the run
// controller, so a handler that dies mid-request leaks nothing.
func (s *Server) execByApp(ctx context.Context, req *exec_v1alpha.SandboxExecExec) error {
	args := req.Args()

	var command []string
	opts := args.Options()
	if opts != nil {
		command = opts.Command()
	}

	runID, sandboxName, err := s.runs.CreateRunEntity(ctx, args.Value(), "", command, execTTY(opts))
	if err != nil {
		return err
	}

	// Bridge the incoming exec streams once and re-wrap them per attach attempt,
	// exactly as the client's attachToRun does. A not-ready attach returns before
	// any stdin is pumped, so re-serving the same reader across attempts is safe.
	inR := stream.ToReader(ctx, args.Input())
	defer inR.Close()
	outW := stream.ToWriter(ctx, args.Output())
	defer outW.Close()

	// winCh is read by a fresh ChanReader per attach attempt. ChanReader is lazy,
	// so a not-ready attempt does not drain it and a queued resize survives to the
	// next attempt.
	//
	// A v0.13 client reports its initial terminal size through the shell options,
	// not the update stream (its stream only carries later SIGWINCH events), so
	// seed that size as the first resize -- the old exec server read the same
	// field to size the pty. Seed before wiring the update stream so it is first
	// in line; the buffer of one holds it until the first successful attach reads
	// it.
	winCh := make(chan *exec_v1alpha.WindowSize, 1)
	if opts != nil && opts.HasWinSize() {
		winCh <- opts.WinSize()
	}
	if args.HasWindowUpdates() {
		stream.ChanWriter(ctx, args.WindowUpdates(), winCh)
	}

	// Mirror the client's attach loop: the sandbox exists only once the run
	// controller has admitted the run, and the node reports "not running yet"
	// until the container boots, so retry while it is simply not ready. A real
	// error -- a denial, a transport failure -- is returned at once rather than
	// waited out.
	deadline := time.Now().Add(execByAppTimeout)
	for {
		node, ready, err := s.runSandboxNode(ctx, sandboxName)
		if err != nil {
			return err
		}

		if ready {
			rcl, err := s.rs.Connect(node.ApiAddress, "dev.miren.runtime/exec")
			if err != nil {
				return fmt.Errorf("failed to connect to node %s: %w", node.ApiAddress, err)
			}

			sec := &exec_v1alpha.SandboxExecClient{Client: rcl}
			_, aerr := sec.Attach(
				ctx,
				sandboxName, "",
				stream.ServeReader(ctx, inR),
				stream.ServeWriter(ctx, outW),
				stream.ChanReader(winCh),
			)
			if aerr == nil {
				break // the container ended; its exit code is on the run.
			}
			if !attachNotReady(aerr) {
				return fmt.Errorf("attaching to run %s: %w", runID, aerr)
			}
		}

		// A short command can finish before the terminal ever attaches, taking
		// its container with it. That is not a failure -- the run did what it was
		// asked and recorded an outcome -- so stop waiting and report it.
		if s.runIsTerminal(ctx, runID) {
			break
		}

		if time.Now().After(deadline) {
			// Cancel before returning, or the run stays pending and the
			// controller executes it later -- after this call already told the
			// legacy client it failed. The legacy protocol hands back no run id,
			// so the caller cannot cancel it and a natural retry would run the
			// command twice. This mirrors the client's own attachToRun, which
			// cancels a run that never started for the same reason.
			if _, cerr := s.runs.CancelRunEntity(ctx, runID.String()); cerr != nil {
				s.Log.Warn("could not cancel a run that never started",
					"run", runID, "error", cerr)
			}
			return fmt.Errorf("run %s for app %s did not start within %s and was canceled", runID, args.Value(), execByAppTimeout)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(execByAppPoll)
	}

	code, err := s.runExitCode(ctx, runID)
	if err != nil {
		return err
	}
	req.Results().SetCode(code)
	return nil
}

// execTTY reports whether a legacy exec-by-app request wants a terminal. A
// v0.13 client never sets Terminal: it signals a pty by carrying an initial
// WinSize, which is the field the old exec server keyed on. Accept either, so
// both the real v0.13 signal and a well-formed Terminal flag allocate a pty.
func execTTY(opts *exec_v1alpha.ShellOptions) bool {
	if opts == nil {
		return false
	}
	return opts.HasWinSize() || opts.Terminal()
}

// runSandboxNode reports the node a run's sandbox is scheduled to. ready is
// false -- with no error -- while the sandbox has not been created or scheduled
// yet, which is the caller's cue to wait rather than fail.
func (s *Server) runSandboxNode(ctx context.Context, sandboxName string) (compute_v1alpha.Node, bool, error) {
	var node compute_v1alpha.Node

	ret, err := s.EAC.Get(ctx, sandboxName)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return node, false, nil
		}
		return node, false, fmt.Errorf("looking up run sandbox %s: %w", sandboxName, err)
	}

	var sch compute_v1alpha.Schedule
	sch.Decode(ret.Entity().Entity())
	if sch.Key.Node == "" {
		return node, false, nil
	}

	nret, err := s.EAC.Get(ctx, string(sch.Key.Node))
	if err != nil {
		return node, false, fmt.Errorf("looking up node %s: %w", sch.Key.Node, err)
	}
	node.Decode(nret.Entity().Entity())
	return node, true, nil
}

// runExitCode reads a finished run's exit code, waiting briefly for the runner
// to record it. The container's stdio closes just before the runner writes the
// outcome, so reading once would be a coin flip on a fast command -- and losing
// it would report a failing command as success to whatever invoked the old CLI.
// A run that ended without a task exit code (canceled, timed out) reports 0
// here; its status, not this number, carries that meaning, and the old protocol
// has nowhere to put it.
func (s *Server) runExitCode(ctx context.Context, runID entity.Id) (int32, error) {
	deadline := time.Now().Add(runSettleTimeout)
	var lastErr error
	for {
		run, err := s.getRun(ctx, runID)
		if err != nil {
			lastErr = err
		} else {
			if reportsExitCode(run.Status) && !run.Result.At.IsZero() {
				return clampInt32(run.Result.Code), nil
			}
			if isTerminalRunStatus(run.Status) {
				return 0, nil
			}
		}

		if time.Now().After(deadline) {
			// A run that could never be read has just been reported as a clean
			// zero-exit, which on the wire is indistinguishable from a real one.
			// Leave a trace so a corrupt or missing run entity is not silently a
			// success in the server log.
			if lastErr != nil {
				s.Log.Warn("could not read a run's outcome before reporting its exit; assuming 0",
					"run", runID, "error", lastErr)
			}
			return 0, nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		time.Sleep(execByAppPoll)
	}
}

// runIsTerminal reports whether a run has reached a terminal state. A read
// failure answers no, so the caller keeps waiting under its own deadline rather
// than abandoning over a transient blip.
func (s *Server) runIsTerminal(ctx context.Context, runID entity.Id) bool {
	run, err := s.getRun(ctx, runID)
	if err != nil {
		return false
	}
	return isTerminalRunStatus(run.Status)
}

func (s *Server) getRun(ctx context.Context, runID entity.Id) (*run_v1alpha.Run, error) {
	ret, err := s.EAC.Get(ctx, runID.String())
	if err != nil {
		return nil, err
	}
	var run run_v1alpha.Run
	run.Decode(ret.Entity().Entity())
	return &run, nil
}

// runSettleTimeout bounds how long runExitCode waits for the outcome to be
// recorded after the container is gone. The two are written by different actors,
// so a small gap is expected and is not the run still working.
const runSettleTimeout = 15 * time.Second

// attachNotReady reports whether an attach failed only because the run's
// sandbox is not up yet, as opposed to a reason waiting will not fix. The typed
// error does not survive the RPC hop to the node, so this matches the node's
// three ways of saying "not yet" by text, the same way the client's
// attachTargetMissing does.
func attachNotReady(err error) bool {
	if errors.Is(err, cond.ErrNotFound{}) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "failed to find sandbox") ||
		strings.Contains(msg, "not scheduled to a node yet") ||
		strings.Contains(msg, "is not running yet")
}

// reportsExitCode says whether a run ended by its command exiting, the only case
// where the recorded code describes the task rather than its teardown. It
// mirrors the app server's own rule for the same entity.
func reportsExitCode(s run_v1alpha.RunStatus) bool {
	switch s {
	case run_v1alpha.SUCCEEDED, run_v1alpha.FAILED:
		return true
	case run_v1alpha.PENDING, run_v1alpha.RUNNING, run_v1alpha.TIMED_OUT,
		run_v1alpha.CANCELED, run_v1alpha.SKIPPED:
		return false
	}
	return false
}

func isTerminalRunStatus(s run_v1alpha.RunStatus) bool {
	switch s {
	case run_v1alpha.SUCCEEDED, run_v1alpha.FAILED, run_v1alpha.TIMED_OUT,
		run_v1alpha.CANCELED, run_v1alpha.SKIPPED:
		return true
	case run_v1alpha.PENDING, run_v1alpha.RUNNING:
		return false
	}
	return false
}

// clampInt32 saturates rather than wrapping at the entity(int64)->wire(int32)
// boundary. Real exit codes are a byte, so this only matters for a corrupt or
// hostile value, where wrapping into a small plausible number is the worst
// outcome.
func clampInt32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
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
