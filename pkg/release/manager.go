package release

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Manager orchestrates the upgrade process
type Manager struct {
	downloader Downloader
	installer  Installer
	verifier   HealthVerifier
	opts       ManagerOptions
}

// ManagerOptions contains options for the upgrade manager
type ManagerOptions struct {
	// InstallPath is where to install binaries
	InstallPath string
	// TempDir is where to store temporary files
	TempDir string
	// ServiceName is the systemd service name
	ServiceName string
	// HealthTimeout is the timeout for health checks
	HealthTimeout time.Duration
	// SkipHealthCheck skips post-upgrade health verification
	SkipHealthCheck bool
	// AutoRollback enables automatic rollback on health check failure
	AutoRollback bool
	// PathSymlink, when non-empty, is a location on $PATH that is kept as a
	// symlink to InstallPath after a successful server/runner upgrade so the
	// documented CLI stays in sync with the managed binary. Empty disables it
	// (e.g. for user/CLI-only upgrades).
	PathSymlink string
}

// DefaultManagerOptions returns default manager options
func DefaultManagerOptions() ManagerOptions {
	// Use os.TempDir() which respects TMPDIR environment variable
	return ManagerOptions{
		InstallPath:     "/var/lib/miren/release/miren",
		TempDir:         os.TempDir(),
		ServiceName:     "miren",
		HealthTimeout:   60 * time.Second,
		SkipHealthCheck: false,
		AutoRollback:    true,
		PathSymlink:     SystemCLIPath,
	}
}

// RunnerManagerOptions returns manager options configured for the runner service.
// The install path is the same as the server (shared binary), but the service
// name targets the miren-runner systemd unit.
func RunnerManagerOptions() ManagerOptions {
	opts := DefaultManagerOptions()
	opts.ServiceName = "miren-runner"
	return opts
}

// NewManager creates a new upgrade manager
func NewManager(opts ManagerOptions) *Manager {
	installOpts := InstallOptions{
		InstallPath:  opts.InstallPath,
		BackupSuffix: ".old",
	}

	healthOpts := DefaultHealthCheckOptions()
	healthOpts.ServiceName = opts.ServiceName

	return &Manager{
		downloader: NewDownloader(),
		installer:  NewInstaller(installOpts),
		verifier:   NewHealthVerifier(healthOpts),
		opts:       opts,
	}
}

// WithDownloader sets a custom downloader
func (m *Manager) WithDownloader(d Downloader) *Manager {
	m.downloader = d
	return m
}

// WithInstaller sets a custom installer
func (m *Manager) WithInstaller(i Installer) *Manager {
	m.installer = i
	return m
}

// WithHealthVerifier sets a custom health verifier
func (m *Manager) WithHealthVerifier(v HealthVerifier) *Manager {
	m.verifier = v
	return m
}

// UpgradeArtifact performs a complete upgrade operation
func (m *Manager) UpgradeArtifact(ctx context.Context, artifact Artifact) error {
	// Get current version for reporting
	currentVersion, _ := m.installer.GetCurrentVersion(ctx)

	// Download the artifact
	fmt.Printf("Downloading %s version %s...\n", artifact.Type, artifact.Version)
	downloadOpts := DownloadOptions{
		TargetDir:      m.opts.TempDir,
		ProgressWriter: NewProgressWriter(os.Stdout),
	}

	downloaded, err := m.downloader.Download(ctx, artifact, downloadOpts)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Install the artifact
	fmt.Printf("Installing new binary...\n")
	if err := m.installer.Install(ctx, downloaded); err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	// Report version change
	newVersion, _ := m.installer.GetCurrentVersion(ctx)
	fmt.Printf("Upgraded from %s to %s\n", currentVersion.Version, newVersion.Version)

	return nil
}

// UpgradeServer upgrades the server binary and restarts the service
func (m *Manager) UpgradeServer(ctx context.Context, artifact Artifact) error {
	// Perform the upgrade
	if err := m.UpgradeArtifact(ctx, artifact); err != nil {
		return err
	}

	// Restart the service
	fmt.Printf("Restarting %s service...\n", m.opts.ServiceName)
	if err := m.restartService(ctx); err != nil {
		return fmt.Errorf("service restart failed: %w", err)
	}

	// Verify health unless skipped
	if !m.opts.SkipHealthCheck {
		fmt.Printf("Verifying service health...\n")
		if err := m.verifier.VerifyHealth(ctx, m.opts.HealthTimeout); err != nil {
			fmt.Printf("Health check failed: %v\n", err)

			if m.opts.AutoRollback && m.installer.HasBackup() {
				fmt.Printf("Attempting automatic rollback...\n")
				if rollbackErr := m.Rollback(ctx); rollbackErr != nil {
					return fmt.Errorf("health check failed and rollback failed: %v (rollback error: %w)", err, rollbackErr)
				}
				return fmt.Errorf("health check failed, successfully rolled back: %w", err)
			}

			return fmt.Errorf("health check failed: %w", err)
		}
		fmt.Printf("Service is healthy\n")
	}

	// Keep the documented on-$PATH CLI pointed at the binary we just installed
	// and verified. Best-effort: the upgrade itself already succeeded, so a
	// symlink failure is a warning rather than a hard error (and never triggers
	// a rollback of a healthy server).
	m.ensurePathSymlink()

	fmt.Printf("Upgrade successful!\n")
	return nil
}

