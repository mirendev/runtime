package entity

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/cond"
)

type MockStore struct {
	mu              sync.RWMutex
	Entities        map[Id]*Entity
	deletedEntities map[Id]*Entity // Tracks recently deleted entities for revision-based reads
	OnWatchIndex    func(ctx context.Context, attr Attr) (clientv3.WatchChan, error)
	GetEntitiesFunc func(ctx context.Context, ids []Id) ([]*Entity, error)
	OnListIndex     func(ctx context.Context, attr Attr) ([]Id, error) // Hook to track ListIndex calls

	NowFunc func() time.Time // Optional function to override current time

	// Entity watchers - maps entity ID to list of channels to notify
	watchersMu sync.RWMutex
	watchers   map[Id][]chan EntityOp

	// Index watchers - maps index key (attr.CAS()) to list of channels to notify
	indexWatchersMu sync.RWMutex
	indexWatchers   map[string][]chan clientv3.WatchResponse

	// WatchFromRevs records the fromRev argument of every WatchIndex call, in
	// order, so tests can assert resume behavior.
	WatchFromRevs []int64
}

var _ Store = &MockStore{}

func NewMockStore() *MockStore {
	return &MockStore{
		Entities:        make(map[Id]*Entity),
		deletedEntities: make(map[Id]*Entity),
		watchers:        make(map[Id][]chan EntityOp),
		indexWatchers:   make(map[string][]chan clientv3.WatchResponse),
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

func (m *MockStore) GetEntityAtRevision(ctx context.Context, id Id, rev int64) (*Entity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.Entities[id]; ok {
		return e, nil
	}
	if e, ok := m.deletedEntities[id]; ok {
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

// validateSessionAttrs checks that if any attributes are session-scoped,
// a session ID was provided via EntityOption. This matches EtcdStore behavior.
func (m *MockStore) validateSessionAttrs(ctx context.Context, attrs []Attr, opts []EntityOption) error {
	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	for _, attr := range attrs {
		schema, err := m.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			continue
		}
		if schema.Session {
			if len(o.session) == 0 {
				return fmt.Errorf("session ID is required for session attributes")
			}
			return nil
		}
	}
	return nil
}

// ensureShortIdLocked mirrors the real store's auto-allocation of db/short-id
// on kinded entities that don't already carry one. Without this, tests that
// resolve entities by their short id would have to inject one by hand.
//
// The caller must hold m.mu for writing so that candidate uniqueness and the
// subsequent insert are serialized; otherwise two concurrent CreateEntity
// calls could each see a candidate as free and both commit it.
func (m *MockStore) ensureShortIdLocked(entity *Entity) error {
	if _, hasKind := entity.Get(EntityKind); !hasKind {
		return nil
	}
	if _, hasShortId := entity.Get(DBShortId); hasShortId {
		return nil
	}
	shortId, err := AllocateShortId(string(entity.Id()), func(candidate string) (bool, error) {
		for _, ent := range m.Entities {
			if attr, ok := ent.Get(DBShortId); ok && attr.Value.String() == candidate {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to allocate short-id: %w", err)
	}
	entity.Set(String(DBShortId, shortId))
	return nil
}

func (m *MockStore) CreateEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error) {
	if err := m.validateSessionAttrs(ctx, entity.attrs, opts); err != nil {
		return nil, err
	}

	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	// Mirror EtcdStore.CreateEntity (store.go:171): allocate an ID before
	// storing. This also makes mock-backed tests fail loudly on a mistyped
	// db/id, the same way production does, instead of silently keying the
	// entity under an empty ID.
	entity.ForceID()

	// Set CreatedAt if not already set (store manages this timestamp)
	if entity.GetCreatedAt().IsZero() {
		entity.SetCreatedAt(m.Now())
	}
	entity.SetUpdatedAt(m.Now())
	entity.SetRevision(1)

	m.mu.Lock()
	// Mirror EtcdStore.CreateEntity (store.go:281-326): create is put-if-absent.
	// A create against an already-existing id is a conflict, not a silent
	// overwrite, unless WithOverwrite was passed. An idempotent re-create with
	// byte-identical attrs returns the existing entity. Without this, mock-backed
	// tests would diverge from production, which enforces uniqueness via an etcd
	// CreateRevision==0 transaction, masking bugs (e.g. duplicate runner_id
	// joins) that production actually rejects.
	if existing, ok := m.Entities[entity.Id()]; ok && !o.overwrite {
		if slices.EqualFunc(existing.attrs, entity.attrs, func(a, b Attr) bool { return a.Equal(b) }) {
			m.mu.Unlock()
			return existing, nil
		}
		m.mu.Unlock()
		return nil, cond.Conflict("entity", entity.Id())
	}
	if err := m.ensureShortIdLocked(entity); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.Entities[entity.Id()] = entity
	m.mu.Unlock()

	// Notify index watchers of the new entity
	go m.notifyIndexWatchers(entity, clientv3.EventTypePut, nil)

	return entity, nil
}

func (m *MockStore) UpdateEntity(ctx context.Context, id Id, entity *Entity, opts ...EntityOption) (*Entity, error) {
	if err := m.validateSessionAttrs(ctx, entity.attrs, opts); err != nil {
		return nil, err
	}

	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	// Determine which incoming attr IDs should replace existing values
	// (cardinality=one) vs accumulate alongside them (cardinality=many).
	// Mirrors EtcdStore.UpdateEntity (store.go:687) so mock-backed tests
	// observe the same merge semantics production does.
	replaceIds := make(map[Id]bool)
	for _, attr := range entity.attrs {
		schema, err := m.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get attribute schema: %w", err)
		}
		if !schema.AllowMany {
			replaceIds[attr.ID] = true
		}
	}

	m.mu.Lock()
	e, ok := m.Entities[id]
	if !ok {
		m.mu.Unlock()
		return nil, cond.NotFound("entity", id)
	}

	// Enforce optimistic concurrency control when the caller pins a revision.
	// Mirrors EtcdStore.UpdateEntity (store.go:669) so mock-backed tests observe
	// the same conflict semantics production does.
	if o.fromRevision != 0 && e.GetRevision() != o.fromRevision {
		m.mu.Unlock()
		return nil, cond.Conflict("entity", id)
	}

	// Keep existing attrs except those being replaced (cardinality=one IDs
	// present in the incoming change). Cardinality=many attrs are preserved
	// so the new incoming values append to the existing set.
	combinedAttrs := make([]Attr, 0, len(e.attrs)+len(entity.attrs))
	for _, existing := range e.attrs {
		if !replaceIds[existing.ID] {
			combinedAttrs = append(combinedAttrs, existing)
		}
	}

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
	prevEntity := e
	m.Entities[id] = updated
	m.mu.Unlock()

	// Notify watchers
	go m.notifyWatchers(id, EntityOp{Type: EntityOpUpdate, Entity: updated})
	go m.notifyIndexWatchers(updated, clientv3.EventTypePut, prevEntity)

	return updated, nil
}

func (m *MockStore) ReplaceEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error) {
	id := entity.Id()
	if id == "" {
		return nil, cond.NotFound("entity", "empty id")
	}

	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	m.mu.Lock()
	existing, ok := m.Entities[id]
	if !ok {
		m.mu.Unlock()
		return nil, cond.NotFound("entity", id)
	}

	// Enforce optimistic concurrency control when the caller pins a revision, so
	// a stale-revision Replace fails loudly with a conflict instead of silently
	// succeeding. Mirrors EtcdStore.ReplaceEntity (store.go:1039). A pinned
	// revision that matches the current one (the common CreateOrReplace /
	// SetInitialEnvVars case) still succeeds.
	if o.fromRevision != 0 && existing.GetRevision() != o.fromRevision {
		m.mu.Unlock()
		return nil, cond.Conflict("entity", id)
	}

	// Update revision and timestamp
	entity.SetRevision(existing.GetRevision() + 1)
	entity.SetUpdatedAt(m.Now())
	// Preserve CreatedAt from existing entity
	if !existing.GetCreatedAt().IsZero() {
		entity.SetCreatedAt(existing.GetCreatedAt())
	}

	prevEntity := existing
	m.Entities[id] = entity
	m.mu.Unlock()

	// Notify watchers
	go m.notifyWatchers(id, EntityOp{Type: EntityOpUpdate, Entity: entity})
	go m.notifyIndexWatchers(entity, clientv3.EventTypePut, prevEntity)

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
	entity, existed := m.Entities[id]
	delete(m.Entities, id)
	if existed {
		m.deletedEntities[id] = entity
	}
	m.mu.Unlock()

	if existed {
		go m.notifyWatchers(id, EntityOp{Type: EntityOpDelete, Entity: entity})
		go m.notifyIndexWatchers(entity, clientv3.EventTypeDelete, entity)
	}

	return nil
}

// WatchFromRevsCopy returns a snapshot of the fromRev arguments seen by
// WatchIndex, for tests asserting resume behavior.
func (m *MockStore) WatchFromRevsCopy() []int64 {
	m.indexWatchersMu.Lock()
	defer m.indexWatchersMu.Unlock()
	return append([]int64(nil), m.WatchFromRevs...)
}

func (m *MockStore) WatchIndex(ctx context.Context, attr Attr, fromRev int64) (clientv3.WatchChan, error) {
	m.indexWatchersMu.Lock()
	m.WatchFromRevs = append(m.WatchFromRevs, fromRev)
	m.indexWatchersMu.Unlock()

	if m.OnWatchIndex != nil {
		return m.OnWatchIndex(ctx, attr)
	}

	ch := make(chan clientv3.WatchResponse, 10)
	indexKey := attr.CAS()

	m.indexWatchersMu.Lock()
	m.indexWatchers[indexKey] = append(m.indexWatchers[indexKey], ch)
	m.indexWatchersMu.Unlock()

	// Clean up watcher when context is cancelled
	go func() {
		<-ctx.Done()
		m.indexWatchersMu.Lock()
		defer m.indexWatchersMu.Unlock()
		watchers := m.indexWatchers[indexKey]
		for i, w := range watchers {
			if w == ch {
				m.indexWatchers[indexKey] = append(watchers[:i], watchers[i+1:]...)
				break
			}
		}
		close(ch)
	}()

	return ch, nil
}

// WaitForEntityWatcher blocks until at least one watcher is registered for the given entity ID,
// or the context is cancelled.
func (m *MockStore) WaitForEntityWatcher(ctx context.Context, id Id) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.watchersMu.RLock()
			hasWatcher := len(m.watchers[id]) > 0
			m.watchersMu.RUnlock()
			if hasWatcher {
				return nil
			}
		}
	}
}

