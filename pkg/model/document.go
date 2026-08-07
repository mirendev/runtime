package model

import "time"

// Document is the presentation-ready view of a single entity.
//
// The server builds it, the CLI renders it, and `--json` emits it verbatim, so
// the human and machine views are the same data and cannot drift. Values are
// natural JSON (strings, numbers, bools, arrays, objects) rather than a tagged
// union, so a consumer can reach straight for what it wants.
//
// A Document is complete except for byte and string values, which the builder
// elides when Options.MaxValueLen says to. That only happens when a caller asks
// for it, so `--json` always carries the whole entity.
type Document struct {
	Id       string `json:"id"`
	ShortId  string `json:"short_id,omitempty"`
	Revision int64  `json:"revision,omitempty"`

	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`

	// Kinds lists every facet the entity carries. Kinds compose: a scheduled
	// sandbox holds compute/kind.sandbox, compute/kind.schedule and
	// core/kind.metadata at once, and which facets are present is often the
	// thing you are debugging.
	Kinds []string `json:"kinds,omitempty"`

	// Facets holds one entry per kind whose schema resolved, in Kinds order.
	Facets []Facet `json:"facets,omitempty"`

	// Unclaimed holds attributes that no facet's schema accounts for. Each
	// facet only describes its own fields, so without this an attribute outside
	// every schema would be invisible.
	Unclaimed []Field `json:"unclaimed,omitempty"`

	// Attribute is set for the kindless entities that make up the schema
	// itself, where the db/* attributes are the content rather than bookkeeping.
	Attribute *AttributeView `json:"attribute,omitempty"`

	// Problems records facets and values that could not be rendered. A single
	// broken facet costs you that facet, not the entity.
	Problems []string `json:"problems,omitempty"`
}

// Elided reports whether the builder shortened any value, so a caller can tell
// the reader how to get the whole thing.
func (d *Document) Elided() bool {
	for _, f := range d.Facets {
		for _, fl := range f.Fields {
			if fl.Truncated {
				return true
			}
		}
	}

	for _, fl := range d.Unclaimed {
		if fl.Truncated {
			return true
		}
	}

	return false
}

// Facet is one kind's contribution to an entity.
type Facet struct {
	// Kind is the full entity kind id, e.g. dev.miren.compute/kind.sandbox.
	Kind string `json:"kind"`

	// Label is the short form used for display, e.g. compute/sandbox.
	Label string `json:"label"`

	Version string  `json:"version,omitempty"`
	Fields  []Field `json:"fields"`
}

// Field is one named value within a facet, or one unclaimed attribute.
type Field struct {
	Name string `json:"name"`

	// Type is the schema's name for this field ("string", "bytes", "component",
	// ...), or empty for an attribute no schema describes. It lets a consumer
	// tell a base64 blob from a short string without guessing at the value.
	Type string `json:"type,omitempty"`

	// Value is the natural representation: a scalar for simple fields, a slice
	// for multi-valued ones, a map for components.
	Value any `json:"value"`

	// Truncated marks a value the builder shortened, and Size carries the real
	// length in bytes so the elision is never ambiguous.
	Truncated bool `json:"truncated,omitempty"`
	Size      int  `json:"size,omitempty"`
}

// AttributeView renders the attribute definitions that make up the schema.
// They carry no kind, and a generic attribute dump buries what they actually
// say, so they get their own shape.
type AttributeView struct {
	Type        string `json:"type,omitempty"`
	Cardinality string `json:"cardinality,omitempty"`
	Unique      string `json:"unique,omitempty"`
	ElementType string `json:"element_type,omitempty"`
	Doc         string `json:"doc,omitempty"`
	Indexed     bool   `json:"indexed,omitempty"`
	Session     bool   `json:"session,omitempty"`
}

// Options controls how much of an entity a Document carries.
type Options struct {
	// MaxValueLen elides rendered byte and string values longer than this many
	// bytes, backing off to a rune boundary so the result stays valid UTF-8.
	// Zero means no limit, which is what the JSON path uses.
	MaxValueLen int
}
