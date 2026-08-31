package build

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc/standard"
)

func newDeployTestBuilder(t *testing.T) *Builder {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	return &Builder{
		Log:    log,
		EAS:    inmem.EAC,
		ec:     entityserver.NewClient(log, inmem.EAC),
		deploy: deploylifecycle.NewTracker(log, inmem.EAC),
	}
}

func deployRequest(cluster string) *build_v1alpha.DeployRequest {
	req := &build_v1alpha.DeployRequest{}
	req.SetClusterId(cluster)
	return req
}

func TestBeginDeployWithoutRequestStillTracksAttempt(t *testing.T) {
	ctx := context.Background()
	b := newDeployTestBuilder(t)

	dt, err := b.beginDeploy(ctx, "web", nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, dt)

	all, err := b.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "web"})
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestBeginDeployAllowsEmptyLegacyClusterID(t *testing.T) {
	ctx := context.Background()
	b := newDeployTestBuilder(t)

	dt, err := b.beginDeploy(ctx, "web", deployRequest(""), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, dt)

	all, err := b.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "web"})
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

// Ephemeral builds have no deployment record by design, even when a request is
// present.
func TestBeginDeploySkipsEphemeral(t *testing.T) {
	ctx := context.Background()
	b := newDeployTestBuilder(t)

	dt, err := b.beginDeploy(ctx, "web", deployRequest("prod"),
		&ephemeralOpts{label: "pr-42", ttl: "24h"}, nil)
	require.NoError(t, err)
	assert.Nil(t, dt)

	all, err := b.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "web"})
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestBeginDeployCreatesExactlyOneRecord(t *testing.T) {
	ctx := context.Background()
	b := newDeployTestBuilder(t)

	dt, err := b.beginDeploy(ctx, "web", deployRequest("prod"), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, dt)

	all, err := b.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "web"})
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, dt.deploymentID, string(all[0].Deployment.ID))
	assert.Equal(t, deploylifecycle.StatusInProgress, all[0].Status())
	assert.Equal(t, "prod", all[0].Deployment.ClusterId)
}

// A build blocked by another in-flight deploy must surface the lock error, and
// must not leave a second record behind.
func TestBeginDeployPropagatesLockHeld(t *testing.T) {
	ctx := context.Background()
	b := newDeployTestBuilder(t)

	_, err := b.beginDeploy(ctx, "web", deployRequest("prod"), nil, nil)
	require.NoError(t, err)

	dt, err := b.beginDeploy(ctx, "web", deployRequest("prod"), nil, nil)
	require.ErrorIs(t, err, deploylifecycle.ErrLockHeld)
	assert.Nil(t, dt)

	all, err := b.deploy.Store().List(ctx, deploylifecycle.Query{AppName: "web"})
	require.NoError(t, err)
	assert.Len(t, all, 1, "the blocked build must not create a second record")
}

// Every method on a nil *deployTracking must be a safe no-op, because the
// untracked build path calls straight through without checking.
func TestDeployTrackingNilIsNoOp(t *testing.T) {
	ctx := context.Background()
	var dt *deployTracking

	assert.NotPanics(t, func() {
		dt.setPhase(ctx, deploylifecycle.PhaseBuilding)
		dt.setAppVersion(ctx, "app_version/v1")
		dt.activate(ctx)
		dt.failOnError(ctx, assert.AnError)
		dt.failOnError(ctx, nil)
		dt.emit("building")
	})
}

func TestFailOnError(t *testing.T) {
	ctx := context.Background()
	b := newDeployTestBuilder(t)

	t.Run("records a failure", func(t *testing.T) {
		dt, err := b.beginDeploy(ctx, "web", deployRequest("prod"), nil, nil)
		require.NoError(t, err)

		dt.failOnError(ctx, assert.AnError)

		rec, err := b.deploy.Store().Get(ctx, dt.deploymentID)
		require.NoError(t, err)
		assert.Equal(t, deploylifecycle.StatusFailed, rec.Status())
		assert.Equal(t, assert.AnError.Error(), rec.Deployment.ErrorMessage)
	})

	t.Run("does nothing on success", func(t *testing.T) {
		dt, err := b.beginDeploy(ctx, "api", deployRequest("prod"), nil, nil)
		require.NoError(t, err)

		dt.failOnError(ctx, nil)

		rec, err := b.deploy.Store().Get(ctx, dt.deploymentID)
		require.NoError(t, err)
		assert.Equal(t, deploylifecycle.StatusInProgress, rec.Status(),
			"a build that returned no error must stay in_progress")
	})
}

// The finalizer must still settle the record when the build's own context was
// cancelled — the client disconnecting is exactly when the lock most needs
// releasing.
func TestFailOnErrorSettlesEvenWhenContextCancelled(t *testing.T) {
	b := newDeployTestBuilder(t)

	dt, err := b.beginDeploy(context.Background(), "web", deployRequest("prod"), nil, nil)
	require.NoError(t, err)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	dt.failOnError(cancelled, assert.AnError)

	rec, err := b.deploy.Store().Get(context.Background(), dt.deploymentID)
	require.NoError(t, err)
	assert.Equal(t, deploylifecycle.StatusFailed, rec.Status())
}

func TestDeploymentContextCancelsWhenRecordIsCancelled(t *testing.T) {
	b := newDeployTestBuilder(t)
	work := newDeploymentContext(context.Background())
	defer work.Close()

	dt, err := b.beginDeploy(work.action, "web", deployRequest("prod"), nil, nil)
	require.NoError(t, err)
	require.NoError(t, b.deploy.Cancel(context.Background(), dt.deploymentID, "operator cancelled"))

	require.Eventually(t, func() bool {
		return errors.Is(context.Cause(work.action), errDeploymentCancelled)
	}, 5*time.Second, 10*time.Millisecond)

	remote, ok := errors.AsType[cond.ErrRemote](work.result(work.action.Err()))
	require.True(t, ok)
	assert.Equal(t, "deployment", remote.Category)
	assert.Equal(t, "cancelled", remote.Code)
}

