package indexwatch_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/entityserver"
)

var testKind = entity.Id("test/widget")

func testIndex() entity.Attr {
	return entity.Ref(entity.EntityKind, testKind)
}

// newTestClient wires an EntityAccessClient to an in-process EntityServer backed
// by the given mock store, so watcher tests can run without etcd.
func newTestClient(store *entity.MockStore) *entityserver_v1alpha.EntityAccessClient {
	server := &entityserver.EntityServer{
		Log:   slog.Default(),
		Store: store,
	}
	return &entityserver_v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)),
	}
}

// makeEntity builds an entity with the given id that matches testIndex().
func makeEntity(id string) *entity.Entity {
	return entity.New(entity.Ref(entity.DBId, entity.Id(id)), testIndex())
}

func recv(t *testing.T, ch <-chan indexwatch.Event, d time.Duration) indexwatch.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(d):
		t.Fatal("timed out waiting for event")
		return indexwatch.Event{}
	}
}

func fastOpts() indexwatch.Options {
	return indexwatch.Options{
		Logger:     slog.Default(),
		MinBackoff: time.Millisecond,
		MaxBackoff: 10 * time.Millisecond,
	}
}

// idsOf returns the set of ids in a snapshot event.
func idsOf(ev indexwatch.Event) map[entity.Id]struct{} {
	ids := make(map[entity.Id]struct{}, len(ev.Entities))
	for _, en := range ev.Entities {
		ids[en.Id()] = struct{}{}
	}
	return ids
}

// putEvent builds a raw etcd put event for id at the given revision. create
// controls whether it reads as a create (CreateRevision == ModRevision) or a
// modify.
func putEvent(id string, rev int64, create bool) clientv3.WatchResponse {
	createRev := rev
	if !create {
		createRev = 1 // any value < rev makes IsModify() true
	}
	return clientv3.WatchResponse{
		Events: []*clientv3.Event{{
			Type: clientv3.EventTypePut,
			Kv: &mvccpb.KeyValue{
				Key:            []byte("k/" + id),
				Value:          []byte(id),
				CreateRevision: createRev,
				ModRevision:    rev,
			},
		}},
	}
}

// deleteEvent builds a raw etcd delete event for id at the given revision.
func deleteEvent(id string, rev int64) clientv3.WatchResponse {
	return clientv3.WatchResponse{
		Events: []*clientv3.Event{{
			Type: clientv3.EventTypeDelete,
			Kv: &mvccpb.KeyValue{
				Key:         []byte("k/" + id),
				ModRevision: rev,
			},
			PrevKv: &mvccpb.KeyValue{
				Key:         []byte("k/" + id),
				Value:       []byte(id),
				ModRevision: rev - 1,
			},
		}},
	}
}

// gatedWatch installs an OnWatchIndex hook that publishes a fresh controllable
// channel per watch attempt, so tests can drive raw responses and simulate
// disconnects.
func gatedWatch(store *entity.MockStore) chan chan clientv3.WatchResponse {
	chans := make(chan chan clientv3.WatchResponse, 16)
	store.OnWatchIndex = func(ctx context.Context, attr entity.Attr) (clientv3.WatchChan, error) {
		ch := make(chan clientv3.WatchResponse)
		chans <- ch
		return ch, nil
	}
	return chans
}

func nextChan(t *testing.T, chans chan chan clientv3.WatchResponse, d time.Duration) chan clientv3.WatchResponse {
	t.Helper()
	select {
	case ch := <-chans:
		return ch
	case <-time.After(d):
		t.Fatal("timed out waiting for watch attempt")
		return nil
	}
}

// TestWatcher_InitialSyncSnapshot verifies the initial snapshot is delivered as a
// single EventSync carrying the full set, and Synced fires.
func TestWatcher_InitialSyncSnapshot(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	for i := range 3 {
		_, err := store.CreateEntity(context.Background(), makeEntity(fmt.Sprintf("widget-%d", i)))
		r.NoError(err)
	}

	sc := newTestClient(store)
	ctx := t.Context()

	w := indexwatch.New(sc, testIndex(), fastOpts())
	r.NoError(w.Start(ctx))
	defer w.Stop()

	ev := recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventSync, ev.Type)
	r.Len(ev.Entities, 3)
	for _, en := range ev.Entities {
		r.NotNil(en)
	}

	select {
	case <-w.Synced():
	case <-time.After(5 * time.Second):
		t.Fatal("Synced did not fire")
	}
}

