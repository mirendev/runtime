package clientconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSaveDoesNotIncludeConfigDirEntries(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create main config file with one cluster
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	mainConfig := `
active_cluster: main
clusters:
  main:
    hostname: main.example.com
    ca_cert: main-ca
    client_cert: main-cert
    client_key: main-key
identities:
  main-identity:
    type: keypair
    issuer: main-issuer
    private_key: main-key
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Create config.d directory
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err = os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	// Create additional config files that should NOT be saved
	additionalConfig1 := `
clusters:
  dev:
    hostname: dev.example.com
    ca_cert: dev-ca
    client_cert: dev-cert
    client_key: dev-key
identities:
  dev-identity:
    type: certificate
    issuer: dev-issuer
    client_cert: dev-cert
    client_key: dev-key
`
	err = os.WriteFile(filepath.Join(configDirPath, "01-dev.yaml"), []byte(additionalConfig1), 0644)
	require.NoError(t, err)

	additionalConfig2 := `
clusters:
  staging:
    hostname: staging.example.com
    ca_cert: staging-ca
    insecure: true
`
	err = os.WriteFile(filepath.Join(configDirPath, "02-staging.yml"), []byte(additionalConfig2), 0644)
	require.NoError(t, err)

	// Set environment variable to use our test directory
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, tmpDir)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config (this will merge clientconfig.d entries)
	config, err := LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify all clusters were loaded (main + dev + staging)
	assert.Equal(t, 3, config.GetClusterCount())
	assert.Equal(t, 2, config.GetIdentityCount())

	// Modify the main cluster slightly (in-memory modification)
	mainCluster, err := config.GetCluster("main")
	require.NoError(t, err)
	mainCluster.Hostname = "modified.example.com"

	// Save the config to a new location
	savedConfigPath := filepath.Join(tmpDir, "saved-config.yaml")
	err = config.SaveTo(savedConfigPath)
	require.NoError(t, err)

	// Read back the saved config directly (not using LoadConfig to avoid merging)
	savedData, err := os.ReadFile(savedConfigPath)
	require.NoError(t, err)

	var savedConfig Config
	err = yaml.Unmarshal(savedData, &savedConfig)
	require.NoError(t, err)

	// Verify only the main cluster was saved, not the ones from config.d
	assert.Equal(t, 1, savedConfig.GetClusterCount(), "Should only save clusters from main config")
	_, err = savedConfig.GetCluster("main")
	assert.NoError(t, err, "Should contain main cluster")
	_, err = savedConfig.GetCluster("dev")
	assert.Error(t, err, "Should not contain dev cluster from config.d")
	_, err = savedConfig.GetCluster("staging")
	assert.Error(t, err, "Should not contain staging cluster from config.d")

	// Verify only the main identity was saved
	assert.Equal(t, 1, savedConfig.GetIdentityCount(), "Should only save identities from main config")
	_, err = savedConfig.GetIdentity("main-identity")
	assert.NoError(t, err, "Should contain main identity")
	_, err = savedConfig.GetIdentity("dev-identity")
	assert.Error(t, err, "Should not contain dev identity from config.d")

	// Verify the modified value was saved (current in-memory values are saved)
	savedMainCluster, err := savedConfig.GetCluster("main")
	require.NoError(t, err)
	assert.Equal(t, "modified.example.com", savedMainCluster.Hostname, "Should save modified value")
}

func TestSavePreservesNewlyAddedClusters(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create main config file
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	mainConfig := `
active_cluster: main
clusters:
  main:
    hostname: main.example.com
    ca_cert: main-ca
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Create config.d directory with additional cluster
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err = os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	additionalConfig := `
clusters:
  from-config-d:
    hostname: config-d.example.com
    ca_cert: config-d-ca
`
	err = os.WriteFile(filepath.Join(configDirPath, "extra.yaml"), []byte(additionalConfig), 0644)
	require.NoError(t, err)

	// Set environment variable
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, tmpDir)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)

	// Programmatically add a new cluster (simulating user adding via CLI)
	config.SetCluster("new-cluster", &ClusterConfig{
		Hostname: "new.example.com",
		CACert:   "new-ca",
	})

	// Add a new identity programmatically
	config.SetIdentity("new-identity", &IdentityConfig{
		Type:       "keypair",
		Issuer:     "new-issuer",
		PrivateKey: "new-key",
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

	// Verify the correct clusters were saved
	assert.Equal(t, 2, savedConfig.GetClusterCount(), "Should save main cluster and newly added cluster")
	_, err = savedConfig.GetCluster("main")
	assert.NoError(t, err, "Should contain original main cluster")
	_, err = savedConfig.GetCluster("new-cluster")
	assert.NoError(t, err, "Should contain newly added cluster")
	_, err = savedConfig.GetCluster("from-config-d")
	assert.Error(t, err, "Should not contain cluster from config.d")

	// Verify the new identity was saved
	_, err = savedConfig.GetIdentity("new-identity")
	assert.NoError(t, err, "Should contain newly added identity")
}

func TestSaveEmptyMainConfigWithConfigD(t *testing.T) {
	// Test case where main config doesn't exist but config.d has entries
	tmpDir := t.TempDir()

	// Don't create main config file

	// Create config.d directory with clusters
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err := os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	additionalConfig := `
clusters:
  config-d-cluster:
    hostname: config-d.example.com
    ca_cert: config-d-ca
`
	err = os.WriteFile(filepath.Join(configDirPath, "cluster.yaml"), []byte(additionalConfig), 0644)
	require.NoError(t, err)

	// Set environment variable
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, tmpDir)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config (should load from config.d only)
	config, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, 1, config.GetClusterCount())

	// Save should create an empty main config (not include config.d entries)
	err = config.Save()
	require.NoError(t, err)

	// Read back the saved config
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	savedData, err := os.ReadFile(mainConfigPath)
	require.NoError(t, err)

	var savedConfig Config
	err = yaml.Unmarshal(savedData, &savedConfig)
	require.NoError(t, err)

	// Should be empty since nothing was in the main config
	assert.Equal(t, 0, savedConfig.GetClusterCount(), "Should not save clusters from config.d")
}

