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
	backedUp := false
	if _, err := os.Stat(i.opts.InstallPath); err == nil {
		if err := i.Backup(ctx); err != nil {
			return fmt.Errorf("failed to backup current binary: %w", err)
		}
		backedUp = true
	}

	// Ensure binary is executable before moving to final location
	if err := os.Chmod(downloaded.Path, 0755); err != nil {
		// Restore backup if we created one
		if backedUp {
			i.Rollback(ctx)
		}
		return fmt.Errorf("failed to set binary permissions: %w", err)
	}

	// Sync the staged binary to disk before rename
	stagedFile, err := os.Open(downloaded.Path)
	if err != nil {
		if backedUp {
			i.Rollback(ctx)
		}
		return fmt.Errorf("failed to open staged binary for sync: %w", err)
	}
	if err := stagedFile.Sync(); err != nil {
		stagedFile.Close()
		if backedUp {
			i.Rollback(ctx)
		}
		return fmt.Errorf("failed to sync staged binary to disk: %w", err)
	}
	stagedFile.Close()

	// Atomic rename from downloaded location to install path
	if err := os.Rename(downloaded.Path, i.opts.InstallPath); err != nil {
		// If rename fails (e.g., cross-device), fall back to copy
		if err := i.copyFile(downloaded.Path, i.opts.InstallPath); err != nil {
			// Restore backup if we created one
			if backedUp {
				i.Rollback(ctx)
			}
			return fmt.Errorf("failed to install binary: %w", err)
		}
		// Clean up source file after successful copy
		os.Remove(downloaded.Path)
	}

	// Sync directory to ensure rename/copy is persisted
	dirFile, err := os.Open(targetDir)
	if err != nil {
		// Non-fatal but log it
		fmt.Fprintf(os.Stderr, "Warning: failed to open directory for sync: %v\n", err)
	} else {
		if err := dirFile.Sync(); err != nil {
			// Non-fatal but log it
			fmt.Fprintf(os.Stderr, "Warning: failed to sync directory: %v\n", err)
		}
		dirFile.Close()
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

	// Ensure backup has proper permissions before restoring
	if err := os.Chmod(backupPath, 0755); err != nil {
		return fmt.Errorf("failed to set backup permissions: %w", err)
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

	// Create temp file in the same directory as destination for atomic rename
	tempFile, err := os.CreateTemp(filepath.Dir(dst), ".miren-install-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()

	// Ensure temp file is cleaned up if we fail
	defer func() {
		if tempFile != nil {
			tempFile.Close()
			os.Remove(tempPath)
		}
	}()

	// Copy contents to temp file
	if _, err = io.Copy(tempFile, source); err != nil {
		return err
	}

	// Sync to disk before rename
	if err := tempFile.Sync(); err != nil {
		return err
	}

	// Close temp file before rename
	tempFile.Close()
	tempFile = nil // Prevent defer cleanup

	// Set permissions before rename
	if err := os.Chmod(tempPath, 0755); err != nil {
		os.Remove(tempPath)
		return err
	}

	// Atomic rename
	if err := os.Rename(tempPath, dst); err != nil {
		os.Remove(tempPath)
		return err
	}

	// Sync directory to ensure rename is persisted
	dirFile, err := os.Open(filepath.Dir(dst))
	if err != nil {
		// Non-fatal but important enough to return as error since this is in copyFile
		return fmt.Errorf("failed to open directory for sync: %w", err)
	}
	defer dirFile.Close()
	if err := dirFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync directory: %w", err)
	}

	return nil
}
