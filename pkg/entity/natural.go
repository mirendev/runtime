package entity

import "slices"

type SchemaField struct {
	Name string `json:"name" cbor:"name"`
	Type string `json:"type" cbor:"type"`

	Id Id `json:"id" cbor:"id"`

	Many bool `json:"many,omitempty" cbor:"many,omitempty"`

	Enum               string             `json:"enum,omitempty" cbor:"enum,omitempty"`
	EnumEncoding       string             `json:"enum_encoding,omitempty" cbor:"enum_encoding,omitempty"`
	EnumLegacyEncoding string             `json:"enum_legacy_encoding,omitempty" cbor:"enum_legacy_encoding,omitempty"`
	EnumMembers        []string           `json:"enum_members,omitempty" cbor:"enum_members,omitempty"`
	EnumValues         map[string]Id      `json:"enum_values,omitempty" cbor:"enum_values,omitempty"`
	EnumLegacyValues   map[string][]Value `json:"enum_legacy_values,omitempty" cbor:"enum_legacy_values,omitempty"`
	Component          *EncodedSchema     `json:"component,omitempty" cbor:"component,omitempty"`
}

type EncodedDomain struct {
	Name       string                    `json:"name" cbor:"name"`
	Version    string                    `json:"version" cbor:"version"`
	Kinds      map[string]*EncodedSchema `json:"kinds" cbor:"kinds"`
	ShortKinds map[string]string         `json:"short_kinds" cbor:"short_kinds"`
}

type EncodedSchema struct {
	Domain  string         `json:"domain" cbor:"domain"`
	Name    string         `json:"name" cbor:"name"`
	Version string         `json:"version" cbor:"version"`
	Kinds   []string       `json:"kinds" cbor:"kinds"`
	Fields  []*SchemaField `json:"fields" cbor:"fields"`

	PrimaryKind string `json:"primary_kind" cbor:"primary_kind"`
}

func (es *EncodedSchema) GetField(name string) *SchemaField {
	for _, field := range es.Fields {
		if field.Name == name {
			return field
		}
	}
	return nil
}

// EnumValue maps a schema-facing enum member to its physical entity value.
func (f *SchemaField) EnumValue(member string) (Value, bool) {
	if !slices.Contains(f.EnumMembers, member) {
		// Older encoded schemas only carried the ref mapping.
		if _, ok := f.EnumValues[member]; !ok {
			return Value{}, false
		}
	}
	if id, ok := f.EnumValues[member]; ok {
		return RefValue(id), true
	}

	// Older encoded schemas described the physical representation directly and
	// did not carry a canonical member-id map.
	switch f.EnumEncoding {
	case "string":
		return StringValue(member), true
	case "keyword":
		keyword := f.Enum + "." + member
		if !ValidKeyword(keyword) {
			return Value{}, false
		}
		return KeywordValue(keyword), true
	case "", "ref":
		return Value{}, false
	default:
		return Value{}, false
	}
}

// EnumMember maps a physical entity value back to its schema-facing member.
func (f *SchemaField) EnumMember(value Value) (string, bool) {
	for _, member := range f.EnumMembers {
		candidate, ok := f.EnumValue(member)
		if ok && candidate.Equal(value) {
			return member, true
		}
	}

	for member, aliases := range f.EnumLegacyValues {
		for _, alias := range aliases {
			if alias.Equal(value) {
				return member, true
			}
		}
	}

	// Older encoded schemas only carried the ref mapping.
	for member, id := range f.EnumValues {
		if value.Equal(RefValue(id)) {
			return member, true
		}
	}
	return "", false
}