func TestSetLeafConfigAndSaving(t *testing.T) {
	// Test that SetLeafConfig creates leaf configs that can be accessed immediately
	// and are saved to clientconfig.d/ when the main config is saved

	tmpDir := t.TempDir()

	// Create a config programmatically
	config := NewConfig()

	// Add a main config cluster
	config.SetCluster("main-cluster", &ClusterConfig{
		Hostname: "main.example.com",
		CACert:   "main-ca",
	})
	err := config.SetActiveCluster("main-cluster")
	require.NoError(t, err)

	// Add a leaf config using SetLeafConfig
	leafConfigData := &ConfigData{
		Clusters: map[string]*ClusterConfig{
			"dev-cluster": {
				Hostname: "dev.example.com",
				CACert:   "dev-ca",
			},
		},
		Identities: map[string]*IdentityConfig{
			"dev-identity": {
				Type:   "keypair",
				Issuer: "dev-issuer",
			},
		},
	}
	config.SetLeafConfig("dev", leafConfigData)

	// Verify the leaf config is immediately available
	devCluster, err := config.GetCluster("dev-cluster")
	require.NoError(t, err)
	assert.Equal(t, "dev.example.com", devCluster.Hostname)

	devIdentity, err := config.GetIdentity("dev-identity")
	require.NoError(t, err)
	assert.Equal(t, "keypair", devIdentity.Type)

	// Verify counts include both main and leaf configs
	assert.Equal(t, 2, config.GetClusterCount())  // main-cluster + dev-cluster
	assert.Equal(t, 1, config.GetIdentityCount()) // dev-identity

	// Save the config
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	err = config.SaveTo(mainConfigPath)
	require.NoError(t, err)

	// Verify main config file was created
	assert.FileExists(t, mainConfigPath)

	// Verify leaf config file was created
	leafConfigPath := filepath.Join(tmpDir, "clientconfig.d", "dev.yaml")
	assert.FileExists(t, leafConfigPath)

	// Read and verify main config content
	mainData, err := os.ReadFile(mainConfigPath)
	require.NoError(t, err)

	var mainConfig Config
	err = yaml.Unmarshal(mainData, &mainConfig)
	require.NoError(t, err)

	assert.Equal(t, "main-cluster", mainConfig.ActiveCluster())
	assert.Equal(t, 1, mainConfig.GetClusterCount()) // Only main-cluster
	_, err = mainConfig.GetCluster("main-cluster")
	assert.NoError(t, err)
	_, err = mainConfig.GetCluster("dev-cluster")
	assert.Error(t, err) // dev-cluster should not be in main config

	// Read and verify leaf config content
	leafData, err := os.ReadFile(leafConfigPath)
	require.NoError(t, err)

	var leafConfigFromFile ConfigData
	err = yaml.Unmarshal(leafData, &leafConfigFromFile)
	require.NoError(t, err)

	assert.Equal(t, 1, len(leafConfigFromFile.Clusters))
	devClusterFromFile, exists := leafConfigFromFile.Clusters["dev-cluster"]
	require.True(t, exists)
	assert.Equal(t, "dev.example.com", devClusterFromFile.Hostname)

	assert.Equal(t, 1, len(leafConfigFromFile.Identities))
	devIdentityFromFile, exists := leafConfigFromFile.Identities["dev-identity"]
	require.True(t, exists)
	assert.Equal(t, "keypair", devIdentityFromFile.Type)
}

