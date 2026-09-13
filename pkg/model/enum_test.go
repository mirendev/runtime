package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
)

func TestNamedRefEnumNaturalRoundTripReadsLegacyRef(t *testing.T) {
	field := &entity.SchemaField{
		Name: "mode",
		Type: "enum",
		Id:   "test/mode",
		Enum: "test/enum.mode",
		EnumValues: map[string]entity.Id{
			"auto":  "test/mode.auto",
			"fixed": "test/mode.fixed",
		},
		EnumLegacyValues: map[string][]entity.Id{
			"auto":  {"test/legacy-mode.auto"},
			"fixed": {"test/legacy-mode.fixed"},
		},
	}

	attrs, err := decodeNaturalValue(field, "auto")
	require.NoError(t, err)
	require.Len(t, attrs, 1)
	assert.Equal(t, entity.KindId, attrs[0].Value.Kind())
	assert.Equal(t, entity.Id("test/mode.auto"), attrs[0].Value.Id())

	rendered, truncated, size := (Options{}).renderField(field, entity.RefValue("test/legacy-mode.auto"))
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
