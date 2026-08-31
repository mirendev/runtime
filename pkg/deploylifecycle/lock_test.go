package deploylifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestLocks(t *testing.T, status StatusLookup) (*Locks, *fakeClock) {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	clock := &fakeClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}

	locks := NewLocks(log, inmem.EAC, status)
	locks.now = clock.Now
	locks.sleep = func(time.Duration) {} // don't add real latency to contention tests
	for _, appName := range []string{"web", "api"} {
		_, err := inmem.EAC.Ensure(context.Background(), []entity.Attr{
			entity.Ref(entity.DBId, entity.Id("app/"+appName)),
			entity.Ref(entity.EntityKind, core_v1alpha.KindApp),
		})
		require.NoError(t, err)
	}

	return locks, clock
}

func TestAcquireFreeLock(t *testing.T) {
	ctx := context.Background()
	locks, clock := newTestLocks(t, nil)

	holder, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	assert.Equal(t, "web", holder.AppName)
	assert.Equal(t, "dep-1", holder.DeploymentID)
	assert.Equal(t, clock.Now(), holder.AcquiredAt)
	assert.Equal(t, clock.Now().Add(DefaultLockTTL), holder.ExpiresAt)
}

func TestAcquireBlockedByLiveHolder(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, func(context.Context, string) (Status, error) {
		return StatusInProgress, nil
	})

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	holder, err := locks.Acquire(ctx, "web", "dep-2")
	require.ErrorIs(t, err, ErrLockHeld)
	require.NotNil(t, holder, "the blocked caller needs the holder to explain the block")
	assert.Equal(t, "dep-1", holder.DeploymentID)
}

// The same deployment retrying its own acquire must not deadlock itself.
func TestAcquireIsIdempotentForSameDeployment(t *testing.T) {
	ctx := context.Background()
	locks, clock := newTestLocks(t, func(context.Context, string) (Status, error) {
		return StatusInProgress, nil
	})

	first, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	clock.Advance(time.Minute)

	second, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)
	assert.Equal(t, "dep-1", second.DeploymentID)
	assert.Equal(t, first.AcquiredAt, second.AcquiredAt,
		"refreshing authority must preserve when the lock began")
	assert.True(t, second.ExpiresAt.After(first.ExpiresAt),
		"re-acquiring should extend the expiry")
}

func TestAcquireStealsExpiredLock(t *testing.T) {
	ctx := context.Background()
	locks, clock := newTestLocks(t, func(context.Context, string) (Status, error) {
		// Still claims to be running — expiry alone must be enough.
		return StatusInProgress, nil
	})

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	clock.Advance(DefaultLockTTL + time.Second)

	holder, err := locks.Acquire(ctx, "web", "dep-2")
	require.NoError(t, err)
	assert.Equal(t, "dep-2", holder.DeploymentID)
}

func TestCommitActivationAtRevisionRejectsChangedBase(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, nil)

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	base, err := locks.eac.Get(ctx, "app/web")
	require.NoError(t, err)
	baseRevision := base.Entity().Revision()

	_, err = locks.eac.Patch(ctx, []entity.Attr{
		entity.Ref(entity.DBId, "app/web"),
		entity.Ref(core_v1alpha.AppActiveVersionId, "app_version/concurrent"),
	}, 0)
	require.NoError(t, err)

	err = locks.CommitActivationAtRevision(ctx, "web", "app/web", "app_version/ours", "dep-1", baseRevision)
	require.Error(t, err)
	assert.True(t, errors.Is(err, cond.ErrConflict{}), "got %T: %v", err, err)

	current, err := locks.eac.Get(ctx, "app/web")
	require.NoError(t, err)
	var app core_v1alpha.App
	app.Decode(&entityAttrs{entity: current.Entity()})
	assert.Equal(t, entity.Id("app_version/concurrent"), app.ActiveVersion)
	assert.Empty(t, app.ActiveDeployment)
}

