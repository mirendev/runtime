package keyring

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/etcdtest"
)

func TestSharedRingRotationAndLock(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	dir := t.TempDir()
	local, err := Ensure(slog.New(slog.DiscardHandler), dir)
	require.NoError(t, err)
	sealed, err := local.Seal([]byte("survives failover"))
	require.NoError(t, err)
	a, err := NewSharedStore(client, prefix+"/", dir, []byte("cluster sealing key"))
	require.NoError(t, err)
	b, err := NewSharedStore(client, prefix, t.TempDir(), []byte("cluster sealing key"))
	require.NoError(t, err)
	writer, stopWriter := context.WithCancel(t.Context())
	defer stopWriter()
	ring, unlock, err := a.Lock(writer)
	require.NoError(t, err)
	require.Equal(t, local.CurrentID(), ring.CurrentID(), "migration must preserve the existing local ring")
	rotated, next, err := ring.Rotate()
	require.NoError(t, err)
	require.NoError(t, a.Save(t.Context(), rotated))
	stopWriter()
	blocked, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	_, _, err = a.Lock(blocked)
	cancel()
	require.Error(t, err, "cancellation must not release a paused writer's lock before its writes finish")
	unlock()
	refreshed, release, err := b.Lock(t.Context())
	require.NoError(t, err)
	require.Equal(t, next.ID, refreshed.CurrentID())
	plain, err := refreshed.Open(sealed)
	require.NoError(t, err)
	require.Equal(t, "survives failover", string(plain))
	release()
	stored, err := client.Get(t.Context(), a.key)
	require.NoError(t, err)
	require.Len(t, stored.Kvs, 1)
	for _, k := range rotated.Keys() {
		require.False(t, bytes.Contains(stored.Kvs[0].Value, k.Material))
	}
	wrong, err := NewSharedStore(client, prefix, t.TempDir(), []byte("other cluster"))
	require.NoError(t, err)
	_, _, err = wrong.Lock(t.Context())
	require.ErrorContains(t, err, "decrypting replicated")
}
