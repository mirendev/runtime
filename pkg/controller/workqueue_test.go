package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogfmt"
	"miren.dev/runtime/servers/entityserver"
)

func TestDirtyQueueCoalescesByKey(t *testing.T) {
	q := newDirtyQueue()
	defer q.Close()

	for i := range 5 {
		result := q.Add(workSignal{
			id:       "widget/one",
			priority: workUrgent,
			present:  true,
			created:  i == 0,
		})
		if i == 0 {
			assert.Equal(t, enqueueQueued, result)
		} else {
			assert.Equal(t, enqueueCoalesced, result)
		}
	}

	assert.Equal(t, 1, q.Stats().depth)
	item, ok := q.Get(t.Context())
	require.True(t, ok)
	assert.Equal(t, entity.Id("widget/one"), item.id)
	assert.True(t, item.created, "the first create survives later update signals")
	q.Done(item, nil)
	assert.Zero(t, q.Stats().depth)
}

func TestDirtyQueuePrioritizesAndPromotesUrgentWork(t *testing.T) {
	q := newDirtyQueue()
	defer q.Close()

	q.Add(workSignal{id: "repair/one", priority: workRepair, present: true})
	q.Add(workSignal{id: "repair/two", priority: workRepair, present: true})
	q.Add(workSignal{id: "repair/two", priority: workUrgent, present: true})
	q.Add(workSignal{id: "watch/one", priority: workUrgent, present: true})

	first, ok := q.Get(t.Context())
	require.True(t, ok)
	assert.Equal(t, entity.Id("repair/two"), first.id)
	q.Done(first, nil)

	second, ok := q.Get(t.Context())
	require.True(t, ok)
	assert.Equal(t, entity.Id("watch/one"), second.id)
	q.Done(second, nil)

	third, ok := q.Get(t.Context())
	require.True(t, ok)
	assert.Equal(t, entity.Id("repair/one"), third.id)
	q.Done(third, nil)
}

func TestDirtyQueueIgnoresStaleLaneTokenAfterReaddingKey(t *testing.T) {
	q := newDirtyQueue()
	defer q.Close()

	q.Add(workSignal{id: "repair/a", priority: workRepair, present: true})
	q.Add(workSignal{id: "repair/a", priority: workUrgent, present: true})
	urgent, ok := q.Get(t.Context())
	require.True(t, ok)
	q.Done(urgent, nil)

	q.Add(workSignal{id: "repair/b", priority: workRepair, present: true})
	q.Add(workSignal{id: "repair/a", priority: workRepair, present: true})
	first, ok := q.Get(t.Context())
	require.True(t, ok)
	assert.Equal(t, entity.Id("repair/b"), first.id)
	q.Done(first, nil)
}

func TestDirtyQueueDoesNotLetRepairFloodDelayUrgentWork(t *testing.T) {
	q := newDirtyQueue()
	defer q.Close()

	for i := range 1100 {
		result := q.Add(workSignal{
			id:       entity.Id(fmt.Sprintf("repair/%d", i)),
			priority: workRepair,
			present:  true,
		})
		assert.Equal(t, enqueueQueued, result)
	}
	q.Add(workSignal{id: "watch/one", priority: workUrgent, present: true})

	assert.Equal(t, 1101, q.Stats().depth)
	first, ok := q.Get(t.Context())
	require.True(t, ok)
	assert.Equal(t, entity.Id("watch/one"), first.id)
	q.Done(first, nil)
}

func TestDirtyQueueKeepsOneFollowUpWhileInFlight(t *testing.T) {
	q := newDirtyQueue()
	defer q.Close()

	q.Add(workSignal{id: "widget/one", priority: workUrgent, present: true})
	first, ok := q.Get(t.Context())
	require.True(t, ok)
	for range 10 {
		q.Add(workSignal{id: "widget/one", priority: workUrgent, present: true})
	}
	q.Done(first, nil)

	second, ok := q.Get(t.Context())
	require.True(t, ok)
	assert.Equal(t, first.id, second.id)
	q.Done(second, nil)
	assert.Zero(t, q.Stats().depth)
}

func TestDirtyQueueWakesWorkersForBurst(t *testing.T) {
	q := newDirtyQueue()
	defer q.Close()

	const workers = 4
	items := make(chan workItem, workers)
	for range workers {
		go func() {
			item, ok := q.Get(t.Context())
			if ok {
				items <- item
			}
		}()
	}
	for i := range workers {
		q.Add(workSignal{
			id:       entity.Id(fmt.Sprintf("work/%d", i)),
			priority: workRepair,
			present:  true,
		})
	}

	for range workers {
		select {
		case item := <-items:
			q.Done(item, nil)
		case <-time.After(time.Second):
			t.Fatal("queued burst left workers asleep")
		}
	}
}

func TestDirtyQueueFreshWorkBypassesFailureBackoff(t *testing.T) {
	q := newDirtyQueue()
	defer q.Close()

	q.Add(workSignal{id: "widget/one", priority: workUrgent, present: true})
	item, ok := q.Get(t.Context())
	require.True(t, ok)
	require.True(t, q.Done(item, errors.New("transient")))

	q.Add(workSignal{id: "widget/one", priority: workUrgent, present: true})
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	retry, ok := q.Get(ctx)
	require.True(t, ok)
	assert.Equal(t, item.id, retry.id)
	q.Done(retry, nil)
}

