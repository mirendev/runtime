package schema

import (
	"context"
	"errors"
	"sync"

	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
)

type SchemaRegistry struct {
	mu      sync.Mutex
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
		_, err := store.CreateEntity(ctx, entity.Attrs(
			entity.Ident, types.Keyword(eid),
		))
		if err != nil && !errors.Is(err, entity.ErrEntityAlreadyExists) {
			return err
		}
	}

	for _, e := range b.attrs {
		_, err := store.CreateEntity(ctx, e.Attrs)
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

			attrs := entity.Attrs(
				entity.Ident, types.Keyword(schemaId),
				entity.Schema, entity.BytesValue(schema.encoded),
			)

			for k, v := range schema.schema.ShortKinds {
				attrs = append(attrs, entity.Attrs(
					entity.SchemaKind, k,
					entity.SchemaKind, v,
				)...)
			}

			_, err := store.CreateEntity(ctx, attrs)
			if err != nil && !errors.Is(err, entity.ErrEntityAlreadyExists) {
				return err
			}

			for kw := range schema.schema.Kinds {
				_, err := store.CreateEntity(ctx, entity.Attrs(
					entity.Ident, types.Keyword(kw),
					entity.EntitySchema, schemaId,
				))
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

	choises []entity.Id

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

func Choices(choices ...entity.Id) AttrOption {
	return func(b *attrBuilder) {
		b.choises = append(b.choises, choices...)
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

	ent := &entity.Entity{
		Attrs: entity.Attrs(
			entity.Ident, types.Keyword(eid),
			entity.Doc, ab.doc,
			entity.Type, typ,
			entity.Cardinality, ab.card,
		),
	}

	if ab.indexed {
		ent.Attrs = append(ent.Attrs, entity.Bool(entity.Index, true))
	}

	if ab.session {
		ent.Attrs = append(ent.Attrs, entity.Bool(entity.Session, true))
	}

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
