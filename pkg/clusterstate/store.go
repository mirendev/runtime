// Package clusterstate stores the small, secret-bearing pieces of coordinator
// state needed to restore a coordinator from an etcd snapshot.
package clusterstate

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"path"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

const formatVersion byte = 1

var ErrNotFound = errors.New("cluster state not found")

// Store encrypts records before placing them in replicated etcd. The key is
// derived from a host-only sealing key, which must be backed up separately.
type Store struct {
	client *clientv3.Client
	prefix string
	aead   cipher.AEAD
}

// Lock holds a cluster-wide mutex. Writes made through it are fenced by the
// mutex's create revision and lease, so a holder which pauses past lease expiry
// cannot overwrite the next holder's work.
type Lock struct {
	store   *Store
	session *concurrency.Session
	mutex   *concurrency.Mutex
}

func New(client *clientv3.Client, prefix string, sealingKey []byte) (*Store, error) {
	if client == nil {
		return nil, errors.New("nil etcd client")
	}
	key := sha256.Sum256(append([]byte("miren cluster state v1\x00"), sealingKey...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{client: client, prefix: path.Join(prefix, "cluster-state"), aead: aead}, nil
}

func (s *Store) recordKey(name string) (string, error) {
	if name == "" || path.Clean(name) != name || name[0] == '/' || name == "." || name == ".." {
		return "", fmt.Errorf("invalid cluster state name %q", name)
	}
	return path.Join(s.prefix, "records", name), nil
}

func (s *Store) seal(key string, value []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	result := make([]byte, 1+4)
	result[0] = formatVersion
	binary.BigEndian.PutUint32(result[1:], uint32(len(nonce)))
	result = append(result, nonce...)
	return s.aead.Seal(result, nonce, value, []byte(key)), nil
}

func (s *Store) open(key string, value []byte) ([]byte, error) {
	if len(value) < 5 || value[0] != formatVersion {
		return nil, errors.New("unsupported cluster state record")
	}
	n := int(binary.BigEndian.Uint32(value[1:5]))
	if n != s.aead.NonceSize() || len(value) < 5+n {
		return nil, errors.New("truncated cluster state record")
	}
	plain, err := s.aead.Open(nil, value[5:5+n], value[5+n:], []byte(key))
	if err != nil {
		return nil, fmt.Errorf("decrypting cluster state: %w", err)
	}
	return plain, nil
}

func (s *Store) Get(ctx context.Context, name string) ([]byte, error) {
	key, err := s.recordKey(name)
	if err != nil {
		return nil, err
	}
	res, err := s.client.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(res.Kvs) == 0 {
		return nil, ErrNotFound
	}
	return s.open(key, res.Kvs[0].Value)
}

func (s *Store) Put(ctx context.Context, name string, value []byte) error {
	return s.put(ctx, name, value, nil)
}

func (s *Store) put(ctx context.Context, name string, value []byte, lock *Lock) error {
	key, err := s.recordKey(name)
	if err != nil {
		return err
	}
	sealed, err := s.seal(key, value)
	if err != nil {
		return err
	}
	txn := s.client.Txn(ctx)
	fenced := false
	if lock != nil {
		txn = txn.If(lock.mutex.IsOwner())
		fenced = true
	}
	res, err := txn.Then(clientv3.OpPut(key, string(sealed))).Commit()
	if err == nil && fenced && !res.Succeeded {
		return concurrency.ErrSessionExpired
	}
	return err
}

func (s *Store) Delete(ctx context.Context, name string) error {
	return s.delete(ctx, name, nil)
}

func (s *Store) delete(ctx context.Context, name string, lock *Lock) error {
	key, err := s.recordKey(name)
	if err != nil {
		return err
	}
	txn := s.client.Txn(ctx)
	fenced := false
	if lock != nil {
		txn = txn.If(lock.mutex.IsOwner())
		fenced = true
	}
	res, err := txn.Then(clientv3.OpDelete(key)).Commit()
	if err == nil && fenced && !res.Succeeded {
		return concurrency.ErrSessionExpired
	}
	return err
}

// Ensure returns the authoritative value, atomically installing bootstrap only
// when no coordinator has created the record yet.
func (s *Store) Ensure(ctx context.Context, name string, bootstrap []byte) ([]byte, error) {
	key, err := s.recordKey(name)
	if err != nil {
		return nil, err
	}
	sealed, err := s.seal(key, bootstrap)
	if err != nil {
		return nil, err
	}
	res, err := s.client.Txn(ctx).If(clientv3.Compare(clientv3.CreateRevision(key), "=", 0)).Then(clientv3.OpPut(key, string(sealed))).Else(clientv3.OpGet(key)).Commit()
	if err != nil {
		return nil, err
	}
	if res.Succeeded {
		return append([]byte(nil), bootstrap...), nil
	}
	get := res.Responses[0].GetResponseRange()
	if get == nil || len(get.Kvs) == 0 {
		return nil, ErrNotFound
	}
	return s.open(key, get.Kvs[0].Value)
}

// Lock serializes cluster-wide mutation such as ACME issuance.
func (s *Store) Lock(ctx context.Context, name string) (*Lock, error) {
	return s.lock(ctx, name, 30)
}

func (s *Store) lock(ctx context.Context, name string, ttl int) (*Lock, error) {
	session, err := concurrency.NewSession(s.client, concurrency.WithContext(ctx), concurrency.WithTTL(ttl))
	if err != nil {
		return nil, err
	}
	mutex := concurrency.NewMutex(session, path.Join(s.prefix, "locks", name))
	if err := mutex.Lock(ctx); err != nil {
		_ = session.Close()
		return nil, err
	}
	return &Lock{store: s, session: session, mutex: mutex}, nil
}

func (l *Lock) Put(ctx context.Context, name string, value []byte) error {
	return l.store.put(ctx, name, value, l)
}

func (l *Lock) Delete(ctx context.Context, name string) error {
	return l.store.delete(ctx, name, l)
}

func (l *Lock) Unlock() {
	if l == nil {
		return
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = l.mutex.Unlock(cleanup)
	_ = l.session.Close()
}
