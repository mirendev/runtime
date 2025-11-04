package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/cloudauth"
)

// newTestContext creates a Context suitable for testing
func newTestContext() *Context {
	return &Context{
		Context: context.Background(),
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}
}

func TestGetOrCreateKey(t *testing.T) {
	t.Run("creates new key when none exists", func(t *testing.T) {
		// Setup temp config directory
		tmpDir := t.TempDir()
		os.Setenv("MIREN_CONFIG", tmpDir)
		defer os.Unsetenv("MIREN_CONFIG")

		ctx := newTestContext()
		keyName := "test-key"

		// Get or create key (should create)
		keyPair, err := getOrCreateKey(ctx, keyName)
		require.NoError(t, err)
		require.NotNil(t, keyPair)

		// Verify keypair is valid
		privateKeyPEM, err := keyPair.PrivateKeyPEM()
		require.NoError(t, err)
		assert.NotEmpty(t, privateKeyPEM)

		publicKeyPEM, err := keyPair.PublicKeyPEM()
		require.NoError(t, err)
		assert.NotEmpty(t, publicKeyPEM)
	})

	t.Run("reuses existing key", func(t *testing.T) {
		// Setup temp config directory
		tmpDir := t.TempDir()
		os.Setenv("MIREN_CONFIG", tmpDir)
		defer os.Unsetenv("MIREN_CONFIG")

		ctx := newTestContext()
		keyName := "test-key"

		// First call - create the key
		keyPair1, err := getOrCreateKey(ctx, keyName)
		require.NoError(t, err)
		require.NotNil(t, keyPair1)

		// Save the key to config
		config := clientconfig.NewConfig()
		privateKeyPEM, err := keyPair1.PrivateKeyPEM()
		require.NoError(t, err)

		config.SetKey(keyName, &clientconfig.KeyConfig{
			Name:        keyName,
			Type:        "ed25519",
			PrivateKey:  privateKeyPEM,
			Fingerprint: keyPair1.Fingerprint(),
		})

		configPath := filepath.Join(tmpDir, "clientconfig.yaml")
		err = config.SaveTo(configPath)
		require.NoError(t, err)

		// Second call - should reuse the key
		keyPair2, err := getOrCreateKey(ctx, keyName)
		require.NoError(t, err)
		require.NotNil(t, keyPair2)

		// Verify the keys are the same
		assert.Equal(t, keyPair1.Fingerprint(), keyPair2.Fingerprint())
	})

	t.Run("creates different keys for different names", func(t *testing.T) {
		// Setup temp config directory
		tmpDir := t.TempDir()
		os.Setenv("MIREN_CONFIG", tmpDir)
		defer os.Unsetenv("MIREN_CONFIG")

		ctx := newTestContext()

		// Create first key
		keyPair1, err := getOrCreateKey(ctx, "key1")
		require.NoError(t, err)

		// Create second key
		keyPair2, err := getOrCreateKey(ctx, "key2")
		require.NoError(t, err)

		// Verify they're different
		assert.NotEqual(t, keyPair1.Fingerprint(), keyPair2.Fingerprint())
	})
}

