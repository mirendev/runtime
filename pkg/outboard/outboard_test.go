package outboard

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outboard.json")

	original := &Config{
		Token:      "test-token-abc123",
		FIFOStdout: "/tmp/stdout.fifo",
		FIFOStderr: "/tmp/stderr.fifo",
		PID:        12345,
		RPCAddr:    "localhost:9999",
		Ready:      true,
	}

	err := WriteConfig(path, original)
	require.NoError(t, err)

	loaded, err := ReadConfig(path)
	require.NoError(t, err)

	assert.Equal(t, original.Token, loaded.Token)
	assert.Equal(t, original.FIFOStdout, loaded.FIFOStdout)
	assert.Equal(t, original.FIFOStderr, loaded.FIFOStderr)
	assert.Equal(t, original.PID, loaded.PID)
	assert.Equal(t, original.RPCAddr, loaded.RPCAddr)
	assert.Equal(t, original.Ready, loaded.Ready)
}

func TestConfigReadMissing(t *testing.T) {
	_, err := ReadConfig("/nonexistent/path")
	assert.Error(t, err)
}

func TestConfigWritePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outboard.json")

	cfg := &Config{Token: "test"}
	err := WriteConfig(path, cfg)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestTokenAuthenticatorValid(t *testing.T) {
	auth := NewTokenAuthenticator("secret-token")

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")

	identity, err := auth.Authenticate(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, identity)
	assert.Equal(t, "outboard", identity.Subject)
}

func TestTokenAuthenticatorInvalid(t *testing.T) {
	auth := NewTokenAuthenticator("secret-token")

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	identity, err := auth.Authenticate(context.Background(), req)
	assert.NoError(t, err)
	assert.Nil(t, identity, "invalid token should return nil identity")
}

func TestTokenAuthenticatorMissing(t *testing.T) {
	auth := NewTokenAuthenticator("secret-token")

	req, _ := http.NewRequest("GET", "/", nil)

	identity, err := auth.Authenticate(context.Background(), req)
	assert.NoError(t, err)
	assert.Nil(t, identity, "missing auth header should return nil identity")
}

func TestTokenAuthenticatorBadScheme(t *testing.T) {
	auth := NewTokenAuthenticator("secret-token")

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	identity, err := auth.Authenticate(context.Background(), req)
	assert.NoError(t, err)
	assert.Nil(t, identity, "non-bearer auth should return nil identity")
}

func TestCreateFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.fifo")

	err := createFIFO(path)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.Mode()&os.ModeNamedPipe != 0)
}

func TestCreateFIFOOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.fifo")

	// Create first FIFO
	err := createFIFO(path)
	require.NoError(t, err)

	// Create again should succeed (removes old one)
	err = createFIFO(path)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.Mode()&os.ModeNamedPipe != 0)
}

func TestDefaultRestartPolicy(t *testing.T) {
	p := DefaultRestartPolicy()
	assert.Equal(t, 0, p.MaxRestarts)
	assert.NotZero(t, p.BackoffBase)
	assert.NotZero(t, p.BackoffMax)
	assert.NotZero(t, p.ResetWindow)
}

func TestConnectorNotRunning(t *testing.T) {
	log := defaultTestLogger()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: t.TempDir(),
	})

	assert.False(t, c.IsRunning())

	_, err := c.PID()
	assert.Error(t, err)
}

func TestConnectorStopNotRunning(t *testing.T) {
	log := defaultTestLogger()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: t.TempDir(),
	})

	err := c.Stop(context.Background())
	assert.NoError(t, err)
}

func defaultTestLogger() *slog.Logger {
	return slog.Default()
}

func TestConnectorDetachNotRunning(t *testing.T) {
	log := defaultTestLogger()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: t.TempDir(),
	})

	// Detach when not running should succeed (no-op)
	err := c.Detach()
	assert.NoError(t, err)
	assert.False(t, c.IsRunning())
}

