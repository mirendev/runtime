package clientconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigLoadSave(t *testing.T) {
	r := require.New(t)

	// Create temporary directory for test
	tmpDir, err := os.MkdirTemp("", "config-test")
	r.NoError(err)
	defer os.RemoveAll(tmpDir)

	// Set environment variable to use temporary directory
	configPath := filepath.Join(tmpDir, "config.yaml")
	os.Setenv(EnvConfigPath, configPath)
	defer os.Unsetenv(EnvConfigPath)

	// Create test configuration
	originalConfig := &Config{
		Clusters: map[string]*ClusterConfig{
			"prod": {
				Hostname: "prod.example.com",
				CACert:   "-----BEGIN CERTIFICATE-----\nMIIE...\n-----END CERTIFICATE-----",
			},
			"staging": {
				Hostname: "staging.example.com",
				CACert:   "-----BEGIN CERTIFICATE-----\nMIIE...\n-----END CERTIFICATE-----",
			},
		},
	}

	// Save configuration
	err = originalConfig.Save()
	r.NoError(err)

	// Load configuration
	loadedConfig, err := LoadConfig()
	r.NoError(err)

	// Verify loaded configuration matches original
	r.Equal(originalConfig.Clusters["prod"].Hostname, loadedConfig.Clusters["prod"].Hostname)
	r.Equal(originalConfig.Clusters["prod"].CACert, loadedConfig.Clusters["prod"].CACert)
	r.Equal(originalConfig.Clusters["staging"].Hostname, loadedConfig.Clusters["staging"].Hostname)
	r.Equal(originalConfig.Clusters["staging"].CACert, loadedConfig.Clusters["staging"].CACert)
}

func TestGetCluster(t *testing.T) {
	r := require.New(t)

	config := &Config{
		Clusters: map[string]*ClusterConfig{
			"prod": {
				Hostname: "prod.example.com",
				CACert:   "-----BEGIN CERTIFICATE-----\nMIIE...\n-----END CERTIFICATE-----",
			},
		},
	}

	// Test getting existing cluster
	cluster, err := config.GetCluster("prod")
	r.NoError(err)
	r.Equal("prod.example.com", cluster.Hostname)

	// Test getting non-existent cluster
	_, err = config.GetCluster("nonexistent")
	r.Error(err)
}

func TestConfigPathResolution(t *testing.T) {
	r := require.New(t)

	// Test environment variable path
	testPath := "/tmp/test-config.yaml"
	os.Setenv(EnvConfigPath, testPath)
	path, err := getConfigPath()
	r.NoError(err)
	r.Equal(testPath, path)
	os.Unsetenv(EnvConfigPath)

	// Test default path
	homeDir, err := os.UserHomeDir()
	r.NoError(err)
	path, err = getConfigPath()
	r.NoError(err)
	r.Equal(filepath.Join(homeDir, DefaultConfigPath), path)
}

func TestIsEmpty(t *testing.T) {
	t.Run("empty config returns true", func(t *testing.T) {
		config := NewConfig()
		assert.True(t, config.IsEmpty(), "New config with no clusters or identities should be empty")
	})

	t.Run("config with clusters returns false", func(t *testing.T) {
		config := NewConfig()
		config.Clusters["test"] = &ClusterConfig{
			Hostname: "test.example.com",
		}
		assert.False(t, config.IsEmpty(), "Config with clusters should not be empty")
	})

	t.Run("config with identities but no clusters returns false", func(t *testing.T) {
		config := NewConfig()
		config.Identities["test-identity"] = &IdentityConfig{
			Type:       "keypair",
			Issuer:     "test-issuer",
			PrivateKey: "test-key",
		}
		assert.False(t, config.IsEmpty(), "Config with identities should not be empty even without clusters")
	})

	t.Run("config with both clusters and identities returns false", func(t *testing.T) {
		config := NewConfig()
		config.Clusters["test"] = &ClusterConfig{
			Hostname: "test.example.com",
		}
		config.Identities["test-identity"] = &IdentityConfig{
			Type:       "keypair",
			Issuer:     "test-issuer",
			PrivateKey: "test-key",
		}
		assert.False(t, config.IsEmpty(), "Config with both clusters and identities should not be empty")
	})
}

