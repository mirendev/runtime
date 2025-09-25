package release

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Installer manages the installation and rollback of artifacts
type Installer interface {
	Install(ctx context.Context, downloaded *DownloadedArtifact) error
	Backup(ctx context.Context) error
	Rollback(ctx context.Context) error
	GetCurrentVersion(ctx context.Context) (VersionInfo, error)
	HasBackup() bool
}

// InstallOptions contains options for installation
type InstallOptions struct {
	// InstallPath is where to install the binary
	InstallPath string
	// BackupSuffix is the suffix for backup files
	BackupSuffix string
}

// DefaultInstallOptions returns default installation options
func DefaultInstallOptions() InstallOptions {
	return InstallOptions{
		InstallPath:  "/var/lib/miren/release/miren",
		BackupSuffix: ".old",
	}
}

// binaryInstaller implements Installer for binary artifacts
type binaryInstaller struct {
	opts InstallOptions
}

// NewInstaller creates a new binary installer
func NewInstaller(opts InstallOptions) Installer {
	return &binaryInstaller{
		opts: opts,
	}
}

// Install installs a downloaded artifact
func (i *binaryInstaller) Install(ctx context.Context, downloaded *DownloadedArtifact) error {
	// Ensure target directory exists
	targetDir := filepath.Dir(i.opts.InstallPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}

	// Backup current binary if it exists
	if _, err := os.Stat(i.opts.InstallPath); err == nil {
		if err := i.Backup(ctx); err != nil {
			return fmt.Errorf("failed to backup current binary: %w", err)
		}
	}

	// Atomic rename from downloaded location to install path
	if err := os.Rename(downloaded.Path, i.opts.InstallPath); err != nil {
		// If rename fails (e.g., cross-device), fall back to copy
		if err := i.copyFile(downloaded.Path, i.opts.InstallPath); err != nil {
			return fmt.Errorf("failed to install binary: %w", err)
		}
		// Clean up source file after successful copy
		os.Remove(downloaded.Path)
	}

	// Ensure binary is executable
	if err := os.Chmod(i.opts.InstallPath, 0755); err != nil {
		return fmt.Errorf("failed to set binary permissions: %w", err)
	}

	// Write checksum file
	checksumPath := i.opts.InstallPath + ".sha256"
	if err := os.WriteFile(checksumPath, []byte(downloaded.Checksum), 0644); err != nil {
		// Non-fatal: log but continue
		fmt.Fprintf(os.Stderr, "Warning: failed to write checksum file: %v\n", err)
	}

	return nil
}

// Backup creates a backup of the current binary
func (i *binaryInstaller) Backup(ctx context.Context) error {
	backupPath := i.opts.InstallPath + i.opts.BackupSuffix

	// Remove old backup if it exists
	os.Remove(backupPath)

	// Rename current binary to backup
	if err := os.Rename(i.opts.InstallPath, backupPath); err != nil {
		// If rename fails, fall back to copy
		if err := i.copyFile(i.opts.InstallPath, backupPath); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	return nil
}

// Rollback restores the previous version from backup
func (i *binaryInstaller) Rollback(ctx context.Context) error {
	backupPath := i.opts.InstallPath + i.opts.BackupSuffix

	// Check if backup exists
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("no backup found at %s: %w", backupPath, err)
	}

	// Remove current binary if it exists
	os.Remove(i.opts.InstallPath)

	// Restore backup
	if err := os.Rename(backupPath, i.opts.InstallPath); err != nil {
		// If rename fails, fall back to copy
		if err := i.copyFile(backupPath, i.opts.InstallPath); err != nil {
			return fmt.Errorf("failed to restore backup: %w", err)
		}
		// Remove backup after successful copy
		os.Remove(backupPath)
	}

	// Ensure binary is executable
	if err := os.Chmod(i.opts.InstallPath, 0755); err != nil {
		return fmt.Errorf("failed to set binary permissions: %w", err)
	}

	return nil
}

// GetCurrentVersion returns the version of the currently installed binary
func (i *binaryInstaller) GetCurrentVersion(ctx context.Context) (VersionInfo, error) {
	if _, err := os.Stat(i.opts.InstallPath); err != nil {
		return VersionInfo{}, fmt.Errorf("no binary installed at %s", i.opts.InstallPath)
	}

	return GetCurrentVersion(i.opts.InstallPath)
}

// HasBackup checks if a backup exists
func (i *binaryInstaller) HasBackup() bool {
	backupPath := i.opts.InstallPath + i.opts.BackupSuffix
	_, err := os.Stat(backupPath)
	return err == nil
}

// copyFile copies a file from src to dst
func (i *binaryInstaller) copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}
