package entity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/mr-tron/base58"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdStore implements Store using etcd
type EtcdStore struct {
	client    *clientv3.Client
	validator *Validator
	prefix    string

	schemaCache map[Id]*AttributeSchema
	mu          sync.RWMutex
}

type Store interface {
	CreateEntity(ctx context.Context, attributes []Attr) (*Entity, error)
	UpdateEntity(ctx context.Context, id Id, attributes []Attr) (*Entity, error)
	DeleteEntity(ctx context.Context, id Id) error
}

var _ Store = (*EtcdStore)(nil)

// NewEtcdStore creates a new etcd-backed entity store
func NewEtcdStore(ctx context.Context, client *clientv3.Client, prefix string) (*EtcdStore, error) {
	store := &EtcdStore{
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

	for _, attr := range entity.Attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, err
		}

		if schema.Index {
			err := s.addToCollection(entity, attr.CAS())
			if err != nil {
				return nil, err
			}
		}
	}

	data, err := encoder.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal entity: %w", err)
	}

	key := s.buildKey(entity.ID)

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

	entity.Revision = int(txnResp.Header.Revision)

	return entity, nil
}

func (s *EtcdStore) saveEntity(ctx context.Context, entity *Entity) error {
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

// GetEntity implements Store interface
func (s *EtcdStore) GetEntity(ctx context.Context, id Id) (*Entity, error) {
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

	err = decoder.Unmarshal(resp.Kvs[0].Value, &entity)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize entity: %w", err)
	}

	entity.postUnmarshal()

	return &entity, nil
}

// UpdateEntity implements Store interface
func (s *EtcdStore) UpdateEntity(ctx context.Context, id Id, attributes []Attr) (*Entity, error) {
	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}

	// Validate attributes
	for i, attr := range attributes {
		if err := s.validator.ValidateAttribute(ctx, &attr); err != nil {
			return nil, fmt.Errorf("invalid attribute %s: %w", attr.ID, err)
		}
		attributes[i] = attr
	}

	entity.Attrs = append(entity.Attrs, attributes...)
	entity.UpdatedAt = now()

	entity.Fixup()

	data, err := encoder.Marshal(entity)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize entity: %w", err)
	}

	key := s.buildKey(entity.ID)

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
func (s *EtcdStore) DeleteEntity(ctx context.Context, id Id) error {
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

	txnResp, err := s.client.Txn(ctx).
		If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).
		Then(clientv3.OpPut(key, entity.ID)).
		Commit()
	if err != nil {
		return err
	}

	if !txnResp.Succeeded {
		return fmt.Errorf("entity already exists: %s", key)
	}

	return nil
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

	return s.client.Watch(ctx, prefix, clientv3.WithPrefix()), nil
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
