package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	saga "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
)

func testTextFormatter(t *testing.T) *TextFormatter {
	t.Helper()
	r := require.New(t)

	store := entity.NewMockStore()
	r.NoError(schema.Apply(context.TODO(), store))

	sc, err := entity.NewSchemaCache(store)
	r.NoError(err)

	tf, err := NewTextFormatter(sc)
	r.NoError(err)

	return tf
}

func TestTextFormatter_Parse(t *testing.T) {
	ctx := t.Context()

	t.Run("decodes a document into an entity", func(t *testing.T) {
		r := require.New(t)
		tf := testTextFormatter(t)

		pf, err := tf.Parse(ctx, []byte(`
kind: dev.miren.saga/saga
version: v1alpha
metadata:
  name: deploy-app
spec:
  definition_name: deploy-app
  definition_version: 2
  status: running
`))
		r.NoError(err)
		r.Len(pf.Entities, 1)

		ent := pf.Entities[0]

		name, ok := ent.Get(saga.SagaDefinitionNameId)
		r.True(ok)
		r.Equal("deploy-app", name.Value.String())

		r.True(entity.Is(ent, saga.KindSaga))
	})

	t.Run("round-trips bytes through base64", func(t *testing.T) {
		r := require.New(t)
		tf := testTextFormatter(t)

		// The same encoding the document builder emits, so what the tool prints
		// can be fed back in.
		pf, err := tf.Parse(ctx, []byte(`
kind: dev.miren.saga/saga
version: v1alpha
metadata:
  name: round-trip
spec:
  execution_order: WyJyZXNvbHZlIiwiYnVpbGQiXQ==
`))
		r.NoError(err)
		r.Len(pf.Entities, 1)

		order, ok := pf.Entities[0].Get(saga.SagaExecutionOrderId)
		r.True(ok)
		r.Equal(entity.KindBytes, order.Value.Kind())
		r.Equal(`["resolve","build"]`, string(order.Value.Bytes()))
	})

	t.Run("reads several documents from one stream", func(t *testing.T) {
		r := require.New(t)
		tf := testTextFormatter(t)

		pf, err := tf.Parse(ctx, []byte(`
kind: dev.miren.saga/saga
version: v1alpha
metadata:
  name: one
spec:
  definition_name: one
---
kind: dev.miren.saga/saga
version: v1alpha
metadata:
  name: two
spec:
  definition_name: two
`))
		r.NoError(err)
		r.Len(pf.Entities, 2)

		// Count alone would pass if the parser returned the same document
		// twice, which would not prove the separator split anything.
		var names []string
		for _, ent := range pf.Entities {
			name, ok := ent.Get(saga.SagaDefinitionNameId)
			r.True(ok)
			names = append(names, name.Value.String())
		}
		r.ElementsMatch([]string{"one", "two"}, names)
	})
}
