package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaFieldEnumValueRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		field SchemaField
		want  Value
	}{
		{
			name: "ref",
			field: SchemaField{
				EnumEncoding: "ref",
				EnumMembers:  []string{"ready"},
				EnumValues:   map[string]Id{"ready": "test/status.ready"},
			},
			want: RefValue("test/status.ready"),
		},
		{
			name: "string",
			field: SchemaField{
				EnumEncoding: "string",
				EnumMembers:  []string{"ready"},
			},
			want: StringValue("ready"),
		},
		{
			name: "keyword",
			field: SchemaField{
				Enum:         "test/enum.status",
				EnumEncoding: "keyword",
				EnumMembers:  []string{"ready"},
			},
			want: KeywordValue("test/enum.status.ready"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, ok := tt.field.EnumValue("ready")
			require.True(t, ok)
			assert.True(t, tt.want.Equal(value))

			member, ok := tt.field.EnumMember(value)
			require.True(t, ok)
			assert.Equal(t, "ready", member)
		})
	}
}
