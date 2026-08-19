//go:build linux

package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"
	"miren.dev/runtime/pkg/enrolltoken"
	"miren.dev/runtime/pkg/runnerconfig"
	"miren.dev/runtime/pkg/ui"
	"miren.dev/runtime/version"
)

const (
	runnerServiceName = "miren-runner.service"
	runnerServicePath = "/etc/systemd/system/miren-runner.service"
)

// RunnerInstall sets up a systemd unit to run a miren distributed runner.
// It handles the full provisioning flow: downloading the release bundle,
// joining a coordinator (interactively or via flags), creating the systemd
// service, and enabling it.
func RunnerInstall(ctx *Context, opts struct {
	Token           string   `short:"t" long:"token" description:"Enrollment token from 'miren runner token create'"`
	Coordinator     string   `short:"c" long:"coordinator" description:"Override coordinator address from the token"`
	Name            string   `long:"name" description:"Human-readable name for this runner (defaults to hostname)"`
	ListenAddr      string   `short:"l" long:"listen" description:"Address this runner will listen on"`
	Labels          []string `long:"labels" description:"Runner labels (key=value)"`
	Branch          string   `short:"b" long:"branch" description:"Branch to download"`
	Force           bool     `short:"f" long:"force" description:"Overwrite existing service file"`
	NoStart         bool     `long:"no-start" description:"Do not start the service after installation"`
	SkipSystemCheck bool     `long:"skip-system-check" description:"Skip minimum system requirements check"`
	ConfigPath      string   `long:"config" description:"Path to runner config" default:"/var/lib/miren/runner/config.yaml"`
	DataPath        string   `long:"data-path" description:"Path to store runner data" default:"/var/lib/miren/runner"`
}) error {
	if opts.Branch == "" {
		if br := version.Branch(); br != "" {
			opts.Branch = br
		} else {
			opts.Branch = "latest"
		}
	}

	// Check prerequisites (root + systemd + networking commands)
	prereqs := checkInstallPrerequisites()
	if !prereqs.hasRoot || !prereqs.hasSystemd || len(prereqs.missingNetCommands) > 0 {
		printRunnerPrerequisiteGuidance(ctx, prereqs)
		if !prereqs.hasRoot {
			return fmt.Errorf("root privileges required")
		}
		if !prereqs.hasSystemd {
			return fmt.Errorf("systemd not available")
		}
		return fmt.Errorf("missing required commands: %s", strings.Join(prereqs.missingNetCommands, ", "))
	}
	ctx.Completed("Prerequisites verified (root, systemd, networking tools)")

	// Check system requirements (memory, disk space)
	if !opts.SkipSystemCheck {
		sysReqs := checkSystemRequirements()
		if printSystemRequirementsGuidance(ctx, sysReqs) {
			return fmt.Errorf("system does not meet minimum requirements")
		}
		ctx.Completed("System requirements verified")
	} else {
		ctx.Info("Skipping system requirements check (--skip-system-check specified)")
	}

	if err := ensureReleaseBundlePresent(ctx, opts.Branch); err != nil {
		return err
	}

	// Join flow: use existing config, flags, interactive prompts, or error
	if err := resolveRunnerJoin(ctx, opts.Token, opts.Coordinator, opts.Name, opts.ListenAddr, opts.Labels, opts.ConfigPath); err != nil {
		return err
	}

	// Create systemd service
	ctx.Info("Installing miren-runner systemd service...")

	serviceExists := false
	if _, err := os.Stat(runnerServicePath); err == nil {
		serviceExists = true
		if !opts.Force {
			ctx.Info("Service file already exists at %s (skipping, use --force to overwrite)", runnerServicePath)
		} else {
			ctx.Info("Service file exists, overwriting due to --force flag")
		}
	}

	if !serviceExists || opts.Force {
		var execStartParts []string
		execStartParts = append(execStartParts, releaseBinPath, "runner", "start")

		if opts.ConfigPath != "/var/lib/miren/runner/config.yaml" {
			execStartParts = append(execStartParts, fmt.Sprintf("--config=%s", opts.ConfigPath))
		}
		if opts.DataPath != "/var/lib/miren/runner" {
			execStartParts = append(execStartParts, fmt.Sprintf("--data-path=%s", opts.DataPath))
		}

		execStart := strings.Join(execStartParts, " ")

		// Propagate the current MIREN_LABS value so the spawned binary
		// also has distributed runners (and any other flags) enabled.
		labsEnv := os.Getenv("MIREN_LABS")

		serviceContent := fmt.Sprintf(`[Unit]
Description=Miren Runner Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment="NO_COLOR=1"
Environment="MIREN_LABS=%s"
ExecStart=%s
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=miren-runner
User=root
WorkingDirectory=/var/lib/miren/release
KillMode=process
TimeoutStopSec=90s

[Install]
WantedBy=multi-user.target
`, labsEnv, execStart)

		if err := os.WriteFile(runnerServicePath, []byte(serviceContent), 0644); err != nil {
			return fmt.Errorf("failed to write service file: %w", err)
		}

		ctx.Completed("Service file created at %s", runnerServicePath)

		ctx.Info("Reloading systemd daemon...")
		cmd := exec.Command("systemctl", "daemon-reload")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to reload systemd: %w\nOutput: %s", err, output)
		}
	}

	// Enable and optionally start
	if opts.NoStart {
		ctx.Info("Enabling miren-runner service...")
		cmd := exec.Command("systemctl", "enable", runnerServiceName)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to enable service: %w\nOutput: %s", err, output)
		}
		ctx.Completed("Miren runner service enabled (but not started)")
	} else {
		ctx.Info("Enabling and starting miren-runner service...")
		cmd := exec.Command("systemctl", "enable", "--now", runnerServiceName)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to enable and start service: %w\nOutput: %s", err, output)
		}
		ctx.Completed("Miren runner service enabled and started")

		statusCmd := exec.Command("systemctl", "is-active", runnerServiceName)
		if output, err := statusCmd.CombinedOutput(); err == nil && strings.TrimSpace(string(output)) == "active" {
			ctx.Completed("Service is running")
		} else {
			ctx.Warn("Service may not be running, check status with: systemctl status miren-runner")
		}
	}

	// Next steps
	fmt.Println()
	ctx.Info("Installation complete!")
	fmt.Println()
	ctx.Info("To check service status:")
	fmt.Println("  systemctl status miren-runner")
	fmt.Println()
	ctx.Info("To view logs:")
	fmt.Println("  journalctl -u miren-runner -f")

	return nil
}

