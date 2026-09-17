package release

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// bundleBackupDir, under the install directory, holds the previous copy of
// every bundled file an install replaced. Keeping them in one place scopes
// Rollback and the pre-install sweep to backups of the installer's own
// making: the install directory may be something like /usr/local/bin for a
// CLI-only upgrade, where a stray *.old is someone else's. miren's own
// backup stays beside it as miren.old, which the executor keys on.
const bundleBackupDir = ".previous"

// Install puts the staged artifact in place: every bundled file, then miren
// last. The executor reads "miren on disk matches the target" as "install
// done" when it resumes, so miren has to be the final write for that to stay
// true. Each file being replaced is kept as a backup for Rollback; a failure
// part way restores the files already swapped.
func (i *binaryInstaller) Install(ctx context.Context, downloaded *DownloadedArtifact) error {
	targetDir := filepath.Dir(i.opts.InstallPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create install directory: %w", err)
	}

	// An install that died after swapping the bundle but before swapping
	// miren left bundle backups with no miren backup. Those are the
	// originals, and this retry keeps them: backing up the half-installed
	// files over them would hand a later rollback the new containerd beside
	// the old miren. Anything else is a finished install whose backups must
	// not survive into this one's set, since Rollback restores whatever is
	// in the backup directory.
	previous, err := i.bundleBackups()
	if err != nil {
		return fmt.Errorf("failed to list bundle backups: %w", err)
	}
	resuming := len(previous) > 0 && !i.hasMirenBackup()
	if !resuming {
		if err := i.clearBackups(); err != nil {
			return fmt.Errorf("failed to clear previous backups: %w", err)
		}
	}

	type placed struct{ path, backup string }
	var installed []placed
	undo := func() {
		for j := len(installed) - 1; j >= 0; j-- {
			i.unplaceFile(installed[j].path, installed[j].backup)
		}
		// Consumed backups leave their directories behind; the tree goes
		// once no backup is left in it, and anything still in it is kept.
		if left, err := i.bundleBackups(); err == nil && len(left) == 0 {
			os.RemoveAll(i.bundleBackupPath())
		}
	}
	for _, rel := range downloaded.Bundled {
		if downloaded.BundleDir == "" {
			return fmt.Errorf("bundled file %s has no staging directory", rel)
		}
		dst := filepath.Join(targetDir, rel)
		backup := filepath.Join(i.bundleBackupPath(), rel)
		if err := i.installFile(filepath.Join(downloaded.BundleDir, rel), dst, backup, resuming); err != nil {
			undo()
			return fmt.Errorf("failed to install %s: %w", rel, err)
		}
		installed = append(installed, placed{dst, backup})
	}
	mirenBackup := i.opts.InstallPath + i.opts.BackupSuffix
	if err := i.installFile(downloaded.Path, i.opts.InstallPath, mirenBackup, false); err != nil {
		undo()
		return fmt.Errorf("failed to install binary: %w", err)
	}
	if downloaded.BundleDir != "" {
		// Everything staged has moved; the directory is only worth removing
		// if that left it empty.
		os.Remove(downloaded.BundleDir)
	}

	// Write checksum file
	checksumPath := i.opts.InstallPath + ".sha256"
	if err := os.WriteFile(checksumPath, []byte(downloaded.Checksum), 0644); err != nil {
		// Non-fatal: log but continue
		fmt.Fprintf(os.Stderr, "Warning: failed to write checksum file: %v\n", err)
	}

	return nil
}

// installFile replaces dst with src, keeping the existing dst at backup.
// keepBackup says a backup from an interrupted install may already be
// there, and if so it is the one to keep.
//
// The server keeps running through an upgrade, and the shims of its
// sandboxes exec runc from this directory for every exec, kill and delete,
// so dst must resolve to a working binary at every instant: the staged
// file is brought onto dst's filesystem first, the backup is a hard link,
// and the swap is one rename.
func (i *binaryInstaller) installFile(src, dst, backup string, keepBackup bool) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	staged, err := i.stage(src, dst)
	if err != nil {
		return err
	}
	kept := keepBackup && i.exists(backup)
	if i.exists(dst) && !kept {
		if err := i.backupFile(dst, backup); err != nil {
			os.Remove(staged)
			return fmt.Errorf("failed to backup current file: %w", err)
		}
	}
	if err := os.Rename(staged, dst); err != nil {
		os.Remove(staged)
		return fmt.Errorf("failed to install file: %w", err)
	}
	i.syncDir(filepath.Dir(dst))

	// Fix SELinux context if needed (RHEL/Oracle Linux)
	fixSELinuxContext(dst)
	return nil
}

