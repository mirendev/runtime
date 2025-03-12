package entity

import (
	"context"
	"fmt"

	"miren.dev/runtime/pkg/controller"
)

// Ensure Entity implements controller.Entity
var _ controller.Entity = (*Entity)(nil)

// GetID implements controller.Entity
func (e *Entity) GetID() string {
	return e.ID
}

// GetKind implements controller.Entity
func (e *Entity) GetKind() string {
	return ""
}

// GetVersion implements controller.Entity
func (e *Entity) GetVersion() string {
	return fmt.Sprintf("%d", e.Revision)
}

// EntityStoreAdapter adapts entity.Store to controller.EntityStore
type EntityStoreAdapter struct {
	store *FileStore
}

func NewEntityStoreAdapter(store *FileStore) *EntityStoreAdapter {
	return &EntityStoreAdapter{store: store}
}

// Create implements controller.EntityStore
func (a *EntityStoreAdapter) Create(ctx context.Context, e controller.Entity) error {
	entity, ok := e.(*Entity)
	if !ok {
		return fmt.Errorf("expected *entity.Entity, got %T", e)
	}

	_, err := a.store.CreateEntity(entity.Attrs)
	return err
}

// Update implements controller.EntityStore
func (a *EntityStoreAdapter) Update(ctx context.Context, e controller.Entity) error {
	entity, ok := e.(*Entity)
	if !ok {
		return fmt.Errorf("expected *entity.Entity, got %T", e)
	}

	_, err := a.store.UpdateEntity(EntityId(entity.ID), entity.Attrs)
	return err
}

// Get implements controller.EntityStore
func (a *EntityStoreAdapter) Get(ctx context.Context, kind, id string) (controller.Entity, error) {
	return a.store.GetEntity(EntityId(id))
}

// Delete implements controller.EntityStore
func (a *EntityStoreAdapter) Delete(ctx context.Context, kind, id string) error {
	return a.store.DeleteEntity(EntityId(id))
}

// List implements controller.EntityStore
func (a *EntityStoreAdapter) List(ctx context.Context, kind string) ([]controller.Entity, error) {
	// TODO: Implement listing by type
	return nil, fmt.Errorf("not implemented")
}

// Watch implements controller.EntityStore
func (a *EntityStoreAdapter) Watch(ctx context.Context, kind string) (<-chan controller.Event, error) {
	// TODO: Implement watching for changes
	return nil, fmt.Errorf("not implemented")
}

// EntityFactory creates new Entity instances
type EntityFactory struct {
	entityType string
}

func NewEntityFactory(entityType string) *EntityFactory {
	return &EntityFactory{entityType: entityType}
}

// Create implements controller.EntityFactory
func (f *EntityFactory) Create() controller.Entity {
	return &Entity{}
}
