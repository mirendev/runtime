package containerd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"miren.dev/runtime/pkg/slogout"
)

// Config defines the configuration for running containerd
type Config struct {
	// Path to the containerd binary
	BinaryPath string

	// Base directory for containerd data and configuration
	BaseDir string

	// Directory containing shim binaries (e.g. containerd-shim-runc-v2)
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

	mu       sync.Mutex
	config   *Config
	cmd      *exec.Cmd
	client   *containerd.Client
	running  bool
	waitDone chan struct{}
	owner    *os.File
}

// NewContainerdComponent creates a new containerd component
func NewContainerdComponent(log *slog.Logger, dataPath string) *ContainerdComponent {
	return &ContainerdComponent{
		log:      log.With("module", "containerd"),
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

// lockBaseDir takes an exclusive flock that this process holds for as long as
// it owns the daemon under baseDir. The kernel drops it when the process dies,
// however it dies, so holding it means no live server owns whatever containerd
// is still running there.
func lockBaseDir(baseDir string) (*os.File, error) {
	path := filepath.Join(baseDir, "owner.lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another server process is already managing containerd in %s", baseDir)
		}
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return f, nil
}

func (c *ContainerdComponent) releaseOwner() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.owner != nil {
		c.owner.Close()
		c.owner = nil
	}
}

// Start starts the containerd daemon
func (c *ContainerdComponent) Start(ctx context.Context, config *Config) (err error) {
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

	owner, err := lockBaseDir(config.BaseDir)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.owner = owner
	c.mu.Unlock()
	defer func() {
		if err != nil {
			c.releaseOwner()
		}
	}()

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

	survivors, err := findSurvivors(config.SocketPath, config.ConfigPath)
	if err != nil {
		return fmt.Errorf("looking for containerd left by a previous server: %w", err)
	}
	if len(survivors) > 0 {
		pids := make([]int, len(survivors))
		for i, proc := range survivors {
			pids[i] = proc.Pid
		}
		c.log.Warn("stopping containerd left running by a previous server process",
			"pids", pids, "socket", config.SocketPath)
		if err := stopSurvivors(survivors, 30*time.Second); err != nil {
			return err
		}
	}

	// Build command
	cmd := exec.Command(config.BinaryPath,
		"--address", config.SocketPath,
		"--config", config.ConfigPath,
	)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	c.log.Info("starting containerd",
		"binary", config.BinaryPath,
		"socket", config.SocketPath,
		"config", config.ConfigPath,
	)

	// Set environment
	cmd.Env = append(os.Environ(), config.Env...)

	// Setup logging for containerd output
	// Use WithJSONParsing() when containerd is configured with format="json"
	// Use WithKeyValueParsing() when containerd uses default logrus format (key=value pairs)
	stdout := slogout.NewWriter(c.log, slog.LevelInfo,
		slogout.WithKeyValueParsing(), slogout.WithMaxLevel(slog.LevelInfo), slogout.WithSource("containerd"))
	stderr := slogout.NewWriter(c.log, slog.LevelError, slogout.WithKeyValueParsing(),
		slogout.WithMaxLevel(slog.LevelInfo), slogout.WithSource("containerd"))
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Start containerd
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start containerd: %w", err)
	}

	// Update state with lock
	waitDone := make(chan struct{})
	c.mu.Lock()
	c.cmd = cmd
	c.running = true
	c.waitDone = waitDone
	c.mu.Unlock()

	c.log.Info("containerd started", "pid", cmd.Process.Pid)

	// Monitor the process in background
	go func() {
		defer close(waitDone)

		if err := cmd.Wait(); err != nil {
			c.log.Error("containerd exited with error", "error", err)
		} else {
			c.log.Info("containerd exited")
		}

		// Close the log writers to flush any remaining data
		stdout.Close()
		stderr.Close()

		c.mu.Lock()
		c.running = false
		c.cmd = nil
		c.mu.Unlock()
	}()

	// Wait for socket to be available (no lock held)
	err = c.waitForSocket(ctx, config.SocketPath, waitDone)
	if err == nil {
		err = checkSocketOwner(config.SocketPath, cmd.Process.Pid)
	}
	if err != nil {
		// Clean up on failure
		cmd.Process.Kill()

		// Wait for the monitoring goroutine to finish
		<-waitDone

		return fmt.Errorf("containerd failed to start: %w", err)
	}

	c.log.Info("containerd socket is ready", "socket", config.SocketPath)

	// Create client for the newly started containerd
	client, err := containerd.New(config.SocketPath,
		containerd.WithDialOpts([]grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		}),
	)
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
	defer c.releaseOwner()
	c.mu.Lock()

	if !c.running || c.cmd == nil {
		c.mu.Unlock()
		return nil
	}

	// Capture cmd reference before any modifications
	cmd := c.cmd
	c.mu.Unlock()

	c.log.Info("stopping containerd")

	// Close client first
	c.mu.Lock()
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
	c.mu.Unlock()

	// Get waitDone channel before sending signal
	c.mu.Lock()
	waitDone := c.waitDone
	c.mu.Unlock()

	// Send SIGTERM for graceful shutdown
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		c.log.Warn("failed to send SIGTERM", "error", err)
	}

	// Wait for process to exit with timeout
	select {
	case <-ctx.Done():
		// Context cancelled, force kill
		c.log.Warn("context cancelled, force killing containerd")
		cmd.Process.Kill()
		<-waitDone // Wait for process to actually exit
	case <-waitDone:
		// Process exited normally
	case <-time.After(30 * time.Second):
		// Timeout, force kill
		c.log.Warn("shutdown timeout, force killing containerd")
		cmd.Process.Kill()
		<-waitDone // Wait for process to actually exit
	}

	c.mu.Lock()
	c.running = false
	c.cmd = nil
	c.mu.Unlock()

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

// checkSocketOwner confirms the daemon answering on socketPath is the one we
// just started. Anything else answering means a daemon we don't own is still
// serving there, and ours will never come up.
func checkSocketOwner(socketPath string, pid int) error {
	owner, err := socketOwner(socketPath)
	if errors.Is(err, errors.ErrUnsupported) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking containerd socket owner: %w", err)
	}
	if owner != pid {
		return fmt.Errorf("containerd socket %s is served by pid %d, not the daemon we started (pid %d)", socketPath, owner, pid)
	}
	return nil
}

// waitForSocket waits for the containerd socket to be accepting connections.
// Checking file existence alone is insufficient — containerd creates the
// socket file before it starts listening, causing a race on fast machines.
// exited closes if the daemon dies first, so a crash fails fast rather than
// waiting out the timeout.
func (c *ContainerdComponent) waitForSocket(ctx context.Context, socketPath string, exited <-chan struct{}) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(30 * time.Second)
	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			return fmt.Errorf("containerd exited before its socket was ready")
		case <-timeout:
			return fmt.Errorf("timeout waiting for containerd socket at %s", socketPath)
		case <-ticker.C:
			conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				c.log.Info("containerd socket is accepting connections", "path", socketPath, "waited", time.Since(startTime))
				return nil
			}
		}
	}
}

// generateDefaultConfig generates a minimal containerd configuration
func (c *ContainerdComponent) generateDefaultConfig(configPath, socketPath, binDir, stateDir, rootDir string) error {
	config := fmt.Sprintf(`# generated by containerd component
version = 3

state = %q
root = %q

`, stateDir, rootDir)

	return os.WriteFile(configPath, []byte(config), 0644)
}