// Lock and record can drift apart: the build failed but the lock outlived it.
// The next deploy should not have to wait out the full TTL for that.
func TestAcquireStealsLockWhoseDeploymentIsTerminal(t *testing.T) {
	ctx := context.Background()

	status := StatusInProgress
	locks, _ := newTestLocks(t, func(context.Context, string) (Status, error) {
		return status, nil
	})

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	_, err = locks.Acquire(ctx, "web", "dep-2")
	require.ErrorIs(t, err, ErrLockHeld, "still running, so still blocking")

	status = StatusFailed

	holder, err := locks.Acquire(ctx, "web", "dep-2")
	require.NoError(t, err, "a finished deployment must not keep blocking")
	assert.Equal(t, "dep-2", holder.DeploymentID)
}

func TestAcquireWaitsForMissingDeploymentCreateWindow(t *testing.T) {
	ctx := context.Background()
	locks, clock := newTestLocks(t, func(context.Context, string) (Status, error) {
		return "", cond.NotFound("deployment", "dep-1")
	})

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	_, err = locks.Acquire(ctx, "web", "dep-2")
	require.ErrorIs(t, err, ErrLockHeld, "Begin acquires the lock just before creating its record")

	clock.Advance(DefaultLockTTL + time.Second)
	holder, err := locks.Acquire(ctx, "web", "dep-2")
	require.NoError(t, err)
	assert.Equal(t, "dep-2", holder.DeploymentID)
}

// A lookup that errors for reasons other than absence must fail closed: we do
// not know the holder is dead, so we must not steal from it.
func TestAcquireDoesNotStealWhenStatusUnreadable(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, func(context.Context, string) (Status, error) {
		return "", errors.New("entity store unavailable")
	})

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	_, err = locks.Acquire(ctx, "web", "dep-2")
	require.ErrorIs(t, err, ErrLockHeld)
}

// Without a StatusLookup the only ground for stealing is expiry.
func TestAcquireWithoutStatusLookupBlocksUntilExpiry(t *testing.T) {
	ctx := context.Background()
	locks, clock := newTestLocks(t, nil)

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	_, err = locks.Acquire(ctx, "web", "dep-2")
	require.ErrorIs(t, err, ErrLockHeld)

	clock.Advance(DefaultLockTTL)

	_, err = locks.Acquire(ctx, "web", "dep-2")
	require.NoError(t, err)
}

func TestReleaseFreesTheLock(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, func(context.Context, string) (Status, error) {
		return StatusInProgress, nil
	})

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	require.NoError(t, locks.Release(ctx, "web", "dep-1"))

	holder, err := locks.Acquire(ctx, "web", "dep-2")
	require.NoError(t, err)
	assert.Equal(t, "dep-2", holder.DeploymentID)
}

// A deployment that was stolen from must not release its successor's lock on
// the way out — that would hand a third deploy a lock it never won.
func TestReleaseByNonHolderIsIgnored(t *testing.T) {
	ctx := context.Background()
	locks, clock := newTestLocks(t, nil)

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	clock.Advance(DefaultLockTTL)

	_, err = locks.Acquire(ctx, "web", "dep-2")
	require.NoError(t, err)

	require.NoError(t, locks.Release(ctx, "web", "dep-1"),
		"releasing a lock you no longer hold is a no-op, not an error")

	holder, err := locks.Get(ctx, "web")
	require.NoError(t, err)
	assert.Equal(t, "dep-2", holder.DeploymentID, "dep-2 must still hold the lock")
}

// The race that an unguarded delete lost. Release reads the holder, and in that
// window the holder's deployment finishes and a queued deploy legitimately
// steals the lock. Releasing must not then free the successor's lock, because a
// third deploy would start alongside it.
func TestReleaseDoesNotFreeALockStolenInTheReadWindow(t *testing.T) {
	ctx := context.Background()

	status := StatusInProgress
	locks, _ := newTestLocks(t, func(context.Context, string) (Status, error) {
		return status, nil
	})

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	// Interleave dep-2's steal between dep-1's read of the holder and its write
	// of the release — the exact window a check-then-act delete cannot guard.
	var stole bool
	locks.releaseRace = func() {
		locks.releaseRace = nil // once, and not re-entrantly

		status = StatusFailed // dep-1 has finished, so its lock is stealable
		_, err := locks.Acquire(ctx, "web", "dep-2")
		require.NoError(t, err)
		stole = true
	}

	require.NoError(t, locks.Release(ctx, "web", "dep-1"))
	require.True(t, stole, "the hook must have run, or this proves nothing")

	holder, err := locks.Get(ctx, "web")
	require.NoError(t, err)
	assert.Equal(t, "dep-2", holder.DeploymentID,
		"dep-1's release must not have taken dep-2's lock")

	status = StatusInProgress // dep-2 is running

	blocking, err := locks.Blocking(ctx, "web")
	require.NoError(t, err)
	require.NotNil(t, blocking, "dep-2 still holds the lock")
	assert.Equal(t, "dep-2", blocking.DeploymentID)

	_, err = locks.Acquire(ctx, "web", "dep-3")
	require.ErrorIs(t, err, ErrLockHeld,
		"a third deploy must not be able to start alongside dep-2")
}

