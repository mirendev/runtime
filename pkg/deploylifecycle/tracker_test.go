package deploylifecycle

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
)

func newTestTracker(t *testing.T) (*Tracker, *fakeClock) {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	clock := &fakeClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}

	tracker := NewTracker(log, inmem.EAC)
	tracker.now = clock.Now
	tracker.store.now = clock.Now
	tracker.locks.now = clock.Now

	return tracker, clock
}

func begin(t *testing.T, tr *Tracker, app, cluster string) string {
	t.Helper()

	rec, err := tr.Begin(context.Background(), BeginParams{AppName: app, ClusterID: cluster})
	require.NoError(t, err)
	return string(rec.Deployment.ID)
}

func TestBeginCreatesRecordAndTakesLock(t *testing.T) {
	ctx := context.Background()
	tr, clock := newTestTracker(t)

	rec, err := tr.Begin(ctx, BeginParams{
		AppName:   "web",
		ClusterID: "prod",
		GitInfo:   core_v1alpha.GitInfo{Sha: "abc123", Branch: "main"},
	})
	require.NoError(t, err)

	assert.Equal(t, StatusInProgress, rec.Status())
	assert.Equal(t, string(PhasePreparing), rec.Deployment.Phase)
	assert.Equal(t, "abc123", rec.Deployment.GitInfo.Sha)
	assert.Equal(t, clock.Now().Format(time.RFC3339), rec.Deployment.DeployedBy.Timestamp)
	assert.Empty(t, rec.AppVersion(), "a forward deploy has no version until the build makes one")

	holder, err := tr.Locks().Get(ctx, "web")
	require.NoError(t, err)
	assert.Equal(t, string(rec.Deployment.ID), holder.DeploymentID)
}

// The whole point of taking the lock inside Begin: the second caller must not
// end up with a record it will never be able to finish.
func TestBeginLeavesNoRecordWhenLockIsHeld(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	first := begin(t, tr, "web", "prod")

	_, err := tr.Begin(ctx, BeginParams{AppName: "web", ClusterID: "prod"})
	require.ErrorIs(t, err, ErrLockHeld)

	holder, ok := HolderFrom(err)
	require.True(t, ok, "the blocked caller must be able to name the blocker")
	assert.Equal(t, first, holder.DeploymentID)

	all, err := tr.Store().List(ctx, Query{AppName: "web"})
	require.NoError(t, err)
	assert.Len(t, all, 1, "the losing deploy must not leave a record behind")
}

func TestBeginRequiresAppAndCluster(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	_, err := tr.Begin(ctx, BeginParams{ClusterID: "prod"})
	require.Error(t, err)

	_, err = tr.Begin(ctx, BeginParams{AppName: "web"})
	require.Error(t, err)
}

func TestSetPhase(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")

	for _, phase := range []Phase{PhaseBuilding, PhasePushing, PhaseActivating} {
		require.NoError(t, tr.SetPhase(ctx, id, phase))

		rec, err := tr.Store().Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, string(phase), rec.Deployment.Phase)
	}
}

func TestSetPhaseOnSettledDeploymentIsConflict(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.Fail(ctx, id, "build broke", ""))

	err := tr.SetPhase(ctx, id, PhaseBuilding)
	require.Error(t, err)
	assert.True(t, isConflict(err), "got %T: %v", err, err)
}

func TestSetAppVersion(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, id, "app_version/v1"))

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "app_version/v1", rec.AppVersion())

	require.Error(t, tr.SetAppVersion(ctx, id, ""), "an empty version is not a version")
}

// The sentinel is gone, so the invariant it used to stand in for has to be
// enforced directly.
func TestActivateRequiresAnAppVersion(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")

	err := tr.Activate(ctx, id)
	require.Error(t, err)
	assert.True(t, isValidationFailure(err), "got %T: %v", err, err)

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusInProgress, rec.Status(), "a refused activation must not settle the record")
}

func TestActivateSettlesPreviousAndReleasesLock(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	first := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, first, "app_version/v1"))
	require.NoError(t, tr.Activate(ctx, first))

	// The lock must be free, or no further deploy could ever start.
	second := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, second, "app_version/v2"))
	require.NoError(t, tr.Activate(ctx, second))

	firstRec, err := tr.Store().Get(ctx, first)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, firstRec.Status())

	secondRec, err := tr.Store().Get(ctx, second)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, secondRec.Status())
	assert.NotEmpty(t, secondRec.Deployment.CompletedAt)
}

