package saga

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// edgeType is the reflect.Type of saga.Edge, used to detect ordering-only fields.
var edgeType = reflect.TypeFor[Edge]()

// fieldMapping maps struct field names to saga keys based on struct tags.
type fieldMapping struct {
	fieldName string
	jsonKey   string // The JSON key name (from json tag or field name)
	sagaKey   string
	optional  bool
	isEdge    bool // true for saga.Edge fields (ordering-only, no data)
}

// extractMappings extracts saga key mappings from a struct type's tags.
// Uses the "saga" tag to specify the key name and options.
// Format: `saga:"keyname"` or `saga:"keyname,optional"`
// Use `saga:"-"` to skip a field.
func extractMappings(t reflect.Type) ([]fieldMapping, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct type, got %v", t.Kind())
	}

	var mappings []fieldMapping
	for field := range t.Fields() {
		tag := field.Tag.Get("saga")

		// Parse tag: "keyname" or "keyname,optional"
		var sagaKey string
		var optional bool
		if tag == "" {
			// Use lowercase field name as default
			sagaKey = strings.ToLower(field.Name)
		} else if tag == "-" {
			continue // Skip this field
		} else {
			parts := strings.Split(tag, ",")
			sagaKey = parts[0]
			if sagaKey == "" {
				// Tag like ",optional" - use default key name
				sagaKey = strings.ToLower(field.Name)
			}
			for _, opt := range parts[1:] {
				if opt == "optional" {
					optional = true
				}
			}
		}

		// Determine JSON key name from json tag or field name
		jsonKey := field.Name
		if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] != "" {
				jsonKey = parts[0]
			}
		}

		mappings = append(mappings, fieldMapping{
			fieldName: field.Name,
			jsonKey:   jsonKey,
			sagaKey:   sagaKey,
			optional:  optional,
			isEdge:    field.Type == edgeType,
		})
	}
	return mappings, nil
}

// typedAction wraps a typed execute/undo function pair as an Action.
type typedAction struct {
	name        string
	executeFunc reflect.Value
	undoFunc    reflect.Value
	inType      reflect.Type
	outType     reflect.Type
	inMappings  []fieldMapping
	outMappings []fieldMapping
}

// Execute calls the typed execute function, handling input/output marshaling.
func (a *typedAction) Execute(ctx context.Context, inputs ActionInputs) (any, error) {
	// Create input struct and populate from inputs
	inVal := reflect.New(a.inType).Elem()
	for _, m := range a.inMappings {
		if m.isEdge {
			continue // Edge fields are ordering-only, no data to populate
		}
		field := inVal.FieldByName(m.fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		// Check if the input exists
		if !inputs.Has(m.sagaKey) {
			if m.optional {
				continue // Skip missing optional inputs
			}
			return nil, fmt.Errorf("missing required input %q for field %q", m.sagaKey, m.fieldName)
		}

		// Create a pointer to the field type for unmarshaling
		target := reflect.New(field.Type()).Interface()
		if err := inputs.Get(m.sagaKey, target); err != nil {
			return nil, fmt.Errorf("getting input %q for field %q: %w", m.sagaKey, m.fieldName, err)
		}
		field.Set(reflect.ValueOf(target).Elem())
	}

	// Call the execute function
	results := a.executeFunc.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		inVal,
	})

	// Handle results: (output, error)
	outVal := results[0]
	errVal := results[1]

	if !errVal.IsNil() {
		return nil, errVal.Interface().(error)
	}

	return outVal.Interface(), nil
}

// Undo calls the typed undo function.
func (a *typedAction) Undo(ctx context.Context, inputs ActionInputs, output any) error {
	// Create input struct and populate from inputs
	inVal := reflect.New(a.inType).Elem()
	for _, m := range a.inMappings {
		if m.isEdge {
			continue // Edge fields are ordering-only, no data to populate
		}
		field := inVal.FieldByName(m.fieldName)
		if !field.IsValid() || !field.CanSet() {
			continue
		}

		// Check if the input exists
		if !inputs.Has(m.sagaKey) {
			if m.optional {
				continue // Skip missing optional inputs
			}
			return fmt.Errorf("missing required input %q for field %q", m.sagaKey, m.fieldName)
		}

		target := reflect.New(field.Type()).Interface()
		if err := inputs.Get(m.sagaKey, target); err != nil {
			return fmt.Errorf("getting input %q for field %q: %w", m.sagaKey, m.fieldName, err)
		}
		field.Set(reflect.ValueOf(target).Elem())
	}

	// Convert output to the expected type
	var outVal reflect.Value
	if output == nil {
		outVal = reflect.Zero(a.outType)
	} else {
		outVal = reflect.ValueOf(output)
		if a.outType.Kind() != reflect.Interface {
			// If output came from JSON deserialization, it might need conversion
			if outVal.Type() != a.outType {
				// Try JSON round-trip for type conversion
				data, err := json.Marshal(output)
				if err != nil {
					return fmt.Errorf("marshaling output for undo: %w", err)
				}
				newOut := reflect.New(a.outType).Interface()
				if err := json.Unmarshal(data, newOut); err != nil {
					return fmt.Errorf("unmarshaling output for undo: %w", err)
				}
				outVal = reflect.ValueOf(newOut).Elem()
			}
		}
	}

	// Call the undo function
	results := a.undoFunc.Call([]reflect.Value{
		reflect.ValueOf(ctx),
		inVal,
		outVal,
	})

	// Handle result: error
	errVal := results[0]
	if !errVal.IsNil() {
		return errVal.Interface().(error)
	}

	return nil
}

