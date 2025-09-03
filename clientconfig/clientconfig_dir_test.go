package clientconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigDir(t *testing.T) {
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
    client_cert: main-cert
    client_key: main-key
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Create config.d directory
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err = os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	// Create additional config files
	additionalConfig1 := `
clusters:
  dev:
    hostname: dev.example.com
    ca_cert: dev-ca
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
    client_cert: staging-cert
    client_key: staging-key
    insecure: true
`
	err = os.WriteFile(filepath.Join(configDirPath, "02-staging.yml"), []byte(additionalConfig2), 0644)
	require.NoError(t, err)

	// Also create a non-YAML file that should be ignored
	err = os.WriteFile(filepath.Join(configDirPath, "README.txt"), []byte("This should be ignored"), 0644)
	require.NoError(t, err)

	// Set environment variable to use our test directory
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify the main config was loaded
	assert.Equal(t, "main", config.ActiveCluster())
	assert.Equal(t, 3, config.GetClusterCount())

	// Verify main cluster
	mainCluster, err := config.GetCluster("main")
	require.NoError(t, err)
	assert.Equal(t, "main.example.com", mainCluster.Hostname)
	assert.Equal(t, "main-ca", mainCluster.CACert)

	// Verify dev cluster from config.d
	devCluster, err := config.GetCluster("dev")
	require.NoError(t, err)
	assert.Equal(t, "dev.example.com", devCluster.Hostname)
	assert.Equal(t, "dev-ca", devCluster.CACert)

	// Verify staging cluster from config.d
	stagingCluster, err := config.GetCluster("staging")
	require.NoError(t, err)
	assert.Equal(t, "staging.example.com", stagingCluster.Hostname)
	assert.Equal(t, "staging-ca", stagingCluster.CACert)
	assert.True(t, stagingCluster.Insecure)
}

func TestLoadConfigDir_NoMainConfig(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create config.d directory without main config
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err := os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	// Create config file in config.d
	additionalConfig := `
active_cluster: dev
clusters:
  dev:
    hostname: dev.example.com
    ca_cert: dev-ca
    client_cert: dev-cert
    client_key: dev-key
`
	err = os.WriteFile(filepath.Join(configDirPath, "dev.yaml"), []byte(additionalConfig), 0644)
	require.NoError(t, err)

	// Set environment variable to use our test directory
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, filepath.Join(tmpDir, "clientconfig.yaml"))
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify only config.d was loaded
	// When main config has no active cluster, it gets set from config.d files
	assert.Equal(t, "dev", config.ActiveCluster())
	assert.Equal(t, 1, config.GetClusterCount())

	// Verify dev cluster
	devCluster, err := config.GetCluster("dev")
	require.NoError(t, err)
	assert.Equal(t, "dev.example.com", devCluster.Hostname)
}

func TestLoadConfigDir_InvalidFile(t *testing.T) {
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
    client_cert: main-cert
    client_key: main-key
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Create config.d directory
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err = os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	// Create an invalid YAML file
	err = os.WriteFile(filepath.Join(configDirPath, "invalid.yaml"), []byte("invalid: yaml: content:"), 0644)
	require.NoError(t, err)

	// Set environment variable to use our test directory
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config - should fail due to invalid YAML
	_, err = LoadConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config file")
}
