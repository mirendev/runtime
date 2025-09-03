package clientconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestProgrammaticConfigSavesEverything(t *testing.T) {
	// Test that a programmatically created config saves all clusters and identities

	// Create a config programmatically (not loaded from disk)
	config := NewConfig()

	// Add clusters first
	config.SetCluster("test-cluster", &ClusterConfig{
		Hostname: "test.example.com",
		CACert:   "test-ca",
	})
	config.SetCluster("another-cluster", &ClusterConfig{
		Hostname: "another.example.com",
		CACert:   "another-ca",
	})

	// Now set active cluster
	err := config.SetActiveCluster("test-cluster")
	require.NoError(t, err)

	// Add identities
	config.SetIdentity("test-identity", &IdentityConfig{
		Type:       "keypair",
		Issuer:     "test-issuer",
		PrivateKey: "test-key",
	})

	// Save the config
	tmpDir := t.TempDir()
	savedPath := filepath.Join(tmpDir, "saved.yaml")
	err = config.SaveTo(savedPath)
	require.NoError(t, err)

	// Read back the saved config
	savedData, err := os.ReadFile(savedPath)
	require.NoError(t, err)

	var savedConfig Config
	err = yaml.Unmarshal(savedData, &savedConfig)
	require.NoError(t, err)

	// Verify everything was saved
	assert.Equal(t, "test-cluster", savedConfig.ActiveCluster())
	assert.Equal(t, 2, savedConfig.GetClusterCount(), "Should save all clusters from programmatic config")
	_, err = savedConfig.GetCluster("test-cluster")
	assert.NoError(t, err)
	_, err = savedConfig.GetCluster("another-cluster")
	assert.NoError(t, err)
	assert.Equal(t, 1, savedConfig.GetIdentityCount(), "Should save all identities from programmatic config")
	_, err = savedConfig.GetIdentity("test-identity")
	assert.NoError(t, err)
}

func TestSaveToStdoutError(t *testing.T) {
	// Test that stdout write errors are properly returned

	config := NewConfig()
	config.SetCluster("test", &ClusterConfig{
		Hostname: "test.example.com",
	})

	// Save original stdout
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	// Create a pipe and close the write end to force an error
	r, w, _ := os.Pipe()
	w.Close()
	os.Stdout = w

	// Try to save to stdout (should fail)
	err := config.SaveTo("-")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write config to stdout")

	r.Close()
}

func TestSaveToBufferForStdout(t *testing.T) {
	// Test successful stdout write using a buffer

	config := NewConfig()
	config.SetCluster("test", &ClusterConfig{
		Hostname: "test.example.com",
		CACert:   "test-ca",
	})
	err := config.SetActiveCluster("test")
	require.NoError(t, err)

	// Save original stdout
	oldStdout := os.Stdout
	defer func() { os.Stdout = oldStdout }()

	// Create a buffer to capture output
	var buf bytes.Buffer
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Start a goroutine to copy from pipe to buffer
	done := make(chan bool)
	go func() {
		buf.ReadFrom(r)
		done <- true
	}()

	// Save to stdout
	err = config.SaveTo("-")
	require.NoError(t, err)

	// Close write end and wait for read to complete
	w.Close()
	<-done

	// Verify the output
	var savedConfig Config
	err = yaml.Unmarshal(buf.Bytes(), &savedConfig)
	require.NoError(t, err)
	assert.Equal(t, "test", savedConfig.ActiveCluster())
	_, err = savedConfig.GetCluster("test")
	assert.NoError(t, err)
}
