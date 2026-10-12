package keyring

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// SharedStore keeps the authoritative ring in replicated etcd, encrypted with
// cluster authority material. The local file remains a recoverable backup.
// Writers and rotation share its lock so retirement cannot race a write using
// an old key on another server.
type SharedStore struct {
	client        *clientv3.Client
	key, dataPath string
	aead          cipher.AEAD
}

func NewSharedStore(client *clientv3.Client, prefix, dataPath string, authorityKey []byte) (*SharedStore, error) {
	key := sha256.Sum256(authorityKey)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SharedStore{client: client, key: prefix + "/secret-keyring", dataPath: dataPath, aead: aead}, nil
}

// Lock loads the latest ring while holding the cluster-wide secret lock.
// Callers must release it after finishing all ring-dependent writes.
func (s *SharedStore) Lock(ctx context.Context) (*Keyring, func(), error) {
	session, err := concurrency.NewSession(s.client, concurrency.WithContext(ctx), concurrency.WithTTL(30))
	if err != nil {
		return nil, nil, err
	}
	mutex := concurrency.NewMutex(session, s.key+"/lock")
	if err := mutex.Lock(ctx); err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = mutex.Unlock(cleanup)
		_ = session.Close()
	}
	ring, err := s.load(ctx)
	if err != nil {
		release()
		return nil, nil, err
	}
	return ring, release, nil
}

func (s *SharedStore) load(ctx context.Context) (*Keyring, error) {
	response, err := s.client.Get(ctx, s.key)
	if err != nil {
		return nil, err
	}
	if len(response.Kvs) == 0 {
		ring, err := Ensure(slog.Default(), s.dataPath)
		if err != nil {
			return nil, err
		}
		if err := s.Save(ctx, ring); err != nil {
			return nil, err
		}
		return ring, nil
	}
	data := response.Kvs[0].Value
	n := s.aead.NonceSize()
	if len(data) < n {
		return nil, fmt.Errorf("replicated secret keyring is truncated")
	}
	plain, err := s.aead.Open(nil, data[:n], data[n:], []byte(s.key))
	if err != nil {
		return nil, fmt.Errorf("decrypting replicated secret keyring: %w", err)
	}
	ring, err := decode(plain)
	if err != nil {
		return nil, err
	}
	if err := Save(Path(s.dataPath), ring); err != nil {
		return nil, err
	}
	return ring, nil
}

// Save must be called under Lock, before exposing the ring to new writes.
func (s *SharedStore) Save(ctx context.Context, ring *Keyring) error {
	plain, err := encode(ring)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := s.aead.Seal(nonce, nonce, plain, []byte(s.key))
	if _, err := s.client.Put(ctx, s.key, string(sealed)); err != nil {
		return err
	}
	return Save(Path(s.dataPath), ring)
}
