package commands

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

// The compatibility fallback hinges on one distinction: a server that answered
// and does not offer app-runs (fall back to legacy exec) versus a server that
// is unreachable, wedged, or refusing us (surface the real error). Falling back
// on the wrong one would either retry an old protocol against a healthy current
// server or hide a transport failure behind a misleading legacy attempt.
func TestServerPredatesRunsOnlyForLookupFailures(t *testing.T) {
	const cap = "dev.miren.runtime/app-runs"
	const remote = "prod:8443"

	t.Run("capability absent falls back", func(t *testing.T) {
		err := rpc.NewResolveLookupError(cap, remote, "unknown object: "+cap)
		assert.True(t, serverPredatesRuns(err))
	})

	t.Run("a diagnostic-wrapped lookup error still falls back", func(t *testing.T) {
		// runsClient returns the error wrapped by RPCClient in a *ui.Diagnostic;
		// errors.Is must still reach the ResolveError through the wrap.
		lookup := rpc.NewResolveLookupError(cap, remote, "unknown object: "+cap)
		wrapped := &ui.Diagnostic{Summary: "old cluster", Cause: lookup}
		assert.True(t, serverPredatesRuns(wrapped))
	})

	notOldServer := []struct {
		name string
		err  error
	}{
		{"unreachable", rpc.NewResolveUnreachableError(cap, remote, 5*time.Second, errors.New("dial"))},
		{"went silent", rpc.NewResolveWentSilentError(cap, remote, 30*time.Second, errors.New("reset"))},
		{"no answer", rpc.NewResolveNoAnswerError(cap, remote, 8*time.Second, errors.New("timeout"))},
		{"unauthorized", rpc.NewResolveStatusError(cap, remote, 401)},
		{"unrelated error", errors.New("boom")},
	}
	for _, tc := range notOldServer {
		t.Run(tc.name+" does not fall back", func(t *testing.T) {
			assert.False(t, serverPredatesRuns(tc.err),
				"only a lookup failure means an old server; this must surface as-is")
		})
	}
}
