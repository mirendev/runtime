package entity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
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
	ID         Id
	Doc        string
	Type       Id
	ElemType   Id
	Unique     uniq
	EnumValues []Value
	AllowMany  bool
	Index      bool
	Session    bool
	Predicate  []*Entity
	CheckProgs []string
}

// Entity represents an entity with a set of attributes
type Entity struct {
	attrs []Attr
}

// New constructs a slice of attributes from a variadic list of Attr, []Attr, func() []Attr, or key-value pairs.
func New(vals ...any) *Entity {
	var attrs []Attr

	i := 0
	for i < len(vals) {
		switch v := vals[i].(type) {
		case func() []Attr:
			attrs = append(attrs, v()...)
			i++
		case []Attr:
			attrs = append(attrs, v...)
			i++
		case Attr:
			attrs = append(attrs, v)
			i++
		case Id:
			attrs = append(attrs, Attr{
				ID:    v,
				Value: AnyValue(vals[i+1]),
			})
			i += 2
		default:
			panic(fmt.Sprintf("expected Id key, got %T", v))
		}
	}

	ts := time.Now()
	e := &Entity{
		attrs: SortedAttrs(attrs),
	}

	e.removeIdent()

	// Only set timestamps if they don't already exist
	if e.GetCreatedAt().IsZero() {
		e.SetCreatedAt(ts)
	}
	if e.GetUpdatedAt().IsZero() {
		e.SetUpdatedAt(ts)
	}

	return e
}

// Attrs returns a clone of the entity's attributes
func (e *Entity) Attrs() []Attr {
	return slices.Clone(e.attrs)
}

// entityExternal is used for JSON marshaling/unmarshaling
type entityExternal struct {
	Attrs []Attr `json:"attrs" cbor:"attrs"`
}

func (e *Entity) MarshalJSON() ([]byte, error) {
	return json.Marshal(entityExternal{Attrs: e.attrs})
}

func (e *Entity) UnmarshalJSON(data []byte) error {
	var ej entityExternal
	if err := json.Unmarshal(data, &ej); err != nil {
		return err
	}
	e.attrs = ej.Attrs
	return nil
}

func (e *Entity) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(entityExternal{Attrs: e.attrs})
}

func (e *Entity) UnmarshalCBOR(data []byte) error {
	var ec entityExternal
	if err := cbor.Unmarshal(data, &ec); err != nil {
		return err
	}
	e.attrs = ec.Attrs

	// Defensive programming: if no DBId attribute, try migrating from old format
	// TODO remove this when we feel that everything has been migrated.
	if _, ok := e.Get(DBId); !ok {
		// Try to decode as old format with struct fields
		var oldEnt OldEntity
		if err := cbor.Unmarshal(data, &oldEnt); err == nil {
			slog.Default().Warn("Entity missing db/id attribute, attempting migration from old format", "id", oldEnt.ID)
			// Check if this actually needs migration (has old-style struct fields)
			needsMigration := oldEnt.ID != "" || oldEnt.Revision != 0 || oldEnt.CreatedAt != 0 || oldEnt.UpdatedAt != 0

			if needsMigration {
				// Migrate struct fields to attributes
				if oldEnt.ID != "" {
					if _, ok := oldEnt.Get(DBId); !ok {
						e.SetID(oldEnt.ID)
					}
				}

				if oldEnt.Revision != 0 {
					e.Set(Int64(Revision, oldEnt.Revision))
				}

				if oldEnt.CreatedAt != 0 {
					createdAt := time.Unix(0, oldEnt.CreatedAt*int64(time.Millisecond))
					e.SetCreatedAt(createdAt)
				}

				if oldEnt.UpdatedAt != 0 {
					updatedAt := time.Unix(0, oldEnt.UpdatedAt*int64(time.Millisecond))
					e.SetUpdatedAt(updatedAt)
				}

				// Sort attributes for consistency
				e.attrs = SortedAttrs(e.attrs)
			}
		} else {
			slog.Default().Warn("Entity missing db/id attribute, attempted migration from old format failed", "error", err)
		}
	}

	return nil
}

func (e *Entity) Id() Id {
	if a, ok := e.Get(DBId); ok {
		if a.Value.Kind() == KindId {
			return a.Value.Id()
		}
	}

	return ""
}

type AttrGetter interface {
	Get(name Id) (Attr, bool)
	GetAll(name Id) []Attr

	Attrs() []Attr
}

var _ AttrGetter = (*Entity)(nil)

