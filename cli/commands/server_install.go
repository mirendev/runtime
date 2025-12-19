//go:build linux

package commands

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/quic-go/quic-go/http3"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/version"
)

// installPrerequisites holds information about the system's readiness for installation
type installPrerequisites struct {
	hasRoot    bool
	hasSystemd bool
	hasDocker  bool
}

// checkInstallPrerequisites checks all prerequisites and returns their status
func checkInstallPrerequisites() installPrerequisites {
	return installPrerequisites{
		hasRoot:    os.Geteuid() == 0,
		hasSystemd: checkSystemdAvailable(),
		hasDocker:  checkDockerAvailable() == nil,
	}
}

// checkSystemdAvailable checks if systemd is available on the system
func checkSystemdAvailable() bool {
	// Check if systemctl exists and is functional
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}

	// Verify systemd is actually running by checking if we can communicate with it
	cmd := exec.Command("systemctl", "--version")
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

// printInstallPrerequisiteGuidance prints helpful guidance based on what's available
func printInstallPrerequisiteGuidance(ctx *Context, prereqs installPrerequisites) {
	fmt.Println()
	ctx.Warn("Cannot proceed with systemd installation.")
	fmt.Println()

	if !prereqs.hasRoot {
		ctx.Info("Root privileges are required for systemd installation.")
		fmt.Println("  Run with sudo: sudo miren server install")
		fmt.Println()
	}

	if !prereqs.hasSystemd {
		ctx.Info("systemd is not available on this system.")
		fmt.Println()

		if prereqs.hasDocker {
			ctx.Info("Docker is available! You can install using Docker instead:")
			fmt.Println("  miren server docker install")
			fmt.Println()
			ctx.Info("This will run the miren server in a Docker container with automatic restarts.")
		} else {
			ctx.Info("Alternative installation options:")
			fmt.Println()
			fmt.Println("  1. Install using Docker (recommended for non-systemd systems):")
			fmt.Println("     First install Docker: https://docs.docker.com/get-docker/")
			fmt.Println("     Then run: miren server docker install")
			fmt.Println()
			fmt.Println("  2. Run the server directly (for testing or development):")
			fmt.Println("     miren server")
			fmt.Println()
			fmt.Println("  3. Use your system's init system to manage the miren server process")
		}
	}
}

// fixSELinuxContext sets the proper SELinux context on the miren binary
// so systemd can execute it. This is needed on systems like RHEL/Oracle Linux
// where SELinux is enforcing and /var/lib files get var_lib_t context by default.
func fixSELinuxContext(ctx *Context, binaryPath string) {
	// Check if SELinux is enforcing by running getenforce
	cmd := exec.Command("getenforce")
	output, err := cmd.Output()
	if err != nil {
		// getenforce not found or failed - SELinux probably not installed
		ctx.Log.Debug("getenforce failed, assuming SELinux not active", "error", err)
		return
	}

	status := strings.TrimSpace(string(output))
	if status != "Enforcing" {
		ctx.Log.Debug("SELinux not enforcing", "status", status)
		return
	}

	ctx.Info("SELinux is enforcing, configuring executable context for binary...")

	// Try to add a permanent file context rule using semanage
	// This requires policycoreutils-python-utils on RHEL/Oracle Linux
	semanageCmd := exec.Command("semanage", "fcontext", "-a", "-t", "bin_t", binaryPath)
	if output, err := semanageCmd.CombinedOutput(); err != nil {
		// semanage might not be installed, or rule might already exist
		outputStr := string(output)
		if strings.Contains(outputStr, "already defined") || strings.Contains(outputStr, "already exists") {
			ctx.Log.Debug("SELinux file context rule already exists")
		} else {
			ctx.Log.Debug("semanage fcontext failed", "error", err, "output", outputStr)
			// Fall back to chcon if semanage isn't available
			ctx.Info("semanage not available, using chcon (context may not persist across relabels)")
			chconCmd := exec.Command("chcon", "-t", "bin_t", binaryPath)
			if output, err := chconCmd.CombinedOutput(); err != nil {
				ctx.Warn("Failed to set SELinux context: %v\nOutput: %s", err, output)
				ctx.Warn("The service may fail to start. Try running: sudo chcon -t bin_t %s", binaryPath)
				return
			}
			ctx.Completed("SELinux context set (temporary)")
			return
		}
	}

	// Apply the context with restorecon
	restoreconCmd := exec.Command("restorecon", "-v", binaryPath)
	if output, err := restoreconCmd.CombinedOutput(); err != nil {
		ctx.Warn("Failed to apply SELinux context with restorecon: %v\nOutput: %s", err, output)
		// Try chcon as fallback
		chconCmd := exec.Command("chcon", "-t", "bin_t", binaryPath)
		if output, err := chconCmd.CombinedOutput(); err != nil {
			ctx.Warn("Failed to set SELinux context: %v\nOutput: %s", err, output)
			ctx.Warn("The service may fail to start. Try running: sudo chcon -t bin_t %s", binaryPath)
		} else {
			ctx.Completed("SELinux context set (temporary)")
		}
	} else {
		ctx.Completed("SELinux context configured (persistent)")
	}
}

