//go:build linux
// +build linux

package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"miren.dev/runtime/components/upgrade"
)

// UpgradeLocal performs a local hot restart upgrade without RPC
func UpgradeLocal(ctx *Context, opts struct {
	BinaryPath string `short:"b" long:"binary" description:"Path to new miren binary"`
	DataPath   string `short:"d" long:"data-path" description:"Data path" default:"/var/lib/miren"`
	PID        int    `short:"p" long:"pid" description:"PID of running miren server (auto-detected if not specified)"`
	Force      bool   `short:"f" long:"force" description:"Force upgrade even if versions match"`
	Timeout    int    `short:"t" long:"timeout" description:"Timeout in seconds" default:"60"`
}) error {
	// Auto-detect the new binary path if not specified
	if opts.BinaryPath == "" {
		// First check for .new binary
		newBinary := filepath.Join(opts.DataPath, "release.new", "miren")
		if info, err := os.Stat(newBinary); err == nil && !info.IsDir() {
			opts.BinaryPath = newBinary
			ctx.Log.Info("found new release binary", "path", newBinary)
		} else {
			// Check standard release locations
			locations := []string{
				filepath.Join(opts.DataPath, "release", "miren"),
				filepath.Join(os.Getenv("HOME"), ".miren/release/miren"),
			}

			for _, loc := range locations {
				if info, err := os.Stat(loc); err == nil && !info.IsDir() {
					opts.BinaryPath = loc
					break
				}
			}
		}

		if opts.BinaryPath == "" {
			return fmt.Errorf("no new miren binary found, please download a release first or specify --binary")
		}
	}

	// Verify the binary exists and is executable
	info, err := os.Stat(opts.BinaryPath)
	if err != nil {
		return fmt.Errorf("binary not found at %s: %w", opts.BinaryPath, err)
	}

	if info.IsDir() {
		return fmt.Errorf("specified path %s is a directory, not a binary", opts.BinaryPath)
	}

	if info.Mode()&0111 == 0 {
		return fmt.Errorf("binary at %s is not executable", opts.BinaryPath)
	}

	// Auto-detect the running miren server PID if not specified
	if opts.PID == 0 {
		pid, err := findMirenServerPID()
		if err != nil {
			return fmt.Errorf("failed to find running miren server: %w", err)
		}
		opts.PID = pid
		ctx.Log.Info("found miren server", "pid", pid)
	}

	// Verify the process exists
	if err := syscall.Kill(opts.PID, 0); err != nil {
		// EPERM means the process exists but we can't signal it (different user)
		// ESRCH means the process doesn't exist
		if !errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("process with PID %d does not exist or is not accessible: %w", opts.PID, err)
		}
	}

	// Get current version
	currentVersion := "unknown"
	if currentBinary, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", opts.PID)); err == nil {
		if ver, err := getVersionFromBinary(currentBinary); err == nil {
			currentVersion = ver
		}
	}

	// Get new version
	newVersion, err := getVersionFromBinary(opts.BinaryPath)
	if err != nil {
		ctx.Log.Warn("failed to get version from new binary", "error", err)
		newVersion = "unknown"
	}

	ctx.UILog.Info("preparing local upgrade",
		"current_version", currentVersion,
		"new_version", newVersion,
		"current_pid", opts.PID,
		"new_binary", opts.BinaryPath)

	// Check if versions match (unless forced)
	if !opts.Force && currentVersion == newVersion && currentVersion != "unknown" {
		return fmt.Errorf("current and new versions are the same (%s), use --force to upgrade anyway", currentVersion)
	}

	if upgrade.IsRunningUnderSystemd() {
		ctx.Log.Info("detected systemd supervision, will notify systemd during upgrade")
	}

	// Load the current server's handoff state if it exists
	coordinator := upgrade.NewCoordinator(ctx.Log, opts.DataPath)
	existingState, err := coordinator.LoadHandoffState()
	if err != nil {
		ctx.Log.Warn("failed to load existing handoff state", "error", err)
	}

	if existingState != nil {
		ctx.Log.Info("found existing handoff state, using it",
			"timestamp", existingState.Timestamp,
			"old_pid", existingState.OldPID)
	} else {
		// No handoff state found - fail fast
		return fmt.Errorf("no handoff state found - the running server must initiate the upgrade first")
	}

	ctx.UILog.Info("initiating hot restart upgrade...")

	// Launch the new process with takeover flag
	args := []string{
		"server",
		"--takeover",
		"--mode", existingState.Mode,
		"--data-path", existingState.DataPath,
		"--address", existingState.ServerAddress,
		"--runner-address", existingState.RunnerAddress,
		"--runner-id", existingState.RunnerID,
	}

	// Add etcd endpoints if present
	for _, endpoint := range existingState.EtcdEndpoints {
		args = append(args, "--etcd", endpoint)
	}

	// Add containerd socket if present
	if existingState.ContainerdSocket != "" {
		args = append(args, "--containerd-socket", existingState.ContainerdSocket)
	}

	// Add ClickHouse address if present
	if existingState.ClickHouseAddress != "" {
		args = append(args, "--clickhouse-addr", existingState.ClickHouseAddress)
	}

	cmd := exec.Command(opts.BinaryPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	// Set process attributes to create new process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	ctx.Log.Info("starting new miren process", "command", opts.BinaryPath, "args", args)

	// Start the new process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new process: %w", err)
	}

	ctx.UILog.Info("new process started", "pid", cmd.Process.Pid)

	// Wait for the new process to be ready (monitor its output or wait for signal)
	timeout := time.After(time.Duration(opts.Timeout) * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			// NOTE: We intentionally kill only the child process, not the process group.
			// This preserves child processes like containerd that should continue running
			// even if the upgrade times out. The child processes will be orphaned and
			// reparented to init, but this is preferable to killing critical services.
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				ctx.Log.Warn("failed to kill new process on timeout", "error", err)
			}
			return fmt.Errorf("timeout waiting for new process to be ready")

		case <-ticker.C:
			// First check if the new process is still running
			if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
				// New process died, upgrade failed
				// Clear the upgrade state to avoid leaving the system in an "upgrading" state
				if cleanupErr := coordinator.ClearHandoffState(); cleanupErr != nil {
					ctx.Log.Warn("failed to clear upgrade state after process failure", "error", cleanupErr)
				}
				return fmt.Errorf("new process exited unexpectedly")
			}

			// Check if the old process has exited (which means handoff completed)
			err := syscall.Kill(opts.PID, 0)
			if err == nil {
				// Process still exists, continue waiting
				continue
			} else if errors.Is(err, syscall.ESRCH) {
				// Old process has exited (No such process), upgrade successful
				ctx.UILog.Info("upgrade completed successfully",
					"old_pid", opts.PID,
					"new_pid", cmd.Process.Pid)

				// Clear the handoff state
				if err := coordinator.ClearHandoffState(); err != nil {
					ctx.Log.Warn("failed to clear handoff state", "error", err)
				}

				// If there's a release.new directory, move it to release
				newReleaseDir := filepath.Join(opts.DataPath, "release.new")
				releaseDir := filepath.Join(opts.DataPath, "release")
				if _, err := os.Stat(newReleaseDir); err == nil {
					// Backup old release
					if _, err := os.Stat(releaseDir); err == nil {
						backupDir := filepath.Join(opts.DataPath, fmt.Sprintf("release.backup.%d", time.Now().Unix()))
						if err := os.Rename(releaseDir, backupDir); err != nil {
							// Handle cross-device move (EXDEV)
							if errors.Is(err, syscall.EXDEV) {
								ctx.Log.Warn("cross-device move detected, using copy for backup", "error", err)
								// Fall back to copy+remove for cross-device
								if err := copyDir(releaseDir, backupDir); err != nil {
									ctx.Log.Warn("failed to copy old release for backup", "error", err)
								} else {
									os.RemoveAll(releaseDir)
									ctx.Log.Info("backed up old release (via copy)", "backup", backupDir)
								}
							} else {
								ctx.Log.Warn("failed to backup old release", "error", err)
							}
						} else {
							ctx.Log.Info("backed up old release", "backup", backupDir)
						}
					}
					// Move new release to active
					if err := os.Rename(newReleaseDir, releaseDir); err != nil {
						// Handle cross-device move (EXDEV)
						if errors.Is(err, syscall.EXDEV) {
							ctx.Log.Warn("cross-device move detected, using copy for activation", "error", err)
							// Fall back to copy+remove for cross-device
							if err := copyDir(newReleaseDir, releaseDir); err != nil {
								ctx.Log.Warn("failed to copy new release for activation", "error", err)
							} else {
								os.RemoveAll(newReleaseDir)
								ctx.Log.Info("activated new release (via copy)", "path", releaseDir)
							}
						} else {
							ctx.Log.Warn("failed to move release.new to release", "error", err)
						}
					} else {
						ctx.Log.Info("activated new release", "path", releaseDir)
					}
				}

				return nil
			} else if errors.Is(err, syscall.EPERM) {
				// Permission denied - old process exists but we can't signal it
				ctx.Log.Error("permission denied checking old process status", "pid", opts.PID)
				return fmt.Errorf("permission denied checking old process (PID %d) - upgrade cannot proceed", opts.PID)
			} else {
				// Other error checking old process
				ctx.Log.Error("unexpected error checking old process status", "pid", opts.PID, "error", err)
				return fmt.Errorf("failed to check old process status: %w", err)
			}
		}
	}
}

