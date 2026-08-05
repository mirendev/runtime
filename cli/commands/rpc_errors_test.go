package commands

import (
	"errors"
	"io"
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

			d, ok := errors.AsType[*ui.Diagnostic](wrapped)
			if !ok {
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
			_, resolveOK := errors.AsType[*rpc.ResolveError](wrapped)
			if !resolveOK {
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

	d, ok := errors.AsType[*ui.Diagnostic](wrapped)
	if !ok {
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

	d, ok := errors.AsType[*ui.Diagnostic](wrapped)
	if !ok {
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

// Naming a cluster we can't load has to be fatal. This used to fall through and
// answer with the *active* cluster's data, so `miren -C typo route list`
// happily printed production routes for a cluster that didn't exist.
func TestRPCClientRefusesUnknownCluster(t *testing.T) {
	c := &Context{
		Stderr:           io.Discard,
		clusterErr:       errors.New(`cluster "typo": failed to connect`),
		requestedCluster: "typo",
	}

	client, err := c.RPCClient("entities")
	if err == nil {
		t.Fatal("expected an error, got a client for a cluster that couldn't be loaded")
	}
	if client != nil {
		t.Error("no client should be returned")
	}

	d, ok := errors.AsType[*ui.Diagnostic](err)
	if !ok {
		t.Fatalf("got %T, want *ui.Diagnostic", err)
	}
	if !strings.Contains(d.Summary, "typo") {
		t.Errorf("Summary = %q, want it to name the requested cluster", d.Summary)
	}
	if !strings.Contains(d.Detail, "wrong cluster") {
		t.Errorf("Detail = %q, want it to explain why we didn't fall back", d.Detail)
	}
}

// The same protection applies when the broken name came from the config's
// active cluster rather than from -C.
func TestRPCClientRefusesBrokenActiveCluster(t *testing.T) {
	c := &Context{
		Stderr:     io.Discard,
		clusterErr: errors.New("active cluster is broken"),
	}

	err := func() error {
		_, err := c.RPCClient("entities")
		return err
	}()
	if err == nil {
		t.Fatal("expected an error for an unloadable active cluster")
	}

	d, ok := errors.AsType[*ui.Diagnostic](err)
	if !ok {
		t.Fatalf("got %T, want *ui.Diagnostic", err)
	}
	if !strings.Contains(d.Summary, "active cluster") {
		t.Errorf("Summary = %q, want it to blame the active cluster", d.Summary)
	}
}

// The underlying reason is the actionable part here, so it shows without -v.
func TestUnusableClusterErrorShowsItsCause(t *testing.T) {
	c := &Context{ClusterName: "prod"}
	err := c.unusableClusterError(errors.New("tls: failed to find any PEM data"))

	d, ok := errors.AsType[*ui.Diagnostic](err)
	if !ok {
		t.Fatalf("got %T, want *ui.Diagnostic", err)
	}
	if !d.ShowCause {
		t.Error("the underlying reason should be visible without -v")
	}
	if !strings.Contains(d.Summary, "prod") {
		t.Errorf("Summary = %q, want it to name the cluster", d.Summary)
	}
}
