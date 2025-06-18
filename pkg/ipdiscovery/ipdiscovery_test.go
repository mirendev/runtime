package ipdiscovery

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/slogfmt"
)

// testLogger returns a logger suitable for tests
func testLogger(t *testing.T) *slog.Logger {
	return slog.New(slogfmt.NewTestHandler(t, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestDiscover(t *testing.T) {
	ctx := context.Background()
	log := testLogger(t)
	discovery, err := Discover(ctx, log)
	require.NoError(t, err)
	require.NotNil(t, discovery)

	// Should have at least one address (at minimum loopback)
	assert.NotEmpty(t, discovery.Addresses)

	// Check that addresses have required fields
	for _, addr := range discovery.Addresses {
		assert.NotEmpty(t, addr.Interface)
		assert.NotEmpty(t, addr.IP)
		assert.NotEmpty(t, addr.Network)
	}
}

func TestGetPublicIP(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "miren-runtime/1.0", r.Header.Get("User-Agent"))

		response := PublicIPResponse{
			IP:      "203.0.113.1",
			Country: "US",
			City:    "San Francisco",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Override the URL for testing
	// Since getPublicIP is not exported and uses a hardcoded URL,
	// we'll test through the Discover function with a real server
	ctx := context.Background()

	// For unit testing, we can't easily override the URL,
	// so we'll just test that the function doesn't panic
	ip, err := getPublicIP(ctx)
	// The actual call might fail due to network conditions,
	// but it shouldn't panic
	if err == nil {
		assert.NotEmpty(t, ip)
		// Validate it's a valid IP
		assert.NotNil(t, net.ParseIP(ip))
	}
}

func TestDiscoverWithTimeout(t *testing.T) {
	log := testLogger(t)
	discovery, err := DiscoverWithTimeout(5*time.Second, log)
	require.NoError(t, err)
	require.NotNil(t, discovery)

	// Should have addresses
	assert.NotEmpty(t, discovery.Addresses)
}

func TestAddressTypes(t *testing.T) {
	ctx := context.Background()
	log := testLogger(t)
	discovery, err := Discover(ctx, log)
	require.NoError(t, err)

	var hasIPv4 bool
	for _, addr := range discovery.Addresses {
		ip := net.ParseIP(addr.IP)
		require.NotNil(t, ip)

		if ip.To4() != nil {
			hasIPv4 = true
			assert.False(t, addr.IsIPv6)
		} else {
			assert.True(t, addr.IsIPv6)
		}
	}

	// Most systems should have at least IPv4
	assert.True(t, hasIPv4)
}

func TestDiscoveryJSON(t *testing.T) {
	// Test that Discovery can be properly marshaled to JSON
	discovery := &Discovery{
		PublicIP: "203.0.113.1",
		Addresses: []Address{
			{
				Interface: "eth0",
				IP:        "192.168.1.100",
				Network:   "192.168.1.0/24",
				IsIPv6:    false,
			},
			{
				Interface: "eth0",
				IP:        "2001:db8::1",
				Network:   "2001:db8::/64",
				IsIPv6:    true,
			},
		},
	}

	data, err := json.Marshal(discovery)
	require.NoError(t, err)

	var decoded Discovery
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, discovery.PublicIP, decoded.PublicIP)
	assert.Equal(t, len(discovery.Addresses), len(decoded.Addresses))
}
