package session

import (
	"context"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

type SandboxWatchController struct {
	Sessions *controller.ReconcileController
	EAC      *entityserver_v1alpha.EntityAccessClient
}

type TeardownWatchController struct {
	SandboxWatchController
}

func NewSandboxWatchController(sessions *controller.ReconcileController, eac *entityserver_v1alpha.EntityAccessClient) *SandboxWatchController {
	return &SandboxWatchController{Sessions: sessions, EAC: eac}
}

func (w *SandboxWatchController) Init(context.Context) error { return nil }
func (w *SandboxWatchController) Create(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) error {
	return w.Update(ctx, sb, meta)
}
func (w *SandboxWatchController) Update(ctx context.Context, sb *compute.Sandbox, _ *entity.Meta) error {
	return w.wakeSandbox(ctx, sb.ID, sb.SessionInfo.Owner)
}
func (w *SandboxWatchController) Delete(ctx context.Context, id entity.Id, sb *compute.Sandbox) error {
	if sb == nil {
		return nil
	}
	return w.wakeSandbox(ctx, id, sb.SessionInfo.Owner)
}
func (w *SandboxWatchController) wakeSandbox(ctx context.Context, sandbox entity.Id, owner entity.Id) error {
	if err := w.wake(owner); err != nil || w.EAC == nil || sandbox == "" {
		return err
	}
	bindings, err := w.EAC.List(ctx, entity.String(sessionapi.BindingSandboxId, sandbox.String()))
	if err != nil {
		return err
	}
	for _, value := range bindings.Values() {
		var binding sessionapi.Binding
		binding.Decode(value.Entity())
		if err := w.wake(entity.Id(binding.Session)); err != nil {
			return err
		}
	}
	return nil
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

func (w *TeardownWatchController) Update(ctx context.Context, ack *compute.SandboxTeardown, _ *entity.Meta) error {
	return w.wakeSandbox(ctx, entity.Id(ack.Sandbox), entity.Id(ack.Session))
}

func (w *TeardownWatchController) Delete(context.Context, entity.Id, *compute.SandboxTeardown) error {
	return nil
}
