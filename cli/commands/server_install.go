//go:build linux

package commands

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/registration"
)

// ServerInstall sets up systemd units to run the miren server
func ServerInstall(ctx *Context, opts struct {
	Address      string            `short:"a" long:"address" description:"Server address to bind to" default:"0.0.0.0:8443"`
	Verbosity    string            `short:"v" long:"verbosity" description:"Verbosity level" default:"-vv"`
	Branch       string            `short:"b" long:"branch" description:"Branch to download if release not found" default:"main"`
	Force        bool              `short:"f" long:"force" description:"Overwrite existing service file"`
	NoStart      bool              `long:"no-start" description:"Do not start the service after installation"`
	WithoutCloud bool              `long:"without-cloud" description:"Skip cloud registration setup"`
	ClusterName  string            `short:"n" long:"name" description:"Cluster name for cloud registration"`
	CloudURL     string            `short:"u" long:"url" description:"Cloud URL for registration" default:"https://miren.cloud"`
	Tags         map[string]string `short:"t" long:"tag" description:"Tags for the cluster (key:value)"`
}) error {
	// Check if running with sufficient privileges
	if os.Geteuid() != 0 {
		return fmt.Errorf("server install requires root privileges (use sudo)")
	}

	// Check if miren binary exists, download if not
	mirenPath := "/var/lib/miren/release/miren"
	if _, err := os.Stat(mirenPath); err != nil {
		ctx.Info("Miren release not found at %s, downloading...", mirenPath)

		// Download the release
		if err := PerformDownloadRelease(ctx, DownloadReleaseOptions{
			Branch: opts.Branch,
			Global: true,
			Force:  false,
			Output: "/var/lib/miren/release",
		}); err != nil {
			return fmt.Errorf("failed to download release: %w", err)
		}

		ctx.Completed("Release downloaded successfully")
	}

	// Track cluster name for later adding to client config
	var registeredClusterName string

	// Register with cloud unless --without-cloud is specified
	if !opts.WithoutCloud {
		// Check if already registered
		existing, err := registration.LoadRegistration("/var/lib/miren/server")
		if err == nil && existing != nil && existing.Status == "approved" {
			ctx.Info("Cluster already registered as '%s' (ID: %s)", existing.ClusterName, existing.ClusterID)
			registeredClusterName = existing.ClusterName
		} else if err == nil && existing != nil && existing.Status == "pending" {
			ctx.Info("Found pending registration for cluster '%s', attempting to complete...", existing.ClusterName)

			// Use cluster name from flag or existing registration
			clusterName := opts.ClusterName
			if clusterName == "" {
				clusterName = existing.ClusterName
			}

			registerOpts := RegisterOptions{
				ClusterName: clusterName,
				CloudURL:    opts.CloudURL,
				Tags:        opts.Tags,
				OutputDir:   "/var/lib/miren/server",
			}

			if err := Register(ctx, registerOpts); err != nil {
				ctx.Warn("Cloud registration failed: %v", err)
				ctx.Info("Continuing with installation without cloud registration")
				ctx.Info("You can register later with: miren register")
			} else {
				ctx.Completed("Cloud registration complete")
				registeredClusterName = clusterName
			}
		} else {
			ctx.Info("Setting up cloud registration...")

			// Use cluster name from flag or hostname
			clusterName := opts.ClusterName
			if clusterName == "" {
				hostname, err := os.Hostname()
				if err != nil {
					hostname = "miren-cluster"
				}
				clusterName = hostname
			}

			registerOpts := RegisterOptions{
				ClusterName: clusterName,
				CloudURL:    opts.CloudURL,
				Tags:        opts.Tags,
				OutputDir:   "/var/lib/miren/server",
			}

			if err := Register(ctx, registerOpts); err != nil {
				ctx.Warn("Cloud registration failed: %v", err)
				ctx.Info("Continuing with installation without cloud registration")
				ctx.Info("You can register later with: miren register")
			} else {
				ctx.Completed("Cloud registration complete")
				registeredClusterName = clusterName
			}
		}
	} else {
		ctx.Info("Skipping cloud registration (--without-cloud specified)")
	}

	ctx.Info("Installing miren systemd service...")

	// Check if service file already exists
	servicePath := "/etc/systemd/system/miren.service"
	serviceExists := false
	if _, err := os.Stat(servicePath); err == nil {
		serviceExists = true
		if !opts.Force {
			ctx.Info("Service file already exists at %s (skipping, use --force to overwrite)", servicePath)
		} else {
			ctx.Info("Service file exists, overwriting due to --force flag")
		}
	}

	// Only write service file if it doesn't exist or --force is specified
	if !serviceExists || opts.Force {
		// Build ExecStart command
		var execStartParts []string
		execStartParts = append(execStartParts, mirenPath, "server")

		if opts.Verbosity != "" {
			execStartParts = append(execStartParts, opts.Verbosity)
		}

		if opts.Address != "" {
			execStartParts = append(execStartParts, fmt.Sprintf("--address=%s", opts.Address))
		}

		execStartParts = append(execStartParts, "--serve-tls")

		execStart := strings.Join(execStartParts, " ")

		// Create systemd service file content
		serviceContent := fmt.Sprintf(`[Unit]
Description=Miren Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment="NO_COLOR=1"
ExecStart=%s
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=miren
User=root
WorkingDirectory=/var/lib/miren/release
KillMode=process
TimeoutStopSec=90s

[Install]
WantedBy=multi-user.target
`, execStart)

		ctx.Log.Info("creating systemd service file", "path", servicePath)

		// Write the service file
		if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
			return fmt.Errorf("failed to write service file: %w", err)
		}

		ctx.Completed("Service file created at %s", servicePath)

		// Reload systemd
		ctx.Info("Reloading systemd daemon...")
		cmd := exec.Command("systemctl", "daemon-reload")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to reload systemd: %w\nOutput: %s", err, output)
		}
	}

	// Enable the service (and optionally start it)
	if opts.NoStart {
		ctx.Info("Enabling miren service...")
		cmd := exec.Command("systemctl", "enable", "miren.service")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to enable service: %w\nOutput: %s", err, output)
		}
		ctx.Completed("Miren service enabled (but not started)")
	} else {
		ctx.Info("Enabling and starting miren service...")
		cmd := exec.Command("systemctl", "enable", "--now", "miren.service")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to enable and start service: %w\nOutput: %s", err, output)
		}
		ctx.Completed("Miren service enabled and started")

		// Check service status
		statusCmd := exec.Command("systemctl", "is-active", "miren.service")
		if output, err := statusCmd.CombinedOutput(); err == nil && strings.TrimSpace(string(output)) == "active" {
			ctx.Completed("Service is running")

			// Add cluster to client config and switch to it
			if registeredClusterName != "" {
				if err := configureClientForCluster(ctx, registeredClusterName, opts.Address); err != nil {
					ctx.Warn("Failed to configure client: %v", err)
					ctx.Info("You can manually add the cluster with: miren cluster add -c %s", registeredClusterName)
				}
			}
		} else {
			ctx.Warn("Service may not be running, check status with: systemctl status miren")
		}
	}

	// Print helpful next steps
	fmt.Println()
	ctx.Info("Installation complete!")
	fmt.Println()
	ctx.Info("To check service status:")
	fmt.Println("  systemctl status miren")
	fmt.Println()
	ctx.Info("To view logs:")
	fmt.Println("  journalctl -u miren -f")

	return nil
}