func TestDirtyQueueRetriesFailuresAfterBackoff(t *testing.T) {
	q := newDirtyQueue()
	defer q.Close()
	q.backoff = func(int) time.Duration { return 10 * time.Millisecond }

	q.Add(workSignal{id: "widget/one", priority: workUrgent, present: true})
	item, ok := q.Get(t.Context())
	require.True(t, ok)
	require.True(t, q.Done(item, errors.New("transient")))

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	retry, ok := q.Get(ctx)
	require.True(t, ok)
	assert.Equal(t, item.id, retry.id)
	q.Done(retry, nil)
}

func TestRetryBackoffIsBounded(t *testing.T) {
	assert.Equal(t, time.Second, retryBackoff(1))
	assert.Equal(t, 32*time.Second, retryBackoff(6))
	assert.Equal(t, time.Minute, retryBackoff(7))
	assert.Equal(t, time.Minute, retryBackoff(100))
}

func TestRetryDelayJittersWithinBackoffWindow(t *testing.T) {
	const ceiling = 32 * time.Second
	buckets := make(map[int]bool)
	for range 500 {
		delay := retryDelay(6)
		assert.GreaterOrEqual(t, delay, ceiling/2)
		assert.LessOrEqual(t, delay, ceiling)
		buckets[int(delay/(ceiling/20))] = true
	}
	assert.GreaterOrEqual(t, len(buckets), 5, "retry delays should spread across the jitter window")
}

func TestWatchSignalsSkipSelfWritesWithoutLosingLaterWork(t *testing.T) {
	c := &ReconcileController{
		name:         "test",
		Log:          slog.Default(),
		queue:        newDirtyQueue(),
		recentWrites: NewRingSet(3),
	}
	defer c.queue.Close()
	c.RecordWrite(2)
	assert.Equal(t, uint64(1), c.counters.writes.Load(), "manual writes should contribute to write velocity")

	c.acceptWatchEvent(indexwatch.Event{Type: indexwatch.EventUpdated, Id: "widget/one", Rev: 2})
	assert.Zero(t, c.queue.Stats().depth)
	c.acceptWatchEvent(indexwatch.Event{Type: indexwatch.EventUpdated, Id: "widget/one", Rev: 3})
	assert.Equal(t, 1, c.queue.Stats().depth)
}

func TestWatchSnapshotQueuesCurrentEntities(t *testing.T) {
	c := &ReconcileController{
		name:         "test",
		Log:          slog.Default(),
		queue:        newDirtyQueue(),
		recentWrites: NewRingSet(3),
	}
	defer c.queue.Close()

	c.acceptWatchEvent(indexwatch.Event{
		Type: indexwatch.EventSync,
		Entities: []*entity.Entity{
			entity.New(entity.Ident, "widget/current"),
		},
	})

	current, ok := c.queue.Get(t.Context())
	require.True(t, ok)
	assert.Equal(t, entity.Id("widget/current"), current.id)
	assert.True(t, current.present)
	c.queue.Done(current, nil)
	assert.Zero(t, c.queue.Stats().depth)
}

func TestReconcileControllerCoalescesAndReadsCurrentState(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store := entity.NewMockStore()
	server := &entityserver.EntityServer{Log: log, Store: store}
	client := &entityserver_v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)),
	}
	id := entity.Id("test/entity1")
	store.AddEntity(id, entity.New(
		entity.Ident, id,
		entity.Type, "test/type",
		entity.Any("generation", int64(1)),
	))

	started := make(chan int64, 2)
	release := make(chan struct{}, 2)
	var calls atomic.Int32
	handler := func(ctx context.Context, event Event) ([]entity.Attr, error) {
		generation, _ := event.Entity.Get("generation")
		started <- generation.Value.Int64()
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	}
	c := NewReconcileController("test", log, entity.Any(entity.Type, "test/type"), client, handler, 0, 2)
	require.NoError(t, c.Start(t.Context()))
	defer c.Stop()

	require.Equal(t, int64(1), <-started)
	store.AddEntity(id, entity.New(
		entity.Ident, id,
		entity.Type, "test/type",
		entity.Any("generation", int64(5)),
	))
	for range 20 {
		c.Enqueue(Event{Type: EventUpdated, Id: id, Entity: entity.New(entity.Any("generation", int64(2)))})
	}
	release <- struct{}{}
	require.Equal(t, int64(5), <-started, "the follow-up must ignore queued snapshots")
	release <- struct{}{}
	require.Eventually(t, func() bool {
		return calls.Load() == 2 && c.queue.Stats().depth == 0
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, uint64(20), c.counters.coalesced.Load())
}

func TestReconcileControllerSkipsNoopWrites(t *testing.T) {
	log := slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
	store := entity.NewMockStore()
	server := &entityserver.EntityServer{Log: log, Store: store}
	client := &entityserver_v1alpha.EntityAccessClient{
		Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(server)),
	}
	id := entity.Id("test/entity1")
	store.AddEntity(id, entity.New(
		entity.Ident, id,
		entity.Type, "test/type",
		entity.Any("status", "ready"),
	))
	c := NewReconcileController(
		"test",
		log,
		entity.Any(entity.Type, "test/type"),
		client,
		func(context.Context, Event) ([]entity.Attr, error) {
			return []entity.Attr{entity.Any("status", "ready")}, nil
		},
		0,
		1,
	)

	require.NoError(t, c.ProcessEventForTest(t.Context(), Event{Type: EventUpdated, Id: id}))
	assert.Zero(t, c.counters.writes.Load())
}
