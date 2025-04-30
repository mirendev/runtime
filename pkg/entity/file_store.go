package entity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mr-tron/base58"
)

// FileStore provides CRUD operations for entities
type FileStore struct {
	basePath    string
	schemaCache map[Id]*AttributeSchema
	mu          sync.RWMutex

	validator *Validator
}

// NewFileStore creates a new entity store with the given base path
func NewFileStore(basePath string) (*FileStore, error) {
	// Ensure the base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	// Create directories for entities and schemas
	if err := os.MkdirAll(filepath.Join(basePath, "entities"), 0755); err != nil {
		return nil, fmt.Errorf("failed to create entities directory: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(basePath, "collections"), 0755); err != nil {
		return nil, fmt.Errorf("failed to create schemas directory: %w", err)
	}

	store := &FileStore{
		basePath:    basePath,
		schemaCache: make(map[Id]*AttributeSchema),
	}

	store.validator = NewValidator(store)

	err := InitSystemEntities(store.saveEntity)
	if err != nil {
		return nil, err
	}

	return store, nil
}

// CreateEntity creates a new entity with the given type and attributes
func (s *FileStore) CreateEntity(ctx context.Context, attributes []Attr, opts ...EntityOption) (*Entity, error) {
	entity := &Entity{
		Attrs:     attributes,
		Revision:  1,
		CreatedAt: now(),
		UpdatedAt: now(),
	}

	// Validate attributes against schemas
	err := s.validator.ValidateEntity(ctx, entity)
	if err != nil {
		return nil, err
	}

	if err := s.saveEntity(entity); err != nil {
		return nil, err
	}

	for _, attr := range entity.Attrs {
		schema, err := s.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return nil, err
		}

		if schema.Index {
			err := s.addToCollection(entity, fmt.Sprintf("%s:%v", attr.ID, attr.Value.Any()))
			if err != nil {
				return nil, err
			}
		}
	}

	return entity, nil
}

// GetEntity retrieves an entity by ID
func (s *FileStore) GetEntity(_ context.Context, id Id) (*Entity, error) {
	key := base58.Encode([]byte(id))
	path := filepath.Join(s.basePath, "entities", key+".cbor")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("looking for %s: %w", id, ErrEntityNotFound)
		}
		return nil, fmt.Errorf("failed to read entity file: %w", err)
	}

	var entity Entity
	if err := decoder.Unmarshal(data, &entity); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entity: %w", err)
	}

	err = entity.postUnmarshal()
	if err != nil {
		return nil, err
	}

	return &entity, nil
}

// UpdateEntity updates an existing entity
func (s *FileStore) UpdateEntity(ctx context.Context, id Id, attributes []Attr, opts ...EntityOption) (*Entity, error) {
	entity, err := s.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}

	// Validate attributes against schemas
	err = s.validator.ValidateAttributes(ctx, attributes)
	if err != nil {
		return nil, err
	}

	entity.Attrs = append(entity.Attrs, attributes...)

	// TODO: Revalidate attributes tooking for duplicates

	entity.Revision++
	entity.UpdatedAt = now()

	if err := s.saveEntity(entity); err != nil {
		return nil, err
	}

	return entity, nil
}

// DeleteEntity deletes an entity by ID
func (s *FileStore) DeleteEntity(_ context.Context, id Id) error {
	key := base58.Encode([]byte(id))
	path := filepath.Join(s.basePath, "entities", key+".cbor")

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrEntityNotFound
		}
		return fmt.Errorf("failed to delete entity file: %w", err)
	}

	return nil
}

// GetAttributeSchema retrieves an attribute schema by ID
func (s *FileStore) GetAttributeSchema(ctx context.Context, id Id) (*AttributeSchema, error) {
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

// saveEntity saves an entity to disk
func (s *FileStore) saveEntity(entity *Entity) error {
	entity.Fixup()

	data, err := encoder.Marshal(entity)
	if err != nil {
		return fmt.Errorf("failed to marshal entity: %w", err)
	}

	key := base58.Encode([]byte(entity.ID))

	path := filepath.Join(s.basePath, "entities", key+".cbor")

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write entity file: %w", err)
	}

	return nil
}

func (s *FileStore) addToCollection(entity *Entity, collection string) error {
	key := base58.Encode([]byte(entity.ID))
	colKey := base58.Encode([]byte(collection))

	path := filepath.Join(s.basePath, "collections", colKey, key)

	os.MkdirAll(filepath.Dir(path), 0755)

	if err := os.WriteFile(path, []byte(entity.ID), 0644); err != nil {
		return fmt.Errorf("failed to write collection file: %w", err)
	}

	return nil
}

func (s *FileStore) ListIndex(ctx context.Context, attr Attr) ([]Id, error) {
	schema, err := s.GetAttributeSchema(ctx, attr.ID)
	if err != nil {
		return nil, err
	}

	if !schema.Index {
		return nil, fmt.Errorf("attribute %s is not indexed", attr.ID)
	}

	return s.ListCollection(ctx, fmt.Sprintf("%s:%v", attr.ID, attr.Value.Any()))
}

func (s *FileStore) ListCollection(_ context.Context, collection string) ([]Id, error) {
	colKey := base58.Encode([]byte(collection))
	path := filepath.Join(s.basePath, "collections", colKey)

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read collection directory: %w", err)
	}

	var ids []Id

	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(path, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read collection file: %w", err)
		}

		ids = append(ids, Id(data))
	}

	return ids, nil
}
