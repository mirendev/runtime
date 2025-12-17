package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/ui"
)

// isLocalCluster checks if a cluster hostname refers to the local machine
func isLocalCluster(hostname string) bool {
	host := hostname
	if h, _, err := net.SplitHostPort(hostname); err == nil {
		host = h
	}
	return isLocalAddress(host)
}

type clusterInfo struct {
	name    string
	cluster *clientconfig.ClusterConfig
	source  string
}

// DoctorConfig shows configuration file information
func DoctorConfig(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil && !errors.Is(err, clientconfig.ErrNoConfig) {
		return err
	}

	ctx.Printf("%s\n", infoBold.Render("Configuration"))
	ctx.Printf("%s\n", infoGray.Render("============="))

	if cfg == nil || errors.Is(err, clientconfig.ErrNoConfig) {
		ctx.Printf("\n%s\n", infoGray.Render("No configuration found"))
		ctx.Printf("\nGet started:\n")
		ctx.Printf("  %s        %s\n", infoBold.Render("miren login"), infoGray.Render("# Authenticate with miren.cloud"))
		ctx.Printf("  %s  %s\n", infoBold.Render("miren cluster add"), infoGray.Render("# Add a cluster manually"))
		return nil
	}

	activeCluster := cfg.ActiveCluster()

	// Separate clusters into local and remote
	var localClusters, remoteClusters []clusterInfo
	cfg.IterateClusters(func(name string, cluster *clientconfig.ClusterConfig) error {
		info := clusterInfo{
			name:    name,
			cluster: cluster,
			source:  cfg.GetClusterSource(name),
		}
		if isLocalCluster(cluster.Hostname) {
			localClusters = append(localClusters, info)
		} else {
			remoteClusters = append(remoteClusters, info)
		}
		return nil
	})

	// Shorten paths for display
	homeDir, _ := os.UserHomeDir()
	shortenPath := func(path string) string {
		if homeDir != "" && strings.HasPrefix(path, homeDir) {
			return "~" + path[len(homeDir):]
		}
		return path
	}

	// Display local clusters table
	if len(localClusters) > 0 {
		ctx.Printf("\n%s\n", infoLabel.Render("Local Clusters:"))
		headers := []string{"CLUSTER", "ADDRESS", "SOURCE FILE"}
		var rows []ui.Row
		for _, c := range localClusters {
			name := c.name
			if c.name == activeCluster {
				name = infoGreen.Render(c.name + "*")
			}
			rows = append(rows, ui.Row{name, c.cluster.Hostname, shortenPath(c.source)})
		}
		columns := ui.AutoSizeColumns(headers, rows, nil)
		table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
		ctx.Printf("%s\n", table.Render())
	}

	// Display remote clusters table
	if len(remoteClusters) > 0 {
		ctx.Printf("\n%s\n", infoLabel.Render("Remote Clusters:"))
		headers := []string{"CLUSTER", "ADDRESS", "SOURCE FILE"}
		var rows []ui.Row
		for _, c := range remoteClusters {
			name := c.name
			if c.name == activeCluster {
				name = infoGreen.Render(c.name + "*")
			}
			rows = append(rows, ui.Row{name, c.cluster.Hostname, shortenPath(c.source)})
		}
		columns := ui.AutoSizeColumns(headers, rows, nil)
		table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
		ctx.Printf("%s\n", table.Render())
	}

	if len(localClusters) > 0 || len(remoteClusters) > 0 {
		ctx.Printf("%s\n", infoGray.Render("* = active cluster"))
	}

	// Interactive mode
	if !ui.IsInteractive() {
		return nil
	}

	ctx.Printf("\n")

	// Main action picker
	for {
		items := []ui.PickerItem{
			ui.SimplePickerItem{Text: "View a cluster config"},
			ui.SimplePickerItem{Text: "Add a cluster"},
			ui.SimplePickerItem{Text: "[done]"},
		}

		selected, err := ui.RunPicker(items, ui.WithTitle("What would you like to do?"))
		if err != nil || selected == nil {
			return nil
		}

		switch selected.ID() {
		case "View a cluster config":
			if err := viewClusterConfig(ctx, cfg, activeCluster); err != nil {
				return err
			}

		case "Add a cluster":
			if err := addClusterFlow(ctx); err != nil {
				return err
			}

		case "[done]":
			return nil
		}
	}
}

