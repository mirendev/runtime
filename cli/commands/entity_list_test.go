package commands

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	v1alpha "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	saga "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/entityserver"
)

// The server caps how much it renders per response, so a caller asking for
// everything only gets everything if the client walks the cursor. That walk is
// what audit-exposure.py depends on, and a silent short read there is the exact
// false negative the tool exists to catch, so it is worth pinning.
func TestFetchDocumentsWalksPages(t *testing.T) {
	const testKind = "dev.miren.core/kind.project"

	// Shrink the server's ceiling so a handful of entities spans several pages.
	restore := entityserver.MaxPageLimit
	entityserver.MaxPageLimit = 3
	t.Cleanup(func() { entityserver.MaxPageLimit = restore })

	setup := func(t *testing.T, count int) *v1alpha.EntityAccessClient {
		t.Helper()
		r := require.New(t)

		store := entity.NewMockStore()
		r.NoError(schema.Apply(context.TODO(), store))

		server, err := entityserver.NewEntityServer(slog.Default(), store)
		r.NoError(err)

		for i := range count {
			_, err := store.CreateEntity(context.TODO(), entity.New([]entity.Attr{
				{ID: entity.Ident, Value: entity.KeywordValue(fmt.Sprintf("test/e%02d", i))},
				{ID: entity.EntityKind, Value: entity.RefValue(testKind)},
			}))
			r.NoError(err)
		}

		return &v1alpha.EntityAccessClient{
			Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
		}
	}

	index := entity.Attr{ID: entity.EntityKind, Value: entity.RefValue(testKind)}

	t.Run("unlimited collects every entity across pages", func(t *testing.T) {
		r := require.New(t)
		eac := setup(t, 10)

		docs, cursor, total, err := fetchDocuments(context.TODO(), eac, index, "", 0, 0)
		r.NoError(err)

		r.Len(docs, 10, "a capped response must not cut a request for everything short")
		r.Equal("", cursor, "the walk ran to the end, so there is nothing to resume")
		r.Equal(int64(10), total)

		// Every entity exactly once.
		seen := map[string]bool{}
		for _, d := range docs {
			r.False(seen[d.Id], "walk repeated %s", d.Id)
			seen[d.Id] = true
		}
	})

	t.Run("a limit above the server cap still spans pages", func(t *testing.T) {
		r := require.New(t)
		eac := setup(t, 10)

		docs, _, _, err := fetchDocuments(context.TODO(), eac, index, "", 7, 0)
		r.NoError(err)
		r.Len(docs, 7, "the walk must keep going until the caller's limit is met")
	})

	t.Run("a limit under the cap stops early and offers a cursor", func(t *testing.T) {
		r := require.New(t)
		eac := setup(t, 10)

		docs, cursor, _, err := fetchDocuments(context.TODO(), eac, index, "", 2, 0)
		r.NoError(err)

		r.Len(docs, 2)
		r.NotEmpty(cursor, "there are more entities behind this page")
	})

	t.Run("an exhausted index offers no cursor", func(t *testing.T) {
		r := require.New(t)
		eac := setup(t, 3)

		docs, cursor, _, err := fetchDocuments(context.TODO(), eac, index, "", 0, 0)
		r.NoError(err)

		r.Len(docs, 3)
		r.Equal("", cursor)
	})

	t.Run("preserves large integers exactly", func(t *testing.T) {
		r := require.New(t)

		store := entity.NewMockStore()
		r.NoError(schema.Apply(context.TODO(), store))

		server, err := entityserver.NewEntityServer(slog.Default(), store)
		r.NoError(err)

		// Past 2^53, so a decode through float64 would round it.
		const big = int64(9007199254740993)

		_, err = store.CreateEntity(context.TODO(), entity.New([]entity.Attr{
			{ID: entity.Ident, Value: entity.KeywordValue("saga/big")},
			{ID: entity.EntityKind, Value: entity.RefValue(saga.KindSaga)},
			{ID: saga.SagaDefinitionVersionId, Value: entity.Int64Value(big)},
		}))
		r.NoError(err)

		eac := &v1alpha.EntityAccessClient{
			Client: rpc.LocalClient(v1alpha.AdaptEntityAccess(server)),
		}

		sagaIndex := entity.Attr{ID: entity.EntityKind, Value: entity.RefValue(saga.KindSaga)}

		docs, _, _, err := fetchDocuments(context.TODO(), eac, sagaIndex, "", 0, 0)
		r.NoError(err)
		r.Len(docs, 1)

		var found bool
		for _, f := range docs[0].Facets {
			for _, fl := range f.Fields {
				if fl.Name == "definition_version" {
					found = true
					r.Equal(fmt.Sprintf("%d", big), fmt.Sprintf("%v", fl.Value),
						"re-encoding must not round a large integer")
				}
			}
		}
		r.True(found, "definition_version should render under the saga facet")
	})
}