// TestWatcher_LiveEvents verifies live create/update/delete map to
// Added/Updated/Deleted via the resumed watch stream.
func TestWatcher_LiveEvents(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	chans := gatedWatch(store)
	sc := newTestClient(store)

	ctx := t.Context()

	w := indexwatch.New(sc, testIndex(), fastOpts())
	r.NoError(w.Start(ctx))
	defer w.Stop()

	// Empty initial snapshot.
	ev := recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventSync, ev.Type)
	r.Empty(ev.Entities)

	ch := nextChan(t, chans, 5*time.Second)

	// The entity must exist in the store for the server to read it on put events.
	_, err := store.CreateEntity(ctx, makeEntity("A"))
	r.NoError(err)

	ch <- putEvent("A", 10, true)
	ev = recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventAdded, ev.Type)
	r.Equal(entity.Id("A"), ev.Id)
	r.Equal(int64(10), ev.Rev)

	ch <- putEvent("A", 11, false)
	ev = recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventUpdated, ev.Type)
	r.Equal(entity.Id("A"), ev.Id)
	r.Equal(int64(11), ev.Rev)

	ch <- deleteEvent("A", 12)
	ev = recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventDeleted, ev.Type)
	r.Equal(entity.Id("A"), ev.Id)
	r.Equal(int64(12), ev.Rev)
}

// TestWatcher_ResumeFromCursorNoResnapshot verifies a transient disconnect
// resumes the watch from cursor+1 without taking a fresh snapshot.
func TestWatcher_ResumeFromCursorNoResnapshot(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	chans := gatedWatch(store)
	sc := newTestClient(store)

	ctx := t.Context()

	w := indexwatch.New(sc, testIndex(), fastOpts())
	r.NoError(w.Start(ctx))
	defer w.Stop()

	// Empty initial snapshot at revision 0 → first watch resumes from 1.
	r.Equal(indexwatch.EventSync, recv(t, w.Updates(), 5*time.Second).Type)

	_, err := store.CreateEntity(ctx, makeEntity("X"))
	r.NoError(err)

	ch1 := nextChan(t, chans, 5*time.Second)
	ch1 <- putEvent("X", 5, true) // cursor advances to 5
	ev := recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventAdded, ev.Type)
	r.Equal(int64(5), ev.Rev)

	// Transient disconnect.
	close(ch1)

	// Resume: next watch attempt delivers an update — NOT a fresh EventSync.
	ch2 := nextChan(t, chans, 5*time.Second)
	ch2 <- putEvent("X", 7, false)
	ev = recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventUpdated, ev.Type, "expected resume, not a re-snapshot")
	r.Equal(int64(7), ev.Rev)

	r.Equal([]int64{1, 6}, store.WatchFromRevsCopy(), "second watch should resume from cursor+1")
}

// TestWatcher_CompactionResnapshot verifies a compaction signal triggers a fresh
// snapshot delivered as a new EventSync.
func TestWatcher_CompactionResnapshot(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	chans := gatedWatch(store)

	_, err := store.CreateEntity(context.Background(), makeEntity("A"))
	r.NoError(err)

	sc := newTestClient(store)
	ctx := t.Context()

	w := indexwatch.New(sc, testIndex(), fastOpts())
	r.NoError(w.Start(ctx))
	defer w.Stop()

	// First snapshot contains A.
	ev := recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventSync, ev.Type)
	r.Contains(idsOf(ev), entity.Id("A"))

	ch1 := nextChan(t, chans, 5*time.Second)

	// A new entity appears, then the watch is compacted → forces a re-snapshot.
	_, err = store.CreateEntity(ctx, makeEntity("B"))
	r.NoError(err)
	ch1 <- clientv3.WatchResponse{Canceled: true, CompactRevision: 999}

	// Fresh snapshot now includes A and B.
	ev = recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventSync, ev.Type)
	got := idsOf(ev)
	r.Contains(got, entity.Id("A"))
	r.Contains(got, entity.Id("B"))
}

// TestWatcher_ProgressAdvancesCursor verifies a progress watermark advances the
// resume cursor without emitting an event.
func TestWatcher_ProgressAdvancesCursor(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	chans := gatedWatch(store)
	sc := newTestClient(store)

	ctx := t.Context()

	w := indexwatch.New(sc, testIndex(), fastOpts())
	r.NoError(w.Start(ctx))
	defer w.Stop()

	r.Equal(indexwatch.EventSync, recv(t, w.Updates(), 5*time.Second).Type)

	ch1 := nextChan(t, chans, 5*time.Second)
	// Progress watermark at revision 50, no events.
	ch1 <- clientv3.WatchResponse{Header: etcdserverpb.ResponseHeader{Revision: 50}}
	// Disconnect so the watcher resumes from the advanced cursor.
	close(ch1)

	// Deliver an event on the resumed watch and confirm no spurious event was
	// emitted from the progress watermark.
	ch2 := nextChan(t, chans, 5*time.Second)
	_, err := store.CreateEntity(ctx, makeEntity("Z"))
	r.NoError(err)
	ch2 <- putEvent("Z", 60, true)

	ev := recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventAdded, ev.Type)
	r.Equal(entity.Id("Z"), ev.Id)

	r.Equal([]int64{1, 51}, store.WatchFromRevsCopy(), "resume should start at progress revision + 1")
}

