package clientconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMirenConfigAsFileDisablesConfigD(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create main config file
	mainConfigPath := filepath.Join(tmpDir, "myconfig.yaml")
	mainConfig := `
active_cluster: main
clusters:
  main:
    hostname: main.example.com
    ca_cert: main-ca
identities:
  main-identity:
    type: keypair
    issuer: main-issuer
    private_key: main-key
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Create config.d directory with additional configs that should be ignored
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err = os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	additionalConfig := `
clusters:
  should-not-load:
    hostname: should-not-load.example.com
    ca_cert: should-not-load-ca
identities:
  should-not-load-identity:
    type: keypair
    issuer: should-not-load-issuer
    private_key: should-not-load-key
`
	err = os.WriteFile(filepath.Join(configDirPath, "extra.yaml"), []byte(additionalConfig), 0644)
	require.NoError(t, err)

	// Set environment variable to point directly to the config file
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, mainConfigPath)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify only the main config was loaded (config.d should be ignored)
	assert.Equal(t, 1, config.GetClusterCount(), "Should only have clusters from main config")
	assert.Equal(t, 1, config.GetIdentityCount(), "Should only have identities from main config")

	// Verify the main cluster exists
	mainCluster, err := config.GetCluster("main")
	assert.NoError(t, err)
	assert.Equal(t, "main.example.com", mainCluster.Hostname)

	// Verify the config.d cluster was NOT loaded
	_, err = config.GetCluster("should-not-load")
	assert.Error(t, err, "Cluster from config.d should not be loaded")

	// Verify the config.d identity was NOT loaded
	_, err = config.GetIdentity("should-not-load-identity")
	assert.Error(t, err, "Identity from config.d should not be loaded")
}

func TestMirenConfigAsDirectoryLoadsConfigD(t *testing.T) {
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
identities:
  main-identity:
    type: keypair
    issuer: main-issuer
    private_key: main-key
`
	err := os.WriteFile(mainConfigPath, []byte(mainConfig), 0644)
	require.NoError(t, err)

	// Create config.d directory with additional configs
	configDirPath := filepath.Join(tmpDir, "clientconfig.d")
	err = os.MkdirAll(configDirPath, 0755)
	require.NoError(t, err)

	additionalConfig := `
clusters:
  from-config-d:
    hostname: from-config-d.example.com
    ca_cert: from-config-d-ca
identities:
  from-config-d-identity:
    type: keypair
    issuer: from-config-d-issuer
    private_key: from-config-d-key
`
	err = os.WriteFile(filepath.Join(configDirPath, "extra.yaml"), []byte(additionalConfig), 0644)
	require.NoError(t, err)

	// Set environment variable to point to the directory
	oldEnv := os.Getenv(EnvConfigPath)
	os.Setenv(EnvConfigPath, tmpDir)
	defer os.Setenv(EnvConfigPath, oldEnv)

	// Load the config
	config, err := LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify both main config and config.d were loaded
	assert.Equal(t, 2, config.GetClusterCount(), "Should have clusters from both main and config.d")
	assert.Equal(t, 2, config.GetIdentityCount(), "Should have identities from both main and config.d")

	// Verify the main cluster exists
	mainCluster, err := config.GetCluster("main")
	assert.NoError(t, err)
	assert.Equal(t, "main.example.com", mainCluster.Hostname)

	// Verify the config.d cluster was loaded
	configDCluster, err := config.GetCluster("from-config-d")
	assert.NoError(t, err)
	assert.Equal(t, "from-config-d.example.com", configDCluster.Hostname)

	// Verify the config.d identity was loaded
	configDIdentity, err := config.GetIdentity("from-config-d-identity")
	assert.NoError(t, err)
	assert.Equal(t, "from-config-d-issuer", configDIdentity.Issuer)
}

func TestMirenConfigPathResolution(t *testing.T) {
	tests := []struct {
		name           string
		envPath        string
		createAsFile   bool
		createAsDir    bool
		expectedIsFile bool
	}{
		{
			name:           "yaml extension treated as file",
			envPath:        "/tmp/test/config.yaml",
			createAsFile:   false,
			createAsDir:    false,
			expectedIsFile: true,
		},
		{
			name:           "yml extension treated as file",
			envPath:        "/tmp/test/config.yml",
			createAsFile:   false,
			createAsDir:    false,
			expectedIsFile: true,
		},
		{
			name:           "existing file treated as file",
			envPath:        "testconfig",
			createAsFile:   true,
			createAsDir:    false,
			expectedIsFile: true,
		},
		{
			name:           "existing directory treated as directory",
			envPath:        "testdir",
			createAsFile:   false,
			createAsDir:    true,
			expectedIsFile: false,
		},
		{
			name:           "non-existent path without extension treated as directory",
			envPath:        "/tmp/test/config",
			createAsFile:   false,
			createAsDir:    false,
			expectedIsFile: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testPath := filepath.Join(tmpDir, tt.envPath)

			// Create file or directory if needed
			if tt.createAsFile {
				err := os.WriteFile(testPath, []byte("test"), 0644)
				require.NoError(t, err)
			} else if tt.createAsDir {
				err := os.MkdirAll(testPath, 0755)
				require.NoError(t, err)
			}

			// Set environment variable
			oldEnv := os.Getenv(EnvConfigPath)
			os.Setenv(EnvConfigPath, testPath)
			defer os.Setenv(EnvConfigPath, oldEnv)

			// Get the config path
			configPath, loadConfigD, err := getConfigPath()
			require.NoError(t, err)

			// Get the config dir path
			configDirPath, err := getConfigDirPath()
			require.NoError(t, err)

			if tt.expectedIsFile {
				// Should treat as file
				assert.Equal(t, testPath, configPath, "Config path should be the file itself")
				assert.False(t, loadConfigD, "Should not load clientconfig.d when MIREN_CONFIG is a file")
				assert.Empty(t, configDirPath, "Config dir should be empty when MIREN_CONFIG is a file")
			} else {
				// Should treat as directory
				assert.Equal(t, filepath.Join(testPath, "clientconfig.yaml"), configPath, "Config path should be within the directory")
				assert.True(t, loadConfigD, "Should load clientconfig.d when MIREN_CONFIG is a directory")
				assert.Equal(t, filepath.Join(testPath, "clientconfig.d"), configDirPath, "Config dir should be within the directory")
			}
		})
	}
}
