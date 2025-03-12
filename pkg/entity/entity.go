package entity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/uuid"
	"github.com/mr-tron/base58"
)

// Common errors
var (
	ErrEntityNotFound      = errors.New("entity not found")
	ErrEntityAlreadyExists = errors.New("entity already exists")
	ErrAttributeNotFound   = errors.New("attribute not found")
	ErrInvalidAttribute    = errors.New("invalid attribute")
	ErrSchemaNotFound      = errors.New("schema not found")
)

type uniq int

const (
	notUniq uniq = iota
	uniqId
	uniqVal
)

// AttributeSchema defines the schema for an attribute
type AttributeSchema struct {
	ID          EntityId `json:"id" cbor:"id"`
	Name        string   `json:"name" cbor:"name"`
	Description string   `json:"description" cbor:"description"`
	Type        EntityId
	ElemType    EntityId
	Required    bool `json:"required" cbor:"required"`
	Unique      uniq
	EnumValues  []any `json:"enum_values" cbor:"enum_values"`
	AllowMany   bool
	Index       bool

	Predicate  []*Entity
	CheckProgs []string
	// Additional validation rules could be added here
}

// Attribute represents a key-value pair with a link to its schema
type Attribute struct {
	SchemaID string `json:"schema_id" cbor:"schema_id"`
	Value    any    `json:"value" cbor:"value"`
}

type Attr struct {
	ID    EntityId `json:"id" cbor:"id"`
	Value any      `json:"v" cbor:"v"`
	Type  string   `json:"t" cbor:"t"`
}

// Entity represents an entity with a set of attributes
type Entity struct {
	ID        string `json:"id" cbor:"id"`
	Revision  int    `json:"revision,omitempty" cbor:"revision,omitempty"`
	CreatedAt int64  `json:"created_at" cbor:"created_at"`
	UpdatedAt int64  `json:"updated_at" cbor:"updated_at"`

	Attrs []Attr `json:"attrs" cbor:"attrs"`
}

func (e *Entity) Get(name EntityId) (Attr, bool) {
	for _, attr := range e.Attrs {
		if attr.ID == name {
			return attr, true
		}
	}

	return Attr{}, false
}

type EntityStore interface {
	GetEntity(id EntityId) (*Entity, error)
	GetAttributeSchema(name EntityId) (*AttributeSchema, error)
	CreateEntity(attributes []Attr) (*Entity, error)
	UpdateEntity(id EntityId, attributes []Attr) (*Entity, error)
	DeleteEntity(id EntityId) error
	ListIndex(id EntityId, val any) ([]EntityId, error)
	ListCollection(collection string) ([]EntityId, error)
}

// FileStore provides CRUD operations for entities
type FileStore struct {
	basePath    string
	schemaCache map[EntityId]*AttributeSchema
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
		schemaCache: make(map[EntityId]*AttributeSchema),
	}

	store.validator = NewValidator(store)

	err := InitSystemEntities(store.saveEntity)
	if err != nil {
		return nil, err
	}

	return store, nil
}

// CreateEntity creates a new entity with the given type and attributes
func (s *FileStore) CreateEntity(attributes []Attr) (*Entity, error) {
	// Validate attributes against schemas
	err := s.validator.ValidateAttributes(attributes)
	if err != nil {
		return nil, err
	}

	entity := &Entity{
		Attrs:     attributes,
		Revision:  1,
		CreatedAt: now(),
		UpdatedAt: now(),
	}

	if err := s.saveEntity(entity); err != nil {
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

	return entity, nil
}

// GetEntity retrieves an entity by ID
func (s *FileStore) GetEntity(id EntityId) (*Entity, error) {
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
	if err := cbor.Unmarshal(data, &entity); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entity: %w", err)
	}

	err = entity.postUnmarshal()
	if err != nil {
		return nil, err
	}

	return &entity, nil
}

func (e *Entity) postUnmarshal() error {
	for i, attr := range e.Attrs {
		if attr.Type == string(EntityTypeRef) {
			if id, ok := attr.Value.(string); ok {
				e.Attrs[i].Value = EntityId(id)
			}
		}
	}

	return nil
}

