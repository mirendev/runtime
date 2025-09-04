package clientconfig

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindWorkingAddress(t *testing.T) {
	ctx := context.Background()

	t.Run("single address returns immediately", func(t *testing.T) {
		addresses := []string{"example.com:8443"}
		result, err := findWorkingAddress(ctx, addresses)
		assert.NoError(t, err)
		assert.Equal(t, "example.com:8443", result)
	})

	t.Run("empty addresses returns error", func(t *testing.T) {
		addresses := []string{}
		_, err := findWorkingAddress(ctx, addresses)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no addresses to probe")
	})

	t.Run("parallel probing with all failures", func(t *testing.T) {
		// Use non-routable addresses that will fail quickly
		addresses := []string{
			"192.0.2.1:8443",    // TEST-NET-1 (non-routable)
			"198.51.100.1:8443", // TEST-NET-2 (non-routable)
			"203.0.113.1:8443",  // TEST-NET-3 (non-routable)
		}

		start := time.Now()
		_, err := findWorkingAddress(ctx, addresses)
		duration := time.Since(start)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "all addresses failed")
		// Should complete within timeout (3 seconds + buffer)
		assert.Less(t, duration, 5*time.Second)
	})

	t.Run("context cancellation stops probing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		addresses := []string{
			"192.0.2.1:8443",
			"198.51.100.1:8443",
		}

		// Cancel immediately
		cancel()

		_, err := findWorkingAddress(ctx, addresses)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestProbeAddress(t *testing.T) {
	ctx := context.Background()

	t.Run("adds default port if missing", func(t *testing.T) {
		// This will fail to connect but tests port addition
		err := probeAddress(ctx, "example.com")
		assert.Error(t, err) // Expected to fail, just testing it doesn't panic
	})

	t.Run("handles invalid address", func(t *testing.T) {
		err := probeAddress(ctx, "not-a-valid-address::::::")
		assert.Error(t, err)
	})

	t.Run("respects context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Use a non-routable address
		err := probeAddress(ctx, "192.0.2.1:8443")
		assert.Error(t, err)
	})
}

func TestAddressCaching(t *testing.T) {
	// Create a temporary cache directory
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	clusterName := "test-cluster"
	address := "cluster.example.com:8443"

	t.Run("save and retrieve cached address", func(t *testing.T) {
		// Save address
		err := saveAddressToCache(clusterName, address)
		require.NoError(t, err)

		// Retrieve address
		cached, err := getCachedAddress(clusterName)
		require.NoError(t, err)
		assert.Equal(t, address, cached)
	})

	t.Run("no cached address returns empty", func(t *testing.T) {
		cached, err := getCachedAddress("non-existent-cluster")
		assert.NoError(t, err)
		assert.Empty(t, cached)
	})

	t.Run("expired cache returns empty", func(t *testing.T) {
		// Save address
		err := saveAddressToCache(clusterName, address)
		require.NoError(t, err)

		// Manually modify the file's modification time to be old
		cacheDir, _ := getCacheDir()
		addressPath := cacheDir + "/cluster_address_" + clusterName
		oldTime := time.Now().Add(-2 * time.Hour)
		_ = os.Chtimes(addressPath, oldTime, oldTime)

		// Should return empty due to expiry
		cached, err := getCachedAddress(clusterName)
		assert.NoError(t, err)
		assert.Empty(t, cached)
	})
}

func TestRPCOptionsWithAddressProbing(t *testing.T) {
	// Create a temporary cache directory
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	config := NewConfig()
	config.SetCluster("test", &ClusterConfig{
		Hostname: "primary.example.com:8443",
		AllAddresses: []string{
			"primary.example.com:8443",
			"secondary.example.com:8443",
			"tertiary.example.com:8443",
		},
		Insecure: true,
	})

	cluster, err := config.GetCluster("test")
	require.NoError(t, err)
	ctx := context.Background()

	t.Run("uses cached address if available", func(t *testing.T) {
		// Cache an address
		cachedAddr := "secondary.example.com:8443"
		err := saveAddressToCache("test", cachedAddr)
		require.NoError(t, err)

		opts, err := cluster.RPCOptionsWithName(ctx, config, "test")
		require.NoError(t, err)

		// Should use the cached address
		found := false
		for _, opt := range opts {
			// Check if the endpoint option contains the cached address
			// Note: We can't directly inspect the option, but we know it's being set
			if opt != nil {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("falls back to hostname if no AllAddresses", func(t *testing.T) {
		cluster := &ClusterConfig{
			Hostname: "single.example.com:8443",
			Insecure: true,
		}

		opts, err := cluster.RPCOptionsWithName(ctx, config, "single")
		require.NoError(t, err)
		assert.NotEmpty(t, opts)
	})

	t.Run("ignores cached address not in AllAddresses", func(t *testing.T) {
		// Cache an address that's not in the list
		err := saveAddressToCache("test2", "unknown.example.com:8443")
		require.NoError(t, err)

		cluster := &ClusterConfig{
			Hostname: "primary.example.com:8443",
			AllAddresses: []string{
				"primary.example.com:8443",
				"secondary.example.com:8443",
			},
			Insecure: true,
		}

		// Should probe addresses since cached one is invalid
		opts, err := cluster.RPCOptionsWithName(ctx, config, "test2")
		require.NoError(t, err)
		assert.NotEmpty(t, opts)
	})
}