// WaitForIndexWatcher blocks until at least one watcher is registered for the given attribute,
// or the context is cancelled. This is useful in tests to ensure a watch is established
// before performing operations that should trigger watch notifications.
func (m *MockStore) WaitForIndexWatcher(ctx context.Context, attr Attr) error {
	indexKey := attr.CAS()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.indexWatchersMu.RLock()
			hasWatcher := len(m.indexWatchers[indexKey]) > 0
			m.indexWatchersMu.RUnlock()
			if hasWatcher {
				return nil
			}
		}
	}
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

// notifyIndexWatchers sends a watch response to all index watchers that match the entity's attributes.
// eventType should be clientv3.EventTypePut for create/update or clientv3.EventTypeDelete for delete.
// For delete events, prevEntity should be the entity before deletion (to get its ID).
func (m *MockStore) notifyIndexWatchers(entity *Entity, eventType mvccpb.Event_EventType, prevEntity *Entity) {
	m.indexWatchersMu.RLock()
	defer m.indexWatchersMu.RUnlock()

	// Check each registered index watcher to see if this entity matches
	allAttrs := enumerateAllAttrs(entity.attrs)

	for indexKey, watchers := range m.indexWatchers {
		// Check if any of the entity's attributes produce this index key
		for _, attr := range allAttrs {
			if attr.CAS() == indexKey {
				// Entity matches this index - notify all watchers
				event := &clientv3.Event{
					Type: eventType,
					Kv: &mvccpb.KeyValue{
						Key:   []byte(indexKey),
						Value: []byte(entity.Id()),
					},
				}
				if prevEntity != nil {
					event.PrevKv = &mvccpb.KeyValue{
						Key:         []byte(indexKey),
						Value:       []byte(prevEntity.Id()),
						ModRevision: prevEntity.GetRevision(),
					}
				}

				resp := clientv3.WatchResponse{
					Events: []*clientv3.Event{event},
				}

				for _, ch := range watchers {
					select {
					case ch <- resp:
					default:
						// Channel full, skip
					}
				}
				break // Only notify once per index key
			}
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

// ListIndexRevision returns the matching ids along with a revision. The mock
// uses the highest entity revision currently in the store as a monotonic proxy
// for the cluster revision, which is sufficient for resume-cursor tests.
func (m *MockStore) ListIndexRevision(ctx context.Context, attr Attr) ([]Id, int64, error) {
	ids, err := m.ListIndex(ctx, attr)
	if err != nil {
		return nil, 0, err
	}

	m.mu.RLock()
	var rev int64
	for _, e := range m.Entities {
		if r := e.GetRevision(); r > rev {
			rev = r
		}
	}
	m.mu.RUnlock()

	return ids, rev, nil
}

// ListIndexPage pages the mock's index by sorting the ids and slicing. The real
// store gets its ordering from etcd's keyspace; the mock has no keyspace, so it
// imposes id order to give the cursor something stable to resume from.
func (m *MockStore) ListIndexPage(ctx context.Context, attr Attr, cursor string, limit int64) (*IndexPage, error) {
	ids, rev, err := m.ListIndexRevision(ctx, attr)
	if err != nil {
		return nil, err
	}

	slices.Sort(ids)

	page := &IndexPage{Revision: rev}

	if cursor == "" {
		page.Total = int64(len(ids))
	} else {
		for len(ids) > 0 && string(ids[0]) <= cursor {
			ids = ids[1:]
		}
	}

	if limit > 0 && int64(len(ids)) > limit {
		page.Cursor = string(ids[limit-1])
		ids = ids[:limit]
	}

	page.Ids = ids

	return page, nil
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
	// Look up the schema entity from the store, just like EtcdStore does.
	// Schema entities are created by schema.Apply during test setup.
	m.mu.RLock()
	entity, ok := m.Entities[id]
	m.mu.RUnlock()

	if ok {
		schema, err := convertEntityToSchema(ctx, m, entity)
		if err == nil {
			return schema, nil
		}
	}

	return &AttributeSchema{ID: id}, nil
}