func TestReleaseRetriesAfterUnrelatedAppWrite(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, nil)

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	locks.releaseRace = func() {
		locks.releaseRace = nil
		_, err := locks.eac.Patch(ctx, []entity.Attr{
			entity.Ref(entity.DBId, "app/web"),
			entity.String(core_v1alpha.AppWorkloadRoleId, "app-writer"),
		}, 0)
		require.NoError(t, err)
	}

	require.NoError(t, locks.Release(ctx, "web", "dep-1"))
	blocking, err := locks.Blocking(ctx, "web")
	require.NoError(t, err)
	assert.Nil(t, blocking)

	res, err := locks.eac.Get(ctx, "app/web")
	require.NoError(t, err)
	var app core_v1alpha.App
	app.Decode(&entityAttrs{entity: res.Entity()})
	assert.Equal(t, "app-writer", app.WorkloadRole)
}

// A released lock is represented by an empty component, so the free/held
// distinction has to survive that.
func TestBlockingReportsOnlyRealBlockers(t *testing.T) {
	ctx := context.Background()

	status := StatusInProgress
	locks, clock := newTestLocks(t, func(context.Context, string) (Status, error) {
		return status, nil
	})

	blocking, err := locks.Blocking(ctx, "web")
	require.NoError(t, err)
	assert.Nil(t, blocking, "a lock that never existed blocks nobody")

	_, err = locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	blocking, err = locks.Blocking(ctx, "web")
	require.NoError(t, err)
	require.NotNil(t, blocking)
	assert.Equal(t, "dep-1", blocking.DeploymentID)

	status = StatusFailed
	blocking, err = locks.Blocking(ctx, "web")
	require.NoError(t, err)
	assert.Nil(t, blocking, "a lock held for a finished deployment blocks nobody")

	status = StatusInProgress
	require.NoError(t, locks.Release(ctx, "web", "dep-1"))

	blocking, err = locks.Blocking(ctx, "web")
	require.NoError(t, err)
	assert.Nil(t, blocking, "a released lock blocks nobody")

	_, err = locks.Acquire(ctx, "web", "dep-2")
	require.NoError(t, err)
	clock.Advance(DefaultLockTTL + time.Second)

	blocking, err = locks.Blocking(ctx, "web")
	require.NoError(t, err)
	assert.Nil(t, blocking, "an expired lock blocks nobody")
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	live := &Holder{DeploymentID: "dep-1", ExpiresAt: now.Add(time.Minute)}
	assert.False(t, live.Expired(now))

	expired := &Holder{DeploymentID: "dep-1", ExpiresAt: now.Add(-time.Minute)}
	assert.True(t, expired.Expired(now))

	// A lock exactly at its expiry is expired: the boundary belongs to the
	// contender, so a lock can never outlive its TTL by a tick.
	boundary := &Holder{DeploymentID: "dep-1", ExpiresAt: now}
	assert.True(t, boundary.Expired(now))
}

func TestReleaseUnheldLockIsNoOp(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, nil)

	require.NoError(t, locks.Release(ctx, "web", "dep-1"))
}

func TestGetFreeLockReportsNotFound(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, nil)

	_, err := locks.Get(ctx, "web")
	require.Error(t, err)
	assert.True(t, errors.Is(err, cond.ErrNotFound{}), "got %T: %v", err, err)
}

