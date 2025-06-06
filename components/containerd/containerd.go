package containerd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
)

// Config defines the configuration for running containerd
type Config struct {
	// Path to the containerd binary
	BinaryPath string

	// Base directory for containerd data and configuration
	BaseDir string

	// Directory containing shim binaries (e.g. containerd-shim-runsc-v1)
	BinDir string

	// Optional: custom config file path. If empty, generates default config
	ConfigPath string

	// Optional: socket path. If empty, uses default in BaseDir
	SocketPath string

	// Optional: additional environment variables
	Env []string
}

// ContainerdComponent manages a containerd instance
type ContainerdComponent struct {
	log      *slog.Logger
	dataPath string

	mu      sync.Mutex
	config  *Config
	cmd     *exec.Cmd
	client  *containerd.Client
	running bool
}

// NewContainerdComponent creates a new containerd component
func NewContainerdComponent(log *slog.Logger, dataPath string) *ContainerdComponent {
	return &ContainerdComponent{
		log:      log.With("component", "containerd"),
		dataPath: dataPath,
	}
}

// populateDefaults fills in default values for the config
func (c *ContainerdComponent) populateDefaults(config *Config) {
	if config.SocketPath == "" {
		runDir := filepath.Join(config.BaseDir, "run")
		config.SocketPath = filepath.Join(runDir, "containerd.sock")
	}

	if config.ConfigPath == "" {
		config.ConfigPath = filepath.Join(config.BaseDir, "config.toml")
	}
}

// Start starts the containerd daemon
func (c *ContainerdComponent) Start(ctx context.Context, config *Config) error {
	// Populate defaults
	c.populateDefaults(config)

	// Lock to check state
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("containerd is already running")
	}
	c.config = config
	c.mu.Unlock()

	// Validate binary exists
	if _, err := os.Stat(config.BinaryPath); err != nil {
		return fmt.Errorf("containerd binary not found at %s: %w", config.BinaryPath, err)
	}

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(config.BaseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	// Setup directories
	stateDir := filepath.Join(config.BaseDir, "state")
	rootDir := filepath.Join(config.BaseDir, "root")
	runDir := filepath.Dir(config.SocketPath)

	for _, dir := range []string{stateDir, rootDir, runDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	c.log.Info("generating containerd config",
		"config_path", config.ConfigPath,
		"socket_path", config.SocketPath,
		"state_dir", stateDir,
		"root_dir", rootDir,
	)
	if err := c.generateDefaultConfig(config.ConfigPath, config.SocketPath, config.BinDir, stateDir, rootDir); err != nil {
		return fmt.Errorf("failed to generate config: %w", err)
	}

	// Build command
	cmd := exec.Command(config.BinaryPath,
		"--address", config.SocketPath,
		"--config", config.ConfigPath,
	)

	c.log.Info("starting containerd",
		"binary", config.BinaryPath,
		"socket", config.SocketPath,
		"config", config.ConfigPath,
	)

	// Set environment
	cmd.Env = append(os.Environ(), config.Env...)

	// Setup logging
	cmd.Stdout = &logWriter{log: c.log, level: slog.LevelInfo}
	cmd.Stderr = &logWriter{log: c.log, level: slog.LevelError}

	// Start containerd
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start containerd: %w", err)
	}

	// Update state with lock
	c.mu.Lock()
	c.cmd = cmd
	c.running = true
	c.mu.Unlock()

	c.log.Info("containerd started", "pid", cmd.Process.Pid)

	// Monitor the process in background
	go func() {
		if err := cmd.Wait(); err != nil {
			c.log.Error("containerd exited with error", "error", err)
		} else {
			c.log.Info("containerd exited")
		}
		c.mu.Lock()
		c.running = false
		c.cmd = nil
		c.mu.Unlock()
	}()

	// Wait for socket to be available (no lock held)
	if err := c.waitForSocket(ctx, config.SocketPath); err != nil {
		// Clean up on failure
		cmd.Process.Kill()
		cmd.Wait()

		c.mu.Lock()
		c.running = false
		c.cmd = nil
		c.mu.Unlock()

		return fmt.Errorf("containerd failed to start: %w", err)
	}

	c.log.Info("containerd socket is ready", "socket", config.SocketPath)

	// Create client for the newly started containerd
	client, err := containerd.New(config.SocketPath)
	if err != nil {
		c.log.Warn("failed to create containerd client", "error", err)
		// Don't fail here, containerd is running
	} else {
		c.mu.Lock()
		c.client = client
		c.mu.Unlock()
	}

	c.log.Info("containerd ready",
		"socket", config.SocketPath,
		"pid", cmd.Process.Pid,
	)

	return nil
}

