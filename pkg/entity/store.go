package entity

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mr-tron/base58"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity/types"
)

// EtcdStore implements Store using etcd
type EtcdStore struct {
	log       *slog.Logger
	client    *clientv3.Client
	validator *Validator
	prefix    string

	schemaCache map[Id]*AttributeSchema
	mu          sync.RWMutex
}

type Store interface {
	GetEntity(ctx context.Context, id Id) (*Entity, error)
	GetEntityAtRevision(ctx context.Context, id Id, rev int64) (*Entity, error)
	GetEntities(ctx context.Context, ids []Id) ([]*Entity, error)
	WatchEntity(ctx context.Context, id Id) (chan EntityOp, error)
	GetAttributeSchema(ctx context.Context, name Id) (*AttributeSchema, error)
	CreateEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error)
	UpdateEntity(ctx context.Context, id Id, entity *Entity, opts ...EntityOption) (*Entity, error)
	ReplaceEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error)
	PatchEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error)
	EnsureEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, bool, error)
	DeleteEntity(ctx context.Context, id Id) error
	WatchIndex(ctx context.Context, attr Attr, fromRev int64) (clientv3.WatchChan, error)
	ListIndex(ctx context.Context, attr Attr) ([]Id, error)
	// ListIndexRevision is like ListIndex but also returns the etcd revision the
	// listing was read at, so a caller can resume a watch from that point.
	ListIndexRevision(ctx context.Context, attr Attr) ([]Id, int64, error)
	// ListIndexPage reads one bounded page of an index. Unlike ListIndex it does
	// not materialize the whole index, so paging through a large one costs
	// O(page) per call rather than O(index).
	ListIndexPage(ctx context.Context, attr Attr, cursor string, limit int64) (*IndexPage, error)
	ListCollection(ctx context.Context, collection string) ([]Id, error)

	CreateSession(ctx context.Context, ttl int64) ([]byte, error)
	RevokeSession(ctx context.Context, session []byte) error
	PingSession(ctx context.Context, session []byte) error
	ListSessionEntities(ctx context.Context, session []byte) ([]Id, error)
}

var ErrNotFound = errors.New("entity not found")

var _ Store = (*EtcdStore)(nil)

// etcdMaxTxnOps is the maximum number of operations allowed in a single etcd transaction.
// Each entity requires 2 operations (primary + session), so we divide by 2 for max entities per batch.
const etcdMaxTxnOps = 128
const maxEntitiesPerBatch = etcdMaxTxnOps / 2

// NewEtcdStore creates a new etcd-backed entity store
func NewEtcdStore(ctx context.Context, log *slog.Logger, client *clientv3.Client, prefix string) (*EtcdStore, error) {
	store := &EtcdStore{
		log:         log.With("module", "etcdstore"),
		client:      client,
		prefix:      prefix,
		schemaCache: make(map[Id]*AttributeSchema),
	}

	store.validator = NewValidator(store)

	err := InitSystemEntities(func(e *Entity) error {
		return store.basicSave(ctx, e)
	})

	if err != nil {
		return nil, err
	}

	return store, nil
}

func (s *EtcdStore) basicSave(ctx context.Context, entity *Entity) error {
	entity.Fixup()

	data, err := encoder.Marshal(entity)
	if err != nil {
		return fmt.Errorf("failed to marshal entity: %w", err)
	}

	key := s.buildKey(entity.Id())

	// First, try to get the existing entity
	getResp, err := s.client.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to get entity from etcd: %w", err)
	}

	// If entity exists, compare with new data
	if len(getResp.Kvs) > 0 {
		existingData := getResp.Kvs[0].Value

		// If data is identical, no need to update
		if string(existingData) == string(data) {
			return nil
		}
	}

	// Entity doesn't exist or data is different - create/update it
	_, err = s.client.Put(ctx, key, string(data))
	if err != nil {
		return fmt.Errorf("failed to create entity in etcd: %w", err)
	}

	return nil
}

// buildKey creates a full etcd key for an entity
func (s *EtcdStore) buildKey(id Id) string {
	return fmt.Sprintf("%s/entity/%s", s.prefix, base58.Encode([]byte(id)))
}

type entityOpts struct {
	bind         bool
	session      []byte
	fromRevision int64
	overwrite    bool
}

type EntityOption func(*entityOpts)

func WithSession(session []byte) EntityOption {
	return func(opts *entityOpts) {
		opts.session = session
	}
}

func BondToSession(session []byte) EntityOption {
	return func(opts *entityOpts) {
		opts.bind = true
		opts.session = session
	}
}

func WithFromRevision(revision int64) EntityOption {
	return func(opts *entityOpts) {
		opts.fromRevision = revision
	}
}

func WithOverwrite(opts *entityOpts) {
	opts.overwrite = true
}

