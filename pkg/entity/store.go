package entity

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/fxamacker/cbor/v2"
	"github.com/mr-tron/base58"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdStore implements Store using etcd
type EtcdStore struct {
	client    *clientv3.Client
	validator *Validator
	prefix    string

	schemaCache map[EntityId]*AttributeSchema
	mu          sync.RWMutex
}

// NewEtcdStore creates a new etcd-backed entity store
func NewEtcdStore(client *clientv3.Client, prefix string) (*EtcdStore, error) {
	store := &EtcdStore{
		client:      client,
		prefix:      prefix,
		schemaCache: make(map[EntityId]*AttributeSchema),
	}

	store.validator = NewValidator(store)

	err := InitSystemEntities(store.saveEntity)
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
func (s *EtcdStore) CreateEntity(attributes []Attr) (*Entity, error) {
	// Validate attributes
	if err := s.validator.ValidateAttributes(attributes); err != nil {
		return nil, err
	}

	entity := &Entity{
		Attrs:     attributes,
		CreatedAt: now(),
		UpdatedAt: now(),
	}

	if err := entity.Fixup(); err != nil {
		return nil, err
	}

	for _, attr := range entity.Attrs {
		schema, err := s.GetAttributeSchema(attr.ID)
		if err != nil {
			return nil, err
		}

		if schema.Index {
			err := s.addToCollection(entity, fmt.Sprintf("%s:%v", attr.ID, attr.Value))
			if err != nil {
				return nil, err
			}
		}
	}

	entity.Fixup()

	ctx := context.Background()

	data, err := cbor.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal entity: %w", err)
	}

	key := s.buildKey(entity.GetID())

	// Use Txn to check that the key doesn't exist yet
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string(data))).
		Commit()

	if err != nil {
		return nil, fmt.Errorf("failed to create entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		return nil, fmt.Errorf("creating %s: %w", entity.GetID(), ErrEntityAlreadyExists)
	}

	entity.Revision = int(txnResp.Header.Revision)

	return entity, nil
}

func (s *EtcdStore) saveEntity(entity *Entity) error {
	ctx := context.Background()

	entity.Fixup()

	data, err := cbor.Marshal(entity)
	if err != nil {
		return fmt.Errorf("failed to marshal entity: %w", err)
	}

	key := s.buildKey(entity.GetID())

	// Use Txn to check that the key doesn't exist yet
	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string(data))).
		Commit()

	if err != nil {
		return fmt.Errorf("failed to create entity in etcd: %w", err)
	}

	if !txnResp.Succeeded {
		return fmt.Errorf("creating %s: %w", entity.GetID(), ErrEntityAlreadyExists)
	}

	return nil
}

// GetEntity implements Store interface
func (s *EtcdStore) GetEntity(id EntityId) (*Entity, error) {
	ctx := context.Background()
	key := s.buildKey(string(id))

	resp, err := s.client.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity from etcd: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return nil, errors.New("entity not found")
	}

	var entity Entity

	entity.ID = string(id)
	entity.Revision = int(resp.Kvs[0].Version)

	err = cbor.Unmarshal(resp.Kvs[0].Value, &entity)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize entity: %w", err)
	}

	entity.postUnmarshal()

	return &entity, nil
}

// UpdateEntity implements Store interface
func (s *EtcdStore) UpdateEntity(id EntityId, attributes []Attr) (*Entity, error) {
	entity, err := s.GetEntity(id)
	if err != nil {
		return nil, err
	}

	// Validate attributes
	for _, attr := range attributes {
		if err := s.validator.ValidateAttribute(attr); err != nil {
			return nil, fmt.Errorf("invalid attribute %s: %w", attr.ID, err)
		}
	}

	entity.Attrs = append(entity.Attrs, attributes...)
	entity.UpdatedAt = now()

	ctx := context.Background()

	entity.Fixup()

	data, err := cbor.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize entity: %w", err)
	}

	key := s.buildKey(entity.GetID())

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

	entity.Revision = int(txnResp.Header.Revision)

	return entity, nil
}

// DeleteEntity implements Store interface
func (s *EtcdStore) DeleteEntity(id EntityId) error {
	ctx := context.Background()

	key := s.buildKey(string(id))

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
func (s *EtcdStore) GetAttributeSchema(id EntityId) (*AttributeSchema, error) {
	// Check the cache first
	s.mu.RLock()
	schema, ok := s.schemaCache[id]
	s.mu.RUnlock()

	if ok {
		return schema, nil
	}
	entity, err := s.GetEntity(id)
	if err != nil {
		return nil, err
	}

	schema, err = convertEntityToSchema(s, entity)
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
	colKey := base58.Encode([]byte(collection))

	ctx := context.Background()
	key = s.buildKey(fmt.Sprintf("collections/%s/%s", colKey, key))

	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, string([]byte(entity.ID)))).
		Commit()
	if err != nil {
		return err
	}

	if !txnResp.Succeeded {
		return fmt.Errorf("entity already exists: %s", key)
	}

	return nil
}

func (s *EtcdStore) ListIndex(id EntityId, val any) ([]EntityId, error) {
	schema, err := s.GetAttributeSchema(id)
	if err != nil {
		return nil, err
	}

	if !schema.Index {
		return nil, fmt.Errorf("attribute %s is not indexed", id)
	}

	return s.ListCollection(fmt.Sprintf("%s:%v", id, val))
}

func (s *EtcdStore) ListCollection(collection string) ([]EntityId, error) {
	colKey := base58.Encode([]byte(collection))

	prefix := s.buildKey(fmt.Sprintf("collections/%s/", colKey))

	ctx := context.Background()

	resp, err := s.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to list entities from etcd: %w", err)
	}

	entities := make([]EntityId, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		entities = append(entities, EntityId(kv.Value))
	}

	return entities, nil
}
