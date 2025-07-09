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

func TestDurationFieldEncoding(t *testing.T) {
	// Test that duration fields are properly encoded and decoded
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"config": {
				"timeout": &schemaAttr{
					Type: "duration",
					Doc:  "Request timeout duration",
				},
				"interval": &schemaAttr{
					Type:     "duration",
					Required: true,
					Doc:      "Polling interval",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Test 1: Check struct generation with duration fields
	t.Run("StructGeneration", func(t *testing.T) {
		if !strings.Contains(code, "type Config struct") {
			t.Error("Expected Config struct to be generated")
		}

		// Check that duration fields use time.Duration type
		if !strings.Contains(code, "Timeout  time.Duration") {
			t.Error("Expected Timeout field with time.Duration type")
			t.Logf("Generated code:\n%s", code)
		}

		if !strings.Contains(code, "Interval time.Duration") {
			t.Error("Expected Interval field with time.Duration type")
		}
	})

	// Test 2: Check JSON tags
	t.Run("JSONTags", func(t *testing.T) {
		// Optional field should have omitempty
		if !strings.Contains(code, `json:"timeout,omitempty"`) {
			t.Error("Optional duration field should have omitempty tag")
		}

		// Required field should not have omitempty
		if strings.Contains(code, `json:"interval,omitempty"`) {
			t.Error("Required duration field should not have omitempty tag")
		}

		if !strings.Contains(code, `json:"interval"`) {
			t.Error("Required duration field should have simple JSON tag")
		}
	})

	// Test 3: Check decoder implementation
	t.Run("Decoder", func(t *testing.T) {
		// Find the Decode method
		decodeStart := strings.Index(code, "func (o *Config) Decode(e entity.AttrGetter) {")
		if decodeStart == -1 {
			t.Fatal("Could not find Decode() method")
		}
		decodeEnd := strings.Index(code[decodeStart:], "\n}")
		if decodeEnd == -1 {
			t.Fatal("Could not find end of Decode() method")
		}
		decodeMethod := code[decodeStart : decodeStart+decodeEnd]

		// Check for duration decoding
		if !strings.Contains(decodeMethod, "KindDuration") {
			t.Error("Decoder should check for KindDuration")
		}

		if !strings.Contains(decodeMethod, ".Duration()") {
			t.Error("Decoder should call Duration() method to extract value")
		}
	})

	// Test 4: Check encoder implementation
	t.Run("Encoder", func(t *testing.T) {
		// Find the Encode method
		encodeStart := strings.Index(code, "func (o *Config) Encode() (attrs []entity.Attr) {")
		if encodeStart == -1 {
			t.Fatal("Could not find Encode() method")
		}
		encodeEnd := strings.Index(code[encodeStart:], "\n}")
		if encodeEnd == -1 {
			t.Fatal("Could not find end of Encode() method")
		}
		encodeMethod := code[encodeStart : encodeStart+encodeEnd]

		// Duration fields should use entity.Duration encoder
		if !strings.Contains(encodeMethod, "entity.Duration(") {
			t.Error("Encoder should use entity.Duration function")
			t.Logf("Encode method:\n%s", encodeMethod)
		}

		// Should check for empty on optional fields
		if !strings.Contains(encodeMethod, "!entity.Empty(o.Timeout)") {
			t.Error("Optional duration field should check for emptiness before encoding")
		}
	})

	// Test 5: Check Empty() method implementation
	t.Run("EmptyMethod", func(t *testing.T) {
		// Find the Empty method
		emptyStart := strings.Index(code, "func (o *Config) Empty() bool {")
		if emptyStart == -1 {
			t.Fatal("Could not find Empty() method")
		}
		emptyEnd := strings.Index(code[emptyStart:], "\n}")
		if emptyEnd == -1 {
			t.Fatal("Could not find end of Empty() method")
		}
		emptyMethod := code[emptyStart : emptyStart+emptyEnd]

		// Duration fields should be checked for emptiness
		if !strings.Contains(emptyMethod, "!entity.Empty(o.Timeout)") {
			t.Error("Empty() should check duration fields")
			t.Logf("Empty method:\n%s", emptyMethod)
		}

		if !strings.Contains(emptyMethod, "!entity.Empty(o.Interval)") {
			t.Error("Empty() should check all duration fields")
		}
	})

	// Test 6: Check schema declaration
	t.Run("SchemaDeclaration", func(t *testing.T) {
		// Should have Duration schema declaration
		if !strings.Contains(code, "sb.Duration(") {
			t.Error("Schema should declare duration fields with sb.Duration()")
		}

		// Check that required field has Required option
		initSchemaStart := strings.Index(code, "func (o *Config) InitSchema(sb *schema.SchemaBuilder) {")
		if initSchemaStart == -1 {
			t.Fatal("Could not find InitSchema() method")
		}
		initSchemaEnd := strings.Index(code[initSchemaStart:], "\n}")
		if initSchemaEnd == -1 {
			t.Fatal("Could not find end of InitSchema() method")
		}
		initSchemaMethod := code[initSchemaStart : initSchemaStart+initSchemaEnd]

		if !strings.Contains(initSchemaMethod, "schema.Required") {
			t.Error("Required duration field should have schema.Required option")
			t.Logf("InitSchema method:\n%s", initSchemaMethod)
		}
	})
}

func TestDurationFieldWithManyValues(t *testing.T) {
	// Test that duration fields with many=true generate []time.Duration arrays
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"schedule": {
				"delays": &schemaAttr{
					Type: "duration",
					Many: true,
					Doc:  "Multiple delay durations",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Check that the generated code includes []time.Duration type
	if !strings.Contains(code, "[]time.Duration") {
		t.Error("Expected []time.Duration for many duration field")
		t.Logf("Generated code:\n%s", code)
	}

	// Check struct field
	if !strings.Contains(code, "Delays []time.Duration") {
		t.Error("Expected Delays field with []time.Duration type")
	}

	// Check JSON tag
	if !strings.Contains(code, `json:"delays,omitempty"`) {
		t.Error("Expected proper JSON tag for array field")
	}

	// Check decoder handles many durations
	decodeStart := strings.Index(code, "func (o *Schedule) Decode(e entity.AttrGetter) {")
	if decodeStart == -1 {
		t.Fatal("Could not find Decode() method")
	}
	decodeEnd := strings.Index(code[decodeStart:], "\n}")
	if decodeEnd == -1 {
		t.Fatal("Could not find end of Decode() method")
	}
	decodeMethod := code[decodeStart : decodeStart+decodeEnd]

	// Should handle GetAll for many values
	if !strings.Contains(decodeMethod, "GetAll(ScheduleDelaysId)") {
		t.Error("Decoder should use GetAll for many values")
		t.Logf("Decode method:\n%s", decodeMethod)
	}

	// Check encoder handles many durations
	encodeStart := strings.Index(code, "func (o *Schedule) Encode() (attrs []entity.Attr) {")
	if encodeStart == -1 {
		t.Fatal("Could not find Encode() method")
	}
	encodeEnd := strings.Index(code[encodeStart:], "\n}")
	if encodeEnd == -1 {
		t.Fatal("Could not find end of Encode() method")
	}
	encodeMethod := code[encodeStart : encodeStart+encodeEnd]

	// Should loop through values
	if !strings.Contains(encodeMethod, "for _, v := range o.Delays") {
		t.Error("Encoder should loop through array values")
		t.Logf("Encode method:\n%s", encodeMethod)
	}

	// Should call entity.Duration for each value
	if !strings.Contains(encodeMethod, "entity.Duration(ScheduleDelaysId, v)") {
		t.Error("Encoder should call entity.Duration for each value")
	}
}

func TestDurationFieldIntegration(t *testing.T) {
	// Test duration fields alongside other field types
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"task": {
				"name": &schemaAttr{
					Type:     "string",
					Required: true,
					Doc:      "Task name",
				},
				"timeout": &schemaAttr{
					Type: "duration",
					Doc:  "Task timeout",
				},
				"retry_delay": &schemaAttr{
					Type: "duration",
					Doc:  "Delay between retries",
				},
				"enabled": &schemaAttr{
					Type: "bool",
					Doc:  "Whether task is enabled",
				},
				"priority": &schemaAttr{
					Type: "int",
					Doc:  "Task priority",
				},
			},
		},
	}

	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Check that all fields are present in the struct
	if !strings.Contains(code, "type Task struct") {
		t.Error("Expected Task struct to be generated")
	}

	// Verify all field types
	fieldChecks := []struct {
		field    string
		typeStr  string
		required bool
	}{
		{"Name", "string", true},
		{"Timeout", "time.Duration", false},
		{"RetryDelay", "time.Duration", false},
		{"Enabled", "bool", false},
		{"Priority", "int64", false},
	}

	for _, check := range fieldChecks {
		// Check for field with flexible spacing (could be one or more spaces/tabs)
		if !strings.Contains(code, check.field) || !strings.Contains(code, check.typeStr) {
			t.Errorf("Expected field %s with type %s", check.field, check.typeStr)
			// Find the struct definition to help debug
			structStart := strings.Index(code, "type Task struct")
			if structStart != -1 {
				structEnd := strings.Index(code[structStart:], "}")
				if structEnd != -1 {
					t.Logf("Task struct:\n%s", code[structStart:structStart+structEnd+1])
				}
			}
		}
	}

	// Check Empty() method handles all fields correctly
	emptyStart := strings.Index(code, "func (o *Task) Empty() bool {")
	emptyEnd := strings.Index(code[emptyStart:], "\n}")
	emptyMethod := code[emptyStart : emptyStart+emptyEnd]

	// All non-bool fields should be checked
	for _, check := range fieldChecks {
		if check.field != "Enabled" { // Bool fields are checked differently
			if !strings.Contains(emptyMethod, "!entity.Empty(o."+check.field+")") {
				t.Errorf("Empty() should check field %s", check.field)
			}
		}
	}
}

func TestEnumFieldEmptiness(t *testing.T) {
	// Test that enum fields correctly implement the Empty() check
	// The fix ensures that when an enum has a value (non-empty string),
	// the Empty() method returns false (meaning the entity is not empty)
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"deployment": {
				"status": &schemaAttr{
					Type:    "enum",
					Choices: []string{"pending", "running", "completed", "failed"},
					Doc:     "Deployment status",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Check that enum type is created
	if !strings.Contains(code, "type DeploymentStatus string") {
		t.Error("Expected DeploymentStatus enum type to be generated")
	}

	// Check that enum constants are created
	expectedConstants := []string{
		"DeploymentStatusPending",
		"DeploymentStatusRunning",
		"DeploymentStatusCompleted",
		"DeploymentStatusFailed",
	}

	for _, constant := range expectedConstants {
		if !strings.Contains(code, constant) {
			t.Errorf("Expected enum constant %s to be generated", constant)
		}
	}

	// Find the Empty() method to verify the fix
	emptyMethodPattern := "func (o *Deployment) Empty() bool {"
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

	emptyMethod := code[emptyMethodStart : emptyMethodStart+emptyMethodEnd+2]

	// The fix ensures that when an enum field has a value (!= ""),
	// the Empty() method returns false (entity is not empty)
	// Look for the corrected check: if o.Status != "" { return false }
	if !strings.Contains(emptyMethod, `if o.Status != ""`) {
		t.Error("Empty() method should check if enum Status is not empty string")
		t.Logf("Empty() method:\n%s", emptyMethod)
	}

	// Verify the structure of the emptiness check
	// It should return false when enum has a value (meaning entity is not empty)
	if !strings.Contains(emptyMethod, `return false`) {
		t.Error("Empty() method should return false when enum fields have values")
	}
}

func TestSingleLabelFieldEncoding(t *testing.T) {
	// Test that single-value label fields generate correct encoding code
	// This test exposes the bug where v.Key and v.Value are referenced
	// but v is not defined in the single-value context
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"resource": {
				"tag": &schemaAttr{
					Type: "label",
					Doc:  "Resource tag",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Check that the struct has a single Label field (not Labels)
	if !strings.Contains(code, "Tag types.Label") {
		t.Error("Expected Tag field with types.Label type")
		t.Logf("Generated code:\n%s", code)
	}

	// Find the Encode() method
	encodeMethodPattern := "func (o *Resource) Encode() (attrs []entity.Attr) {"
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

	encodeMethod := code[encodeMethodStart : encodeMethodStart+encodeMethodEnd+2]

	// The encoder should reference o.Tag.Key and o.Tag.Value directly
	// NOT v.Key and v.Value (which would cause a compilation error)
	if strings.Contains(encodeMethod, "v.Key") || strings.Contains(encodeMethod, "v.Value") {
		t.Error("Single-value label encoder should not reference v.Key or v.Value")
		t.Logf("Encode() method:\n%s", encodeMethod)
	}

	// Should contain the correct references
	if !strings.Contains(encodeMethod, "o.Tag.Key") || !strings.Contains(encodeMethod, "o.Tag.Value") {
		t.Error("Single-value label encoder should reference o.Tag.Key and o.Tag.Value")
		t.Logf("Encode() method:\n%s", encodeMethod)
	}

	// Verify the label encoding call structure
	if !strings.Contains(encodeMethod, "entity.Label(ResourceTagId, o.Tag.Key, o.Tag.Value)") {
		t.Error("Expected correct Label encoding call with o.Tag.Key and o.Tag.Value")
	}
}

func TestRegisterEncodedSchemaStableWithDifferentFieldOrder(t *testing.T) {
	// Test that RegisterEncodedSchema produces the same output regardless of field and kind order
	// This is important because Go maps iterate in random order
	// Running multiple iterations to catch non-deterministic behavior that may only occur occasionally

	// Schema with multiple kinds and fields
	sf := &schemaFile{
		Domain:  "test.order",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"item": {
				"alpha": &schemaAttr{
					Type: "string",
					Doc:  "Alpha field",
				},
				"beta": &schemaAttr{
					Type: "int",
					Doc:  "Beta field",
				},
				"gamma": &schemaAttr{
					Type: "bool",
					Doc:  "Gamma field",
				},
			},
			"user": {
				"name": &schemaAttr{
					Type: "string",
					Doc:  "User name",
				},
				"age": &schemaAttr{
					Type: "int",
					Doc:  "User age",
				},
				"active": &schemaAttr{
					Type: "bool",
					Doc:  "Is user active",
				},
			},
			"product": {
				"title": &schemaAttr{
					Type: "string",
					Doc:  "Product title",
				},
				"price": &schemaAttr{
					Type: "int",
					Doc:  "Product price",
				},
			},
		},
	}

	// Extract RegisterEncodedSchema line from generated code
	extractRegisterLine := func(code string) string {
		pattern := `schema.RegisterEncodedSchema("test.order", "v1", []byte(`
		start := strings.Index(code, pattern)
		if start == -1 {
			t.Fatal("Could not find RegisterEncodedSchema call")
		}
		end := strings.Index(code[start:], "))")
		if end == -1 {
			t.Fatal("Could not find end of RegisterEncodedSchema call")
		}
		return code[start : start+end+2]
	}

	// Generate the schema multiple times to catch non-deterministic behavior
	const iterations = 5
	var outputs []string

	for i := 0; i < iterations; i++ {
		code, err := GenerateSchema(sf, "test")
		if err != nil {
			t.Fatalf("Failed to generate schema on iteration %d: %v", i, err)
		}

		line := extractRegisterLine(code)
		outputs = append(outputs, line)
	}

	// Verify all outputs are identical
	for i := 1; i < iterations; i++ {
		if outputs[i] != outputs[0] {
			t.Fatalf("RegisterEncodedSchema output differs on iteration %d:\nFirst:  %s\nIteration %d: %s",
				i, outputs[0], i, outputs[i])
		}
	}
}