// Stop stops the containerd daemon
func (c *ContainerdComponent) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running || c.cmd == nil {
		return nil
	}

	c.log.Info("stopping containerd")

	// Close client first
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}

	// Send SIGTERM for graceful shutdown
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		c.log.Warn("failed to send SIGTERM", "error", err)
	}

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Context cancelled, force kill
		c.log.Warn("context cancelled, force killing containerd")
		c.cmd.Process.Kill()
	case err := <-done:
		if err != nil && err.Error() != "signal: terminated" {
			c.log.Warn("containerd exited with error", "error", err)
		}
	case <-time.After(30 * time.Second):
		// Timeout, force kill
		c.log.Warn("shutdown timeout, force killing containerd")
		c.cmd.Process.Kill()
		<-done // Wait for process to actually exit
	}

	c.running = false
	c.cmd = nil
	c.log.Info("containerd stopped")

	return nil
}

// IsRunning returns true if containerd is running
func (c *ContainerdComponent) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// SocketPath returns the path to the containerd socket
func (c *ContainerdComponent) SocketPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.config == nil {
		return ""
	}

	return c.config.SocketPath
}

// PID returns the PID of the running containerd process
func (c *ContainerdComponent) PID() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running || c.cmd == nil || c.cmd.Process == nil {
		return 0, fmt.Errorf("containerd is not running")
	}

	return c.cmd.Process.Pid, nil
}

// Client returns the containerd client
func (c *ContainerdComponent) Client() (*containerd.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil, fmt.Errorf("containerd is not running")
	}

	if c.client == nil {
		return nil, fmt.Errorf("containerd client not available")
	}

	return c.client, nil
}

// waitForSocket waits for the containerd socket to be available
func (c *ContainerdComponent) waitForSocket(ctx context.Context, socketPath string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second)
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for containerd socket at %s", socketPath)
		case <-ticker.C:
			if _, err := os.Stat(socketPath); err == nil {
				// Socket exists, give it a moment to be ready
				time.Sleep(100 * time.Millisecond)
				c.log.Info("containerd socket found", "path", socketPath, "waited", time.Since(startTime))
				return nil
			} else if !os.IsNotExist(err) {
				c.log.Warn("error checking socket", "path", socketPath, "error", err)
			}
		}
	}
}

// generateDefaultConfig generates a minimal containerd configuration
func (c *ContainerdComponent) generateDefaultConfig(configPath, socketPath, binDir, stateDir, rootDir string) error {
	// Build the runsc shim path
	runscShimPath := filepath.Join(binDir, "containerd-shim-runsc-v1")

	config := fmt.Sprintf(`# generated by containerd component
version = 2

state = %q
root = %q

[plugins]
  [plugins."io.containerd.runtime.v1.linux"]
    shim_debug = true
  [plugins."io.containerd.runtime.v1.linux".runtimes.runsc]
    runtime_type = "io.containerd.runsc.v1"
    [plugins."io.containerd.runtime.v1.linux".runtimes.runsc.options]
      ShimBinary = %q

`, stateDir, rootDir, runscShimPath)

	return os.WriteFile(configPath, []byte(config), 0644)
}

// logWriter implements io.Writer to log output
type logWriter struct {
	log   *slog.Logger
	level slog.Level
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	w.log.Log(context.Background(), w.level, msg)
	return len(p), nil
}
