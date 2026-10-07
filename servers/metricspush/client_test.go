package metricspush

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A runner asks once and reuses the answer, so starting a burst of sandboxes
// costs one round trip to the coordinator rather than one each.
func TestClientAvailabilityIsCached(t *testing.T) {
	status, calls := http.StatusNoContent, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	now := time.Unix(1_700_000_000, 0)
	c := &Client{http: srv.Client(), url: srv.URL + IngestPath, now: func() time.Time { return now }}

	require.True(t, c.Available(t.Context()))
	status = http.StatusServiceUnavailable
	require.True(t, c.Available(t.Context()), "a fresh answer is reused")
	require.Equal(t, 1, calls)

	now = now.Add(availabilityTTL)
	require.False(t, c.Available(t.Context()), "a stale answer is asked again")
	require.Equal(t, 2, calls)
}

// A check that gets no answer is retried soon, since every sandbox started
// while it stands goes without push for good.
func TestClientRetriesUnreachableSooner(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	url := srv.URL + IngestPath
	srv.Close() // unreachable until we say otherwise

	now := time.Unix(1_700_000_000, 0)
	c := &Client{http: &http.Client{}, url: url, now: func() time.Time { return now }}
	require.False(t, c.Available(t.Context()))

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c.url = srv.URL + IngestPath

	now = now.Add(unreachableTTL)
	require.True(t, c.Available(t.Context()), "an unanswered check is not held for a full minute")
}