// configureClientForCluster adds the cluster to the client config and sets it as active.
// This allows the user to immediately use `miren` commands against the local cluster.
func configureClientForCluster(ctx *Context, clusterName, serverAddress string) error {
	// Determine the local address to connect to
	// The serverAddress is the bind address (e.g., "0.0.0.0:8443"), we need to connect to localhost
	port := "8443"
	if parts := strings.Split(serverAddress, ":"); len(parts) == 2 {
		port = parts[1]
	}
	localAddress := "localhost:" + port

	// Wait for the server to be fully ready and extract TLS certificate
	ctx.Info("Waiting for server to be ready...")
	var caCert, fingerprint string
	var err error
	for i := 0; i < 5; i++ {
		time.Sleep(2 * time.Second)
		caCert, fingerprint, err = extractTLSCertificateFromAddress(ctx, localAddress)
		if err == nil {
			break
		}
		if i < 4 {
			ctx.Info("Server not ready yet, retrying...")
		}
	}
	if err != nil {
		return fmt.Errorf("failed to extract TLS certificate: %w", err)
	}
	ctx.Info("Certificate fingerprint: %s", fingerprint)

	// Load or create client config
	mainConfig, err := clientconfig.LoadConfig()
	if err != nil {
		if err == clientconfig.ErrNoConfig {
			mainConfig = clientconfig.NewConfig()
		} else {
			return fmt.Errorf("failed to load client config: %w", err)
		}
	}

	// Get the identity to use (if any configured)
	var identity string
	if mainConfig != nil && mainConfig.HasIdentities() {
		identities := mainConfig.GetIdentityNames()
		if len(identities) == 1 {
			identity = identities[0]
		} else if len(identities) > 1 {
			// Use the first identity, user can change later
			identity = identities[0]
			ctx.Info("Using identity '%s' (multiple available)", identity)
		}
	}

	// Create cluster config
	clusterConfig := &clientconfig.ClusterConfig{
		Hostname: localAddress,
		Identity: identity,
		CACert:   caCert,
	}

	// Create leaf config data
	leafConfigData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			clusterName: clusterConfig,
		},
	}

	// Add as leaf config
	mainConfig.SetLeafConfig(clusterName, leafConfigData)

	// Set as active cluster
	if err := mainConfig.SetActiveCluster(clusterName); err != nil {
		return fmt.Errorf("failed to set active cluster: %w", err)
	}

	// Save config
	if err := mainConfig.Save(); err != nil {
		return fmt.Errorf("failed to save client config: %w", err)
	}

	// If running as root via sudo, chown the config files to the invoking user
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			configDir := filepath.Join(u.HomeDir, ".config", "miren")
			// Chown the main config directory and its contents
			_ = filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
				if err == nil {
					_ = os.Chown(path, uid, gid)
				}
				return nil
			})
		}
	}

	ctx.Completed("Added cluster '%s' and set as active", clusterName)
	return nil
}

