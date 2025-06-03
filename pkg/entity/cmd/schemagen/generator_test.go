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
	// So the generated code should NOT have: `if !entity.Empty(o.Enabled)` in the Encode() method
	// Instead it should always encode the bool value

	// Look for the encoder section for the bool field (case-sensitive field names)
	if !strings.Contains(code, "attrs = append(attrs, entity.Bool(ExampleEnabledId, o.Enabled))") {
		t.Error("Generated code should always encode bool values without Empty() check")
		t.Logf("Generated code:\n%s", code)
	}

	// Extract the Encode() method to check it specifically
	encodeMethodPattern := "func (o *Example) Encode() (attrs []entity.Attr) {"
	encodeMethodStart := strings.Index(code, encodeMethodPattern)
	if encodeMethodStart == -1 {
		t.Error("Could not find Encode() method in generated code")
		return
	}

	// Find the end of the Encode() method
	encodeMethodEnd := strings.Index(code[encodeMethodStart:], "\n}")
	if encodeMethodEnd == -1 {
		t.Error("Could not find end of Encode() method")
		return
	}

	encodeMethod := code[encodeMethodStart : encodeMethodStart+encodeMethodEnd]

	// Ensure the Encode() method doesn't have the problematic Empty() check for bools
	if strings.Contains(encodeMethod, "!entity.Empty(o.Enabled)") {
		t.Error("Encode() method should not use Empty() check for bool types, as it prevents encoding false values")
		t.Logf("Encode() method:\n%s", encodeMethod)
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

func TestEntityEmptyConsidersAllFields(t *testing.T) {
	// Test that Entity.Empty() method considers bool fields along with other fields
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"mixed": {
				"name": &schemaAttr{
					Type: "string",
					Doc:  "String field",
				},
				"enabled": &schemaAttr{
					Type: "bool",
					Doc:  "Bool field",
				},
				"count": &schemaAttr{
					Type: "int",
					Doc:  "Int field",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// The Empty() method should check all non-bool fields for emptiness
	// Bool fields should also be considered (when they have non-zero values, entity should not be empty)
	// Look for the Empty() method
	if !strings.Contains(code, "func (o *Mixed) Empty() bool {") {
		t.Error("Expected Empty() method to be generated")
		t.Logf("Generated code:\n%s", code)
		return
	}

	// The Empty() method should check string field
	if !strings.Contains(code, "!entity.Empty(o.Name)") {
		t.Error("Empty() method should check string field")
	}

	// The Empty() method should check int field
	if !strings.Contains(code, "!entity.Empty(o.Count)") {
		t.Error("Empty() method should check int field")
	}

	// The Empty() method should also consider bool field
	// When a bool is true (non-zero), the entity should not be empty
	// Look for a check that considers the bool field in the Empty() method
	emptyMethodPattern := "func (o *Mixed) Empty() bool {"
	emptyMethodStart := strings.Index(code, emptyMethodPattern)
	if emptyMethodStart == -1 {
		t.Error("Could not find Empty() method in generated code")
		return
	}

	// Find the closing brace of the Empty() method
	emptyMethodEnd := strings.Index(code[emptyMethodStart:], "\n}")
	if emptyMethodEnd == -1 {
		t.Error("Could not find end of Empty() method")
		return
	}

	emptyMethod := code[emptyMethodStart : emptyMethodStart+emptyMethodEnd]

	// The Empty() method should check the bool field - when it's true, entity should not be empty
	if !strings.Contains(emptyMethod, "o.Enabled") {
		t.Error("Empty() method should consider bool field to determine if entity is empty")
		t.Logf("Empty() method:\n%s", emptyMethod)
	}
}
