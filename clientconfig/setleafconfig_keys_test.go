package clientconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSetLeafConfigRetainsKeys guards against a regression where SetLeafConfig
// built its in-memory leaf Config without copying Keys, so a key registered via
// a leaf config was invisible to GetKey/HasKey until the config was reloaded
// from disk.
func TestSetLeafConfigRetainsKeys(t *testing.T) {
	cfg := NewConfig()

	cfg.SetLeafConfig("key-miren-cli", &ConfigData{
		Keys: map[string]*KeyConfig{
			"miren-cli": {
				Name:       "miren-cli",
				Type:       "ed25519",
				PrivateKey: "-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----",
			},
		},
	})

	// The key must be visible immediately, before any Save/reload.
	require.True(t, cfg.HasKey("miren-cli"), "HasKey should see a leaf-config key before reload")

	key, err := cfg.GetKey("miren-cli")
	require.NoError(t, err, "GetKey should resolve a leaf-config key before reload")
	require.Equal(t, "ed25519", key.Type)
}
