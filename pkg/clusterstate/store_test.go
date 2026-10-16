package clusterstate

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/client/v3/concurrency"
	"miren.dev/runtime/pkg/etcdtest"
)

func TestSealingKeyCreationAndRecovery(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	dir := t.TempDir()
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	key, err := EnsureSealingKey(t.Context(), log, client, prefix, dir)
	require.NoError(t, err)
	require.Len(t, key, 32)
	require.Contains(t, logs.String(), "level=INFO")
	require.Contains(t, logs.String(), SealingKeyPath(dir))
	require.Contains(t, logs.String(), "back it up separately, etcd snapshots cannot be restored without it")
	info, err := os.Stat(SealingKeyPath(dir))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	store, err := New(client, prefix, key)
	require.NoError(t, err)
	require.NoError(t, store.Put(t.Context(), "authority", []byte("credential")))
	again, err := EnsureSealingKey(t.Context(), log, client, prefix, dir)
	require.NoError(t, err)
	require.Equal(t, key, again)
	require.NoError(t, os.WriteFile(SealingKeyPath(dir), key[:31], 0600))
	_, err = EnsureSealingKey(t.Context(), log, client, prefix, dir)
	require.ErrorContains(t, err, "expected 32 bytes")
	require.NoError(t, os.Remove(SealingKeyPath(dir)))
	_, err = EnsureSealingKey(t.Context(), log, client, prefix, dir)
	require.ErrorContains(t, err, "restore the original key")
	require.NoFileExists(t, SealingKeyPath(dir))
	require.Equal(t, 1, strings.Count(logs.String(), "generated cluster sealing key"))
}

func TestEncryptedAndWrongKey(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := New(client, prefix, []byte("authority-one"))
	require.NoError(t, err)
	require.NoError(t, store.Put(t.Context(), "secret", []byte("plaintext-secret")))
	raw, err := client.Get(t.Context(), prefix+"/cluster-state/records/secret")
	require.NoError(t, err)
	require.Len(t, raw.Kvs, 1)
	require.False(t, bytes.Contains(raw.Kvs[0].Value, []byte("plaintext-secret")))

	wrong, err := New(client, prefix, []byte("authority-two"))
	require.NoError(t, err)
	_, err = wrong.Get(t.Context(), "secret")
	require.ErrorContains(t, err, "decrypting cluster state")
}

func TestEnsureInitialRace(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := New(client, prefix, []byte("authority"))
	require.NoError(t, err)
	const contenders = 16
	values := make(chan string, contenders)
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(value byte) {
			defer wg.Done()
			got, err := store.Ensure(t.Context(), "initial", []byte{value})
			require.NoError(t, err)
			values <- string(got)
		}(byte(i))
	}
	wg.Wait()
	close(values)
	var authoritative string
	for value := range values {
		if authoritative == "" {
			authoritative = value
		}
		require.Equal(t, authoritative, value)
	}
}

func TestStaleLockCannotWriteOrDelete(t *testing.T) {
	client, prefix := etcdtest.TestEtcdClient(t)
	store, err := New(client, prefix, []byte("authority"))
	require.NoError(t, err)
	stale, err := store.lock(t.Context(), "mutation", 1)
	require.NoError(t, err)
	require.NoError(t, stale.Put(t.Context(), "value", []byte("old")))
	require.NoError(t, stale.session.Close())
	select {
	case <-stale.session.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("session did not expire")
	}

	fresh, err := store.Lock(t.Context(), "mutation")
	require.NoError(t, err)
	defer fresh.Unlock()
	require.NoError(t, fresh.Put(t.Context(), "value", []byte("new")))
	require.ErrorIs(t, stale.Put(t.Context(), "value", []byte("stale")), concurrency.ErrSessionExpired)
	require.ErrorIs(t, stale.Delete(t.Context(), "value"), concurrency.ErrSessionExpired)
	got, err := store.Get(t.Context(), "value")
	require.NoError(t, err)
	require.Equal(t, "new", string(got))
}
