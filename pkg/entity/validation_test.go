package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
			attr: Attr{
				ID:    EntityDoc,
				Value: "test documentation",
			},
			wantErr: false,
		},
		{
			name: "invalid string type",
			attr: Attr{
				ID:    EntityDoc,
				Value: 123,
			},
			wantErr: true,
		},
		{
			name: "valid keyword",
			attr: Attr{
				ID:    EntityIdent,
				Value: "test/ident",
			},
			wantErr: false,
		},
		{
			name: "invalid keyword type",
			attr: Attr{
				ID:    EntityIdent,
				Value: 123,
			},
			wantErr: true,
		},
		{
			name: "valid cardinality",
			attr: Attr{
				ID:    EntityCard,
				Value: EntityCardOne,
			},
			wantErr: false,
		},
		{
			name: "invalid cardinality value",
			attr: Attr{
				ID:    EntityCard,
				Value: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateAttribute(tt.attr)
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
			entity: &Entity{
				ID: "test",
				Attrs: []Attr{
					{ID: EntityIdent, Value: "test/entity"},
					{ID: EntityDoc, Value: "Test entity"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid attribute",
			entity: &Entity{
				ID: "test",
				Attrs: []Attr{
					{ID: EntityIdent, Value: 123}, // Should be string
					{ID: EntityDoc, Value: "Test entity"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateEntity(tt.entity)
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
		typ     EntityId
		wantErr bool
	}{
		{
			name:    "valid string",
			value:   "test",
			typ:     EntityTypeStr,
			wantErr: false,
		},
		{
			name:    "invalid string",
			value:   123,
			typ:     EntityTypeStr,
			wantErr: true,
		},
		{
			name:    "valid int",
			value:   123,
			typ:     EntityTypeInt,
			wantErr: false,
		},
		{
			name:    "invalid int",
			value:   "123",
			typ:     EntityTypeInt,
			wantErr: true,
		},
		{
			name:    "valid float",
			value:   123.45,
			typ:     EntityTypeFloat,
			wantErr: false,
		},
		{
			name:    "invalid float",
			value:   "123.45",
			typ:     EntityTypeFloat,
			wantErr: true,
		},
		{
			name:    "valid bool",
			value:   true,
			typ:     EntityTypeBool,
			wantErr: false,
		},
		{
			name:    "invalid bool",
			value:   "true",
			typ:     EntityTypeBool,
			wantErr: true,
		},
		{
			name:    "valid time string",
			value:   "2023-01-01T00:00:00Z",
			typ:     EntityTypeTime,
			wantErr: false,
		},
		{
			name:    "valid time int64",
			value:   int64(1672531200000),
			typ:     EntityTypeTime,
			wantErr: false,
		},
		{
			name:    "invalid time",
			value:   "invalid",
			typ:     EntityTypeTime,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateToType(tt.value, tt.typ)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