// resolveRunnerJoin handles the join step of runner installation. It picks the
// right strategy based on what's available: existing config on disk, an
// enrollment token passed via flag, or interactive prompts when there's a TTY.
func resolveRunnerJoin(ctx *Context, token, coordinatorOverride, name, listenAddr string, labels []string, configPath string) error {
	// Case 1: config already exists (previous runner join)
	if runnerconfig.Exists(configPath) {
		cfg, err := runnerconfig.Load(configPath)
		if err != nil {
			return fmt.Errorf("runner config exists at %s but couldn't be read: %w", configPath, err)
		}
		ctx.Completed("Using existing runner config (runner '%s', coordinator %s)", cfg.Name, cfg.CoordinatorAddress)
		return nil
	}

	// Case 2: token provided via flag
	if token != "" {
		coordinator, secret, err := decodeToken(token)
		if err != nil {
			return err
		}
		if coordinatorOverride != "" {
			coordinator = coordinatorOverride
		}
		ctx.Info("Joining coordinator...")
		return performRunnerJoin(ctx, runnerJoinOpts{
			Coordinator: coordinator,
			Secret:      secret,
			Name:        name,
			ListenAddr:  listenAddr,
			Labels:      labels,
			ConfigPath:  configPath,
		})
	}

	// Case 3: interactive TTY, prompt for the token
	if term.IsTerminal(int(os.Stdin.Fd())) {
		ctx.Info("No runner config found. Let's join a coordinator.")
		fmt.Println()

		var err error
		token, err = ui.PromptForInput(
			ui.WithLabel("Enter enrollment token"),
			ui.WithPlaceholder("mren_..."),
		)
		if err != nil {
			return fmt.Errorf("failed to read token: %w", err)
		}

		coordinator, secret, err := decodeToken(token)
		if err != nil {
			return err
		}
		if coordinatorOverride != "" {
			coordinator = coordinatorOverride
		}

		fmt.Println()
		ctx.Info("Joining coordinator...")
		return performRunnerJoin(ctx, runnerJoinOpts{
			Coordinator: coordinator,
			Secret:      secret,
			Name:        name,
			ListenAddr:  listenAddr,
			Labels:      labels,
			ConfigPath:  configPath,
		})
	}

	// Case 4: non-interactive, missing required info
	return fmt.Errorf("no runner config found at %s and no token provided\n\n"+
		"  Run interactively, or pass --token:\n"+
		"    miren runner install --token <enrollment-token>\n\n"+
		"  Or join first, then install:\n"+
		"    miren runner join <token>\n"+
		"    miren runner install", configPath)
}

// decodeToken validates and decodes an enrollment token into a coordinator
// address and secret.
func decodeToken(token string) (coordinator, secret string, err error) {
	if !enrolltoken.IsToken(token) {
		return "", "", fmt.Errorf("invalid token format (expected mren_ prefix)")
	}
	coordinator, secret, err = enrolltoken.Decode(token)
	if err != nil {
		return "", "", fmt.Errorf("invalid token: %w", err)
	}
	return coordinator, secret, nil
}

