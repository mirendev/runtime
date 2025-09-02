package clientconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_RealWorldScenario(t *testing.T) {
	// This test simulates a real-world scenario where:
	// 1. User has no main clientconfig.yaml file
	// 2. User only has a file in clientconfig.d/
	// 3. Config should load successfully

	// Create a temporary HOME directory to simulate user environment
	tmpHome := t.TempDir()

	// Create the .config/miren directory structure
	configDir := filepath.Join(tmpHome, ".config", "miren")
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Create clientconfig.d directory
	configDDir := filepath.Join(configDir, "clientconfig.d")
	err = os.MkdirAll(configDDir, 0755)
	require.NoError(t, err)

	// DO NOT create clientconfig.yaml - simulating the issue scenario

	// Create a realistic config file in clientconfig.d
	clusterConfig := `
clusters:
  miren-cloud:
    hostname: api.miren.cloud
    ca_cert: |
      -----BEGIN CERTIFICATE-----
      MIIDQTCCAimgAwIBAgITBmyfz5m/jAo54vB4ikPmljZbyjANBgkqhkiG9w0BAQsF
      ADA5MQswCQYDVQQGEwJVUzEPMA0GA1UEChMGQW1hem9uMRkwFwYDVQQDExBBbWF6
      -----END CERTIFICATE-----
    cloud_auth: true
active_cluster: miren-cloud
`
	err = os.WriteFile(filepath.Join(configDDir, "miren-cloud.yaml"), []byte(clusterConfig), 0644)
	require.NoError(t, err)

	// Clear any existing env var and use default path
	oldEnv := os.Getenv(EnvConfigPath)
	os.Unsetenv(EnvConfigPath)
	defer func() {
		if oldEnv != "" {
			os.Setenv(EnvConfigPath, oldEnv)
		}
	}()

	// Override home directory for this test
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", oldHome)

	// Verify that the main config file does NOT exist
	mainConfigPath := filepath.Join(configDir, "clientconfig.yaml")
	_, err = os.Stat(mainConfigPath)
	assert.True(t, os.IsNotExist(err), "Main config file should not exist")

	// Verify that the config.d file DOES exist
	configDFile := filepath.Join(configDDir, "miren-cloud.yaml")
	_, err = os.Stat(configDFile)
	require.NoError(t, err, "Config.d file should exist")

	// Load the config - this should work
	config, err := LoadConfig()
	require.NoError(t, err, "Should load config successfully from clientconfig.d only")
	require.NotNil(t, config)

	// Verify the configuration was loaded correctly
	assert.Equal(t, "miren-cloud", config.ActiveCluster)
	assert.Len(t, config.Clusters, 1)

	cluster, err := config.GetCluster("miren-cloud")
	require.NoError(t, err)
	assert.Equal(t, "api.miren.cloud", cluster.Hostname)
	assert.True(t, cluster.CloudAuth)
	assert.Contains(t, cluster.CACert, "BEGIN CERTIFICATE")
}

func TestLoadConfig_WithEnvVarPointingToNonExistentFile(t *testing.T) {
	// Test the specific case where MIREN_CONFIG env var points to a non-existent file
	// but clientconfig.d exists with valid configs

	tmpDir := t.TempDir()

	// Set up the directory structure relative to the env var path
	configPath := filepath.Join(tmpDir, "custom", "path", "clientconfig.yaml")
	configDir := filepath.Dir(configPath)
	configDDir := filepath.Join(configDir, "clientconfig.d")

	err := os.MkdirAll(configDDir, 0755)
	require.NoError(t, err)

	// Create a config in clientconfig.d
	config := `
clusters:
  test-cluster:
    hostname: test.example.com
    insecure: true
active_cluster: test-cluster
`
	err = os.WriteFile(filepath.Join(configDDir, "test.yaml"), []byte(config), 0644)
	require.NoError(t, err)

	// Set MIREN_CONFIG to point to the non-existent main config
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, configPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Verify main config doesn't exist
	_, err = os.Stat(configPath)
	assert.True(t, os.IsNotExist(err), "Main config should not exist")

	// Verify config.d exists and has files
	entries, err := os.ReadDir(configDDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "Should have one file in config.d")

	// Load the config
	loadedConfig, err := LoadConfig()
	require.NoError(t, err, "Should successfully load from config.d when main file doesn't exist")
	require.NotNil(t, loadedConfig)

	// Verify it loaded correctly
	assert.Equal(t, "test-cluster", loadedConfig.ActiveCluster)
	assert.Len(t, loadedConfig.Clusters, 1)

	cluster, err := loadedConfig.GetCluster("test-cluster")
	require.NoError(t, err)
	assert.Equal(t, "test.example.com", cluster.Hostname)
	assert.True(t, cluster.Insecure)
}
