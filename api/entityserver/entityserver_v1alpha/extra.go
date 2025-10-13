package entityserver_v1alpha

import (
	"time"

	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

func (e *Entity) Entity() *entity.Entity {
	createdAt := time.UnixMilli(e.CreatedAt())
	updatedAt := time.UnixMilli(e.UpdatedAt())

	ent, err := entity.NewEntity(e.Attrs())
	if err != nil {
		panic(err)
	}

	ent.SetID(types.Id(e.Id()))

	ent.SetCreatedAt(createdAt)
	ent.SetUpdatedAt(updatedAt)
	ent.SetRevision(e.Revision())
	return ent
}