// CreateEntity implements Store interface
func (s *EtcdStore) CreateEntity(
	ctx context.Context,
	entity *Entity,
	opts ...EntityOption,
) (*Entity, error) {
	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	entity.ForceID()

	// Allocate a short-id for entities that have an entity/kind attribute
	// (skip system/schema entities which are saved via basicSave)
	if _, hasKind := entity.Get(EntityKind); hasKind {
		if _, hasShortId := entity.Get(DBShortId); !hasShortId {
			shortId, err := AllocateShortId(string(entity.Id()), func(candidate string) (bool, error) {
				return s.uniqueExists(ctx, String(DBShortId, candidate))
			})
			if err != nil {
				return nil, fmt.Errorf("failed to allocate short-id: %w", err)
			}
			entity.Set(String(DBShortId, shortId))
		}
	}

	// Revision is an attr maintained by the store itself, so we remove it
	// and allow it to be repopulated later. This is mostly to avoid confusion
	// when retrieving an entity, before stamping it with the current etcd revision.
	entity.Remove(Revision)

	// Set CreatedAt if not already set (store manages this timestamp)
	if entity.GetCreatedAt().IsZero() {
		entity.SetCreatedAt(time.Now())
	}

	// Validate attributes
	if err := s.validator.ValidateEntity(ctx, entity); err != nil {
		return nil, err
	}

	var (
		sid      int64
		sessPart string
	)

	if len(o.session) > 0 {
		sid, _ = binary.Varint(o.session)
		sessPart = base58.Encode(o.session)
	}

	// Separate attributes into primary and session, and collect indexed attributes
	primary, session, indexedAttrs, err := s.separateSessionAttributes(ctx, entity.attrs)
	if err != nil {
		return nil, err
	}

	// Build collection operations for indexed attributes
	var coltxopt []clientv3.Op
	for _, attrs := range indexedAttrs {
		for _, attr := range attrs {
			coltxopt = append(coltxopt, s.addToCollectionOp(entity, attr.CAS(), plainIndexLease(o.bind, sid)))

			if sessPart != "" {
				coltxopt = append(coltxopt, s.addToCollectionSessionOp(entity, attr.CAS(), sessPart, sid))
			}
		}
	}

	// Collect unique-value attributes and build transaction conditions + ops
	uniqueAttrs, err := s.collectUniqueAttrs(ctx, entity)
	if err != nil {
		return nil, err
	}

	var uniqueConditions []clientv3.Cmp
	for _, attr := range uniqueAttrs {
		uniqueKey := s.buildUniqueKey(attr)
		uniqueConditions = append(uniqueConditions, clientv3.Compare(clientv3.CreateRevision(uniqueKey), "=", 0))
		coltxopt = append(coltxopt, s.addUniqueOp(attr, entity.Id()))
	}

	entity.attrs = primary
	key := s.buildKey(entity.Id())

	// Retry loop: if the create transaction fails due to a unique-value
	// collision (TOCTOU race on short-id or other unique attr), allocate
	// a new value and try again.
	const maxUniqueRetries = 3
	for attempt := 0; ; attempt++ {
		// Build entity save operations (must rebuild each attempt since
		// short-id changes affect the serialized entity data)
		txopt, err := s.buildEntitySaveOps(entity, key, primary, session, &o)
		if err != nil {
			return nil, err
		}

		txopt = append(txopt, coltxopt...)

		// Build transaction conditions: entity must not exist + all unique values must be free
		conditions := []clientv3.Cmp{
			clientv3.Compare(clientv3.CreateRevision(key), "=", 0),
		}
		conditions = append(conditions, uniqueConditions...)

		txnResp, err := s.client.Txn(ctx).
			If(conditions...).
			Then(txopt...).
			Else(clientv3.OpGet(key)).
			Commit()

		if err != nil {
			return nil, fmt.Errorf("failed to create entity in etcd: %w", err)
		}

		if txnResp.Succeeded {
			entity.SetRevision(txnResp.Header.Revision)
			return entity, nil
		}

		// Transaction failed — figure out why.
		// Check if the entity key already exists (genuine conflict) vs
		// a unique-value condition failed (retryable).
		if len(txnResp.Responses) == 1 {
			rng := txnResp.Responses[0].GetResponseRange()
			if len(rng.Kvs) > 0 {
				// Entity key exists — this is a genuine entity conflict.
				var curr Entity
				if decoder.Unmarshal(rng.Kvs[0].Value, &curr) == nil {
					if slices.EqualFunc(curr.attrs, entity.attrs, func(a, b Attr) bool {
						return a.Equal(b)
					}) {
						entity.SetRevision(rng.Header.Revision)
						return entity, nil
					}
				}

				if o.overwrite {
					// On overwrite, preserve the existing entity's short-id
					// to avoid changing a stable identifier.
					if existingShortId := curr.ShortId(); existingShortId != "" {
						entity.Set(String(DBShortId, existingShortId))
						primary = entity.Attrs()
					}

					txopt, err = s.buildEntitySaveOps(entity, key, primary, session, &o)
					if err != nil {
						return nil, err
					}
					// No unique ops needed on overwrite — values are already claimed
					txopt = append(txopt, coltxopt...)

					txnResp, err = s.client.Txn(ctx).
						Then(txopt...).
						Commit()
					if err != nil {
						return nil, fmt.Errorf("failed to create entity in etcd (on overwrite): %w", err)
					}

					entity.SetRevision(txnResp.Header.Revision)
					return entity, nil
				}

				s.log.Error("failed to create entity in etcd", "id", entity.Id())
				return nil, cond.Conflict("entity", entity.Id())
			}
		}

		// Entity key didn't exist, so a unique-value condition failed.
		// Re-allocate the short-id and retry.
		if attempt >= maxUniqueRetries {
			s.log.Error("unique value collision persisted after retries", "id", entity.Id())
			return nil, fmt.Errorf("failed to create entity: unique value collision after %d retries", maxUniqueRetries)
		}

		s.log.Info("unique value collision, retrying with new short-id",
			"id", entity.Id(), "old_short_id", entity.ShortId(), "attempt", attempt+1)

		newShortId, err := AllocateShortId(string(entity.Id()), func(candidate string) (bool, error) {
			return s.uniqueExists(ctx, String(DBShortId, candidate))
		})
		if err != nil {
			return nil, fmt.Errorf("failed to reallocate short-id: %w", err)
		}
		entity.Set(String(DBShortId, newShortId))

		// Rebuild primary attrs and unique conditions with the new short-id
		primary = entity.Attrs()

		uniqueAttrs, err = s.collectUniqueAttrs(ctx, entity)
		if err != nil {
			return nil, err
		}
		uniqueConditions = nil
		// Rebuild coltxopt without old unique ops — keep collection ops, replace unique ops
		coltxopt = coltxopt[:0]
		for _, attrs := range indexedAttrs {
			for _, attr := range attrs {
				coltxopt = append(coltxopt, s.addToCollectionOp(entity, attr.CAS(), plainIndexLease(o.bind, sid)))
				if sessPart != "" {
					coltxopt = append(coltxopt, s.addToCollectionSessionOp(entity, attr.CAS(), sessPart, sid))
				}
			}
		}
		for _, attr := range uniqueAttrs {
			uniqueKey := s.buildUniqueKey(attr)
			uniqueConditions = append(uniqueConditions, clientv3.Compare(clientv3.CreateRevision(uniqueKey), "=", 0))
			coltxopt = append(coltxopt, s.addUniqueOp(attr, entity.Id()))
		}
	}
}

// GetEntity implements Store interface
func (s *EtcdStore) GetEntity(ctx context.Context, id Id) (*Entity, error) {
	key := s.buildKey(id)

	tr, err := s.client.Txn(ctx).Then(
		clientv3.OpGet(key),
		clientv3.OpGet(key+"/session/", clientv3.WithPrefix()),
	).Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to get entity from etcd: %w", err)
	}

	if !tr.Succeeded {
		return nil, cond.NotFound("entity", id)
	}

	if len(tr.Responses) != 2 {
		return nil, cond.NotFound("entity", id)
	}

	resp := tr.Responses[0].GetResponseRange()

	if len(resp.Kvs) == 0 {
		return nil, cond.NotFound("entity", id)
	}

	var entity Entity

	err = decoder.Unmarshal(resp.Kvs[0].Value, &entity)
	if err != nil {
		return nil, cond.Corruption("entity", "failed to deserialize entity: %s", err)
	}

	if resp.Kvs[0].Lease != 0 {
		ttlr, err := s.client.TimeToLive(ctx, clientv3.LeaseID(resp.Kvs[0].Lease))
		if err == nil {
			entity.attrs = append(entity.attrs, Duration(TTL, time.Duration(ttlr.TTL)*time.Second))
		}
	}

	entity.SetRevision(resp.Kvs[0].ModRevision)

	resp = tr.Responses[1].GetResponseRange()

	for _, kv := range resp.Kvs {
		var attrs []Attr
		err = decoder.Unmarshal(kv.Value, &attrs)
		if err != nil {
			return nil, cond.Corruption("entity", "failed to deserialize entity: %s", err)
		}

		sid := string(kv.Key)

		idx := strings.LastIndexByte(sid, '/')
		if idx != -1 {
			sid = sid[idx+1:]
			attrs = append(attrs, String(AttrSession, sid))
		}

		entity.attrs = append(entity.attrs, attrs...)
	}

	entity.postUnmarshal()

	return &entity, nil
}

// GetEntityAtRevision reads an entity at a specific etcd revision.
// This is used to retrieve entity data for delete events where the entity
// may no longer exist at the current revision.
func (s *EtcdStore) GetEntityAtRevision(ctx context.Context, id Id, rev int64) (*Entity, error) {
	key := s.buildKey(id)

	resp, err := s.client.Get(ctx, key, clientv3.WithRev(rev))
	if err != nil {
		return nil, fmt.Errorf("failed to get entity at revision %d: %w", rev, err)
	}

	if len(resp.Kvs) == 0 {
		return nil, cond.NotFound("entity", id)
	}

	var entity Entity

	err = decoder.Unmarshal(resp.Kvs[0].Value, &entity)
	if err != nil {
		return nil, cond.Corruption("entity", "failed to deserialize entity: %s", err)
	}

	entity.SetRevision(resp.Kvs[0].ModRevision)
	entity.postUnmarshal()

	return &entity, nil
}

