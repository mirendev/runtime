package entity

import (
	"context"
	"sync"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/cond"
)

type MockStore struct {
	mu              sync.RWMutex
	Entities        map[Id]*Entity
	OnWatchIndex    func(ctx context.Context, attr Attr) (clientv3.WatchChan, error)
	GetEntitiesFunc func(ctx context.Context, ids []Id) ([]*Entity, error)
	OnListIndex     func(ctx context.Context, attr Attr) ([]Id, error) // Hook to track ListIndex calls

	NowFunc func() time.Time // Optional function to override current time

	// Entity watchers - maps entity ID to list of channels to notify
	watchersMu sync.RWMutex
	watchers   map[Id][]chan EntityOp
}

var _ Store = &MockStore{}

func NewMockStore() *MockStore {
	return &MockStore{
		Entities: make(map[Id]*Entity),
		watchers: make(map[Id][]chan EntityOp),
	}
}

func (m *MockStore) Now() time.Time {
	if m.NowFunc != nil {
		return m.NowFunc()
	}
	return time.Now()
}

func (m *MockStore) GetEntity(ctx context.Context, id Id) (*Entity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.Entities[id]; ok {
		return e, nil
	}
	return nil, cond.NotFound("entity", id)
}

// AddEntity is a thread-safe helper to directly add an entity to the mock store
func (m *MockStore) AddEntity(id Id, entity *Entity) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entity.Fixup()
	m.Entities[id] = entity
}

// RemoveEntity is a thread-safe helper to directly remove an entity from the mock store
func (m *MockStore) RemoveEntity(id Id) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Entities, id)
}

func (m *MockStore) GetEntities(ctx context.Context, ids []Id) ([]*Entity, error) {
	if m.GetEntitiesFunc != nil {
		return m.GetEntitiesFunc(ctx, ids)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	entities := make([]*Entity, 0, len(ids))
	for _, id := range ids {
		if e, ok := m.Entities[id]; ok {
			entities = append(entities, e)
		} else {
			entities = append(entities, nil)
		}
	}
	return entities, nil
}

func (m *MockStore) CreateEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error) {
	// Set CreatedAt if not already set (store manages this timestamp)
	if entity.GetCreatedAt().IsZero() {
		entity.SetCreatedAt(m.Now())
	}
	entity.SetUpdatedAt(m.Now())
	entity.SetRevision(1)

	m.mu.Lock()
	m.Entities[entity.Id()] = entity
	m.mu.Unlock()
	return entity, nil
}

func (m *MockStore) UpdateEntity(ctx context.Context, id Id, entity *Entity, opts ...EntityOption) (*Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.Entities[id]
	if !ok {
		return nil, cond.NotFound("entity", id)
	}

	// Build combined attribute list
	combinedAttrs := make([]Attr, 0, len(e.attrs))

	// First, copy over existing attributes that aren't being updated
	attrMap := make(map[Id]Attr)
	for _, attr := range entity.attrs {
		attrMap[attr.ID] = attr
	}

	for _, existing := range e.attrs {
		if _, isUpdated := attrMap[existing.ID]; !isUpdated {
			combinedAttrs = append(combinedAttrs, existing)
		}
	}

	// Then add the new/updated attributes
	combinedAttrs = append(combinedAttrs, entity.attrs...)

	// Create a copy to avoid modifying the original
	updated := New(combinedAttrs)

	updated.SetRevision(e.GetRevision() + 1)
	updated.SetUpdatedAt(m.Now())
	// Preserve CreatedAt from existing entity
	if !e.GetCreatedAt().IsZero() {
		updated.SetCreatedAt(e.GetCreatedAt())
	}

	// Update the entity in the store
	m.Entities[id] = updated

	// Notify watchers
	go m.notifyWatchers(id, EntityOp{Type: EntityOpUpdate, Entity: updated})

	return updated, nil
}

func (m *MockStore) ReplaceEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error) {
	id := entity.Id()
	if id == "" {
		return nil, cond.NotFound("entity", "empty id")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.Entities[id]
	if !ok {
		return nil, cond.NotFound("entity", id)
	}

	// Update revision and timestamp
	entity.SetRevision(existing.GetRevision() + 1)
	entity.SetUpdatedAt(m.Now())
	// Preserve CreatedAt from existing entity
	if !existing.GetCreatedAt().IsZero() {
		entity.SetCreatedAt(existing.GetCreatedAt())
	}

	m.Entities[id] = entity

	// Notify watchers
	go m.notifyWatchers(id, EntityOp{Type: EntityOpUpdate, Entity: entity})

	return entity, nil
}

func (m *MockStore) PatchEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error) {
	id := entity.Id()
	if id == "" {
		return nil, cond.NotFound("entity", "empty id")
	}

	// Use UpdateEntity logic
	return m.UpdateEntity(ctx, id, entity, opts...)
}