func TestLoadConfig_OnlyIdentitiesInConfigD(t *testing.T) {
	// Test that a config with only identities (no clusters) in config.d loads successfully
	tmpDir := t.TempDir()

	// Create config.d directory
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err := os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	// Create a config file with only identities, no clusters
	identityConfig := `
identities:
  cloud-identity:
    type: keypair
    issuer: https://auth.miren.cloud
    private_key: |
      -----BEGIN RSA PRIVATE KEY-----
      MIIEowIBAAKCAQEA...
      -----END RSA PRIVATE KEY-----
  local-identity:
    type: certificate
    client_cert: |
      -----BEGIN CERTIFICATE-----
      MIIDQTCCAimgAwIBAgITBmyfz5m/jAo54vB4ikPmljZbyjANBgkqhkiG9w0BAQsF
      -----END CERTIFICATE-----
    client_key: |
      -----BEGIN RSA PRIVATE KEY-----
      MIIEowIBAAKCAQEA...
      -----END RSA PRIVATE KEY-----
`
	err = os.WriteFile(filepath.Join(configDirPath, "identities.yaml"), []byte(identityConfig), 0644)
	require.NoError(t, err)

	// Set environment variable to point to non-existent main config
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Verify main config doesn't exist
	_, err = os.Stat(mainConfigPath)
	assert.True(t, os.IsNotExist(err), "Main config file should not exist")

	// Load the config - should succeed even with only identities
	config, err := LoadConfig()
	require.NoError(t, err, "Should load successfully with only identities")
	require.NotNil(t, config)

	// Verify identities were loaded
	assert.Len(t, config.Identities, 2, "Should have loaded 2 identities")
	assert.Contains(t, config.Identities, "cloud-identity")
	assert.Contains(t, config.Identities, "local-identity")

	// Verify no clusters were loaded
	assert.Len(t, config.Clusters, 0, "Should have no clusters")

	// Verify config is not considered empty
	assert.False(t, config.IsEmpty(), "Config with identities should not be empty")

	// Verify we can retrieve the identities
	cloudIdentity, err := config.GetIdentity("cloud-identity")
	require.NoError(t, err)
	assert.Equal(t, "keypair", cloudIdentity.Type)
	assert.Equal(t, "https://auth.miren.cloud", cloudIdentity.Issuer)

	localIdentity, err := config.GetIdentity("local-identity")
	require.NoError(t, err)
	assert.Equal(t, "certificate", localIdentity.Type)
}

func TestLoadConfig_MixedConfigDFiles(t *testing.T) {
	// Test loading from config.d with some files having clusters, some having identities
	tmpDir := t.TempDir()

	// Create config.d directory
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err := os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	// File 1: Only identities
	identityConfig := `
identities:
  shared-identity:
    type: keypair
    issuer: https://auth.example.com
    private_key: key1
`
	err = os.WriteFile(filepath.Join(configDirPath, "01-identities.yaml"), []byte(identityConfig), 0644)
	require.NoError(t, err)

	// File 2: Cluster that references the identity
	clusterConfig := `
clusters:
  prod:
    hostname: prod.example.com
    identity: shared-identity
    ca_cert: prod-ca
`
	err = os.WriteFile(filepath.Join(configDirPath, "02-prod.yaml"), []byte(clusterConfig), 0644)
	require.NoError(t, err)

	// File 3: Another cluster with inline credentials
	devConfig := `
clusters:
  dev:
    hostname: dev.example.com
    client_cert: dev-cert
    client_key: dev-key
    insecure: true
active_cluster: dev
`
	err = os.WriteFile(filepath.Join(configDirPath, "03-dev.yaml"), []byte(devConfig), 0644)
	require.NoError(t, err)

	// Set environment variable
	mainConfigPath := filepath.Join(tmpDir, "clientconfig.yaml")
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify everything was loaded and merged correctly
	assert.Len(t, config.Identities, 1, "Should have 1 identity")
	assert.Len(t, config.Clusters, 2, "Should have 2 clusters")
	assert.Equal(t, "dev", config.ActiveCluster, "Active cluster should be set")

	// Verify the prod cluster references the shared identity
	prodCluster, err := config.GetCluster("prod")
	require.NoError(t, err)
	assert.Equal(t, "shared-identity", prodCluster.Identity)

	// Verify the shared identity exists
	sharedIdentity, err := config.GetIdentity("shared-identity")
	require.NoError(t, err)
	assert.Equal(t, "keypair", sharedIdentity.Type)
	assert.Equal(t, "https://auth.example.com", sharedIdentity.Issuer)
}
