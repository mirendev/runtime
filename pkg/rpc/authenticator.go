package rpc

import (
	"context"
	"fmt"
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

// LocalOnlyAuthenticator requires a valid client certificate for all requests.
// This is used when cloud authentication is not enabled, ensuring that only
// clients with certificates issued by the local CA can access the server.
type LocalOnlyAuthenticator struct{}

func (l *LocalOnlyAuthenticator) AuthenticateRequest(ctx context.Context, r *http.Request) (bool, string, error) {
	// Even with an Authorization header, we require a valid client certificate
	return l.NoAuthorization(ctx, r)
}

func (l *LocalOnlyAuthenticator) NoAuthorization(ctx context.Context, r *http.Request) (bool, string, error) {
	// Require a valid client certificate
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		cert := r.TLS.PeerCertificates[0]
		return true, cert.Subject.CommonName, nil
	}
	return false, "", fmt.Errorf("authentication required")
}
