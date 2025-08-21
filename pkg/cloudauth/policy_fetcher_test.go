package cloudauth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"miren.dev/runtime/pkg/rbac"
)

func TestPolicyFetcher(t *testing.T) {
	// Create a mock server that returns a policy
	mockPolicy := &rbac.Policy{
		Rules: []rbac.Rule{
			{
				ID:          "test-rule",
				Name:        "Test Rule",
				Description: "Test rule for service accounts",
				TagSelector: rbac.TagSelector{
					Expressions: []rbac.TagExpression{
						{Tag: "test", Value: "true", Operator: "equals"},
					},
				},
				Groups: []string{"test-group"},
				Permissions: []rbac.Permission{
					{
						Resource: "apps/*",
						Actions:  []string{"read", "write"},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/self/rbac-rules" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Check for authorization header when auth client is provided
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" && authHeader != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", authHeader)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockPolicy)
	}))
	defer server.Close()

	// Test without auth client
	t.Run("without auth", func(t *testing.T) {
		fetcher := NewPolicyFetcher(server.URL, nil)

		ctx := context.Background()
		err := fetcher.fetchPolicy(ctx)
		if err != nil {
			t.Fatalf("failed to fetch policy: %v", err)
		}

		policy := fetcher.GetPolicy()
		if policy == nil {
			t.Fatal("expected policy, got nil")
		}

		if len(policy.Rules) != 1 {
			t.Errorf("expected 1 rule, got %d", len(policy.Rules))
		}

		if policy.Rules[0].Name != "Test Rule" {
			t.Errorf("expected rule name 'Test Rule', got %s", policy.Rules[0].Name)
		}
	})

	// Test periodic refresh
	t.Run("periodic refresh", func(t *testing.T) {
		// Use a short refresh interval for testing
		fetcher := NewPolicyFetcher(server.URL, nil, WithRefreshInterval(10*time.Millisecond))

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := fetcher.Start(ctx)
		if err != nil {
			t.Fatalf("failed to start fetcher: %v", err)
		}

		// Give it a moment to fetch
		time.Sleep(10 * time.Millisecond)

		policy := fetcher.GetPolicy()
		if policy == nil {
			t.Fatal("expected policy after start, got nil")
		}

		fetcher.Stop()
	})

	// Test with server error
	t.Run("server error", func(t *testing.T) {
		errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal server error"))
		}))
		defer errorServer.Close()

		fetcher := NewPolicyFetcher(errorServer.URL, nil)

		ctx := context.Background()
		err := fetcher.fetchPolicy(ctx)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		// Should return nil policy on error
		policy := fetcher.GetPolicy()
		if policy != nil {
			t.Fatal("expected nil policy on error, got policy")
		}
	})
}

func TestPolicyFetcherRefreshInterval(t *testing.T) {
	fetchCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&rbac.Policy{
			Rules: []rbac.Rule{
				{
					ID:   "test-rule",
					Name: "Test Rule",
				},
			},
		})
	}))
	defer server.Close()

	// Create fetcher with 50ms refresh interval
	fetcher := NewPolicyFetcher(server.URL, nil, WithRefreshInterval(50*time.Millisecond))
	
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	
	err := fetcher.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start fetcher: %v", err)
	}
	
	// Wait for multiple refresh cycles
	time.Sleep(180 * time.Millisecond)
	
	fetcher.Stop()
	
	// Should have fetched at least 3 times (initial + 3 refreshes in 180ms with 50ms interval)
	if fetchCount < 3 {
		t.Errorf("expected at least 3 fetches, got %d", fetchCount)
	}
}

func TestPolicyFetcherIntegration(t *testing.T) {
	// Test the integration with RPCAuthenticator
	mockPolicy := &rbac.Policy{
		Rules: []rbac.Rule{
			{
				ID:          "test-sa-rule",
				Name:        "Test Service Account Rule",
				Description: "Allow test service account to execute apps",
				TagSelector: rbac.TagSelector{},
				Groups:      []string{"test-services"},
				Permissions: []rbac.Permission{
					{
						Resource: "apps/*",
						Actions:  []string{"execute"},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockPolicy)
	}))
	defer server.Close()

	// Create authenticator with cloud URL (RBAC is automatically enabled)
	config := Config{
		CloudURL: server.URL,
		Logger:   slog.Default(),
		Tags: map[string]interface{}{
			"cluster": "test",
		},
	}

	auth, err := NewRPCAuthenticator(config)
	if err != nil {
		t.Fatalf("failed to create authenticator: %v", err)
	}
	defer auth.Stop()

	// Give the fetcher time to fetch the policy
	time.Sleep(50 * time.Millisecond)

	// Verify RBAC evaluation works with fetched policy
	req := &rbac.Request{
		Subject:  "test-sa",
		Groups:   []string{"test-services"},
		Resource: "apps/test",
		Action:   "execute",
		Tags: map[string]interface{}{
			"cluster": "test",
		},
	}

	decision := auth.rbacEval.Evaluate(req)
	if decision != rbac.DecisionAllow {
		t.Errorf("expected allow decision, got %v", decision)
	}

	// Test denied request
	req.Action = "delete"
	decision = auth.rbacEval.Evaluate(req)
	if decision != rbac.DecisionDeny {
		t.Errorf("expected deny decision, got %v", decision)
	}
}

