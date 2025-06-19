package containerd

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerdComponent_populateDefaults(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	c := NewContainerdComponent(logger, "/tmp/test-data")

	tests := []struct {
		name     string
		config   *Config
		expected *Config
	}{
		{
			name: "empty socket and config paths",
			config: &Config{
				BaseDir: "/var/lib/containerd",
			},
			expected: &Config{
				BaseDir:    "/var/lib/containerd",
				SocketPath: "/var/lib/containerd/run/containerd.sock",
				ConfigPath: "/var/lib/containerd/config.toml",
			},
		},
		{
			name: "custom socket and config paths",
			config: &Config{
				BaseDir:    "/var/lib/containerd",
				SocketPath: "/custom/socket.sock",
				ConfigPath: "/custom/config.toml",
			},
			expected: &Config{
				BaseDir:    "/var/lib/containerd",
				SocketPath: "/custom/socket.sock",
				ConfigPath: "/custom/config.toml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.populateDefaults(tt.config)
			assert.Equal(t, tt.expected.SocketPath, tt.config.SocketPath)
			assert.Equal(t, tt.expected.ConfigPath, tt.config.ConfigPath)
		})
	}
}

func TestContainerdComponent_generateDefaultConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	c := NewContainerdComponent(logger, "/tmp/test-data")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	socketPath := "/var/run/containerd.sock"
	binDir := "/usr/local/bin"
	stateDir := "/var/lib/containerd/state"
	rootDir := "/var/lib/containerd/root"

	err := c.generateDefaultConfig(configPath, socketPath, binDir, stateDir, rootDir)
	require.NoError(t, err)

	// Verify file was created
	_, err = os.Stat(configPath)
	require.NoError(t, err)

	// Read and verify content
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)

	configStr := string(content)
	assert.Contains(t, configStr, `state = "/var/lib/containerd/state"`)
	assert.Contains(t, configStr, `root = "/var/lib/containerd/root"`)
}

func TestContainerdComponent_PID(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	c := NewContainerdComponent(logger, tmpDir)

	t.Run("PID returns error when not running", func(t *testing.T) {
		_, err := c.PID()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not running")
	})

	t.Run("PID returns error when cmd is nil", func(t *testing.T) {
		c.running = true
		c.cmd = nil
		_, err := c.PID()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not running")
	})
}

func TestContainerdComponent_Stop(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	t.Run("stop when not running", func(t *testing.T) {
		c := NewContainerdComponent(logger, tmpDir)
		err := c.Stop(context.Background())
		assert.NoError(t, err)
	})

}

func TestContainerdComponent_IsRunning(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	c := NewContainerdComponent(logger, "/tmp/test-data")

	assert.False(t, c.IsRunning())

	c.running = true
	assert.True(t, c.IsRunning())
}

func TestContainerdComponent_SocketPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	c := NewContainerdComponent(logger, "/tmp/test-data")

	// No config
	assert.Empty(t, c.SocketPath())

	// With config
	c.config = &Config{
		SocketPath: "/var/run/containerd.sock",
	}
	assert.Equal(t, "/var/run/containerd.sock", c.SocketPath())
}

func TestContainerdComponent_Client(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	c := NewContainerdComponent(logger, "/tmp/test-data")

	t.Run("not running", func(t *testing.T) {
		_, err := c.Client()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not running")
	})

	t.Run("no client", func(t *testing.T) {
		c.running = true
		_, err := c.Client()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})
}

// Helper function to check if a process exists
func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// Integration test - only run if containerd binary is available
func TestContainerdComponent_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if running as root (containerd typically requires root)
	if os.Geteuid() != 0 {
		t.Skip("Skipping integration test - requires root privileges")
	}

	// Check if containerd is available
	containerdPath, err := exec.LookPath("containerd")
	if err != nil {
		t.Skip("containerd binary not found in PATH")
	}

	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	baseDir := filepath.Join(tmpDir, "containerd")

	// Create directories upfront
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(baseDir, 0755))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c := NewContainerdComponent(logger, dataDir)

	config := &Config{
		BinaryPath: containerdPath,
		BaseDir:    baseDir,
		BinDir:     filepath.Dir(containerdPath),
	}

	// Use a timeout context for the test
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start containerd
	err = c.Start(ctx, config)
	require.NoError(t, err)
	assert.True(t, c.IsRunning())

	// Get PID through API
	pid, err := c.PID()
	require.NoError(t, err)
	assert.True(t, processExists(pid))

	// Socket should exist
	assert.NotEmpty(t, c.SocketPath())

	cl, err := c.Client()
	require.NoError(t, err)

	ver, err := cl.Version(ctx)
	require.NoError(t, err)

	t.Logf("Containerd version: %s", ver.Version)

	// Wait a bit for socket to be ready
	time.Sleep(2 * time.Second)

	// Check socket file exists
	_, err = os.Stat(c.SocketPath())
	assert.NoError(t, err)

	// Stop containerd
	err = c.Stop(ctx)
	require.NoError(t, err)
	assert.False(t, c.IsRunning())

	// Process should eventually stop
	time.Sleep(1 * time.Second)
	assert.False(t, processExists(pid))
}
