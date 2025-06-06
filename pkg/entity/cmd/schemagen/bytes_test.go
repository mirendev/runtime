package main

import (
	"strings"
	"testing"
)

func TestBytesFieldEncoding(t *testing.T) {
	// Test schema with both single and many bytes fields
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"document": {
				"content": &schemaAttr{
					Type: "bytes",
					Doc:  "Single bytes field",
				},
				"attachments": &schemaAttr{
					Type: "bytes",
					Many: true,
					Doc:  "Many bytes field",
				},
			},
		},
	}

	// Generate the schema code
	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Test 1: Single bytes field should only encode when non-empty
	t.Run("SingleBytesEncodesWhenNonEmpty", func(t *testing.T) {
		// Find the Encode method
		encodeStart := strings.Index(code, "func (o *Document) Encode() (attrs []entity.Attr) {")
		if encodeStart == -1 {
			t.Fatal("Could not find Encode() method")
		}
		encodeEnd := strings.Index(code[encodeStart:], "\n}")
		if encodeEnd == -1 {
			t.Fatal("Could not find end of Encode() method")
		}
		encodeMethod := code[encodeStart : encodeStart+encodeEnd]

		// The single bytes field should check for len > 0 before appending
		if !strings.Contains(encodeMethod, "if len(o.Content) > 0 {") {
			t.Error("Single bytes field should only encode when length > 0")
			t.Logf("Encode method:\n%s", encodeMethod)
		}

		// Should not have the buggy condition
		if strings.Contains(encodeMethod, "if len(o.Content) == 0 {") {
			t.Error("Single bytes field should NOT encode when empty (len == 0)")
		}
	})

	// Test 2: Empty method should correctly identify empty bytes
	t.Run("EmptyMethodChecksBytes", func(t *testing.T) {
		// Find the Empty method
		emptyStart := strings.Index(code, "func (o *Document) Empty() bool {")
		if emptyStart == -1 {
			t.Fatal("Could not find Empty() method")
		}
		emptyEnd := strings.Index(code[emptyStart:], "\n}")
		if emptyEnd == -1 {
			t.Fatal("Could not find end of Empty() method")
		}
		emptyMethod := code[emptyStart : emptyStart+emptyEnd]

		// The Empty method should return false when bytes are non-empty
		if !strings.Contains(emptyMethod, "if len(o.Content) > 0 {") {
			t.Error("Empty() should check if bytes length > 0")
			t.Logf("Empty method:\n%s", emptyMethod)
		}

		// Following the check, it should return false (not empty)
		if !strings.Contains(emptyMethod, "return false") {
			t.Error("Empty() should return false when bytes are non-empty")
		}
	})

	// Test 3: Many bytes field should iterate when non-empty
	t.Run("ManyBytesIteratesWhenNonEmpty", func(t *testing.T) {
		// The many bytes field should use a for loop
		if !strings.Contains(code, "for _, v := range o.Attachments {") {
			t.Error("Many bytes field should iterate over values")
		}

		// Empty check for many bytes should check len != 0
		emptyStart := strings.Index(code, "func (o *Document) Empty() bool {")
		emptyEnd := strings.Index(code[emptyStart:], "\n}")
		emptyMethod := code[emptyStart : emptyStart+emptyEnd]

		if !strings.Contains(emptyMethod, "if len(o.Attachments) != 0 {") {
			t.Error("Empty() should check if many bytes length != 0")
		}
	})
}

func TestBytesFieldWithOtherTypes(t *testing.T) {
	// Test that bytes fields work correctly alongside other field types
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"mixed": {
				"name": &schemaAttr{
					Type: "string",
					Doc:  "Name field",
				},
				"data": &schemaAttr{
					Type: "bytes",
					Doc:  "Binary data",
				},
				"active": &schemaAttr{
					Type: "bool",
					Doc:  "Active flag",
				},
			},
		},
	}

	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Check if struct was generated
	if !strings.Contains(code, "type Mixed struct") {
		t.Error("Expected Mixed struct to be generated")
		t.Logf("Generated code:\n%s", code)
	}

	// Verify the struct has the correct field type (check with flexible spacing)
	if !strings.Contains(code, "Data") || !strings.Contains(code, "[]byte") {
		t.Error("Expected Data field to be []byte type")
		// Find the struct definition to debug
		structStart := strings.Index(code, "type Mixed struct")
		if structStart != -1 {
			structEnd := strings.Index(code[structStart:], "}")
			if structEnd != -1 {
				t.Logf("Mixed struct:\n%s", code[structStart:structStart+structEnd+1])
			}
		}
	}

	// Verify JSON tags
	if !strings.Contains(code, `json:"data,omitempty"`) {
		t.Error("Expected omitempty tag for optional bytes field")
	}
}

func TestRequiredBytesField(t *testing.T) {
	// Test required bytes field behavior
	sf := &schemaFile{
		Domain:  "test",
		Version: "v1",
		Kinds: map[string]schemaAttrs{
			"config": {
				"signature": &schemaAttr{
					Type:     "bytes",
					Required: true,
					Doc:      "Required signature",
				},
			},
		},
	}

	code, err := GenerateSchema(sf, "test")
	if err != nil {
		t.Fatalf("Failed to generate schema: %v", err)
	}

	// Required fields should not have omitempty
	if strings.Contains(code, `json:"signature,omitempty"`) {
		t.Error("Required bytes field should not have omitempty tag")
	}

	// Should have just the field name
	if !strings.Contains(code, `json:"signature"`) {
		t.Error("Required bytes field should have simple JSON tag")
	}

	// Even required bytes fields should check for empty before encoding
	// (to avoid sending empty byte slices unnecessarily)
	encodeStart := strings.Index(code, "func (o *Config) Encode() (attrs []entity.Attr) {")
	encodeEnd := strings.Index(code[encodeStart:], "\n}")
	encodeMethod := code[encodeStart : encodeStart+encodeEnd]

	if !strings.Contains(encodeMethod, "if len(o.Signature) > 0 {") {
		t.Error("Even required bytes fields should only encode when non-empty")
	}
}
