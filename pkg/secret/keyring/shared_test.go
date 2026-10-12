package keyring

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/etcdtest"
)

func TestSharedRingRotationAndLock(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	a, err := NewSharedStore(client, prefix, t.TempDir(), []byte("cluster authority"))
	require.NoError(t, err)
	b, err := NewSharedStore(client, prefix, t.TempDir(), []byte("cluster authority"))
	require.NoError(t, err)
	ring, unlock, err := a.Lock(t.Context())
	require.NoError(t, err)
	sealed, err := ring.Seal([]byte("survives failover"))
	require.NoError(t, err)
	rotated, next, err := ring.Rotate()
	require.NoError(t, err)
	require.NoError(t, a.Save(t.Context(), rotated))
	blocked, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	_, _, err = b.Lock(blocked)
	cancel()
	require.Error(t, err, "another server must not retire a key during a write")
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
