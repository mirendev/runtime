package model

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"miren.dev/runtime/pkg/entity"
)

// systemAttrs are the bookkeeping attributes that belong in a Document's header
// rather than in its body. Everything else an entity carries is content.
var systemAttrs = map[entity.Id]bool{
	entity.DBId:       true,
	entity.DBShortId:  true,
	entity.Ident:      true,
	entity.Revision:   true,
	entity.CreatedAt:  true,
	entity.UpdatedAt:  true,
	entity.EntityKind: true,
}

// attributeDefAttrs are the attributes that describe an attribute. An entity
// carrying these is part of the schema itself rather than application data.
var attributeDefAttrs = map[entity.Id]bool{
	entity.Type:           true,
	entity.Cardinality:    true,
	entity.Uniq:           true,
	entity.EntityElemType: true,
	entity.Doc:            true,
	entity.Index:          true,
	entity.Session:        true,
}

// BuildDocument turns an entity into its presentation-ready form.
//
// It never fails on a single bad facet: a kind whose schema will not resolve is
// recorded in Problems and its attributes fall through to Unclaimed, so a
// listing of thousands is not lost to one malformed entity.
func BuildDocument(ctx context.Context, sc *entity.SchemaCache, ent *entity.Entity, opts Options) *Document {
	doc := &Document{
		Id: ent.Id().String(),
	}

	if a, ok := ent.Get(entity.DBShortId); ok {
		doc.ShortId = a.Value.String()
	}

	doc.Revision = ent.GetRevision()

	if t := ent.GetCreatedAt(); !t.IsZero() {
		doc.CreatedAt = new(t)
	}

	if t := ent.GetUpdatedAt(); !t.IsZero() {
		doc.UpdatedAt = new(t)
	}

	// claimed tracks every attribute some facet accounted for, so what is left
	// over can be surfaced rather than silently dropped.
	claimed := make(map[entity.Id]bool)

	for _, kindAttr := range ent.GetAll(entity.EntityKind) {
		// Value.Id panics on any other kind, and a malformed kind attribute is
		// exactly the sort of thing a debug listing exists to surface.
		if kindAttr.Value.Kind() != entity.KindId {
			doc.Problems = append(doc.Problems,
				fmt.Sprintf("kind attribute is %s, expected a reference", kindAttr.Value.Kind()))
			continue
		}

		kindId := kindAttr.Value.Id()
		doc.Kinds = append(doc.Kinds, string(kindId))

		es, err := sc.GetKindSchema(ctx, kindId)
		if err != nil {
			doc.Problems = append(doc.Problems,
				fmt.Sprintf("no schema for kind %s: %v", kindId, err))
			continue
		}

		facet := Facet{
			Kind:    string(kindId),
			Label:   facetLabel(string(kindId)),
			Version: es.Version,
			Fields:  opts.buildFields(es, attrsById(ent), claimed),
		}

		doc.Facets = append(doc.Facets, facet)
	}

	// Metadata rides along on nearly everything, so it says the least about
	// what an entity is; let the kinds that do say something come first.
	sortMetadataLast(doc.Kinds)
	slices.SortStableFunc(doc.Facets, func(a, b Facet) int {
		return boolCmp(isMixin(a.Kind), isMixin(b.Kind))
	})

	if len(doc.Kinds) == 0 {
		doc.Attribute = attributeView(ent)
	}

	// Whatever no facet claimed, minus the header bookkeeping and minus
	// anything the attribute view already renders.
	for _, a := range ent.Attrs() {
		if claimed[a.ID] || systemAttrs[a.ID] {
			continue
		}

		if doc.Attribute != nil && attributeDefAttrs[a.ID] {
			continue
		}

		doc.Unclaimed = append(doc.Unclaimed, opts.rawField(a))
	}

	return doc
}

// attrsById groups an entity's attributes for lookup. Attributes are a
// multimap, so a repeated id collects rather than overwrites.
func attrsById(ent *entity.Entity) map[entity.Id][]entity.Attr {
	byId := make(map[entity.Id][]entity.Attr)
	for _, a := range ent.Attrs() {
		byId[a.ID] = append(byId[a.ID], a)
	}

	return byId
}