func (s *EtcdStore) GetEntities(ctx context.Context, ids []Id) ([]*Entity, error) {
	if len(ids) == 0 {
		return []*Entity{}, nil
	}

	// Process entities in batches to avoid exceeding etcd transaction limits
	entities := make([]*Entity, len(ids))

	for start := 0; start < len(ids); start += maxEntitiesPerBatch {
		end := start + maxEntitiesPerBatch
		if end > len(ids) {
			end = len(ids)
		}

		batchIds := ids[start:end]

		// Build all the ops for this batch
		var ops []clientv3.Op
		for _, id := range batchIds {
			key := s.buildKey(id)
			ops = append(ops, clientv3.OpGet(key))
			ops = append(ops, clientv3.OpGet(key+"/session/", clientv3.WithPrefix()))
		}

		// Execute transaction for this batch
		tr, err := s.client.Txn(ctx).Then(ops...).Commit()
		if err != nil {
			return nil, fmt.Errorf("failed to get entities from etcd: %w", err)
		}

		if !tr.Succeeded {
			return nil, fmt.Errorf("transaction failed")
		}

		// Process results for this batch
		for i := 0; i < len(batchIds); i++ {
			// Each entity has 2 responses: primary and session
			primaryIdx := i * 2
			sessionIdx := i*2 + 1

			if primaryIdx >= len(tr.Responses) || sessionIdx >= len(tr.Responses) {
				continue
			}

			primaryResp := tr.Responses[primaryIdx].GetResponseRange()
			if len(primaryResp.Kvs) == 0 {
				// Entity not found, leave nil in the result array
				s.log.Warn("failed to get primary entity from etcd", "id", batchIds[i])
				continue
			}

			var entity Entity
			err = decoder.Unmarshal(primaryResp.Kvs[0].Value, &entity)
			if err != nil {
				continue
			}

			entity.SetRevision(primaryResp.Kvs[0].ModRevision)

			// Process session attributes
			sessionResp := tr.Responses[sessionIdx].GetResponseRange()
			for _, kv := range sessionResp.Kvs {
				var attrs []Attr
				err = decoder.Unmarshal(kv.Value, &attrs)
				if err != nil {
					continue
				}

				sid := string(kv.Key)
				//if there is a '/' at the end of the key, everything that follows is a session id.
				idx := strings.LastIndexByte(sid, '/')
				if idx != -1 {
					sid = sid[idx+1:]
					attrs = append(attrs, String(AttrSession, sid))
				}

				entity.attrs = append(entity.attrs, attrs...)
			}

			entity.postUnmarshal()
			entities[start+i] = &entity
		}
	}

	return entities, nil
}

type EntityOpType int

const (
	EntityOpCreate EntityOpType = iota
	EntityOpUpdate
	EntityOpDelete
	EntityOpStated
)

type EntityOp struct {
	Type EntityOpType
	*Entity
}

func (s *EtcdStore) WatchEntity(ctx context.Context, id Id) (chan EntityOp, error) {
	key := s.buildKey(id)
	wc := s.client.Watch(ctx, key, clientv3.WithPrevKV())

	och := make(chan EntityOp)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case watchevent, ok := <-wc:
				if !ok {
					return
				}
				for _, event := range watchevent.Events {
					var (
						eventType EntityOpType
						read      bool
					)

					switch {
					case event.IsCreate():
						eventType = EntityOpCreate
						read = true
					case event.IsModify():
						eventType = EntityOpUpdate
						read = true
					case event.Type == clientv3.EventTypeDelete:
						eventType = EntityOpDelete
					default:
						read = true
						eventType = EntityOpStated
					}

					op := EntityOp{
						Type: eventType,
					}

					if read {
						var entity Entity

						err := decoder.Unmarshal(event.Kv.Value, &entity)
						if err != nil {
							s.log.Error("failed to get entity for event", "error", err, "id", event.Kv.Value)
							continue
						}

						entity.SetRevision(event.Kv.ModRevision)

						entity.postUnmarshal()
						op.Entity = &entity
					} else if eventType == EntityOpDelete && event.PrevKv != nil {
						var entity Entity

						err := decoder.Unmarshal(event.PrevKv.Value, &entity)
						if err != nil {
							s.log.Error("failed to decode previous entity for delete event", "error", err)
						} else {
							entity.SetRevision(event.PrevKv.ModRevision)
							entity.postUnmarshal()
							op.Entity = &entity
						}
					}

					select {
					case <-ctx.Done():
						return
					case och <- op:
						// ok
					}
				}
			}
		}
	}()

	return och, nil
}

// UpdateEntity implements Store interface
func (s *EtcdStore) UpdateEntity(
	ctx context.Context,
	id Id,
	changes *Entity,
	opts ...EntityOption,
) (*Entity, error) {
	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	if id == "" {
		return nil, fmt.Errorf("entity ID is required")
	}

	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}

	if o.fromRevision != 0 && entity.GetRevision() != o.fromRevision {
		s.log.Error("entity revision mismatch", "expected", o.fromRevision, "actual", entity.GetRevision())
		return nil, cond.Conflict("entity", entity.Id())
	}

	// Snapshot original entity for unique-value diffing
	originalEntity := entity.Clone()

	// Keep track of original indexed attributes for removal (including nested ones)
	originalIndexedAttrs, err := s.collectIndexedAttributes(ctx, entity.attrs)
	if err != nil {
		return nil, err
	}

	// Validate attributes
	for _, attr := range changes.attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get attribute schema: %w", err)
		}

		if !schema.AllowMany {
			if _, ok := entity.Get(attr.ID); ok {
				entity.Remove(attr.ID)
			}
		}
	}

	// Remove AttrSession attributes from changes because they're virtual
	// and we just ignore if someone gives us one
	changes.Remove(AttrSession)

	err = entity.Merge(changes)
	if err != nil {
		return nil, fmt.Errorf("failed to update entity: %w", err)
	}

	rev := entity.GetRevision()

	// Revision is a store-maintained attr, so we remove it from the changes.
	entity.Remove(Revision)

	// An update only owns the attributes it sets. Exempt unchanged references
	// from the existence check so a pre-existing reference whose target was
	// deleted out from under the entity can't block an unrelated change. See
	// MIR-1320.
	err = s.validator.ValidateUpdate(ctx, entity.attrs, originalEntity.attrs)
	if err != nil {
		return nil, err
	}

	var (
		sessPart string
		sid      int64
	)

	if len(o.session) != 0 {
		sid, _ = binary.Varint(o.session)
		sessPart = base58.Encode(o.session)
	}

	// Separate attributes into primary and session, and collect indexed attributes (including nested ones)
	primary, session, newIndexedAttrs, err := s.separateSessionAttributes(ctx, entity.attrs)
	if err != nil {
		return nil, err
	}

	// Reindex changed attributes via the shared helper so the lease and watcher
	// semantics stay in one place (also used by ReplaceEntity/PatchEntity).
	coltxopt := s.buildCollectionOps(entity, originalIndexedAttrs, newIndexedAttrs, sessPart, sid, o.bind)

	// Build unique-value update operations (release old, claim new)
	uniqueOps, uniqueConditions, err := s.buildUniqueUpdateOps(ctx, entity.Id(), originalEntity, entity)
	if err != nil {
		return nil, err
	}
	coltxopt = append(coltxopt, uniqueOps...)

	entity.attrs = primary

	// Build entity save operations
	key := s.buildKey(entity.Id())
	txopt, err := s.buildEntitySaveOps(entity, key, primary, session, &o)
	if err != nil {
		return nil, err
	}

	txopt = append(txopt, coltxopt...)

	var txnResp *clientv3.TxnResponse

	// Build conditions: revision check + unique-value constraints
	var conditions []clientv3.Cmp
	if o.fromRevision != 0 {
		conditions = append(conditions, clientv3.Compare(clientv3.ModRevision(key), "=", rev))
	}
	conditions = append(conditions, uniqueConditions...)

	if len(conditions) > 0 {
		txnResp, err = s.client.Txn(ctx).
			If(conditions...).
			Then(txopt...).
			Commit()
	} else {
		txnResp, err = s.client.Txn(ctx).
			Then(txopt...).
			Commit()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to update entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		s.log.Error("failed to update entity in etcd", "error", err, "id", entity.Id(), "rev", rev, "server-rev", txnResp.Header.Revision)
		return nil, cond.Conflict("entity", entity.Id())
	}

	entity.SetRevision(txnResp.Header.Revision)

	return entity, nil
}