// stage moves src next to dst, executable and synced to disk, so the final
// step can be a rename. A rename across filesystems (the download dir is
// often tmpfs) falls back to a copy.
func (i *binaryInstaller) stage(src, dst string) (string, error) {
	tmpFile, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".new-*")
	if err != nil {
		return "", fmt.Errorf("failed to create staging file: %w", err)
	}
	tmp := tmpFile.Name()
	if err := os.Rename(src, tmp); err == nil {
		tmpFile.Close()
	} else {
		source, err := os.Open(src)
		if err != nil {
			tmpFile.Close()
			os.Remove(tmp)
			return "", err
		}
		_, err = io.Copy(tmpFile, source)
		source.Close()
		tmpFile.Close()
		if err != nil {
			os.Remove(tmp)
			return "", fmt.Errorf("failed to copy staged file: %w", err)
		}
		os.Remove(src)
	}
	if err := os.Chmod(tmp, 0755); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("failed to set permissions: %w", err)
	}
	if err := i.syncFile(tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// unplaceFile undoes installFile: the backup comes back if there is one,
// otherwise the file was new and goes away.
func (i *binaryInstaller) unplaceFile(dst, backup string) {
	if i.exists(backup) {
		i.restoreFile(dst, backup)
		return
	}
	os.Remove(dst)
}

// Backup keeps the current binary as the .old backup Rollback restores.
func (i *binaryInstaller) Backup(ctx context.Context) error {
	return i.backupFile(i.opts.InstallPath, i.opts.InstallPath+i.opts.BackupSuffix)
}

// backupFile keeps path's current file at backup without taking it away
// from path: a hard link where the filesystem allows one, a copy where it
// does not. The backup is on disk before this returns, since Rollback will
// depend on it the moment path is replaced.
func (i *binaryInstaller) backupFile(path, backup string) error {
	if err := os.MkdirAll(filepath.Dir(backup), 0755); err != nil {
		return err
	}
	os.Remove(backup)
	if err := os.Link(path, backup); err != nil {
		if err := i.copyFile(path, backup); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}
	i.syncDir(filepath.Dir(backup))
	return nil
}

// Rollback puts every backed-up file back, miren last for the same reason
// Install writes it last: a rollback that dies part way must not leave a
// restored miren beside an unrestored containerd, or a resumed executor
// would read the binary's version and call the whole rollback done. That
// is the only ordering that matters; the bundle files themselves go back
// in whatever order they are listed. An interrupted install has no miren
// backup but a bundle to put back.
func (i *binaryInstaller) Rollback(ctx context.Context) error {
	bundled, err := i.bundleBackups()
	if err != nil {
		return fmt.Errorf("failed to list bundle backups: %w", err)
	}
	mirenBackup := i.opts.InstallPath + i.opts.BackupSuffix
	if !i.exists(mirenBackup) && len(bundled) == 0 {
		return fmt.Errorf("no backup found at %s", mirenBackup)
	}

	targetDir := filepath.Dir(i.opts.InstallPath)
	for _, rel := range bundled {
		if err := i.restoreFile(filepath.Join(targetDir, rel), filepath.Join(i.bundleBackupPath(), rel)); err != nil {
			return err
		}
	}
	if i.exists(mirenBackup) {
		if err := i.restoreFile(i.opts.InstallPath, mirenBackup); err != nil {
			return err
		}
	}
	return os.RemoveAll(i.bundleBackupPath())
}

// restoreFile replaces path with backup in one rename, consuming the
// backup.
func (i *binaryInstaller) restoreFile(path, backup string) error {
	if _, err := os.Stat(backup); err != nil {
		return fmt.Errorf("no backup found at %s: %w", backup, err)
	}

	// Ensure backup has proper permissions before restoring
	if err := os.Chmod(backup, 0755); err != nil {
		return fmt.Errorf("failed to set backup permissions: %w", err)
	}

	if err := os.Rename(backup, path); err != nil {
		if err := i.copyFile(backup, path); err != nil {
			return fmt.Errorf("failed to restore backup: %w", err)
		}
		os.Remove(backup)
	}
	i.syncDir(filepath.Dir(path))

	return nil
}

func (i *binaryInstaller) bundleBackupPath() string {
	return filepath.Join(filepath.Dir(i.opts.InstallPath), bundleBackupDir)
}

// bundleBackups lists the backed-up bundle files relative to the install
// directory, in lexical order. Empty when there is no backup directory,
// which is what a miren-only install leaves, or when nothing is left in it.
func (i *binaryInstaller) bundleBackups() ([]string, error) {
	root := i.bundleBackupPath()
	if !i.exists(root) {
		return nil, nil
	}
	var bundled []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		bundled = append(bundled, rel)
		return nil
	})
	return bundled, err
}

