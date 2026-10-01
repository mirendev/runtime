package cloudauth

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"miren.dev/runtime/pkg/auth"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid config with cloud URL",
			config: Config{
				CloudURL: "https://miren.cloud",
				Logger:   slog.Default(),
				Tags: map[string]any{
					"environment": "production",
					"cluster":     "us-west-1",
				},
			},
			wantError: false,
		},
		{
			name: "empty tag key",
			config: Config{
				CloudURL: "https://miren.cloud",
				Logger:   slog.Default(),
				Tags: map[string]any{
					"": "value",
				},
			},
			wantError: true,
			errorMsg:  "tag key cannot be empty",
		},
		{
			name: "invalid tag value type - slice",
			config: Config{
				CloudURL: "https://miren.cloud",
				Logger:   slog.Default(),
				Tags: map[string]any{
					"invalid": []string{"a", "b"},
				},
			},
			wantError: true,
			errorMsg:  "tag value for key \"invalid\" must be a simple type",
		},
		{
			name: "invalid tag value type - map",
			config: Config{
				CloudURL: "https://miren.cloud",
				Logger:   slog.Default(),
				Tags: map[string]any{
					"invalid": map[string]string{"nested": "value"},
				},
			},
			wantError: true,
			errorMsg:  "tag value for key \"invalid\" must be a simple type",
		},
		{
			name: "valid tag types",
			config: Config{
				CloudURL: "https://miren.cloud",
				Logger:   slog.Default(),
				Tags: map[string]any{
					"string_tag": "value",
					"int_tag":    42,
					"float_tag":  3.14,
					"bool_tag":   true,
					"nil_tag":    nil,
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestNewRPCAuthenticatorValidation(t *testing.T) {
	// Test that NewRPCAuthenticator calls Validate
	config := Config{
		// Missing Logger should cause validation error
		Logger: nil,
	}

	_, err := NewRPCAuthenticator(t.Context(), config)
	if err == nil {
		t.Error("expected validation error, got nil")
	}
	if !contains(err.Error(), "invalid configuration") {
		t.Errorf("expected 'invalid configuration' error, got %v", err)
	}
}

func TestDefaultCloudURL(t *testing.T) {
	// Test that default CloudURL is used when not provided
	config := Config{
		CloudURL: "", // Empty CloudURL should use default
		Logger:   slog.Default(),
	}

	auth, err := NewRPCAuthenticator(t.Context(), config)
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}
	defer auth.Stop()

	// Verify JWT validator is created (which means CloudURL was set)
	if auth.jwtValidator == nil {
		t.Error("expected JWT validator to be initialized with default CloudURL")
	}

	// Verify RBAC evaluator is created
	if auth.rbacEval == nil {
		t.Error("expected RBAC evaluator to be initialized with default CloudURL")
	}

	// Verify policy fetcher is created
	if auth.policyFetcher == nil {
		t.Error("expected policy fetcher to be initialized with default CloudURL")
	}
}

func TestRPCAuthenticatorUsesCurrentCloudGroups(t *testing.T) {
	response := `{"groups":[{"id":"grp_current","name":"Current"}]}`
	status := http.StatusOK
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/self/users/usr_123/groups" {
			t.Errorf("unexpected groups request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer service-token" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected request headers: %v", r.Header)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := &AuthClient{
		serverURL:    server.URL,
		httpClient:   server.Client(),
		currentToken: "service-token",
		tokenExpiry:  time.Now().Add(time.Hour),
	}
	a := &RPCAuthenticator{
		authClient: client,
		tokenCache: auth.NewTokenCache(ctx),
		logger:     slog.Default(),
	}
	a.tokenCache.Set("user-token", &auth.Claims{GroupIDs: []string{"grp_old"}, RegisteredClaims: jwt.RegisteredClaims{Subject: "usr_123"}})

	checkGroups := func(want ...string) {
		t.Helper()
		identity, err := a.authenticateJWT(ctx, "Bearer user-token")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(identity.Groups, want) {
			t.Errorf("groups = %v, want %v", identity.Groups, want)
		}
	}

	checkGroups("grp_current")
	response = `{"groups":[{"id":"grp_new","name":"New"}]}`
	checkGroups("grp_new") // The same cached JWT must not cache its groups.
	response = `{"groups":[]}`
	checkGroups() // An empty cloud result must not fall back to the token.
	status = http.StatusServiceUnavailable
	checkGroups("grp_old")
	status = http.StatusNotFound
	checkGroups("grp_old")
	status = http.StatusOK
	response = `{"groups":[{}]}`
	checkGroups("grp_old")
	a.authClient = nil
	checkGroups("grp_old")
	if requests != 6 {
		t.Errorf("groups requests = %d, want 6", requests)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr)))
}