// extractEntityId extracts the entity ID from the db/id attribute in the attributes list
func extractEntityId(attributes []Attr) (Id, error) {
	for _, attr := range attributes {
		if attr.ID == DBId {
			switch v := attr.Value.Any().(type) {
			case Id:
				return v, nil
			case string:
				return Id(v), nil
			case types.Keyword:
				return Id(v), nil
			default:
				return "", fmt.Errorf("invalid db/id attribute type: %T", attr.Value.Any())
			}
		}
	}
	return "", fmt.Errorf("db/id attribute is required")
}

// checkRevisionConflict checks if the entity revision matches the expected revision
func (s *EtcdStore) checkRevisionConflict(entity *Entity, expectedRevision int64) error {
	actualRevision := entity.GetRevision()
	if expectedRevision != 0 && actualRevision != expectedRevision {
		s.log.Error("entity revision mismatch", "expected", expectedRevision, "actual", actualRevision)
		return cond.Conflict("entity", entity.Id())
	}
	return nil
}

// collectIndexedAttributes builds a map of indexed attributes from the entity
func (s *EtcdStore) collectIndexedAttributes(ctx context.Context, attrs []Attr) (map[Id][]Attr, error) {
	indexedAttrs := make(map[Id][]Attr)
	// Enumerate all attributes including nested ones in components
	allAttrs := enumerateAllAttrs(attrs)
	for _, attr := range allAttrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get attribute schema: %w", err)
		}
		if schema.Index {
			indexedAttrs[attr.ID] = append(indexedAttrs[attr.ID], attr)
		}
	}
	return indexedAttrs, nil
}

// separateSessionAttributes separates attributes into primary and session attributes
// hasSessionAttr reports whether any of the given attributes is session-scoped.
// It's a lighter-weight check than separateSessionAttributes for callers that
// only need the yes/no answer and don't want to rebuild the primary/session/
// indexed split.
func (s *EtcdStore) hasSessionAttr(ctx context.Context, attrs []Attr) (bool, error) {
	for _, attr := range attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return false, err
		}
		if schema.Session {
			return true, nil
		}
	}
	return false, nil
}

func (s *EtcdStore) separateSessionAttributes(ctx context.Context, attrs []Attr) (primary, session []Attr, indexedAttrs map[Id][]Attr, err error) {
	indexedAttrs = make(map[Id][]Attr)
	// Enumerate all attributes including nested ones in components for indexing
	allAttrs := enumerateAllAttrs(attrs)
	for _, attr := range allAttrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get attribute schema: %w", err)
		}

		if schema.Index {
			indexedAttrs[attr.ID] = append(indexedAttrs[attr.ID], attr)
		}
	}

	// Separate top-level attributes into session vs primary (don't enumerate here)
	for _, attr := range attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get attribute schema: %w", err)
		}

		if schema.Session {
			session = append(session, attr)
		} else {
			primary = append(primary, attr)
		}
	}
	return primary, session, indexedAttrs, nil
}

// buildCollectionOps builds etcd operations for updating indexed attribute collections
func (s *EtcdStore) buildCollectionOps(entity *Entity, originalIndexedAttrs, newIndexedAttrs map[Id][]Attr, sessPart string, sid int64, bind bool) []clientv3.Op {
	var ops []clientv3.Op

	// Remove old indexed attributes that are no longer present or have changed
	for attrID, oldAttrs := range originalIndexedAttrs {
		newAttrs := newIndexedAttrs[attrID]
		for _, oldAttr := range oldAttrs {
			found := slices.ContainsFunc(newAttrs, oldAttr.Equal)
			if !found {
				ops = append(ops, s.deleteFromCollectionOp(entity, oldAttr.CAS()))
			}
		}
	}

	// Add all new indexed attributes
	for _, newAttrs := range newIndexedAttrs {
		for _, newAttr := range newAttrs {
			ops = append(ops, s.addToCollectionOp(entity, newAttr.CAS(), plainIndexLease(bind, sid)))
			if sessPart != "" {
				ops = append(ops, s.addToCollectionSessionOp(entity, newAttr.CAS(), sessPart, sid))
			}
		}
	}

	return ops
}

// buildUniqueUpdateOps compares the old and new unique-value attributes and
// returns operations to release old unique keys and claim new ones, along with
// conditions that ensure the new keys don't already exist.
func (s *EtcdStore) buildUniqueUpdateOps(ctx context.Context, entityId Id, oldEntity, newEntity *Entity) (ops []clientv3.Op, conditions []clientv3.Cmp, err error) {
	oldUnique, err := s.collectUniqueAttrs(ctx, oldEntity)
	if err != nil {
		return nil, nil, err
	}
	newUnique, err := s.collectUniqueAttrs(ctx, newEntity)
	if err != nil {
		return nil, nil, err
	}

	// Build maps for comparison
	oldByID := make(map[Id]Attr)
	for _, attr := range oldUnique {
		oldByID[attr.ID] = attr
	}
	newByID := make(map[Id]Attr)
	for _, attr := range newUnique {
		newByID[attr.ID] = attr
	}

	// Release old unique keys for values that changed or were removed
	for id, oldAttr := range oldByID {
		newAttr, exists := newByID[id]
		if !exists || !oldAttr.Equal(newAttr) {
			ops = append(ops, s.deleteUniqueOp(oldAttr))
		}
	}

	// Claim new unique keys for values that changed or were added
	for id, newAttr := range newByID {
		oldAttr, exists := oldByID[id]
		if !exists || !oldAttr.Equal(newAttr) {
			uniqueKey := s.buildUniqueKey(newAttr)
			conditions = append(conditions, clientv3.Compare(clientv3.CreateRevision(uniqueKey), "=", 0))
			ops = append(ops, s.addUniqueOp(newAttr, entityId))
		}
	}

	return ops, conditions, nil
}

// buildEntitySaveOps builds etcd operations for saving entity data (primary and session attributes)
func (s *EtcdStore) buildEntitySaveOps(entity *Entity, key string, primary, session []Attr, o *entityOpts) ([]clientv3.Op, error) {
	var ops []clientv3.Op

	entity.attrs = slices.Clone(primary)
	// Store manages UpdatedAt - set it on every save
	entity.SetUpdatedAt(time.Now())

	data, err := encoder.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize entity: %w", err)
	}

	var sid int64
	var sessPart string
	if len(o.session) != 0 {
		sid, _ = binary.Varint(o.session)
		sessPart = base58.Encode(o.session)
	}

	if o.bind {
		ops = append(ops, clientv3.OpPut(key, string(data), clientv3.WithLease(clientv3.LeaseID(sid))))
	} else {
		ops = append(ops, clientv3.OpPut(key, string(data)))
	}

	if len(session) > 0 {
		if len(o.session) == 0 {
			return nil, fmt.Errorf("session ID is required for session attributes")
		}

		skey := key + "/session/" + sessPart
		sdata, err := encoder.Marshal(session)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal session attributes: %w", err)
		}
		ops = append(ops, clientv3.OpPut(skey, string(sdata), clientv3.WithLease(clientv3.LeaseID(sid))))
	}

	return ops, nil
}

