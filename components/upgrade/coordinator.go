package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// HandoffState contains the state to be preserved during upgrade
type HandoffState struct {
	// Version of the handoff protocol
	Version int `json:"version"`

	// PID of the old process
	OldPID int `json:"old_pid"`

	// Timestamp of handoff initiation
	Timestamp time.Time `json:"timestamp"`

	// ContainerdSocket path for reconnection
	ContainerdSocket string `json:"containerd_socket"`

	// EtcdEndpoints for reconnection
	EtcdEndpoints []string `json:"etcd_endpoints"`

	// ClickHouseAddress if using external or embedded
	ClickHouseAddress string `json:"clickhouse_address,omitempty"`

	// ServerAddress that the server is listening on
	ServerAddress string `json:"server_address"`

	// RunnerAddress for the runner service
	RunnerAddress string `json:"runner_address"`

	// DataPath for persistent data
	DataPath string `json:"data_path"`

	// RunnerID for the runner
	RunnerID string `json:"runner_id"`

	// Mode of operation (standalone, distributed)
	Mode string `json:"mode"`

	// Additional custom state that components can add
	CustomState map[string]interface{} `json:"custom_state,omitempty"`
}

// Coordinator manages the hot restart upgrade process
type Coordinator struct {
	log          *slog.Logger
	dataPath     string
	statePath    string
	mu           sync.RWMutex
	state        *HandoffState
	upgrading    bool
	readyChannel chan error
}

// NewCoordinator creates a new upgrade coordinator
func NewCoordinator(log *slog.Logger, dataPath string) *Coordinator {
	return &Coordinator{
		log:       log.With("component", "upgrade-coordinator"),
		dataPath:  dataPath,
		statePath: filepath.Join(dataPath, "upgrade-state.json"),
	}
}

// IsUpgrading returns true if an upgrade is in progress
func (c *Coordinator) IsUpgrading() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.upgrading
}

// LoadHandoffState loads a previously saved handoff state
func (c *Coordinator) LoadHandoffState() (*HandoffState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No state file, not an upgrade
		}
		return nil, fmt.Errorf("failed to read handoff state: %w", err)
	}

	var state HandoffState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal handoff state: %w", err)
	}

	// Check if state is stale (older than 5 minutes)
	if time.Since(state.Timestamp) > 5*time.Minute {
		c.log.Warn("handoff state is stale, ignoring", "age", time.Since(state.Timestamp))
		os.Remove(c.statePath)
		return nil, nil
	}

	c.state = &state
	return &state, nil
}

// SaveHandoffState saves the current state for handoff
func (c *Coordinator) SaveHandoffState(state *HandoffState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state.Version = 1
	state.Timestamp = time.Now()
	state.OldPID = os.Getpid()

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal handoff state: %w", err)
	}

	// Write atomically by writing to temp file and renaming
	tempPath := c.statePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write handoff state: %w", err)
	}

	if err := os.Rename(tempPath, c.statePath); err != nil {
		return fmt.Errorf("failed to rename handoff state: %w", err)
	}

	c.state = state
	c.log.Info("saved handoff state", "path", c.statePath)
	return nil
}

// ClearHandoffState removes the handoff state file
func (c *Coordinator) ClearHandoffState() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.Remove(c.statePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear handoff state: %w", err)
	}

	c.state = nil
	return nil
}

// InitiateUpgrade starts the upgrade process by launching the new binary
func (c *Coordinator) InitiateUpgrade(ctx context.Context, newBinaryPath string, state *HandoffState) error {
	c.mu.Lock()
	if c.upgrading {
		c.mu.Unlock()
		return fmt.Errorf("upgrade already in progress")
	}
	c.upgrading = true
	c.readyChannel = make(chan error, 1)
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.upgrading = false
		c.mu.Unlock()
	}()

	// Save the handoff state
	if err := c.SaveHandoffState(state); err != nil {
		return fmt.Errorf("failed to save handoff state: %w", err)
	}

	// Verify the new binary exists and is executable
	info, err := os.Stat(newBinaryPath)
	if err != nil {
		return fmt.Errorf("new binary not found at %s: %w", newBinaryPath, err)
	}

	if info.Mode()&0111 == 0 {
		return fmt.Errorf("new binary at %s is not executable", newBinaryPath)
	}

	c.log.Info("launching new miren process", "binary", newBinaryPath)

	// Build the command for the new process
	args := []string{
		"server",
		"--takeover",
		"--mode", state.Mode,
		"--data-path", state.DataPath,
		"--address", state.ServerAddress,
		"--runner-address", state.RunnerAddress,
		"--runner-id", state.RunnerID,
	}

	// Add etcd endpoints if present
	for _, endpoint := range state.EtcdEndpoints {
		args = append(args, "--etcd", endpoint)
	}

	// Add containerd socket if present
	if state.ContainerdSocket != "" {
		args = append(args, "--containerd-socket", state.ContainerdSocket)
	}

	// Add ClickHouse address if present
	if state.ClickHouseAddress != "" {
		args = append(args, "--clickhouse-addr", state.ClickHouseAddress)
	}

	cmd := exec.Command(newBinaryPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	// Set process attributes to create new process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	// Start the new process
	if err := cmd.Start(); err != nil {
		c.ClearHandoffState()
		return fmt.Errorf("failed to start new process: %w", err)
	}

	c.log.Info("new process started", "pid", cmd.Process.Pid)

	// Wait for the new process to signal readiness or timeout
	select {
	case <-ctx.Done():
		cmd.Process.Kill()
		c.ClearHandoffState()
		return fmt.Errorf("upgrade cancelled: %w", ctx.Err())

	case err := <-c.readyChannel:
		if err != nil {
			cmd.Process.Kill()
			c.ClearHandoffState()
			return fmt.Errorf("new process failed to start: %w", err)
		}

		c.log.Info("new process signaled ready, initiating graceful shutdown")
		// The new process is ready, we can now gracefully shutdown
		return nil

	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		c.ClearHandoffState()
		return fmt.Errorf("timeout waiting for new process to be ready")
	}
}

// SignalReady signals that the new process is ready to take over
func (c *Coordinator) SignalReady() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == nil {
		return fmt.Errorf("no handoff state found")
	}

	// Signal the old process that we're ready
	oldPID := c.state.OldPID
	if oldPID > 0 && oldPID != os.Getpid() {
		c.log.Info("signaling old process to shutdown", "old_pid", oldPID)

		// Send SIGUSR1 to indicate readiness
		if err := syscall.Kill(oldPID, syscall.SIGUSR1); err != nil {
			c.log.Warn("failed to signal old process", "error", err)
		}
	}

	// Clear the handoff state as we've successfully taken over
	return c.ClearHandoffState()
}

// WaitForReadiness waits for the new process to signal readiness
func (c *Coordinator) WaitForReadiness(ctx context.Context) error {
	c.mu.RLock()
	readyChan := c.readyChannel
	c.mu.RUnlock()

	if readyChan == nil {
		return fmt.Errorf("no upgrade in progress")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-readyChan:
		return err
	}
}

// HandleReadinessSignal handles SIGUSR1 from the new process indicating it's ready
func (c *Coordinator) HandleReadinessSignal() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.readyChannel != nil {
		select {
		case c.readyChannel <- nil:
		default:
		}
	}
}
