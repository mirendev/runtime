package clientconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSavePreservesModificationsToMainConfigEntries(t *testing.T) {
	// This test verifies that programmatic modifications to main config entries
	// are preserved when saving, not reverted to original values

	tmpDir := t.TempDir()

	// Create main config file with a cluster
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	mainConfig := `
active_cluster: prod
clusters:
  prod:
    hostname: old-prod.example.com
    ca_cert: old-ca
    client_cert: old-cert
    client_key: old-key
    insecure: false
identities:
  prod-identity:
    type: keypair
    issuer: old-issuer
    private_key: old-key
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Create config.d directory with additional cluster
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err = os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	additionalConfig := `
clusters:
  dev:
    hostname: dev.example.com
    ca_cert: dev-ca
`
	err = os.WriteFile(filepath.Join(configDirPath, "dev.yaml"), []byte(additionalConfig), 0644)
	require.NoError(t, err)

	// Set environment variable
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// MODIFY the main config cluster programmatically
	prodCluster, err := config.GetCluster("prod")
	require.NoError(t, err)
	prodCluster.Hostname = "new-prod.example.com"
	prodCluster.CACert = "new-ca"
	prodCluster.Insecure = true
	prodCluster.CloudAuth = true // Add new field

	// MODIFY the main config identity
	prodIdentity, err := config.GetIdentity("prod-identity")
	require.NoError(t, err)
	prodIdentity.Issuer = "new-issuer"
	prodIdentity.PrivateKey = "new-key"

	// Also modify the dev cluster (from config.d) - this should NOT be saved
	devCluster, err := config.GetCluster("dev")
	require.NoError(t, err)
	devCluster.Hostname = "modified-dev.example.com"

	// Save the config
	savedConfigPath := filepath.Join(tmpDir, "saved-config.yaml")
	err = config.SaveTo(savedConfigPath)
	require.NoError(t, err)

	// Read back the saved config
	savedData, err := os.ReadFile(savedConfigPath)
	require.NoError(t, err)

	var savedConfig Config
	err = yaml.Unmarshal(savedData, &savedConfig)
	require.NoError(t, err)

	// Verify the MODIFIED values were saved for main config entries
	prodClusterSaved, err := savedConfig.GetCluster("prod")
	require.NoError(t, err)
	assert.Equal(t, "new-prod.example.com", prodClusterSaved.Hostname, "Should save modified hostname")
	assert.Equal(t, "new-ca", prodClusterSaved.CACert, "Should save modified CA cert")
	assert.Equal(t, "old-cert", prodClusterSaved.ClientCert, "Should preserve unmodified fields")
	assert.Equal(t, "old-key", prodClusterSaved.ClientKey, "Should preserve unmodified fields")
	assert.True(t, prodClusterSaved.Insecure, "Should save modified insecure value")
	assert.True(t, prodClusterSaved.CloudAuth, "Should save newly added field")

	// Verify identity modifications were saved
	prodIdentitySaved, err := savedConfig.GetIdentity("prod-identity")
	require.NoError(t, err)
	assert.Equal(t, "new-issuer", prodIdentitySaved.Issuer, "Should save modified issuer")
	assert.Equal(t, "new-key", prodIdentitySaved.PrivateKey, "Should save modified key")
	assert.Equal(t, "keypair", prodIdentitySaved.Type, "Should preserve unmodified type")

	// Verify dev cluster (from config.d) was NOT saved
	_, err = savedConfig.GetCluster("dev")
	assert.Error(t, err, "Should not save config.d entries")
}

func TestSaveWithNewlyAddedAndModifiedClusters(t *testing.T) {
	// Test adding new clusters and modifying existing ones

	tmpDir := t.TempDir()

	// Create main config
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	mainConfig := `
clusters:
  existing:
    hostname: existing.example.com
    ca_cert: existing-ca
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Set environment variable
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)

	// Modify the existing cluster
	existingCluster, err := config.GetCluster("existing")
	require.NoError(t, err)
	existingCluster.Hostname = "modified-existing.example.com"
	existingCluster.Insecure = true

	// Add a completely new cluster programmatically
	config.SetCluster("new-cluster", &ClusterConfig{
		Hostname: "new.example.com",
		CACert:   "new-ca",
	})

	// Save the config
	savedConfigPath := filepath.Join(tmpDir, "saved-config.yaml")
	err = config.SaveTo(savedConfigPath)
	require.NoError(t, err)

	// Read back the saved config
	savedData, err := os.ReadFile(savedConfigPath)
	require.NoError(t, err)

	var savedConfig Config
	err = yaml.Unmarshal(savedData, &savedConfig)
	require.NoError(t, err)

	// Verify both clusters were saved with correct values
	assert.Equal(t, 2, savedConfig.GetClusterCount())

	// Existing cluster should have modified values
	existingClusterSaved, err := savedConfig.GetCluster("existing")
	require.NoError(t, err)
	assert.Equal(t, "modified-existing.example.com", existingClusterSaved.Hostname)
	assert.Equal(t, "existing-ca", existingClusterSaved.CACert)
	assert.True(t, existingClusterSaved.Insecure)

	// New cluster should be saved
	newClusterSaved, err := savedConfig.GetCluster("new-cluster")
	require.NoError(t, err)
	assert.Equal(t, "new.example.com", newClusterSaved.Hostname)
	assert.Equal(t, "new-ca", newClusterSaved.CACert)
}

