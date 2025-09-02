package clientconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_OnlyConfigDirNoMainFile(t *testing.T) {
	// This test verifies that config loads correctly when only clientconfig.d
	// files exist and there is no main clientconfig.yaml file

	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// DO NOT create main config file - this is the key difference
	// We want to test the case where clientconfig.yaml doesn't exist at all

	// Create config.d directory
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err := os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	// Create a config file in config.d
	clusterConfig := `
clusters:
  production:
    hostname: prod.example.com
    ca_cert: |
      -----BEGIN CERTIFICATE-----
      PROD-CA-CERT
      -----END CERTIFICATE-----
    client_cert: |
      -----BEGIN CERTIFICATE-----
      PROD-CLIENT-CERT
      -----END CERTIFICATE-----
    client_key: |
      -----BEGIN RSA PRIVATE KEY-----
      PROD-CLIENT-KEY
      -----END RSA PRIVATE KEY-----
active_cluster: production
`
	err = os.WriteFile(filepath.Join(configDirPath, "production.yaml"), []byte(clusterConfig), 0644)
	require.NoError(t, err)

	// Set environment variable to point to non-existent main config
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Verify the main config file does NOT exist
	_, err = os.Stat(mainConfigPath)
	assert.True(t, os.IsNotExist(err), "Main config file should not exist")

	// Load the config - this should work even without main config file
	config, err := LoadConfig()
	require.NoError(t, err, "Should be able to load config from clientconfig.d even without main config file")
	require.NotNil(t, config)

	// Verify the config was loaded correctly
	assert.Equal(t, "production", config.ActiveCluster)
	assert.Len(t, config.Clusters, 1)

	// Verify cluster details
	prodCluster, err := config.GetCluster("production")
	require.NoError(t, err)
	assert.Equal(t, "prod.example.com", prodCluster.Hostname)
	assert.Contains(t, prodCluster.CACert, "PROD-CA-CERT")
	assert.Contains(t, prodCluster.ClientCert, "PROD-CLIENT-CERT")
	assert.Contains(t, prodCluster.ClientKey, "PROD-CLIENT-KEY")
}

func TestLoadConfig_MultipleConfigDFilesNoMainFile(t *testing.T) {
	// Test with multiple files in clientconfig.d and no main config

	tmpDir := t.TempDir()

	// Create config.d directory
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err := os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	// Create first config file
	config1 := `
clusters:
  cluster1:
    hostname: cluster1.example.com
    ca_cert: ca1
identities:
  identity1:
    type: keypair
    issuer: issuer1
    private_key: key1
`
	err = os.WriteFile(filepath.Join(configDirPath, "01-cluster1.yaml"), []byte(config1), 0644)
	require.NoError(t, err)

	// Create second config file with active cluster
	config2 := `
clusters:
  cluster2:
    hostname: cluster2.example.com
    ca_cert: ca2
    identity: identity1
active_cluster: cluster2
`
	err = os.WriteFile(filepath.Join(configDirPath, "02-cluster2.yaml"), []byte(config2), 0644)
	require.NoError(t, err)

	// Set environment variable
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Verify main config doesn't exist
	_, err = os.Stat(mainConfigPath)
	assert.True(t, os.IsNotExist(err))

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify both clusters were loaded
	assert.Len(t, config.Clusters, 2)
	assert.Contains(t, config.Clusters, "cluster1")
	assert.Contains(t, config.Clusters, "cluster2")

	// Verify identity was loaded
	assert.Len(t, config.Identities, 1)
	assert.Contains(t, config.Identities, "identity1")

	// Verify active cluster was set from config.d
	assert.Equal(t, "cluster2", config.ActiveCluster)

	// Verify cluster2 references identity1
	cluster2, err := config.GetCluster("cluster2")
	require.NoError(t, err)
	assert.Equal(t, "identity1", cluster2.Identity)
}

func TestLoadConfig_EmptyConfigDirNoMainFile(t *testing.T) {
	// Test the case where config.d exists but is empty and no main config

	tmpDir := t.TempDir()

	// Create empty config.d directory
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err := os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	// Set environment variable
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config - should return ErrNoConfig
	config, err := LoadConfig()
	assert.ErrorIs(t, err, ErrNoConfig, "Should return ErrNoConfig when both main config and config.d are empty")
	assert.Nil(t, config)
}