func TestSaveKeyPairToConfig(t *testing.T) {
	t.Run("saves key separately and creates identity with KeyRef", func(t *testing.T) {
		// Setup temp config directory
		tmpDir := t.TempDir()
		os.Setenv("MIREN_CONFIG", tmpDir)
		defer os.Unsetenv("MIREN_CONFIG")

		// Generate a test keypair
		keyPair, err := cloudauth.GenerateKeyPair()
		require.NoError(t, err)

		identityName := "test-identity"
		cloudURL := "https://test.miren.cloud"
		keyName := "test-key"

		// Save to config
		err = saveKeyPairToConfig(identityName, cloudURL, keyPair, keyName)
		require.NoError(t, err)

		// Load config and verify
		config, err := clientconfig.LoadConfig()
		require.NoError(t, err)

		// Check that the key was saved
		assert.True(t, config.HasKey(keyName), "Key should be saved")
		key, err := config.GetKey(keyName)
		require.NoError(t, err)
		assert.Equal(t, keyName, key.Name)
		assert.Equal(t, "ed25519", key.Type)
		assert.NotEmpty(t, key.PrivateKey)
		assert.Equal(t, keyPair.Fingerprint(), key.Fingerprint)

		// Check that the identity was created with KeyRef
		assert.True(t, config.HasIdentity(identityName), "Identity should be saved")
		identity, err := config.GetIdentity(identityName)
		require.NoError(t, err)
		assert.Equal(t, "keypair", identity.Type)
		assert.Equal(t, "https://test.miren.cloud", identity.Issuer)
		assert.Equal(t, keyName, identity.KeyRef, "Identity should reference the key")
		assert.Empty(t, identity.PrivateKey, "Identity should not have direct PrivateKey")
	})

	t.Run("does not create duplicate keys", func(t *testing.T) {
		// Setup temp config directory
		tmpDir := t.TempDir()
		os.Setenv("MIREN_CONFIG", tmpDir)
		defer os.Unsetenv("MIREN_CONFIG")

		// Generate a test keypair
		keyPair, err := cloudauth.GenerateKeyPair()
		require.NoError(t, err)

		keyName := "test-key"

		// Save first identity with the key
		err = saveKeyPairToConfig("identity1", "https://test.miren.cloud", keyPair, keyName)
		require.NoError(t, err)

		// Save second identity with the same key name
		err = saveKeyPairToConfig("identity2", "https://test.miren.cloud", keyPair, keyName)
		require.NoError(t, err)

		// Verify only one key exists
		config, err := clientconfig.LoadConfig()
		require.NoError(t, err)

		keyNames := config.GetKeyNames()
		assert.Contains(t, keyNames, keyName)

		// Both identities should reference the same key
		identity1, err := config.GetIdentity("identity1")
		require.NoError(t, err)
		assert.Equal(t, keyName, identity1.KeyRef)

		identity2, err := config.GetIdentity("identity2")
		require.NoError(t, err)
		assert.Equal(t, keyName, identity2.KeyRef)
	})
}

