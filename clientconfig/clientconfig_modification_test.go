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
	os.Setenv(EnvConfigPath, tmpDir)
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
	assert.Equal(t, IdentityKeypair, prodIdentitySaved.Type, "Should preserve unmodified type")

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
	os.Setenv(EnvConfigPath, tmpDir)
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
	os.Setenv(EnvConfigPath, tmpDir)
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

func TestRemoveClusterHandlesLeafConfigs(t *testing.T) {
	// Test that RemoveCluster properly handles clusters in both main and leaf configs

	tmpDir := t.TempDir()

	// Create main config with a cluster
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	mainConfig := `
active_cluster: main-cluster
clusters:
  main-cluster:
    hostname: main.example.com
  other-main:
    hostname: other.example.com
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Create config.d with leaf clusters
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err = os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	leafConfig := `
clusters:
  leaf-cluster:
    hostname: leaf.example.com
  another-leaf:
    hostname: another-leaf.example.com
`
	err = os.WriteFile(filepath.Join(configDirPath, "leaf.yaml"), []byte(leafConfig), 0644)
	require.NoError(t, err)

	// Set environment variable
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, tmpDir)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)

	// Verify initial state
	assert.True(t, config.HasCluster("main-cluster"))
	assert.True(t, config.HasCluster("other-main"))
	assert.True(t, config.HasCluster("leaf-cluster"))
	assert.True(t, config.HasCluster("another-leaf"))
	assert.Equal(t, 4, config.GetClusterCount())

	// Test 1: Remove a cluster from main config (should succeed)
	err = config.RemoveCluster("other-main")
	assert.NoError(t, err, "Should successfully remove cluster from main config")
	assert.False(t, config.HasCluster("other-main"))
	assert.Equal(t, 3, config.GetClusterCount())

	// Test 2: Remove a cluster from leaf config (should succeed)
	err = config.RemoveCluster("leaf-cluster")
	assert.NoError(t, err, "Should successfully remove cluster from leaf config")
	assert.False(t, config.HasCluster("leaf-cluster"), "Cluster should be removed")
	assert.Equal(t, 2, config.GetClusterCount())

	// Test 3: Try to remove non-existent cluster (should fail)
	err = config.RemoveCluster("does-not-exist")
	assert.Error(t, err, "Should fail to remove non-existent cluster")
	assert.Contains(t, err.Error(), "not found", "Error should mention not found")

	// Test 4: Remove the active cluster, which takes the active pointer with it
	require.Equal(t, "main-cluster", config.ActiveCluster())
	err = config.RemoveCluster("main-cluster")
	assert.NoError(t, err, "Should successfully remove the active cluster")
	assert.False(t, config.HasCluster("main-cluster"))
	assert.Equal(t, "", config.ActiveCluster(), "active must not name a removed cluster")
	assert.Equal(t, 1, config.GetClusterCount())

	// Test 6: Verify the leaf config removal persists after save
	err = config.Save()
	require.NoError(t, err, "Should save successfully")

	// Reload and verify the leaf cluster is still gone
	config2, err := LoadConfig()
	require.NoError(t, err)
	assert.False(t, config2.HasCluster("leaf-cluster"), "Removed leaf cluster should not reappear")
	assert.True(t, config2.HasCluster("another-leaf"), "Other leaf cluster should still exist")
	assert.False(t, config2.HasCluster("main-cluster"), "Removed main cluster should not reappear")
	assert.False(t, config2.HasCluster("other-main"), "Removed main cluster should not reappear")
	assert.Equal(t, "", config2.ActiveCluster(), "active must not be restored to a removed cluster")
}

// TestRemoveClusterDeletesEmptiedLeafFile verifies that removing the last
// cluster from a leaf config deletes its file on save, rather than leaving an
// empty "{}" document behind.
func TestRemoveClusterDeletesEmptiedLeafFile(t *testing.T) {
	tmpDir := t.TempDir()

	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	mainConfig := "active_cluster: main\nclusters:\n  main:\n    hostname: main.example.com\n"
	require.NoError(t, os.WriteFile(mainConfigPath, []byte(mainConfig), 0644))

	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	require.NoError(t, os.MkdirAll(configDirPath, 0755))
	leafPath := filepath.Join(configDirPath, "50-local.yaml")
	leafConfig := "clusters:\n  local:\n    hostname: localhost:8443\n"
	require.NoError(t, os.WriteFile(leafPath, []byte(leafConfig), 0644))

	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, tmpDir)
	defer os.Setenv(EnvConfigPath, oldEnv)

	config, err := LoadConfig()
	require.NoError(t, err)
	require.True(t, config.HasCluster("local"))

	// Removing the leaf's only cluster empties it; saving should delete the file.
	require.NoError(t, config.RemoveCluster("local"))
	require.NoError(t, config.Save())

	_, statErr := os.Stat(leafPath)
	assert.True(t, os.IsNotExist(statErr), "emptied leaf file should be removed, stat err was: %v", statErr)
}

// TestClearActiveClusterAllowsRemovingIt verifies that clearing the active
// cluster unsets it and leaves the rest of the config alone. This is the
// sequence uninstall relies on.
func TestClearActiveClusterAllowsRemovingIt(t *testing.T) {
	config := NewConfig()
	config.SetCluster("local", &ClusterConfig{Hostname: "localhost:8443"})
	config.SetCluster("cloud", &ClusterConfig{Hostname: "cloud.example.com"})
	require.NoError(t, config.SetActiveCluster("local"))

	config.ClearActiveCluster()
	assert.Equal(t, "", config.ActiveCluster())
	require.NoError(t, config.RemoveCluster("local"))
	assert.False(t, config.HasCluster("local"))
	assert.True(t, config.HasCluster("cloud"), "other clusters are untouched")
}

// TestRemoveActiveCluster covers the case that used to be a dead end: with one
// cluster configured there is nowhere to switch to first, so refusing to remove
// the active cluster meant it could never be removed at all.
func TestRemoveActiveCluster(t *testing.T) {
	t.Run("the only cluster can be removed", func(t *testing.T) {
		config := NewConfig()
		config.SetCluster("homelab", &ClusterConfig{Hostname: "homelab:8443"})
		require.NoError(t, config.SetActiveCluster("homelab"))

		require.NoError(t, config.RemoveCluster("homelab"))
		assert.False(t, config.HasCluster("homelab"))
		assert.Equal(t, "", config.ActiveCluster(), "active must not name a removed cluster")
	})

	t.Run("removing the active one leaves the others alone", func(t *testing.T) {
		config := NewConfig()
		config.SetCluster("homelab", &ClusterConfig{Hostname: "homelab:8443"})
		config.SetCluster("cloud", &ClusterConfig{Hostname: "cloud.example.com"})
		require.NoError(t, config.SetActiveCluster("homelab"))

		require.NoError(t, config.RemoveCluster("homelab"))
		assert.Equal(t, "", config.ActiveCluster())
		assert.True(t, config.HasCluster("cloud"))
	})

	t.Run("a failed removal leaves the active pointer intact", func(t *testing.T) {
		config := NewConfig()
		config.SetCluster("homelab", &ClusterConfig{Hostname: "homelab:8443"})
		require.NoError(t, config.SetActiveCluster("homelab"))

		require.Error(t, config.RemoveCluster("nonexistent"))
		assert.Equal(t, "homelab", config.ActiveCluster())
	})
}

// A leaf config carries its own active_cluster and it wins over the main file's,
// so removing the cluster it names has to clear both. Missing the leaf wrote it
// back naming a cluster that no longer existed, and the next load refused the
// entire configuration. The earlier leaf test passed only because its fixture
// happened not to set active_cluster.
func TestRemoveActiveClusterDefinedInLeaf(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clientconfig.yaml"),
		[]byte("clusters:\n  other:\n    hostname: other.example.com\n"), 0644))

	leafDir := filepath.Join(dir, "clientconfig.d")
	require.NoError(t, os.MkdirAll(leafDir, 0755))
	leafPath := filepath.Join(leafDir, "homelab.yaml")
	require.NoError(t, os.WriteFile(leafPath,
		[]byte("active_cluster: homelab\nclusters:\n  homelab:\n    hostname: homelab:8443\n"), 0644))

	t.Setenv(EnvConfigPath, dir)

	config, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "homelab", config.ActiveCluster(), "the leaf's active_cluster should be in force")

	require.NoError(t, config.RemoveCluster("homelab"))
	assert.Equal(t, "", config.ActiveCluster())
	require.NoError(t, config.Save())

	// The reload is the real assertion: a leaf still naming the removed cluster
	// fails Validate and takes the whole configuration down with it.
	reloaded, err := LoadConfig()
	require.NoError(t, err, "configuration must still load after removing the active cluster")
	assert.Equal(t, "", reloaded.ActiveCluster(), "active must not name a removed cluster")
	assert.False(t, reloaded.HasCluster("homelab"))
	assert.True(t, reloaded.HasCluster("other"), "other clusters are untouched")
}

// The leaf holding the active pointer isn't necessarily the leaf that owns the
// cluster. Here the main config owns it and a separate leaf carries only
// active_cluster, so clearing in memory reached the right struct but nothing
// queued that leaf for save and the stale pointer survived on disk.
func TestRemoveActiveClusterPointedAtByAnotherLeaf(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clientconfig.yaml"),
		[]byte("clusters:\n  homelab:\n    hostname: homelab:8443\n"), 0644))

	leafDir := filepath.Join(dir, "clientconfig.d")
	require.NoError(t, os.MkdirAll(leafDir, 0755))
	pointer := filepath.Join(leafDir, "pointer.yaml")
	require.NoError(t, os.WriteFile(pointer, []byte("active_cluster: homelab\n"), 0644))

	t.Setenv(EnvConfigPath, dir)

	config, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "homelab", config.ActiveCluster())

	require.NoError(t, config.RemoveCluster("homelab"))
	require.NoError(t, config.Save())

	reloaded, err := LoadConfig()
	require.NoError(t, err, "a stale pointer in an unrelated leaf must not survive the save")
	assert.Equal(t, "", reloaded.ActiveCluster())
	assert.False(t, reloaded.HasCluster("homelab"))
}
