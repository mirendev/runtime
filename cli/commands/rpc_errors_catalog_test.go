package commands

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata golden files")

// TestRPCErrorMessageCatalog renders every RPC failure message into a single
// golden file.
//
// The point is reviewability: these messages are the product, and a golden file
// makes a wording change show up as a readable diff in the PR rather than being
// buried in a Go string literal. Regenerate with:
//
//	go test ./cli/commands -run Catalog -update
func TestRPCErrorMessageCatalog(t *testing.T) {
	c := diagnosticContext()

	cases := []struct {
		scenario string
		err      error
	}{
		{
			scenario: "server isn't running, or the address is wrong, or traffic is blocked",
			err:      rpc.NewResolveUnreachableError("entities", "localhost:8443", 5*time.Second, errors.New("timeout: no recent network activity")),
		},
		{
			scenario: "connection established, then the server went quiet mid-request",
			err:      rpc.NewResolveWentSilentError("entities", "localhost:8443", 30*time.Second, errors.New("timeout: no recent network activity")),
		},
		{
			scenario: "request accepted, no reply before our deadline",
			err:      rpc.NewResolveNoAnswerError("entities", "localhost:8443", 8*time.Second, errors.New("context deadline exceeded")),
		},
		{
			scenario: "cluster is healthy but doesn't offer the capability",
			err:      rpc.NewResolveLookupError("entities", "localhost:8443", "unknown object: entities"),
		},
		{
			scenario: "not authorized",
			err:      rpc.NewResolveStatusError("entities", "localhost:8443", 401),
		},
		{
			scenario: "CI token verified but matched no binding",
			err: rpc.NewResolveStatusErrorWithReason("entities", "localhost:8443", 401,
				rpc.AuthErrorOIDCBindingMismatch,
				"OIDC token did not match any CI binding (issuer=https://token.actions.githubusercontent.com subject=repo:acme@1234567/web-app@7654321:ref:refs/heads/main repository=acme/web-app)"),
		},
		{
			scenario: "unexpected HTTP status",
			err:      rpc.NewResolveStatusError("entities", "localhost:8443", 404),
		},
		{
			scenario: "response body wasn't what we expected",
			err:      rpc.NewResolveDecodeError("entities", "localhost:8443", errors.New("cbor: cannot unmarshal UTF-8 text string into Go value of type rpc.lookupResponse")),
		},
	}

	var buf bytes.Buffer
	for _, tc := range cases {
		fmt.Fprintf(&buf, "# %s\n\n", tc.scenario)

		d, ok := errors.AsType[*ui.Diagnostic](c.wrapRPCError(tc.err))
		if !ok {
			t.Fatalf("%s: no diagnostic produced", tc.scenario)
		}
		d.WriteForTerminal(&buf)
		buf.WriteString("\n")
	}

	// The failures that happen before anything is dialed. These come from the
	// configuration rather than the transport, so they don't go through
	// wrapRPCError, but they're the same product and deserve the same review.
	for _, tc := range configDiagnosticCases() {
		fmt.Fprintf(&buf, "# %s\n\n", tc.scenario)
		tc.diagnostic.WriteForTerminal(&buf)
		buf.WriteString("\n")
	}

	// The same failure under -v, which appends the underlying transport error.
	fmt.Fprintf(&buf, "# with -v (underlying error preserved)\n\n")
	verbose, ok := errors.AsType[*ui.Diagnostic](c.wrapRPCError(cases[0].err))
	if !ok {
		t.Fatal("no diagnostic produced for the verbose case")
	}
	verbose.ShowCause = true
	verbose.WriteForTerminal(&buf)

	const golden = "testdata/rpc_error_messages.txt"

	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden file: %v (run with -update to create it)", err)
	}
	if !bytes.Equal(want, buf.Bytes()) {
		t.Errorf("messages changed. Run `go test ./cli/commands -run Catalog -update` and review the diff.\n\n--- got ---\n%s", buf.String())
	}
}

// configDiagnosticCases are the failures raised from the client configuration,
// before any connection is attempted.
func configDiagnosticCases() []struct {
	scenario   string
	diagnostic *ui.Diagnostic
} {
	populated := clientconfig.NewConfig()
	populated.SetCluster("cloud", &clientconfig.ClusterConfig{Hostname: "cloud.example.com:8443"})
	populated.SetCluster("homelab", &clientconfig.ClusterConfig{Hostname: "homelab:8443"})

	named := &Context{
		ClusterName:      "homelab",
		requestedCluster: "homelb",
		clusterErr:       errors.New(`cluster "homelb": failed to connect to "homelb": failed to establish QUIC connection`),
	}
	active := &Context{
		clusterErr: errors.New("cluster \"homelab\" not found in configuration"),
	}
	unusable := &Context{ClusterName: "homelab"}
	inactive := &Context{ClientConfig: populated}

	asDiagnostic := func(err error) *ui.Diagnostic {
		d, ok := errors.AsType[*ui.Diagnostic](err)
		if !ok {
			panic(fmt.Sprintf("expected a diagnostic, got %T", err))
		}
		return d
	}

	return []struct {
		scenario   string
		diagnostic *ui.Diagnostic
	}{
		{
			scenario:   "-C names something that is neither a configured cluster nor an address",
			diagnostic: asDiagnostic(named.unknownClusterError()),
		},
		{
			scenario:   "the configured active cluster couldn't be loaded",
			diagnostic: asDiagnostic(active.unknownClusterError()),
		},
		{
			scenario:   "the cluster is configured but its entry can't produce a connection",
			diagnostic: asDiagnostic(unusable.unusableClusterError("homelab", errors.New("cluster has a client certificate but no client key"))),
		},
		{
			scenario:   "clusters are configured but none is active",
			diagnostic: asDiagnostic(inactive.noActiveClusterError()),
		},
	}
}
