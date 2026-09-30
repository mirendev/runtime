package session

import (
	"context"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

type SandboxWatchController struct {
	Sessions *controller.ReconcileController
}

type TeardownWatchController struct {
	SandboxWatchController
}

func NewSandboxWatchController(sessions *controller.ReconcileController) *SandboxWatchController {
	return &SandboxWatchController{Sessions: sessions}
}

func (w *SandboxWatchController) Init(context.Context) error { return nil }
func (w *SandboxWatchController) Create(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) error {
	return w.Update(ctx, sb, meta)
}
func (w *SandboxWatchController) Update(_ context.Context, sb *compute.Sandbox, _ *entity.Meta) error {
	return w.wake(sb.SessionInfo.Owner)
}
func (w *SandboxWatchController) Delete(_ context.Context, _ entity.Id, sb *compute.Sandbox) error {
	if sb == nil {
		return nil
	}
	return w.wake(sb.SessionInfo.Owner)
}
func (w *SandboxWatchController) wake(id entity.Id) error {
	if id != "" && w.Sessions != nil {
		w.Sessions.Enqueue(controller.Event{Type: controller.EventUpdated, Id: id})
	}
	return nil
}

func (w *TeardownWatchController) Create(ctx context.Context, ack *compute.SandboxTeardown, _ *entity.Meta) error {
	return w.Update(ctx, ack, nil)
}

func (w *TeardownWatchController) Update(_ context.Context, ack *compute.SandboxTeardown, _ *entity.Meta) error {
	return w.wake(entity.Id(ack.Session))
}

func (w *TeardownWatchController) Delete(context.Context, entity.Id, *compute.SandboxTeardown) error {
	return nil
}
