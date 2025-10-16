package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateComponentAttribute(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	tests := []struct {
		name    string
		attr    Attr
		wantErr bool
		errStr  string
	}{
		{
			name: "valid component",
			attr: Attr{
				ID: Id("test/component"),
				Value: ComponentValue([]Attr{
					Any(Doc, "Test component"),
				}),
			},
			wantErr: false,
		},
		{
			name: "component with invalid attribute",
			attr: Attr{
				ID: Id("test/component"),
				Value: ComponentValue([]Attr{
					Any(Doc, 123), // Should be string
				}),
			},
			wantErr: true,
			errStr:  "must be a string",
		},
		{
			name: "component with forbidden Ident",
			attr: Attr{
				ID: Id("test/component"),
				Value: ComponentValue([]Attr{
					Any(Ident, "test/ident"), // Components cannot have Ident
					Any(Doc, "Test component"),
				}),
			},
			wantErr: true,
			errStr:  "must not have an Ident attribute",
		},
		{
			name: "invalid component type",
			attr: Attr{
				ID:    Id("test/component"),
				Value: StringValue("not a component"),
			},
			wantErr: true,
			errStr:  "must be a component",
		},
	}

	// First create a schema for the component attribute
	_, err := store.CreateEntity(t.Context(), Attrs(
		Ident, "test/component",
		Type, TypeComponent,
	))
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// First create a schema for the component attribute
			_, err := store.CreateEntity(t.Context(), Attrs(
				tt.attr,
			))
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errStr, "expected error message to contain: %s", tt.errStr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAttribute(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	validator := NewValidator(store)

	tests := []struct {
		name    string
		attr    Attr
		wantErr bool
	}{
		{
			name: "valid string",
			attr: String(
				Doc,
				"test documentation",
			),
			wantErr: false,
		},
		{
			name: "invalid string type",
			attr: Int(
				Doc,
				123,
			),
			wantErr: true,
		},
		{
			name: "valid keyword",
			attr: Keyword(
				Ident,
				"test/ident",
			),
			wantErr: false,
		},
		{
			name: "invalid keyword type",
			attr: Int(
				Ident,
				123,
			),
			wantErr: true,
		},
		{
			name: "valid cardinality",
			attr: Ref(
				Cardinality,
				CardinalityOne,
			),
			wantErr: false,
		},
		{
			name: "invalid cardinality value",
			attr: String(
				Cardinality,
				"invalid",
			),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateAttribute(t.Context(), &tt.attr)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	validator := NewValidator(store)

	tests := []struct {
		name    string
		entity  *Entity
		wantErr bool
	}{
		{
			name: "valid entity",
			entity: func() *Entity {
				e := NewEntity(
					Keyword(Ident, "test/entity"),
					Any(Doc, "Test entity"),
				)
				return e
			}(),
			wantErr: false,
		},
		{
			name: "invalid attribute",
			entity: func() *Entity {
				e := NewEntity(
					Any(Ident, 123), // Should be string
					Any(Doc, "Test entity"),
				)
				return e
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateEntity(t.Context(), tt.entity)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateToType(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	validator := NewValidator(store)

	tests := []struct {
		name    string
		value   any
		typ     Id
		wantErr bool
	}{
		{
			name:    "valid string",
			value:   "test",
			typ:     TypeStr,
			wantErr: false,
		},
		{
			name:    "invalid string",
			value:   123,
			typ:     TypeStr,
			wantErr: true,
		},
		{
			name:    "valid int",
			value:   123,
			typ:     TypeInt,
			wantErr: false,
		},
		{
			name:    "invalid int",
			value:   "123",
			typ:     TypeInt,
			wantErr: true,
		},
		{
			name:    "valid float",
			value:   123.45,
			typ:     TypeFloat,
			wantErr: false,
		},
		{
			name:    "invalid float",
			value:   "123.45",
			typ:     TypeFloat,
			wantErr: true,
		},
		{
			name:    "valid bool",
			value:   true,
			typ:     TypeBool,
			wantErr: false,
		},
		{
			name:    "invalid bool",
			value:   "true",
			typ:     TypeBool,
			wantErr: true,
		},
		{
			name:    "valid time string",
			value:   "2023-01-01T00:00:00Z",
			typ:     TypeTime,
			wantErr: false,
		},
		{
			name:    "valid time int64",
			value:   int64(1672531200000),
			typ:     TypeTime,
			wantErr: false,
		},
		{
			name:    "invalid time",
			value:   "invalid",
			typ:     TypeTime,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateToType(t.Context(), tt.value, tt.typ)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidate_EntityAttrs(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	r := require.New(t)

	_, err := store.CreateEntity(t.Context(), Attrs(
		Ident, "test/name",
		Doc, "Entity name",
		Cardinality, CardinalityOne,
		Type, TypeStr,
	))
	r.NoError(err)

	_, err = store.CreateEntity(t.Context(), Attrs(
		Ident, "test/has_name",
		Doc, "Test entity",
		Cardinality, CardinalityOne,
		EntityAttrs, []any{Id("test/name")},
	))
	r.NoError(err)

	validator := NewValidator(store)

	bad := NewEntity(
		Ref(Ensure, "test/has_name"),
	)

	err = validator.ValidateEntity(t.Context(), bad)
	r.Error(err)

	good := NewEntity(
		Ref(Ensure, "test/has_name"),
		String("test/name", "test"),
	)

	err = validator.ValidateEntity(t.Context(), good)
	r.NoError(err)
}
