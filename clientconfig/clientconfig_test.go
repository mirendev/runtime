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
	originalConfig := NewConfig()
	originalConfig.SetCluster("prod", &ClusterConfig{
		Hostname: "prod.example.com",
		CACert:   "-----BEGIN CERTIFICATE-----\nMIIE...\n-----END CERTIFICATE-----",
	})
	originalConfig.SetCluster("staging", &ClusterConfig{
		Hostname: "staging.example.com",
		CACert:   "-----BEGIN CERTIFICATE-----\nMIIE...\n-----END CERTIFICATE-----",
	})

	// Save configuration
	err = originalConfig.Save()
	r.NoError(err)

	// Load configuration
	loadedConfig, err := LoadConfig()
	r.NoError(err)

	// Verify loaded configuration matches original
	origProd, err := originalConfig.GetCluster("prod")
	r.NoError(err)
	loadedProd, err := loadedConfig.GetCluster("prod")
	r.NoError(err)
	r.Equal(origProd.Hostname, loadedProd.Hostname)
	r.Equal(origProd.CACert, loadedProd.CACert)

	origStaging, err := originalConfig.GetCluster("staging")
	r.NoError(err)
	loadedStaging, err := loadedConfig.GetCluster("staging")
	r.NoError(err)
	r.Equal(origStaging.Hostname, loadedStaging.Hostname)
	r.Equal(origStaging.CACert, loadedStaging.CACert)
}

func TestGetCluster(t *testing.T) {
	r := require.New(t)

	config := NewConfig()
	config.SetCluster("prod", &ClusterConfig{
		Hostname: "prod.example.com",
		CACert:   "-----BEGIN CERTIFICATE-----\nMIIE...\n-----END CERTIFICATE-----",
	})

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
