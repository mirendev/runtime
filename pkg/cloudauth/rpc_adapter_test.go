package cloudauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
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
	responses := []struct {
		status int
		body   string
		groups []string
	}{
		{http.StatusOK, `{"groups":[{"id":"grp_current","name":"Current"}]}`, []string{"grp_current"}},
		{http.StatusOK, `{"groups":[{"id":"grp_new","name":"New"}]}`, []string{"grp_new"}},
		{http.StatusOK, `{"groups":[]}`, nil},
		{http.StatusServiceUnavailable, `{"error":"unavailable"}`, []string{"grp_old"}},
		{http.StatusNotFound, `{"error":"user not found"}`, []string{"grp_old"}},
		{http.StatusOK, `{"groups":[{}]}`, []string{"grp_old"}},
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(requests.Add(1)) - 1
		if i >= len(responses) {
			t.Error("unexpected extra groups request")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/self/users/usr_123/groups" {
			t.Errorf("unexpected groups request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer service-token" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected request headers: %v", r.Header)
		}
		w.WriteHeader(responses[i].status)
		_, _ = w.Write([]byte(responses[i].body))
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

	// The same cached JWT must see changed membership, including empty groups.
	for _, response := range responses {
		checkGroups(response.groups...)
	}
	a.authClient = nil
	checkGroups("grp_old")
	if got := requests.Load(); got != int32(len(responses)) {
		t.Errorf("groups requests = %d, want %d", got, len(responses))
	}
}

func TestUserGroupsLookupDeadline(t *testing.T) {
	for _, refreshToken := range []bool{false, true} {
		t.Run(fmt.Sprintf("refresh_token=%t", refreshToken), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
			}))
			defer server.Close()
			keyPair, err := GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			client, err := NewAuthClient(server.URL, keyPair)
			if err != nil {
				t.Fatal(err)
			}
			if !refreshToken {
				client.currentToken = "service-token"
				client.tokenExpiry = time.Now().Add(time.Hour)
			}
			// A longer caller deadline ensures this exercises the lookup's own bound.
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			start := time.Now()
			_, err = client.GetUserGroups(ctx, "usr_123")
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected lookup deadline error, got %v", err)
			}
			if elapsed := time.Since(start); elapsed >= 4*time.Second {
				t.Errorf("lookup took %v; expected its own short deadline", elapsed)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && len(substr) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr)))
}
