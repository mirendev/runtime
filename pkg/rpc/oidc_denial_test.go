package rpc_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/example"
)

// stubAuthenticator fails every request with a fixed error.
type stubAuthenticator struct{ err error }

func (s *stubAuthenticator) Authenticate(ctx context.Context, creds *rpc.Credentials) (*rpc.Identity, error) {
	return nil, s.err
}

// TestOIDCBindingMismatchReachesTheClient is the end-to-end check for the half
// of MIR-1491 that was about diagnosis rather than matching. A CI deploy that
// presents a valid token matching no binding used to surface as a bare 401,
// which the CLI rendered as "your session may have expired, run 'miren login'" —
// advice that is wrong in a way that costs hours, since the credentials are
// fine.
//
// The unit tests cover the pieces (the authenticator builds the error, the 401
// handler sets the headers, the client parses them). This one runs a real
// QUIC/HTTP3 listener and a real client so the reason has to survive the actual
// transport: headers set on an error response, carried over the wire, and
// plumbed back into the ResolveError the CLI inspects.
func TestOIDCBindingMismatchReachesTheClient(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	subject := "repo:acme@277133432/app@1316584243:ref:refs/heads/main"

	ss, err := rpc.NewState(ctx,
		rpc.WithSkipVerify,
		rpc.WithAuthenticator(&stubAuthenticator{err: &oidcauth.BindingMismatchError{
			Issuer:     "https://token.actions.githubusercontent.com",
			Subject:    subject,
			Repository: "acme/app",
		}}),
	)
	r.NoError(err)
	ss.Server().ExposeValue("meter", example.AdaptEmitTemps(&exampleEmit{}))

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	_, err = cs.Connect(ss.ListenAddr(), "meter")
	r.Error(err, "expected the connection to be denied")

	var resolveErr *rpc.ResolveError
	r.True(errors.As(err, &resolveErr), "expected a *rpc.ResolveError, got %T: %v", err, err)
	r.Equal(401, resolveErr.StatusCode)

	// Code is what lets the CLI suppress the 'miren login' hint.
	r.Equal(rpc.AuthErrorOIDCBindingMismatch, resolveErr.Code)

	// Detail is what turns a log-spelunking session into an obvious fix.
	r.Contains(resolveErr.Detail, subject)
	r.Contains(resolveErr.Detail, "acme/app")
}

// An authenticator that didn't opt into disclosure must not leak its reason
// across the wire, even though it travels the same path.
func TestOrdinaryAuthFailureDisclosesNothingToTheClient(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	ss, err := rpc.NewState(ctx,
		rpc.WithSkipVerify,
		rpc.WithAuthenticator(&stubAuthenticator{
			err: fmt.Errorf("signing key 4f2a not found in keyring"),
		}),
	)
	r.NoError(err)
	ss.Server().ExposeValue("meter", example.AdaptEmitTemps(&exampleEmit{}))

	cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	r.NoError(err)

	_, err = cs.Connect(ss.ListenAddr(), "meter")
	r.Error(err)

	var resolveErr *rpc.ResolveError
	r.True(errors.As(err, &resolveErr), "expected a *rpc.ResolveError, got %T: %v", err, err)
	r.Equal(401, resolveErr.StatusCode)
	r.Empty(resolveErr.Code)
	r.Empty(resolveErr.Detail)
	r.NotContains(err.Error(), "keyring")
}
