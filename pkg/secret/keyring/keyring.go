// Package keyring holds the cluster's key hierarchy for secrets at rest and
// performs the envelope encryption around it.
//
// Each stored version is sealed with its own random data key (DEK), and only
// that DEK is wrapped by a cluster key-encryption key (KEK). Three things fall
// out of the indirection. Rotation stays cheap: rolling a KEK re-wraps the
// handful of bytes in each DEK and never touches the (arbitrarily large)
// ciphertext. The KEK only ever performs small fixed-size wrap and unwrap
// operations, so it can move behind a KMS or HSM boundary later without
// streaming every secret value across it. And a leaked DEK exposes exactly one
// version rather than every secret sharing a key.
//
// The keyring holds up to five KEKs so keys can rotate without a flag day: a
// new KEK is added and becomes what new writes wrap with, existing DEKs are
// re-wrapped off the outgoing key in the background, and the old key retires
// once nothing points at it.
package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// MaxKeys caps how many KEKs the ring holds at once. Rotation needs the
// outgoing key alive alongside the incoming one; five leaves room for several
// overlapping rotations without letting retired keys accumulate forever.
const MaxKeys = 5

// keyLen is the size of a KEK, a DEK, and the MAC key: AES-256 throughout.
const keyLen = 32

var (
	// ErrUnknownKEK means a stored version names a KEK the ring no longer
	// holds, so its DEK cannot be unwrapped. Fail closed: this is data loss,
	// not a reason to try another key.
	ErrUnknownKEK = errors.New("secret was wrapped with a key this cluster no longer holds")

	// ErrNoCurrentKey means the ring holds no key to wrap new writes with.
	ErrNoCurrentKey = errors.New("keyring has no current key")

	// ErrRingFull means a rotation would exceed MaxKeys without first retiring
	// an old key.
	ErrRingFull = fmt.Errorf("keyring already holds the maximum of %d keys", MaxKeys)
)

// Key is one key-encryption key in the ring.
type Key struct {
	// ID identifies the key in a stored version's kek_id, so a resolve knows
	// which key to unwrap with and a rotation knows which rows still point at a
	// retiring key.
	ID string

	// Material is the raw AES-256 key.
	Material []byte
}

// Keyring is the cluster's set of KEKs. The current key is what new writes wrap
// with; the rest are retained so already-stored versions stay resolvable while
// they are re-wrapped.
//
// A Keyring is immutable once built. Rotation produces a new ring rather than
// mutating one in place, so a concurrent resolve never observes a half-rotated
// ring.
type Keyring struct {
	keys    []Key
	current string
}

// New builds a keyring from an ordered set of keys, treating currentID as the
// key new writes wrap with.
func New(keys []Key, currentID string) (*Keyring, error) {
	if len(keys) == 0 {
		return nil, ErrNoCurrentKey
	}
	if len(keys) > MaxKeys {
		return nil, ErrRingFull
	}

	seen := make(map[string]bool, len(keys))
	foundCurrent := false
	for _, k := range keys {
		if k.ID == "" {
			return nil, errors.New("keyring key has no id")
		}
		if seen[k.ID] {
			return nil, fmt.Errorf("keyring has duplicate key id %q", k.ID)
		}
		seen[k.ID] = true

		if len(k.Material) != keyLen {
			// Never quote the material itself, even in an error.
			return nil, fmt.Errorf("keyring key %q is %d bytes, want %d", k.ID, len(k.Material), keyLen)
		}
		if k.ID == currentID {
			foundCurrent = true
		}
	}
	if !foundCurrent {
		return nil, fmt.Errorf("keyring current key %q is not in the ring", currentID)
	}

	return &Keyring{keys: keys, current: currentID}, nil
}

// Generate builds a keyring holding a single fresh key. Used the first time a
// cluster stores a secret.
func Generate() (*Keyring, error) {
	key, err := generateKey()
	if err != nil {
		return nil, err
	}
	return New([]Key{key}, key.ID)
}

// CurrentID returns the id of the key new writes wrap with.
func (r *Keyring) CurrentID() string { return r.current }

// Keys returns the ring's keys. The slice is a copy, but the key material is
// shared — callers must not mutate it.
func (r *Keyring) Keys() []Key {
	out := make([]Key, len(r.keys))
	copy(out, r.keys)
	return out
}

// key looks up a KEK by id.
func (r *Keyring) key(id string) ([]byte, error) {
	for _, k := range r.keys {
		if k.ID == id {
			return k.Material, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownKEK, id)
}

// Sealed is the stored form of one secret version's payload.
type Sealed struct {
	// Ciphertext is the value encrypted under a per-version DEK.
	Ciphertext []byte

	// WrappedDEK is that DEK encrypted under the KEK named by KEKID.
	WrappedDEK []byte

	// KEKID names which KEK wrapped the DEK.
	KEKID string

	// ValueMAC is a keyed hash of the plaintext, so a later write can recognize
	// an identical value without decrypting anything. See MAC.
	ValueMAC string
}

// Seal encrypts a value for storage: a fresh DEK per version, the value sealed
// under it, and the DEK wrapped by the current KEK.
func (r *Keyring) Seal(value []byte) (Sealed, error) {
	kekID := r.current
	kek, err := r.key(kekID)
	if err != nil {
		return Sealed{}, err
	}

	dek := make([]byte, keyLen)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return Sealed{}, fmt.Errorf("generating data key: %w", err)
	}

	ciphertext, err := encrypt(dek, value)
	if err != nil {
		return Sealed{}, fmt.Errorf("sealing value: %w", err)
	}

	wrapped, err := encrypt(kek, dek)
	if err != nil {
		return Sealed{}, fmt.Errorf("wrapping data key: %w", err)
	}

	mac, err := r.MAC(value)
	if err != nil {
		return Sealed{}, err
	}

	return Sealed{
		Ciphertext: ciphertext,
		WrappedDEK: wrapped,
		KEKID:      kekID,
		ValueMAC:   mac,
	}, nil
}

