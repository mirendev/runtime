package entity

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/ext"
	"golang.org/x/crypto/blake2b"
	"miren.dev/runtime/pkg/cel/library"
	etypes "miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/mapx"
)

// Validator provides methods to validate attribute values against their schemas
type Validator struct {
	store EntityStore

	env *cel.Env

	cacheProgs  map[[32]byte]cel.Program
	ensureCache map[Id]map[Id]struct{}
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
		store:       store,
		env:         env,
		cacheProgs:  make(map[[32]byte]cel.Program),
		ensureCache: make(map[Id]map[Id]struct{}),
	}
}

func asEntityId(val any) (Id, error) {
	switch v := val.(type) {
	case Id:
		return v, nil
	case string:
		return Id(v), nil
	case etypes.Keyword:
		return Id(v), nil
	default:
		return "", fmt.Errorf("value not convertable to an entity ID")
	}
}

// ValidateEntity validates all attributes in an entity against their schemas
func (v *Validator) ValidateEntity(ctx context.Context, entity *Entity) error {
	require := map[Id]struct{}{}

	var valid []Attr

	for _, attr := range entity.Attrs {
		if attr.ID == Ensure {
			ensure, ok := attr.Value.Any().(Id)
			if !ok {
				return fmt.Errorf("attribute %s must be a ref", Ensure)
			}

			if m, ok := v.ensureCache[ensure]; ok {
				require = maps.Clone(m)
			} else {
				ee, err := v.store.GetEntity(ctx, ensure)
				if err != nil {
					return fmt.Errorf("attribute %s must be a valid entity ref", Ensure)
				}

				for _, elem := range ee.Attrs {
					if elem.ID == EntityAttrs {
						attrs, ok := elem.Value.Any().([]any)
						if !ok {
							return fmt.Errorf("attribute %s must be an array of strings", EntityAttrs)
						}

						for _, elem := range attrs {
							id, err := asEntityId(elem)
							if err != nil {
								return fmt.Errorf("attribute %s must be an array of EntityId, was %T: %w", EntityAttrs, elem, err)
							}

							require[id] = struct{}{}
						}
					}
				}

				v.ensureCache[ensure] = maps.Clone(require)
			}
		} else {
			valid = append(valid, attr)
		}
	}

	count := make(map[Id]int)
	for i, attr := range valid {
		count[attr.ID]++
		delete(require, attr.ID)
		if err := v.ValidateAttribute(ctx, &attr); err != nil {
			return err
		}

		valid[i] = attr
	}

	if len(require) > 0 {
		return fmt.Errorf("missing required attributes: %v", mapx.Keys(require))
	}

	// Now do a card check

	for _, attr := range valid {
		schema, err := v.store.GetAttributeSchema(ctx, attr.ID)
		if err != nil {
			return fmt.Errorf("schema not found for attribute %s: %w", attr.ID, err)
		}

		if !schema.AllowMany && count[attr.ID] > 1 {
			return fmt.Errorf("attribute %s has multiple values, only one supported", attr.ID)
		}
	}

	entity.Attrs = valid

	return nil
}