// buildFields renders the fields one schema describes, marking each attribute
// it consumes as claimed.
func (o Options) buildFields(es *entity.EncodedSchema, byId map[entity.Id][]entity.Attr, claimed map[entity.Id]bool) []Field {
	var fields []Field

	for _, f := range es.Fields {
		attrs := byId[f.Id]

		// Claim the field even when absent, so an empty optional field is not
		// mistaken for an attribute outside the schema.
		claimed[f.Id] = true

		if len(attrs) == 0 {
			continue
		}

		if f.Many {
			var (
				values    []any
				truncated bool
				largest   int
			)

			for _, a := range attrs {
				v, t, size := o.renderField(f, a.Value)
				if t && size > largest {
					largest = size
				}
				truncated = truncated || t
				values = append(values, v)
			}

			fields = append(fields, Field{
				Name:      f.Name,
				Type:      f.Type,
				Value:     values,
				Truncated: truncated,
				Size:      sizeIf(truncated, largest),
			})
			continue
		}

		v, truncated, size := o.renderField(f, attrs[0].Value)
		fields = append(fields, Field{Name: f.Name, Type: f.Type, Value: v, Truncated: truncated, Size: sizeIf(truncated, size)})
	}

	return fields
}

// renderField renders a value in the light of what its schema says it is. The
// schema knows things the value does not, like which enum member an id names
// and what a component's nested fields are called.
func (o Options) renderField(f *entity.SchemaField, v entity.Value) (any, bool, int) {
	switch f.Type {
	case "enum":
		if v.Kind() != entity.KindId {
			return o.renderValue(v)
		}

		id := v.Id()
		for name, enumId := range f.EnumValues {
			if enumId == id {
				return name, false, 0
			}
		}

		return string(id), false, 0

	case "component":
		if v.Kind() != entity.KindComponent {
			return o.renderValue(v)
		}

		comp := v.Component()
		if comp == nil {
			return nil, false, 0
		}

		// Name the nested fields from the component's own schema when we have
		// one; fall back to raw attribute ids when we do not.
		if f.Component == nil {
			return o.renderAttrsMap(comp.Attrs())
		}

		ignored := make(map[entity.Id]bool)

		var (
			truncated bool
			largest   int
		)

		m := make(map[string]any)
		for _, nf := range o.buildFields(f.Component, attrsById(entity.New(comp.Attrs())), ignored) {
			m[nf.Name] = nf.Value

			if nf.Truncated {
				truncated = true
				if nf.Size > largest {
					largest = nf.Size
				}
			}
		}

		return m, truncated, largest

	default:
		return o.renderValue(v)
	}
}

// rawField renders an attribute with no schema to guide it, keyed by its id.
func (o Options) rawField(a entity.Attr) Field {
	v, truncated, size := o.renderValue(a.Value)

	return Field{
		Name:      string(a.ID),
		Value:     v,
		Truncated: truncated,
		Size:      sizeIf(truncated, size),
	}
}

// attributeView recognises the kindless entities that make up the schema. They
// are a large and uniform population, and reading them as a generic attribute
// dump buries what they actually say.
func attributeView(ent *entity.Entity) *AttributeView {
	if _, ok := ent.Get(entity.Type); !ok {
		return nil
	}

	av := &AttributeView{}

	if a, ok := ent.Get(entity.Type); ok && a.Value.Kind() == entity.KindId {
		av.Type = strings.TrimPrefix(string(a.Value.Id()), "db/type.")
	}

	if a, ok := ent.Get(entity.Cardinality); ok && a.Value.Kind() == entity.KindId {
		av.Cardinality = strings.TrimPrefix(string(a.Value.Id()), "db/cardinality.")
	}

	if a, ok := ent.Get(entity.Uniq); ok && a.Value.Kind() == entity.KindId {
		av.Unique = strings.TrimPrefix(string(a.Value.Id()), "db/unique.")
	}

	if a, ok := ent.Get(entity.EntityElemType); ok && a.Value.Kind() == entity.KindId {
		av.ElementType = strings.TrimPrefix(string(a.Value.Id()), "db/type.")
	}

	if a, ok := ent.Get(entity.Doc); ok {
		av.Doc = a.Value.String()
	}

	if a, ok := ent.Get(entity.Index); ok && a.Value.Kind() == entity.KindBool {
		av.Indexed = a.Value.Bool()
	}

	if a, ok := ent.Get(entity.Session); ok && a.Value.Kind() == entity.KindBool {
		av.Session = a.Value.Bool()
	}

	return av
}

// facetLabel shortens a kind id for display: dev.miren.compute/kind.sandbox
// becomes compute/sandbox. The full id stays on the Facet for anyone who needs
// to match on it.
func facetLabel(kind string) string {
	domain, name, ok := strings.Cut(kind, "/")
	if !ok {
		return kind
	}

	domain = strings.TrimPrefix(domain, "dev.miren.")
	name = strings.TrimPrefix(name, "kind.")

	return domain + "/" + name
}

func isMixin(kind string) bool {
	return facetLabel(kind) == "core/metadata"
}

func sortMetadataLast(kinds []string) {
	slices.SortStableFunc(kinds, func(a, b string) int {
		return boolCmp(isMixin(a), isMixin(b))
	})
}

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

func sizeIf(truncated bool, size int) int {
	if !truncated {
		return 0
	}

	return size
}
