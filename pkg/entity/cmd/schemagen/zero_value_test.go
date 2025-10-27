package main

import (
	"strings"
	"testing"
)

func TestRequiredIntFieldEncodingZeroValue(t *testing.T) {
	// Test that required int fields are always encoded, even when zero
	// This is critical for fields like DesiredInstances where 0 is a valid value (scale-to-zero)
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"pool": {
				"desired_instances": &schemaAttr{
					Type:     "int",
					Required: true,
					Doc:      "Target number of instances (0 means scale to zero)",
				},
				"current_instances": &schemaAttr{
					Type: "int",
					Doc:  "Current number of instances",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Find the Encode() method
	encodeMethodPattern := "func (o *Pool) Encode() (attrs []entity.Attr) {"
	encodeMethodStart := strings.Index(code, encodeMethodPattern)
	if encodeMethodStart == -1 {
		t.Fatal("Could not find Encode() method in generated code")
	}

	encodeMethodEnd := strings.Index(code[encodeMethodStart:], "\n}")
	if encodeMethodEnd == -1 {
		t.Fatal("Could not find end of Encode() method")
	}

	encodeMethod := code[encodeMethodStart : encodeMethodStart+encodeMethodEnd]

	// Required int fields should NOT use Empty() check (similar to bool handling)
	// The current bug: required ints still use Empty() check, so 0 values are not encoded
	if strings.Contains(encodeMethod, "!entity.Empty(o.DesiredInstances)") {
		t.Error("Required int field should not use Empty() check, as it prevents encoding zero values")
		t.Error("Zero is a valid value for DesiredInstances (scale-to-zero)")
		t.Logf("Encode() method:\n%s", encodeMethod)
	}

	// Should always encode required fields
	if !strings.Contains(encodeMethod, "entity.Int64(PoolDesiredInstancesId, o.DesiredInstances)") {
		t.Error("Required int field should always be encoded without Empty() check")
		t.Logf("Encode() method:\n%s", encodeMethod)
	}

	// Optional fields should still use Empty() check
	if !strings.Contains(encodeMethod, "!entity.Empty(o.CurrentInstances)") {
		t.Error("Optional int field should use Empty() check to avoid encoding zero values")
	}
}

func TestRequiredIntFieldJSONTags(t *testing.T) {
	// Verify that required int fields don't have omitempty in JSON tags
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"config": {
				"count": &schemaAttr{
					Type:     "int",
					Required: true,
					Doc:      "Required count",
				},
				"optional_count": &schemaAttr{
					Type: "int",
					Doc:  "Optional count",
				},
			},
		},
	}

	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Required int should not have omitempty
	if strings.Contains(code, `json:"count,omitempty"`) {
		t.Error("Required int field should not have omitempty tag")
	}

	if !strings.Contains(code, `json:"count"`) {
		t.Error("Required int field should have simple JSON tag")
	}

	// Optional int should have omitempty
	if !strings.Contains(code, `json:"optional_count,omitempty"`) {
		t.Error("Optional int field should have omitempty tag")
	}
}

