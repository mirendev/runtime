package entity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/mr-tron/base58"
	clientv3 "go.etcd.io/etcd/client/v3"
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
	WatchEntity(ctx context.Context, id Id) (chan EntityOp, error)
	GetAttributeSchema(ctx context.Context, name Id) (*AttributeSchema, error)
	CreateEntity(ctx context.Context, attributes []Attr) (*Entity, error)
	UpdateEntity(ctx context.Context, id Id, attributes []Attr) (*Entity, error)
	DeleteEntity(ctx context.Context, id Id) error
	WatchIndex(ctx context.Context, attr Attr) (clientv3.WatchChan, error)
	ListIndex(ctx context.Context, attr Attr) ([]Id, error)
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
		return store.saveEntity(ctx, e)
	})

	if err != nil {
		return nil, err
	}

	return store, nil
}

// buildKey creates a full etcd key for an entity
func (s *EtcdStore) buildKey(id string) string {
	return fmt.Sprintf("%s/%s", s.prefix, id)
}

// CreateEntity implements Store interface
func (s *EtcdStore) CreateEntity(ctx context.Context, attributes []Attr) (*Entity, error) {
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

	var collections []Attr

	for _, attr := range entity.Attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, err
		}

		if schema.Index {
			collections = append(collections, attr)
		}
	}

	// A default ID
	if entity.ID == "" {
		entity.ID = Id(idgen.GenNS("e"))
	}

	data, err := encoder.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal entity: %w", err)
	}

	key := s.buildKey(entity.ID.String())

	// Use Txn to check that the key doesn't exist yet
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string(data))).
		Commit()

	if err != nil {
		return nil, fmt.Errorf("failed to create entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		return nil, fmt.Errorf("creating %s: %w", entity.ID, ErrEntityAlreadyExists)
	}

	entity.Revision = txnResp.Header.Revision

	// Gotta be sure we add to collections AFTER creating the entity so that
	// fast readers don't see the entity before it's fully created
	for _, attr := range collections {
		err := s.addToCollection(entity, attr.CAS())
		if err != nil {
			return nil, err
		}
	}

	return entity, nil
}

func (s *EtcdStore) saveEntity(ctx context.Context, entity *Entity) error {
	entity.Fixup()

	data, err := encoder.Marshal(entity)
	if err != nil {
		return fmt.Errorf("failed to marshal entity: %w", err)
	}

	key := s.buildKey(entity.ID.String())

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

// GetEntity implements Store interface
func (s *EtcdStore) GetEntity(ctx context.Context, id Id) (*Entity, error) {
	key := s.buildKey(id.String())

	resp, err := s.client.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity from etcd: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return nil, ErrNotFound
	}

	var entity Entity

	err = decoder.Unmarshal(resp.Kvs[0].Value, &entity)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize entity: %w", err)
	}

	entity.Revision = resp.Kvs[0].ModRevision

	entity.postUnmarshal()

	return &entity, nil
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
	key := s.buildKey(id.String())
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
func (s *EtcdStore) UpdateEntity(ctx context.Context, id Id, attributes []Attr) (*Entity, error) {
	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}

	var collections []Attr

	// Validate attributes
	for _, attr := range attributes {
		if err := s.validator.ValidateAttribute(ctx, &attr); err != nil {
			return nil, fmt.Errorf("invalid attribute %s: %w", attr.ID, err)
		}

		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, err
		}

		if schema.Index {
			collections = append(collections, attr)
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

	data, err := encoder.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize entity: %w", err)
	}

	key := s.buildKey(entity.ID.String())

	// Use Txn to check that the key exists before updating
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), ">", 0)).
		Then(clientv3.OpPut(key, string(data))).
		Commit()

	if err != nil {
		return nil, fmt.Errorf("failed to update entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		return nil, errors.New("entity does not exist")
	}

	entity.Revision = txnResp.Header.Revision

	// Gotta be sure we add to collections AFTER creating the entity so that
	// fast readers don't see the entity before it's fully created
	for _, attr := range collections {
		err := s.addToCollection(entity, attr.CAS())
		if err != nil {
			return nil, err
		}
	}

	return entity, nil
}

// DeleteEntity implements Store interface
func (s *EtcdStore) DeleteEntity(ctx context.Context, id Id) error {
	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil
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

	key := s.buildKey(id.String())

	// Use Txn to check that the key exists before deleting
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), ">", 0)).
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

func (s *EtcdStore) addToCollection(entity *Entity, collection string) error {
	key := base58.Encode([]byte(entity.ID))
	colKey := tr.Replace(collection)

	ctx := context.Background()
	key = s.buildKey(fmt.Sprintf("collections/%s/%s", colKey, key))

	/*
		txnResp, err := s.client.Txn(ctx).
			If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
			Then(clientv3.OpPut(key, entity.ID.String())).
			Commit()
		if err != nil {
			return err
		}

		if !txnResp.Succeeded {
			return fmt.Errorf("entity already exists: %s", key)
		}
	*/

	_, err := s.client.Put(ctx, key, entity.ID.String())
	return err
}

func (s *EtcdStore) deleteFromCollection(entity *Entity, collection string) error {
	key := base58.Encode([]byte(entity.ID))
	colKey := tr.Replace(collection)

	ctx := context.Background()
	key = s.buildKey(fmt.Sprintf("collections/%s/%s", colKey, key))

	/*
		txnResp, err := s.client.Txn(ctx).
			If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
			Then(clientv3.OpPut(key, entity.ID.String())).
			Commit()
		if err != nil {
			return err
		}

		if !txnResp.Succeeded {
			return fmt.Errorf("entity already exists: %s", key)
		}
	*/

	_, err := s.client.Delete(ctx, key)
	return err
}

func (s *EtcdStore) ListIndex(ctx context.Context, attr Attr) ([]Id, error) {
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

	prefix := s.buildKey(fmt.Sprintf("collections/%s/", colKey))

	resp, err := s.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to list entities from etcd: %w", err)
	}

	entities := make([]Id, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		entities = append(entities, Id(kv.Value))
	}

	return entities, nil
}

func (s *EtcdStore) CollectionPrefix(ctx context.Context, collection string) (string, error) {
	colKey := tr.Replace(collection)

	return s.buildKey(fmt.Sprintf("collections/%s/", colKey)), nil
}