// findMirenServerPID finds the PID of a running miren server process
func findMirenServerPID() (int, error) {
	// Look for miren server process, use -o to get oldest (first started) process
	output, err := exec.Command("pgrep", "-o", "-f", "miren server").Output()
	if err != nil {
		return 0, fmt.Errorf("no miren server process found")
	}

	pidStr := strings.TrimSpace(string(output))
	if pidStr == "" {
		return 0, fmt.Errorf("no miren server process found")
	}

	// Validate and parse the PID
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse PID %q: %w", pidStr, err)
	}

	if pid <= 0 {
		return 0, fmt.Errorf("invalid PID: %d", pid)
	}

	return pid, nil
}

// getVersionFromBinary executes the binary with version subcommand to get its version
func getVersionFromBinary(binaryPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timeout getting version from %s", binaryPath)
		}
		return "", fmt.Errorf("failed to get version: %w (stderr: %s)", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// copyDir recursively copies a directory
func copyDir(src, dst string) error {
	// Create destination directory
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	// Copy directory contents
	cmd := exec.Command("cp", "-r", src+"/.", dst)
	return cmd.Run()
}

// UpgradeStatus checks the status of an ongoing upgrade
func UpgradeStatus(ctx *Context, opts struct{}) error {
	// Check for upgrade state file
	dataPath := "/var/lib/miren"
	if envPath := os.Getenv("MIREN_DATA_PATH"); envPath != "" {
		dataPath = envPath
	}

	coordinator := upgrade.NewCoordinator(ctx.Log, dataPath)
	state, err := coordinator.LoadHandoffState()
	if err != nil {
		return fmt.Errorf("failed to load upgrade state: %w", err)
	}

	if state == nil {
		ctx.UILog.Info("no upgrade in progress")
		return nil
	}

	ctx.UILog.Info("upgrade in progress",
		"started", state.Timestamp.Format(time.RFC3339),
		"old_pid", state.OldPID,
		"mode", state.Mode)

	// Check if old process is still running
	if err := syscall.Kill(state.OldPID, 0); err == nil {
		ctx.UILog.Info("old process still running", "pid", state.OldPID)
	} else {
		ctx.UILog.Info("old process has exited", "pid", state.OldPID)
	}

	return nil
}

// UpgradeRollback rolls back a failed upgrade
func UpgradeRollback(ctx *Context, opts struct{}) error {
	dataPath := "/var/lib/miren"
	if envPath := os.Getenv("MIREN_DATA_PATH"); envPath != "" {
		dataPath = envPath
	}

	coordinator := upgrade.NewCoordinator(ctx.Log, dataPath)
	if err := coordinator.ClearHandoffState(); err != nil {
		return fmt.Errorf("failed to clear upgrade state: %w", err)
	}

	ctx.UILog.Info("upgrade state cleared")
	return nil
}
