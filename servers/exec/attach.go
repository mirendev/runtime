package exec

import (
	"context"
	"fmt"
	"io"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/stream"
)

// defaultAttachContainer is the container name the app's own process runs
// under, matching what the deployment launcher and the run controller put in a
// sandbox spec.
const defaultAttachContainer = "app"

// attachDrainTimeout bounds how long an attach keeps delivering queued output
// after its container has gone. A healthy client finishes in well under this;
// the bound only matters for a connection that is wedged without having
// failed, which would otherwise hold the handler open forever.
const attachDrainTimeout = time.Minute

// Attach joins the caller to a container's existing primary process.
//
// This is deliberately not Exec. Exec starts a second process inside the
// container, which means the thing you are "attached" to belongs to your
// connection: it dies when you disconnect, and its exit code goes with it. That
// is fine for `miren sandbox exec`, where running a command is the point, and
// wrong for a task run, where the command is the workload and your terminal is
// just a window onto it.
//
// So Attach subscribes to the container's stdio instead. Several clients can
// watch at once, any of them can leave, and none of it reaches the workload --
// which is what lets a run outlive the client that started it.
func (s *Server) Attach(ctx context.Context, req *exec_v1alpha.SandboxExecAttach) error {
	args := req.Args()

	sandboxID := args.Sandbox()
	if sandboxID == "" {
		return fmt.Errorf("attach: sandbox is required")
	}

	container := args.Container()
	if container == "" {
		container = defaultAttachContainer
	}

	if s.Hubs == nil {
		return fmt.Errorf("attach: this node is not serving attachable containers")
	}

	hub := s.Hubs.Get(entity.Id(sandboxID), container)
	if hub == nil {
		// No hub yet, and the two reasons need different answers. A container
		// declared attachable simply has not booted yet, and the caller should
		// wait. One never declared stdin never will be attachable: containerd
		// wires up the FIFO at task creation and cannot add one later, so
		// waiting would burn the client's whole timeout for nothing.
		attachable, err := s.containerDeclaresStdin(ctx, sandboxID, container)
		if err != nil {
			return fmt.Errorf("attach: %w", err)
		}
		if attachable {
			return NotReadyError{Sandbox: sandboxID, Container: container}
		}
		return fmt.Errorf("attach: container %q of %s is not attachable", container, sandboxID)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	out := stream.ToWriter(ctx, args.Output())
	defer out.Close()

	sub := hub.Subscribe(out)
	defer sub.Stop()

	// Window updates drive the container's own pty, so a resize from any
	// attached client is seen by all of them. Shared-terminal semantics are the
	// consequence, and the honest one: they are all looking at one process.
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
					// The wire fields are signed. A negative or zero dimension
					// is meaningless, and casting one to uint32 would ask the
					// pty for a gigantic terminal rather than being ignored.
					if ws == nil || ws.Width() <= 0 || ws.Height() <= 0 {
						continue
					}
					if err := hub.Resize(ctx, uint32(ws.Width()), uint32(ws.Height())); err != nil {
						s.Log.Debug("failed to resize attached terminal", "error", err)
					}
				}
			}
		}()
	}

	in := stream.ToReader(ctx, args.Input())
	defer in.Close()

	// Pump the client's input into the container for as long as the client is
	// there. Reaching EOF here ends the pump and nothing else: the Hub's stdin
	// stays open, so the container's shell doesn't see EOF and exit.
	//
	// Deliberately not the termination condition for the attach itself. The
	// client's stdin says nothing about the workload -- a non-interactive
	// caller's is /dev/null and EOFs at once -- so ending here would report a
	// task that is still running as finished, and would leave an interactive
	// client's later keystrokes writing into a closed pipe.
	copyErr := make(chan error, 1)
	go func() {
		_, err := io.Copy(hubStdin{hub}, in)
		copyErr <- err
	}()

	select {
	case <-hub.Done():
		// The container is gone, which is the one thing that genuinely ends an
		// attach. Whatever the client is still sending is abandoned along with
		// the input pump, which costs nothing: there is no longer a process on
		// the other end of that stdin to receive it.
		s.Log.Debug("attach ended with the container", "sandbox", sandboxID, "container", container)
		return finishAttach(ctx, sub)

	case <-ctx.Done():
		// The client disconnected. That is not an error and, deliberately, not
		// cancellation either -- the run keeps going and can be attached again.
		s.Log.Debug("attach client disconnected", "sandbox", sandboxID, "container", container)
		return nil

	case err := <-copyErr:
		// The client's input ended on its own. Keep serving output until the
		// container ends or the client goes away; a script that pipes in a
		// command and then waits for the result depends on this.
		if err != nil && err != io.EOF {
			return fmt.Errorf("attach: relaying input: %w", err)
		}
		select {
		case <-hub.Done():
			return finishAttach(ctx, sub)
		case <-ctx.Done():
			return nil
		}
	}
}

// finishAttach delivers whatever output is still queued for a client whose
// container has ended, and says so if the client did not get all of it.
//
// The container ending is not the output ending. Chunks the Hub accepted are
// still crossing the network one round trip at a time, and returning first
// cuts the client's stream short -- which, with the run's exit code reported
// separately, reads as a complete and successful command (MIR-2012). So the
// attach waits for the queue, and a stream with gaps in it comes back as an
// error rather than as a quiet success.
func finishAttach(ctx context.Context, sub *sandbox.Subscription) error {
	select {
	case <-sub.Drained():
	case <-ctx.Done():
		// The client left; there is nobody to tell.
		return nil
	case <-time.After(attachDrainTimeout):
		return OutputTruncatedError{Reason: fmt.Sprintf("the client did not receive the rest within %s", attachDrainTimeout)}
	}

	if n := sub.Dropped(); n > 0 {
		return OutputTruncatedError{Reason: fmt.Sprintf("%d bytes of output never reached the client", n)}
	}
	return nil
}

// OutputTruncatedError says an attached client did not receive all of a
// container's output. The log stream still has it.
type OutputTruncatedError struct {
	Reason string
}

func (e OutputTruncatedError) Error() string {
	return "attach: output truncated: " + e.Reason
}

// NotReadyError says the container is attachable but has not booted yet, which
// is worth waiting for. It is distinct from "not attachable" so a client can
// tell a race from a permanent answer instead of retrying both for two minutes
// and then reporting one generic failure.
type NotReadyError struct {
	Sandbox   string
	Container string
}

func (e NotReadyError) Error() string {
	return fmt.Sprintf("attach: container %q of %s is not running yet", e.Container, e.Sandbox)
}

// containerDeclaresStdin reports whether the sandbox spec asked for this
// container to be attachable, which is what separates "not yet" from "never".
func (s *Server) containerDeclaresStdin(ctx context.Context, sandboxID, container string) (bool, error) {
	resp, err := s.EAC.Get(ctx, sandboxID)
	if err != nil {
		return false, fmt.Errorf("reading sandbox %s: %w", sandboxID, err)
	}

	var sb compute.Sandbox
	sb.Decode(resp.Entity().Entity())

	for _, c := range sb.Spec.Container {
		if c.Name == container {
			return c.Stdin, nil
		}
	}
	return false, nil
}

// hubStdin adapts a Hub to io.Writer for the input pump.
type hubStdin struct{ hub *sandbox.Hub }

func (h hubStdin) Write(p []byte) (int, error) { return h.hub.WriteStdin(p) }
