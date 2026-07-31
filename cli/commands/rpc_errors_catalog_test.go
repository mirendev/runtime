package commands

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

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

		var d *ui.Diagnostic
		if !errors.As(c.wrapRPCError(tc.err), &d) {
			t.Fatalf("%s: no diagnostic produced", tc.scenario)
		}
		d.WriteForTerminal(&buf)
		buf.WriteString("\n")
	}

	// The same failure under -v, which appends the underlying transport error.
	fmt.Fprintf(&buf, "# with -v (underlying error preserved)\n\n")
	var verbose *ui.Diagnostic
	if !errors.As(c.wrapRPCError(cases[0].err), &verbose) {
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
