package entity

import (
	"context"
	"errors"
	"fmt"
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
	//ID        types.Id  `json:"id" cbor:"id"`
	//Revision  int64     `json:"revision,omitempty" cbor:"revision,omitempty"`
	//CreatedAt time.Time `json:"created_at" cbor:"created_at"`
	//UpdatedAt time.Time `json:"updated_at" cbor:"updated_at"`

	Attrs []Attr `json:"attrs" cbor:"attrs"`
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

	AllAttrs() []Attr
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
	for _, attr := range e.Attrs {
		if attr.ID == name {
			return attr, true
		}
	}

	return Attr{}, false
}

func (e *Entity) GetAll(name Id) []Attr {
	var attrs []Attr

	for _, attr := range e.Attrs {
		if attr.ID == name {
			attrs = append(attrs, attr)
		}
	}

	return attrs
}

func (e *Entity) AllAttrs() []Attr {
	return e.Attrs
}

func (e *Entity) Remove(id Id) bool {
	orig := len(e.Attrs)
	e.Attrs = slices.DeleteFunc(e.Attrs, func(attr Attr) bool {
		return attr.ID == id
	})

	return orig != len(e.Attrs)
}

func (e *Entity) GetRevision() int64 {
	if attr, ok := e.Get(Revision); ok {
		return attr.Value.Int64()
	}
	return 0
}

func (e *Entity) SetRevision(rev int64) {
	e.Remove(Revision)
	e.Attrs = append(e.Attrs, Int64(Revision, rev))
	e.Attrs = SortedAttrs(e.Attrs)
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
	return slices.CompareFunc(e.Attrs, other.Attrs, func(a, b Attr) int {
		return a.Compare(b)
	})
}

func (e *Entity) Update(attrs []Attr) error {
	e.Attrs = append(e.Attrs, attrs...)
	return e.Fixup()
}

type EntityComponent struct {
	Attrs []Attr `json:"attrs" cbor:"attrs"`
}

var _ AttrGetter = (*EntityComponent)(nil)

func (e *EntityComponent) Get(name Id) (Attr, bool) {
	for _, attr := range e.Attrs {
		if attr.ID == name {
			return attr, true
		}
	}

	return Attr{}, false
}

func (e *Entity) Set(newAttr Attr) bool {
	for i, attr := range e.Attrs {
		if attr.ID == newAttr.ID {
			e.Attrs[i].Value = newAttr.Value

			// The value impacts the sort order.
			e.Attrs = SortedAttrs(e.Attrs)
			return true
		}
	}

	e.Attrs = append(e.Attrs, newAttr)
	e.Attrs = SortedAttrs(e.Attrs)

	return false
}

func (e *EntityComponent) GetAll(name Id) []Attr {
	var attrs []Attr
	for _, attr := range e.Attrs {
		if attr.ID == name {
			attrs = append(attrs, attr)
		}
	}

	return attrs
}

func (e *EntityComponent) AllAttrs() []Attr {
	return e.Attrs
}

type EntityStore interface {
	GetEntity(ctx context.Context, id Id) (*Entity, error)
	GetAttributeSchema(ctx context.Context, name Id) (*AttributeSchema, error)
	CreateEntity(ctx context.Context, attributes []Attr, opts ...EntityOption) (*Entity, error)
	UpdateEntity(ctx context.Context, id Id, attributes []Attr, opts ...EntityOption) (*Entity, error)
	DeleteEntity(ctx context.Context, id Id) error
	ListIndex(ctx context.Context, attr Attr) ([]Id, error)
	ListCollection(ctx context.Context, collection string) ([]Id, error)
}

func (e *Entity) postUnmarshal() error {
	return nil
}

func convertEntityToSchema(ctx context.Context, s EntityStore, entity *Entity) (*AttributeSchema, error) {
	var schema AttributeSchema

	for _, attr := range entity.Attrs {
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

				for _, predAttr := range e.Attrs {
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

	e.Attrs = SortedAttrs(e.Attrs)

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

	e.ensureDBId()
}

type ToAttr interface {
	Attr | []Attr | func() []Attr
}

func NewEntity[T ToAttr](attrs ...T) *Entity {
	var attrList []Attr

	for _, a := range attrs {
		switch v := any(a).(type) {
		case func() []Attr:
			attrList = append(attrList, v()...)
		case []Attr:
			attrList = append(attrList, v...)
		case Attr:
			attrList = append(attrList, v)
		}
	}

	ts := time.Now()
	e := &Entity{
		Attrs: attrList,
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

func Blank() *Entity {
	return &Entity{}
}

func (e *Entity) ensureDBId() {
}

func (e *Entity) SetID(id Id) bool {
	return e.Set(Ref(DBId, id))
}

var (
	encoder cbor.EncMode
	decoder cbor.DecMode
	tags    = cbor.NewTagSet()
)

func Encode(v any) ([]byte, error) {
	return encoder.Marshal(v)
}

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

func MustKeyword(str string) types.Keyword {
	if !ValidKeyword(str) {
		panic(fmt.Sprintf("invalid keyword: %q", str))
	}

	return types.Keyword(str)
}

func Attrs(vals ...any) []Attr {
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
	return SortedAttrs(attrs)
}
