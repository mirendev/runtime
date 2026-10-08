package entity

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
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

	// rev is the store-wide revision, advanced by every write and stamped onto
	// the written entity the way etcd stamps ModRevision. It is the axis that
	// ListIndexRevision reports and WatchIndex resumes along, so a cursor taken
	// from a list is directly comparable to the revision of a later event.
	// Guarded by mu.
	rev int64

	// Index watchers - maps index key (attr.CAS()) to list of channels to notify
	indexWatchersMu sync.RWMutex
	indexWatchers   map[string][]chan clientv3.WatchResponse

	// indexLog records every index event in commit order so WatchIndex can
	// replay from a revision. The mock never compacts it. Guarded by
	// indexWatchersMu, as is indexEntryCreated, which remembers the revision
	// each (index, entity) entry first appeared at so a put can be reported as
	// a create or a modify the way etcd's CreateRevision does.
	indexLog          []indexLogEntry
	indexEntryCreated map[string]int64

	// WatchFromRevs records the fromRev argument of every WatchIndex call, in
	// order, so tests can assert resume behavior.
	WatchFromRevs []int64

	// staleIndexEntries holds fault-injected index entries, keyed by attr.CAS().
	// See AddStaleIndexEntry.
	staleIndexEntries map[string][]Id

	// Sessions model etcd leases. A live session owns the attribute blob each
	// write under it stores, and the entities bound to it; revoking it drops
	// both, so the entity, and every index match a session value gave it, goes
	// with the session. Entities holds the merged view GetEntity returns.
	// Guarded by mu.
	sessionSeq   int64
	sessions     map[string]bool
	sessionBlobs map[Id]map[string][]Attr
	boundTo      map[Id]string
}

var _ Store = &MockStore{}

// indexLogEntry is one index event as WatchIndex delivers it, tagged with the
// revision it was committed at and the index it belongs to.
type indexLogEntry struct {
	rev      int64
	indexKey string
	resp     clientv3.WatchResponse
}

// indexWatchBuffer is the slack a WatchIndex channel has for live events beyond
// its replayed backlog. Delivery is non-blocking, so a consumer that falls
// this far behind loses events, the same as before replay existed.
const indexWatchBuffer = 10

func NewMockStore() *MockStore {
	return &MockStore{
		Entities:          make(map[Id]*Entity),
		deletedEntities:   make(map[Id]*Entity),
		watchers:          make(map[Id][]chan EntityOp),
		indexWatchers:     make(map[string][]chan clientv3.WatchResponse),
		indexEntryCreated: make(map[string]int64),
		sessions:          make(map[string]bool),
		sessionBlobs:      make(map[Id]map[string][]Attr),
		boundTo:           make(map[Id]string),
	}
}

func (m *MockStore) SourceEpoch(context.Context) (string, error) {
	return "mock-source-epoch", nil
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

// AddEntity is a thread-safe helper to directly add an entity to the mock store.
// It is fixture setup rather than a write: no revision is assigned and no watch
// event is produced. A fixture that carries its own revision moves the store
// head up to it so a watch resumed from that head starts past the fixture.
func (m *MockStore) AddEntity(id Id, entity *Entity) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entity.Fixup()
	m.Entities[id] = entity
	if rev := entity.GetRevision(); rev > m.rev {
		m.rev = rev
	}

	// The fixture's index entries exist from here on, so a later write to the
	// entity must read as a modify of them rather than a create. Record them
	// at the fixture's own revision, which is below any revision a write can
	// be stamped with.
	m.indexWatchersMu.Lock()
	defer m.indexWatchersMu.Unlock()
	for _, attr := range enumerateAllAttrs(entity.attrs) {
		key := indexEntryKey(attr.CAS(), entity.Id())
		if _, ok := m.indexEntryCreated[key]; !ok {
			m.indexEntryCreated[key] = entity.GetRevision()
		}
	}
}

// RemoveEntity is a thread-safe helper to directly remove an entity from the mock store
func (m *MockStore) RemoveEntity(id Id) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entity, ok := m.Entities[id]
	delete(m.Entities, id)
	if !ok {
		return
	}

	m.indexWatchersMu.Lock()
	defer m.indexWatchersMu.Unlock()
	for _, attr := range enumerateAllAttrs(entity.attrs) {
		delete(m.indexEntryCreated, indexEntryKey(attr.CAS(), entity.Id()))
	}
}

