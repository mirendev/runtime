package saga

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStageCursor covers the cursor the durable backends use to walk several
// status indexes as one sequence.
//
// The inner half belongs to the store that issued it and is opaque here, which
// is why the stage is a prefix split on the first separator rather than
// anything parsed: a store cursor is free to contain separators of its own, and
// a scheme that could not survive that would corrupt a walk only on the stores
// where it mattered.
func TestStageCursor(t *testing.T) {
	t.Run("round-trips a store cursor containing separators", func(t *testing.T) {
		inner := "abc:def:ghi"

		stage, got, err := decodeStageCursor(encodeStageCursor(2, inner), 3)
		require.NoError(t, err)
		assert.Equal(t, 2, stage)
		assert.Equal(t, inner, got, "the store's own separators must survive the round trip")
	})

	t.Run("an empty cursor starts at the first stage", func(t *testing.T) {
		stage, inner, err := decodeStageCursor("", 3)
		require.NoError(t, err)
		assert.Equal(t, 0, stage)
		assert.Equal(t, "", inner)
	})

	t.Run("rejects a cursor with no stage", func(t *testing.T) {
		_, _, err := decodeStageCursor("no-separator-here", 3)
		assert.Error(t, err, "a malformed cursor must not silently restart the walk")
	})

	t.Run("rejects a non-numeric stage", func(t *testing.T) {
		_, _, err := decodeStageCursor("x:inner", 3)
		assert.Error(t, err)
	})

	t.Run("rejects a stage that does not exist", func(t *testing.T) {
		// A cursor issued against a longer stage list, or simply invented.
		// Reading it as stage 0 would silently rewalk the whole set.
		_, _, err := decodeStageCursor("7:inner", 3)
		assert.Error(t, err)

		_, _, err = decodeStageCursor("-1:inner", 3)
		assert.Error(t, err)
	})

	t.Run("advances to the next stage when one is exhausted", func(t *testing.T) {
		next := nextStageCursor(0, "", 3)

		stage, inner, err := decodeStageCursor(next, 3)
		require.NoError(t, err)
		assert.Equal(t, 1, stage, "an exhausted index must hand the walk to the next one")
		assert.Equal(t, "", inner, "and start it at the head")
	})

	t.Run("stays in the current stage while it has more", func(t *testing.T) {
		stage, inner, err := decodeStageCursor(nextStageCursor(1, "more", 3), 3)
		require.NoError(t, err)
		assert.Equal(t, 1, stage)
		assert.Equal(t, "more", inner)
	})

	t.Run("ends the walk after the last stage", func(t *testing.T) {
		assert.Equal(t, "", nextStageCursor(2, "", 3),
			"an empty cursor is the only thing that ends a walk, so the last stage must produce one")
	})
}

func TestClampLimit(t *testing.T) {
	// A limit is a request, not an instruction. Honouring an enormous one would
	// reintroduce through the front door the unbounded read this exists to
	// remove, and honouring zero would make "I didn't think about it" mean
	// "give me everything".
	assert.Equal(t, defaultPageLimit, clampLimit(0))
	assert.Equal(t, defaultPageLimit, clampLimit(-1))
	assert.Equal(t, maxPageLimit, clampLimit(maxPageLimit+1))
	assert.Equal(t, 50, clampLimit(50))
}
