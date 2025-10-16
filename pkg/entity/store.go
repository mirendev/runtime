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
	GetEntities(ctx context.Context, ids []Id) ([]*Entity, error)
	WatchEntity(ctx context.Context, id Id) (chan EntityOp, error)
	GetAttributeSchema(ctx context.Context, name Id) (*AttributeSchema, error)
	CreateEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error)
	UpdateEntity(ctx context.Context, id Id, entity *Entity, opts ...EntityOption) (*Entity, error)
	ReplaceEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error)
	PatchEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error)
	EnsureEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, bool, error)
	DeleteEntity(ctx context.Context, id Id) error
	WatchIndex(ctx context.Context, attr Attr) (clientv3.WatchChan, error)
	ListIndex(ctx context.Context, attr Attr) ([]Id, error)

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

	// Use Txn to check that the key doesn't exist yet
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string(data))).
		Commit()

	if err != nil {
		return fmt.Errorf("failed to create entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		return fmt.Errorf("creating %s: %w", entity.Id(), ErrEntityAlreadyExists)
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

	// Validate attributes
	if err := s.validator.ValidateEntity(ctx, entity); err != nil {
		return nil, err
	}

	var primary, session []Attr

	var coltxopt []clientv3.Op

	var (
		sid      int64
		sessPart string
	)

	if len(o.session) > 0 {
		sid, _ = binary.Varint(o.session)
		sessPart = base58.Encode(o.session)
	}

	for _, attr := range entity.attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, err
		}

		if schema.Index {
			coltxopt = append(coltxopt, s.addToCollectionOp(entity, attr.CAS()))

			if sessPart != "" {
				coltxopt = append(coltxopt, s.addToCollectionSessionOp(entity, attr.CAS(), sessPart, sid))
			}
		}

		if schema.Session {
			session = append(session, attr)
		} else {
			primary = append(primary, attr)
		}
	}

	entity.attrs = primary

	// Build entity save operations
	key := s.buildKey(entity.Id())
	txopt, err := s.buildEntitySaveOps(entity, key, primary, session, &o)
	if err != nil {
		return nil, err
	}

	txopt = append(txopt, coltxopt...)

	// Use Txn to check that the key doesn't exist yet
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(txopt...).
		Else(clientv3.OpGet(key)).
		Commit()

	if err != nil {
		return nil, fmt.Errorf("failed to create entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		if len(txnResp.Responses) == 1 {
			// If the current value of the entity has the same attributes as
			// the entity we were trying to store, then we're all done!
			rng := txnResp.Responses[0].GetResponseRange()

			var curr Entity

			if decoder.Unmarshal(rng.Kvs[0].Value, &curr) == nil {
				s.log.Debug("entity already exists, checking if attrs match", "id", entity.Id())
				if slices.EqualFunc(curr.attrs, entity.attrs, func(a, b Attr) bool {
					return a.Equal(b)
				}) {
					s.log.Debug("attrs match, so returning success", "id", entity.Id(), "revision", rng.Header.Revision)
					entity.SetRevision(rng.Header.Revision)
					return entity, nil
				}
			}

			if o.overwrite {
				txnResp, err = s.client.Txn(ctx).
					Then(txopt...).
					Else(clientv3.OpGet(key)).
					Commit()
				if err != nil {
					return nil, fmt.Errorf("failed to create entity in etcd (on overwrite): %w", err)
				}

				entity.SetRevision(txnResp.Header.Revision)
				return entity, nil
			}
		}

		s.log.Error("failed to create entity in etcd", "id", entity.Id())
		return nil, cond.Conflict("entity", entity.Id())
	}

	entity.SetRevision(txnResp.Header.Revision)

	return entity, nil
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
	wc := s.client.Watch(ctx, key)

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

	// Keep track of original indexed attributes for removal
	originalIndexedAttrs := make(map[Id][]Attr)
	for _, attr := range entity.attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get attribute schema: %w", err)
		}
		if schema.Index {
			originalIndexedAttrs[attr.ID] = append(originalIndexedAttrs[attr.ID], attr)
		}
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

	err = entity.Update(changes.attrs)
	if err != nil {
		return nil, fmt.Errorf("failed to update entity: %w", err)
	}

	err = s.validator.ValidateAttributes(ctx, entity.attrs)
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

	var primary, session []Attr

	var coltxopt []clientv3.Op

	// Build map of new indexed attributes
	newIndexedAttrs := make(map[Id][]Attr)
	for _, attr := range entity.attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get attribute schema: %w", err)
		}

		if schema.Index {
			newIndexedAttrs[attr.ID] = append(newIndexedAttrs[attr.ID], attr)
		}

		if schema.Session {
			session = append(session, attr)
		} else {
			primary = append(primary, attr)
		}
	}

	// Compare old and new indexed attributes and only update collections when values change
	for attrID, oldAttrs := range originalIndexedAttrs {
		newAttrs := newIndexedAttrs[attrID]

		// Remove old attributes that are no longer present or have changed values
		for _, oldAttr := range oldAttrs {
			found := slices.ContainsFunc(newAttrs, oldAttr.Equal)
			if !found {
				coltxopt = append(coltxopt, s.deleteFromCollectionOp(entity, oldAttr.CAS()))
			}
		}
	}

	// All new indexed attributes should update their respective indexes so any watchers will get notified
	for _, newAttrs := range newIndexedAttrs {
		for _, newAttr := range newAttrs {
			coltxopt = append(coltxopt, s.addToCollectionOp(entity, newAttr.CAS()))
			// And if this is a session-bound update, add a subordinate index entry
			// so that watchers will be updated when the lease expires
			if sessPart != "" {
				coltxopt = append(coltxopt, s.addToCollectionSessionOp(entity, newAttr.CAS(), sessPart, sid))
			}
		}
	}

	entity.attrs = primary

	// Build entity save operations
	key := s.buildKey(entity.Id())
	txopt, err := s.buildEntitySaveOps(entity, key, primary, session, &o)
	if err != nil {
		return nil, err
	}

	txopt = append(txopt, coltxopt...)

	// Use Txn to check that the key exists before updating
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", entity.GetRevision())).
		Then(txopt...).
		Commit()

	if err != nil {
		return nil, fmt.Errorf("failed to update entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		s.log.Error("failed to update entity in etcd", "error", err, "id", entity.Id())
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
	for _, attr := range attrs {
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
func (s *EtcdStore) separateSessionAttributes(ctx context.Context, attrs []Attr) (primary, session []Attr, indexedAttrs map[Id][]Attr, err error) {
	indexedAttrs = make(map[Id][]Attr)
	for _, attr := range attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get attribute schema: %w", err)
		}

		if schema.Index {
			indexedAttrs[attr.ID] = append(indexedAttrs[attr.ID], attr)
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
func (s *EtcdStore) buildCollectionOps(entity *Entity, originalIndexedAttrs, newIndexedAttrs map[Id][]Attr, sessPart string, sid int64) []clientv3.Op {
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
			ops = append(ops, s.addToCollectionOp(entity, newAttr.CAS()))
			if sessPart != "" {
				ops = append(ops, s.addToCollectionSessionOp(entity, newAttr.CAS(), sessPart, sid))
			}
		}
	}

	return ops
}

// buildEntitySaveOps builds etcd operations for saving entity data (primary and session attributes)
func (s *EtcdStore) buildEntitySaveOps(entity *Entity, key string, primary, session []Attr, o *entityOpts) ([]clientv3.Op, error) {
	var ops []clientv3.Op

	entity.attrs = primary
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

	// Get current entity to check revision
	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}

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

	// Validate replacement attributes
	if err := s.validator.ValidateAttributes(ctx, repl.attrs); err != nil {
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
	coltxopt := s.buildCollectionOps(repl, originalIndexedAttrs, newIndexedAttrs, sessPart, sid)

	// Build entity save operations
	key := s.buildKey(repl.Id())
	txopt, err := s.buildEntitySaveOps(repl, key, primary, session, &o)
	if err != nil {
		return nil, err
	}

	txopt = append(txopt, coltxopt...)

	// Use Txn to check that the entity hasn't changed
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", repl.GetRevision())).
		Then(txopt...).
		Commit()

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

	// Get current entity
	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}

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

	err = entity.Update(current.attrs)
	if err != nil {
		return nil, fmt.Errorf("failed to update entity: %w", err)
	}

	err = s.validator.ValidateAttributes(ctx, entity.attrs)
	if err != nil {
		return nil, err
	}

	// Separate primary and session attributes, collect new indexed attrs
	primary, session, newIndexedAttrs, err := s.separateSessionAttributes(ctx, entity.attrs)
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
	coltxopt := s.buildCollectionOps(entity, originalIndexedAttrs, newIndexedAttrs, sessPart, sid)

	entity.attrs = primary

	// Build entity save operations
	key := s.buildKey(entity.Id())
	txopt, err := s.buildEntitySaveOps(entity, key, primary, session, &o)
	if err != nil {
		return nil, err
	}

	txopt = append(txopt, coltxopt...)

	// Use Txn to check that the key exists before updating
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", entity.GetRevision())).
		Then(txopt...).
		Commit()

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

// DeleteEntity implements Store interface
func (s *EtcdStore) DeleteEntity(ctx context.Context, id Id) error {
	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil
		}
		return err
	}

	for _, attr := range entity.attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return err
		}

		if schema.Index {
			// TODO: Batch this as an op into the below txn
			err := s.deleteFromCollection(entity, attr.CAS())
			if err != nil {
				return fmt.Errorf("failed to delete entity from collection: %w", err)
			}
		}
	}

	key := s.buildKey(id)

	// Use Txn to check that the key exists before deleting
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", entity.GetRevision())).
		Then(clientv3.OpDelete(key)).
		Commit()

	if err != nil {
		return fmt.Errorf("failed to delete entity from etcd: %w", err)
	}

	if !txnResp.Succeeded {
		return errors.New("entity does not exist")
	}

	return nil
}

