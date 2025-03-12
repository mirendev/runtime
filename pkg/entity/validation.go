package entity

import (
	"fmt"
	"slices"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/ext"
	"golang.org/x/crypto/blake2b"
	"miren.dev/runtime/pkg/cel/library"
)

// Validator provides methods to validate attribute values against their schemas
type Validator struct {
	store EntityStore

	env *cel.Env

	cacheProgs map[[32]byte]cel.Program
}

// NewValidator creates a new attribute validator
func NewValidator(store EntityStore) *Validator {
	env, err := cel.NewEnv(
		cel.Variable("entity", types.StringType),
		cel.Variable("attr", types.StringType),
		cel.Variable("value", types.DynType),

		ext.Encoders(),
		ext.Math(),
		ext.Sets(),
		ext.Strings(),

		library.IP(),
		library.CIDR(),
		library.URLs(),
		library.Regex(),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to create CEL environment: %v", err))
	}

	return &Validator{
		store:      store,
		env:        env,
		cacheProgs: make(map[[32]byte]cel.Program),
	}
}

// ValidateEntity validates all attributes in an entity against their schemas
func (v *Validator) ValidateEntity(entity *Entity) error {
	for _, attr := range entity.Attrs {
		if err := v.ValidateAttribute(attr); err != nil {
			return err
		}
	}
	return nil
}

func (v *Validator) ValidateAttributes(attrs []Attr) error {
	// Check them one-off...

	count := make(map[EntityId]int)
	for _, attr := range attrs {
		count[attr.ID]++
		if err := v.ValidateAttribute(attr); err != nil {
			return err
		}
	}

	// Now do a card check

	for _, attr := range attrs {
		schema, err := v.store.GetAttributeSchema(attr.ID)
		if err != nil {
			return fmt.Errorf("schema not found for attribute %s: %w", attr.ID, err)
		}

		if !schema.AllowMany && count[attr.ID] > 1 {
			return fmt.Errorf("attribute %s has multiple values, only one supported", attr.ID)
		}
	}

	return nil
}

