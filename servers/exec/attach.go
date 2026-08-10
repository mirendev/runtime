package exec

import (
	"context"
	"fmt"
	"io"

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

	unsubscribe := hub.Subscribe(out)
	defer unsubscribe()

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

	// Pump the client's input into the container until the client goes away.
	// Reaching EOF here ends this attach and nothing else: the Hub's stdin
	// stays open, so the container's shell doesn't see EOF and exit.
	_, err := io.Copy(hubStdin{hub}, in)

	if ctx.Err() != nil {
		// The client disconnected. That is not an error and, deliberately, not
		// cancellation either -- the run keeps going and can be attached again.
		s.Log.Debug("attach client disconnected", "sandbox", sandboxID, "container", container)
		return nil
	}

	if err != nil && err != io.EOF {
		return fmt.Errorf("attach: relaying input: %w", err)
	}

	return nil
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