// When activation fails after the version is already live, the plain path must
// release the lock as a backstop so the app is not stalled behind a record that
// will never settle. Activation is forced to fail deterministically by moving
// the record to a state Transition-to-active rejects while the lock is held.
func TestActivateKeepsLockWhenActivationDidNotCommit(t *testing.T) {
	ctx := context.Background()
	b := newDeployTestBuilder(t)

	rec, err := b.beginDeploy(ctx, "web", deployRequest("prod"), nil, nil)
	require.NoError(t, err)

	// Drive the record to "active" out of band, keeping the lock. Activate's
	// transition (active -> active) is then a conflict, so tracker.Activate
	// errors without releasing the lock — the exact leak scenario.
	stored, err := b.deploy.Store().Get(ctx, rec.deploymentID)
	require.NoError(t, err)
	stored.Deployment.Status = string(deploylifecycle.StatusActive)
	require.NoError(t, b.deploy.Store().Put(ctx, stored))

	// Precondition: the lock is still held (active is non-terminal, so not
	// stealable), which would block the next deploy.
	blocking, err := b.deploy.Locks().Blocking(ctx, "web")
	require.NoError(t, err)
	require.NotNil(t, blocking, "lock should still be held before the backstop runs")

	require.Error(t, rec.activate(ctx))

	// A failure before the activation commit leaves the lock with this attempt,
	// so the caller can still settle the record.
	blocking, err = b.deploy.Locks().Blocking(ctx, "web")
	require.NoError(t, err)
	assert.NotNil(t, blocking, "a pre-commit failure must not pretend the deploy is over")
}

// A client disconnect (cancelled context) at activation must not prevent the
// settle: the detached context lets it complete.
func TestActivateSurvivesCancelledContext(t *testing.T) {
	ctx := context.Background()
	b := newDeployTestBuilder(t)

	rec, err := b.beginDeploy(ctx, "web", deployRequest("prod"), nil, nil)
	require.NoError(t, err)
	version := &core_v1alpha.AppVersion{App: "app/web", Version: "v1"}
	_, err = b.ec.Create(ctx, "v1", version)
	require.NoError(t, err)
	require.NoError(t, b.deploy.SetAppVersion(ctx, rec.deploymentID, "app_version/v1"))

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	require.NoError(t, rec.activate(cancelled))

	settled, err := b.deploy.Store().Get(ctx, rec.deploymentID)
	require.NoError(t, err)
	assert.Equal(t, deploylifecycle.StatusSucceeded, settled.Status(),
		"activation must complete on a detached context despite a cancelled parent")

	blocking, err := b.deploy.Locks().Blocking(ctx, "web")
	require.NoError(t, err)
	assert.Nil(t, blocking, "a completed activation releases the lock")
}

func TestGitInfoFromRequest(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		assert.Equal(t, "", gitInfoFromRequest(nil).Sha)
	})

	t.Run("request without git info", func(t *testing.T) {
		info := gitInfoFromRequest(deployRequest("prod"))
		assert.Equal(t, "", info.Sha)
	})

	t.Run("full conversion", func(t *testing.T) {
		ts := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

		gi := &build_v1alpha.GitInfo{}
		gi.SetSha("abc123")
		gi.SetBranch("main")
		gi.SetRepository("git@github.com:acme/web.git")
		gi.SetIsDirty(true)
		gi.SetWorkingTreeHash("deadbeef")
		gi.SetCommitMessage("fix the thing")
		gi.SetCommitAuthorName("Ada")
		gi.SetCommitAuthorEmail("ada@example.com")
		gi.SetCommitTimestamp(standard.ToTimestamp(ts))

		req := &build_v1alpha.DeployRequest{}
		req.SetGitInfo(gi)

		info := gitInfoFromRequest(req)
		assert.Equal(t, "abc123", info.Sha)
		assert.Equal(t, "main", info.Branch)
		assert.Equal(t, "git@github.com:acme/web.git", info.Repository)
		assert.True(t, info.IsDirty)
		assert.Equal(t, "deadbeef", info.WorkingTreeHash)
		assert.Equal(t, "fix the thing", info.Message)
		assert.Equal(t, "Ada", info.Author)
		assert.Equal(t, "ada@example.com", info.CommitAuthorEmail)

		// Stored as an RFC3339 string in the server's local zone, matching the
		// old deployment server; compare the instant, not the rendering.
		gotTS, err := time.Parse(time.RFC3339, info.CommitTimestamp)
		require.NoError(t, err)
		assert.True(t, gotTS.Equal(ts), "want %s, got %s", ts, gotTS)
	})
}

func TestEphemeralOptsFromArgs(t *testing.T) {
	assert.Nil(t, ephemeralOptsFromArgs(false, "", false, ""))
	assert.Nil(t, ephemeralOptsFromArgs(true, "", false, ""), "an empty label is not ephemeral")

	got := ephemeralOptsFromArgs(true, "pr-1", false, "")
	require.NotNil(t, got)
	assert.Equal(t, "pr-1", got.label)
	assert.Equal(t, "24h", got.ttl, "ttl defaults when unset")

	got = ephemeralOptsFromArgs(true, "pr-1", true, "48h")
	require.NotNil(t, got)
	assert.Equal(t, "48h", got.ttl)
}