// wrapTypedAction creates an Action from typed execute and undo functions.
// executeFunc signature: func(ctx context.Context, in InType) (OutType, error)
// undoFunc signature: func(ctx context.Context, in InType, out OutType) error
func wrapTypedAction(name string, executeFunc, undoFunc any) (*typedAction, error) {
	execVal := reflect.ValueOf(executeFunc)
	undoVal := reflect.ValueOf(undoFunc)

	// Type references for validation
	ctxType := reflect.TypeFor[context.Context]()
	errType := reflect.TypeFor[error]()

	// Validate execute function signature
	execType := execVal.Type()
	if execType.Kind() != reflect.Func {
		return nil, fmt.Errorf("execute must be a function")
	}
	if execType.NumIn() != 2 {
		return nil, fmt.Errorf("execute must have 2 parameters (ctx, input), got %d", execType.NumIn())
	}
	if execType.NumOut() != 2 {
		return nil, fmt.Errorf("execute must return 2 values (output, error), got %d", execType.NumOut())
	}
	if !execType.In(0).Implements(ctxType) {
		return nil, fmt.Errorf("execute first parameter must be context.Context")
	}
	if !execType.Out(1).Implements(errType) {
		return nil, fmt.Errorf("execute second return must implement error")
	}

	// Validate undo function signature
	undoType := undoVal.Type()
	if undoType.Kind() != reflect.Func {
		return nil, fmt.Errorf("undo must be a function")
	}
	if undoType.NumIn() != 3 {
		return nil, fmt.Errorf("undo must have 3 parameters (ctx, input, output), got %d", undoType.NumIn())
	}
	if undoType.NumOut() != 1 {
		return nil, fmt.Errorf("undo must return 1 value (error), got %d", undoType.NumOut())
	}
	if !undoType.In(0).Implements(ctxType) {
		return nil, fmt.Errorf("undo first parameter must be context.Context")
	}
	if !undoType.Out(0).Implements(errType) {
		return nil, fmt.Errorf("undo return must implement error")
	}

	// Extract types
	inType := execType.In(1)
	outType := execType.Out(0)

	// Validate input type is a struct (not a pointer) to enable FieldByName in Execute/Undo
	if inType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("execute input must be a struct value, got %v", inType)
	}

	// Validate type consistency between execute and undo
	if undoType.In(1) != inType {
		return nil, fmt.Errorf("undo input type %v doesn't match execute input type %v",
			undoType.In(1), inType)
	}
	if undoType.In(2) != outType {
		return nil, fmt.Errorf("undo output type %v doesn't match execute output type %v",
			undoType.In(2), outType)
	}

	// Extract field mappings
	inMappings, err := extractMappings(inType)
	if err != nil {
		return nil, fmt.Errorf("extracting input mappings: %w", err)
	}
	outMappings, err := extractMappings(outType)
	if err != nil {
		return nil, fmt.Errorf("extracting output mappings: %w", err)
	}

	return &typedAction{
		name:        name,
		executeFunc: execVal,
		undoFunc:    undoVal,
		inType:      inType,
		outType:     outType,
		inMappings:  inMappings,
		outMappings: outMappings,
	}, nil
}

// inputKeys returns the saga keys this action reads from.
func (a *typedAction) inputKeys() []string {
	keys := make([]string, len(a.inMappings))
	for i, m := range a.inMappings {
		keys[i] = m.sagaKey
	}
	return keys
}

// outputKeys returns the saga keys this action writes to.
func (a *typedAction) outputKeys() []string {
	keys := make([]string, len(a.outMappings))
	for i, m := range a.outMappings {
		keys[i] = m.sagaKey
	}
	return keys
}
