package rpc

import (
	"context"
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

	ordinary := iface.rpcMethod("listThings")
	require.NotNil(t, ordinary.Handler, "an ordinary method still dispatches")

	restOnly := iface.rpcMethod("httpListThings")
	assert.Nil(t, restOnly.Handler,
		"a rest-only method must resolve to nothing dispatchable; every call site treats a nil Handler as unknown")

	// The same answer a typo gets, which is the point: an RPC caller should not
	// be able to tell a rest-only method from one that does not exist.
	missing := iface.rpcMethod("noSuchThing")
	assert.Equal(t, missing.Handler == nil, restOnly.Handler == nil)
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