func TestActivateRollbackSettlesPreviousAsRolledBack(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	first := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, first, "app_version/v2"))
	require.NoError(t, tr.Activate(ctx, first))

	rollback := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, rollback, "app_version/v1"))
	require.NoError(t, tr.ActivateRollback(ctx, rollback))

	firstRec, err := tr.Store().Get(ctx, first)
	require.NoError(t, err)
	assert.Equal(t, StatusRolledBack, firstRec.Status(),
		"the version we rolled away from was not a success")
}

func TestFailRecordsErrorAndReleasesLock(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.Fail(ctx, id, "compile error", "line 1\nline 2"))

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, rec.Status())
	assert.Equal(t, "compile error", rec.Deployment.ErrorMessage)
	assert.Equal(t, "line 1\nline 2", rec.Deployment.BuildLogs)
	assert.NotEmpty(t, rec.Deployment.CompletedAt)

	blocking, err := tr.Locks().Blocking(ctx, "web")
	require.NoError(t, err)
	assert.Nil(t, blocking, "a failed deploy must not keep the lock")
}

// A cancelled deploy whose build then dies must still read as cancelled: the
// operator's action is the real account of what happened.
func TestFailAfterCancelKeepsCancelledButRecordsTheError(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.Cancel(ctx, id, "operator cancelled"))
	require.NoError(t, tr.Fail(ctx, id, "build aborted", "some logs"))

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, rec.Status())
	assert.Equal(t, "operator cancelled", rec.Deployment.ErrorMessage,
		"the cancellation reason is the real account and must survive a later build failure")
	assert.Equal(t, "some logs", rec.Deployment.BuildLogs,
		"the build logs are still worth keeping for diagnosis")
}

// Refusing active -> failed is correct: the version is live, so the deploy did
// not fail. Fail() must say so rather than quietly rewriting history.
func TestFailAfterActivateIsRefused(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, id, "app_version/v1"))
	require.NoError(t, tr.Activate(ctx, id))

	err := tr.Fail(ctx, id, "late error", "")
	require.Error(t, err)
	assert.True(t, isConflict(err), "got %T: %v", err, err)

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, rec.Status(), "the live deployment must stay active")
}

// The deferred-handler form of the same situation: fire unconditionally, and a
// deployment that already finished is left exactly as it was.
func TestFailIfUnsettled(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	t.Run("records a real failure", func(t *testing.T) {
		id := begin(t, tr, "web", "prod")

		require.NoError(t, tr.FailIfUnsettled(ctx, id, "compile error", "logs"))

		rec, err := tr.Store().Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, StatusFailed, rec.Status())
		assert.Equal(t, "compile error", rec.Deployment.ErrorMessage)
	})

	t.Run("leaves an activated deployment alone", func(t *testing.T) {
		id := begin(t, tr, "web", "prod")
		require.NoError(t, tr.SetAppVersion(ctx, id, "app_version/v1"))
		require.NoError(t, tr.Activate(ctx, id))

		require.NoError(t, tr.FailIfUnsettled(ctx, id, "late error", "logs"),
			"a defer firing after a successful deploy is not an error")

		rec, err := tr.Store().Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, StatusActive, rec.Status())
		assert.Empty(t, rec.Deployment.ErrorMessage,
			"a successful deploy must not be annotated with a spurious error")
	})

	t.Run("still surfaces errors that are not about being settled", func(t *testing.T) {
		err := tr.FailIfUnsettled(ctx, "deployment/missing", "boom", "")
		require.Error(t, err, "a missing deployment is a real problem, not a settled one")
	})
}

// ReleaseLock is the backstop for a deploy whose activation settle failed after
// the version already went live: it frees the lock without settling the record,
// so the next deploy is not blocked for the full lock TTL.
func TestReleaseLockFreesAStuckDeployment(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	stuck := begin(t, tr, "web", "prod") // in_progress, lock held

	// While that record holds the lock and has not settled, the next deploy is
	// blocked — this is the stall the backstop exists to prevent.
	_, err := tr.Begin(ctx, BeginParams{AppName: "web", ClusterID: "prod"})
	require.ErrorIs(t, err, ErrLockHeld)

	require.NoError(t, tr.ReleaseLock(ctx, stuck))

	// The record is untouched (still in_progress) but the lock is free.
	rec, err := tr.Store().Get(ctx, stuck)
	require.NoError(t, err)
	assert.Equal(t, StatusInProgress, rec.Status(), "ReleaseLock must not change the record")

	next, err := tr.Begin(ctx, BeginParams{AppName: "web", ClusterID: "prod"})
	require.NoError(t, err, "the next deploy must proceed once the lock is released")
	assert.NotEqual(t, stuck, string(next.Deployment.ID))
}

