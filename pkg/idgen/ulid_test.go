package idgen

import (
	"crypto/rand"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestULIDIsMonotonicWithinAMillisecond(t *testing.T) {
	// Pin the millisecond so every mint takes the increment path, rather
	// than trusting the loop to be fast enough to share one.
	ulidMu.Lock()
	defer ulidMu.Unlock()
	ms := ulid.Now()
	prev := ulidAt(ms)
	for i := 0; i < 1000; i++ {
		id := ulidAt(ms)
		require.Greater(t, id, prev)
		prev = id
	}
}

func TestULIDHoldsOrderWhenTheClockStepsBack(t *testing.T) {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	ms := ulid.Now()
	prev := ulidAt(ms)
	id := ulidAt(ms - 5)
	require.Greater(t, id, prev)
	require.Equal(t, ms, ulid.MustParse(id).Time())
}

func TestULIDSurvivesEntropyOverflow(t *testing.T) {
	ulidMu.Lock()
	defer ulidMu.Unlock()
	ms := ulid.Now()
	prev := ulidAt(ms)
	ulidEntropy = ulid.Monotonic(&ceilingEntropy{}, 0)
	t.Cleanup(func() { ulidEntropy = ulid.Monotonic(rand.Reader, 0) })
	first := ulidAt(ms) // fresh read: lands on the ceiling
	require.Greater(t, first, prev)
	id := ulidAt(ms) // increment overflows
	require.Greater(t, id, first)
	require.Equal(t, ms+1, ulid.MustParse(id).Time())
	// A caller whose clock still reads the old millisecond stays ordered.
	next := ulidAt(ms)
	require.Greater(t, next, id)
	require.Equal(t, ms+1, ulid.MustParse(next).Time())
}

// ceilingEntropy yields all-ones for the first 10 bytes, so the first
// monotonic increment overflows, then real randomness so the library's
// rejection-sampled increments terminate.
type ceilingEntropy struct{ n int }

func (c *ceilingEntropy) Read(p []byte) (int, error) {
	n, err := rand.Read(p)
	for i := 0; i < n && c.n < 10; i, c.n = i+1, c.n+1 {
		p[i] = 0xff
	}
	return n, err
}
