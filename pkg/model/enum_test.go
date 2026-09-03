package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
)

func TestLegacyNamedStringEnumNaturalRoundTrip(t *testing.T) {
	field := &entity.SchemaField{
		Name:         "mode",
		Type:         "string",
		Id:           "test/mode",
		Enum:         "test/enum.mode",
		EnumEncoding: "string",
		EnumMembers:  []string{"auto", "fixed"},
	}

	attrs, err := decodeNaturalValue(field, "auto")
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	assert.Equal(t, entity.KindString, attrs[0].Value.Kind())
	assert.Equal(t, "auto", attrs[0].Value.Any())

	rendered, truncated, size := (Options{}).renderField(field, attrs[0].Value)
	assert.Equal(t, "auto", rendered)
	assert.False(t, truncated)
	assert.Zero(t, size)

	_, err = decodeNaturalValue(field, "legacy")
	require.ErrorContains(t, err, `failed to decode field mode: enum value "legacy" is not declared, expected one of [auto fixed]`)
}

func TestCanonicalRefEnumNaturalRoundTripReadsLegacyString(t *testing.T) {
	field := &entity.SchemaField{
		Name:         "mode",
		Type:         "enum",
		Id:           "test/mode",
		Enum:         "test/enum.mode",
		EnumEncoding: "ref",
		EnumMembers:  []string{"auto", "fixed"},
		EnumValues: map[string]entity.Id{
			"auto":  "test/mode.auto",
			"fixed": "test/mode.fixed",
		},
		EnumLegacyValues: map[string][]entity.Value{
			"auto":  {entity.StringValue("auto")},
			"fixed": {entity.StringValue("fixed")},
		},
	}

	attrs, err := decodeNaturalValue(field, "auto")
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	assert.Equal(t, entity.KindId, attrs[0].Value.Kind())
	assert.Equal(t, entity.Id("test/mode.auto"), attrs[0].Value.Id())

	rendered, truncated, size := (Options{}).renderField(field, entity.StringValue("auto"))
	assert.Equal(t, "auto", rendered)
	assert.False(t, truncated)
	assert.Zero(t, size)
}

func TestLegacyRefEnumNaturalRoundTrip(t *testing.T) {
	field := &entity.SchemaField{
		Name:       "status",
		Type:       "enum",
		Id:         "test/status",
		EnumValues: map[string]entity.Id{"ready": "test/status.ready"},
	}

	attrs, err := decodeNaturalValue(field, "ready")
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	assert.Equal(t, entity.KindId, attrs[0].Value.Kind())
	assert.Equal(t, entity.Id("test/status.ready"), attrs[0].Value.Id())
}