func TestConnectorReconnectNoConfig(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Reconnect should fail when no config file exists
	err := c.Reconnect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no existing config")
}

func TestConnectorReconnectInvalidConfig(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Write an invalid config (missing required fields)
	cfg := &Config{
		Token: "test-token",
		// Missing PID, RPCAddr, and Ready=false
	}
	err := WriteConfig(filepath.Join(dir, "outboard.json"), cfg)
	require.NoError(t, err)

	// Reconnect should fail due to invalid config state
	err = c.Reconnect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config state")
}

func TestConnectorReconnectProcessNotRunning(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Write a config with a PID that doesn't exist
	cfg := &Config{
		Token:   "test-token",
		PID:     999999999, // Very unlikely to exist
		RPCAddr: "localhost:9999",
		Ready:   true,
	}
	err := WriteConfig(filepath.Join(dir, "outboard.json"), cfg)
	require.NoError(t, err)

	// Reconnect should fail because process isn't running
	err = c.Reconnect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "process not running")
}

func TestConnectorReconnectAlreadyConnected(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Manually set running to true to simulate already connected state
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	// Reconnect should fail when already connected
	err := c.Reconnect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already connected")
}

func TestConnectorStartOrReconnectFallsBackToStart(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:       "test",
		BinaryPath: "/nonexistent/binary", // Will fail to start
		DataPath:   dir,
	})

	// StartOrReconnect should try reconnect first (fails - no config),
	// then try start (also fails - no binary)
	err := c.StartOrReconnect(context.Background())
	assert.Error(t, err)
	// The error should be from Start, not from Reconnect
	assert.Contains(t, err.Error(), "starting outboard process")
}

func TestConnectorDetachPreservesConfigFile(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Write a config to simulate a running state
	configPath := filepath.Join(dir, "outboard.json")
	cfg := &Config{
		Token:   "test-token",
		PID:     os.Getpid(), // Use current process PID
		RPCAddr: "localhost:9999",
		Ready:   true,
	}
	err := WriteConfig(configPath, cfg)
	require.NoError(t, err)

	// Manually set connector state to simulate running
	c.mu.Lock()
	c.running = true
	c.configPath = configPath
	c.mu.Unlock()

	// Detach should succeed
	err = c.Detach()
	assert.NoError(t, err)
	assert.False(t, c.IsRunning())

	// Config file should still exist after detach
	_, err = os.Stat(configPath)
	assert.NoError(t, err, "config file should be preserved after detach")

	// Config should still be readable
	loadedCfg, err := ReadConfig(configPath)
	require.NoError(t, err)
	assert.Equal(t, cfg.PID, loadedCfg.PID)
	assert.Equal(t, cfg.RPCAddr, loadedCfg.RPCAddr)
	assert.True(t, loadedCfg.Ready)
}

func TestConnectorReconnectReadsPIDFromConfig(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Use the current process PID (so the "is running" check passes)
	// but RPC connection will fail (expected)
	cfg := &Config{
		Token:   "test-token",
		PID:     os.Getpid(),
		RPCAddr: "localhost:59999", // Port that likely has nothing listening
		Ready:   true,
	}
	err := WriteConfig(filepath.Join(dir, "outboard.json"), cfg)
	require.NoError(t, err)

	// Reconnect should pass the PID check but fail on RPC connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = c.Reconnect(ctx)
	assert.Error(t, err)
	// Should get past PID check and fail on RPC
	assert.Contains(t, err.Error(), "RPC connection failed")
}

func TestConnectorDetachClearsState(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Set up some state manually
	c.mu.Lock()
	c.running = true
	c.token = "some-token"
	c.configPath = filepath.Join(dir, "outboard.json")
	c.exitCh = make(chan struct{})
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	// Detach
	err := c.Detach()
	require.NoError(t, err)

	// Verify state was cleared
	c.mu.Lock()
	defer c.mu.Unlock()
	assert.False(t, c.running)
	assert.Nil(t, c.cmd)
	assert.Nil(t, c.rpcState)
	assert.Nil(t, c.controlClient)
}