// ServerUninstall removes the systemd service and optionally removes /var/lib/miren
func ServerUninstall(ctx *Context, opts struct {
	RemoveData bool   `long:"remove-data" description:"Remove /var/lib/miren directory after backing it up"`
	BackupDir  string `long:"backup-dir" description:"Directory to save backup tarball" default:"."`
	SkipBackup bool   `long:"skip-backup" description:"Skip backup when removing data (dangerous)"`
}) error {
	// Check if running with sufficient privileges
	if os.Geteuid() != 0 {
		return fmt.Errorf("server uninstall requires root privileges (use sudo)")
	}

	servicePath := "/etc/systemd/system/miren.service"

	// Check if service file exists
	if _, err := os.Stat(servicePath); os.IsNotExist(err) {
		return fmt.Errorf("service file not found at %s", servicePath)
	}

	// Check if service is running
	isRunningCmd := exec.Command("systemctl", "is-active", "miren.service")
	output, err := isRunningCmd.CombinedOutput()
	isRunning := err == nil && strings.TrimSpace(string(output)) == "active"

	// If service is running, drain it first
	if isRunning {
		ctx.Info("Service is running, draining before shutdown...")
		ctx.Info("Sending SIGUSR2 to drain runner...")

		// Send SIGUSR2 to the service using systemctl kill
		killCmd := exec.Command("systemctl", "kill", "-s", "SIGUSR2", "miren.service")
		if err := killCmd.Run(); err != nil {
			ctx.Warn("Failed to send SIGUSR2: %v", err)
		} else {
			ctx.Info("Drain signal sent, waiting 5 seconds for graceful shutdown...")
			time.Sleep(5 * time.Second)
		}
	}

	// Stop the service
	ctx.Info("Stopping miren service...")
	cmd := exec.Command("systemctl", "stop", "miren.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		ctx.Warn("Failed to stop service: %v\nOutput: %s", err, output)
	} else {
		ctx.Completed("Service stopped")
	}

	// Disable the service
	ctx.Info("Disabling miren service...")
	cmd = exec.Command("systemctl", "disable", "miren.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		ctx.Warn("Failed to disable service: %v\nOutput: %s", err, output)
	} else {
		ctx.Completed("Service disabled")
	}

	// Remove the service file
	ctx.Info("Removing service file...")
	if err := os.Remove(servicePath); err != nil {
		return fmt.Errorf("failed to remove service file: %w", err)
	}

	ctx.Completed("Service file removed from %s", servicePath)

	// Reload systemd
	ctx.Info("Reloading systemd daemon...")
	cmd = exec.Command("systemctl", "daemon-reload")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w\nOutput: %s", err, output)
	}

	ctx.Completed("Systemd service uninstalled!")

	// Handle data directory removal
	mirenDataDir := "/var/lib/miren"
	if opts.RemoveData {
		// Check if directory exists
		if _, err := os.Stat(mirenDataDir); os.IsNotExist(err) {
			ctx.Info("Data directory %s does not exist, skipping removal", mirenDataDir)
		} else {
			// Create backup unless skipped
			var backupPath string
			if !opts.SkipBackup {
				timestamp := time.Now().Format("2006-01-02-150405")
				backupFilename := fmt.Sprintf("miren-backup-%s.tar.gz", timestamp)
				backupPath = fmt.Sprintf("%s/%s", opts.BackupDir, backupFilename)

				ctx.Info("Creating backup of %s...", mirenDataDir)
				if err := createTarGzBackup(mirenDataDir, backupPath); err != nil {
					return fmt.Errorf("failed to create backup: %w", err)
				}
				ctx.Completed("Backup created at %s", backupPath)
			} else {
				ctx.Warn("Skipping backup as requested")
			}

			// Remove the directory
			ctx.Info("Removing %s...", mirenDataDir)
			if err := os.RemoveAll(mirenDataDir); err != nil {
				if backupPath != "" {
					ctx.Warn("Failed to remove data directory, but backup is safe at: %s", backupPath)
				}
				return fmt.Errorf("failed to remove data directory: %w", err)
			}
			ctx.Completed("Data directory removed")

			if backupPath != "" {
				fmt.Println()
				ctx.Info("Backup saved to: %s", backupPath)
			}
		}
	} else {
		// Print note about release directory
		fmt.Println()
		ctx.Info("Note: The miren data at /var/lib/miren has not been removed.")
		ctx.Info("To remove it with backup: sudo miren server uninstall --remove-data")
		ctx.Info("To remove it without backup: sudo rm -rf /var/lib/miren")
	}

	return nil
}

