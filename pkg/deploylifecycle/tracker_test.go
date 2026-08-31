package deploylifecycle

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
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

	ensureTestApp(t, tracker, "web")
	for _, id := range []string{"app_version/v1", "app_version/v2"} {
		_, err := inmem.EAC.Ensure(context.Background(), []entity.Attr{
			entity.Ref(entity.DBId, entity.Id(id)),
			entity.Ref(entity.EntityKind, core_v1alpha.KindAppVersion),
			entity.Ref(core_v1alpha.AppVersionAppId, "app/web"),
		})
		require.NoError(t, err)
	}

	return tracker, clock
}

func ensureTestApp(t *testing.T, tr *Tracker, app string) {
	t.Helper()
	_, err := tr.store.eac.Ensure(context.Background(), []entity.Attr{
		entity.Ref(entity.DBId, entity.Id("app/"+app)),
		entity.Ref(entity.EntityKind, core_v1alpha.KindApp),
	})
	require.NoError(t, err)
}

func begin(t *testing.T, tr *Tracker, app, cluster string) string {
	t.Helper()
	ensureTestApp(t, tr, app)

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
	assert.Empty(t, rec.Deployment.Outcome, "an attempt has no outcome until it finishes")
	assert.Equal(t, string(StatusInProgress), rec.Deployment.Status)
	assert.Equal(t, entity.Id("app/web"), rec.AppID())
	assert.Equal(t, string(PhasePreparing), rec.Deployment.Phase)
	assert.Equal(t, "abc123", rec.Deployment.GitInfo.Sha)
	assert.Equal(t, clock.Now().Format(time.RFC3339), rec.Deployment.DeployedBy.Timestamp)
	assert.Empty(t, rec.AppVersion(), "a forward deploy has no version until the build makes one")

	holder, err := tr.Locks().Get(ctx, "web")
	require.NoError(t, err)
	assert.Equal(t, string(rec.Deployment.ID), holder.DeploymentID)
}

func TestBeginCapturesAuthenticatedIdentity(t *testing.T) {
	tr, _ := newTestTracker(t)
	ctx := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{
		Subject: "user-42", Method: rpc.AuthMethodOIDC,
	})
	rec, err := tr.Begin(ctx, BeginParams{AppName: "web", Operation: OperationBuild})
	require.NoError(t, err)
	assert.Equal(t, "user-42", rec.Deployment.DeployedBy.Subject)
	assert.Equal(t, "oidc", rec.Deployment.DeployedBy.AuthMethod)
}

func TestBeginPreservesCallerIdentityWithoutAuthenticatedOverride(t *testing.T) {
	tr, _ := newTestTracker(t)
	rec, err := tr.Begin(context.Background(), BeginParams{
		AppName: "web", Operation: OperationBuild,
		DeployedBy: core_v1alpha.DeployedBy{Subject: "wired-subject", AuthMethod: "wired-method"},
	})
	require.NoError(t, err)
	assert.Equal(t, "wired-subject", rec.Deployment.DeployedBy.Subject)
	assert.Equal(t, "wired-method", rec.Deployment.DeployedBy.AuthMethod)
}

// A losing contender must not end up with a record it can never finish.
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

	reservations, err := tr.store.eac.List(ctx,
		entity.String(core_v1alpha.DeploymentStatusId, lockReservationStatus))
	require.NoError(t, err)
	assert.Empty(t, reservations.Values(), "the losing deploy must discard its private reservation")
}

func TestBeginRequiresApp(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	_, err := tr.Begin(ctx, BeginParams{ClusterID: "prod"})
	require.Error(t, err)

	_, err = tr.Begin(ctx, BeginParams{AppName: "web"})
	require.NoError(t, err, "cluster_id is legacy display metadata, not identity")
}

func TestBeginConfigChangeDoesNotCreateMissingApp(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	_, err := tr.Begin(ctx, BeginParams{
		AppName: "missing", Operation: OperationConfigChange,
	})
	require.Error(t, err)

	_, _, getErr := tr.store.AppByName(ctx, "missing")
	require.Error(t, getErr)
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
	require.NoError(t, tr.Fail(ctx, id, "build broke"))

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
	assert.Equal(t, StatusSucceeded, secondRec.Status())
	assert.Equal(t, string(StatusSucceeded), secondRec.Deployment.Outcome)
	assert.Equal(t, string(StatusActive), secondRec.Deployment.Status,
		"a downgraded runtime still sees the current successful attempt as active")
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
	assert.Equal(t, StatusSucceeded, firstRec.Status(),
		"serving history does not rewrite an attempt's successful outcome")
	assert.Equal(t, string(StatusRolledBack), firstRec.Deployment.Status)
}

func TestFailRecordsBoundedSummaryAndReleasesLock(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.Fail(ctx, id, "compile error"))

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, rec.Status())
	assert.Equal(t, "compile error", rec.Deployment.ErrorMessage)
	assert.NotEmpty(t, rec.Deployment.CompletedAt)

	blocking, err := tr.Locks().Blocking(ctx, "web")
	require.NoError(t, err)
	assert.Nil(t, blocking, "a failed deploy must not keep the lock")
}

func TestFailTruncatesSummaryAtUTF8Boundary(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.Fail(ctx, id, strings.Repeat("界", maxFailureSummaryBytes)))

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(rec.Deployment.ErrorMessage), maxFailureSummaryBytes)
	assert.True(t, utf8.ValidString(rec.Deployment.ErrorMessage))
	assert.Contains(t, rec.Deployment.ErrorMessage, failureSummaryEllipsis)
}