// ServerInstall sets up systemd units to run the miren server
func ServerInstall(ctx *Context, opts struct {
	Address      string            `short:"a" long:"address" description:"Server address to bind to" default:"0.0.0.0:8443"`
	Verbosity    string            `short:"v" long:"verbosity" description:"Verbosity level" default:"-vv"`
	Branch       string            `short:"b" long:"branch" description:"Branch to download if release not found"`
	Force        bool              `short:"f" long:"force" description:"Overwrite existing service file"`
	NoStart      bool              `long:"no-start" description:"Do not start the service after installation"`
	WithoutCloud bool              `long:"without-cloud" description:"Skip cloud registration setup"`
	ClusterName  string            `short:"n" long:"name" description:"Cluster name for cloud registration"`
	CloudURL     string            `short:"u" long:"url" description:"Cloud URL for registration" default:"https://miren.cloud"`
	Tags         map[string]string `short:"t" long:"tag" description:"Tags for the cluster (key:value)"`
}) error {
	if opts.Branch == "" {
		if br := version.Branch(); br != "unknown" {
			opts.Branch = br
		} else {
			opts.Branch = "latest"
		}
	}

	// Check all prerequisites upfront
	prereqs := checkInstallPrerequisites()

	if !prereqs.hasRoot || !prereqs.hasSystemd {
		printInstallPrerequisiteGuidance(ctx, prereqs)
		if !prereqs.hasRoot {
			return fmt.Errorf("root privileges required")
		}
		return fmt.Errorf("systemd not available")
	}

	ctx.Completed("Prerequisites verified (root, systemd)")

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

		// Fix SELinux context if needed (RHEL/Oracle Linux with SELinux enforcing)
		fixSELinuxContext(ctx, mirenPath)
	}

	// Register with cloud unless --without-cloud is specified
	if !opts.WithoutCloud {
		// Check if already registered
		existing, err := registration.LoadRegistration("/var/lib/miren/server")
		if err == nil && existing != nil && existing.Status == "approved" {
			ctx.Info("Cluster already registered as '%s' (ID: %s)", existing.ClusterName, existing.ClusterID)
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
				ctx.Info("You can register later with: miren server register")
			} else {
				ctx.Completed("Cloud registration complete")
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
				ctx.Info("You can register later with: miren server register")
			} else {
				ctx.Completed("Cloud registration complete")
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
		} else {
			ctx.Warn("Service may not be running, check status with: systemctl status miren")
		}

		// Wait for server to be ready
		ctx.Info("Waiting for miren server to initialize...")
		if err := waitForSystemdServerReady(ctx, opts.Address); err != nil {
			ctx.Warn("Failed to confirm server is ready: %v", err)
			ctx.Info("The server may still be starting. Check logs with: journalctl -u miren -f")
		} else {
			ctx.Completed("Server is ready")
		}

		// Configure client to connect to local server
		ctx.Info("Configuring miren client...")
		if err := configureLocalClient(ctx, opts.Address); err != nil {
			ctx.Warn("Failed to configure client: %v", err)
			ctx.Info("You may need to configure the client manually")
		} else {
			ctx.Completed("Client configuration saved")
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

// waitForSystemdServerReady waits for the miren server to be ready, checking both
// the health endpoint and the systemd service status
func waitForSystemdServerReady(ctx *Context, serverAddress string) error {
	maxRetries := 30
	retryDelay := 2 * time.Second

	// Parse the server address to build the health URL
	// Server install always uses --serve-tls so it's always HTTPS
	host, port, err := net.SplitHostPort(serverAddress)
	if err != nil {
		// No port specified, use default
		host = serverAddress
		port = "8443"
	}

	// Replace 0.0.0.0 or empty host with localhost for health checks
	if host == "0.0.0.0" || host == "" {
		host = "localhost"
	}

	healthURL := fmt.Sprintf("https://%s/healthz", net.JoinHostPort(host, port))

	ctx.Log.Debug("checking server health", "host", host, "port", port, "max_retries", maxRetries)

	// Create HTTP3 client with InsecureSkipVerify since we're using self-signed certs
	client := &http.Client{
		Transport: &http3.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 5 * time.Second,
	}
	defer client.CloseIdleConnections()

	ctx.Log.Debug("health check endpoint", "url", healthURL)

	for i := range maxRetries {
		// First check if the systemd service is still running
		statusCmd := exec.Command("systemctl", "is-active", "miren.service")
		output, err := statusCmd.Output()
		status := strings.TrimSpace(string(output))

		if err != nil || (status != "active" && status != "activating") {
			ctx.Log.Error("systemd service is not running", "status", status)
			return fmt.Errorf("miren service stopped unexpectedly (status: %s)", status)
		}

		// Try to connect to the server's health endpoint via HTTP3
		reqCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, "GET", healthURL, nil)
		if err != nil {
			cancel()
			return fmt.Errorf("failed to create request: %w", err)
		}

		ctx.Log.Debug("attempting health check", "attempt", i+1, "max_retries", maxRetries)
		resp, err := client.Do(req)
		cancel()

		if err == nil {
			resp.Body.Close()
			ctx.Log.Info("server health check successful", "attempt", i+1)
			return nil
		}

		ctx.Log.Debug("health check failed", "attempt", i+1, "error", err)

		if i < maxRetries-1 {
			ctx.Info("Waiting for server... (attempts remaining: %d)", maxRetries-i-1)
			time.Sleep(retryDelay)
		}
	}

	ctx.Log.Error("server failed to become ready", "max_retries", maxRetries, "url", healthURL)
	return fmt.Errorf("timeout waiting for server to start")
}

// configureLocalClient generates and saves client configuration for the local server
func configureLocalClient(ctx *Context, serverAddress string) error {
	mirenPath := "/var/lib/miren/release/miren"

	// Extract port from server address (format: host:port)
	port := "8443"
	if idx := strings.LastIndex(serverAddress, ":"); idx != -1 {
		port = serverAddress[idx+1:]
	}
	target := "localhost:" + port

	ctx.Log.Debug("generating client config", "target", target)

	// Generate client config by running miren auth generate
	cmd := exec.Command(mirenPath, "auth", "generate", "-C", "local", "-t", target, "-c", "-")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			ctx.Log.Error("failed to generate client config", "error", err, "stderr", string(exitErr.Stderr))
			return fmt.Errorf("failed to generate client config: %w: %s", err, exitErr.Stderr)
		}
		ctx.Log.Error("failed to generate client config", "error", err)
		return fmt.Errorf("failed to generate client config: %w", err)
	}

	ctx.Log.Debug("auth generate output received", "output_length", len(output))

	if len(strings.TrimSpace(string(output))) == 0 {
		ctx.Log.Error("generated client config is empty")
		return fmt.Errorf("generated client config is empty")
	}

	// Parse the generated config to extract cluster information
	var generatedConfig clientconfig.ConfigData
	if err := yaml.Unmarshal(output, &generatedConfig); err != nil {
		ctx.Log.Error("failed to parse generated config", "error", err)
		return fmt.Errorf("failed to parse generated config: %w", err)
	}

	ctx.Log.Debug("parsed generated config", "clusters", len(generatedConfig.Clusters))

	// Verify the local cluster exists in the generated config
	localCluster, ok := generatedConfig.Clusters["local"]
	if !ok {
		ctx.Log.Error("local cluster not found in generated config", "available_clusters", generatedConfig.Clusters)
		return fmt.Errorf("local cluster not found in generated config")
	}

	ctx.Log.Debug("found local cluster in config", "hostname", localCluster.Hostname)

	// Load existing user config
	config, err := clientconfig.LoadConfig()
	if err != nil {
		if !errors.Is(err, clientconfig.ErrNoConfig) {
			ctx.Log.Error("failed to load existing client config", "error", err)
			return fmt.Errorf("failed to load existing client config: %w", err)
		}
		config = clientconfig.NewConfig()
	} else {
		ctx.Log.Debug("loaded existing client config", "active_cluster", config.ActiveCluster())
	}

	// Create leaf config data with the local cluster
	leafConfigData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			"local": localCluster,
		},
	}

	// Add as a leaf config (this will be saved to clientconfig.d/50-local.yaml)
	ctx.Log.Debug("setting leaf config", "name", "50-local")
	config.SetLeafConfig("50-local", leafConfigData)

	// Set the active cluster to local if none is set
	if config.ActiveCluster() == "" {
		ctx.Log.Debug("setting active cluster to local")
		config.SetActiveCluster("local")
	}

	// Save the config
	ctx.Log.Debug("saving client config")
	if err := config.Save(); err != nil {
		ctx.Log.Error("failed to save local cluster leaf config", "error", err)
		return fmt.Errorf("failed to save local cluster leaf config: %w", err)
	}

	// Fix ownership and permissions if running under sudo
	spath := config.SourcePath()
	if spath != "" {
		// Fix ownership for all parent directories that we may have created
		pathsToFix := []string{
			filepath.Dir(filepath.Dir(spath)),
			filepath.Dir(spath),
			filepath.Join(filepath.Dir(spath), "clientconfig.d"),
			filepath.Join(filepath.Dir(spath), "clientconfig.d", "50-local.yaml"),
			spath,
		}

		for _, entry := range pathsToFix {
			if err := fixOwnershipIfSudo(entry); err != nil {
				ctx.Log.Warn("failed to fix directory ownership", "dir", entry, "error", err)
			}

			fi, err := os.Stat(entry)
			if err != nil {
				ctx.Log.Warn("failed to stat path for permission fix", "path", entry, "error", err)
				continue
			}

			// Only fix permissions on files, not directories
			if fi.IsDir() {
				continue
			}

			if err := os.Chmod(entry, 0600); err != nil {
				ctx.Log.Warn("failed to set config file permissions", "path", entry, "error", err)
			}
		}
	}

	ctx.Log.Info("wrote local cluster config", "cluster", "local", "address", localCluster.Hostname)
	return nil
}