func MustGet(e AttrGetter, name Id) Attr {
	attr, ok := e.Get(name)
	if !ok {
		panic(fmt.Sprintf("attribute %q not found", name))
	}
	return attr
}

func (e *Entity) Get(name Id) (Attr, bool) {
	for _, attr := range e.attrs {
		if attr.ID == name {
			return attr, true
		}
	}

	return Attr{}, false
}

func (e *Entity) GetAll(name Id) []Attr {
	var attrs []Attr

	for _, attr := range e.attrs {
		if attr.ID == name {
			attrs = append(attrs, attr)
		}
	}

	return attrs
}

func (e *Entity) Remove(id Id) bool {
	orig := len(e.attrs)
	e.attrs = slices.DeleteFunc(e.attrs, func(attr Attr) bool {
		return attr.ID == id
	})

	return orig != len(e.attrs)
}

func (e *Entity) GetRevision() int64 {
	if attr, ok := e.Get(Revision); ok {
		return attr.Value.Int64()
	}
	return 0
}

func (e *Entity) SetRevision(rev int64) {
	e.Remove(Revision)
	e.attrs = append(e.attrs, Int64(Revision, rev))
	e.attrs = SortedAttrs(e.attrs)
}

func (e *Entity) GetCreatedAt() time.Time {
	if attr, ok := e.Get(CreatedAt); ok {
		return attr.Value.Time()
	}
	return time.Time{}
}

func (e *Entity) SetCreatedAt(ts time.Time) bool {
	return e.Set(Time(CreatedAt, ts))
}

func (e *Entity) GetUpdatedAt() time.Time {
	if attr, ok := e.Get(UpdatedAt); ok {
		return attr.Value.Time()
	}
	return time.Time{}
}

func (e *Entity) SetUpdatedAt(ts time.Time) bool {
	return e.Set(Time(UpdatedAt, ts))
}

func (e *Entity) Compare(other *Entity) int {
	return slices.CompareFunc(e.attrs, other.attrs, func(a, b Attr) int {
		return a.Compare(b)
	})
}

func (e *Entity) Merge(other *Entity) error {
	for _, attr := range other.attrs {
		switch attr.ID {
		case CreatedAt:
			// Keep existing CreatedAt if there is one
			if e.GetCreatedAt().IsZero() {
				e.Set(attr)
			}
		case UpdatedAt:
			e.Set(attr)
		case Revision:
			// Keep existing Revision if there is one
			if e.GetRevision() == 0 {
				e.Set(attr)
			}
		default:
			// Don't use Set because we might be merging card many attributes
			e.attrs = append(e.attrs, attr.Clone())
		}
	}

	return e.Fixup()
}

func (e *Entity) Update(attrs []Attr) error {
	for _, attr := range attrs {
		e.Set(attr)
	}
	return e.Fixup()
}

type EntityComponent struct {
	attrs []Attr
}

// Attrs returns a clone of the component's attributes
func (e *EntityComponent) Attrs() []Attr {
	return append([]Attr(nil), e.attrs...)
}

// entityComponentExternal is used for JSON marshaling/unmarshaling
type entityComponentExternal struct {
	Attrs []Attr `json:"attrs" cbor:"attrs"`
}

func (e *EntityComponent) MarshalJSON() ([]byte, error) {
	return json.Marshal(entityComponentExternal{Attrs: e.attrs})
}

func (e *EntityComponent) UnmarshalJSON(data []byte) error {
	var ec entityComponentExternal
	if err := json.Unmarshal(data, &ec); err != nil {
		return err
	}
	e.attrs = ec.Attrs
	return nil
}

func (e *EntityComponent) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(entityComponentExternal{Attrs: e.attrs})
}

func (e *EntityComponent) UnmarshalCBOR(data []byte) error {
	var ec entityComponentExternal
	if err := cbor.Unmarshal(data, &ec); err != nil {
		return err
	}
	e.attrs = ec.Attrs
	return nil
}

var _ AttrGetter = (*EntityComponent)(nil)

func (e *EntityComponent) Get(name Id) (Attr, bool) {
	for _, attr := range e.attrs {
		if attr.ID == name {
			return attr, true
		}
	}

	return Attr{}, false
}

func (e *Entity) Set(newAttr Attr) bool {
	for i, attr := range e.attrs {
		if attr.ID == newAttr.ID {
			e.attrs[i].Value = newAttr.Value

			// The value impacts the sort order.
			e.attrs = SortedAttrs(e.attrs)
			return true
		}
	}

	e.attrs = append(e.attrs, newAttr)
	e.attrs = SortedAttrs(e.attrs)

	return false
}