// ReleaseLock must not free a lock a newer deployment already holds.
func TestReleaseLockIsHolderGuarded(t *testing.T) {
	ctx := context.Background()
	tr, clock := newTestTracker(t)

	first := begin(t, tr, "web", "prod")

	// A second deploy steals the lock once the first's expires.
	clock.Advance(DefaultLockTTL + time.Second)
	second := begin(t, tr, "web", "prod")

	// The first deployment releasing its (now superseded) lock must be a no-op.
	require.NoError(t, tr.ReleaseLock(ctx, first))

	holder, err := tr.Locks().Get(ctx, "web")
	require.NoError(t, err)
	assert.Equal(t, second, holder.DeploymentID, "the second deploy must still hold the lock")
}

func TestReleaseLockUnknownDeployment(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	err := tr.ReleaseLock(ctx, "deployment/missing")
	require.Error(t, err, "cannot release a lock for a record that does not exist")
}

func TestCancelReleasesLock(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.Cancel(ctx, id, "operator cancelled"))

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, rec.Status())
	assert.Equal(t, "operator cancelled", rec.Deployment.ErrorMessage)

	// The lock is free, so the next deploy proceeds.
	next := begin(t, tr, "web", "prod")
	assert.NotEqual(t, id, next)
}

func TestCancelSettledDeploymentIsConflict(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, id, "app_version/v1"))
	require.NoError(t, tr.Activate(ctx, id))

	err := tr.Cancel(ctx, id, "too late")
	require.Error(t, err)
	assert.True(t, isConflict(err), "got %T: %v", err, err)
}

func TestOperationsOnUnknownDeployment(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	const missing = "deployment/missing"

	assert.Error(t, tr.SetPhase(ctx, missing, PhaseBuilding))
	assert.Error(t, tr.SetAppVersion(ctx, missing, "app_version/v1"))
	assert.Error(t, tr.Activate(ctx, missing))
	assert.Error(t, tr.Fail(ctx, missing, "boom", ""))
	assert.Error(t, tr.Cancel(ctx, missing, ""))

	assert.Error(t, tr.SetPhase(ctx, "", PhaseBuilding), "an empty id is not a lookup")
}

// A deploy whose driver died leaves the lock behind. The next deploy should get
// through on the strength of the record being terminal, without waiting out the
// full TTL.
func TestAbandonedLockIsReclaimedOnceRecordIsTerminal(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")

	// Settle the record directly, as an out-of-band reaper would, leaving the
	// lock in place.
	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	rec.Deployment.Status = string(StatusFailed)
	require.NoError(t, tr.Store().Put(ctx, rec))

	next, err := tr.Begin(ctx, BeginParams{AppName: "web", ClusterID: "prod"})
	require.NoError(t, err, "a lock held for a finished deployment must not block forever")
	assert.NotEqual(t, id, string(next.Deployment.ID))
}

// Phase updates and a cancel can land at the same moment; the retry loop must
// make one of them lose cleanly rather than clobbering the other.
func TestConcurrentUpdatesConverge(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")

	var wg sync.WaitGroup
	start := make(chan struct{})

	for _, phase := range []Phase{PhaseBuilding, PhasePushing, PhaseActivating} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// Either it applies or it conflicts with the cancel; both are fine,
			// a lost update is not.
			_ = tr.SetPhase(ctx, id, phase)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_ = tr.Cancel(ctx, id, "raced")
	}()

	close(start)
	wg.Wait()

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Contains(t, []Status{StatusInProgress, StatusCancelled}, rec.Status())
}

func TestBeginRecordsProvenanceForRollback(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	source := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, source, "app_version/v1"))
	require.NoError(t, tr.Activate(ctx, source))

	rec, err := tr.Begin(ctx, BeginParams{
		AppName:            "web",
		ClusterID:          "prod",
		AppVersion:         "app_version/v1",
		SourceDeploymentID: source,
	})
	require.NoError(t, err)

	assert.Equal(t, source, rec.Deployment.SourceDeploymentId)
	assert.Equal(t, "app_version/v1", rec.AppVersion(),
		"a rollback knows its version up front, unlike a build")
}
