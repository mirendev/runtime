package saga

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	saga_v1alpha "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/schema"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/etcdtest"
)

// newEtcdStorage builds saga storage on a real EtcdStore against a fresh prefix.
func newEtcdStorage(t *testing.T) (*EntityStorage, *entity.EtcdStore) {
	t.Helper()

	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := entity.NewEtcdStore(t.Context(), testutils.TestLogger(t), client, prefix)
	require.NoError(t, err)
	require.NoError(t, schema.Apply(t.Context(), store))

	return NewEntityStorage(store, testutils.TestLogger(t)), store
}

// TestRetention_AgainstRealEtcd is the smoke test for the parts a mock store
// cannot vouch for: that the status index on a real EtcdStore actually returns
// terminal executions, and that deleting them really removes them.
//
// The index assertion is the point. Retention finds its work through
// ListIndex(status), so if a delete left the entity gone but its index entry
// behind, every later sweep would keep rediscovering a phantom and the store
// would accumulate exactly the stale entries MIR-1320 was about. Asserting on
// the index directly after deletion is what proves that did not happen.
func TestRetention_AgainstRealEtcd(t *testing.T) {
	ctx := context.Background()
	storage, store := newEtcdStorage(t)

	saveAged(t, storage, "etcd-old-completed", StatusCompleted, 30*24*time.Hour)
	saveAged(t, storage, "etcd-old-failed", StatusFailed, 8*24*time.Hour)
	saveAged(t, storage, "etcd-fresh", StatusCompleted, 1*time.Hour)
	saveAged(t, storage, "etcd-running", StatusRunning, 90*24*time.Hour)

	terminal, err := storage.ListTerminal(ctx)
	require.NoError(t, err)
	require.Len(t, terminal, 3, "the three terminal executions must be discoverable through the real status index")

	result, err := RunRetention(ctx, storage, weekRetention(), testutils.TestLogger(t))
	require.NoError(t, err)
	assert.Equal(t, 2, result.Deleted)
	assert.Zero(t, result.Failed)

	assert.False(t, executionExists(t, storage, "etcd-old-completed"))
	assert.False(t, executionExists(t, storage, "etcd-old-failed"))
	assert.True(t, executionExists(t, storage, "etcd-fresh"))
	assert.True(t, executionExists(t, storage, "etcd-running"),
		"an ancient running saga must survive: recovery still needs it")

	// The index must not still be advertising the deleted executions. A stale
	// entry here is invisible to Get but would make every future sweep rescan
	// entities that no longer exist.
	completedIds, err := store.ListIndex(ctx, entity.Ref(
		saga_v1alpha.SagaStatusId, saga_v1alpha.SagaStatusCompletedId))
	require.NoError(t, err)
	assert.NotContains(t, completedIds, entity.Id("etcd-old-completed"),
		"deleting an execution must clean up its status index entry")
	assert.Contains(t, completedIds, entity.Id("etcd-fresh"))

	failedIds, err := store.ListIndex(ctx, entity.Ref(
		saga_v1alpha.SagaStatusId, saga_v1alpha.SagaStatusFailedId))
	require.NoError(t, err)
	assert.NotContains(t, failedIds, entity.Id("etcd-old-failed"),
		"deleting an execution must clean up its status index entry")

	// A second sweep is a no-op rather than rediscovering ghosts.
	result, err = RunRetention(ctx, storage, weekRetention(), testutils.TestLogger(t))
	require.NoError(t, err)
	assert.Zero(t, result.Deleted)
	assert.Equal(t, 1, result.Scanned, "only the surviving terminal execution should remain to scan")
}

// TestRetention_LegacyExecutionAgainstRealEtcd checks the upgrade path where it
// actually matters: an execution written without saga timestamps must pick up
// the real store's system timestamp, not read as infinitely old and get
// deleted the moment retention runs.
func TestRetention_LegacyExecutionAgainstRealEtcd(t *testing.T) {
	ctx := context.Background()
	storage, _ := newEtcdStorage(t)

	require.NoError(t, storage.Save(ctx, &Execution{
		ID:              "etcd-legacy",
		DefinitionName:  "create-sandbox",
		Status:          StatusCompleted,
		InitialInputs:   map[string]any{},
		ExecutedActions: map[string]*ActionResult{},
		ExecutionOrder:  []string{},
	}))

	terminal, err := storage.ListTerminal(ctx)
	require.NoError(t, err)
	require.Len(t, terminal, 1)
	assert.False(t, terminal[0].FinishedAt.IsZero(),
		"EtcdStore must have stamped a system timestamp we can fall back to")

	result, err := RunRetention(ctx, storage, weekRetention(), testutils.TestLogger(t))
	require.NoError(t, err)
	assert.Zero(t, result.Deleted,
		"a legacy execution the store stamped just now must not be collected")
	assert.True(t, executionExists(t, storage, "etcd-legacy"))
}
