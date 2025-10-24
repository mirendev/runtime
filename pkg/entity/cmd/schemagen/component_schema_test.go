package main

import (
	"testing"

	"miren.dev/runtime/pkg/entity"
)

// buildTestSchemas is a test helper that builds component and kind schemas
// for testing. It processes the schemaFile's Components and the specified kind,
// returning the component schemas map and the kind's EncodedSchema.
func buildTestSchemas(t *testing.T, sf *schemaFile, kindName string) (map[string]*entity.EncodedSchema, *entity.EncodedSchema) {
	t.Helper()

	componentSchemas := make(map[string]*entity.EncodedSchema)
	usedAttrs := make(map[string]struct{})

	// Build standalone component schemas
	for compName, attrs := range sf.Components {
		var g gen
		g.usedAttrs = usedAttrs
		g.componentSchemas = componentSchemas
		g.isComponent = true
		g.name = compName
		g.prefix = sf.Domain + ".component." + compName
		g.local = toCamal(compName)
		g.sf = sf
		g.ec = &entity.EncodedSchema{
			Domain:  sf.Domain,
			Name:    sf.Domain + "/component." + compName,
			Version: sf.Version,
		}

		for name, attr := range attrs {
			if attr.Attr == "" {
				attr.Attr = "component." + compName + "." + name
			}
			fullAttrId := sf.Domain + "/" + attr.Attr
			usedAttrs[fullAttrId] = struct{}{}
			g.attr(name, attr)
		}

		componentSchemas[compName] = g.ec
	}

	// Build the kind schema
	kindAttrs, ok := sf.Kinds[kindName]
	if !ok {
		t.Fatalf("Kind %s not found in schema", kindName)
	}

	var kindGen gen
	kindGen.usedAttrs = usedAttrs
	kindGen.componentSchemas = componentSchemas
	kindGen.kind = kindName
	kindGen.name = kindName
	kindGen.prefix = sf.Domain + "." + kindName
	kindGen.local = toCamal(kindName)
	kindGen.sf = sf
	kindGen.ec = &entity.EncodedSchema{
		Domain:  sf.Domain,
		Name:    sf.Domain + "/" + kindName,
		Version: sf.Version,
	}

	for name, attr := range kindAttrs {
		if attr.Attr == "" {
			attr.Attr = kindName + "." + name
		}
		fullAttrId := sf.Domain + "/" + attr.Attr
		usedAttrs[fullAttrId] = struct{}{}
		kindGen.attr(name, attr)
	}

	return componentSchemas, kindGen.ec
}

// TestComponentFieldSchemaPopulated verifies that when a kind field references
// a standalone component, the SchemaField.Component is properly populated.
// This is a regression test for a panic that occurred when encoding entities
// where Component was nil, causing naturalEncodeMap to crash on a nil pointer.
func TestComponentFieldSchemaPopulated(t *testing.T) {
	sf := &schemaFile{
		Domain:  "test.regression",
		Version: "v1",
		Components: map[string]schemaAttrs{
			"config_spec": {
				"host": &schemaAttr{
					Type: "string",
					Doc:  "Hostname",
				},
				"port": &schemaAttr{
					Type: "int",
					Doc:  "Port number",
				},
			},
		},
		Kinds: map[string]schemaAttrs{
			"service": {
				"name": &schemaAttr{
					Type: "string",
					Doc:  "Service name",
				},
				"config": &schemaAttr{
					Type: "config_spec",
					Doc:  "Service configuration",
				},
			},
		},
	}

	// Generate the schema code (validates it compiles without errors)
	_, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Build test schemas to verify the in-memory EncodedSchema structure
	_, serviceSchema := buildTestSchemas(t, sf, "service")

	// Verify that the config field has a non-nil Component schema
	var configField *entity.SchemaField
	for _, field := range serviceSchema.Fields {
		if field.Name == "config" {
			configField = field
			break
		}
	}

	if configField == nil {
		t.Fatal("Expected to find 'config' field in service schema")
	}

	if configField.Type != "component" {
		t.Errorf("Expected config field to have type 'component', got %s", configField.Type)
	}

	if configField.Component == nil {
		t.Error("REGRESSION: config field's Component schema is nil - this would cause a panic during encoding")
	}

	// Verify the Component schema is correct
	if configField.Component != nil {
		if configField.Component.Name != "test.regression/component.config_spec" {
			t.Errorf("Expected Component schema name 'test.regression/component.config_spec', got %s", configField.Component.Name)
		}

		// Verify the component schema has the expected fields
		if len(configField.Component.Fields) != 2 {
			t.Errorf("Expected Component schema to have 2 fields, got %d", len(configField.Component.Fields))
		}

		// Verify field names
		foundHost := false
		foundPort := false
		for _, f := range configField.Component.Fields {
			if f.Name == "host" {
				foundHost = true
			}
			if f.Name == "port" {
				foundPort = true
			}
		}

		if !foundHost {
			t.Error("Expected Component schema to have 'host' field")
		}
		if !foundPort {
			t.Error("Expected Component schema to have 'port' field")
		}
	}
}

// TestMultipleComponentReferencesShareSchema verifies that multiple fields
// referencing the same standalone component share the same Component schema.
func TestMultipleComponentReferencesShareSchema(t *testing.T) {
	sf := &schemaFile{
		Domain:  "test.regression",
		Version: "v1",
		Components: map[string]schemaAttrs{
			"endpoint": {
				"url": &schemaAttr{Type: "string"},
			},
		},
		Kinds: map[string]schemaAttrs{
			"api": {
				"primary":   &schemaAttr{Type: "endpoint"},
				"secondary": &schemaAttr{Type: "endpoint"},
			},
		},
	}

	// Build test schemas
	_, apiSchema := buildTestSchemas(t, sf, "api")

	// Find both fields
	var primaryField, secondaryField *entity.SchemaField
	for _, field := range apiSchema.Fields {
		if field.Name == "primary" {
			primaryField = field
		}
		if field.Name == "secondary" {
			secondaryField = field
		}
	}

	if primaryField == nil || secondaryField == nil {
		t.Fatal("Expected to find both 'primary' and 'secondary' fields")
	}

	// Both should have non-nil Component schemas
	if primaryField.Component == nil {
		t.Error("primary field's Component schema is nil")
	}
	if secondaryField.Component == nil {
		t.Error("secondary field's Component schema is nil")
	}

	// Both should point to the same schema instance (shared)
	if primaryField.Component != secondaryField.Component {
		t.Error("Expected both fields to share the same Component schema instance")
	}
}