// clearBackups removes the previous install's backups: miren's, and the
// bundle's.
func (i *binaryInstaller) clearBackups() error {
	if err := os.RemoveAll(i.bundleBackupPath()); err != nil {
		return err
	}
	if err := os.Remove(i.opts.InstallPath + i.opts.BackupSuffix); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (i *binaryInstaller) hasMirenBackup() bool {
	return i.exists(i.opts.InstallPath + i.opts.BackupSuffix)
}

func (i *binaryInstaller) exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (i *binaryInstaller) syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file for sync: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync file to disk: %w", err)
	}
	return nil
}

// syncDir persists a rename. Best-effort: the rename itself already took.
func (i *binaryInstaller) syncDir(dir string) {
	dirFile, err := os.Open(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to open directory for sync: %v\n", err)
		return
	}
	if err := dirFile.Sync(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to sync directory: %v\n", err)
	}
	dirFile.Close()
}

// GetCurrentVersion returns the version of the currently installed binary
func (i *binaryInstaller) GetCurrentVersion(ctx context.Context) (VersionInfo, error) {
	if _, err := os.Stat(i.opts.InstallPath); err != nil {
		return VersionInfo{}, fmt.Errorf("no binary installed at %s", i.opts.InstallPath)
	}

	return GetCurrentVersion(i.opts.InstallPath)
}

// HasBackup reports whether Rollback has something to put back: miren's
// backup, or the bundle backups an interrupted install left behind.
func (i *binaryInstaller) HasBackup() bool {
	if i.hasMirenBackup() {
		return true
	}
	bundled, err := i.bundleBackups()
	return err == nil && len(bundled) > 0
}

// fixSELinuxContext ensures the binary has the correct SELinux context for execution.
// This is needed on RHEL/Oracle Linux where SELinux is enforcing and files in /var/lib
// get var_lib_t context by default, which prevents systemd from executing them.
func fixSELinuxContext(binaryPath string) {
	// Check if SELinux is enforcing
	cmd := exec.Command("getenforce")
	output, err := cmd.Output()
	if err != nil {
		// getenforce not found or failed - SELinux probably not installed
		return
	}

	status := strings.TrimSpace(string(output))
	if status != "Enforcing" {
		return
	}

	// SELinux is enforcing - run restorecon to apply the correct context.
	// If semanage fcontext was used during install, this will apply that rule.
	// If not, restorecon will apply the default context for the path.
	restoreconCmd := exec.Command("restorecon", "-v", binaryPath)
	if output, err := restoreconCmd.CombinedOutput(); err != nil {
		// restorecon failed - try chcon as fallback
		chconCmd := exec.Command("chcon", "-t", "bin_t", binaryPath)
		if output, err := chconCmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to set SELinux context: %v\nOutput: %s\n", err, output)
		}
	} else {
		// Check if context was actually set to something executable
		// If restorecon set it back to var_lib_t, we need to use chcon
		lsCmd := exec.Command("ls", "-Z", binaryPath)
		if lsOutput, err := lsCmd.Output(); err == nil {
			if strings.Contains(string(lsOutput), "var_lib_t") {
				// restorecon set wrong context, override with chcon
				chconCmd := exec.Command("chcon", "-t", "bin_t", binaryPath)
				if output, err := chconCmd.CombinedOutput(); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to set SELinux context: %v\nOutput: %s\n", err, output)
				}
			}
		}
		_ = output // silence unused warning
	}
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
