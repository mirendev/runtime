package entity

type SchemaField struct {
	Name string `json:"name" cbor:"name"`
	Type string `json:"type" cbor:"type"`

	Id Id `json:"id" cbor:"id"`

	Many bool `json:"many,omitempty" cbor:"many,omitempty"`

	Enum             string          `json:"enum,omitempty" cbor:"enum,omitempty"`
	EnumValues       map[string]Id   `json:"enum_values,omitempty" cbor:"enum_values,omitempty"`
	EnumLegacyValues map[string][]Id `json:"enum_legacy_values,omitempty" cbor:"enum_legacy_values,omitempty"`
	Component        *EncodedSchema  `json:"component,omitempty" cbor:"component,omitempty"`
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
	id, ok := f.EnumValues[member]
	return RefValue(id), ok
}

// EnumMember maps a physical entity value back to its schema-facing member.
func (f *SchemaField) EnumMember(value Value) (string, bool) {
	for member, id := range f.EnumValues {
		if value.Equal(RefValue(id)) {
			return member, true
		}
	}
	for member, aliases := range f.EnumLegacyValues {
		for _, alias := range aliases {
			if value.Equal(RefValue(alias)) {
				return member, true
			}
		}
	}
	return "", false
}