// Open reverses Seal, returning the plaintext in memory.
func (r *Keyring) Open(s Sealed) ([]byte, error) {
	kek, err := r.key(s.KEKID)
	if err != nil {
		return nil, err
	}

	dek, err := decrypt(kek, s.WrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("unwrapping data key: %w", err)
	}

	value, err := decrypt(dek, s.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("opening value: %w", err)
	}
	return value, nil
}

// MAC returns a keyed hash of a plaintext under the current KEK, used to
// recognize that a value is already stored without decrypting anything.
//
// It is keyed rather than a bare digest on purpose. A stored SHA-256 of the
// plaintext would be a precomputable oracle: an attacker who reads the store
// could grind a rainbow table against short or low-entropy secrets without ever
// touching the ciphertext. Under a key they do not hold, the digest tells them
// nothing.
//
// Because the MAC is keyed by the current KEK, a value's MAC changes when keys
// rotate. That only costs a redundant version on the first write after a
// rotation — never correctness.
func (r *Keyring) MAC(value []byte) (string, error) {
	kek, err := r.key(r.current)
	if err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, kek)
	mac.Write(value)
	return r.current + ":" + base64.RawStdEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Rotate returns a new ring with a freshly generated key installed as current,
// retaining the existing keys so already-stored versions stay resolvable while
// they are re-wrapped. Retire the outgoing key with Retire once nothing points
// at it.
func (r *Keyring) Rotate() (*Keyring, Key, error) {
	if len(r.keys) >= MaxKeys {
		return nil, Key{}, ErrRingFull
	}

	key, err := generateKey()
	if err != nil {
		return nil, Key{}, err
	}

	keys := make([]Key, 0, len(r.keys)+1)
	keys = append(keys, r.keys...)
	keys = append(keys, key)

	ring, err := New(keys, key.ID)
	if err != nil {
		return nil, Key{}, err
	}
	return ring, key, nil
}

// Rewrap moves a sealed payload onto the current KEK without decrypting the
// value: it unwraps the DEK under its old key and re-wraps it under the new
// one, so a rotation touches wrapped_dek and leaves ciphertext untouched.
//
// The value's MAC is recomputed under the current key so that duplicate
// detection keeps working after a rotation. That is the one place a rewrap
// needs the plaintext's MAC, and it is derived from the DEK-decrypted value
// held only in memory.
func (r *Keyring) Rewrap(s Sealed) (Sealed, error) {
	if s.KEKID == r.current {
		return s, nil
	}

	oldKEK, err := r.key(s.KEKID)
	if err != nil {
		return Sealed{}, err
	}

	dek, err := decrypt(oldKEK, s.WrappedDEK)
	if err != nil {
		return Sealed{}, fmt.Errorf("unwrapping data key: %w", err)
	}

	newKEK, err := r.key(r.current)
	if err != nil {
		return Sealed{}, err
	}

	wrapped, err := encrypt(newKEK, dek)
	if err != nil {
		return Sealed{}, fmt.Errorf("re-wrapping data key: %w", err)
	}

	value, err := decrypt(dek, s.Ciphertext)
	if err != nil {
		return Sealed{}, fmt.Errorf("opening value: %w", err)
	}
	mac, err := r.MAC(value)
	if err != nil {
		return Sealed{}, err
	}

	return Sealed{
		Ciphertext: s.Ciphertext,
		WrappedDEK: wrapped,
		KEKID:      r.current,
		ValueMAC:   mac,
	}, nil
}

// Retire drops a key from the ring. It refuses to retire the current key, and
// the caller is responsible for having re-wrapped everything that pointed at
// it — anything still naming a retired key becomes unresolvable.
func (r *Keyring) Retire(id string) (*Keyring, error) {
	if id == r.current {
		return nil, fmt.Errorf("cannot retire the current key %q", id)
	}

	keys := make([]Key, 0, len(r.keys))
	found := false
	for _, k := range r.keys {
		if k.ID == id {
			found = true
			continue
		}
		keys = append(keys, k)
	}
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKEK, id)
	}

	return New(keys, r.current)
}

// generateKey mints a KEK with a random id and random material.
func generateKey() (Key, error) {
	material := make([]byte, keyLen)
	if _, err := io.ReadFull(rand.Reader, material); err != nil {
		return Key{}, fmt.Errorf("generating key material: %w", err)
	}

	idBytes := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, idBytes); err != nil {
		return Key{}, fmt.Errorf("generating key id: %w", err)
	}

	return Key{
		ID:       base64.RawURLEncoding.EncodeToString(idBytes),
		Material: material,
	}, nil
}

// encrypt seals plaintext with AES-256-GCM, prefixing the random nonce.
func encrypt(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt reverses encrypt. Its error never quotes the ciphertext or the key.
func decrypt(key, sealed []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}

	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("ciphertext is too short to hold a nonce")
	}

	nonce, body := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, errors.New("ciphertext failed authentication")
	}
	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
