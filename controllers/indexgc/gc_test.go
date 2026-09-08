package indexgc

import (
	"log/slog"
	"testing"
	"time"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/etcdtest"
)

// The sweep itself is covered in pkg/entity; these cover the wiring around it.

// seedOrphan writes a collection entry pointing at a nonexistent entity, the
// cheapest thing a sweep will delete.
func seedOrphan(t *testing.T, store *entity.EtcdStore, client *clientv3.Client, entityID entity.Id) string {
	t.Helper()

	key := store.Prefix() + "/collections/test_kind_widget/" + base58.Encode([]byte(entityID))
	_, err := client.Put(t.Context(), key, string(entityID))
	require.NoError(t, err)
	return key
}

func exists(t *testing.T, client *clientv3.Client, key string) bool {
	t.Helper()

	resp, err := client.Get(t.Context(), key)
	require.NoError(t, err)
	return len(resp.Kvs) > 0
}

func newTestController(t *testing.T, cfg GCConfig) (*GCController, *entity.EtcdStore, *clientv3.Client) {
	t.Helper()

	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := entity.NewEtcdStore(t.Context(), slog.Default(), client, prefix)
	require.NoError(t, err)

	return &GCController{Log: slog.Default(), Store: store, Config: cfg}, store, client
}

// TestGCController_SweepsOnSchedule proves the loop reaches the sweep at all.
// The controller is fire-and-forget, so a wiring mistake here looks exactly like
// a cluster that never converges.
func TestGCController_SweepsOnSchedule(t *testing.T) {
	c, store, client := newTestController(t, GCConfig{
		InitialDelay:       10 * time.Millisecond,
		CheckInterval:      10 * time.Millisecond,
		MaxDeletesPerSweep: 100,
		SweepTimeout:       30 * time.Second,
	})

	key := seedOrphan(t, store, client, entity.Id("fake/nonexistent"))
	require.True(t, exists(t, client, key))

	c.Start(t.Context())
	defer c.Stop()

	require.Eventually(t, func() bool {
		return !exists(t, client, key)
	}, 10*time.Second, 20*time.Millisecond, "the scheduled sweep never removed the orphan")
}

// TestGCController_StopBeforeFirstSweep covers shutdown during the initial
// delay, where a controller started at boot spends its first minute.
func TestGCController_StopBeforeFirstSweep(t *testing.T) {
	c, store, client := newTestController(t, GCConfig{
		InitialDelay:       time.Hour,
		CheckInterval:      time.Hour,
		MaxDeletesPerSweep: 100,
		SweepTimeout:       30 * time.Second,
	})

	key := seedOrphan(t, store, client, entity.Id("fake/nonexistent"))

	c.Start(t.Context())

	stopped := make(chan struct{})
	go func() {
		c.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return; the sweep loop is not honouring cancellation")
	}

	assert.True(t, exists(t, client, key),
		"a controller stopped during its initial delay must not have swept")
}

func TestGCController_StopIsSafeFromAnyState(t *testing.T) {
	never := &GCController{Log: slog.Default()}
	assert.NotPanics(t, never.Stop, "stopping an unstarted controller must be a no-op")

	c, _, _ := newTestController(t, GCConfig{
		InitialDelay:       time.Hour,
		CheckInterval:      time.Hour,
		MaxDeletesPerSweep: 100,
		SweepTimeout:       30 * time.Second,
	})
	c.Start(t.Context())
	c.Stop()
	assert.NotPanics(t, c.Stop, "Stop must be idempotent")
}
