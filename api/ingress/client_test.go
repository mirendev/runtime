package ingress

import (
	"context"
	"log/slog"
	"testing"

	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestClientLookupCaseInsensitive(t *testing.T) {
	ctx := context.Background()

	// Setup in-memory entity server
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	ec := entityserver.NewClient(slog.Default(), inmem.EAC)

	// Create ingress client
	client := &Client{
		log: slog.Default(),
		ec:  ec,
		eac: inmem.EAC,
	}

	// Create a test app ID
	testAppID := entity.Id("test-app-123")

	// Test case 1: Store route with mixed case host
	t.Run("LookupWithVariousCases", func(t *testing.T) {
		originalHost := "Example.Com"

		// Set route with mixed case
		route, err := client.SetRoute(ctx, originalHost, testAppID)
		if err != nil {
			t.Fatalf("failed to set route: %v", err)
		}
		if route == nil {
			t.Fatal("expected route to be created, got nil")
		}

		// Verify the route was stored with lowercase host
		if route.Host != "example.com" {
			t.Errorf("expected host to be stored as 'example.com', got %q", route.Host)
		}

		// Test lookup with exact case as stored (lowercase)
		result, err := client.Lookup(ctx, "example.com")
		if err != nil {
			t.Fatalf("lookup with lowercase failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route with lowercase host")
		} else if result.App != testAppID {
			t.Errorf("expected app ID %q, got %q", testAppID, result.App)
		}

		// Test lookup with all uppercase
		result, err = client.Lookup(ctx, "EXAMPLE.COM")
		if err != nil {
			t.Fatalf("lookup with uppercase failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route with uppercase host")
		} else if result.App != testAppID {
			t.Errorf("expected app ID %q, got %q", testAppID, result.App)
		}

		// Test lookup with mixed case (different from original)
		result, err = client.Lookup(ctx, "ExAmPlE.CoM")
		if err != nil {
			t.Fatalf("lookup with mixed case failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route with mixed case host")
		} else if result.App != testAppID {
			t.Errorf("expected app ID %q, got %q", testAppID, result.App)
		}

		// Test lookup with original case used when setting
		result, err = client.Lookup(ctx, originalHost)
		if err != nil {
			t.Fatalf("lookup with original case failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route with original case host")
		} else if result.App != testAppID {
			t.Errorf("expected app ID %q, got %q", testAppID, result.App)
		}
	})

	// Test case 2: Multiple routes with different hosts
	t.Run("MultipleRoutesCaseInsensitive", func(t *testing.T) {
		testAppID2 := entity.Id("test-app-456")
		testAppID3 := entity.Id("test-app-789")

		// Create routes with different hosts
		_, err := client.SetRoute(ctx, "api.example.com", testAppID2)
		if err != nil {
			t.Fatalf("failed to set route for api.example.com: %v", err)
		}

		_, err = client.SetRoute(ctx, "WEB.EXAMPLE.COM", testAppID3)
		if err != nil {
			t.Fatalf("failed to set route for WEB.EXAMPLE.COM: %v", err)
		}

		// Lookup api.example.com with different cases
		result, err := client.Lookup(ctx, "API.EXAMPLE.COM")
		if err != nil {
			t.Fatalf("lookup failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route")
		} else if result.App != testAppID2 {
			t.Errorf("expected app ID %q, got %q", testAppID2, result.App)
		}

		// Lookup web.example.com with different cases
		result, err = client.Lookup(ctx, "web.example.com")
		if err != nil {
			t.Fatalf("lookup failed: %v", err)
		}
		if result == nil {
			t.Error("expected to find route")
		} else if result.App != testAppID3 {
			t.Errorf("expected app ID %q, got %q", testAppID3, result.App)
		}
	})

	// Test case 3: Non-existent host returns nil
	t.Run("NonExistentHostReturnsNil", func(t *testing.T) {
		result, err := client.Lookup(ctx, "does-not-exist.com")
		if err != nil {
			t.Fatalf("lookup should not error on non-existent host: %v", err)
		}
		if result != nil {
			t.Error("expected nil for non-existent host")
		}

		// Try with different case
		result, err = client.Lookup(ctx, "DOES-NOT-EXIST.COM")
		if err != nil {
			t.Fatalf("lookup should not error on non-existent host: %v", err)
		}
		if result != nil {
			t.Error("expected nil for non-existent host")
		}
	})
}