// ValidateAttribute validates a single attribute against its schema
func (v *Validator) ValidateAttribute(attr Attr) error {
	name := attr.ID

	schema, err := v.store.GetAttributeSchema(name)
	if err != nil {
		return fmt.Errorf("schema not found for attribute %s: %w", name, err)
	}

	// Validate value based on type
	switch schema.Type {
	case EntityTypeAny:
		// ok
	case EntityTypeKW, EntityTypeStr:
		if _, ok := attr.Value.(string); !ok {
			return fmt.Errorf("attribute %s must be a string (is %T)", name, attr.Value)
		}
	case EntityTypeInt:
		// Go's CBOR libraries may unmarshal integers as different types
		switch v := attr.Value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// Valid
		default:
			return fmt.Errorf("attribute %s must be an integer, got %T", name, v)
		}
	case EntityTypeFloat:
		switch v := attr.Value.(type) {
		case float32, float64:
			// Valid
		default:
			return fmt.Errorf("attribute %s must be a float, got %T", name, v)
		}
	case EntityTypeBool:
		if _, ok := attr.Value.(bool); !ok {
			return fmt.Errorf("attribute %s must be a boolean", name)
		}
	case EntityTypeRef:
		str, ok := attr.Value.(EntityId)
		if !ok {
			return fmt.Errorf("attribute %s must be a string representing an entity ID", name)
		}
		// Check if the referenced entity exists
		if _, err := v.store.GetEntity(str); err != nil {
			return fmt.Errorf("attribute %s references a non-existent entity: %w", name, err)
		}
	case EntityTypeTime:
		// Timestamps can be stored as various integer types representing milliseconds since epoch
		switch v := attr.Value.(type) {
		case int64:
			// Valid
		case string:
			// Try to parse as RFC3339
			if _, err := time.Parse(time.RFC3339, v); err != nil {
				// Try to parse as RFC3339Nano
				if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
					return fmt.Errorf("attribute %s is not a valid timestamp: %w", name, err)
				}
			}
		default:
			return fmt.Errorf("attribute %s must be a timestamp (int64 or RFC3339 string), got %T", name, v)
		}
	case EntityTypeEnum:
		idx := slices.Index(schema.EnumValues, attr.Value)
		if idx == -1 {
			return fmt.Errorf("attribute %s must be one of %v (was %v)", name, schema.EnumValues, attr.Value)
		}
	case EntityTypeArray:
		tuple, ok := attr.Value.([]any)
		if !ok {
			return fmt.Errorf("attribute %s must be an array", name)
		}

		for i, elem := range tuple {
			if err := v.validateToType(elem, schema.ElemType); err != nil {
				return fmt.Errorf("attribute %s[%d]: %w", name, i, err)
			}
		}
	default:
		// Custom type!

		return fmt.Errorf("unknown attribute type %s for attribute %s", schema.Type, name)
	}

	for _, prog := range schema.CheckProgs {
		cacheKey := blake2b.Sum256([]byte(prog))

		run, ok := v.cacheProgs[cacheKey]
		if !ok {
			ast, issues := v.env.Compile(prog)
			if issues != nil && issues.Err() != nil {
				return fmt.Errorf("failed to compile check program for attribute %s: %w", name, issues.Err())
			}

			ast, issues = v.env.Check(ast)
			if issues != nil && issues.Err() != nil {
				return fmt.Errorf("failed to check check program for attribute %s: %w", name, issues.Err())
			}

			run, err = v.env.Program(ast)
			if err != nil {
				return fmt.Errorf("failed to create program for attribute %s: %w", name, err)
			}

			v.cacheProgs[cacheKey] = run
		}

		out, _, err := run.Eval(map[string]any{
			"entity": attr.ID,
			"attr":   attr.ID,
			"value":  attr.Value,
		})
		if err != nil {
			return fmt.Errorf("failed to run check program for attribute %s: %w", name, err)
		}

		spew.Dump(out)

		if out != types.True {
			return fmt.Errorf("check failed for attribute %s: %v", name, out)
		}
	}

	return nil
}

// ValidateAttribute validates a single attribute against its schema
func (v *Validator) validateToType(val any, typ EntityId) error {
	// Validate value based on type
	switch typ {
	case EntityTypeKW, EntityTypeStr:
		if _, ok := val.(string); !ok {
			return fmt.Errorf("value must be a string")
		}
	case EntityTypeInt:
		// Go's CBOR libraries may unmarshal integers as different types
		switch v := val.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// Valid
		default:
			return fmt.Errorf("value must be an integer, got %T", v)
		}
	case EntityTypeFloat:
		switch v := val.(type) {
		case float32, float64:
			// Valid
		default:
			return fmt.Errorf("value must be a float, got %T", v)
		}
	case EntityTypeBool:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("value must be a boolean")
		}
	case EntityTypeRef:
		str, ok := val.(EntityId)
		if !ok {
			return fmt.Errorf("value must be a string representing an entity ID")
		}
		// Check if the referenced entity exists
		if _, err := v.store.GetEntity(str); err != nil {
			return fmt.Errorf("value references a non-existent entity: %w", err)
		}
	case EntityTypeTime:
		// Timestamps can be stored as various integer types representing milliseconds since epoch
		switch v := val.(type) {
		case int64:
			// Valid
		case string:
			// Try to parse as RFC3339
			if _, err := time.Parse(time.RFC3339, v); err != nil {
				// Try to parse as RFC3339Nano
				if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
					return fmt.Errorf("value is not a valid timestamp: %w", err)
				}
			}
		default:
			return fmt.Errorf("value must be a timestamp (int64 or RFC3339 string), got %T", v)
		}
	default:
		return fmt.Errorf("unknown type %s for value", typ)
	}

	return nil
}
