package commands

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	saga_v1alpha "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/saga"
)

// TestListSagasAgainstEntityServer runs the queries `debug saga list` builds
// against a real entity server holding sagas written by the real storage layer.
// The unit tests above cover decoding and rendering; what they cannot tell us is
// whether the definition_name and status index lookups actually resolve, which
// is the part that decides whether the command returns anything at all.
func TestListSagasAgainstEntityServer(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	ctx := &Context{Context: context.Background(), Stdout: &bytes.Buffer{}}
	storage := saga.NewEntityStorage(inmem.Store, testutils.TestLogger(t))

	executions := []*saga.Execution{
		{ID: "saga/sg-A1", DefinitionName: "provision-shared-postgresql", Status: saga.StatusRunning},
		{ID: "saga/sg-B1", DefinitionName: "provision-shared-postgresql", Status: saga.StatusCompleted},
		{ID: "saga/sg-C1", DefinitionName: "build-and-deploy", Status: saga.StatusFailed},
		{ID: "saga/sg-D1", DefinitionName: "build-and-deploy", Status: saga.StatusRunning},
	}
	for _, exec := range executions {
		require.NoError(t, storage.Save(ctx, exec))
	}

	ids := func(records []*sagaRecord) []string {
		out := make([]string, 0, len(records))
		for _, r := range records {
			out = append(out, r.exec.ID)
		}
		return out
	}

	t.Run("by definition name", func(t *testing.T) {
		records, err := listSagas(ctx, inmem.EAC, []entity.Attr{
			entity.String(saga_v1alpha.SagaDefinitionNameId, "build-and-deploy"),
		})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"saga/sg-C1", "saga/sg-D1"}, ids(records))
	})

	t.Run("timestamps come from the store, not the saga record", func(t *testing.T) {
		attr, err := sagaStatusIndex(saga.StatusRunning)
		require.NoError(t, err)

		records, err := listSagas(ctx, inmem.EAC, []entity.Attr{attr})
		require.NoError(t, err)
		require.NotEmpty(t, records)

		// The saga schema has no timestamp fields, so the UPDATED column is
		// only meaningful if the store stamps the entity itself.
		//
		// createdAt is not asserted, and the omission is deliberate: it passes
		// here and fails against a real server. Saga storage saves by full
		// replace, MockStore's replace carries db/entity.created forward and
		// EtcdStore's does not, so asserting it here would claim coverage this
		// harness cannot give. Verified by hand against a dev server; the
		// MIR-1526 retention work is fixing the underlying loss.
		for _, r := range records {
			assert.False(t, r.updatedAt.IsZero(), "%s has no updated timestamp", r.exec.ID)
		}
	})

	t.Run("by status", func(t *testing.T) {
		attr, err := sagaStatusIndex(saga.StatusFailed)
		require.NoError(t, err)

		records, err := listSagas(ctx, inmem.EAC, []entity.Attr{attr})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"saga/sg-C1"}, ids(records))
	})

	t.Run("active statuses, deduplicated across indexes", func(t *testing.T) {
		var indexes []entity.Attr
		for _, s := range activeSagaStatuses {
			attr, err := sagaStatusIndex(s)
			require.NoError(t, err)
			indexes = append(indexes, attr)
		}

		records, err := listSagas(ctx, inmem.EAC, indexes)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"saga/sg-A1", "saga/sg-D1"}, ids(records))
	})

	t.Run("by parent, for the children of a nested saga", func(t *testing.T) {
		require.NoError(t, storage.Save(ctx, &saga.Execution{
			ID:                "saga/sg-Child1",
			DefinitionName:    "format-disk",
			Status:            saga.StatusCompleted,
			ParentExecutionID: "saga/sg-A1",
		}))

		records, err := listSagas(ctx, inmem.EAC, []entity.Attr{
			entity.Ref(saga_v1alpha.SagaParentExecutionIdId, entity.Id("saga/sg-A1")),
		})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"saga/sg-Child1"}, ids(records))
	})

	// The --all path queries the kind index instead, and that is not covered
	// here: LookupKind resolves against schema entities the in-memory harness
	// does not seed, so it fails for every kind, not just saga. It was verified
	// by hand against a dev server.
}
