package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaFieldEnumValueRoundTrip(t *testing.T) {
	field := SchemaField{
		Enum:       "test/enum.status",
		EnumValues: map[string]Id{"ready": "test/status.ready"},
	}

	value, ok := field.EnumValue("ready")
	require.True(t, ok)
	assert.True(t, RefValue("test/status.ready").Equal(value))

	member, ok := field.EnumMember(value)
	require.True(t, ok)
	assert.Equal(t, "ready", member)
}

func TestSchemaFieldNamedEnumReadsLegacyRef(t *testing.T) {
	field := SchemaField{
		Enum:       "test/enum.status",
		EnumValues: map[string]Id{"ready": "test/enum.status.ready"},
		EnumLegacyValues: map[string][]Id{
			"ready": {"test/status.ready"},
		},
	}

	value, ok := field.EnumValue("ready")
	require.True(t, ok)
	assert.True(t, RefValue("test/enum.status.ready").Equal(value))

	member, ok := field.EnumMember(RefValue("test/status.ready"))
	require.True(t, ok)
	assert.Equal(t, "ready", member)
}
