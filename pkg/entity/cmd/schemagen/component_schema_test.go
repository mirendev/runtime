package main

import (
	"testing"

	"miren.dev/runtime/pkg/entity"
)

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

	// Generate the schema
	_, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Now verify the in-memory EncodedSchema structure
	// We need to examine the gen struct to check the EncodedSchema
	// Since GenerateSchema doesn't return the EncodedSchema, we need to
	// verify it through the generated code structure

	// Create the generator and process standalone components
	var componentSchemas = make(map[string]*entity.EncodedSchema)
	for compName, attrs := range sf.Components {
		var g gen
		g.usedAttrs = make(map[string]struct{})
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
			g.usedAttrs[fullAttrId] = struct{}{}
			g.attr(name, attr)
		}

		componentSchemas[compName] = g.ec
	}

	// Now process the kind that references the component
	var kindGen gen
	kindGen.usedAttrs = make(map[string]struct{})
	kindGen.componentSchemas = componentSchemas
	kindGen.kind = "service"
	kindGen.name = "service"
	kindGen.prefix = sf.Domain + ".service"
	kindGen.local = "Service"
	kindGen.sf = sf
	kindGen.ec = &entity.EncodedSchema{
		Domain:  sf.Domain,
		Name:    sf.Domain + "/service",
		Version: sf.Version,
	}

	for name, attr := range sf.Kinds["service"] {
		if attr.Attr == "" {
			attr.Attr = "service." + name
		}
		fullAttrId := sf.Domain + "/" + attr.Attr
		kindGen.usedAttrs[fullAttrId] = struct{}{}
		kindGen.attr(name, attr)
	}

	// Verify that the config field has a non-nil Component schema
	var configField *entity.SchemaField
	for _, field := range kindGen.ec.Fields {
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

	// Generate component schemas
	var componentSchemas = make(map[string]*entity.EncodedSchema)
	for compName, attrs := range sf.Components {
		var g gen
		g.usedAttrs = make(map[string]struct{})
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
			g.usedAttrs[fullAttrId] = struct{}{}
			g.attr(name, attr)
		}

		componentSchemas[compName] = g.ec
	}

	// Process the kind
	var kindGen gen
	kindGen.usedAttrs = make(map[string]struct{})
	kindGen.componentSchemas = componentSchemas
	kindGen.kind = "api"
	kindGen.name = "api"
	kindGen.prefix = sf.Domain + ".api"
	kindGen.local = "Api"
	kindGen.sf = sf
	kindGen.ec = &entity.EncodedSchema{
		Domain:  sf.Domain,
		Name:    sf.Domain + "/api",
		Version: sf.Version,
	}

	for name, attr := range sf.Kinds["api"] {
		if attr.Attr == "" {
			attr.Attr = "api." + name
		}
		fullAttrId := sf.Domain + "/" + attr.Attr
		kindGen.usedAttrs[fullAttrId] = struct{}{}
		kindGen.attr(name, attr)
	}

	// Find both fields
	var primaryField, secondaryField *entity.SchemaField
	for _, field := range kindGen.ec.Fields {
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