// ReplaceEntity atomically replaces all attributes of an entity
// The db/id attribute must be present in the attributes to identify the entity
func (s *EtcdStore) ReplaceEntity(
	ctx context.Context,
	current *Entity,
	opts ...EntityOption,
) (*Entity, error) {
	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	// Extract ID from db/id attribute
	id, err := extractEntityId(current.attrs)
	if err != nil {
		return nil, err
	}

	// Get current entity to check revision and snapshot for unique-value diffing
	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}

	originalEntity := entity.Clone()
	rev := entity.GetRevision()

	repl := current.Clone()

	if repl.Id() != id {
		return nil, fmt.Errorf("db/id attribute does not match existing entity ID")
	}

	if repl.GetRevision() == 0 {
		repl.SetRevision(rev)
	}

	// Check revision if specified
	if err := s.checkRevisionConflict(repl, o.fromRevision); err != nil {
		return nil, err
	}

	// Keep track of original indexed attributes for removal
	originalIndexedAttrs, err := s.collectIndexedAttributes(ctx, repl.attrs)
	if err != nil {
		return nil, err
	}

	// Revision is a store-maintained attr, so we remove it from the replacement.
	repl.Remove(Revision)

	// Validate replacement attributes. A replace only owns the references it
	// sets: a reference carried over unchanged from the existing entity must not
	// fail the existence check just because its target was deleted. See MIR-1320.
	if err := s.validator.ValidateUpdate(ctx, repl.attrs, originalEntity.attrs); err != nil {
		return nil, err
	}

	// Separate primary and session attributes, collect new indexed attrs
	primary, session, newIndexedAttrs, err := s.separateSessionAttributes(ctx, repl.attrs)
	if err != nil {
		return nil, err
	}

	// Build collection update operations
	var sid int64
	var sessPart string
	if len(o.session) != 0 {
		sid, _ = binary.Varint(o.session)
		sessPart = base58.Encode(o.session)
	}
	coltxopt := s.buildCollectionOps(repl, originalIndexedAttrs, newIndexedAttrs, sessPart, sid, o.bind)

	// Build unique-value update operations (release old, claim new)
	uniqueOps, uniqueConditions, err := s.buildUniqueUpdateOps(ctx, repl.Id(), originalEntity, repl)
	if err != nil {
		return nil, err
	}
	coltxopt = append(coltxopt, uniqueOps...)

	// Build entity save operations
	key := s.buildKey(repl.Id())
	txopt, err := s.buildEntitySaveOps(repl, key, primary, session, &o)
	if err != nil {
		return nil, err
	}

	txopt = append(txopt, coltxopt...)

	var txnResp *clientv3.TxnResponse

	// Build conditions: revision check + unique-value constraints
	var conditions []clientv3.Cmp
	if o.fromRevision != 0 {
		conditions = append(conditions, clientv3.Compare(clientv3.ModRevision(key), "=", rev))
	}
	conditions = append(conditions, uniqueConditions...)

	if len(conditions) > 0 {
		txnResp, err = s.client.Txn(ctx).
			If(conditions...).
			Then(txopt...).
			Commit()
	} else {
		txnResp, err = s.client.Txn(ctx).
			Then(txopt...).
			Commit()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to replace entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		s.log.Error("failed to replace entity in etcd", "error", err, "id", repl.Id())
		return nil, cond.Conflict("entity", repl.Id())
	}

	repl.SetRevision(txnResp.Header.Revision)

	return repl, nil
}

// PatchEntity merges attributes into an existing entity
// For cardinality=one attributes, replaces the value
// For cardinality=many attributes, adds to existing values
// The db/id attribute must be present in the attributes to identify the entity
func (s *EtcdStore) PatchEntity(
	ctx context.Context,
	current *Entity,
	opts ...EntityOption,
) (*Entity, error) {
	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	// Extract ID from db/id attribute
	id, err := extractEntityId(current.attrs)
	if err != nil {
		return nil, err
	}

	// Get current entity and snapshot for unique-value diffing
	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}

	originalEntity := entity.Clone()

	// Check revision if specified
	if err := s.checkRevisionConflict(entity, o.fromRevision); err != nil {
		return nil, err
	}

	// Keep track of original indexed attributes for removal
	originalIndexedAttrs, err := s.collectIndexedAttributes(ctx, entity.attrs)
	if err != nil {
		return nil, err
	}

	// Validate and merge attributes (remove cardinality=one, keep cardinality=many)
	for _, attr := range current.attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get attribute schema: %w", err)
		}

		if !schema.AllowMany {
			if _, ok := entity.Get(attr.ID); ok {
				entity.Remove(attr.ID)
			}
		}
	}

	// Remove AttrSession attributes from changes because they're virtual
	// and we just ignore if someone gives us one
	current.Remove(AttrSession)

	err = entity.Merge(current)
	if err != nil {
		return nil, fmt.Errorf("failed to update entity: %w", err)
	}

	rev := entity.GetRevision()

	// Revision is a store-maintained attr, so we remove it from the changes.
	entity.Remove(Revision)

	// A patch only owns the attributes it sets. Exempt unchanged references from
	// the existence check so a pre-existing reference whose target was deleted
	// out from under the entity can't block an unrelated change (e.g. a
	// status-only patch). See MIR-1320.
	err = s.validator.ValidateUpdate(ctx, entity.attrs, originalEntity.attrs)
	if err != nil {
		return nil, err
	}

	// Separate primary and session attributes, collect new indexed attrs
	primary, session, newIndexedAttrs, err := s.separateSessionAttributes(ctx, entity.attrs)
	if err != nil {
		return nil, err
	}

	// When the caller has no session, the only session attributes present were
	// read from the existing entity (this patch didn't set them) — they belong
	// to another client's session lease and live under their own sub-key. Leave
	// them untouched rather than trying to re-save them (which would fail for
	// lack of a session id). A patch that itself carries session attributes
	// without a session is still a real error, handled by buildEntitySaveOps.
	if len(o.session) == 0 && len(session) > 0 {
		inputHasSession, serr := s.hasSessionAttr(ctx, current.attrs)
		if serr != nil {
			return nil, serr
		}
		if !inputHasSession {
			session = nil
		}
	}

	// Build collection update operations
	var sid int64
	var sessPart string
	if len(o.session) != 0 {
		sid, _ = binary.Varint(o.session)
		sessPart = base58.Encode(o.session)
	}
	coltxopt := s.buildCollectionOps(entity, originalIndexedAttrs, newIndexedAttrs, sessPart, sid, o.bind)

	// Build unique-value update operations (release old, claim new)
	uniqueOps, uniqueConditions, err := s.buildUniqueUpdateOps(ctx, entity.Id(), originalEntity, entity)
	if err != nil {
		return nil, err
	}
	coltxopt = append(coltxopt, uniqueOps...)

	entity.attrs = primary

	// Build entity save operations
	key := s.buildKey(entity.Id())
	txopt, err := s.buildEntitySaveOps(entity, key, primary, session, &o)
	if err != nil {
		return nil, err
	}

	txopt = append(txopt, coltxopt...)

	var txnResp *clientv3.TxnResponse

	// Build conditions: revision check + unique-value constraints
	var conditions []clientv3.Cmp
	if o.fromRevision != 0 {
		conditions = append(conditions, clientv3.Compare(clientv3.ModRevision(key), "=", rev))
	}
	conditions = append(conditions, uniqueConditions...)

	if len(conditions) > 0 {
		txnResp, err = s.client.Txn(ctx).
			If(conditions...).
			Then(txopt...).
			Commit()
	} else {
		txnResp, err = s.client.Txn(ctx).
			Then(txopt...).
			Commit()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to patch entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		s.log.Error("failed to patch entity in etcd", "error", err, "id", entity.Id())
		return nil, cond.Conflict("entity", entity.Id())
	}

	entity.SetRevision(txnResp.Header.Revision)

	return entity, nil
}