// printRunnerPrerequisiteGuidance prints guidance when prerequisites aren't met.
func printRunnerPrerequisiteGuidance(ctx *Context, prereqs installPrerequisites) {
	fmt.Println()
	ctx.Warn("Cannot proceed with runner installation.")
	fmt.Println()

	if !prereqs.hasRoot {
		ctx.Info("Root privileges are required.")
		fmt.Println("  Run with sudo: sudo miren runner install")
		fmt.Println()
	}

	if len(prereqs.missingNetCommands) > 0 {
		printMissingNetCommandGuidance(ctx, prereqs.missingNetCommands)
	}

	if !prereqs.hasSystemd {
		ctx.Info("systemd is not available on this system.")
		fmt.Println()
		ctx.Info("To run the runner directly (for testing):")
		fmt.Println("  miren runner start")
	}
}

// RunnerUninstall removes the miren-runner systemd service.
func RunnerUninstall(ctx *Context, opts struct {
	RemoveData bool   `long:"remove-data" description:"Remove runner data directory"`
	DataPath   string `long:"data-path" description:"Path to runner data" default:"/var/lib/miren/runner"`
}) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("runner uninstall requires root privileges (use sudo)")
	}

	if _, err := os.Stat(runnerServicePath); os.IsNotExist(err) {
		return fmt.Errorf("service file not found at %s", runnerServicePath)
	}

	// Stop the service if running
	isRunningCmd := exec.Command("systemctl", "is-active", runnerServiceName)
	output, err := isRunningCmd.CombinedOutput()
	if err == nil && strings.TrimSpace(string(output)) == "active" {
		ctx.Info("Stopping miren-runner service...")
		cmd := exec.Command("systemctl", "stop", runnerServiceName)
		if output, err := cmd.CombinedOutput(); err != nil {
			ctx.Warn("Failed to stop service: %v\nOutput: %s", err, output)
		} else {
			ctx.Completed("Service stopped")
		}
	}

	// Disable the service
	ctx.Info("Disabling miren-runner service...")
	cmd := exec.Command("systemctl", "disable", runnerServiceName)
	if output, err := cmd.CombinedOutput(); err != nil {
		ctx.Warn("Failed to disable service: %v\nOutput: %s", err, output)
	} else {
		ctx.Completed("Service disabled")
	}

	// Remove the service file
	if err := os.Remove(runnerServicePath); err != nil {
		return fmt.Errorf("failed to remove service file: %w", err)
	}
	ctx.Completed("Service file removed from %s", runnerServicePath)

	// Reload systemd
	cmd = exec.Command("systemctl", "daemon-reload")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w\nOutput: %s", err, output)
	}

	ctx.Completed("Miren runner service uninstalled")

	if opts.RemoveData {
		cleanPath := filepath.Clean(opts.DataPath)
		if cleanPath == "/" || cleanPath == "." || cleanPath == "" {
			return fmt.Errorf("refusing to remove unsafe data path %q", opts.DataPath)
		}
		if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
			ctx.Info("Data directory %s does not exist, skipping removal", cleanPath)
		} else {
			ctx.Info("Removing %s...", cleanPath)
			if err := os.RemoveAll(cleanPath); err != nil {
				return fmt.Errorf("failed to remove data directory: %w", err)
			}
			ctx.Completed("Data directory removed")
		}
	} else {
		fmt.Println()
		ctx.Info("Runner data at %s has not been removed.", opts.DataPath)
		ctx.Info("To remove it: sudo miren runner uninstall --remove-data")
	}

	return nil
}

// RunnerServiceStatus shows the status of the miren-runner systemd service.
// This is separate from RunnerStatus which shows runner health via RPC.
func RunnerServiceStatus(ctx *Context, opts struct {
	Follow bool `short:"f" long:"follow" description:"Follow logs in real-time"`
}) error {
	if _, err := os.Stat(runnerServicePath); os.IsNotExist(err) {
		ctx.Warn("Service file not found at %s", runnerServicePath)
		ctx.Info("The miren-runner service is not installed. Run 'sudo miren runner install' to set it up.")
		return nil
	}

	cmd := exec.Command("systemctl", "status", runnerServiceName, "--no-pager")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		ctx.Log.Debug("systemctl status returned error", "error", err)
	}

	if opts.Follow {
		fmt.Println()
		ctx.Info("Following logs (Ctrl+C to stop)...")
		fmt.Println()

		cmd = exec.Command("journalctl", "-u", "miren-runner", "-f", "--no-pager")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		return cmd.Run()
	}

	return nil
}