func (e *EntityComponent) GetAll(name Id) []Attr {
	var attrs []Attr
	for _, attr := range e.attrs {
		if attr.ID == name {
			attrs = append(attrs, attr)
		}
	}

	return attrs
}

type EntityStore interface {
	GetEntity(ctx context.Context, id Id) (*Entity, error)
	GetAttributeSchema(ctx context.Context, name Id) (*AttributeSchema, error)
	CreateEntity(ctx context.Context, entity *Entity, opts ...EntityOption) (*Entity, error)
	UpdateEntity(ctx context.Context, id Id, entity *Entity, opts ...EntityOption) (*Entity, error)
	DeleteEntity(ctx context.Context, id Id) error
	ListIndex(ctx context.Context, attr Attr) ([]Id, error)
	ListCollection(ctx context.Context, collection string) ([]Id, error)
}

func (e *Entity) postUnmarshal() error {
	return nil
}

func convertEntityToSchema(ctx context.Context, s EntityStore, entity *Entity) (*AttributeSchema, error) {
	var schema AttributeSchema

	for _, attr := range entity.attrs {
		switch attr.ID {
		case Ident:
			switch id := attr.Value.Any().(type) {
			case types.Keyword:
				schema.ID = Id(id)
			default:
				return nil, fmt.Errorf("invalid entity ident (expected EntityId): %v (%T)", id, attr.Value)
			}
		case Doc:
			if doc, ok := attr.Value.Any().(string); ok {
				schema.Doc = doc
			} else {
				return nil, fmt.Errorf("invalid entity doc: %v", attr.Value)
			}
		case Type:
			if typ, ok := attr.Value.Any().(Id); ok {
				schema.Type = typ
			} else {
				return nil, fmt.Errorf("invalid entity type: %v", attr.Value)
			}
		case EnumValues:
			if val, ok := attr.Value.Any().([]Value); ok {
				schema.EnumValues = val
			} else {
				return nil, fmt.Errorf("enum values must be a slice, was %T", attr.Value.Any())
			}
		case EntityElemType:
			if elemType, ok := attr.Value.Any().(Id); ok {
				schema.ElemType = elemType
			} else {
				return nil, fmt.Errorf("invalid element type: %v", attr.Value)
			}
		case Cardinality:
			switch attr.Value.Any() {
			case CardinalityOne:
				//ok
			case CardinalityMany:
				schema.AllowMany = true
			default:
				return nil, fmt.Errorf("invalid cardinality: %v", attr.Value)
			}
		case Uniq:
			switch attr.Value.Any() {
			case UniqueId:
				schema.Unique = uniqId
			case UniqueValue:
				schema.Unique = uniqVal
			default:
				return nil, fmt.Errorf("invalid uniqueness: %v", attr.Value)
			}
		case Index:
			if val, ok := attr.Value.Any().(bool); ok {
				schema.Index = val
			} else {
				return nil, fmt.Errorf("invalid index: %v", attr.Value.Any())
			}
		case Session:
			if val, ok := attr.Value.Any().(bool); ok {
				schema.Session = val
			} else {
				return nil, fmt.Errorf("invalid index: %v", attr.Value.Any())
			}
		case AttrPred:
			if pred, ok := attr.Value.Any().(Id); ok {
				e, err := s.GetEntity(ctx, pred)
				if err != nil {
					return nil, fmt.Errorf("invalid predicate: %w", err)
				}

				schema.Predicate = append(schema.Predicate, e)

				for _, predAttr := range e.attrs {
					if predAttr.ID == Program {
						schema.CheckProgs = append(schema.CheckProgs, predAttr.Value.Any().(string))
					}
				}

			} else {
				return nil, fmt.Errorf("invalid predicate: %v", attr.Value)
			}
		}
	}

	return &schema, nil
}

func (e *Entity) removeIdent() {
	// First check for db/id attribute (preferred)
	if _, ok := e.Get(DBId); !ok {
		// Fall back to db/ident for backwards compatibility
		if ident, ok := e.Get(Ident); ok {
			var identId Id
			switch id := ident.Value.Any().(type) {
			case Id:
				identId = id
			case string:
				identId = Id(id)
			case types.Keyword:
				identId = Id(id)
			}

			if identId != "" {
				e.SetID(identId)

				// We remove idents now that we're using db/id as the primary ID
				e.Remove(Ident)
			}
		}
	}
}