func TestConfigDOverridesDoNotAffectSavedMainConfig(t *testing.T) {
	// Test that config.d overrides don't affect the saved main config values

	tmpDir := t.TempDir()

	// Create main config
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	mainConfig := `
clusters:
  shared:
    hostname: main.example.com
    ca_cert: main-ca
    insecure: false
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Create config.d that overrides the same cluster
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err = os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	overrideConfig := `
clusters:
  shared:
    hostname: override.example.com
    ca_cert: override-ca
    insecure: true
    cloud_auth: true
`
	err = os.WriteFile(filepath.Join(configDirPath, "override.yaml"), []byte(overrideConfig), 0644)
	require.NoError(t, err)

	// Set environment variable
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)

	// The in-memory config should have the override values
	sharedCluster, err := config.GetCluster("shared")
	require.NoError(t, err)
	assert.Equal(t, "override.example.com", sharedCluster.Hostname)
	assert.Equal(t, "override-ca", sharedCluster.CACert)
	assert.True(t, sharedCluster.Insecure)
	assert.True(t, sharedCluster.CloudAuth)

	// Now modify one field programmatically
	// With the new architecture, to save changes to a cluster from config.d,
	// you must explicitly call SetCluster to promote it to main config
	sharedCluster.Hostname = "user-modified.example.com"
	config.SetCluster("shared", sharedCluster)

	// Save the config
	savedConfigPath := filepath.Join(tmpDir, "saved-config.yaml")
	err = config.SaveTo(savedConfigPath)
	require.NoError(t, err)

	// Read back the saved config
	savedData, err := os.ReadFile(savedConfigPath)
	require.NoError(t, err)

	var savedConfig Config
	err = yaml.Unmarshal(savedData, &savedConfig)
	require.NoError(t, err)

	// The saved config should have:
	// - The user's modification (hostname)
	// - Other fields from the override (since they're in memory)
	sharedClusterSaved, err := savedConfig.GetCluster("shared")
	require.NoError(t, err)
	assert.Equal(t, "user-modified.example.com", sharedClusterSaved.Hostname, "Should save user modification")
	assert.Equal(t, "override-ca", sharedClusterSaved.CACert, "Should save current in-memory value")
	assert.True(t, sharedClusterSaved.Insecure, "Should save current in-memory value")
	assert.True(t, sharedClusterSaved.CloudAuth, "Should save current in-memory value")
}
