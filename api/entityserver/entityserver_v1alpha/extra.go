package entityserver_v1alpha

import (
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

func (e *Entity) Entity() *entity.Entity {
	return &entity.Entity{
		ID:    types.Id(e.Id()),
		Attrs: e.Attrs(),
	}
}
