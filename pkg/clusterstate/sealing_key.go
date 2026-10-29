package clusterstate

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"

	clientv3 "go.etcd.io/etcd/client/v3"
	"miren.dev/runtime/pkg/atomicfile"
)

// SealingKeyPath is the host-only root key. Back it up separately from etcd;
// it is not a recoverable cache and must never be uploaded with snapshots.
func SealingKeyPath(dataPath string) string {
	return filepath.Join(dataPath, "server", "cluster-sealing.key")
}

// EnsureSealingKey creates a key only before metadata migration. A restored
// snapshot without its original sealing key must fail rather than mint a new
// root that cannot decrypt the cluster's credentials.
func EnsureSealingKey(ctx context.Context, log *slog.Logger, client *clientv3.Client, prefix, dataPath string) ([]byte, error) {
	filename := SealingKeyPath(dataPath)
	key, err := os.ReadFile(filename)
	if err == nil {
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid cluster sealing key at %s: expected 32 bytes", filename)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, stored := range []string{path.Join(prefix, "cluster-state") + "/", path.Join(prefix, "secret-keyring")} {
		result, err := client.Get(ctx, stored, clientv3.WithPrefix(), clientv3.WithLimit(1))
		if err != nil {
			return nil, err
		}
		if len(result.Kvs) != 0 {
			return nil, fmt.Errorf("missing cluster sealing key at %s: restore the original key from its separate backup", filename)
		}
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return nil, err
	}
	if err := atomicfile.Write(filename, key, 0600); err != nil {
		return nil, err
	}
	log.Info("generated cluster sealing key; back it up separately, etcd snapshots cannot be restored without it", "path", filename)
	return key, nil
}
