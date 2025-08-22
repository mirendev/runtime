package rpc

import (
	"context"
	"net/http"
	"testing"
)

// TestAuthenticator verifies the authenticator integration
func TestAuthenticator(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		expectAllowed bool
		authenticator Authenticator
	}{
		{
			name:          "NoOpAuthenticator allows all requests",
			authHeader:    "",
			expectAllowed: true,
			authenticator: &NoOpAuthenticator{},
		},
		{
			name:          "NoOpAuthenticator allows with auth header",
			authHeader:    "Bearer token123",
			expectAllowed: true,
			authenticator: &NoOpAuthenticator{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "/_rpc/call/test/method", nil)
			if err != nil {
				t.Fatal(err)
			}

			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			if tt.authHeader != "" {
				allowed, identity, err := tt.authenticator.AuthenticateRequest(context.Background(), req)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if allowed != tt.expectAllowed {
					t.Errorf("expected allowed=%v, got %v", tt.expectAllowed, allowed)
				}
				if allowed && identity == "" {
					t.Error("expected non-empty identity for allowed request")
				}
			} else {
				allowed, identity, err := tt.authenticator.NoAuthorization(context.Background(), req)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if allowed != tt.expectAllowed {
					t.Errorf("expected allowed=%v, got %v", tt.expectAllowed, allowed)
				}
				if allowed && identity == "" {
					t.Error("expected non-empty identity for allowed request")
				}
			}
		})
	}
}
