package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	etypes "miren.dev/runtime/pkg/entity/types"
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
	_, err := store.CreateEntity(t.Context(), New(
		Ident, "test/component",
		Type, TypeComponent,
	))
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// First create a schema for the component attribute
			_, err := store.CreateEntity(t.Context(), New(
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
				e := New(
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
				e := New(
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
			err := validator.validateToType(t.Context(), tt.value, tt.typ, false)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateRefChoices guards MIR-1425: a ref-typed attribute that declares a
// choice set (schema.Choices, surfaced as EnumValues) must reject any value
// outside that set, even a value that points at a real entity — the case bare
// existence checking can't catch.
func TestValidateRefChoices(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	r := require.New(t)
	ctx := t.Context()

	// A ref attribute that declares a choice set, mirroring what the schema
	// builder now emits for schema.Choices(...).
	_, err := store.CreateEntity(ctx, New(
		Ident, "test/status",
		Doc, "status",
		Cardinality, CardinalityOne,
		Type, TypeRef,
		EnumValues, ArrayValue(Id("test/status.a"), Id("test/status.b")),
	))
	r.NoError(err)

	// A plain ref attribute with no choices, to prove behavior is unchanged.
	_, err = store.CreateEntity(ctx, New(
		Ident, "test/target",
		Doc, "target",
		Cardinality, CardinalityOne,
		Type, TypeRef,
	))
	r.NoError(err)

	// All three referents exist in the store. status.c exists but is NOT a
	// declared choice — the nastier case existence alone can't reject.
	for _, id := range []string{"test/status.a", "test/status.b", "test/status.c"} {
		_, err := store.CreateEntity(ctx, New(Ident, id))
		r.NoError(err)
	}

	validator := NewValidator(store)

	// A declared choice passes.
	a := Ref("test/status", "test/status.a")
	r.NoError(validator.ValidateAttribute(ctx, &a))

	// An existing-but-unlisted value is rejected on membership, not existence.
	c := Ref("test/status", "test/status.c")
	err = validator.ValidateAttribute(ctx, &c)
	r.Error(err)
	r.Contains(err.Error(), "must be one of")

	// Clearing to empty stays allowed.
	empty := Ref("test/status", "")
	r.NoError(validator.ValidateAttribute(ctx, &empty))

	// A choices-less ref still only checks existence: existing ok, missing not.
	ok := Ref("test/target", "test/status.a")
	r.NoError(validator.ValidateAttribute(ctx, &ok))

	missing := Ref("test/target", "test/does_not_exist")
	err = validator.ValidateAttribute(ctx, &missing)
	r.Error(err)
	r.Contains(err.Error(), "non-existent")
}

// TestValidateUpdateRefChoicesExempt guards that an unchanged choice ref is not
// re-validated on an unrelated patch (the MIR-1320 concern): a value that
// predates enforcement must not block a status-only update elsewhere.
func TestValidateUpdateRefChoicesExempt(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	r := require.New(t)
	ctx := t.Context()

	_, err := store.CreateEntity(ctx, New(
		Ident, "test/status",
		Doc, "status",
		Cardinality, CardinalityOne,
		Type, TypeRef,
		EnumValues, ArrayValue(Id("test/status.a"), Id("test/status.b")),
	))
	r.NoError(err)
	_, err = store.CreateEntity(ctx, New(Ident, "test/status.legacy"))
	r.NoError(err)

	validator := NewValidator(store)

	// A pre-existing out-of-set value carried unchanged through an update is
	// exempt and does not block the write.
	legacy := Ref("test/status", "test/status.legacy")
	r.NoError(validator.ValidateUpdate(ctx, []Attr{legacy}, []Attr{legacy}))

	// But changing that attribute to a still-invalid value is rejected.
	r.Error(validator.ValidateUpdate(ctx, []Attr{legacy}, nil))
}

func TestValidateUpdateEnumChoicesExempt(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := t.Context()
	_, err := store.CreateEntity(ctx, New(
		Ident, "test/enum-status",
		Doc, "status",
		Cardinality, CardinalityOne,
		Type, TypeEnum,
		EnumValues, ArrayValue(Id("test/status.a"), Id("test/status.b")),
	))
	require.NoError(t, err)

	validator := NewValidator(store)
	legacy := Ref("test/enum-status", "test/status.legacy")
	require.NoError(t, validator.ValidateUpdate(ctx, []Attr{legacy}, []Attr{legacy}))
	require.Error(t, validator.ValidateUpdate(ctx, []Attr{legacy}, nil))
}

func TestValidateUpdateKeepsNestedExemptionsInTheirComponent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := t.Context()
	for _, schema := range []*Entity{
		New(
			Ident, "test/components",
			Cardinality, CardinalityMany,
			Type, TypeComponent,
		),
		New(
			Ident, "test/component-name",
			Cardinality, CardinalityOne,
			Type, TypeStr,
		),
		New(
			Ident, "test/component-status",
			Cardinality, CardinalityOne,
			Type, TypeEnum,
			EntityElemType, TypeRef,
			EnumValues, ArrayValue(Id("test/status.ready"), Id("test/status.done")),
		),
	} {
		_, err := store.CreateEntity(ctx, schema)
		require.NoError(t, err)
	}

	component := func(name string, status Id) Attr {
		return Component("test/components", []Attr{
			String("test/component-name", name),
			Ref("test/component-status", status),
		})
	}
	original := []Attr{
		component("one", "test/status.legacy-one"),
		component("two", "test/status.legacy-two"),
	}
	validator := NewValidator(store)
	require.NoError(t, validator.ValidateUpdate(ctx, original, original))

	// legacy-two is unchanged in component two, but copying it into component
	// one is still a new invalid value. It must not borrow component two's
	// exemption merely because both nested attributes have the same ID.
	changed := []Attr{
		component("one", "test/status.legacy-two"),
		component("two", "test/status.legacy-two"),
	}
	require.ErrorContains(t, validator.ValidateUpdate(ctx, changed, original), "must be one of")
}

func TestValidateEnumMemberRequiresEntity(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := t.Context()
	_, err := store.CreateEntity(ctx, New(
		Ident, "test/enum-status",
		Doc, "status",
		Cardinality, CardinalityOne,
		Type, TypeEnum,
		EntityElemType, TypeRef,
		EnumValues, ArrayValue(Id("test/status.ready"), "legacy"),
	))
	require.NoError(t, err)

	validator := NewValidator(store)
	canonical := Ref("test/enum-status", "test/status.ready")
	require.ErrorContains(t, validator.ValidateAttribute(ctx, &canonical), "non-existent enum member")

	_, err = store.CreateEntity(ctx, New(Ident, "test/status.ready"))
	require.NoError(t, err)
	require.NoError(t, validator.ValidateAttribute(ctx, &canonical))

	legacy := String("test/enum-status", "legacy")
	require.NoError(t, validator.ValidateAttribute(ctx, &legacy))
}

func TestValidatePhysicalEnumChoices(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := t.Context()
	_, err := store.CreateEntity(ctx, New(
		Ident, "test/string-mode",
		Doc, "mode",
		Cardinality, CardinalityOne,
		Type, TypeStr,
		EnumValues, ArrayValue("auto", "fixed"),
	))
	require.NoError(t, err)
	_, err = store.CreateEntity(ctx, New(
		Ident, "test/keyword-mode",
		Doc, "mode",
		Cardinality, CardinalityOne,
		Type, TypeKeyword,
		EnumValues, ArrayValue(etypes.Keyword("test/mode.auto"), etypes.Keyword("test/mode.fixed")),
	))
	require.NoError(t, err)

	validator := NewValidator(store)
	validString := String("test/string-mode", "auto")
	invalidString := String("test/string-mode", "legacy")
	require.NoError(t, validator.ValidateAttribute(ctx, &validString))
	require.Error(t, validator.ValidateAttribute(ctx, &invalidString))
	require.NoError(t, validator.ValidateUpdate(ctx, []Attr{invalidString}, []Attr{invalidString}))

	validKeyword := Keyword("test/keyword-mode", etypes.Keyword("test/mode.auto"))
	invalidKeyword := Keyword("test/keyword-mode", etypes.Keyword("test/mode.legacy"))
	require.NoError(t, validator.ValidateAttribute(ctx, &validKeyword))
	require.Error(t, validator.ValidateAttribute(ctx, &invalidKeyword))
	require.NoError(t, validator.ValidateUpdate(ctx, []Attr{invalidKeyword}, []Attr{invalidKeyword}))
}

func TestValidate_EntityAttrs(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	r := require.New(t)

	_, err := store.CreateEntity(t.Context(), New(
		Ident, "test/name",
		Doc, "Entity name",
		Cardinality, CardinalityOne,
		Type, TypeStr,
	))
	r.NoError(err)

	_, err = store.CreateEntity(t.Context(), New(
		Ident, "test/has_name",
		Doc, "Test entity",
		Cardinality, CardinalityOne,
		EntityAttrs, []any{Id("test/name")},
	))
	r.NoError(err)

	validator := NewValidator(store)

	bad := New(
		Ref(Ensure, "test/has_name"),
	)

	err = validator.ValidateEntity(t.Context(), bad)
	r.Error(err)

	good := New(
		Ref(Ensure, "test/has_name"),
		String("test/name", "test"),
	)

	err = validator.ValidateEntity(t.Context(), good)
	r.NoError(err)
}