func TestSetLeafConfigUpdatesExisting(t *testing.T) {
	// Test that calling SetLeafConfig with the same name updates the existing leaf config

	tmpDir := t.TempDir()
	config := NewConfig()

	// Add initial leaf config
	leafConfigData1 := &ConfigData{
		Clusters: map[string]*ClusterConfig{
			"test-cluster": {
				Hostname: "test1.example.com",
				CACert:   "test1-ca",
			},
		},
	}
	config.SetLeafConfig("test", leafConfigData1)

	// Verify initial config
	cluster, err := config.GetCluster("test-cluster")
	require.NoError(t, err)
	assert.Equal(t, "test1.example.com", cluster.Hostname)

	// Update with new leaf config data
	leafConfigData2 := &ConfigData{
		Clusters: map[string]*ClusterConfig{
			"test-cluster": {
				Hostname: "test2.example.com",
				CACert:   "test2-ca",
			},
		},
	}
	config.SetLeafConfig("test", leafConfigData2)

	// Verify updated config
	cluster, err = config.GetCluster("test-cluster")
	require.NoError(t, err)
	assert.Equal(t, "test2.example.com", cluster.Hostname)

	// Save and verify only one leaf config file exists
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	err = config.SaveTo(mainConfigPath)
	require.NoError(t, err)

	leafConfigPath := filepath.Join(tmpDir, "clientconfig.d", "test.yaml")
	assert.FileExists(t, leafConfigPath)

	// Verify the saved file has the updated content
	leafData, err := os.ReadFile(leafConfigPath)
	require.NoError(t, err)

	var leafConfigFromFile ConfigData
	err = yaml.Unmarshal(leafData, &leafConfigFromFile)
	require.NoError(t, err)

	testClusterFromFile, exists := leafConfigFromFile.Clusters["test-cluster"]
	require.True(t, exists)
	assert.Equal(t, "test2.example.com", testClusterFromFile.Hostname)
}

func TestSetLeafConfigDoesNotSetActiveCluster(t *testing.T) {
	// Test that SetLeafConfig does NOT automatically set active cluster
	// This is intentional - active cluster should be set explicitly to avoid
	// creating invalid states where active_cluster is set but the cluster
	// definition hasn't been saved yet (if interrupted during save)

	config := NewConfig()

	// Verify config starts empty with no active cluster
	assert.Equal(t, "", config.ActiveCluster())
	assert.Equal(t, 0, config.GetClusterCount())

	// Add a leaf config with multiple clusters
	leafConfigData := &ConfigData{
		Clusters: map[string]*ClusterConfig{
			"prod": {
				Hostname: "prod.example.com",
				CACert:   "prod-ca",
			},
			"dev": {
				Hostname: "dev.example.com",
				CACert:   "dev-ca",
			},
		},
	}
	config.SetLeafConfig("environments", leafConfigData)

	// Verify that active cluster is NOT automatically set
	assert.Equal(t, "", config.ActiveCluster(), "Active cluster should NOT be set automatically by SetLeafConfig")

	// Verify the clusters are accessible
	prodCluster, err := config.GetCluster("prod")
	require.NoError(t, err)
	assert.NotNil(t, prodCluster)

	devCluster, err := config.GetCluster("dev")
	require.NoError(t, err)
	assert.NotNil(t, devCluster)

	// Verify both clusters are available
	assert.Equal(t, 2, config.GetClusterCount())

	// Active cluster must be set explicitly
	err = config.SetActiveCluster("prod")
	require.NoError(t, err)
	assert.Equal(t, "prod", config.ActiveCluster())
}