// Concurrent contenders for one app must produce exactly one admitted owner.
func TestConcurrentAcquireYieldsExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, func(context.Context, string) (Status, error) {
		return StatusInProgress, nil
	})

	const contenders = 16

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []string
	)

	start := make(chan struct{})
	for i := range contenders {
		id := "dep-" + string(rune('a'+i))
		wg.Go(func() {
			<-start

			holder, err := locks.Acquire(ctx, "web", id)
			if err != nil {
				assert.ErrorIs(t, err, ErrLockHeld, "a loser must lose cleanly")
				return
			}
			mu.Lock()
			winners = append(winners, holder.DeploymentID)
			mu.Unlock()
		})
	}

	close(start)
	wg.Wait()

	require.Len(t, winners, 1, "exactly one deployment may hold the lock")

	holder, err := locks.Get(ctx, "web")
	require.NoError(t, err)
	assert.Equal(t, winners[0], holder.DeploymentID,
		"the stored lock must agree with the caller that thinks it won")
}

// A steal that loses the compare-and-swap must back off before retrying, rather
// than spin against the entity store.
func TestAcquireBacksOffOnStealConflict(t *testing.T) {
	ctx := context.Background()

	// status makes the holder always stealable, so a contender keeps trying.
	locks, _ := newTestLocks(t, func(context.Context, string) (Status, error) {
		return StatusFailed, nil
	})

	var sleeps int
	locks.sleep = func(time.Duration) { sleeps++ }

	// Seed a stealable holder.
	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	// Force the replace to conflict once by advancing the lock's revision out
	// from under the next acquire: another steal lands between read and write.
	locks.stealRace = func() {
		locks.stealRace = nil
		_, err := locks.Acquire(ctx, "web", "dep-x")
		require.NoError(t, err)
	}

	_, err = locks.Acquire(ctx, "web", "dep-2")
	require.NoError(t, err)
	assert.Positive(t, sleeps, "a contended steal must back off before retrying")
}

func TestAcquireRequiresIdentifiers(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, nil)

	for _, tc := range []struct{ app, deployment string }{
		{"", "dep-1"},
		{"web", ""},
	} {
		_, err := locks.Acquire(ctx, tc.app, tc.deployment)
		require.Error(t, err, "app=%q deployment=%q", tc.app, tc.deployment)
	}
}

func TestLockIsCanonicalOnAppAndShadowedForDowngrade(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, nil)

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	res, err := locks.eac.Get(ctx, "app/web")
	require.NoError(t, err)
	var app core_v1alpha.App
	app.Decode(&entityAttrs{entity: res.Entity()})
	assert.Equal(t, "dep-1", app.DeploymentLock.DeploymentId)

	legacy, err := locks.getLegacy(ctx, "web")
	require.NoError(t, err)
	assert.Equal(t, "dep-1", legacy.DeploymentID)
}

func TestLegacyOnlyLockBlocksCurrentAcquire(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, nil)

	legacy := locks.holderFor("web", "old-deployment")
	_, err := locks.eac.Ensure(ctx, encodeLegacyLock(legacyLockID("web"), legacy))
	require.NoError(t, err)

	holder, err := locks.Acquire(ctx, "web", "new-deployment")
	require.ErrorIs(t, err, ErrLockHeld)
	require.NotNil(t, holder)
	assert.Equal(t, "old-deployment", holder.DeploymentID)

	_, err = locks.Get(ctx, "web")
	assert.ErrorIs(t, err, cond.ErrNotFound{}, "blocked acquire must not publish the canonical app lock")
}

// The lock is app-scoped: the same app is one lock regardless of cluster, but
// different apps are different locks.
func TestLocksAreScopedPerApp(t *testing.T) {
	ctx := context.Background()
	locks, _ := newTestLocks(t, func(context.Context, string) (Status, error) {
		return StatusInProgress, nil
	})

	_, err := locks.Acquire(ctx, "web", "dep-1")
	require.NoError(t, err)

	// A second deploy of the same app is blocked, even if the client meant a
	// different cluster — the coordinator only serves one cluster, and the
	// client cluster string is unreliable.
	_, err = locks.Acquire(ctx, "web", "dep-2")
	require.ErrorIs(t, err, ErrLockHeld, "the same app is a single lock")

	_, err = locks.Acquire(ctx, "api", "dep-3")
	require.NoError(t, err, "a different app is a different lock")
}