func TestGetPrivateKeyPEM(t *testing.T) {
	t.Run("resolves private key from KeyRef", func(t *testing.T) {
		// Generate a test keypair
		keyPair, err := cloudauth.GenerateKeyPair()
		require.NoError(t, err)

		privateKeyPEM, err := keyPair.PrivateKeyPEM()
		require.NoError(t, err)

		// Create config with key and identity
		config := clientconfig.NewConfig()
		keyName := "test-key"

		config.SetKey(keyName, &clientconfig.KeyConfig{
			Name:        keyName,
			Type:        "ed25519",
			PrivateKey:  privateKeyPEM,
			Fingerprint: keyPair.Fingerprint(),
		})

		identity := &clientconfig.IdentityConfig{
			Type:   "keypair",
			Issuer: "https://test.miren.cloud",
			KeyRef: keyName,
		}

		// Resolve the private key
		resolvedPEM, err := config.GetPrivateKeyPEM(identity)
		require.NoError(t, err)
		assert.Equal(t, privateKeyPEM, resolvedPEM)

		// Verify we can load the keypair from the resolved PEM
		loadedKeyPair, err := cloudauth.LoadKeyPairFromPEM(resolvedPEM)
		require.NoError(t, err)
		assert.Equal(t, keyPair.Fingerprint(), loadedKeyPair.Fingerprint())
	})

	t.Run("falls back to direct PrivateKey for backward compatibility", func(t *testing.T) {
		// Generate a test keypair
		keyPair, err := cloudauth.GenerateKeyPair()
		require.NoError(t, err)

		privateKeyPEM, err := keyPair.PrivateKeyPEM()
		require.NoError(t, err)

		// Create config with identity that has direct PrivateKey (legacy)
		config := clientconfig.NewConfig()
		identity := &clientconfig.IdentityConfig{
			Type:       "keypair",
			Issuer:     "https://test.miren.cloud",
			PrivateKey: privateKeyPEM,
		}

		// Resolve the private key
		resolvedPEM, err := config.GetPrivateKeyPEM(identity)
		require.NoError(t, err)
		assert.Equal(t, privateKeyPEM, resolvedPEM)
	})

	t.Run("returns error when KeyRef does not exist", func(t *testing.T) {
		config := clientconfig.NewConfig()
		identity := &clientconfig.IdentityConfig{
			Type:   "keypair",
			Issuer: "https://test.miren.cloud",
			KeyRef: "nonexistent-key",
		}

		_, err := config.GetPrivateKeyPEM(identity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nonexistent-key")
	})

	t.Run("returns error when no private key available", func(t *testing.T) {
		config := clientconfig.NewConfig()
		identity := &clientconfig.IdentityConfig{
			Type:   "keypair",
			Issuer: "https://test.miren.cloud",
		}

		_, err := config.GetPrivateKeyPEM(identity)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no private key")
	})
}

func TestKeyManagementIntegration(t *testing.T) {
	t.Run("full workflow: create, save, load, and use key", func(t *testing.T) {
		// Setup temp config directory
		tmpDir := t.TempDir()
		os.Setenv("MIREN_CONFIG", tmpDir)
		defer os.Unsetenv("MIREN_CONFIG")

		ctx := newTestContext()
		keyName := "miren-cli"
		identityName := "cloud"
		cloudURL := "https://miren.cloud"

		// Step 1: Create a new key
		keyPair1, err := getOrCreateKey(ctx, keyName)
		require.NoError(t, err)
		fingerprint1 := keyPair1.Fingerprint()

		// Step 2: Save the key to config
		err = saveKeyPairToConfig(identityName, cloudURL, keyPair1, keyName)
		require.NoError(t, err)

		// Step 3: Simulate a new login attempt - should reuse the key
		keyPair2, err := getOrCreateKey(ctx, keyName)
		require.NoError(t, err)
		assert.Equal(t, fingerprint1, keyPair2.Fingerprint(), "Should have same fingerprint")

		// Step 4: Load config and verify we can resolve the private key
		config, err := clientconfig.LoadConfig()
		require.NoError(t, err)

		identity, err := config.GetIdentity(identityName)
		require.NoError(t, err)

		privateKeyPEM, err := config.GetPrivateKeyPEM(identity)
		require.NoError(t, err)

		// Step 5: Verify we can use the resolved key
		loadedKeyPair, err := cloudauth.LoadKeyPairFromPEM(privateKeyPEM)
		require.NoError(t, err)
		assert.Equal(t, fingerprint1, loadedKeyPair.Fingerprint())

		// Sign something with both keypairs to verify they're functionally identical
		message := []byte("test message")
		sig1, err := keyPair1.Sign(message)
		require.NoError(t, err)
		sig2, err := loadedKeyPair.Sign(message)
		require.NoError(t, err)

		// Verify signatures with public keys
		assert.True(t, cloudauth.VerifySignature(keyPair1.PublicKey, message, sig1))
		assert.True(t, cloudauth.VerifySignature(loadedKeyPair.PublicKey, message, sig2))
	})

	t.Run("multiple identities can share the same key", func(t *testing.T) {
		// Setup temp config directory
		tmpDir := t.TempDir()
		os.Setenv("MIREN_CONFIG", tmpDir)
		defer os.Unsetenv("MIREN_CONFIG")

		// Generate a single keypair
		keyPair, err := cloudauth.GenerateKeyPair()
		require.NoError(t, err)
		keyName := "shared-key"

		// Create multiple identities with the same key
		err = saveKeyPairToConfig("identity1", "https://cloud1.miren.cloud", keyPair, keyName)
		require.NoError(t, err)

		err = saveKeyPairToConfig("identity2", "https://cloud2.miren.cloud", keyPair, keyName)
		require.NoError(t, err)

		err = saveKeyPairToConfig("identity3", "https://cloud3.miren.cloud", keyPair, keyName)
		require.NoError(t, err)

		// Verify all identities reference the same key
		config, err := clientconfig.LoadConfig()
		require.NoError(t, err)

		for _, identityName := range []string{"identity1", "identity2", "identity3"} {
			identity, err := config.GetIdentity(identityName)
			require.NoError(t, err)
			assert.Equal(t, keyName, identity.KeyRef)

			// Verify we can resolve the same key for all identities
			privateKeyPEM, err := config.GetPrivateKeyPEM(identity)
			require.NoError(t, err)

			loadedKeyPair, err := cloudauth.LoadKeyPairFromPEM(privateKeyPEM)
			require.NoError(t, err)
			assert.Equal(t, keyPair.Fingerprint(), loadedKeyPair.Fingerprint())
		}

		// Verify only one key exists in config
		keyNames := config.GetKeyNames()
		assert.Len(t, keyNames, 1)
		assert.Contains(t, keyNames, keyName)
	})
}
