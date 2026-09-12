package saga

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	saga_v1alpha "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

// TestStalledSweep_AgainstRealEtcd is the smoke test for the part a mock store
// cannot vouch for: that forcing an execution really moves it between status
// indexes on a real EtcdStore.
//
// The index assertions are the point. The sweep finds its work through the
// in-flight indexes, so a write that left the pending entry behind would have
// every later sweep rediscover the same execution forever. That is the leak
// MIR-1735 fixed in ReplaceEntity, and this sweep would notice first if it
// came back.
func TestStalledSweep_AgainstRealEtcd(t *testing.T) {
	ctx := context.Background()
	storage, store := newEtcdStorage(t)

	saveAged(t, storage, "etcd-stranded-pending", StatusPending, 30*24*time.Hour)
	saveAged(t, storage, "etcd-stranded-undoing", StatusUndoing, 30*24*time.Hour)
	saveAged(t, storage, "etcd-working", StatusRunning, 1*time.Hour)
	saveAged(t, storage, "etcd-finished", StatusCompleted, 90*24*time.Hour)

	summaries, err := collectIncompleteSummaries(ctx, storage)
	require.NoError(t, err)
	require.Len(t, summaries, 3, "the three in-flight executions must be discoverable through the real status indexes")

	result, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
	require.NoError(t, err)
	assert.Equal(t, 2, result.Forced)
	assert.Zero(t, result.Failed)

	assert.Equal(t, StatusFailed, statusOf(t, storage, "etcd-stranded-pending"))
	assert.Equal(t, StatusFailed, statusOf(t, storage, "etcd-stranded-undoing"))
	assert.Equal(t, StatusRunning, statusOf(t, storage, "etcd-working"),
		"an execution inside the window is still someone's work in progress")
	assert.Equal(t, StatusCompleted, statusOf(t, storage, "etcd-finished"),
		"the sweep must never rewrite a recorded success")

	pendingIds, err := store.ListIndex(ctx, entity.Ref(
		saga_v1alpha.SagaStatusId, saga_v1alpha.SagaStatusPendingId))
	require.NoError(t, err)
	assert.NotContains(t, pendingIds, entity.Id("etcd-stranded-pending"),
		"forcing an execution must clear its old status index entry")

	undoingIds, err := store.ListIndex(ctx, entity.Ref(
		saga_v1alpha.SagaStatusId, saga_v1alpha.SagaStatusUndoingId))
	require.NoError(t, err)
	assert.NotContains(t, undoingIds, entity.Id("etcd-stranded-undoing"))

	failedIds, err := store.ListIndex(ctx, entity.Ref(
		saga_v1alpha.SagaStatusId, saga_v1alpha.SagaStatusFailedId))
	require.NoError(t, err)
	assert.Contains(t, failedIds, entity.Id("etcd-stranded-pending"),
		"a forced execution must be findable where retention looks")
	assert.Contains(t, failedIds, entity.Id("etcd-stranded-undoing"))

	// A second sweep converges rather than rediscovering what it just forced.
	result, err = RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
	require.NoError(t, err)
	assert.Zero(t, result.Forced)
	assert.Zero(t, result.Recovered, "a converged sweep must not keep re-reading records it already dealt with")
	assert.Equal(t, 1, result.Scanned, "only the execution still legitimately in flight remains to scan")
}

// TestStalledSweep_HandsOffToRetentionAgainstRealEtcd is the reason the sweep
// transitions instead of deleting: forcing moves an execution into the one
// state that already has a rule for getting rid of it.
func TestStalledSweep_HandsOffToRetentionAgainstRealEtcd(t *testing.T) {
	ctx := context.Background()
	storage, _ := newEtcdStorage(t)

	saveAged(t, storage, "etcd-handoff", StatusPending, 90*24*time.Hour)

	_, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
	require.NoError(t, err)

	retained, err := RunRetention(ctx, storage, weekRetention(), testutils.TestLogger(t))
	require.NoError(t, err)
	assert.Zero(t, retained.Deleted,
		"a just-forced execution is inside the retention window: the operator gets a week to see what was forced and why")
	assert.Equal(t, 1, retained.Scanned, "but it is retention's to consider now")

	// Once its window does pass, the ordinary rule collects it. Retention has
	// no special case for a forced record and needs none: after the transition
	// it is a terminal execution like any other, and only its recorded error
	// still says how it got there.
	expired, err := RunRetention(ctx, storage,
		RetentionConfig{Retention: time.Nanosecond, MaxDeletes: 1000},
		testutils.TestLogger(t))
	require.NoError(t, err)
	assert.Equal(t, 1, expired.Deleted)
	assert.False(t, executionExists(t, storage, "etcd-handoff"))
}

// TestStalledSweep_UnblocksConvergentRetryAgainstRealEtcd is the other half of
// the handoff. An execution named after its entity is stuck differently: its
// owner keeps arriving under that name, and a pending record answers every
// arrival with "still in flight". Forcing it is what lets DropIfFailed clear
// the name so the next reconcile actually retries.
func TestStalledSweep_UnblocksConvergentRetryAgainstRealEtcd(t *testing.T) {
	ctx := context.Background()
	storage, _ := newEtcdStorage(t)

	const id = "deprovision-assoc-123"
	saveAged(t, storage, id, StatusPending, 90*24*time.Hour)

	// Before the sweep the name is occupied by a record nothing will resume,
	// and its owner's own escape hatch cannot help: DropIfFailed only clears a
	// failed record, and this one never got there.
	require.NoError(t, DropIfFailed(ctx, storage, id))
	assert.True(t, executionExists(t, storage, id),
		"DropIfFailed must not touch a pending record, which is exactly why it was stuck")

	_, err := RunStalledSweep(ctx, storage, weekStalled(), testutils.TestLogger(t))
	require.NoError(t, err)

	require.NoError(t, DropIfFailed(ctx, storage, id))
	assert.False(t, executionExists(t, storage, id),
		"forcing to failed is what lets the owning controller clear the name and retry")
}
