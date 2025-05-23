package clientconfig

import (
	"os"
	"path/filepath"
	"testing"

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
