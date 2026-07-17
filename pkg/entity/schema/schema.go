package schema

import (
	"context"
	"errors"
	"slices"
	"sort"

	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

type SchemaRegistry struct {
	schemas map[string]*SchemaBuilder
}

var defaultRegistry = &SchemaRegistry{
	schemas: make(map[string]*SchemaBuilder),
}

type SchemaBuilder struct {
	domain     string
	version    string
	attrs      map[entity.Id]*entity.Entity
	singletons []entity.Id
}

func Builder(domain, version string) *SchemaBuilder {
	sb := &SchemaBuilder{
		domain: domain,
		attrs:  make(map[entity.Id]*entity.Entity),
	}

	//defaultRegistry.mu.Lock()
	//defer defaultRegistry.mu.Unlock()

	if _, exists := defaultRegistry.schemas[domain]; exists {
		panic("Schema already exists for domain: " + domain)
	}

	defaultRegistry.schemas[domain] = sb

	return sb
}

func Register(domain string, version string, fn func(schema *SchemaBuilder)) {
	//defaultRegistry.mu.Lock()
	//defer defaultRegistry.mu.Unlock()

	if _, exists := defaultRegistry.schemas[domain]; exists {
		panic("Schema already registered for domain: " + domain)
	}

	schema := Builder(domain, version)

	fn(schema)

	defaultRegistry.schemas[domain] = schema
}

func (b *SchemaBuilder) Apply(ctx context.Context, store entity.Store) error {
	for _, eid := range b.singletons {
		_, err := store.CreateEntity(ctx, entity.New(
			entity.Ident, types.Keyword(eid),
		), entity.WithOverwrite)
		if err != nil && !errors.Is(err, entity.ErrEntityAlreadyExists) {
			return err
		}
	}

	for _, e := range b.attrs {
		_, err := store.CreateEntity(ctx, entity.New(slices.Clone(e.Attrs())), entity.WithOverwrite)
		if err != nil && !errors.Is(err, entity.ErrEntityAlreadyExists) {
			return err
		}
	}

	return nil
}

func Apply(ctx context.Context, store entity.Store) error {
	//defaultRegistry.mu.Lock()
	//defer defaultRegistry.mu.Unlock()

	for _, schema := range defaultRegistry.schemas {
		if err := schema.Apply(ctx, store); err != nil {
			return err
		}
	}

	for domain, vers := range encodedRegistry {
		for ver, schema := range vers {
			schemaId := entity.Id(domain + "/schema." + ver)

			attrs := []entity.Attr{
				entity.Any(entity.Ident, types.Keyword(schemaId)),
				entity.Any(entity.Schema, entity.BytesValue(schema.encoded)),
			}

			for k, v := range schema.schema.ShortKinds {
				attrs = append(attrs,
					entity.Any(entity.SchemaKind, k),
					entity.Any(entity.SchemaKind, v),
				)
			}

			_, err := store.CreateEntity(ctx, entity.New(attrs), entity.WithOverwrite)
			if err != nil && !errors.Is(err, entity.ErrEntityAlreadyExists) {
				return err
			}

			for kw := range schema.schema.Kinds {
				_, err := store.CreateEntity(ctx, entity.New(
					entity.Ident, types.Keyword(kw),
					entity.EntitySchema, schemaId,
				), entity.WithOverwrite)
				if err != nil && !errors.Is(err, entity.ErrEntityAlreadyExists) {
					return err
				}
			}

		}
	}

	return nil
}

func (b *SchemaBuilder) Id(name string) entity.Id {
	eid := entity.Id(b.domain + "/" + name)
	if _, exists := b.attrs[eid]; !exists {
		panic("Attribute does not exist: " + string(eid))
	}

	return eid
}

type attrBuilder struct {
	card     entity.Id
	doc      string
	required bool
	indexed  bool
	session  bool
	tags     []string

	choices []entity.Id

	extra []entity.Attr
}

type AttrOption func(*attrBuilder)

func Many(b *attrBuilder) {
	b.card = entity.CardinalityMany
}

func Doc(doc string) AttrOption {
	return func(b *attrBuilder) {
		b.doc = doc
	}
}

func Required(b *attrBuilder) {
	b.required = true
}

func Indexed(b *attrBuilder) {
	b.indexed = true
}

func Session(b *attrBuilder) {
	b.session = true
}

func Tags(tags ...string) AttrOption {
	return func(b *attrBuilder) {
		b.tags = append(b.tags, tags...)
	}
}

func Choices(choices ...entity.Id) AttrOption {
	return func(b *attrBuilder) {
		b.choices = append(b.choices, choices...)
	}
}

func AdditionalAttrs(attrs ...entity.Attr) AttrOption {
	return func(b *attrBuilder) {
		b.extra = append(b.extra, attrs...)
	}
}

func (s *SchemaBuilder) Attr(name, id string, typ entity.Id, opts ...AttrOption) entity.Id {
	eid := entity.Id(id)

	if _, exists := s.attrs[eid]; exists {
		panic("Attribute already exists: " + string(eid))
	}

	var ab attrBuilder
	ab.card = entity.CardinalityOne // default to one

	for _, opt := range opts {
		opt(&ab)
	}

	attrs := []any{
		entity.Ident, types.Keyword(eid),
		entity.Doc, ab.doc,
		entity.Type, typ,
		entity.Cardinality, ab.card,
	}

	if ab.indexed {
		attrs = append(attrs, entity.Index, true)
	}

	if ab.session {
		attrs = append(attrs, entity.Session, true)
	}

	for _, tag := range ab.tags {
		attrs = append(attrs, entity.Tag, tag)
	}

	// Choices declare the set of valid values for the attribute. Surface them
	// as EnumValues so convertEntityToSchema lands them on AttributeSchema and
	// the validator can enforce membership (see MIR-1425). This is the same
	// attribute the Enum builder injects, so ref-typed choices and true enums
	// share one enforcement path.
	if len(ab.choices) > 0 {
		choices := make([]any, len(ab.choices))
		for i, c := range ab.choices {
			choices[i] = c
		}
		attrs = append(attrs, entity.EnumValues, entity.ArrayValue(choices...))
	}

	// AdditionalAttrs carries pre-built attrs (e.g. the Enum builder's
	// EnumValues) that would otherwise be dropped on the floor.
	for _, extra := range ab.extra {
		attrs = append(attrs, extra)
	}

	ent := entity.New(attrs...)

	s.attrs[eid] = ent

	return eid
}

func (s *SchemaBuilder) Label(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeLabel, opts...)
}