func (e *Entity) Fixup() error {
	// First check for db/id attribute (preferred)
	if attr, ok := e.Get(DBId); ok {
		if attr.Value.Kind() != KindId {
			return fmt.Errorf("invalid db/id attribute (expected KindId): %s", attr.Value.Kind())
		}
	} else {
		// Fall back to db/ident for backwards compatibility
		if ident, ok := e.Get(Ident); ok {
			var identId Id
			switch id := ident.Value.Any().(type) {
			case Id:
				identId = id
			case string:
				identId = Id(id)
			case types.Keyword:
				identId = Id(id)
			default:
				return fmt.Errorf("invalid entity ident (expected EntityId): %v (%T)", ident.Value.Any(), ident.Value)
			}

			if identId != "" {
				e.SetID(identId)
			}

			// We remove idents now that we're using db/id as the primary ID
			e.Remove(Ident)
		}
	}

	e.attrs = SortedAttrs(e.attrs)

	return nil
}

func (e *Entity) ForceID() {
	if e.Id() != "" {
		return
	}

	// Try to use entity kind as prefix for auto-generated ID
	prefix := "e"
	if kind, ok := e.Get(EntityKind); ok {
		var kindStr string
		switch k := kind.Value.Any().(type) {
		case Id:
			kindStr = string(k)
		case string:
			kindStr = k
		case types.Keyword:
			kindStr = string(k)
		}

		// Extract last segment after rightmost . or /
		if kindStr != "" {
			lastDot := strings.LastIndex(kindStr, ".")
			lastSlash := strings.LastIndex(kindStr, "/")
			cutPos := max(lastDot, lastSlash)
			if cutPos >= 0 && cutPos < len(kindStr)-1 {
				prefix = kindStr[cutPos+1:]
			} else {
				prefix = kindStr
			}
		}
	}

	e.SetID(Id(idgen.GenNS(prefix)))
}

// Timeless returns a copy of the entity without CreatedAt, UpdatedAt and Revision attributes
func (e *Entity) Timeless() *Entity {
	o := e.Clone()

	o.Remove(CreatedAt)
	o.Remove(UpdatedAt)
	o.Remove(Revision)

	return o
}

// Blank creates a new empty entity
func Blank() *Entity {
	return &Entity{}
}

// SetID sets the entity ID (db/id attribute) and returns true if it replaced an existing ID
func (e *Entity) SetID(id Id) bool {
	return e.Set(Ref(DBId, id))
}

var (
	encoder cbor.EncMode
	decoder cbor.DecMode
	tags    = cbor.NewTagSet()
)

// Encode encodes a value to CBOR format
func Encode(v any) ([]byte, error) {
	return encoder.Marshal(v)
}

// Decode decodes CBOR data into a value
func Decode(data []byte, v any) error {
	return decoder.Unmarshal(data, v)
}

func init() {
	tags.Add(cbor.TagOptions{
		EncTag: cbor.EncTagRequired,
		DecTag: cbor.DecTagOptional,
	}, reflect.TypeOf(Id("")), 50)

	tags.Add(cbor.TagOptions{
		EncTag: cbor.EncTagRequired,
		DecTag: cbor.DecTagOptional,
	}, reflect.TypeOf(types.Keyword("")), 51)

	var err error
	encoder, err = cbor.EncOptions{
		Sort:          cbor.SortBytewiseLexical,
		ShortestFloat: cbor.ShortestFloat16,
		Time:          cbor.TimeRFC3339Nano,
		TagsMd:        cbor.TagsAllowed,
	}.EncModeWithSharedTags(tags)
	if err != nil {
		panic(err)
	}

	decoder, err = cbor.DecOptions{}.DecModeWithSharedTags(tags)
	if err != nil {
		panic(err)
	}
}

type (
	Id = types.Id
)

var keySpecial = []rune{'_', '-', '/', '.', ':'}

// ValidKeyword checks if a string is a valid keyword
func ValidKeyword(str string) bool {
	r, sz := utf8.DecodeRuneInString(str)
	if !unicode.IsLetter(r) {
		return false
	}

	str = str[sz:]

	var special bool

	for len(str) > 0 {
		r, sz = utf8.DecodeRuneInString(str)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			special = false
			str = str[sz:]
			continue
		}

		if slices.Contains(keySpecial, r) {
			special = true
			str = str[sz:]
			continue
		}

		return false
	}

	return !special
}

// MustKeyword converts a string to a Keyword, panicking if it's not valid
func MustKeyword(str string) types.Keyword {
	if !ValidKeyword(str) {
		panic(fmt.Sprintf("invalid keyword: %q", str))
	}

	return types.Keyword(str)
}
