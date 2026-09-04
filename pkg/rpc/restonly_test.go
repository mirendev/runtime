package rpc

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restOnlyMethods builds the arrangement the flag exists for: the same answer
// offered twice, once shaped for a program and once shaped for a URL.
func restOnlyMethods() []Method {
	noop := func(context.Context, Call) error { return nil }

	return []Method{
		{
			Name:          "listThings",
			InterfaceName: "Things",
			Index:         0,
			Handler:       noop,
		},
		{
			Name:          "httpListThings",
			InterfaceName: "Things",
			Index:         1,
			RestOnly:      true,
			Handler:       noop,
			HTTP:          &HTTPBinding{Verb: "GET", Path: "/api/v1/things"},
		},
	}
}

// rpcMethod is what every dispatch site resolves through, so this is the check
// that the flag actually withdraws the method rather than merely labelling it.
func TestRestOnlyMethodIsUnreachableOverRPC(t *testing.T) {
	iface := NewInterface(restOnlyMethods(), struct{}{})

	ordinary, found := iface.rpcMethod("listThings")
	require.True(t, found)
	require.NotNil(t, ordinary.Handler, "an ordinary method still dispatches")

	_, ok := iface.rpcMethod("httpListThings")
	assert.False(t, ok, "a rest-only method must not resolve for RPC dispatch")

	// The same answer a typo gets, which is the point: an RPC caller should not
	// be able to tell a rest-only method from one that does not exist.
	_, missing := iface.rpcMethod("noSuchThing")
	assert.Equal(t, missing, ok)
}

// Every path that invokes a handler by name has to consult rpcMethod. The first
// version of this feature guarded three of them and left the inline router, the
// actor path and the in-process test client reading the map directly, so a
// rest-only method stayed reachable by all three. This asserts the map has no
// other readers rather than trusting a grep done once.
func TestEveryDispatchPathGoesThroughRpcMethod(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	direct := regexp.MustCompile(`\.methods\[`)

	for _, f := range files {
		if f == "server.go" {
			// rpcMethod itself is the one sanctioned reader.
			continue
		}

		src, err := os.ReadFile(f)
		require.NoError(t, err)

		for n, line := range strings.Split(string(src), "\n") {
			if direct.MatchString(line) {
				t.Errorf("%s:%d reads the method map directly; use Interface.rpcMethod so rest-only methods stay unreachable:\n\t%s",
					f, n+1, strings.TrimSpace(line))
			}
		}
	}
}

// Withdrawing it from RPC must not unmount its route. The REST gateway
// enumerates Methods() and dispatches through the handler it finds there.
func TestRestOnlyMethodKeepsItsHTTPRoute(t *testing.T) {
	iface := NewInterface(restOnlyMethods(), struct{}{})

	var mounted []string
	for _, m := range iface.Methods() {
		if m.HTTP == nil {
			continue
		}
		mounted = append(mounted, m.Name)
		require.NotNil(t, m.Handler, "the REST gateway dispatches through this handler")
	}

	assert.Equal(t, []string{"httpListThings"}, mounted)
}

// The advertised list is how a client discovers what it may call. Listing a
// rest-only method would invite a call that answers "unknown method".
func TestRestOnlyMethodIsNotAdvertised(t *testing.T) {
	iface := NewInterface(restOnlyMethods(), struct{}{})

	resp := newMethodsResponse(iface.methods)

	assert.Contains(t, resp.Methods, "listThings")
	assert.NotContains(t, resp.Methods, "httpListThings")
	assert.NotContains(t, resp.Params, "httpListThings",
		"parameter-level capability detection must not describe it either")
}