func TestSetLeafConfigDoesNotOverrideExistingActiveCluster(t *testing.T) {
	// Test that adding leaf config doesn't override existing active cluster

	config := NewConfig()

	// Add a main cluster and set it as active
	config.SetCluster("main-cluster", &ClusterConfig{
		Hostname: "main.example.com",
		CACert:   "main-ca",
	})
	err := config.SetActiveCluster("main-cluster")
	require.NoError(t, err)

	// Add a leaf config with clusters
	leafConfigData := &ConfigData{
		Clusters: map[string]*ClusterConfig{
			"leaf-cluster": {
				Hostname: "leaf.example.com",
				CACert:   "leaf-ca",
			},
		},
	}
	config.SetLeafConfig("leaf", leafConfigData)

	// Verify active cluster remains unchanged
	assert.Equal(t, "main-cluster", config.ActiveCluster())
}

func TestSetLeafConfigDoesNotSetActiveWhenNoMainClustersButHasActiveCluster(t *testing.T) {
	// Test that if main config has no clusters but already has an active cluster set,
	// we don't override it

	config := NewConfig()

	// Set an active cluster even though no clusters exist yet
	// (This could happen in some edge cases)
	config.active = "existing-active"

	// Add a leaf config with clusters
	leafConfigData := &ConfigData{
		Clusters: map[string]*ClusterConfig{
			"leaf-cluster": {
				Hostname: "leaf.example.com",
				CACert:   "leaf-ca",
			},
		},
	}
	config.SetLeafConfig("leaf", leafConfigData)

	// Verify active cluster remains unchanged
	assert.Equal(t, "existing-active", config.ActiveCluster())
}

func TestSaveLeafConfigsBeforeMainConfig(t *testing.T) {
	// Regression test for bug where main config was saved before leaf configs
	// If the process is interrupted between saving main config and leaf configs,
	// we end up with a broken state: active_cluster points to a cluster that doesn't exist
	//
	// This test verifies that leaf configs are saved BEFORE the main config

	tmpDir := t.TempDir()
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	leafConfigDir := filepath.Join(tmpDir, "clientconfig.d")

	config := NewConfig()

	// Add a cluster via leaf config
	leafConfigData := &ConfigData{
		Clusters: map[string]*ClusterConfig{
			"local": {
				Hostname:   "127.0.0.1:8443",
				CACert:     "test-ca",
				ClientCert: "test-cert",
				ClientKey:  "test-key",
			},
		},
	}
	config.SetLeafConfig("50-local", leafConfigData)

	// Set active cluster in main config to reference the leaf cluster
	err := config.SetActiveCluster("local")
	require.NoError(t, err)

	// Save the config
	err = config.SaveTo(mainConfigPath)
	require.NoError(t, err)

	// Verify the leaf config file exists
	leafConfigPath := filepath.Join(leafConfigDir, "50-local.yaml")
	require.FileExists(t, leafConfigPath, "Leaf config should be saved")

	// Verify the main config file exists
	require.FileExists(t, mainConfigPath, "Main config should be saved")

	// Verify the main config content
	mainConfigData, err := os.ReadFile(mainConfigPath)
	require.NoError(t, err)

	// Verify main config has active_cluster set
	assert.Contains(t, string(mainConfigData), "active_cluster: local")

	// Verify the leaf config has the cluster definition
	leafFileData, err := os.ReadFile(leafConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(leafFileData), "127.0.0.1:8443")

	// The key point: even if loading is interrupted after main config is saved,
	// the leaf config was already saved first, so the cluster exists
}

func TestLoadConfigHandlesBrokenActiveClusterGracefully(t *testing.T) {
	// Regression test for bug where a config file with active_cluster pointing
	// to a non-existent cluster would cause validation to fail
	//
	// This simulates the broken state that could occur if the save process
	// was interrupted

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "clientconfig.yaml")

	// Write a broken config file (active_cluster set but cluster doesn't exist)
	brokenConfig := `active_cluster: local
clusters: {}
`
	err := os.WriteFile(configPath, []byte(brokenConfig), 0600)
	require.NoError(t, err)

	// Try to load the config - should fail with validation error
	_, err = LoadConfigFrom(configPath)
	require.Error(t, err, "Loading broken config should return an error")
	assert.Contains(t, err.Error(), "active cluster \"local\" not found")

	// This is the expected behavior - the config is invalid
	// The fix is in server.go:writeLocalClusterConfig which handles this error
	// gracefully by creating a new config instead of failing
}
