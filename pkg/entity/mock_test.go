package entity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// A fixture placed with AddEntity has index entries from the moment it lands,
// so the first write against it must read as a modify of those entries, not a
// create. Only the mock has fixtures, so this lives outside the conformance
// suite; the create-versus-modify contract itself is pinned there.
func TestMockStoreFixtureUpdateReadsAsModify(t *testing.T) {
	ctx := t.Context()
	store := NewMockStore()
	require.NoError(t, InitSystemEntities(func(e *Entity) error {
		store.AddEntity(e.Id(), e)
		return nil
	}))

	target := Id("mock-fixture-target/v1")
	index := Ref(Id("mock/ref"), target)
	fixture := New(Ref(DBId, Id("mock-fixture")), index)
	fixture.SetRevision(7)
	store.AddEntity(fixture.Id(), fixture)

	_, rev, err := store.ListIndexRevision(ctx, index)
	require.NoError(t, err)
	ch, err := store.WatchIndex(ctx, index, rev+1)
	require.NoError(t, err)

	_, err = store.UpdateEntity(ctx, fixture.Id(), New(String(Doc, "touched")))
	require.NoError(t, err)

	var event *clientv3.Event
	select {
	case resp := <-ch:
		require.Len(t, resp.Events, 1)
		event = resp.Events[0]
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the fixture's update event")
	}

	assert.True(t, event.IsModify(), "a write to a fixture must read as a modify, not a create")
	assert.Equal(t, int64(7), event.Kv.CreateRevision, "the entry dates from the fixture's own revision")
	assert.Greater(t, event.Kv.ModRevision, int64(7))
}
