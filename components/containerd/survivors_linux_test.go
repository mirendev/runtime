package containerd

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeContainerd writes a script named containerd, so the kernel reports
// "containerd" as its comm just as it does for the real daemon. The script
// ignores its arguments and sleeps; trapTerm controls whether SIGTERM ends it.
func fakeContainerd(t *testing.T, trapTerm bool) string {
	t.Helper()
	trap := "trap 'exit 0' TERM"
	if !trapTerm {
		trap = "trap '' TERM"
	}
	path := filepath.Join(t.TempDir(), "containerd")
	script := "#!/bin/sh\n" + trap + "\nwhile :; do sleep 0.05; done\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0755))
	return path
}

// startSurvivor runs a fake daemon the way a previous server would have. The
// returned channel closes once it has exited and been reaped.
func startSurvivor(t *testing.T, binary, socketPath, configPath string) (int, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command(binary, "--address", socketPath, "--config", configPath)
	require.NoError(t, cmd.Start())
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		cmd.Process.Kill()
		<-done
	})
	// Give the shell time to install its trap.
	time.Sleep(200 * time.Millisecond)
	return cmd.Process.Pid, done
}

func writeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, nil, 0644))
	return path
}

func TestFindSurvivors(t *testing.T) {
	binary := fakeContainerd(t, true)
	socketPath := filepath.Join(t.TempDir(), "containerd.sock")
	configPath := writeConfig(t)

	ours, _ := startSurvivor(t, binary, socketPath, configPath)
	startSurvivor(t, binary, filepath.Join(t.TempDir(), "other.sock"), configPath)
	// Same socket path, different config file: what a containerd in a
	// container looks like from the host.
	startSurvivor(t, binary, socketPath, writeConfig(t))

	procs, err := findSurvivors(socketPath, configPath)
	require.NoError(t, err)
	require.Len(t, procs, 1)
	assert.Equal(t, ours, procs[0].Pid)
}

func TestStopSurvivors(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "containerd.sock")
	configPath := writeConfig(t)

	t.Run("SIGTERM", func(t *testing.T) {
		_, done := startSurvivor(t, fakeContainerd(t, true), socketPath, configPath)
		procs, err := findSurvivors(socketPath, configPath)
		require.NoError(t, err)
		require.Len(t, procs, 1)

		require.NoError(t, stopSurvivors(procs, 5*time.Second))
		<-done
	})

	t.Run("falls back to SIGKILL", func(t *testing.T) {
		_, done := startSurvivor(t, fakeContainerd(t, false), socketPath, configPath)
		procs, err := findSurvivors(socketPath, configPath)
		require.NoError(t, err)
		require.Len(t, procs, 1)

		start := time.Now()
		require.NoError(t, stopSurvivors(procs, 300*time.Millisecond))
		<-done
		assert.Less(t, time.Since(start), 5*time.Second)
	})
}

func TestSocketOwner(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "owner.sock")
	l, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer l.Close()

	owner, err := socketOwner(socketPath)
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), owner)
}

// TestStart_SocketServedByAnotherProcess reproduces the leak: a daemon this
// component didn't start answers on the socket, and the one it did start never
// comes up. Start must notice rather than adopt the stranger.
func TestStart_SocketServedByAnotherProcess(t *testing.T) {
	baseDir := t.TempDir()
	socketPath := filepath.Join(baseDir, "containerd.sock")
	l, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer l.Close()

	c := NewContainerdComponent(slog.New(slog.NewTextHandler(os.Stderr, nil)), baseDir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = c.Start(ctx, &Config{
		BinaryPath: fakeContainerd(t, true),
		BaseDir:    baseDir,
		SocketPath: socketPath,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not the daemon we started")
	assert.False(t, c.IsRunning())
}

func TestStart_StopsSurvivors(t *testing.T) {
	baseDir := t.TempDir()
	socketPath := filepath.Join(baseDir, "containerd.sock")
	binary := fakeContainerd(t, true)
	_, done := startSurvivor(t, binary, socketPath, filepath.Join(baseDir, "config.toml"))

	c := NewContainerdComponent(slog.New(slog.NewTextHandler(os.Stderr, nil)), baseDir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// The fake never listens, so Start still fails, but only after it has
	// cleared the survivor out of the way.
	err := c.Start(ctx, &Config{BinaryPath: binary, BaseDir: baseDir, SocketPath: socketPath})
	require.Error(t, err)
	select {
	case <-done:
	default:
		t.Fatal("survivor still running after Start")
	}
}

func TestStart_DaemonExitsEarly(t *testing.T) {
	baseDir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "containerd")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nexit 1\n"), 0755))

	c := NewContainerdComponent(slog.New(slog.NewTextHandler(os.Stderr, nil)), baseDir)
	start := time.Now()
	err := c.Start(context.Background(), &Config{BinaryPath: binary, BaseDir: baseDir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited before its socket was ready")
	assert.Less(t, time.Since(start), 5*time.Second)
}

// A second server pointed at the same data directory must not mistake the
// first server's live daemon for a survivor.
func TestStart_LeavesLiveOwnersDaemonAlone(t *testing.T) {
	baseDir := t.TempDir()
	socketPath := filepath.Join(baseDir, "containerd.sock")
	binary := fakeContainerd(t, true)
	_, done := startSurvivor(t, binary, socketPath, filepath.Join(baseDir, "config.toml"))

	owner, err := lockBaseDir(baseDir)
	require.NoError(t, err)
	defer owner.Close()

	c := NewContainerdComponent(slog.New(slog.NewTextHandler(os.Stderr, nil)), baseDir)
	err = c.Start(context.Background(), &Config{BinaryPath: binary, BaseDir: baseDir, SocketPath: socketPath})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already managing containerd")
	select {
	case <-done:
		t.Fatal("live owner's daemon was stopped")
	case <-time.After(200 * time.Millisecond):
	}

	// Once the owner is gone, the lock is free again.
	owner.Close()
	relock, err := lockBaseDir(baseDir)
	require.NoError(t, err)
	relock.Close()
}
