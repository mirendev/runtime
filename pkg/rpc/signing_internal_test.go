package rpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// freshTimestamp bounds replay in both directions: an old stamp is stale, and a
// far-future stamp can't silently extend the window.
func TestFreshTimestamp(t *testing.T) {
	r := require.New(t)

	now := time.Now()

	r.NoError(freshTimestamp(now.Format(time.RFC3339Nano)))
	r.NoError(freshTimestamp(now.Add(-authFreshness / 2).Format(time.RFC3339Nano)))
	r.NoError(freshTimestamp(now.Add(authFreshness / 2).Format(time.RFC3339Nano)))

	r.Error(freshTimestamp(now.Add(-2 * authFreshness).Format(time.RFC3339Nano)))
	r.Error(freshTimestamp(now.Add(2 * authFreshness).Format(time.RFC3339Nano)))
	r.Error(freshTimestamp("not-a-timestamp"))
}