func viewClusterConfig(ctx *Context, cfg *clientconfig.Config, activeCluster string) error {
	// Build cluster picker
	var items []ui.PickerItem
	cfg.IterateClusters(func(name string, _ *clientconfig.ClusterConfig) error {
		display := name
		if name == activeCluster {
			display = name + "*"
		}
		items = append(items, ui.SimplePickerItem{Text: display})
		return nil
	})
	items = append(items, ui.SimplePickerItem{Text: "[back]"})

	selected, err := ui.RunPicker(items, ui.WithTitle("Select a cluster:"))
	if err != nil || selected == nil || selected.ID() == "[back]" {
		return nil
	}

	// Get cluster name (remove * suffix if present)
	clusterName := strings.TrimSuffix(selected.ID(), "*")

	cluster, err := cfg.GetCluster(clusterName)
	if err != nil {
		return err
	}

	sourcePath := cfg.GetClusterSource(clusterName)

	ctx.Printf("\n%s %s\n", infoLabel.Render("Cluster:"), clusterName)
	ctx.Printf("%s %s\n", infoLabel.Render("Source:"), sourcePath)
	ctx.Printf("\n")

	// Display cluster config
	ctx.Printf("%s %s\n", infoGray.Render("hostname:"), cluster.Hostname)
	if cluster.Identity != "" {
		ctx.Printf("%s %s\n", infoGray.Render("identity:"), cluster.Identity)
	}
	if cluster.CACert != "" {
		ctx.Printf("%s %s\n", infoGray.Render("ca_cert:"), infoGray.Render("(configured)"))
	}
	ctx.Printf("\n")

	return nil
}

func addClusterFlow(ctx *Context) error {
	items := []ui.PickerItem{
		ui.SimplePickerItem{Text: "From miren.cloud"},
		ui.SimplePickerItem{Text: "Manual"},
		ui.SimplePickerItem{Text: "[back]"},
	}

	selected, err := ui.RunPicker(items, ui.WithTitle("Add a cluster:"))
	if err != nil || selected == nil || selected.ID() == "[back]" {
		return nil
	}

	switch selected.ID() {
	case "From miren.cloud":
		return addFromCloudFlow(ctx)

	case "Manual":
		return addManualFlow(ctx)
	}

	return nil
}

func addFromCloudFlow(ctx *Context) error {
	items := []ui.PickerItem{
		ui.SimplePickerItem{Text: "Select an existing cluster"},
		ui.SimplePickerItem{Text: "Register a new cluster"},
		ui.SimplePickerItem{Text: "[back]"},
	}

	selected, err := ui.RunPicker(items, ui.WithTitle("From miren.cloud:"))
	if err != nil || selected == nil || selected.ID() == "[back]" {
		return nil
	}

	switch selected.ID() {
	case "Select an existing cluster":
		ctx.Printf("\n")
		// Check if already logged in
		mainConfig, _ := clientconfig.LoadConfig()
		if mainConfig == nil || !mainConfig.HasIdentities() {
			// Need to login first
			ctx.Printf("%s\n\n", infoGray.Render("You need to login first..."))
			if err := LoginWithDefaults(ctx); err != nil {
				return err
			}
			ctx.Printf("\n")
		}
		// Now add a cluster from the cloud
		err := addCluster(ctx, "", "", "", false)
		if err != nil && strings.Contains(err.Error(), "already exists") {
			ctx.Printf("\n")
			confirmed, confirmErr := ui.Confirm(ui.WithMessage("Overwrite existing cluster config?"), ui.WithDefault(false))
			if confirmErr != nil || !confirmed {
				return nil
			}
			return addCluster(ctx, "", "", "", true)
		}
		return err

	case "Register a new cluster":
		return registerNewClusterFlow(ctx)
	}

	return nil
}

func addManualFlow(ctx *Context) error {
	addr, err := ui.PromptForInput(ui.WithLabel("Server address"))
	if err != nil || addr == "" {
		return nil
	}

	ctx.Printf("\n")
	return addCluster(ctx, "", "", addr, false)
}