// EnsureEntity creates an entity only if it doesn't already exist (idempotent create)
// The db/id attribute must be present in the attributes to identify the entity
// Returns (entity, true, nil) if created, (entity, false, nil) if already exists
func (s *EtcdStore) EnsureEntity(
	ctx context.Context,
	entity *Entity,
	opts ...EntityOption,
) (*Entity, bool, error) {
	// Extract ID from db/id attribute
	id, err := extractEntityId(entity.attrs)
	if err != nil {
		return nil, false, err
	}

	// Check if entity already exists
	existing, err := s.GetEntity(ctx, id)
	if err == nil {
		// Entity exists, return it with created=false
		return existing, false, nil
	}

	// Entity doesn't exist, check that error is NotFound
	if !errors.Is(err, cond.ErrNotFound{}) {
		return nil, false, fmt.Errorf("failed to check entity existence: %w", err)
	}

	// Create the entity
	entity, err = s.CreateEntity(ctx, entity, opts...)
	if err != nil {
		// Check if it was created by another concurrent operation
		if errors.Is(err, cond.ErrConflict{}) || errors.Is(err, ErrEntityAlreadyExists) {
			existing, getErr := s.GetEntity(ctx, id)
			if getErr == nil {
				return existing, false, nil
			}
		}
		return nil, false, err
	}

	return entity, true, nil
}

// deleteEntityRaceHook is a test seam. When non-nil, DeleteEntity invokes it
// after reading the entity but before committing the guarded delete transaction,
// letting a white-box test deterministically interleave a concurrent write into
// the revision-guard window. It is always nil in production.
var deleteEntityRaceHook func()

// DeleteEntity implements Store interface.
//
// The delete is atomic: the entity key, its index (collection) entries, and its
// unique-value keys are all removed in a single revision-guarded transaction. If
// they were removed separately (as they once were, with the index deletes issued
// as immediate unconditional Deletes before the entity-key txn), a concurrent
// no-OCC patch could slip into the gap and re-add index entries after the entity
// was gone, leaking a stale index entry (MIR-1334).
//
// Because the txn is guarded on the entity's ModRevision, a concurrent write that
// bumps the revision between our read and the txn makes it fail. Pools are patched
// on every reconcile tick via the no-OCC path, so a single-shot delete could lose
// that race repeatedly; we re-read and retry a bounded number of times so the
// delete wins against a busy reconcile loop instead of spuriously reporting the
// entity as already gone.
func (s *EtcdStore) DeleteEntity(ctx context.Context, id Id) error {
	const maxDeleteRetries = 3
	for attempt := 0; ; attempt++ {
		entity, err := s.GetEntity(ctx, id)
		if err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				return nil
			}
			return err
		}

		// Collect all indexed attributes including nested ones within components
		indexedAttrs, err := s.collectIndexedAttributes(ctx, entity.attrs)
		if err != nil {
			return err
		}

		// Test seam: exercise the read-before-txn window deterministically. This
		// is the exact gap a concurrent no-OCC patch could slip into (MIR-1334),
		// so a test can inject one here to prove the delete stays atomic. Always
		// nil in production.
		if deleteEntityRaceHook != nil {
			deleteEntityRaceHook()
		}

		key := s.buildKey(id)

		// Build all delete operations: entity key + index entries + unique-value
		// keys, so the whole delete commits (or fails) as one guarded txn.
		ops := []clientv3.Op{clientv3.OpDelete(key)}

		for _, attrs := range indexedAttrs {
			for _, attr := range attrs {
				ops = append(ops, s.deleteFromCollectionOp(entity, attr.CAS()))
			}
		}

		uniqueAttrs, err := s.collectUniqueAttrs(ctx, entity)
		if err != nil {
			return err
		}
		for _, attr := range uniqueAttrs {
			ops = append(ops, s.deleteUniqueOp(attr))
		}

		// Guard on the entity's ModRevision so the delete is all-or-nothing and
		// can't interleave with a concurrent patch.
		txnResp, err := s.client.Txn(ctx).
			If(clientv3.Compare(clientv3.ModRevision(key), "=", entity.GetRevision())).
			Then(ops...).
			Commit()

		if err != nil {
			return fmt.Errorf("failed to delete entity from etcd: %w", err)
		}

		if txnResp.Succeeded {
			return nil
		}

		// The guard failed: either the entity was concurrently deleted, or a
		// concurrent write bumped its revision. Re-read and retry; GetEntity
		// returning NotFound on the next pass resolves the former.
		if attempt >= maxDeleteRetries {
			s.log.Error("delete lost revision race after retries", "id", id)
			return cond.Conflict("entity", id)
		}
		s.log.Info("delete revision race, re-reading and retrying",
			"id", id, "attempt", attempt+1)
	}
}

// ClearSchemaCache clears the in-memory schema cache, forcing subsequent
// GetAttributeSchema calls to re-read from etcd. This is used before reindexing
// to ensure fresh schema definitions are used.
func (s *EtcdStore) ClearSchemaCache() {
	s.mu.Lock()
	s.schemaCache = make(map[Id]*AttributeSchema)
	s.mu.Unlock()
}

// GetAttributeSchema implements Store interface
func (s *EtcdStore) GetAttributeSchema(ctx context.Context, id Id) (*AttributeSchema, error) {
	// Check the cache first
	s.mu.RLock()
	schema, ok := s.schemaCache[id]
	s.mu.RUnlock()

	if ok {
		if schema.Type == "" {
			panic(fmt.Sprintf("attribute schema %q in cache has empty type", id))
		}
		return schema, nil
	}

	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}

	schema, err = convertEntityToSchema(ctx, s, entity)
	if err != nil {
		return nil, err
	}

	// Update the cache
	s.mu.Lock()
	s.schemaCache[schema.ID] = schema
	s.mu.Unlock()

	return schema, nil
}

func (s *EtcdStore) addToCollectionSessionOp(entity *Entity, collection, suffix string, sid int64) clientv3.Op {
	key := base58.Encode([]byte(entity.Id()))
	colKey := tr.Replace(collection)

	key = fmt.Sprintf("%s/collections/%s/%s/%s", s.prefix, colKey, key, suffix)

	return clientv3.OpPut(key, entity.Id().String(), clientv3.WithLease(clientv3.LeaseID(sid)))
}

// plainIndexLease returns the lease that an entity's plain (durable) index
// entries should carry. When the entity key itself is leased to a session
// (BondToSession sets bind), its index entries must share that lease so etcd
// garbage-collects them atomically with the entity. Otherwise the unleased
// index entry outlives the entity when the session lease expires, leaving a
// stale "phantom" index entry pointing at a deleted entity. For unbound
// (durable) entities this returns NoLease so the index entry stays durable and
// is cleaned up by DeleteEntity. See MIR-1320.
func plainIndexLease(bind bool, sid int64) clientv3.LeaseID {
	if bind {
		return clientv3.LeaseID(sid)
	}
	return clientv3.NoLease
}

func (s *EtcdStore) addToCollectionOp(entity *Entity, collection string, lease clientv3.LeaseID) clientv3.Op {
	key := base58.Encode([]byte(entity.Id()))
	colKey := tr.Replace(collection)

	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	if lease != clientv3.NoLease {
		return clientv3.OpPut(key, entity.Id().String(), clientv3.WithLease(lease))
	}
	return clientv3.OpPut(key, entity.Id().String())
}

func (s *EtcdStore) deleteFromCollectionOp(entity *Entity, collection string) clientv3.Op {
	key := base58.Encode([]byte(entity.Id()))
	colKey := tr.Replace(collection)

	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	return clientv3.OpDelete(key)
}

func (s *EtcdStore) ListIndex(ctx context.Context, attr Attr) ([]Id, error) {
	ids, _, err := s.ListIndexRevision(ctx, attr)
	return ids, err
}