// ServerStatus shows the status of the miren systemd service
func ServerStatus(ctx *Context, opts struct {
	Follow bool `short:"f" long:"follow" description:"Follow logs in real-time"`
}) error {
	servicePath := "/etc/systemd/system/miren.service"

	// Check if service file exists
	if _, err := os.Stat(servicePath); os.IsNotExist(err) {
		ctx.Warn("Service file not found at %s", servicePath)
		ctx.Info("The miren service is not installed. Run 'sudo miren server install' to set it up.")
		return nil
	}

	// Show service status
	cmd := exec.Command("systemctl", "status", "miren.service", "--no-pager")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// systemctl status returns non-zero if service is not running, which is fine
		ctx.Log.Debug("systemctl status returned error", "error", err)
	}

	// If follow flag is set, tail the logs
	if opts.Follow {
		fmt.Println()
		ctx.Info("Following logs (Ctrl+C to stop)...")
		fmt.Println()

		cmd = exec.Command("journalctl", "-u", "miren", "-f", "--no-pager")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		return cmd.Run()
	}

	return nil
}

// createTarGzBackup creates a tar.gz backup of the specified directory
func createTarGzBackup(sourceDir, targetPath string) error {
	// Create the output file
	outFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer outFile.Close()

	// Create gzip writer
	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	// Walk the directory and add files to the tar
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header for %s: %w", path, err)
		}

		// Update header name to be relative to source directory
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}
		header.Name = relPath

		// Set modification time
		header.ModTime = info.ModTime().Truncate(time.Second)

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write tar header for %s: %w", path, err)
		}

		// If it's a regular file, write its contents
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file %s: %w", path, err)
			}
			defer file.Close()

			if _, err := io.Copy(tarWriter, file); err != nil {
				return fmt.Errorf("failed to write file %s to tar: %w", path, err)
			}
		}

		return nil
	})
}
