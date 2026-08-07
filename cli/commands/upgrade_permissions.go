package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
	"miren.dev/runtime/pkg/ui"
)

// permissionError represents an error when the user lacks write permissions
type permissionError struct {
	path string
	err  error
}

func (e *permissionError) Error() string {
	return fmt.Sprintf("cannot write to %s: %v", e.path, e.err)
}

// checkInstallPermissions checks if the current user has write permissions
// for the installation path. It checks both the directory (for creating new files)
// and the file itself (for replacing existing files).
func checkInstallPermissions(installPath string) error {
	dir := filepath.Dir(installPath)

	// Check if we can write to the directory
	if err := unix.Access(dir, unix.W_OK); err != nil {
		return &permissionError{path: dir, err: err}
	}

	// If the file exists, check if we can write to it
	if _, err := os.Stat(installPath); err == nil {
		if err := unix.Access(installPath, unix.W_OK); err != nil {
			return &permissionError{path: installPath, err: err}
		}
	}

	return nil
}

// upgradeInstallOption represents an option for handling permission errors
type upgradeInstallOption int

const (
	upgradeOptionCancel upgradeInstallOption = iota
	upgradeOptionSudo
	upgradeOptionUser
)

// handlePermissionError handles permission errors by offering options to the user.
// In interactive mode, it shows a picker menu. In non-interactive mode, it prints
// helpful error messages with command suggestions.
func handlePermissionError(ctx *Context, currentPath string, permErr error) (upgradeInstallOption, error) {
	userPath, err := getUserMirenPath()
	if err != nil {
		return upgradeOptionCancel, fmt.Errorf("cannot determine user install path: %w", err)
	}

	if ui.IsInteractive() {
		return handlePermissionErrorInteractive(ctx, currentPath, userPath)
	}
	return handlePermissionErrorNonInteractive(ctx, currentPath, userPath)
}

func handlePermissionErrorInteractive(ctx *Context, currentPath, userPath string) (upgradeInstallOption, error) {
	ctx.Warn("Cannot write to %s: permission denied", filepath.Dir(currentPath))
	ctx.Info("")
	ctx.Info("The binary is installed in a location that requires elevated permissions.")
	ctx.Info("Current location: %s", currentPath)
	ctx.Info("")

	items := []ui.PickerItem{
		ui.SimplePickerItem{Text: "Cancel and re-run with sudo"},
		ui.SimplePickerItem{Text: fmt.Sprintf("Install to %s (user directory)", userPath)},
		ui.SimplePickerItem{Text: "Cancel"},
	}

	selected, err := ui.RunPicker(items,
		ui.WithTitle("How would you like to proceed?"),
	)
	if err != nil {
		return upgradeOptionCancel, fmt.Errorf("failed to run picker: %w", err)
	}
	if selected == nil {
		return upgradeOptionCancel, nil
	}

	switch selected.ID() {
	case "Cancel and re-run with sudo":
		return upgradeOptionSudo, nil
	case fmt.Sprintf("Install to %s (user directory)", userPath):
		return upgradeOptionUser, nil
	default:
		return upgradeOptionCancel, nil
	}
}

func handlePermissionErrorNonInteractive(ctx *Context, currentPath, userPath string) (upgradeInstallOption, error) {
	ctx.Warn("Cannot write to %s: permission denied", filepath.Dir(currentPath))
	ctx.Info("")
	ctx.Info("To fix this, either:")
	ctx.Info("  1. Run with sudo: sudo miren upgrade")
	ctx.Info("  2. Install to user directory: miren upgrade --user")
	ctx.Info("     (installs to %s)", userPath)
	ctx.Info("")

	return upgradeOptionCancel, fmt.Errorf("permission denied: cannot write to %s", filepath.Dir(currentPath))
}

// ensureUserInstallDir ensures the user install directory exists and returns
// whether PATH needs to be updated.
func ensureUserInstallDir(installPath string) (needsPathUpdate bool, err error) {
	dir := filepath.Dir(installPath)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Check if the directory is in PATH
	pathEnv := os.Getenv("PATH")
	paths := strings.SplitSeq(pathEnv, ":")
	for p := range paths {
		if p == dir {
			return false, nil
		}
	}

	return true, nil
}