// ListIndexRevision lists the ids matching attr and returns the etcd revision the
// listing was read at. The revision lets a caller resume a watch from that point
// (gap-free) instead of re-reconciling on every reconnect.
func (s *EtcdStore) ListIndexRevision(ctx context.Context, attr Attr) ([]Id, int64, error) {
	if attr.ID == DBId {
		if attr.Value.Kind() != KindId {
			return nil, 0, cond.ValidationFailure("attribute", "invalid value type for ID")
		}

		id := attr.Value.Id()

		gr, err := s.client.Get(ctx, s.buildKey(id), clientv3.WithCountOnly())
		if err != nil {
			return nil, 0, err
		}

		if gr.Count == 0 {
			return nil, gr.Header.Revision, nil
		}

		return []Id{id}, gr.Header.Revision, nil
	}

	schema, err := s.GetAttributeSchema(ctx, attr.ID)
	if err != nil {
		return nil, 0, err
	}

	if !schema.Index {
		return nil, 0, fmt.Errorf("attribute %s is not indexed", attr.ID)
	}

	return s.listCollectionRevision(ctx, attr.CAS())
}

// IndexPage is one bounded page of an index listing.
type IndexPage struct {
	Ids []Id

	// Cursor resumes the listing after the last entry in this page, and is
	// empty once the index is exhausted. It is opaque: it encodes a store key,
	// not an entity id, because the index is ordered by its own keys and making
	// an entity-id cursor work would mean sorting the whole index on every page.
	Cursor string

	// Total counts the entries in the index and is filled only when the caller
	// starts from the head, since that is the only point a total is meaningful.
	// It counts index entries, which equals the entity count unless an entity
	// somehow holds two entries in the same index.
	Total int64

	// Revision is the store revision the page was read at.
	Revision int64
}

// ListIndexPage reads up to limit ids from attr's index, resuming after cursor.
//
// Pages are not pinned to a common revision across calls, so an entity written
// between two pages can be missed or repeated. Pinning would need the caller to
// carry a revision that outlives the compaction window, which is the wrong
// trade for a debug listing; within a single page the read is consistent.
func (s *EtcdStore) ListIndexPage(ctx context.Context, attr Attr, cursor string, limit int64) (*IndexPage, error) {
	// A db/id index holds at most one entry, so there is nothing to page.
	if attr.ID == DBId {
		ids, rev, err := s.ListIndexRevision(ctx, attr)
		if err != nil {
			return nil, err
		}

		return &IndexPage{Ids: ids, Total: int64(len(ids)), Revision: rev}, nil
	}

	prefix, err := s.IndexPrefix(ctx, attr)
	if err != nil {
		return nil, err
	}

	page := &IndexPage{}

	// Counting is the only thing here that scales with the index, so only pay
	// for it when it can actually tell the caller something. An unlimited read
	// returns everything, so the total is just what came back; and past the
	// first page the caller already has it.
	if cursor == "" && limit > 0 {
		cr, err := s.client.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithCountOnly())
		if err != nil {
			return nil, fmt.Errorf("failed to count index entries: %w", err)
		}

		page.Total = cr.Count
		page.Revision = cr.Header.Revision
	}

	// errPageFull stops the scan once we have what we asked for; scanPagedFunc
	// stops on any error from fn.
	errPageFull := errors.New("page full")

	seen := make(map[Id]struct{})
	lastKey := ""

	scanErr := scanPagedFunc(ctx, s.client, prefix, func(kv *mvccpb.KeyValue) error {
		lastKey = string(kv.Key)

		id := Id(kv.Value)
		if _, dup := seen[id]; dup {
			return nil
		}
		seen[id] = struct{}{}

		page.Ids = append(page.Ids, id)

		if limit > 0 && int64(len(page.Ids)) >= limit {
			return errPageFull
		}

		return nil
	}, withStartKey(cursor), withPageSize(pageSizeFor(limit)))

	switch {
	case scanErr == nil:
		// Ran to the end of the index, so there is nothing to resume from.
	case errors.Is(scanErr, errPageFull):
		page.Cursor = lastKey + "\x00"
	default:
		return nil, fmt.Errorf("failed to list index page: %w", scanErr)
	}

	// An unlimited read already holds the whole index, so the total is free.
	if limit <= 0 {
		page.Total = int64(len(page.Ids))
	}

	return page, nil
}

// pageSizeFor keeps the etcd page from overshooting a small limit while still
// using the default for an unbounded read.
func pageSizeFor(limit int64) int64 {
	if limit <= 0 || limit > defaultScanPageSize {
		return defaultScanPageSize
	}

	return limit
}

func (s *EtcdStore) IndexPrefix(ctx context.Context, attr Attr) (string, error) {
	schema, err := s.GetAttributeSchema(ctx, attr.ID)
	if err != nil {
		return "", err
	}

	if !schema.Index {
		return "", fmt.Errorf("attribute %s is not indexed", attr.ID)
	}

	return s.CollectionPrefix(ctx, attr.CAS())
}

func (s *EtcdStore) WatchIndex(ctx context.Context, attr Attr, fromRev int64) (clientv3.WatchChan, error) {
	if attr.ID == DBId {
		if attr.Value.Kind() != KindId {
			return nil, cond.ValidationFailure("attribute", "invalid value type for ID")
		}

		id := attr.Value.Id()
		key := s.buildKey(id)
		var wopts []clientv3.OpOption
		if fromRev > 0 {
			wopts = append(wopts, clientv3.WithRev(fromRev))
		}
		wc := s.client.Watch(ctx, key, wopts...)

		ret := make(chan clientv3.WatchResponse, 1)

		go func() {
			defer close(ret)

			for {
				select {
				case <-ctx.Done():
					return
				case wr, ok := <-wc:
					if !ok {
						return
					}

					ret <- clientv3.WatchResponse{
						Header: wr.Header,
						Events: []*clientv3.Event{
							{
								Type: clientv3.EventTypePut,
								Kv: &mvccpb.KeyValue{
									Key:   []byte(key),
									Value: []byte(id),
								},
							},
						},
					}
				}
			}
		}()

		return ret, nil
	}

	schema, err := s.GetAttributeSchema(ctx, attr.ID)
	if err != nil {
		return nil, err
	}

	if !schema.Index {
		return nil, fmt.Errorf("attribute %s is not indexed", attr.ID)
	}

	prefix, err := s.CollectionPrefix(ctx, attr.CAS())
	if err != nil {
		return nil, err
	}

	// WithProgressNotify keeps the resume cursor fresh during idle periods;
	// WithRev resumes the event stream from a prior revision (gap-free) when set.
	opts := []clientv3.OpOption{clientv3.WithPrefix(), clientv3.WithPrevKV(), clientv3.WithProgressNotify()}
	if fromRev > 0 {
		opts = append(opts, clientv3.WithRev(fromRev))
	}

	return s.client.Watch(ctx, prefix, opts...), nil
}

var tr = strings.NewReplacer("/", "_", ":", "_")

func (s *EtcdStore) ListCollection(ctx context.Context, collection string) ([]Id, error) {
	ids, _, err := s.listCollectionRevision(ctx, collection)
	return ids, err
}

func (s *EtcdStore) listCollectionRevision(ctx context.Context, collection string) ([]Id, int64, error) {
	colKey := tr.Replace(collection)

	prefix := fmt.Sprintf("%s/collections/%s/", s.prefix, colKey)

	resp, err := s.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list entities from etcd: %w", err)
	}

	seen := make(map[Id]struct{})

	entities := make([]Id, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		id := Id(kv.Value)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		entities = append(entities, id)
	}

	return entities, resp.Header.Revision, nil
}

