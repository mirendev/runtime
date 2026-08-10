package exec

import (
	"context"
	"fmt"
	"io"

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
		// Either the sandbox isn't on this node, or its container was created
		// without stdin. The second case is not recoverable at attach time:
		// containerd wires up a stdin FIFO when the task is created and cannot
		// add one later, so this has to be a clear error rather than a
		// half-working attach with no input.
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

// hubStdin adapts a Hub to io.Writer for the input pump.
type hubStdin struct{ hub *sandbox.Hub }

func (h hubStdin) Write(p []byte) (int, error) { return h.hub.WriteStdin(p) }