func TestConnectorReconnectConfigPathSetCorrectly(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Write config
	expectedPath := filepath.Join(dir, "outboard.json")
	cfg := &Config{
		Token:   "test-token",
		PID:     999999999,
		RPCAddr: "localhost:9999",
		Ready:   true,
	}
	err := WriteConfig(expectedPath, cfg)
	require.NoError(t, err)

	// Attempt reconnect (will fail on PID check)
	_ = c.Reconnect(context.Background())

	// Verify configPath was set correctly
	c.mu.Lock()
	defer c.mu.Unlock()
	assert.Equal(t, expectedPath, c.configPath)
}

func TestConnectorReconnectRequiresReadyFlag(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Write config with Ready=false
	cfg := &Config{
		Token:   "test-token",
		PID:     os.Getpid(),
		RPCAddr: "localhost:9999",
		Ready:   false, // Not ready!
	}
	err := WriteConfig(filepath.Join(dir, "outboard.json"), cfg)
	require.NoError(t, err)

	// Reconnect should fail because Ready=false
	err = c.Reconnect(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config state")
}

func TestConnectorMultipleDetachCalls(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Multiple detach calls should all succeed (idempotent)
	for i := range 3 {
		err := c.Detach()
		assert.NoError(t, err, "detach call %d should succeed", i+1)
	}
}

func TestConnectorReconnectSetupsFIFOForwarding(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Create FIFOs manually to simulate existing outboard setup
	stdoutFIFO := filepath.Join(dir, "stdout.fifo")
	stderrFIFO := filepath.Join(dir, "stderr.fifo")
	require.NoError(t, createFIFO(stdoutFIFO))
	require.NoError(t, createFIFO(stderrFIFO))

	// Write config with FIFO paths and current PID (so process check passes)
	cfg := &Config{
		Token:      "test-token",
		PID:        os.Getpid(),
		RPCAddr:    "localhost:59998", // Will fail RPC, but that's after FIFO setup
		Ready:      true,
		FIFOStdout: stdoutFIFO,
		FIFOStderr: stderrFIFO,
	}
	require.NoError(t, WriteConfig(filepath.Join(dir, "outboard.json"), cfg))

	// Verify FIFOs exist before reconnect attempt
	_, err := os.Stat(stdoutFIFO)
	require.NoError(t, err, "stdout FIFO should exist")
	_, err = os.Stat(stderrFIFO)
	require.NoError(t, err, "stderr FIFO should exist")

	// Reconnect will pass PID check but fail on RPC - that's expected
	// The important thing is that FIFO paths are read from config
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = c.Reconnect(ctx)

	// Should fail on RPC connection, but verify it got past the config reading
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "RPC connection failed")

	// Config path should be set (indicating config was read)
	c.mu.Lock()
	configPath := c.configPath
	c.mu.Unlock()
	assert.Equal(t, filepath.Join(dir, "outboard.json"), configPath)
}

func TestConnectorReconnectHandlesMissingFIFOs(t *testing.T) {
	log := defaultTestLogger()
	dir := t.TempDir()
	c := NewConnector(log, ConnectorConfig{
		Name:     "test",
		DataPath: dir,
	})

	// Write config WITHOUT FIFO paths (old config format or FIFOs removed)
	cfg := &Config{
		Token:      "test-token",
		PID:        os.Getpid(),
		RPCAddr:    "localhost:59997",
		Ready:      true,
		FIFOStdout: "", // Empty - no FIFO
		FIFOStderr: "", // Empty - no FIFO
	}
	require.NoError(t, WriteConfig(filepath.Join(dir, "outboard.json"), cfg))

	// Reconnect should handle empty FIFO paths gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Reconnect(ctx)

	// Should fail on RPC, not on FIFO setup
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "RPC connection failed")
}