func (s *SchemaBuilder) String(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeStr, opts...)
}

func (s *SchemaBuilder) Keyword(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeKeyword, opts...)
}

func (s *SchemaBuilder) Bool(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeBool, opts...)
}

func (s *SchemaBuilder) Bytes(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeBytes, opts...)
}

func (s *SchemaBuilder) Int64(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeInt, opts...)
}

func (s *SchemaBuilder) Float(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeFloat, opts...)
}

func (s *SchemaBuilder) Time(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeTime, opts...)
}

func (s *SchemaBuilder) Duration(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeDuration, opts...)
}

func (s *SchemaBuilder) Enum(name, id string, values any, opts ...AttrOption) entity.Id {
	opts = append(opts, AdditionalAttrs(
		entity.Attr{ID: entity.EnumValues, Value: entity.ArrayValue(values)},
	))

	return s.Attr(name, id, entity.TypeEnum, opts...)
}

func (s *SchemaBuilder) Component(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeComponent, opts...)
}

func (s *SchemaBuilder) Ref(name, id string, opts ...AttrOption) entity.Id {
	return s.Attr(name, id, entity.TypeRef, opts...)
}

func (s *SchemaBuilder) Singleton(id string, opts ...AttrOption) entity.Id {
	eid := entity.Id(id)
	s.singletons = append(s.singletons, eid)
	return eid
}

func (b *SchemaBuilder) Builder(name string) *SchemaBuilder {
	return Builder(b.domain+"."+name, b.version)
}

// IndexedAttributeIDs returns a sorted list of all attribute IDs that are marked
// as indexed in the in-memory schema registry. This inspects the attribute entities
// registered via Builder/Register, not etcd.
func IndexedAttributeIDs() []entity.Id {
	var ids []entity.Id

	for _, sb := range defaultRegistry.schemas {
		for eid, ent := range sb.attrs {
			for _, attr := range ent.Attrs() {
				if attr.ID == entity.Index && attr.Value.Kind() == entity.KindBool && attr.Value.Bool() {
					ids = append(ids, eid)
					break
				}
			}
		}
	}

	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})

	return ids
}

// IndexHash computes a deterministic blake2b-256 hash of all indexed attribute IDs
// from the in-memory schema registry, encoded as base58. The hash changes when
// indexes are added or removed, enabling detection of schema changes at startup.
func IndexHash() string {
	ids := IndexedAttributeIDs()

	h, _ := blake2b.New256(nil)
	for _, id := range ids {
		h.Write([]byte(id))
		h.Write([]byte{0}) // separator
	}

	return base58.Encode(h.Sum(nil))
}
