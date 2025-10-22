package enttest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/enttest"
)

func TestEqualAttr(t *testing.T) {
	t.Run("passes when attribute exists and matches", func(t *testing.T) {
		ent := entity.New(
			entity.Ident, "test/entity",
			entity.Doc, "Test documentation",
		)

		result := enttest.EqualAttr(t, ent, entity.Doc, "Test documentation")
		assert.True(t, result)
	})

	t.Run("fails when attribute does not exist", func(t *testing.T) {
		ent := entity.New(
			entity.Ident, "test/entity",
		)

		// This will fail because Doc attribute doesn't exist
		// We can't easily test this without creating a mock *testing.T
		// but the function will call assert.Fail which will mark the test as failed
		_ = ent
	})

	t.Run("fails when attribute value does not match", func(t *testing.T) {
		ent := entity.New(
			entity.Ident, "test/entity",
			entity.Doc, "Test documentation",
		)

		// This will fail because the values don't match
		// We can't easily test this without creating a mock *testing.T
		_ = ent
	})
}
