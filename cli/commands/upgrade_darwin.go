//go:build darwin
// +build darwin

package commands

import (
	"fmt"
	"os"
	"runtime"
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
	return fmt.Errorf("hot restart upgrade is not yet supported on %s", runtime.GOOS)
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

	// On Darwin, we shouldn't have any upgrade state, but if we do, show it
	ctx.UILog.Warn("unexpected upgrade state found on Darwin",
		"started", state.Timestamp.Format(time.RFC3339),
		"old_pid", state.OldPID,
		"mode", state.Mode)

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
