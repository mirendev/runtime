package entityserver_v1alpha

import (
	"time"

	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

// Entity converts the wire entity to an entity.Entity. Stored entities carry
// their id, revision and timestamps alongside the attributes, but parsed ones
// (EntityAccess.Parse) carry only attributes, so only fields actually present
// are copied. Copying absent ones would hand a write path an empty db/id and a
// 1970 creation time that the store keeps, since it only stamps a zero one.
func (e *Entity) Entity() *entity.Entity {
	ent := entity.New(e.Attrs())

	if e.HasId() {
		ent.SetID(types.Id(e.Id()))
	}
	if e.HasCreatedAt() {
		ent.SetCreatedAt(time.UnixMilli(e.CreatedAt()))
	}
	if e.HasUpdatedAt() {
		ent.SetUpdatedAt(time.UnixMilli(e.UpdatedAt()))
	}
	if e.HasRevision() {
		ent.SetRevision(e.Revision())
	}
	return ent
}
