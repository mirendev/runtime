package rpc

import (
	"context"
	"net/http"
)

// Authenticator is an interface for authenticating RPC requests
type Authenticator interface {
	// AuthenticateRequest authenticates an HTTP request and returns whether it's allowed
	// It can check JWT tokens, certificates, or other authentication methods
	AuthenticateRequest(ctx context.Context, r *http.Request) (authenticated bool, identity string, err error)

	// NoAuthorization is called when a request has no Authorization header
	// This allows the authenticator to decide if such requests should be allowed
	// (e.g., based on client certificates or to enforce mandatory authentication)
	NoAuthorization(ctx context.Context, r *http.Request) (allowed bool, identity string, err error)
}

// NoOpAuthenticator is a no-op authenticator that allows all requests
type NoOpAuthenticator struct{}

func (n *NoOpAuthenticator) AuthenticateRequest(ctx context.Context, r *http.Request) (bool, string, error) {
	return true, "anonymous", nil
}

func (n *NoOpAuthenticator) NoAuthorization(ctx context.Context, r *http.Request) (bool, string, error) {
	return true, "anonymous", nil
}
