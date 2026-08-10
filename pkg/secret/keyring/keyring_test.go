package keyring

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSealOpenRoundTrip(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	value := []byte("sk_live_abc123")
	sealed, err := ring.Seal(value)
	require.NoError(t, err)

	got, err := ring.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, value, got)
}

// Nothing recognizable about the plaintext may survive into what gets stored.
func TestSealedPayloadDoesNotContainPlaintext(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	value := []byte("sk_live_abc123")
	sealed, err := ring.Seal(value)
	require.NoError(t, err)

	assert.False(t, bytes.Contains(sealed.Ciphertext, value))
	assert.False(t, bytes.Contains(sealed.WrappedDEK, value))
	assert.NotContains(t, sealed.ValueMAC, string(value))
}

// Each version gets its own DEK, so a leaked DEK exposes one version rather
// than every secret sharing a key.
func TestSealUsesAFreshDataKeyPerVersion(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	value := []byte("same value both times")
	first, err := ring.Seal(value)
	require.NoError(t, err)
	second, err := ring.Seal(value)
	require.NoError(t, err)

	assert.NotEqual(t, first.WrappedDEK, second.WrappedDEK)
	assert.NotEqual(t, first.Ciphertext, second.Ciphertext)

	// The MAC is the one part that must match, since it is what lets a write
	// recognize an identical existing value without decrypting.
	assert.Equal(t, first.ValueMAC, second.ValueMAC)
}

func TestMACIsStableAndValueDependent(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	a, err := ring.MAC([]byte("value"))
	require.NoError(t, err)
	b, err := ring.MAC([]byte("value"))
	require.NoError(t, err)
	c, err := ring.MAC([]byte("other"))
	require.NoError(t, err)

	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
}

// Keyed, not a bare digest: two clusters holding the same value must not
// produce the same MAC, or the stored digest becomes a precomputable oracle.
func TestMACIsKeyedNotABareDigest(t *testing.T) {
	first, err := Generate()
	require.NoError(t, err)
	second, err := Generate()
	require.NoError(t, err)

	a, err := first.MAC([]byte("value"))
	require.NoError(t, err)
	b, err := second.MAC([]byte("value"))
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	sealed, err := ring.Seal([]byte("sk_live"))
	require.NoError(t, err)
	sealed.Ciphertext[len(sealed.Ciphertext)-1] ^= 0xff

	_, err = ring.Open(sealed)
	assert.Error(t, err)
}

func TestOpenFailsClosedOnUnknownKEK(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	sealed, err := ring.Seal([]byte("sk_live"))
	require.NoError(t, err)
	sealed.KEKID = "not-a-key"

	_, err = ring.Open(sealed)
	assert.ErrorIs(t, err, ErrUnknownKEK)
}

// A rotation must leave already-stored versions readable: the outgoing key
// stays in the ring until its rows have been re-wrapped.
func TestRotateKeepsExistingVersionsResolvable(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)
	oldID := ring.CurrentID()

	value := []byte("sk_live")
	sealed, err := ring.Seal(value)
	require.NoError(t, err)

	rotated, newKey, err := ring.Rotate()
	require.NoError(t, err)
	assert.NotEqual(t, oldID, rotated.CurrentID())
	assert.Equal(t, newKey.ID, rotated.CurrentID())

	got, err := rotated.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, value, got)
}

// The whole point of the envelope: a rotation re-wraps the DEK and leaves the
// (arbitrarily large) ciphertext untouched.
func TestRewrapMovesTheDEKAndLeavesCiphertextAlone(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	value := []byte("sk_live")
	sealed, err := ring.Seal(value)
	require.NoError(t, err)

	rotated, _, err := ring.Rotate()
	require.NoError(t, err)

	rewrapped, err := rotated.Rewrap(sealed)
	require.NoError(t, err)

	assert.Equal(t, sealed.Ciphertext, rewrapped.Ciphertext)
	assert.NotEqual(t, sealed.WrappedDEK, rewrapped.WrappedDEK)
	assert.Equal(t, rotated.CurrentID(), rewrapped.KEKID)

	got, err := rotated.Open(rewrapped)
	require.NoError(t, err)
	assert.Equal(t, value, got)
}

// Duplicate detection has to keep working after a rotation, which means the
// re-wrap recomputes the MAC under the new current key.
func TestRewrapRefreshesTheMAC(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	value := []byte("sk_live")
	sealed, err := ring.Seal(value)
	require.NoError(t, err)

	rotated, _, err := ring.Rotate()
	require.NoError(t, err)

	rewrapped, err := rotated.Rewrap(sealed)
	require.NoError(t, err)

	fresh, err := rotated.MAC(value)
	require.NoError(t, err)
	assert.Equal(t, fresh, rewrapped.ValueMAC)
}

func TestRewrapIsANoOpOnTheCurrentKey(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	sealed, err := ring.Seal([]byte("sk_live"))
	require.NoError(t, err)

	rewrapped, err := ring.Rewrap(sealed)
	require.NoError(t, err)
	assert.Equal(t, sealed, rewrapped)
}

// A version wrapped by a key that has been retired is unrecoverable, and must
// say so rather than appearing to succeed with different bytes.
func TestRetireMakesOldVersionsFailClosed(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)
	oldID := ring.CurrentID()

	sealed, err := ring.Seal([]byte("sk_live"))
	require.NoError(t, err)

	rotated, _, err := ring.Rotate()
	require.NoError(t, err)

	retired, err := rotated.Retire(oldID)
	require.NoError(t, err)

	_, err = retired.Open(sealed)
	assert.ErrorIs(t, err, ErrUnknownKEK)
}

func TestRetireRefusesTheCurrentKey(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	_, err = ring.Retire(ring.CurrentID())
	assert.Error(t, err)
}

func TestRotateStopsAtMaxKeys(t *testing.T) {
	ring, err := Generate()
	require.NoError(t, err)

	for i := 1; i < MaxKeys; i++ {
		ring, _, err = ring.Rotate()
		require.NoError(t, err)
	}
	assert.Len(t, ring.Keys(), MaxKeys)

	_, _, err = ring.Rotate()
	assert.ErrorIs(t, err, ErrRingFull)
}

func TestNewRejectsMalformedRings(t *testing.T) {
	good, err := generateKey()
	require.NoError(t, err)

	t.Run("no keys", func(t *testing.T) {
		_, err := New(nil, "x")
		assert.ErrorIs(t, err, ErrNoCurrentKey)
	})

	t.Run("current not in ring", func(t *testing.T) {
		_, err := New([]Key{good}, "someone-else")
		assert.Error(t, err)
	})

	t.Run("duplicate ids", func(t *testing.T) {
		_, err := New([]Key{good, good}, good.ID)
		assert.Error(t, err)
	})

	t.Run("wrong key length", func(t *testing.T) {
		_, err := New([]Key{{ID: "short", Material: []byte("too short")}}, "short")
		assert.Error(t, err)
	})
}