// UpdateEntity updates an existing entity
func (s *FileStore) UpdateEntity(id EntityId, attributes []Attr) (*Entity, error) {
	entity, err := s.GetEntity(id)
	if err != nil {
		return nil, err
	}

	// Validate attributes against schemas
	err = s.validator.ValidateAttributes(attributes)
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
func (s *FileStore) DeleteEntity(id EntityId) error {
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

func convertEntityToSchema(s EntityStore, entity *Entity) (*AttributeSchema, error) {
	var schema AttributeSchema

	for _, attr := range entity.Attrs {
		switch attr.ID {
		case EntityIdent:
			switch id := attr.Value.(type) {
			case EntityId:
				schema.ID = id
			case KW:
				schema.ID = EntityId(id)
			case string:
				schema.ID = EntityId(id)
			default:
				return nil, fmt.Errorf("invalid entity ident (expected EntityId): %v (%T)", attr.Value, attr.Value)
			}
		case EntityDoc:
			if doc, ok := attr.Value.(string); ok {
				schema.Description = doc
			} else {
				return nil, fmt.Errorf("invalid entity doc: %v", attr.Value)
			}
		case EntityType:
			if typ, ok := attr.Value.(EntityId); ok {
				schema.Type = typ
			} else {
				return nil, fmt.Errorf("invalid entity type: %v", attr.Value)
			}
		case EntityEnumValues:
			rv := reflect.ValueOf(attr.Value)
			if rv.Kind() != reflect.Slice {
				return nil, fmt.Errorf("enum values must be a slice")
			}

			values := make([]any, rv.Len())
			for i := range rv.Len() {
				values[i] = rv.Index(i).Interface()
			}

			schema.EnumValues = values
		case EntityElemType:
			if elemType, ok := attr.Value.(EntityId); ok {
				schema.ElemType = elemType
			} else {
				return nil, fmt.Errorf("invalid element type: %v", attr.Value)
			}
		case EntityCard:
			switch attr.Value {
			case EntityCardOne:
				//ok
			case EntityCardMany:
				schema.AllowMany = true
			default:
				return nil, fmt.Errorf("invalid cardinality: %v", attr.Value)
			}
		case EntityUniq:
			switch attr.Value {
			case EntityUniqueId:
				schema.Unique = uniqId
			case EntityUniqueValue:
				schema.Unique = uniqVal
			default:
				return nil, fmt.Errorf("invalid uniqueness: %v", attr.Value)
			}
		case EntityIndex:
			if val, ok := attr.Value.(bool); ok {
				schema.Index = val
			} else {
				return nil, fmt.Errorf("invalid index: %v", attr.Value)
			}
		case EntityAttrPred:
			if pred, ok := attr.Value.(EntityId); ok {
				e, err := s.GetEntity(pred)
				if err != nil {
					return nil, fmt.Errorf("invalid predicate: %w", err)
				}

				schema.Predicate = append(schema.Predicate, e)

				for _, predAttr := range e.Attrs {
					if predAttr.ID == EntityProgPred {
						schema.CheckProgs = append(schema.CheckProgs, predAttr.Value.(string))
					}
				}

			} else {
				return nil, fmt.Errorf("invalid predicate: %v", attr.Value)
			}
		}
	}

	if schema.ElemType != "" {
		switch schema.ElemType {
		case EntityTypeRef:
			for i, v := range schema.EnumValues {
				schema.EnumValues[i] = EntityId(v.(string))
			}
		}
	}

	return &schema, nil
}

// GetAttributeSchema retrieves an attribute schema by ID
func (s *FileStore) GetAttributeSchema(id EntityId) (*AttributeSchema, error) {
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

func (e *Entity) Fixup() error {
	if e.ID == "" {
		if ident, ok := e.Get(EntityIdent); ok {
			switch id := ident.Value.(type) {
			case EntityId:
				e.ID = string(id)
			case string:
				e.ID = id
			case KW:
				e.ID = string(id)
			}
		}
	}

	if e.ID == "" {
		e.ID = uuid.New().String()
	}

	for i, attr := range e.Attrs {
		if attr.Type == "" {
			switch attr.Value.(type) {
			case EntityId:
				e.Attrs[i].Type = string(EntityTypeRef)
			}
		}
	}

	return nil
}

// saveEntity saves an entity to disk
func (s *FileStore) saveEntity(entity *Entity) error {
	entity.Fixup()

	data, err := cbor.Marshal(entity)
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

func (s *FileStore) ListIndex(id EntityId, val any) ([]EntityId, error) {
	schema, err := s.GetAttributeSchema(id)
	if err != nil {
		return nil, err
	}

	if !schema.Index {
		return nil, fmt.Errorf("attribute %s is not indexed", id)
	}

	return s.ListCollection(fmt.Sprintf("%s:%v", id, val))
}

func (s *FileStore) ListCollection(collection string) ([]EntityId, error) {
	colKey := base58.Encode([]byte(collection))

	path := filepath.Join(s.basePath, "collections", colKey)

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read collection directory: %w", err)
	}

	var ids []EntityId

	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(path, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read collection file: %w", err)
		}

		ids = append(ids, EntityId(data))
	}

	return ids, nil
}

// Helper function to get current time
func now() int64 {
	return int64(time.Now().UnixNano() / 1000000) // milliseconds
}

type (
	EntityId string
	KW       string
)

const (
	EntityIdent      EntityId = "db/ident"
	EntityDoc        EntityId = "db/doc"
	EntityUniq       EntityId = "db/uniq"
	EntityCard       EntityId = "db/cardinality"
	EntityType       EntityId = "db/type"
	EntityEnumValues EntityId = "db/enumValues"
	EntityElemType   EntityId = "db/elementType"

	EntityUniqueId    EntityId = "db/unique.identity"
	EntityUniqueValue EntityId = "db/unique.value"

	EntityCardOne  EntityId = "db/cardinality.one"
	EntityCardMany EntityId = "db/cardinality.many"

	EntityTypeAny   EntityId = "db/type.any"
	EntityTypeRef   EntityId = "db/type.ref"
	EntityTypeStr   EntityId = "db/type.str"
	EntityTypeKW    EntityId = "db/type.keyword"
	EntityTypeInt   EntityId = "db/type.int"
	EntityTypeFloat EntityId = "db/type.float"
	EntityTypeBool  EntityId = "db/type.bool"
	EntityTypeTime  EntityId = "db/type.time"
	EntityTypeEnum  EntityId = "db/type.enum"
	EntityTypeArray EntityId = "db/type.array"

	EntityIndex EntityId = "db/index"

	EntityAttrPred EntityId = "db/attr.pred"
	EntityProgPred EntityId = "db/program"

	EntityKind EntityId = "entity/kind"

	EntityNetIP   EntityId = "db/pred.ip"
	EntityNetCIDR EntityId = "db/pred.cidr"
)

func Attrs(vals ...any) []Attr {
	if len(vals)%2 != 0 {
		panic("odd number of arguments")
	}

	var attrs []Attr

	for i := 0; i < len(vals); i += 2 {
		id, ok := vals[i].(EntityId)
		if !ok {
			panic(fmt.Sprintf("expected EntityId key, got %T", vals[i]))
		}

		attrs = append(attrs, Attr{
			ID:    id,
			Value: vals[i+1],
		})
	}
	return attrs
}

func InitSystemEntities(save func(*Entity) error) error {
	ident := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityIdent),
			EntityDoc, "Entity identifier",
			EntityUniq, EntityUniqueId,
			EntityCard, EntityCardOne,
			EntityType, EntityTypeKW,
		),
	}

	doc := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityDoc),
			EntityDoc, "Entity documentation",
			EntityCard, EntityCardOne,
			EntityType, EntityTypeStr,
		),
	}

	uniq := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityUniq),
			EntityDoc, "Unique attribute value",
			EntityCard, EntityCardOne,
			EntityType, EntityTypeRef,
		),
	}

	card := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityCard),
			EntityDoc, "Cardinality of an attribute",
			EntityCard, EntityCardOne,
			EntityType, EntityTypeEnum,
			EntityElemType, EntityTypeRef,
			EntityEnumValues, []EntityId{EntityCardOne, EntityCardMany},
		),
	}

	types := []EntityId{
		EntityTypeAny, EntityTypeRef, EntityTypeStr, EntityTypeKW,
		EntityTypeInt, EntityTypeFloat, EntityTypeBool, EntityTypeTime,
		EntityTypeEnum, EntityTypeArray,
	}

	typ := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityType),
			EntityDoc, "Type of an attribute",
			EntityCard, EntityCardOne,
			EntityType, EntityTypeRef,
		),
	}

	enumValues := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityEnumValues),
			EntityDoc, "Enum values",
			EntityCard, EntityCardMany,
			EntityType, EntityTypeArray,
			EntityElemType, EntityTypeAny,
		),
	}

	enumType := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityElemType),
			EntityDoc, "Enum type",
			EntityCard, EntityCardOne,
			EntityType, EntityTypeEnum,
			EntityElemType, EntityTypeRef,
			EntityEnumValues, types,
		),
	}

	index := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityIndex),
			EntityDoc, "Index",
			EntityCard, EntityCardOne,
			EntityType, EntityTypeBool,
		),
	}

	entityKind := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityKind),
			EntityDoc, "Entity kind",
			EntityCard, EntityCardOne,
			EntityType, EntityTypeKW,
			EntityIndex, true,
		),
	}

	id := func(id EntityId, doc string) *Entity {
		return &Entity{
			Attrs: Attrs(
				EntityIdent, KW(id),
				EntityDoc, doc,
			),
		}
	}

	uniqueIdentity := id(EntityUniqueId, "Unique identity")
	uniqueValue := id(EntityUniqueValue, "Unique value")
	cardOne := id(EntityCardOne, "Cardinality one")
	cardMany := id(EntityCardMany, "Cardinality many")

	typeAny := id(EntityTypeAny, "Any type")
	typeRef := id(EntityTypeRef, "Reference type")
	typeStr := id(EntityTypeStr, "String type")
	typeKW := id(EntityTypeKW, "Keyword type")
	typeInt := id(EntityTypeInt, "Integer type")
	typeFloat := id(EntityTypeFloat, "Float type")
	typeBool := id(EntityTypeBool, "Boolean type")
	typeTime := id(EntityTypeTime, "Time type")
	typeEnum := id(EntityTypeEnum, "Enum type")
	typeArray := id(EntityTypeArray, "Array type")

	attrPred := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityAttrPred),
			EntityDoc, "Attribute predicate",
			EntityCard, EntityCardMany,
			EntityType, EntityTypeRef,
		),
	}

	predIP := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityNetIP),
			EntityDoc, "A program that checks if a value is an IP address",
			EntityProgPred, "isIP(value)",
		),
	}

	predCidr := &Entity{
		Attrs: Attrs(
			EntityIdent, KW(EntityNetCIDR),
			EntityDoc, "A program that checks if a value is an IP CIDR address",
			EntityProgPred, "isCIDR(value)",
		),
	}

	entities := []*Entity{
		ident, doc, uniq, card, typ, enumValues, enumType,
		uniqueIdentity, uniqueValue, cardOne, cardMany,
		typeAny, typeRef, typeStr, typeKW, typeInt, typeFloat, typeBool, typeTime, typeEnum,
		typeArray, index,
		attrPred, predIP, predCidr,
		entityKind,
	}

	for _, entity := range entities {
		if err := save(entity); err != nil {
			if !errors.Is(err, ErrEntityAlreadyExists) {
				return err
			}
		}
	}

	return nil
}

// SetAttributeValue sets an attribute value
func (e *Entity) Set(name EntityId, value any) {
	for i, attr := range e.Attrs {
		if attr.ID == name {
			e.Attrs[i].Value = value
			return
		}
	}

	e.Attrs = append(e.Attrs, Attr{ID: name, Value: value})
}

// RemoveAttribute removes an attribute
func (e *Entity) Remove(name EntityId) error {
	idx := slices.IndexFunc(e.Attrs, func(a Attr) bool {
		return a.ID == name
	})

	if idx == -1 {
		return ErrAttributeNotFound
	}

	e.Attrs = slices.Delete(e.Attrs, idx, idx+1)

	return nil
}
