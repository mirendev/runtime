package outboard

import (
	"context"
	"crypto/subtle"
	"strings"

	"miren.dev/runtime/pkg/rpc"
)

// TokenAuthenticator implements rpc.Authenticator using a shared bearer token.
// It validates the Authorization header using constant-time comparison.
type TokenAuthenticator struct {
	token string
}

// NewTokenAuthenticator creates a new TokenAuthenticator with the given token.
func NewTokenAuthenticator(token string) *TokenAuthenticator {
	return &TokenAuthenticator{token: token}
}

func (t *TokenAuthenticator) Authenticate(ctx context.Context, creds *rpc.Credentials) (*rpc.Identity, error) {
	auth := creds.Authorization
	if auth == "" {
		return nil, nil // No credentials
	}

	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return nil, nil // Invalid scheme, treat as no credentials
	}

	provided := auth[len(prefix):]
	if subtle.ConstantTimeCompare([]byte(provided), []byte(t.token)) != 1 {
		return nil, nil // Invalid token
	}

	return &rpc.Identity{
		Subject: "outboard",
		Method:  rpc.AuthMethodToken,
	}, nil
}
