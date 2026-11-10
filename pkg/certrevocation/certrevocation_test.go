package certrevocation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/indexwatch"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

type failingStore struct {
	entity.Store
}

func (failingStore) ListIndexEntitiesPage(context.Context, entity.Attr, string, int64) (*entity.EntityPage, error) {
	return nil, errors.New("etcd unavailable")
}

type undecodableStore struct {
	entity.Store
}

func (undecodableStore) ListIndexEntitiesPage(context.Context, entity.Attr, string, int64) (*entity.EntityPage, error) {
	return &entity.EntityPage{
		Ids:         []entity.Id{"revocation/corrupt"},
		Entities:    []*entity.Entity{nil},
		Undecodable: map[entity.Id]bool{"revocation/corrupt": true},
	}, nil
}

func TestLoad(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()

	_, err := inmem.Client.Create(ctx, "abc", &core_v1alpha.CertificateRevocation{Serial: "abc", RevokedAt: time.Now()})
	require.NoError(t, err)

	t.Run("installs stored revocations", func(t *testing.T) {
		set := rpc.NewCertRevocations()
		require.NoError(t, Load(ctx, testutils.TestLogger(t), inmem.Store, set))
		require.True(t, set.Loaded())
		require.True(t, set.Contains("abc"))
		require.False(t, set.Contains("def"))
	})

	t.Run("a failed read leaves the set unloaded", func(t *testing.T) {
		set := rpc.NewCertRevocations()
		require.Error(t, Load(ctx, testutils.TestLogger(t), failingStore{inmem.Store}, set))
		require.False(t, set.Loaded(), "an unloaded set is what keeps certificates out")
	})

	t.Run("an unreadable record fails the load", func(t *testing.T) {
		set := rpc.NewCertRevocations()
		require.ErrorContains(t, Load(ctx, testutils.TestLogger(t), undecodableStore{inmem.Store}, set), "unreadable")
		require.False(t, set.Loaded())
	})

	t.Run("a record with no usable serial fails the load", func(t *testing.T) {
		_, err := inmem.Client.Create(ctx, "bad", &core_v1alpha.CertificateRevocation{Serial: "not-hex"})
		require.NoError(t, err)

		set := rpc.NewCertRevocations()
		require.ErrorContains(t, Load(ctx, testutils.TestLogger(t), inmem.Store, set), "invalid certificate serial")
		require.False(t, set.Loaded())
	})
}

func runFollow(t *testing.T, set *rpc.CertRevocations, events ...indexwatch.Event) {
	t.Helper()
	updates := make(chan indexwatch.Event, len(events))
	for _, ev := range events {
		updates <- ev
	}
	close(updates)
	follow(t.Context(), testutils.TestLogger(t), updates, set)
}

func revocationEntity(id entity.Id, serial string) *entity.Entity {
	return entity.New(entity.Ident, id, (&core_v1alpha.CertificateRevocation{Serial: serial}).Encode)
}

func TestFollow(t *testing.T) {
	set := rpc.NewCertRevocations()

	a := revocationEntity("revocation/a", "a")
	runFollow(t, set,
		indexwatch.Event{Type: indexwatch.EventSync, Entities: []*entity.Entity{a}},
		indexwatch.Event{Type: indexwatch.EventAdded, Id: "revocation/b", Entity: revocationEntity("revocation/b", "b")},
		indexwatch.Event{Type: indexwatch.EventAdded, Id: "revocation/bad", Entity: revocationEntity("revocation/bad", "not-hex")},
	)

	require.True(t, set.Loaded(), "a runner has no other way to load")
	require.True(t, set.Contains("a"))
	require.True(t, set.Contains("b"))
}

func TestBadFirstSnapshotLeavesSetUnloaded(t *testing.T) {
	bad := revocationEntity("revocation/bad", "not-hex")
	good := revocationEntity("revocation/a", "a")

	t.Run("not yet loaded", func(t *testing.T) {
		set := rpc.NewCertRevocations()
		runFollow(t, set, indexwatch.Event{Type: indexwatch.EventSync, Entities: []*entity.Entity{good, bad}})
		require.False(t, set.Loaded(), "an incomplete list must not open the gate")
		require.True(t, set.Contains("a"))
	})

	t.Run("already loaded", func(t *testing.T) {
		set := rpc.NewCertRevocations()
		set.Load(nil)
		runFollow(t, set, indexwatch.Event{Type: indexwatch.EventSync, Entities: []*entity.Entity{good, bad}})
		require.True(t, set.Loaded())
		require.True(t, set.Contains("a"))
	})
}

// A queued delete can be older than a write-through Revoke of the same serial.
func TestDeleteEventNeverLiftsARevocation(t *testing.T) {
	set := rpc.NewCertRevocations()
	set.Load(nil)

	a := revocationEntity("revocation/a", "a")
	runFollow(t, set,
		indexwatch.Event{Type: indexwatch.EventSync, Entities: []*entity.Entity{a}},
		indexwatch.Event{Type: indexwatch.EventDeleted, Id: a.Id(), Entity: a},
	)

	require.True(t, set.Contains("a"))
}

// A snapshot can predate a revocation the handler already enforced, and it omits
// records that will not decode, so leaving a serial out must not lift it.
func TestSnapshotNeverLiftsARevocation(t *testing.T) {
	set := rpc.NewCertRevocations()
	set.Load([]string{"abc"})
	set.Add("def")

	runFollow(t, set, indexwatch.Event{Type: indexwatch.EventSync})

	require.True(t, set.Contains("abc"))
	require.True(t, set.Contains("def"))
}

// The server must outlive the store: losing the watch changes nothing about
// what it enforces.
func TestFollowEndingKeepsLastKnownSet(t *testing.T) {
	set := rpc.NewCertRevocations()
	set.Load([]string{"abc"})

	runFollow(t, set)

	require.True(t, set.Loaded())
	require.True(t, set.Contains("abc"))
}
