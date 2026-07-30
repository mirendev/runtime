package commands

import (
	"errors"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

func diagnosticContext() *Context {
	return &Context{ClusterName: "local", CommandName: "route list"}
}

// Each kind has to produce its own diagnosis. The bug this replaces collapsed
// every transport failure into one message, so the regression to guard is two
// different failures rendering the same text.
func TestDiagnoseResolveDistinguishesKinds(t *testing.T) {
	c := diagnosticContext()

	tests := []struct {
		name        string
		err         error
		wantSummary string
		wantDetail  []string
	}{
		{
			name:        "unreachable",
			err:         rpc.NewResolveUnreachableError("entities", "localhost:8443", 5*time.Second, errors.New("timeout")),
			wantSummary: `couldn't reach cluster "local" at localhost:8443`,
			wantDetail:  []string{"Nothing answered after 5s"},
		},
		{
			name:        "went silent",
			err:         rpc.NewResolveWentSilentError("entities", "localhost:8443", 30*time.Second, errors.New("timeout")),
			wantSummary: `lost the connection to cluster "local" at localhost:8443`,
			wantDetail:  []string{"Connected successfully"},
		},
		{
			name:        "no answer",
			err:         rpc.NewResolveNoAnswerError("entities", "localhost:8443", 8*time.Second, errors.New("timeout")),
			wantSummary: `cluster "local" never answered`,
			wantDetail:  []string{"Waited 8s", "still open"},
		},
		{
			// The whole point of plumbing CommandName down: the user typed
			// "miren route list", not "entities".
			name:        "capability missing names the command, not the capability",
			err:         rpc.NewResolveLookupError("entities", "localhost:8443", "unknown object: entities"),
			wantSummary: `cluster "local" doesn't provide what "miren route list" needs`,
			wantDetail:  []string{"isn't currently offering"},
		},
		{
			name:        "unauthorized",
			err:         rpc.NewResolveStatusError("entities", "localhost:8443", 401),
			wantSummary: `access denied on cluster "local"`,
			wantDetail:  []string{"permission"},
		},
		{
			name:        "unexpected status",
			err:         rpc.NewResolveStatusError("entities", "localhost:8443", 404),
			wantSummary: `cluster "local" returned an unexpected response (HTTP 404)`,
			wantDetail:  []string{"isn't a miren server"},
		},
		{
			name:        "undecodable body",
			err:         rpc.NewResolveDecodeError("entities", "localhost:8443", errors.New("cbor: bad")),
			wantSummary: `couldn't understand the response from cluster "local"`,
			wantDetail:  []string{"expected format"},
		},
	}

	seen := map[string]string{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := c.wrapRPCError(tt.err)

			var d *ui.Diagnostic
			if !errors.As(wrapped, &d) {
				t.Fatalf("wrapRPCError returned %T, want *ui.Diagnostic", wrapped)
			}
			if d.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", d.Summary, tt.wantSummary)
			}
			for _, want := range tt.wantDetail {
				if !strings.Contains(d.Detail, want) {
					t.Errorf("Detail %q does not mention %q", d.Detail, want)
				}
			}
			if prev, dup := seen[d.Summary]; dup {
				t.Errorf("%s renders the same summary as %s", tt.name, prev)
			}
			seen[d.Summary] = tt.name

			// Every diagnosis must leave the structured error reachable, since
			// doctor re-classifies failures handed to it.
			var re *rpc.ResolveError
			if !errors.As(wrapped, &re) {
				t.Error("the underlying rpc.ResolveError is no longer reachable")
			}
		})
	}
}

// The 401 sentinel predates this change and is exported, so matching on it has
// to keep working through the diagnostic wrapper.
func TestUnauthorizedStillMatchesSentinel(t *testing.T) {
	c := diagnosticContext()
	wrapped := c.wrapRPCError(rpc.NewResolveStatusError("entities", "localhost:8443", 401))

	if !errors.Is(wrapped, ErrAccessDenied) {
		t.Error("errors.Is(err, ErrAccessDenied) no longer holds")
	}
}

// Without a command name (library callers, tests) the message should still be
// intelligible rather than containing an empty pair of quotes.
func TestDiagnoseResolveFallsBackToCapabilityName(t *testing.T) {
	c := &Context{ClusterName: "local"}
	wrapped := c.wrapRPCError(rpc.NewResolveLookupError("entities", "localhost:8443", "unknown object: entities"))

	var d *ui.Diagnostic
	if !errors.As(wrapped, &d) {
		t.Fatalf("wrapRPCError returned %T, want *ui.Diagnostic", wrapped)
	}
	if !strings.Contains(d.Summary, `"entities"`) {
		t.Errorf("Summary = %q, want it to name the capability", d.Summary)
	}
}

// An unnamed cluster shouldn't produce 'cluster ""'.
func TestDiagnoseResolveHandlesUnnamedCluster(t *testing.T) {
	c := &Context{CommandName: "route list"}
	wrapped := c.wrapRPCError(rpc.NewResolveUnreachableError("entities", "localhost:8443", time.Second, errors.New("timeout")))

	var d *ui.Diagnostic
	if !errors.As(wrapped, &d) {
		t.Fatalf("wrapRPCError returned %T, want *ui.Diagnostic", wrapped)
	}
	if strings.Contains(d.Summary, `cluster ""`) {
		t.Errorf("Summary = %q, want a readable fallback", d.Summary)
	}
	if !strings.Contains(d.Summary, "the cluster") {
		t.Errorf("Summary = %q, want it to fall back to 'the cluster'", d.Summary)
	}
}

// Unclassified transport failures should pass through untouched rather than
// being dressed up in a diagnosis we can't actually support.
func TestUnclassifiedTransportErrorPassesThrough(t *testing.T) {
	c := diagnosticContext()
	original := rpc.NewResolveHTTPError(errors.New("connection reset"), "error performing http request: %v", "connection reset")

	if got := c.wrapRPCError(original); got != original {
		t.Errorf("wrapRPCError modified an unclassified error: %v", got)
	}
}

// Non-RPC errors must pass through completely untouched.
func TestNonResolveErrorsPassThrough(t *testing.T) {
	c := diagnosticContext()
	original := errors.New("something else entirely")

	if got := c.wrapRPCError(original); got != original {
		t.Errorf("wrapRPCError modified an unrelated error: %v", got)
	}
}