// GetAttributeSchema implements Store interface
func (s *EtcdStore) GetAttributeSchema(ctx context.Context, id Id) (*AttributeSchema, error) {
	// Check the cache first
	s.mu.RLock()
	schema, ok := s.schemaCache[id]
	s.mu.RUnlock()

	if ok {
		if schema.Type == "" {
			panic("attribute schema in cache has empty type")
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

func (s *EtcdStore) addToCollectionOp(entity *Entity, collection string) clientv3.Op {
	key := base58.Encode([]byte(entity.Id()))
	colKey := tr.Replace(collection)

	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	return clientv3.OpPut(key, entity.Id().String())
}

func (s *EtcdStore) deleteFromCollectionOp(entity *Entity, collection string) clientv3.Op {
	key := base58.Encode([]byte(entity.Id()))
	colKey := tr.Replace(collection)

	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	return clientv3.OpDelete(key)
}

func (s *EtcdStore) deleteFromCollection(entity *Entity, collection string) error {
	key := base58.Encode([]byte(entity.Id()))
	colKey := tr.Replace(collection)

	ctx := context.Background()
	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	_, err := s.client.Delete(ctx, key)
	return err
}

func (s *EtcdStore) ListIndex(ctx context.Context, attr Attr) ([]Id, error) {
	if attr.ID == DBId {
		if attr.Value.Kind() != KindId {
			return nil, cond.ValidationFailure("attribute", "invalid value type for ID")
		}

		id := attr.Value.Id()

		gr, err := s.client.Get(ctx, s.buildKey(id), clientv3.WithCountOnly())
		if err != nil {
			return nil, err
		}

		if gr.Count == 0 {
			return nil, nil
		}

		return []Id{attr.Value.Id()}, nil
	}

	schema, err := s.GetAttributeSchema(ctx, attr.ID)
	if err != nil {
		return nil, err
	}

	if !schema.Index {
		return nil, fmt.Errorf("attribute %s is not indexed", attr.ID)
	}

	return s.ListCollection(ctx, attr.CAS())
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

func (s *EtcdStore) WatchIndex(ctx context.Context, attr Attr) (clientv3.WatchChan, error) {
	if attr.ID == DBId {
		if attr.Value.Kind() != KindId {
			return nil, cond.ValidationFailure("attribute", "invalid value type for ID")
		}

		id := attr.Value.Id()
		key := s.buildKey(id)
		wc := s.client.Watch(ctx, key)

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

	return s.client.Watch(ctx, prefix, clientv3.WithPrefix(), clientv3.WithPrevKV()), nil
}

var tr = strings.NewReplacer("/", "_", ":", "_")

func (s *EtcdStore) ListCollection(ctx context.Context, collection string) ([]Id, error) {
	colKey := tr.Replace(collection)

	prefix := fmt.Sprintf("%s/collections/%s/", s.prefix, colKey)

	resp, err := s.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to list entities from etcd: %w", err)
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

	return entities, nil
}

func (s *EtcdStore) CollectionPrefix(ctx context.Context, collection string) (string, error) {
	colKey := tr.Replace(collection)

	return fmt.Sprintf("%s/collections/%s/", s.prefix, colKey), nil
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
