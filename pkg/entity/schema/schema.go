package schema

import (
	"context"
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
		if err != nil {
			return err
		}
	}

	for _, e := range b.attrs {
		_, err := store.CreateEntity(ctx, e.Attrs)
		if err != nil {
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

func (s *SchemaBuilder) Attr(name string, typ entity.Id, opts ...AttrOption) entity.Id {
	eid := entity.Id(s.domain + "/" + name)

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
		ent.Attrs = append(ent.Attrs, entity.Attr{ID: entity.Index, Value: entity.BoolValue(true)})
	}

	s.attrs[eid] = ent

	return eid
}

func (s *SchemaBuilder) Label(name string, opts ...AttrOption) entity.Id {
	return s.Attr(name, entity.TypeLabel, opts...)
}

func (s *SchemaBuilder) String(name string, opts ...AttrOption) entity.Id {
	return s.Attr(name, entity.TypeStr, opts...)
}

func (s *SchemaBuilder) Keyword(name string, opts ...AttrOption) entity.Id {
	return s.Attr(name, entity.TypeKeyword, opts...)
}

func (s *SchemaBuilder) Bool(name string, opts ...AttrOption) entity.Id {
	return s.Attr(name, entity.TypeBool, opts...)
}

func (s *SchemaBuilder) Int64(name string, opts ...AttrOption) entity.Id {
	return s.Attr(name, entity.TypeInt, opts...)
}

func (s *SchemaBuilder) Float(name string, opts ...AttrOption) entity.Id {
	return s.Attr(name, entity.TypeFloat, opts...)
}

func (s *SchemaBuilder) Time(name string, opts ...AttrOption) entity.Id {
	return s.Attr(name, entity.TypeTime, opts...)
}

func (s *SchemaBuilder) Enum(name string, values any, opts ...AttrOption) entity.Id {
	opts = append(opts, AdditionalAttrs(
		entity.Attr{ID: entity.EnumValues, Value: entity.ArrayValue(values)},
	))

	return s.Attr(name, entity.TypeEnum, opts...)
}

func (s *SchemaBuilder) Component(name string, opts ...AttrOption) entity.Id {
	return s.Attr(name, entity.TypeComponent, opts...)
}

func (s *SchemaBuilder) Ref(name string, opts ...AttrOption) entity.Id {
	return s.Attr(name, entity.TypeRef, opts...)
}

func (s *SchemaBuilder) Singleton(name string, opts ...AttrOption) entity.Id {
	eid := entity.Id(s.domain + "/" + name)
	s.singletons = append(s.singletons, eid)
	return eid
}

func (b *SchemaBuilder) Builder(name string) *SchemaBuilder {
	return Builder(b.domain+"."+name, b.version)
}