// indexEntryKey names one (index, entity) entry in indexEntryCreated.
func indexEntryKey(indexKey string, id Id) string {
	return indexKey + "\x00" + string(id)
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

// ListIndexEntitiesPage composes the mock's own paging and batch read.
//
// It reports nothing undecodable and ignores the revision when resolving
// entities: the mock keeps one version of each entity, so there is no history
// to read at and nothing stored that could fail to decode. It cannot prove a
// caller pinned the right revision, which is what the EtcdStore conformance
// case is for. What it can still check is that a caller handles nils, indexes
// the map without a nil check, and walks the cursor to the end.
func (m *MockStore) ListIndexEntitiesPage(
	ctx context.Context,
	attr Attr,
	cursor string,
	limit int64,
) (*EntityPage, error) {
	page, err := m.ListIndexPage(ctx, attr, cursor, limit)
	if err != nil {
		return nil, err
	}

	entities, err := m.GetEntities(ctx, page.Ids)
	if err != nil {
		return nil, err
	}

	return &EntityPage{
		Ids:         page.Ids,
		Entities:    entities,
		Undecodable: map[Id]bool{},
		Cursor:      page.Cursor,
		Total:       page.Total,
		Revision:    page.Revision,
	}, nil
}

// validateSessionAttrs checks that if any attributes are session-scoped,
// a session ID was provided via EntityOption, and that no attribute definition
// is both indexed and session-scoped. This matches EtcdStore behavior.
func (m *MockStore) validateSessionAttrs(ctx context.Context, attrs []Attr, opts []EntityOption) error {
	if err := rejectIndexedSession(attrs); err != nil {
		return err
	}

	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	if len(o.session) == 0 && len(m.sessionAttrIDs(ctx, attrs)) > 0 {
		return fmt.Errorf("session ID is required for session attributes")
	}
	return nil
}

// sessionAttrIDs returns the IDs among attrs whose schema is session-scoped.
// Schema lookups read the store, so this runs before a write takes m.mu.
func (m *MockStore) sessionAttrIDs(ctx context.Context, attrs ...[]Attr) map[Id]bool {
	ids := make(map[Id]bool)
	for _, set := range attrs {
		for _, attr := range set {
			schema, err := m.GetAttributeSchema(ctx, attr.ID)
			if err == nil && schema.Session {
				ids[attr.ID] = true
			}
		}
	}
	return ids
}

// checkSessionLocked fails a write under a session that is not live, the way
// etcd refuses a put carrying an unknown lease.
func (m *MockStore) checkSessionLocked(o *entityOpts) error {
	if len(o.session) != 0 && !m.sessions[string(o.session)] {
		return rpctypes.ErrLeaseNotFound
	}
	return nil
}

// recordSessionWriteLocked mirrors what an EtcdStore write does to session
// state once view, the entity as written, is stored. The writing session's
// blob is rewritten with every session attribute the entity carries, and the
// entity key is leased to the session only when the write binds it, so a later
// unbound write makes the entity durable again. Session attributes written
// without a session (fixtures) are left as plain attributes.
func (m *MockStore) recordSessionWriteLocked(view *Entity, o *entityOpts, sessionIDs map[Id]bool) {
	id := view.Id()
	tok := string(o.session)

	if tok != "" {
		var blob []Attr
		for _, attr := range view.attrs {
			if sessionIDs[attr.ID] {
				blob = append(blob, attr)
			}
		}
		if len(blob) > 0 {
			if m.sessionBlobs[id] == nil {
				m.sessionBlobs[id] = make(map[string][]Attr)
			}
			m.sessionBlobs[id][tok] = blob
		}
	}

	if o.bind {
		m.boundTo[id] = tok
	} else {
		delete(m.boundTo, id)
	}
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

	sessionIDs := m.sessionAttrIDs(ctx, entity.attrs)

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkSessionLocked(&o); err != nil {
		return nil, err
	}
	// Mirror EtcdStore.CreateEntity (store.go:281-326): create is put-if-absent.
	// A create against an already-existing id is a conflict, not a silent
	// overwrite, unless WithOverwrite was passed. An idempotent re-create with
	// byte-identical attrs returns the existing entity. Without this, mock-backed
	// tests would diverge from production, which enforces uniqueness via an etcd
	// CreateRevision==0 transaction, masking bugs (e.g. duplicate runner_id
	// joins) that production actually rejects.
	existing := m.Entities[entity.Id()]
	if existing != nil && !o.overwrite {
		// The revision is the store's to assign, so it is not part of what
		// makes a re-create identical.
		entity.SetRevision(existing.GetRevision())
		if slices.EqualFunc(existing.attrs, entity.attrs, func(a, b Attr) bool { return a.Equal(b) }) {
			return existing, nil
		}
		return nil, cond.Conflict("entity", entity.Id())
	}
	if err := m.ensureShortIdLocked(entity); err != nil {
		return nil, err
	}
	m.Entities[entity.Id()] = entity
	m.recordSessionWriteLocked(entity, &o, sessionIDs)
	m.commitLocked(entity, clientv3.EventTypePut, existing)

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

	// The written entity's session attributes include ones it already
	// carried, so classify those too, ahead of taking the lock.
	m.mu.RLock()
	var existingAttrs []Attr
	if e, ok := m.Entities[id]; ok {
		existingAttrs = e.attrs
	}
	m.mu.RUnlock()
	sessionIDs := m.sessionAttrIDs(ctx, entity.attrs, existingAttrs)

	m.mu.Lock()
	if err := m.checkSessionLocked(&o); err != nil {
		m.mu.Unlock()
		return nil, err
	}
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
	// Capacity covers the attrs retained by the loop below; the trailing append
	// grows once more for the incoming ones. Kept as a single length rather
	// than the sum so the allocation size is not a computed value.
	combinedAttrs := make([]Attr, 0, len(e.attrs))
	for _, existing := range e.attrs {
		if !replaceIds[existing.ID] {
			combinedAttrs = append(combinedAttrs, existing)
		}
	}

	combinedAttrs = append(combinedAttrs, entity.attrs...)

	// Create a copy to avoid modifying the original
	updated := New(combinedAttrs)

	updated.SetUpdatedAt(m.Now())
	// Preserve CreatedAt from existing entity
	if !e.GetCreatedAt().IsZero() {
		updated.SetCreatedAt(e.GetCreatedAt())
	}
	if updated.GetCreatedAt().IsZero() {
		createdAt := e.GetUpdatedAt()
		if createdAt.IsZero() {
			createdAt = m.Now()
		}
		updated.SetCreatedAt(createdAt)
	}

	// Update the entity in the store
	m.Entities[id] = updated
	m.recordSessionWriteLocked(updated, &o, sessionIDs)
	m.commitLocked(updated, clientv3.EventTypePut, e)
	m.mu.Unlock()

	go m.notifyWatchers(id, EntityOp{Type: EntityOpUpdate, Entity: updated})

	return updated, nil
}

func (m *MockStore) ReplaceEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error) {
	id := entity.Id()
	if id == "" {
		return nil, cond.NotFound("entity", "empty id")
	}

	if err := m.validateSessionAttrs(ctx, entity.attrs, opts); err != nil {
		return nil, err
	}

	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	sessionIDs := m.sessionAttrIDs(ctx, entity.attrs)

	m.mu.Lock()
	if err := m.checkSessionLocked(&o); err != nil {
		m.mu.Unlock()
		return nil, err
	}
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

	entity.SetUpdatedAt(m.Now())
	// Preserve CreatedAt from existing entity
	if !existing.GetCreatedAt().IsZero() {
		entity.SetCreatedAt(existing.GetCreatedAt())
	}
	if entity.GetCreatedAt().IsZero() {
		createdAt := existing.GetUpdatedAt()
		if createdAt.IsZero() {
			createdAt = m.Now()
		}
		entity.SetCreatedAt(createdAt)
	}

	// A replace rewrites the entity key and at most its own session's blob,
	// which it fills from the replacement alone, so record that before any
	// other session's attributes join the view. Every blob the write left
	// alone, the writer's own included when the replacement carries no
	// session attributes, still holds its attributes, so they stay in the
	// view the way GetEntity would merge them back in.
	m.recordSessionWriteLocked(entity, &o, sessionIDs)
	rewrote := len(o.session) != 0 && slices.ContainsFunc(entity.attrs, func(a Attr) bool { return sessionIDs[a.ID] })
	for _, tok := range slices.Sorted(maps.Keys(m.sessionBlobs[id])) {
		if rewrote && tok == string(o.session) {
			continue
		}
		entity.attrs = append(entity.attrs, m.sessionBlobs[id][tok]...)
	}

	m.Entities[id] = entity
	m.commitLocked(entity, clientv3.EventTypePut, existing)
	m.mu.Unlock()

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

	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	sessionIDs := m.sessionAttrIDs(ctx, entity.attrs)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if entity exists
	if e, ok := m.Entities[id]; ok {
		return e, false, nil
	}

	if err := m.checkSessionLocked(&o); err != nil {
		return nil, false, err
	}

	// Create new entity
	if err := m.ensureShortIdLocked(entity); err != nil {
		return nil, false, err
	}
	entity.SetCreatedAt(m.Now())
	entity.SetUpdatedAt(m.Now())
	m.Entities[id] = entity
	m.recordSessionWriteLocked(entity, &o, sessionIDs)
	m.commitLocked(entity, clientv3.EventTypePut, nil)
	return entity, true, nil
}