// ensurePathSymlink points m.opts.PathSymlink at the managed InstallPath when a
// PathSymlink is configured. Failures are logged, not returned, so they don't
// undo an otherwise-successful upgrade.
func (m *Manager) ensurePathSymlink() {
	if m.opts.PathSymlink == "" {
		return
	}
	switch err := EnsurePathSymlink(m.opts.InstallPath, m.opts.PathSymlink); {
	case errors.Is(err, ErrPathManagedElsewhere):
		fmt.Fprintf(os.Stderr, "Note: leaving %s alone, it looks managed by another tool (e.g. Homebrew); the CLI on your $PATH may not track the server.\n", m.opts.PathSymlink)
	case err != nil:
		fmt.Fprintf(os.Stderr, "Warning: failed to update %s symlink: %v\n", m.opts.PathSymlink, err)
	default:
		fmt.Printf("Updated %s -> %s\n", m.opts.PathSymlink, m.opts.InstallPath)
	}
}

// RestartAndVerify restarts the systemd service and verifies its health
// without performing any download or installation. Used by the drift-recovery
// path when the running daemon is stale relative to the on-disk binary (e.g.,
// after `miren upgrade` rewrote the release-dir binary but no service restart
// has happened yet).
func (m *Manager) RestartAndVerify(ctx context.Context) error {
	fmt.Printf("Restarting %s service...\n", m.opts.ServiceName)
	if err := m.restartService(ctx); err != nil {
		return fmt.Errorf("service restart failed: %w", err)
	}

	if !m.opts.SkipHealthCheck {
		fmt.Printf("Verifying service health...\n")
		if err := m.verifier.VerifyHealth(ctx, m.opts.HealthTimeout); err != nil {
			return fmt.Errorf("health check failed: %w", err)
		}
		fmt.Printf("Service is healthy\n")
	}

	return nil
}

// Rollback rolls back to the previous version
func (m *Manager) Rollback(ctx context.Context) error {
	// Check if backup exists
	if !m.installer.HasBackup() {
		return fmt.Errorf("no backup available for rollback")
	}

	// Get current version for reporting
	currentVersion, _ := m.installer.GetCurrentVersion(ctx)

	// Perform the rollback
	fmt.Printf("Rolling back from %s...\n", currentVersion.Version)
	if err := m.installer.Rollback(ctx); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	// Get rolled back version
	rolledBackVersion, _ := m.installer.GetCurrentVersion(ctx)
	fmt.Printf("Rolled back to %s\n", rolledBackVersion.Version)

	// Restart the service
	fmt.Printf("Restarting %s service...\n", m.opts.ServiceName)
	if err := m.restartService(ctx); err != nil {
		return fmt.Errorf("service restart failed: %w", err)
	}

	// Verify health unless skipped
	if !m.opts.SkipHealthCheck {
		fmt.Printf("Verifying service health...\n")
		if err := m.verifier.VerifyHealth(ctx, m.opts.HealthTimeout); err != nil {
			return fmt.Errorf("health check failed after rollback: %w", err)
		}
		fmt.Printf("Service is healthy\n")
	}

	fmt.Printf("Rollback successful!\n")
	return nil
}

// GetCurrentVersion returns the current installed version
func (m *Manager) GetCurrentVersion(ctx context.Context) (VersionInfo, error) {
	return m.installer.GetCurrentVersion(ctx)
}

// GetLatestVersion returns the latest available version
func (m *Manager) GetLatestVersion(ctx context.Context, artifactType ArtifactType) (string, error) {
	return m.downloader.GetLatestVersion(ctx, artifactType)
}

// CheckForUpdate checks if an update is available
func (m *Manager) CheckForUpdate(ctx context.Context, artifactType ArtifactType) (bool, string, error) {
	current, err := m.GetCurrentVersion(ctx)
	if err != nil {
		return false, "", err
	}

	latest, err := m.GetLatestVersion(ctx, artifactType)
	if err != nil {
		return false, "", err
	}

	metadata, err := m.downloader.GetVersionMetadata(ctx, latest)
	if err != nil {
		return false, "", fmt.Errorf("failed to get metadata for %s: %w", latest, err)
	}

	latestInfo := VersionInfo{
		Version:   metadata.Version,
		Commit:    metadata.Commit,
		BuildDate: metadata.BuildDate,
	}

	hasUpdate := latestInfo.IsNewer(current)
	return hasUpdate, latestInfo.Version, nil
}

// restartService restarts the systemd service
func (m *Manager) restartService(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "systemctl", "restart", m.opts.ServiceName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart failed: %w\nOutput: %s", err, output)
	}
	return nil
}