func (v *Validator) ValidateAttributes(ctx context.Context, attrs []Attr) error {
	// Check them one-off...

	count := make(map[Id]int)
	for i, attr := range attrs {
		count[attr.ID]++
		if err := v.ValidateAttribute(ctx, &attr); err != nil {
			return err
		}

		attrs[i] = attr
	}

	// Now do a card check

	for _, attr := range attrs {
		schema, err := v.store.GetAttributeSchema(ctx, attr.ID)
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
func (v *Validator) ValidateAttribute(ctx context.Context, attr *Attr) error {
	name := attr.ID

	schema, err := v.store.GetAttributeSchema(ctx, name)
	if err != nil {
		return fmt.Errorf("schema not found for attribute %s: %w", name, err)
	}

	// Validate value based on type
	switch schema.Type {
	case TypeAny:
		// ok
	case TypeKeyword:
		switch v := attr.Value.Any().(type) {
		case etypes.Keyword:
			// Valid
		case string:
			if !ValidKeyword(v) {
				return fmt.Errorf("attribute %s must be a valid keyword", name)
			}

			attr.Value = KeywordValue(etypes.Keyword(v))

		default:
			return fmt.Errorf("attribute %s must be a keyword or string, got %T", name, v)
		}
	case TypeStr:
		if _, ok := attr.Value.Any().(string); !ok {
			return fmt.Errorf("attribute %s must be a string (is %T)", name, attr.Value.Any())
		}
	case TypeBytes:
		if _, ok := attr.Value.Any().([]byte); !ok {
			return fmt.Errorf("attribute %s must be a byte array (is %T)", name, attr.Value.Any())
		}

	case TypeLabel:
		if _, ok := attr.Value.Any().(etypes.Label); !ok {
			return fmt.Errorf("attribute %s must be a label (is %T)", name, attr.Value.Any())
		}
	case TypeInt:
		// Go's CBOR libraries may unmarshal integers as different types
		switch v := attr.Value.Any().(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// Valid
		default:
			return fmt.Errorf("attribute %s must be an integer, got %T", name, v)
		}
	case TypeFloat:
		switch v := attr.Value.Any().(type) {
		case float32, float64:
			// Valid
		default:
			return fmt.Errorf("attribute %s must be a float, got %T", name, v)
		}
	case TypeBool:
		if _, ok := attr.Value.Any().(bool); !ok {
			return fmt.Errorf("attribute %s must be a boolean", name)
		}
	case TypeRef:
		str, ok := attr.Value.Any().(Id)
		if !ok {
			return fmt.Errorf("attribute %s must be a string representing an entity ID", name)
		}
		// Check if the referenced entity exists
		if _, err := v.store.GetEntity(ctx, str); err != nil {
			return fmt.Errorf("attribute %s references a non-existent entity: %w", name, err)
		}
	case TypeTime:
		// Timestamps can be stored as various integer types representing milliseconds since epoch
		switch v := attr.Value.Any().(type) {
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
	case TypeEnum:
		idx := slices.IndexFunc(schema.EnumValues, func(e Value) bool {
			return e.Equal(attr.Value)
		})
		if idx == -1 {
			return fmt.Errorf("attribute %s must be one of %v (was %v)", name, schema.EnumValues, attr.Value)
		}
	case TypeArray:
		tuple, ok := attr.Value.Any().([]any)
		if !ok {
			return fmt.Errorf("attribute %s must be an array", name)
		}

		for i, elem := range tuple {
			if err := v.validateToType(ctx, elem, schema.ElemType); err != nil {
				return fmt.Errorf("attribute %s[%d]: %w", name, i, err)
			}
		}
	case TypeDuration:
		if attr.Value.Kind() != KindDuration {
			return fmt.Errorf("attribute %s must be a duration", name)
		}

	case TypeComponent:
		if attr.Value.Kind() != KindComponent {
			return fmt.Errorf("attribute %s must be a component (was %s, %T)", name, attr.Value.Kind(), attr.Value.Any())
		}

		comp := attr.Value.Component()
		err := v.ValidateAttributes(ctx, comp.Attrs)
		if err != nil {
			return fmt.Errorf("attribute %s must be a valid component: %w", name, err)
		}

		// Components are not allowed to use Ident, so let's be sure they don't.
		_, ok := comp.Get(Ident)
		if ok {
			return fmt.Errorf("attribute %s (a component) must not have an Ident attribute", name)
		}

	default:
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
			"value":  attr.Value.Any(),
		})
		if err != nil {
			return fmt.Errorf("failed to run check program for attribute %s: %w", name, err)
		}

		if out != types.True {
			return fmt.Errorf("check failed for attribute %s: %v", name, out)
		}
	}

	return nil
}

// ValidateAttribute validates a single attribute against its schema
func (v *Validator) validateToType(ctx context.Context, val any, typ Id) error {
	// Validate value based on type
	switch typ {
	case TypeKeyword, TypeStr:
		if _, ok := val.(string); !ok {
			return fmt.Errorf("value must be a string")
		}
	case TypeInt:
		// Go's CBOR libraries may unmarshal integers as different types
		switch v := val.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			// Valid
		default:
			return fmt.Errorf("value must be an integer, got %T", v)
		}
	case TypeFloat:
		switch v := val.(type) {
		case float32, float64:
			// Valid
		default:
			return fmt.Errorf("value must be a float, got %T", v)
		}
	case TypeBool:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("value must be a boolean")
		}
	case TypeRef:
		str, ok := val.(Id)
		if !ok {
			return fmt.Errorf("value must be a string representing an entity ID")
		}
		// Check if the referenced entity exists
		if _, err := v.store.GetEntity(ctx, str); err != nil {
			return fmt.Errorf("value references a non-existent entity: %w", err)
		}
	case TypeTime:
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
