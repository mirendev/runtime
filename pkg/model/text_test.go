package model

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/entity/types"
)

func testTextFormatter(t *testing.T) (*TextFormatter, *entity.MockStore) {
	r := require.New(t)
	store := entity.NewMockStore()
	sc, err := entity.NewSchemaCache(store)
	r.NoError(err)

	tf, err := NewTextFormatter(sc)
	r.NoError(err)
	return tf, store
}

func TestTextFormatter_Format(t *testing.T) {
	ctx := t.Context()

	t.Run("works with simple entity", func(t *testing.T) {
		r := require.New(t)
		tf, store := testTextFormatter(t)

		ts := time.Unix(1136214245, 0) // 2006-01-02T15:04:05Z

		store.NowFunc = func() time.Time {
			return ts
		}
		testEntity, err := store.CreateEntity(context.Background(), []entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("test/entity")},
			{ID: entity.Doc, Value: entity.StringValue("Test entity")},
		})
		r.NoError(err)

		out, err := tf.Format(ctx, testEntity)
		r.NoError(err)

		expected := `attrs:
  - id: db/doc
    value: Test entity
  - id: db/entity.created-at
    value: 2006-01-02T15:04:05Z
  - id: db/entity.revision
    value: 1
  - id: db/entity.updated-at
    value: 2006-01-02T15:04:05Z
  - id: db/id
    value: test/entity
`
		r.Equal(expected, out)
	})

	t.Run("works for simple entity with schema", func(t *testing.T) {
		r := require.New(t)
		tf, store := testTextFormatter(t)

		ts := time.Unix(1136214245, 0).UTC() // 2006-01-02T15:04:05Z

		store.NowFunc = func() time.Time {
			return ts
		}

		// TODO: we're depending on a real schema/kind here, it would be better if we could create isolated schemas and kinds in the context of tests
		err := schema.Apply(ctx, store)
		r.NoError(err)

		testEntity, err := store.CreateEntity(context.Background(), entity.Attrs(
			entity.Ident, types.Keyword("test/myproject"),
			entity.EntityKind, entity.RefValue("dev.miren.core/kind.project"),
		))
		r.NoError(err)

		out, err := tf.Format(ctx, testEntity)
		r.NoError(err)

		expected := `id: test/myproject
kind: dev.miren.core/project
version: v1alpha
spec: {}
attrs:
  - id: db/entity.created-at
    value: 2006-01-02T15:04:05Z
  - id: db/entity.revision
    value: 1
  - id: db/entity.updated-at
    value: 2006-01-02T15:04:05Z
  - id: db/id
    value: test/myproject
  - id: entity/kind
    value: dev.miren.core/kind.project
`

		r.Equal(expected, out)
	})
}