func (m *MockStore) EnsureEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, bool, error) {
	id := entity.Id()
	if id == "" {
		return nil, false, cond.NotFound("entity", "empty id")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if entity exists
	if e, ok := m.Entities[id]; ok {
		return e, false, nil
	}

	// Create new entity
	entity.SetRevision(1)
	entity.SetCreatedAt(m.Now())
	entity.SetUpdatedAt(m.Now())
	m.Entities[id] = entity
	return entity, true, nil
}

func (m *MockStore) DeleteEntity(ctx context.Context, id Id) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Entities, id)
	return nil
}

func (m *MockStore) WatchIndex(ctx context.Context, attr Attr) (clientv3.WatchChan, error) {
	if m.OnWatchIndex != nil {
		return m.OnWatchIndex(ctx, attr)
	}

	ch := make(chan clientv3.WatchResponse)

	m.mu.Lock()
	mockEntity := New(
		Ref(DBId, "mock/entity"),
		Keyword(Ident, "mock/entity"),
	)
	m.Entities[Id("/mock/entity")] = mockEntity
	m.mu.Unlock()

	go func() {
		// Simulate a watch event after some time
		// In a real implementation, this would listen to etcd events
		// and send them to the channel
		select {
		case <-ctx.Done():
			return
		default:
			// Send a mock event (this is just for demonstration)
			ch <- clientv3.WatchResponse{
				Events: []*clientv3.Event{
					{
						Type: clientv3.EventTypePut,
						Kv:   &mvccpb.KeyValue{Key: []byte("abcdef"), Value: []byte("/mock/entity")},
					},
				},
			}

			close(ch) // Close the channel after sending the event
		}
	}()

	// This is a mock, so we won't implement actual watch functionality
	return ch, nil
}

// WatchEntity registers a watcher for an entity and returns a channel that receives updates
func (m *MockStore) WatchEntity(ctx context.Context, id Id) (chan EntityOp, error) {
	ch := make(chan EntityOp, 10)

	m.watchersMu.Lock()
	m.watchers[id] = append(m.watchers[id], ch)
	m.watchersMu.Unlock()

	// Clean up watcher when context is cancelled
	go func() {
		<-ctx.Done()
		m.watchersMu.Lock()
		defer m.watchersMu.Unlock()
		watchers := m.watchers[id]
		for i, w := range watchers {
			if w == ch {
				m.watchers[id] = append(watchers[:i], watchers[i+1:]...)
				break
			}
		}
		close(ch)
	}()

	return ch, nil
}

// notifyWatchers sends an entity operation to all watchers of the given entity ID
func (m *MockStore) notifyWatchers(id Id, op EntityOp) {
	m.watchersMu.RLock()
	defer m.watchersMu.RUnlock()
	for _, ch := range m.watchers[id] {
		select {
		case ch <- op:
		default:
			// Channel full, skip
		}
	}
}

func (m *MockStore) ListIndex(ctx context.Context, attr Attr) ([]Id, error) {
	// Call hook if provided
	if m.OnListIndex != nil {
		return m.OnListIndex(ctx, attr)
	}

	// Default implementation: Filter entities by the given attribute
	// Recursively enumerate attributes including nested ones in components
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []Id
	for id, entity := range m.Entities {
		allAttrs := enumerateAllAttrs(entity.attrs)
		for _, a := range allAttrs {
			if a.ID == attr.ID && a.Value.Equal(attr.Value) {
				ids = append(ids, id)
				break
			}
		}
	}

	return ids, nil
}

func (m *MockStore) ListCollection(ctx context.Context, collection string) ([]Id, error) {
	// For the mock store, we use the same logic as ListIndex
	// since we don't have a separate collection index structure.
	// In practice, ListCollection is used by ListIndex in real stores.
	// For testing purposes, we'll just iterate through all entities
	// and check if any attribute CAS matches the collection string.
	m.mu.RLock()
	defer m.mu.RUnlock()

	var ids []Id
	for id, entity := range m.Entities {
		allAttrs := enumerateAllAttrs(entity.attrs)
		for _, a := range allAttrs {
			if a.CAS() == collection {
				ids = append(ids, id)
				break
			}
		}
	}

	return ids, nil
}

func (m *MockStore) CreateSession(ctx context.Context, id int64) ([]byte, error) {
	return []byte("mock-session-id"), nil
}

// ListSessionEntities
func (m *MockStore) ListSessionEntities(ctx context.Context, id []byte) ([]Id, error) {
	// For simplicity, return all entities as a list
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []Id
	for eid := range m.Entities {
		ids = append(ids, eid)
	}
	return ids, nil
}

// PingSession
func (m *MockStore) PingSession(ctx context.Context, id []byte) error {
	return nil
}

// RevokeSession
func (m *MockStore) RevokeSession(ctx context.Context, id []byte) error {
	return nil
}

func (m *MockStore) GetAttributeSchema(ctx context.Context, id Id) (*AttributeSchema, error) {
	// For simplicity, return a mock schema
	return &AttributeSchema{
		ID: id,
	}, nil
}