func (m *MockStore) DeleteEntity(ctx context.Context, id Id) error {
	m.mu.Lock()
	entity, existed := m.Entities[id]
	m.deleteLocked(id)
	m.mu.Unlock()

	if existed {
		go m.notifyWatchers(id, EntityOp{Type: EntityOpDelete, Entity: entity})
	}

	return nil
}

// deleteLocked removes an entity along with its session state. The caller
// holds m.mu.
func (m *MockStore) deleteLocked(id Id) {
	entity, existed := m.Entities[id]
	delete(m.Entities, id)
	delete(m.sessionBlobs, id)
	delete(m.boundTo, id)
	if existed {
		m.deletedEntities[id] = entity
		m.commitLocked(entity, clientv3.EventTypeDelete, entity)
	}
}

// WatchFromRevsCopy returns a snapshot of the fromRev arguments seen by
// WatchIndex, for tests asserting resume behavior.
func (m *MockStore) WatchFromRevsCopy() []int64 {
	m.indexWatchersMu.Lock()
	defer m.indexWatchersMu.Unlock()
	return append([]int64(nil), m.WatchFromRevs...)
}

// WatchIndex registers a watcher for the index. With fromRev > 0 it first
// replays every event committed at or after that revision, the way etcd's
// WithRev does, so a write landing between a caller's List and its WatchIndex
// is resumed rather than lost. Consumers built on indexwatch.Watcher depend on
// that for gap-free delivery. With fromRev == 0 it delivers changes from this
// moment on only.
//
// Replay and registration happen under one lock, so a write can never fall
// between them. The mock never compacts, so any fromRev is resumable.
func (m *MockStore) WatchIndex(ctx context.Context, attr Attr, fromRev int64) (clientv3.WatchChan, error) {
	m.indexWatchersMu.Lock()
	m.WatchFromRevs = append(m.WatchFromRevs, fromRev)
	m.indexWatchersMu.Unlock()

	if m.OnWatchIndex != nil {
		return m.OnWatchIndex(ctx, attr)
	}

	indexKey := attr.CAS()

	m.indexWatchersMu.Lock()
	var backlog []clientv3.WatchResponse
	if fromRev > 0 {
		for _, entry := range m.indexLog {
			if entry.rev >= fromRev && entry.indexKey == indexKey {
				backlog = append(backlog, entry.resp)
			}
		}
	}
	ch := make(chan clientv3.WatchResponse, len(backlog)+indexWatchBuffer)
	for _, resp := range backlog {
		ch <- resp
	}
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

// commitLocked assigns the next store revision to a write, stamps it onto the
// entity for a put, and records and delivers the index events the write
// produces. The caller holds m.mu, which is what makes revision order and
// event order the same thing: no other write can commit in between.
//
// eventType is clientv3.EventTypePut for create/update or
// clientv3.EventTypeDelete for delete. prevEntity is the entity's prior value
// when it had one (for a delete, the entity being deleted), so events can
// carry PrevKv the way etcd's WithPrevKV does.
func (m *MockStore) commitLocked(entity *Entity, eventType mvccpb.Event_EventType, prevEntity *Entity) {
	m.rev++
	rev := m.rev
	if eventType == clientv3.EventTypePut {
		entity.SetRevision(rev)
	}

	m.indexWatchersMu.Lock()
	defer m.indexWatchersMu.Unlock()

	// A put that drops a value deletes that value's index entry, as the real
	// store's buildCollectionOps does, so a watcher on the old value hears it go.
	if eventType == clientv3.EventTypePut && prevEntity != nil {
		current := make(map[string]bool)
		for _, attr := range enumerateAllAttrs(entity.attrs) {
			current[attr.CAS()] = true
		}
		gone := make(map[string]bool)
		for _, attr := range enumerateAllAttrs(prevEntity.attrs) {
			indexKey := attr.CAS()
			if current[indexKey] || gone[indexKey] {
				continue
			}
			gone[indexKey] = true
			m.emitIndexEventLocked(indexKey, entity.Id(), clientv3.EventTypeDelete, rev, prevEntity)
		}
	}

	// An index is a keyspace, so a write touches each index key at most once no
	// matter how many of the entity's attributes map to it.
	seen := make(map[string]bool)
	for _, attr := range enumerateAllAttrs(entity.attrs) {
		indexKey := attr.CAS()
		if seen[indexKey] {
			continue
		}
		seen[indexKey] = true
		m.emitIndexEventLocked(indexKey, entity.Id(), eventType, rev, prevEntity)
	}
}

// emitIndexEventLocked records and delivers one index event for the entity's
// entry under indexKey. The caller holds m.mu and m.indexWatchersMu.
func (m *MockStore) emitIndexEventLocked(indexKey string, id Id, eventType mvccpb.Event_EventType, rev int64, prevEntity *Entity) {
	// etcd reports a put as a create when the key's CreateRevision equals
	// its ModRevision, and as a modify otherwise. Track when each index
	// entry first appeared so the mock can say the same.
	entryKey := indexEntryKey(indexKey, id)
	createRev := rev
	switch eventType {
	case clientv3.EventTypePut:
		if first, ok := m.indexEntryCreated[entryKey]; ok {
			createRev = first
		} else {
			m.indexEntryCreated[entryKey] = rev
		}
	case clientv3.EventTypeDelete:
		// A deleted key has no CreateRevision, only the revision it left at.
		createRev = 0
		delete(m.indexEntryCreated, entryKey)
	}

	event := &clientv3.Event{
		Type: eventType,
		Kv: &mvccpb.KeyValue{
			Key:            []byte(indexKey),
			Value:          []byte(id),
			CreateRevision: createRev,
			ModRevision:    rev,
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
		Header: etcdserverpb.ResponseHeader{Revision: rev},
		Events: []*clientv3.Event{event},
	}
	m.indexLog = append(m.indexLog, indexLogEntry{rev: rev, indexKey: indexKey, resp: resp})

	for _, ch := range m.indexWatchers[indexKey] {
		select {
		case ch <- resp:
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

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listIndexLocked(attr), nil
}

// listIndexLocked filters entities by the given attribute, recursively
// enumerating attributes including nested ones in components. The caller
// holds m.mu.
func (m *MockStore) listIndexLocked(attr Attr) []Id {
	var ids []Id
	seen := make(map[Id]bool)
	for id, entity := range m.Entities {
		allAttrs := enumerateAllAttrs(entity.attrs)
		for _, a := range allAttrs {
			if a.ID == attr.ID && a.Value.Equal(attr.Value) {
				ids = append(ids, id)
				seen[id] = true
				break
			}
		}
	}

	// Deduplicated because an index is a keyspace: it cannot hold the same
	// entity twice under one value.
	for _, id := range m.staleIndexEntries[attr.CAS()] {
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}

	return ids
}

// AddStaleIndexEntry makes ListIndex report id under attr even though the stored
// entity does not carry that value. MockStore derives its index from live
// attributes and so cannot drift on its own; this is how a test reaches the case
// where EtcdStore's separate collection keyspace disagrees with the entity.
func (m *MockStore) AddStaleIndexEntry(attr Attr, id Id) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.staleIndexEntries == nil {
		m.staleIndexEntries = make(map[string][]Id)
	}
	key := attr.CAS()
	m.staleIndexEntries[key] = append(m.staleIndexEntries[key], id)
}

// ListIndexRevision returns the matching ids along with the store revision
// they were read at. A WatchIndex resumed from one past that revision sees
// exactly the writes that landed after this list, the way etcd's header
// revision pairs with WithRev. The ids and the revision are read under one
// lock so no write can land between them.
//
// With OnListIndex installed, the revision is read before the hook runs, so a
// hook that blocks to model a slow List reports the revision the List started
// at rather than one that includes writes landing while it was held open.
func (m *MockStore) ListIndexRevision(ctx context.Context, attr Attr) ([]Id, int64, error) {
	if m.OnListIndex != nil {
		m.mu.RLock()
		rev := m.rev
		m.mu.RUnlock()
		ids, err := m.OnListIndex(ctx, attr)
		if err != nil {
			return nil, 0, err
		}
		return ids, rev, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listIndexLocked(attr), m.rev, nil
}

// ListIndexPage pages the mock's index by sorting the ids and slicing. The real
// store gets its ordering from etcd's keyspace; the mock has no keyspace, so it
// imposes id order and its cursor is an entity id rather than a store key.
// Cursors are opaque and never cross between backends, so the difference is
// invisible to callers, but it is why IndexPage.Cursor promises only that a
// cursor is opaque and resumable.
func (m *MockStore) ListIndexPage(ctx context.Context, attr Attr, cursor string, limit int64) (*IndexPage, error) {
	return m.ListIndexPageAtRevision(ctx, attr, cursor, limit, 0)
}

func (m *MockStore) ListIndexPageAtRevision(
	ctx context.Context,
	attr Attr,
	cursor string,
	limit, revision int64,
) (*IndexPage, error) {
	ids, rev, err := m.ListIndexRevision(ctx, attr)
	if err != nil {
		return nil, err
	}
	if revision > 0 {
		rev = revision
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

// CreateSession starts a session. Tokens are varint-encoded like the lease ids
// EtcdStore hands out. The mock has no clock, so a session lives until it is
// revoked and ttl is ignored.
func (m *MockStore) CreateSession(ctx context.Context, ttl int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionSeq++
	tok := binary.AppendVarint(nil, m.sessionSeq)
	m.sessions[string(tok)] = true
	return tok, nil
}

// ListSessionEntities returns the entities the session holds a key of: those
// bound to it and those it has stored session attributes on.
func (m *MockStore) ListSessionEntities(ctx context.Context, session []byte) ([]Id, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tok := string(session)
	var ids []Id
	for id := range m.Entities {
		_, hasBlob := m.sessionBlobs[id][tok]
		if hasBlob || (m.boundTo[id] == tok && tok != "") {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
}

// PingSession fails once the session is gone, with the error etcd's keepalive
// returns for a lease it no longer holds.
func (m *MockStore) PingSession(ctx context.Context, session []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.sessions[string(session)] {
		return rpctypes.ErrLeaseNotFound
	}
	return nil
}

// RevokeSession ends a session the way revoking its lease does: entities bound
// to it are deleted, and every entity it stored session attributes on loses
// them, leaving whatever the entity key and other sessions still hold.
func (m *MockStore) RevokeSession(ctx context.Context, session []byte) error {
	tok := string(session)

	m.mu.Lock()
	if !m.sessions[tok] {
		m.mu.Unlock()
		return rpctypes.ErrLeaseNotFound
	}
	delete(m.sessions, tok)

	var updates []EntityOp
	for id, owner := range m.boundTo {
		if owner == tok {
			if e, ok := m.Entities[id]; ok {
				updates = append(updates, EntityOp{Type: EntityOpDelete, Entity: e})
			}
			m.deleteLocked(id)
		}
	}
	for id, blobs := range m.sessionBlobs {
		if _, ok := blobs[tok]; !ok {
			continue
		}
		if updated := m.dropSessionBlobLocked(id, tok); updated != nil {
			updates = append(updates, EntityOp{Type: EntityOpUpdate, Entity: updated})
		}
	}
	m.mu.Unlock()

	for _, op := range updates {
		go m.notifyWatchers(op.Id(), op)
	}
	return nil
}

// dropSessionBlobLocked removes one session's blob from an entity and rebuilds
// its view from what remains: the entity key's attributes plus every other
// session's blob. Removing the blob leaves the entity key alone, so the entity
// keeps its revision. The caller holds m.mu.
func (m *MockStore) dropSessionBlobLocked(id Id, tok string) *Entity {
	blobs := m.sessionBlobs[id]
	sessionIDs := make(map[Id]bool)
	for _, blob := range blobs {
		for _, attr := range blob {
			sessionIDs[attr.ID] = true
		}
	}
	delete(blobs, tok)
	if len(blobs) == 0 {
		delete(m.sessionBlobs, id)
	}

	prev, ok := m.Entities[id]
	if !ok {
		return nil
	}

	var attrs []Attr
	for _, attr := range prev.attrs {
		if !sessionIDs[attr.ID] {
			attrs = append(attrs, attr)
		}
	}
	others := make([]string, 0, len(blobs))
	for other := range blobs {
		others = append(others, other)
	}
	slices.Sort(others)
	for _, other := range others {
		attrs = append(attrs, blobs[other]...)
	}

	updated := &Entity{attrs: attrs}
	rev := prev.GetRevision()
	m.Entities[id] = updated
	m.commitLocked(updated, clientv3.EventTypePut, prev)
	updated.SetRevision(rev)
	return updated
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