func (s *EtcdStore) CollectionPrefix(ctx context.Context, collection string) (string, error) {
	colKey := tr.Replace(collection)

	return fmt.Sprintf("%s/collections/%s/", s.prefix, colKey), nil
}

// BuildUniqueKeyForTest exposes buildUniqueKey for use in tests.
func (s *EtcdStore) BuildUniqueKeyForTest(attr Attr) string {
	return s.buildUniqueKey(attr)
}

// buildUniqueKey returns the etcd key for a unique-value constraint.
// The key is namespaced by the attribute's CAS to avoid collisions
// between different unique attributes that happen to share a value.
func (s *EtcdStore) buildUniqueKey(attr Attr) string {
	return fmt.Sprintf("%s/unique/%s", s.prefix, attr.CAS())
}

// addUniqueOp returns an etcd put operation that claims a unique value.
func (s *EtcdStore) addUniqueOp(attr Attr, entityId Id) clientv3.Op {
	return clientv3.OpPut(s.buildUniqueKey(attr), string(entityId))
}

// deleteUniqueOp returns an etcd delete operation that releases a unique value.
func (s *EtcdStore) deleteUniqueOp(attr Attr) clientv3.Op {
	return clientv3.OpDelete(s.buildUniqueKey(attr))
}

// uniqueExists checks whether a unique value is already claimed.
func (s *EtcdStore) uniqueExists(ctx context.Context, attr Attr) (bool, error) {
	resp, err := s.client.Get(ctx, s.buildUniqueKey(attr), clientv3.WithCountOnly())
	if err != nil {
		return false, fmt.Errorf("failed to check unique value existence: %w", err)
	}
	return resp.Count > 0, nil
}

// collectUniqueAttrs returns all attributes on the entity that have
// UniqueValue enforcement according to their schema.
func (s *EtcdStore) collectUniqueAttrs(ctx context.Context, entity *Entity) ([]Attr, error) {
	var unique []Attr
	for _, attr := range entity.attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			continue // schema entities won't have schemas for their own attrs
		}
		if schema.Unique == uniqVal {
			unique = append(unique, attr)
		}
	}
	return unique, nil
}

// GetOneIndex looks up a single entity by an indexed attribute value.
// Returns the entity ID if exactly one match is found, or ErrNotFound.
func (s *EtcdStore) GetOneIndex(ctx context.Context, attr Attr) (Id, error) {
	schema, err := s.GetAttributeSchema(ctx, attr.ID)
	if err != nil {
		return "", err
	}

	if !schema.Index {
		return "", fmt.Errorf("attribute %s is not indexed", attr.ID)
	}

	colKey := tr.Replace(attr.CAS())
	prefix := fmt.Sprintf("%s/collections/%s/", s.prefix, colKey)

	resp, err := s.client.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithLimit(1))
	if err != nil {
		return "", fmt.Errorf("failed to query index: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return "", cond.NotFound("entity", attr.Value.String())
	}

	return Id(resp.Kvs[0].Value), nil
}

func (s *EtcdStore) CreateSession(ctx context.Context, ttl int64) ([]byte, error) {
	resp, err := s.client.Grant(ctx, ttl)
	if err != nil {
		return nil, fmt.Errorf("failed to create lease: %w", err)
	}

	return binary.AppendVarint(nil, int64(resp.ID)), nil
}

func (s *EtcdStore) RevokeSession(ctx context.Context, session []byte) error {
	id, _ := binary.Varint(session)
	_, err := s.client.Revoke(ctx, clientv3.LeaseID(id))
	if err != nil {
		return fmt.Errorf("failed to revoke lease: %w", err)
	}

	return nil
}

func (s *EtcdStore) PingSession(ctx context.Context, session []byte) error {
	id, _ := binary.Varint(session)
	_, err := s.client.KeepAliveOnce(ctx, clientv3.LeaseID(id))
	if err != nil {
		return fmt.Errorf("failed to assert lease: %w", err)
	}

	return nil
}

func (s *EtcdStore) ListSessionEntities(ctx context.Context, session []byte) ([]Id, error) {
	id, _ := binary.Varint(session)

	resp, err := s.client.TimeToLive(ctx, clientv3.LeaseID(id), clientv3.WithAttachedKeys())
	if err != nil {
		return nil, err
	}

	var ret []Id

	entprefix := fmt.Sprintf("%s/entity/", s.prefix)

	for _, bkey := range resp.Keys {
		if !strings.HasPrefix(string(bkey), entprefix) {
			continue
		}

		key := strings.TrimPrefix(string(bkey), entprefix)

		sess := strings.IndexByte(key, '/')
		if sess != -1 {
			key = key[:sess]
		}

		id, err := base58.Decode(key)
		if err != nil {
			return nil, fmt.Errorf("failed to decode entity ID: %w", err)
		}

		ret = append(ret, Id(id))
	}

	return ret, nil
}

func (s *EtcdStore) CreateLease(ctx context.Context, ttl int64) (int64, error) {
	resp, err := s.client.Grant(ctx, ttl)
	if err != nil {
		return 0, fmt.Errorf("failed to create lease: %w", err)
	}

	return int64(resp.ID), nil
}

func (s *EtcdStore) RevokeLease(ctx context.Context, lease int64) error {
	_, err := s.client.Revoke(ctx, clientv3.LeaseID(lease))
	if err != nil {
		return fmt.Errorf("failed to revoke lease: %w", err)
	}

	return nil
}

func (s *EtcdStore) AssertLease(ctx context.Context, lease int64) error {
	_, err := s.client.KeepAliveOnce(ctx, clientv3.LeaseID(lease))
	if err != nil {
		return fmt.Errorf("failed to assert lease: %w", err)
	}

	return nil
}

// enumerateAllAttrs recursively enumerates all attributes including those nested in components.
// This allows indexing of nested component fields.
func enumerateAllAttrs(attrs []Attr) []Attr {
	var result []Attr
	for _, attr := range attrs {
		result = append(result, attr)

		// If this is a component, recursively enumerate its nested attributes
		if attr.Value.Kind() == KindComponent {
			comp := attr.Value.Component()
			if comp != nil {
				nestedAttrs := comp.Attrs()
				result = append(result, enumerateAllAttrs(nestedAttrs)...)
			}
		}
	}
	return result
}

// ListAllEntityIDs returns all entity IDs in the store
func (s *EtcdStore) ListAllEntityIDs(ctx context.Context) ([]Id, error) {
	prefix := fmt.Sprintf("%s/entity/", s.prefix)
	kvs, err := s.scanPaged(ctx, prefix, withKeysOnly())
	if err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}

	var ids []Id
	for _, kv := range kvs {
		key := string(kv.Key)
		// Skip session keys (they have /session/ in the path)
		if strings.Contains(key, "/session/") {
			continue
		}
		// Extract entity ID from key
		// Key format: /prefix/entity/base58(entityid)
		key = strings.TrimPrefix(key, prefix)
		if key == "" {
			continue
		}
		decoded, err := base58.Decode(key)
		if err != nil {
			s.log.Warn("failed to decode entity ID", "key", key, "error", err)
			continue
		}
		ids = append(ids, Id(decoded))
	}

	return ids, nil
}

// DeletePrefixCount deletes all keys with prefix and returns count
func (s *EtcdStore) DeletePrefixCount(ctx context.Context, prefix string) (int64, error) {
	resp, err := s.client.Delete(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return 0, fmt.Errorf("failed to delete prefix: %w", err)
	}
	return resp.Deleted, nil
}

// Prefix returns the etcd prefix used by this store
func (s *EtcdStore) Prefix() string {
	return s.prefix
}

// Client returns the underlying etcd client
func (s *EtcdStore) Client() *clientv3.Client {
	return s.client
}
