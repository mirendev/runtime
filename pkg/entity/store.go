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
	"miren.dev/runtime/pkg/idgen"
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
	CreateEntity(ctx context.Context, attributes []Attr, opts ...EntityOption) (*Entity, error)
	UpdateEntity(ctx context.Context, id Id, attributes []Attr, opts ...EntityOption) (*Entity, error)
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

// NewEtcdStore creates a new etcd-backed entity store
func NewEtcdStore(ctx context.Context, log *slog.Logger, client *clientv3.Client, prefix string) (*EtcdStore, error) {
	store := &EtcdStore{
		log:         log,
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

	key := s.buildKey(entity.ID)

	// Use Txn to check that the key doesn't exist yet
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string(data))).
		Commit()

	if err != nil {
		return fmt.Errorf("failed to create entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		return fmt.Errorf("creating %s: %w", entity.ID, ErrEntityAlreadyExists)
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
	attributes []Attr,
	opts ...EntityOption,
) (*Entity, error) {
	var o entityOpts
	for _, opt := range opts {
		opt(&o)
	}

	entity := &Entity{
		Attrs:     attributes,
		CreatedAt: now(),
		UpdatedAt: now(),
	}

	// Validate attributes
	if err := s.validator.ValidateEntity(ctx, entity); err != nil {
		return nil, err
	}

	if err := entity.Fixup(); err != nil {
		return nil, err
	}

	// A default ID
	if entity.ID == "" {
		entity.ID = Id(idgen.GenNS("e"))
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

	for _, attr := range entity.Attrs {
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

	entity.Attrs = primary

	var txopt []clientv3.Op

	data, err := encoder.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal entity: %w", err)
	}

	key := s.buildKey(entity.ID)

	if o.bind {
		txopt = append(txopt, clientv3.OpPut(key, string(data), clientv3.WithLease(clientv3.LeaseID(sid))))
	} else {
		txopt = append(txopt, clientv3.OpPut(key, string(data)))
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

		txopt = append(txopt,
			clientv3.OpPut(skey, string(sdata), clientv3.WithLease(clientv3.LeaseID(sid))))
	}

	txopt = append(txopt, coltxopt...)

	var ifCmp []clientv3.Cmp

	if !o.overwrite {
		ifCmp = append(ifCmp, clientv3.Compare(clientv3.CreateRevision(key), "=", 0))
	}

	// Use Txn to check that the key doesn't exist yet
	txnResp, err := s.client.Txn(ctx).
		If(ifCmp...).
		Then(txopt...).
		Else(clientv3.OpGet(key)).
		Commit()

	if err != nil {
		return nil, fmt.Errorf("failed to create entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		if len(txnResp.Responses) == 1 {
			// If the current value of the entity has the same attributes as
			// we were going to store, then we're all done!
			rng := txnResp.Responses[0].GetResponseRange()

			var curr Entity

			if decoder.Unmarshal(rng.Kvs[0].Value, &curr) == nil {
				if slices.EqualFunc(curr.Attrs, entity.Attrs, func(a, b Attr) bool {
					return a.Equal(b)
				}) {
					entity.Revision = rng.Header.Revision
					return entity, nil
				}
			}
		}

		s.log.Error("failed to create entity in etcd", "error", err, "id", entity.ID)
		return nil, cond.Conflict("entity", entity.ID)
	}

	entity.Revision = txnResp.Header.Revision

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
		ttlr, err := s.client.Lease.TimeToLive(ctx, clientv3.LeaseID(resp.Kvs[0].Lease))
		if err == nil {
			entity.Attrs = append(entity.Attrs, Duration(TTL, time.Duration(ttlr.TTL)*time.Second))
		}
	}

	entity.Revision = resp.Kvs[0].ModRevision

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

		entity.Attrs = append(entity.Attrs, attrs...)
	}

	entity.postUnmarshal()

	return &entity, nil
}