// A cancelled deploy whose build then dies must still read as cancelled: the
// operator's action is the real account of what happened.
func TestFailAfterCancelKeepsCancelledReason(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.Cancel(ctx, id, "operator cancelled"))
	require.NoError(t, tr.Fail(ctx, id, "build aborted"))

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, rec.Status())
	assert.Equal(t, "operator cancelled", rec.Deployment.ErrorMessage,
		"the cancellation reason is the real account and must survive a later build failure")
}

// Refusing active -> failed is correct: the version is live, so the deploy did
// not fail. Fail() must say so rather than quietly rewriting history.
func TestFailAfterActivateIsRefused(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, id, "app_version/v1"))
	require.NoError(t, tr.Activate(ctx, id))

	err := tr.Fail(ctx, id, "late error")
	require.Error(t, err)
	assert.True(t, isConflict(err), "got %T: %v", err, err)

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, rec.Status(), "the live deployment must stay successful")
}

// The deferred-handler form of the same situation: fire unconditionally, and a
// deployment that already finished is left exactly as it was.
func TestFailIfUnsettled(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	t.Run("records a real failure", func(t *testing.T) {
		id := begin(t, tr, "web", "prod")

		require.NoError(t, tr.FailIfUnsettled(ctx, id, "compile error"))

		rec, err := tr.Store().Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, StatusFailed, rec.Status())
		assert.Equal(t, "compile error", rec.Deployment.ErrorMessage)
	})

	t.Run("leaves an activated deployment alone", func(t *testing.T) {
		id := begin(t, tr, "web", "prod")
		require.NoError(t, tr.SetAppVersion(ctx, id, "app_version/v1"))
		require.NoError(t, tr.Activate(ctx, id))

		require.NoError(t, tr.FailIfUnsettled(ctx, id, "late error"),
			"a defer firing after a successful deploy is not an error")

		rec, err := tr.Store().Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, StatusSucceeded, rec.Status())
		assert.Empty(t, rec.Deployment.ErrorMessage,
			"a successful deploy must not be annotated with a spurious error")
	})

	t.Run("still surfaces errors that are not about being settled", func(t *testing.T) {
		err := tr.FailIfUnsettled(ctx, "deployment/missing", "boom")
		require.Error(t, err, "a missing deployment is a real problem, not a settled one")
	})
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
	assert.Error(t, tr.Fail(ctx, missing, "boom"))
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
	rec.setOutcome(StatusFailed)
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
		wg.Go(func() {
			<-start
			// Either it applies or it conflicts with the cancel; both are fine,
			// a lost update is not.
			_ = tr.SetPhase(ctx, id, phase)
		})
	}

	wg.Go(func() {
		<-start
		_ = tr.Cancel(ctx, id, "raced")
	})

	close(start)
	wg.Wait()

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Contains(t, []Status{StatusInProgress, StatusCancelled}, rec.Status())
}

func TestBeginRecordsParentForRollback(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)

	parent := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, parent, "app_version/v1"))
	require.NoError(t, tr.Activate(ctx, parent))

	rec, err := tr.Begin(ctx, BeginParams{
		AppName:            "web",
		ClusterID:          "prod",
		AppVersion:         "app_version/v1",
		ParentDeploymentID: parent,
	})
	require.NoError(t, err)

	assert.Equal(t, parent, rec.ParentDeploymentID())
	assert.Equal(t, "app_version/v1", rec.AppVersion(),
		"a rollback knows its version up front, unlike a build")
}

func TestReconcileSettlesActivationCrashWindow(t *testing.T) {
	ctx := context.Background()
	tr, _ := newTestTracker(t)
	previous := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, previous, "app_version/v1"))
	require.NoError(t, tr.Activate(ctx, previous))

	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.SetAppVersion(ctx, id, "app_version/v2"))

	rec, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	app, revision, err := tr.Store().AppByName(ctx, "web")
	require.NoError(t, err)
	require.NoError(t, tr.Store().SetActivePointers(ctx, app.ID, revision, "app_version/v2", rec.Deployment.ID))

	require.NoError(t, tr.Reconcile(ctx, id))
	settled, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, settled.Status())
	assert.False(t, settled.CompletedAt().IsZero())
	old, err := tr.Store().Get(ctx, previous)
	require.NoError(t, err)
	assert.Equal(t, string(StatusSucceeded), old.Deployment.Status,
		"reconciliation must retire the previous legacy-active record")
}

func TestReconcileInterruptsExpiredAttemptWithoutLock(t *testing.T) {
	ctx := context.Background()
	tr, clock := newTestTracker(t)
	id := begin(t, tr, "web", "prod")
	require.NoError(t, tr.Locks().Release(ctx, "web", id))
	clock.Advance(DefaultLockTTL + time.Second)

	require.NoError(t, tr.Reconcile(ctx, id))
	settled, err := tr.Store().Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusInterrupted, settled.Status())
}

func TestReconcileInterruptsAttemptAfterClaimIsStolen(t *testing.T) {
	ctx := context.Background()
	tr, clock := newTestTracker(t)
	first := begin(t, tr, "web", "prod")

	clock.Advance(DefaultLockTTL + time.Second)
	second := begin(t, tr, "web", "prod")
	require.NotEqual(t, first, second)

	require.NoError(t, tr.Reconcile(ctx, first))
	settled, err := tr.Store().Get(ctx, first)
	require.NoError(t, err)
	assert.Equal(t, StatusInterrupted, settled.Status())

	holder, err := tr.Locks().Get(ctx, "web")
	require.NoError(t, err)
	assert.Equal(t, second, holder.DeploymentID,
		"reconciling the old attempt must preserve its successor's lock")
}
