package lifecyclesync

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/serverlifecycle"
)

func testStore(t *testing.T) *serverlifecycle.Store {
	t.Helper()
	store, err := serverlifecycle.NewStore(t.TempDir())
	require.NoError(t, err)
	return store
}

func TestWatcherReadsLedgerOnFirstPass(t *testing.T) {
	store := testStore(t)

	done := serverlifecycle.NewOperation(serverlifecycle.ActionUpgrade, "cli")
	done.TargetVersion = "v0.15.0"
	require.NoError(t, store.Create(done))
	done.Phase = serverlifecycle.PhaseSucceeded
	done.NewVersion = "v0.15.0"
	require.NoError(t, store.Update(done))

	running := serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cloud")
	require.NoError(t, store.Create(running))

	watcher := NewWatcher(slog.Default(), store)
	sub, stop := watcher.Subscribe()
	defer stop()
	active, count, err := watcher.sync(context.Background())
	require.NoError(t, err)
	require.True(t, active)
	require.Equal(t, 2, count)

	// Both records are announced once, oldest first, as the ledger sorts.
	<-sub.Wake()
	got := sub.Drain()
	require.Len(t, got, 2)
	require.Equal(t, done.ID, got[0].ID)
	require.Equal(t, running.ID, got[1].ID)
}

func TestSubscriptionCoalescesUnreadChanges(t *testing.T) {
	store := testStore(t)
	watcher := NewWatcher(slog.Default(), store)
	sub, stop := watcher.Subscribe()
	defer stop()

	op := serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli")
	require.NoError(t, store.Create(op))
	_, _, err := watcher.sync(context.Background())
	require.NoError(t, err)
	// Two more changes land before anyone reads. Nothing is dropped, and the
	// reader sees the latest state once rather than the history.
	op.Phase = serverlifecycle.PhaseRestarting
	require.NoError(t, store.Update(op))
	_, _, err = watcher.sync(context.Background())
	require.NoError(t, err)
	op.Phase = serverlifecycle.PhaseSucceeded
	require.NoError(t, store.Update(op))
	_, _, err = watcher.sync(context.Background())
	require.NoError(t, err)

	<-sub.Wake()
	got := sub.Drain()
	require.Len(t, got, 1)
	require.Equal(t, serverlifecycle.PhaseSucceeded, got[0].Phase)
	require.Empty(t, sub.Drain())
}

func TestWatcherAnnouncesChangesOnce(t *testing.T) {
	store := testStore(t)
	watcher := NewWatcher(slog.Default(), store)
	sub, stop := watcher.Subscribe()
	defer stop()

	op := serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli")
	require.NoError(t, store.Create(op))

	_, _, err := watcher.sync(context.Background())
	require.NoError(t, err)
	<-sub.Wake()
	require.Equal(t, op.ID, sub.Drain()[0].ID)

	// Nothing changed: nothing announced.
	_, _, err = watcher.sync(context.Background())
	require.NoError(t, err)
	select {
	case <-sub.Wake():
		t.Fatalf("unexpected wake: %v", sub.Drain())
	default:
	}

	op.Phase = serverlifecycle.PhaseRestarting
	require.NoError(t, store.Update(op))
	_, _, err = watcher.sync(context.Background())
	require.NoError(t, err)
	<-sub.Wake()
	got := sub.Drain()
	require.Len(t, got, 1)
	require.Equal(t, serverlifecycle.PhaseRestarting, got[0].Phase)
}

func TestWatcherRunFollowsWrites(t *testing.T) {
	store := testStore(t)
	watcher := NewWatcher(slog.Default(), store)
	sub, stop := watcher.Subscribe()
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = watcher.Run(ctx) }()

	// Kick after each write: the directory watch is what makes this prompt
	// on a real host, but the test must not depend on fsnotify being there.
	op := serverlifecycle.NewOperation(serverlifecycle.ActionRestart, "cli")
	require.NoError(t, store.Create(op))
	watcher.Kick()
	select {
	case <-sub.Wake():
		require.Equal(t, op.ID, sub.Drain()[0].ID)
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not notice the new record")
	}

	op.Phase = serverlifecycle.PhaseSucceeded
	require.NoError(t, store.Update(op))
	watcher.Kick()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-sub.Wake():
			got := sub.Drain()
			if len(got) > 0 && got[len(got)-1].Phase == serverlifecycle.PhaseSucceeded {
				return
			}
		case <-deadline:
			t.Fatal("watcher did not notice the phase change")
		}
	}
}
