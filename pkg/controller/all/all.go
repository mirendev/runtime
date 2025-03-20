package all

import (
	"time"

	"miren.dev/runtime/controllers/sandbox"
	"miren.dev/runtime/pkg/asm"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
)

func All(reg *asm.Registry) (*controller.ControllerManager, error) {
	cm := controller.NewControllerManager()

	var co sandbox.SandboxController
	if err := reg.Populate(&co); err != nil {
		return nil, err
	}

	var store *entity.EtcdStore
	if err := reg.Populate(&store); err != nil {
		return nil, err
	}

	cm.AddController(
		controller.NewReconcileController(
			"sandbox",
			entity.Keyword(entity.EntityKind, "sandbox"),
			store,
			controller.AdaptController(&co, store),
			time.Minute,
			1,
		),
	)

	return cm, nil
}
