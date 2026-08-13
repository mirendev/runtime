package run

import (
	"context"
	"log/slog"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

// SandboxWatchController wakes the run controller when a run's sandbox changes.
//
// It is needed because a reconcile controller watches exactly one index, and
// the run controller watches runs. A sandbox reaching STOPPED with an exit code
// produces no event on that index, so without this bridge a finished run would
// sit in running until the deadline sweep happened to look at it -- turning
// every completion into a delay of up to the sweep interval.
type SandboxWatchController struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient

	RunController *controller.ReconcileController
}

func NewSandboxWatchController(
	log *slog.Logger,
	eac *entityserver_v1alpha.EntityAccessClient,
	runController *controller.ReconcileController,
) *SandboxWatchController {
	return &SandboxWatchController{
		Log:           log.With("module", "run-sandbox-watch"),
		EAC:           eac,
		RunController: runController,
	}
}

func (w *SandboxWatchController) Init(ctx context.Context) error { return nil }

func (w *SandboxWatchController) Create(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) error {
	return w.Update(ctx, sb, meta)
}

func (w *SandboxWatchController) Update(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) error {
	return w.wake(ctx, runIDFor(sb))
}

// Delete matters as much as Update. A sandbox deleted out from under a running
// run means nothing will ever report its exit, so the run has to be told rather
// than left waiting for a timeout that may be hours away.
func (w *SandboxWatchController) Delete(ctx context.Context, id entity.Id, sb *compute.Sandbox) error {
	if sb == nil {
		return nil
	}
	return w.wake(ctx, runIDFor(sb))
}

func (w *SandboxWatchController) wake(ctx context.Context, runID entity.Id) error {
	if runID == "" || w.RunController == nil {
		return nil
	}

	resp, err := w.EAC.Get(ctx, runID.String())
	if err != nil {
		// The run is gone; its sandbox is now the GC's problem, not ours.
		w.Log.Debug("sandbox references a run that no longer exists", "run", runID, "error", err)
		return nil
	}

	w.RunController.Enqueue(controller.Event{
		Type:   controller.EventUpdated,
		Id:     runID,
		Entity: resp.Entity().Entity(),
	})
	return nil
}

// runIDFor recovers the run a sandbox belongs to.
//
// The back-reference rides in the sandbox's log attributes rather than a
// dedicated field, because it has to be there regardless: those labels are
// copied verbatim onto every log line, which is what makes `miren logs run`
// work without any change to the log pipeline. Reusing them here avoids a
// second source of truth that could disagree with the first.
func runIDFor(sb *compute.Sandbox) entity.Id {
	for _, l := range sb.Spec.LogAttribute {
		if l.Key == "miren.run" {
			return entity.Id(l.Value)
		}
	}
	return ""
}