// TestWatcher_CreatedResponseDoesNotAdvanceCursor verifies that etcd's initial
// Created confirmation (empty events, Created=true) does NOT advance the resume
// cursor. The Created header carries the current store revision but arrives
// before the historical backlog replays; advancing the cursor to it and then
// disconnecting would skip the un-replayed backlog on resume, re-opening the gap
// the watcher exists to prevent (bug 1b).
func TestWatcher_CreatedResponseDoesNotAdvanceCursor(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	chans := gatedWatch(store)
	sc := newTestClient(store)

	ctx := t.Context()

	w := indexwatch.New(sc, testIndex(), fastOpts())
	r.NoError(w.Start(ctx))
	defer w.Stop()

	// Empty initial snapshot at revision 0 → first watch resumes from 1.
	r.Equal(indexwatch.EventSync, recv(t, w.Updates(), 5*time.Second).Type)

	ch1 := nextChan(t, chans, 5*time.Second)
	// The Created confirmation reports the current revision (100) but no backlog.
	ch1 <- clientv3.WatchResponse{Created: true, Header: etcdserverpb.ResponseHeader{Revision: 100}}
	// Disconnect before any real event arrives.
	close(ch1)

	// Resume must start from cursor+1 = 1 (unchanged), NOT 101. If the Created
	// response had advanced the cursor to 100, events 1..100 would be lost.
	ch2 := nextChan(t, chans, 5*time.Second)
	_, err := store.CreateEntity(ctx, makeEntity("Z"))
	r.NoError(err)
	ch2 <- putEvent("Z", 60, true)

	ev := recv(t, w.Updates(), 5*time.Second)
	r.Equal(indexwatch.EventAdded, ev.Type)
	r.Equal(entity.Id("Z"), ev.Id)

	r.Equal([]int64{1, 1}, store.WatchFromRevsCopy(),
		"Created response must not advance the cursor; resume should still start at 1")
}

// TestWatcher_BlockingBackpressure verifies a slow consumer never loses live
// events with a buffer of 1: the watcher blocks rather than dropping.
func TestWatcher_BlockingBackpressure(t *testing.T) {
	r := require.New(t)

	const n = 30
	store := entity.NewMockStore()
	chans := gatedWatch(store)
	sc := newTestClient(store)

	ctx := t.Context()

	opts := fastOpts()
	opts.BufferSize = 1
	w := indexwatch.New(sc, testIndex(), opts)
	r.NoError(w.Start(ctx))
	defer w.Stop()

	// Empty initial snapshot.
	r.Equal(indexwatch.EventSync, recv(t, w.Updates(), 5*time.Second).Type)

	ch := nextChan(t, chans, 5*time.Second)

	// Pre-create the entities so the server can read them on put events.
	for i := range n {
		_, err := store.CreateEntity(ctx, makeEntity(fmt.Sprintf("widget-%d", i)))
		r.NoError(err)
	}

	// Push events faster than they are consumed; the unbuffered channel + buffer
	// of 1 force backpressure all the way to this goroutine.
	go func() {
		for i := range n {
			ch <- putEvent(fmt.Sprintf("widget-%d", i), int64(i+10), true)
		}
	}()

	seen := make(map[entity.Id]struct{}, n)
	deadline := time.After(15 * time.Second)
	for len(seen) < n {
		select {
		case ev := <-w.Updates():
			r.Equal(indexwatch.EventAdded, ev.Type)
			seen[ev.Id] = struct{}{}
			time.Sleep(time.Millisecond) // slow consumer
		case <-deadline:
			t.Fatalf("only received %d/%d events", len(seen), n)
		}
	}
	r.Len(seen, n)
}

// TestWatcher_StopClosesUpdates verifies Stop is idempotent and closes Updates.
func TestWatcher_StopClosesUpdates(t *testing.T) {
	r := require.New(t)

	store := entity.NewMockStore()
	sc := newTestClient(store)

	w := indexwatch.New(sc, testIndex(), fastOpts())
	r.NoError(w.Start(context.Background()))

	select {
	case <-w.Synced():
	case <-time.After(5 * time.Second):
		t.Fatal("Synced did not fire")
	}

	w.Stop()
	w.Stop() // idempotent

	// Drain any buffered snapshot event; the channel must end up closed.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-w.Updates():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("Updates not closed after Stop")
		}
	}
}
