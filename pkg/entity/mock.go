package entity

import (
	"context"
	"sync"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/entity/types"
)

type MockStore struct {
	mu              sync.RWMutex
	Entities        map[Id]*Entity
	OnWatchIndex    func(ctx context.Context, attr Attr) (clientv3.WatchChan, error)
	GetEntitiesFunc func(ctx context.Context, ids []Id) ([]*Entity, error)
	OnListIndex     func(ctx context.Context, attr Attr) ([]Id, error) // Hook to track ListIndex calls
}

var _ Store = &MockStore{}

func NewMockStore() *MockStore {
	return &MockStore{
		Entities: make(map[Id]*Entity),
	}
}

func (m *MockStore) GetEntity(ctx context.Context, id Id) (*Entity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.Entities[id]; ok {
		return e, nil
	}
	return nil, ErrNotFound
}

// AddEntity is a thread-safe helper to directly add an entity to the mock store
func (m *MockStore) AddEntity(id Id, entity *Entity) {
	m.mu.Lock()
	defer m.mu.Unlock()
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

func (m *MockStore) CreateEntity(ctx context.Context, attrs []Attr, opts ...EntityOption) (*Entity, error) {
	// Find the ident attribute
	var ident types.Keyword
	for _, attr := range attrs {
		if attr.ID == Ident {
			ident = attr.Value.Keyword()
			break
		}
	}

	e := &Entity{
		ID:       Id(ident),
		Attrs:    attrs,
		Revision: 1,
	}
	m.mu.Lock()
	m.Entities[e.ID] = e
	m.mu.Unlock()
	return e, nil
}

func (m *MockStore) UpdateEntity(ctx context.Context, id Id, attrs []Attr, opts ...EntityOption) (*Entity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.Entities[id]
	if !ok {
		return nil, ErrNotFound
	}

	// Create a copy to avoid modifying the original
	updated := &Entity{
		ID:        e.ID,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
		Revision:  e.Revision + 1,
		Attrs:     make([]Attr, 0, len(e.Attrs)),
	}

	// First, copy over existing attributes that aren't being updated
	attrMap := make(map[Id]Attr)
	for _, attr := range attrs {
		attrMap[attr.ID] = attr
	}

	for _, existing := range e.Attrs {
		if _, isUpdated := attrMap[existing.ID]; !isUpdated {
			updated.Attrs = append(updated.Attrs, existing)
		}
	}

	// Then add the new/updated attributes
	updated.Attrs = append(updated.Attrs, attrs...)
	updated.UpdatedAt = time.Now().Unix()

	// Update the entity in the store
	m.Entities[id] = updated

	return updated, nil
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
	m.Entities[Id("/mock/entity")] = &Entity{
		Attrs: []Attr{
			Keyword(Ident, "mock/entity"),
		},
	}
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

// WatchEntity
func (m *MockStore) WatchEntity(ctx context.Context, id Id) (chan EntityOp, error) {
	return nil, nil
}

func (m *MockStore) ListIndex(ctx context.Context, attr Attr) ([]Id, error) {
	// Call hook if provided
	if m.OnListIndex != nil {
		return m.OnListIndex(ctx, attr)
	}

	// Default implementation: Filter entities by the given attribute
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []Id
	for id, entity := range m.Entities {
		for _, a := range entity.Attrs {
			if a.ID == attr.ID && a.Value.Equal(attr.Value) {
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
