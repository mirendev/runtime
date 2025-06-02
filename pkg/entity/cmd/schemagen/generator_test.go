package main

import (
	"strings"
	"testing"
)

func TestBoolEncodingFalseValue(t *testing.T) {
	// Create a test schema with a bool field
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"example": {
				"enabled": &schemaAttr{
					Type: "bool",
					Doc:  "Whether the feature is enabled",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Check that the generated code includes a Bool encoder that doesn't check for emptiness
	// The issue is that the original code uses `Empty()` check, which returns true for `false` values
	// So the generated code should NOT have: `if !entity.Empty(o.Enabled)`
	// Instead it should always encode the bool value

	// Look for the encoder section for the bool field (case-sensitive field names)
	if !strings.Contains(code, "attrs = append(attrs, entity.Bool(ExampleEnabledId, o.Enabled))") {
		t.Error("Generated code should always encode bool values without Empty() check")
		t.Logf("Generated code:\n%s", code)
	}

	// Ensure we don't have the problematic Empty() check for bools
	if strings.Contains(code, "!entity.Empty(o.Enabled)") {
		t.Error("Generated code should not use Empty() check for bool types, as it prevents encoding false values")
		t.Logf("Generated code:\n%s", code)
	}
}

func TestBoolEncodingStructCreation(t *testing.T) {
	// Create a test schema with a bool field
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"config": {
				"debug": &schemaAttr{
					Type: "bool",
					Doc:  "Debug mode flag",
				},
				"verbose": &schemaAttr{
					Type: "bool",
					Doc:  "Verbose logging flag",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Check that struct is created with bool fields
	expectedStruct := "type Config struct"
	if !strings.Contains(code, expectedStruct) {
		t.Errorf("Expected to find struct definition: %s", expectedStruct)
	}

	// Check that bool fields are defined (with proper casing and tags)
	if !strings.Contains(code, "Debug   bool") {
		t.Error("Expected Debug bool field in struct")
		t.Logf("Generated code:\n%s", code)
	}

	if !strings.Contains(code, "Verbose bool") {
		t.Error("Expected Verbose bool field in struct")
		t.Logf("Generated code:\n%s", code)
	}
}

func TestStringFieldEncodingWithEmpty(t *testing.T) {
	// Create a test schema with a string field to compare with bool behavior
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"item": {
				"name": &schemaAttr{
					Type: "string",
					Doc:  "Item name",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// String fields SHOULD use Empty() check (this is correct behavior)
	if !strings.Contains(code, "!entity.Empty(o.Name)") {
		t.Error("String fields should use Empty() check to avoid encoding empty strings")
	}
}

func TestBoolFieldRequired(t *testing.T) {
	// Test that required bool fields are handled correctly
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"feature": {
				"enabled": &schemaAttr{
					Type:     "bool",
					Required: true,
					Doc:      "Feature enabled status",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Check that the field is marked as required in JSON tags (no omitempty)
	if !strings.Contains(code, `Enabled bool`) {
		t.Error("Expected bool field to be created")
	}

	// Required fields should not have omitempty in JSON tags
	if strings.Contains(code, `json:"enabled,omitempty"`) {
		t.Error("Required bool field should not have omitempty tag")
	}

	// Should have just the field name
	if !strings.Contains(code, `json:"enabled"`) {
		t.Error("Required bool field should have simple JSON tag")
	}
}

func TestBoolFieldOptional(t *testing.T) {
	// Test that optional bool fields are handled correctly
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"setting": {
				"enabled": &schemaAttr{
					Type: "bool",
					Doc:  "Optional feature flag",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Optional fields should have omitempty in JSON tags
	if !strings.Contains(code, `json:"enabled,omitempty"`) {
		t.Error("Optional bool field should have omitempty tag")
	}
}