func registerNewClusterFlow(ctx *Context) error {
	registrationPath := "/var/lib/miren/server/registration.json"

	// Check if already registered - try reading directly first
	existing, readErr := registration.LoadRegistration("/var/lib/miren/server")

	// If we can't read it, check if the file exists (might be permission denied)
	if readErr != nil && (os.IsPermission(readErr) || strings.Contains(readErr.Error(), "permission denied")) {
		// File exists but we can't read it - need sudo to check
		if _, statErr := os.Stat(registrationPath); statErr == nil {
			ctx.Printf("\n%s\n", infoGray.Render("Found registration file but need elevated permissions to read it."))
			ctx.Printf("Check registration with sudo? [Y/n] ")
			var response string
			fmt.Scanln(&response)
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "" && response != "y" && response != "yes" {
				return nil
			}
			// Read with sudo by calling cat
			cmd := exec.Command("sudo", "cat", registrationPath)
			output, err := cmd.Output()
			if err == nil {
				var reg registration.StoredRegistration
				if jsonErr := json.Unmarshal(output, &reg); jsonErr == nil {
					existing = &reg
				}
			}
		}
	}

	if existing != nil && existing.Status == "approved" {
		ctx.Printf("\n%s '%s' %s\n", infoLabel.Render("Already registered as"), existing.ClusterName, infoGray.Render("(ID: "+existing.ClusterID+")"))
		ctx.Printf("\n")

		// Offer to delete and re-register
		ctx.Printf("Delete existing registration and register a new cluster? [y/N] ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			return nil
		}

		// Delete the registration file (always need sudo for /var/lib/miren)
		ctx.Printf("\n%s\n", infoGray.Render("Deleting existing registration..."))
		cmd := exec.Command("sudo", "rm", registrationPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to delete registration: %w", err)
		}
		ctx.Printf("%s\n\n", infoGreen.Render("✓ Registration deleted"))
	}

	// Get cluster name
	clusterName, err := ui.PromptForInput(ui.WithLabel("Cluster name"))
	if err != nil || clusterName == "" {
		return nil
	}
	ctx.Printf("\n")

	// Try to register
	err = Register(ctx, RegisterOptions{
		ClusterName: clusterName,
		CloudURL:    "https://miren.cloud",
		OutputDir:   "/var/lib/miren/server",
	})

	// Handle permission denied - retry with sudo
	if err != nil && strings.Contains(err.Error(), "permission denied") {
		ctx.Printf("\n%s\n", infoGray.Render("Registration requires elevated permissions."))
		ctx.Printf("Retry with sudo? [Y/n] ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "" && response != "y" && response != "yes" {
			return nil
		}
		ctx.Printf("\n")

		// Get current executable path and resolve symlinks
		exe, exeErr := os.Executable()
		if exeErr != nil {
			return fmt.Errorf("failed to get executable path: %w", exeErr)
		}
		exe, _ = filepath.EvalSymlinks(exe)

		ctx.Printf("Running: sudo %s server register -n %s\n\n", exe, clusterName)
		cmd := exec.Command("sudo", exe, "server", "register", "-n", clusterName)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Registration successful - offer to restart server
	ctx.Printf("\n")
	ctx.Printf("%s\n", infoGray.Render("The local server needs to be restarted to use the new registration."))
	ctx.Printf("Restart the miren server now? [Y/n] ")
	var response string
	fmt.Scanln(&response)
	response = strings.TrimSpace(strings.ToLower(response))
	if response == "" || response == "y" || response == "yes" {
		ctx.Printf("\n%s", infoGray.Render("Restarting miren server..."))

		// Try systemctl first
		cmd := exec.Command("sudo", "systemctl", "restart", "miren")
		if err := cmd.Run(); err != nil {
			// Systemd not available or failed, try killing and restarting manually
			ctx.Printf("\n%s", infoGray.Render("systemctl not available, restarting manually..."))
			exec.Command("sudo", "pkill", "-9", "-f", "miren server").Run()
			time.Sleep(2 * time.Second)
			cmd = exec.Command("sudo", "/var/lib/miren/release/miren", "server", "-vv", "--address=0.0.0.0:8443", "--serve-tls")
			cmd.Start() // Start in background
		}

		// Wait for server to start up and report status
		ctx.Printf(" waiting for server to initialize")
		for i := 0; i < 10; i++ {
			time.Sleep(1 * time.Second)
			fmt.Print(".")
		}
		ctx.Printf("\n%s\n", infoGreen.Render("✓ Server restarted"))
	}

	// Offer to add the cluster to config
	ctx.Printf("\n")
	ctx.Printf("Add the new cluster to your config now? [Y/n] ")
	fmt.Scanln(&response)
	response = strings.TrimSpace(strings.ToLower(response))
	if response == "" || response == "y" || response == "yes" {
		ctx.Printf("\n")
		return addCluster(ctx, "", "", "", false)
	}

	return nil
}