func (s *EtcdStore) GetEntities(ctx context.Context, ids []Id) ([]*Entity, error) {
	if len(ids) == 0 {
		return []*Entity{}, nil
	}

	// Build all the ops for the transaction
	var ops []clientv3.Op
	for _, id := range ids {
		key := s.buildKey(id)
		ops = append(ops, clientv3.OpGet(key))
		ops = append(ops, clientv3.OpGet(key+"/session/", clientv3.WithPrefix()))
	}

	// Execute transaction
	tr, err := s.client.Txn(ctx).Then(ops...).Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to get entities from etcd: %w", err)
	}

	if !tr.Succeeded {
		return nil, fmt.Errorf("transaction failed")
	}

	// Process results
	entities := make([]*Entity, 0, len(ids))
	for i := 0; i < len(ids); i++ {
		// Each entity has 2 responses: primary and session
		primaryIdx := i * 2
		sessionIdx := i*2 + 1

		if primaryIdx >= len(tr.Responses) || sessionIdx >= len(tr.Responses) {
			continue
		}

		primaryResp := tr.Responses[primaryIdx].GetResponseRange()
		if len(primaryResp.Kvs) == 0 {
			// Entity not found, add nil to maintain order
			entities = append(entities, nil)
			s.log.Warn("failed to get primary entity from etcd", "id", ids[i])
			continue
		}

		var entity Entity
		err = decoder.Unmarshal(primaryResp.Kvs[0].Value, &entity)
		if err != nil {
			entities = append(entities, nil)
			continue
		}

		// Handle TTL if present
		if primaryResp.Kvs[0].Lease != 0 {
			ttlr, err := s.client.Lease.TimeToLive(ctx, clientv3.LeaseID(primaryResp.Kvs[0].Lease))
			if err == nil {
				entity.Attrs = append(entity.Attrs, Duration(TTL, time.Duration(ttlr.TTL)*time.Second))
			}
		}

		entity.Revision = primaryResp.Kvs[0].ModRevision

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

			entity.Attrs = append(entity.Attrs, attrs...)
		}

		entity.postUnmarshal()
		entities = append(entities, &entity)
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

						entity.Revision = event.Kv.ModRevision

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
	attributes []Attr,
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

	if o.fromRevision != 0 && entity.Revision != o.fromRevision {
		s.log.Error("entity revision mismatch", "expected", o.fromRevision, "actual", entity.Revision)
		return nil, cond.Conflict("entity", entity.ID)
	}

	// Validate attributes
	for _, attr := range attributes {
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

	err = entity.Update(attributes)
	if err != nil {
		return nil, fmt.Errorf("failed to update entity: %w", err)
	}

	err = s.validator.ValidateAttributes(ctx, entity.Attrs)
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

	for _, attr := range entity.Attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get attribute schema: %w", err)
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

	entity.Attrs = primary

	data, err := encoder.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize entity: %w", err)
	}

	key := s.buildKey(entity.ID)

	var txopt []clientv3.Op

	if o.bind {
		txopt = append(txopt, clientv3.OpPut(key, string(data), clientv3.WithLease(clientv3.LeaseID(sid))))
	} else {
		txopt = append(txopt, clientv3.OpPut(key, string(data)))
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
		txopt = append(txopt,
			clientv3.OpPut(skey, string(sdata), clientv3.WithLease(clientv3.LeaseID(sid))))
	}

	txopt = append(txopt, coltxopt...)

	// Use Txn to check that the key exists before updating
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", entity.Revision)).
		Then(txopt...).
		Commit()

	if err != nil {
		return nil, fmt.Errorf("failed to update entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		s.log.Error("failed to update entity in etcd", "error", err, "id", entity.ID)
		return nil, cond.Conflict("entity", entity.ID)
	}

	entity.Revision = txnResp.Header.Revision

	return entity, nil
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

	for _, attr := range entity.Attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return err
		}

		if schema.Index {
			err := s.deleteFromCollection(entity, attr.CAS())
			if err != nil {
				return fmt.Errorf("failed to delete entity from collection: %w", err)
			}
		}
	}

	key := s.buildKey(id)

	// Use Txn to check that the key exists before deleting
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", entity.Revision)).
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

func (s *EtcdStore) addToCollectionSession(entity *Entity, collection, suffix string, sid int64) error {
	key := base58.Encode([]byte(entity.ID))
	colKey := tr.Replace(collection)

	ctx := context.Background()
	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	key = fmt.Sprintf("%s/%s", key, suffix)

	_, err := s.client.Put(ctx, key, entity.ID.String(), clientv3.WithLease(clientv3.LeaseID(sid)))
	return err
}

func (s *EtcdStore) addToCollection(entity *Entity, collection string) error {
	key := base58.Encode([]byte(entity.ID))
	colKey := tr.Replace(collection)

	ctx := context.Background()
	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	_, err := s.client.Put(ctx, key, entity.ID.String())
	return err
}

func (s *EtcdStore) addToCollectionSessionOp(entity *Entity, collection, suffix string, sid int64) clientv3.Op {
	key := base58.Encode([]byte(entity.ID))
	colKey := tr.Replace(collection)

	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	key = fmt.Sprintf("%s/%s", key, suffix)

	return clientv3.OpPut(key, entity.ID.String(), clientv3.WithLease(clientv3.LeaseID(sid)))
}

func (s *EtcdStore) addToCollectionOp(entity *Entity, collection string) clientv3.Op {
	key := base58.Encode([]byte(entity.ID))
	colKey := tr.Replace(collection)

	key = fmt.Sprintf("%s/collections/%s/%s", s.prefix, colKey, key)

	return clientv3.OpPut(key, entity.ID.String())
}

func (s *EtcdStore) deleteFromCollection(entity *Entity, collection string) error {
	key := base58.Encode([]byte(entity.ID))
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

		gr, err := s.client.KV.Get(ctx, s.buildKey(id), clientv3.WithCountOnly())
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
