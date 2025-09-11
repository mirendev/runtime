// +build ignore

package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"miren.dev/runtime/components/upgrade"
	"miren.dev/runtime/pkg/rpc"
)

// Upgrade performs a hot restart upgrade of the miren server
func Upgrade(ctx *Context, opts struct {
	BinaryPath string `short:"b" long:"binary" description:"Path to new miren binary (defaults to latest downloaded release)"`
	Force      bool   `short:"f" long:"force" description:"Force upgrade even if versions match"`
	Timeout    int    `short:"t" long:"timeout" description:"Timeout in seconds for upgrade" default:"60"`
}) error {
	// Determine the binary path
	if opts.BinaryPath == "" {
		// Check for downloaded release in standard locations
		locations := []string{
			"/var/lib/miren/release.new/miren",
			"/var/lib/miren/release/miren",
			filepath.Join(os.Getenv("HOME"), ".miren/release/miren"),
		}
		
		for _, loc := range locations {
			if info, err := os.Stat(loc); err == nil && !info.IsDir() {
				opts.BinaryPath = loc
				break
			}
		}
		
		if opts.BinaryPath == "" {
			return fmt.Errorf("no miren binary specified and no release found in standard locations")
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
	
	ctx.Log.Info("using binary for upgrade", "path", opts.BinaryPath)
	
	// Connect to the running miren server
	var client rpc.ServerUpgradeClient
	if err := ctx.Client.Resolve(&client); err != nil {
		return fmt.Errorf("failed to connect to miren server: %w", err)
	}
	
	// Get current server version
	currentVersion, err := client.GetVersion(ctx)
	if err != nil {
		ctx.Log.Warn("failed to get current server version", "error", err)
		currentVersion = "unknown"
	}
	
	// Get new binary version
	newVersion, err := getVersionFromBinary(opts.BinaryPath)
	if err != nil {
		ctx.Log.Warn("failed to get version from new binary", "error", err)
		newVersion = "unknown"
	}
	
	ctx.UILog.Info("preparing upgrade", 
		"current_version", currentVersion,
		"new_version", newVersion,
		"binary", opts.BinaryPath)
	
	// Check if versions match (unless forced)
	if !opts.Force && currentVersion == newVersion && currentVersion != "unknown" {
		return fmt.Errorf("current and new versions are the same (%s), use --force to upgrade anyway", currentVersion)
	}
	
	// Initiate the upgrade
	upgradeCtx, cancel := context.WithTimeout(ctx, time.Duration(opts.Timeout)*time.Second)
	defer cancel()
	
	ctx.UILog.Info("initiating hot restart upgrade...")
	
	request := &rpc.UpgradeRequest{
		NewBinaryPath: opts.BinaryPath,
		Force:         opts.Force,
	}
	
	result, err := client.InitiateUpgrade(upgradeCtx, request)
	if err != nil {
		return fmt.Errorf("upgrade failed: %w", err)
	}
	
	if !result.Success {
		return fmt.Errorf("upgrade failed: %s", result.Message)
	}
	
	ctx.UILog.Info("upgrade completed successfully", "message", result.Message)
	return nil
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
	if processExists(state.OldPID) {
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

// getVersionFromBinary executes the binary with --version flag to get its version
func getVersionFromBinary(binaryPath string) (string, error) {
	cmd := exec.Command(binaryPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	
	return strings.TrimSpace(string(output)), nil
}

// processExists checks if a process with the given PID exists
func processExists(pid int) bool {
	// Send signal 0 to check if process exists
	err := syscall.Kill(pid, 0)
	return err == nil
}