func TestMultipleRequiredTypes(t *testing.T) {
	// Test that required flag works correctly for different field types
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"resource": {
				"name": &schemaAttr{
					Type:     "string",
					Required: true,
					Doc:      "Resource name",
				},
				"count": &schemaAttr{
					Type:     "int",
					Required: true,
					Doc:      "Resource count",
				},
				"enabled": &schemaAttr{
					Type:     "bool",
					Required: true,
					Doc:      "Whether enabled",
				},
				"timeout": &schemaAttr{
					Type:     "duration",
					Required: true,
					Doc:      "Timeout duration",
				},
			},
		},
	}

	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Find the Encode() method
	encodeMethodPattern := "func (o *Resource) Encode() (attrs []entity.Attr) {"
	encodeMethodStart := strings.Index(code, encodeMethodPattern)
	if encodeMethodStart == -1 {
		t.Fatal("Could not find Encode() method in generated code")
	}

	encodeMethodEnd := strings.Index(code[encodeMethodStart:], "\n}")
	if encodeMethodEnd == -1 {
		t.Fatal("Could not find end of Encode() method")
	}

	encodeMethod := code[encodeMethodStart : encodeMethodStart+encodeMethodEnd]

	// Required string should NOT use Empty() check (strings can be required but empty string may be valid)
	// Actually, for strings, Empty() check should still be used even if required, because
	// empty string is the zero value. But for int and bool, zero values are meaningful.

	// Required int should NOT use Empty() check
	if strings.Contains(encodeMethod, "!entity.Empty(o.Count)") {
		t.Error("Required int field should not use Empty() check")
		t.Logf("Encode() method:\n%s", encodeMethod)
	}

	// Required bool should NOT use Empty() check (bool is already handled correctly)
	if strings.Contains(encodeMethod, "!entity.Empty(o.Enabled)") {
		t.Error("Required bool field should not use Empty() check")
	}

	// Required duration should NOT use Empty() check (0 duration may be valid)
	if strings.Contains(encodeMethod, "!entity.Empty(o.Timeout)") {
		t.Error("Required duration field should not use Empty() check")
	}

	// Required string can still use Empty() check (empty string is typically not meaningful)
	// So we don't test for o.Name here
}

func TestScaleToZeroScenario(t *testing.T) {
	// Real-world scenario: SandboxPool with DesiredInstances that can be 0
	sf := &schemaFile{
		Domain:  "test.compute",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"sandbox_pool": {
				"service": &schemaAttr{
					Type: "string",
					Doc:  "Service name",
				},
				"desired_instances": &schemaAttr{
					Type:     "int",
					Required: true,
					Doc:      "Target number of sandbox instances (can be 0 for scale-to-zero)",
				},
				"current_instances": &schemaAttr{
					Type: "int",
					Doc:  "Current number of sandbox instances",
				},
				"ready_instances": &schemaAttr{
					Type: "int",
					Doc:  "Number of RUNNING sandboxes",
				},
			},
		},
	}

	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Find the Encode() method
	encodeMethodPattern := "func (o *SandboxPool) Encode() (attrs []entity.Attr) {"
	encodeMethodStart := strings.Index(code, encodeMethodPattern)
	if encodeMethodStart == -1 {
		t.Fatal("Could not find Encode() method in generated code")
	}

	encodeMethodEnd := strings.Index(code[encodeMethodStart:], "\n}")
	if encodeMethodEnd == -1 {
		t.Fatal("Could not find end of Encode() method")
	}

	encodeMethod := code[encodeMethodStart : encodeMethodStart+encodeMethodEnd]

	// DesiredInstances (required) should always be encoded
	if strings.Contains(encodeMethod, "!entity.Empty(o.DesiredInstances)") {
		t.Error("Required DesiredInstances should always be encoded, even when 0")
		t.Error("This breaks scale-to-zero functionality where DesiredInstances=0 is a valid state")
		t.Logf("Encode() method:\n%s", encodeMethod)
	}

	// CurrentInstances and ReadyInstances (optional) should use Empty() check
	if !strings.Contains(encodeMethod, "!entity.Empty(o.CurrentInstances)") {
		t.Error("Optional CurrentInstances should use Empty() check")
	}

	if !strings.Contains(encodeMethod, "!entity.Empty(o.ReadyInstances)") {
		t.Error("Optional ReadyInstances should use Empty() check")
	}

	// Verify the generated code has the unconditional append for DesiredInstances
	expectedLine := "attrs = append(attrs, entity.Int64(SandboxPoolDesiredInstancesId, o.DesiredInstances))"
	if !strings.Contains(encodeMethod, expectedLine) {
		t.Error("Required DesiredInstances should have unconditional encoding")
		t.Logf("Expected line:\n%s", expectedLine)
		t.Logf("Encode() method:\n%s", encodeMethod)
	}
}
