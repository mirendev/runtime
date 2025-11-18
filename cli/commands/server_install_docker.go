package commands

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/quic-go/quic-go/http3"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

// ServerInstallDocker sets up a Docker container to run the miren server
func ServerInstallDocker(ctx *Context, opts struct {
	Image        string            `short:"i" long:"image" description:"Docker image to use" default:"oci.miren.cloud/miren:latest"`
	Name         string            `short:"n" long:"name" description:"Container name" default:"miren"`
	Force        bool              `short:"f" long:"force" description:"Remove existing container if present"`
	HTTPPort     int               `long:"http-port" description:"HTTP port mapping" default:"80"`
	HostNetwork  bool              `long:"host-network" description:"Use host networking (ignores port mappings)"`
	WithoutCloud bool              `long:"without-cloud" description:"Skip cloud registration setup"`
	ClusterName  string            `long:"cluster-name" description:"Cluster name for cloud registration"`
	CloudURL     string            `short:"u" long:"url" description:"Cloud URL for registration" default:"https://miren.cloud"`
	Tags         map[string]string `short:"t" long:"tag" description:"Tags for the cluster (key:value)"`
}) error {
	// Derive volume name from container name
	volumeName := opts.Name + "-data"
	// Check if Docker is installed and running
	ctx.Info("Checking Docker availability...")
	if err := checkDockerAvailable(); err != nil {
		return fmt.Errorf("docker is not available: %w\nPlease install Docker Desktop from https://www.docker.com/products/docker-desktop", err)
	}
	ctx.Completed("Docker is available and running")

	// Check if container already exists
	containerExists, err := dockerContainerExists(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for existing container: %w", err)
	}

	if containerExists {
		if !opts.Force {
			return fmt.Errorf("container '%s' already exists (use --force to remove and recreate)", opts.Name)
		}

		ctx.Info("Removing existing container '%s'...", opts.Name)
		if err := dockerRemoveContainer(opts.Name, true); err != nil {
			return fmt.Errorf("failed to remove existing container: %w", err)
		}
		ctx.Completed("Existing container removed")
	}

	// Create volume if it doesn't exist
	if err := dockerEnsureVolume(volumeName); err != nil {
		return fmt.Errorf("failed to ensure volume exists: %w", err)
	}

	// Register with cloud unless --without-cloud is specified
	if !opts.WithoutCloud {
		if err := performDockerRegistrationPreStart(ctx, opts.Image, volumeName, dockerRegistrationOptions{
			ClusterName: opts.ClusterName,
			CloudURL:    opts.CloudURL,
			Tags:        opts.Tags,
		}); err != nil {
			ctx.Warn("Cloud registration failed: %v", err)
			ctx.Info("Continuing with installation without cloud registration")
			ctx.Info("You can register later by running: docker exec %s miren register", opts.Name)
		} else {
			ctx.Completed("Cloud registration complete")
		}
	} else {
		ctx.Info("Skipping cloud registration (--without-cloud specified)")
	}

	// Create and optionally start the container
	ctx.Info("Creating miren server container...")
	containerID, err := dockerCreateContainer(opts.Name, opts.Image, dockerContainerConfig{
		HTTPPort:    opts.HTTPPort,
		VolumeName:  volumeName,
		HostNetwork: opts.HostNetwork,
	})
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}
	ctx.Completed("Container created: %s", containerID[:12])

	// Start the container
	ctx.Info("Starting miren server container...")
	if err := dockerStartContainer(opts.Name); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	ctx.Completed("Container started")

	// Wait for server to be ready
	ctx.Info("Waiting for miren server to initialize...")
	if err := waitForServerReady(ctx); err != nil {
		ctx.Warn("Failed to confirm server is ready: %v", err)
		ctx.Info("The server may still be starting. Check logs with: docker logs %s", opts.Name)
	} else {
		ctx.Completed("Server is ready")
	}

	// Copy client configuration from container
	ctx.Info("Configuring miren client...")
	if err := copyClientConfig(ctx, opts.Name); err != nil {
		ctx.Warn("Failed to copy client configuration: %v", err)
		ctx.Info("You may need to configure the client manually")
	} else {
		ctx.Completed("Client configuration saved")
	}

	// Print helpful information
	fmt.Println()
	ctx.Info("Installation complete!")
	fmt.Println()
	ctx.Info("Container management:")
	fmt.Printf("  View status:  docker ps -f name=%s\n", opts.Name)
	fmt.Printf("  View logs:    docker logs %s\n", opts.Name)
	fmt.Printf("  Follow logs:  docker logs -f %s\n", opts.Name)
	fmt.Printf("  Stop:         docker stop %s\n", opts.Name)
	fmt.Printf("  Start:        docker start %s\n", opts.Name)
	fmt.Printf("  Remove:       docker rm -f %s\n", opts.Name)
	fmt.Println()

	return nil
}

// ServerUninstallDocker removes the miren Docker container and optionally the volume
func ServerUninstallDocker(ctx *Context, opts struct {
	Name         string `short:"n" long:"name" description:"Container name" default:"miren"`
	RemoveVolume bool   `long:"remove-volume" description:"Remove the data volume"`
	Force        bool   `short:"f" long:"force" description:"Force removal even if container is running"`
}) error {
	// Derive volume name from container name
	volumeName := opts.Name + "-data"
	// Check if Docker is available
	if err := checkDockerAvailable(); err != nil {
		return fmt.Errorf("docker is not available: %w", err)
	}

	// Check if container exists
	containerExists, err := dockerContainerExists(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for container: %w", err)
	}

	if !containerExists {
		ctx.Warn("Container '%s' does not exist", opts.Name)
		if !opts.RemoveVolume {
			return nil
		}
	} else {
		// Check if container is running
		isRunning, err := dockerContainerIsRunning(opts.Name)
		if err != nil {
			return fmt.Errorf("failed to check container status: %w", err)
		}

		// If container is running and not forcing, ask for confirmation
		if isRunning && !opts.Force {
			confirmed, err := ui.Confirm(
				ui.WithMessage(fmt.Sprintf("Container '%s' is currently running. Stop and remove it?", opts.Name)),
				ui.WithDefault(false),
			)
			if err != nil {
				return fmt.Errorf("confirmation failed: %w", err)
			}
			if !confirmed {
				ctx.Info("Uninstall cancelled")
				return nil
			}
		}

		// Stop and remove container
		ctx.Info("Removing container '%s'...", opts.Name)
		if err := dockerRemoveContainer(opts.Name, opts.Force); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
		ctx.Completed("Container removed")
	}

	// Remove volume if requested
	if opts.RemoveVolume {
		ctx.Info("Removing volume '%s'...", volumeName)
		if err := dockerRemoveVolume(volumeName); err != nil {
			ctx.Warn("Failed to remove volume: %v", err)
			ctx.Info("You can remove it manually with: docker volume rm %s", volumeName)
		} else {
			ctx.Completed("Volume removed")
		}
	} else {
		fmt.Println()
		ctx.Info("Note: The data volume '%s' has not been removed.", volumeName)
		ctx.Info("To remove it: docker volume rm %s", volumeName)
	}

	fmt.Println()
	ctx.Info("Uninstallation complete!")

	return nil
}

// ServerStatusDocker shows the status of the miren Docker container
func ServerStatusDocker(ctx *Context, opts struct {
	Name   string `short:"n" long:"name" description:"Container name" default:"miren"`
	Follow bool   `short:"f" long:"follow" description:"Follow logs in real-time"`
}) error {
	// Check if Docker is available
	if err := checkDockerAvailable(); err != nil {
		return fmt.Errorf("docker is not available: %w", err)
	}

	// Check if container exists
	containerExists, err := dockerContainerExists(opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for container: %w", err)
	}

	if !containerExists {
		ctx.Warn("Container '%s' does not exist", opts.Name)
		ctx.Info("Run 'miren server docker install' to create it")
		return nil
	}

	// Show container status using docker ps
	cmd := exec.Command("docker", "ps", "-a", "-f", fmt.Sprintf("name=%s", opts.Name), "--format", "table {{.Names}}\t{{.Status}}\t{{.Ports}}")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		ctx.Warn("Failed to get container status: %v", err)
	}

	fmt.Println()

	// Show recent logs
	if !opts.Follow {
		ctx.Info("Recent logs (use -f to follow):")
		fmt.Println()
		cmd = exec.Command("docker", "logs", "--tail", "50", opts.Name)
	} else {
		ctx.Info("Following logs (Ctrl+C to stop)...")
		fmt.Println()
		cmd = exec.Command("docker", "logs", "-f", opts.Name)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Helper functions

type dockerContainerConfig struct {
	HTTPPort    int
	VolumeName  string
	HostNetwork bool
}

func checkDockerAvailable() error {
	// Check if docker command exists
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker command not found")
	}

	// Check if Docker daemon is running
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker daemon is not running")
	}

	return nil
}

func dockerContainerExists(name string) (bool, error) {
	cmd := exec.Command("docker", "ps", "-a", "-q", "-f", fmt.Sprintf("name=^%s$", name))
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(output))) > 0, nil
}

func dockerContainerIsRunning(name string) (bool, error) {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name)
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) == "true", nil
}

func dockerCreateContainer(name, image string, config dockerContainerConfig) (string, error) {
	args := []string{
		"run", "-d",
		"--name", name,
		"--init",
		"--privileged",
		"--restart", "always",
	}

	// Use host networking if requested, otherwise map ports
	if config.HostNetwork {
		args = append(args, "--network", "host")
	} else {
		args = append(args,
			"-p", fmt.Sprintf("%d:80/tcp", config.HTTPPort),
			"-p", "8443:8443/udp",
			"-p", "443:443/tcp",
		)
	}

	// Add volume and image
	args = append(args,
		"-v", fmt.Sprintf("%s:/var/lib/miren", config.VolumeName),
		image,
	)

	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, exitErr.Stderr)
		}
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func dockerStartContainer(name string) error {
	cmd := exec.Command("docker", "start", name)
	return cmd.Run()
}

func dockerRemoveContainer(name string, force bool) error {
	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, name)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}

func dockerRemoveVolume(name string) error {
	cmd := exec.Command("docker", "volume", "rm", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}

func dockerExec(containerName string, command []string) (string, error) {
	args := append([]string{"exec", containerName}, command...)
	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%w: %s", err, exitErr.Stderr)
		}
		return "", err
	}
	return string(output), nil
}

func waitForServerReady(ctx *Context) error {
	maxRetries := 30
	retryDelay := 2 * time.Second

	ctx.Log.Debug("checking server health", "host", "localhost", "max_retries", maxRetries)

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

	healthURL := fmt.Sprintf("https://%s:8443/healthz", "localhost")
	ctx.Log.Debug("health check endpoint", "url", healthURL)

	for i := range maxRetries {
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
			// Server responded to health check, it's ready
			return nil
		}

		// Check if it's a connection error vs server responding with error
		// If we get any response (even an error response), the server is up
		if resp != nil {
			resp.Body.Close()
			ctx.Log.Info("server responded with error but is running", "attempt", i+1)
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

func copyClientConfig(ctx *Context, containerName string) error {
	ctx.Log.Debug("generating client config", "host", "localhost")

	// Generate client config by running miren auth generate
	target := "localhost:8443"
	ctx.Log.Debug("running auth generate", "target", target, "cluster", "docker")
	output, err := dockerExec(containerName, []string{"miren", "auth", "generate", "-C", "docker", "-t", target, "-c", "-"})
	if err != nil {
		ctx.Log.Error("failed to generate client config", "error", err)
		return fmt.Errorf("failed to generate client config: %w", err)
	}

	ctx.Log.Debug("auth generate output received", "output_length", len(output))

	if len(strings.TrimSpace(output)) == 0 {
		ctx.Log.Error("generated client config is empty")
		return fmt.Errorf("generated client config is empty")
	}

	// Parse the generated config to extract cluster information
	var generatedConfig clientconfig.ConfigData
	if err := yaml.Unmarshal([]byte(output), &generatedConfig); err != nil {
		ctx.Log.Error("failed to parse generated config", "error", err)
		return fmt.Errorf("failed to parse generated config: %w", err)
	}

	ctx.Log.Debug("parsed generated config", "clusters", len(generatedConfig.Clusters))

	// Verify the docker cluster exists in the generated config
	dockerCluster, ok := generatedConfig.Clusters["docker"]
	if !ok {
		ctx.Log.Error("docker cluster not found in generated config", "available_clusters", generatedConfig.Clusters)
		return fmt.Errorf("docker cluster not found in generated config")
	}

	ctx.Log.Debug("found docker cluster in config", "hostname", dockerCluster.Hostname)

	// Load existing user config
	config, err := clientconfig.LoadConfig()
	if err != nil {
		if !errors.Is(err, clientconfig.ErrNoConfig) {
			ctx.Log.Error("failed to load existing client config", "error", err)
			return fmt.Errorf("failed to load existing client config: %w", err)
		}
		ctx.Log.Warn("error loading existing client config, creating new one", "error", err)
		config = clientconfig.NewConfig()
	} else {
		ctx.Log.Debug("loaded existing client config", "active_cluster", config.ActiveCluster())
	}

	// Create leaf config data with the docker cluster
	leafConfigData := &clientconfig.ConfigData{
		Clusters: map[string]*clientconfig.ClusterConfig{
			"docker": dockerCluster,
		},
	}

	// Add as a leaf config (this will be saved to clientconfig.d/50-docker.yaml)
	ctx.Log.Debug("setting leaf config", "name", "50-docker")
	config.SetLeafConfig("50-docker", leafConfigData)

	// Set the active cluster to docker if none is set
	if config.ActiveCluster() == "" {
		ctx.Log.Debug("setting active cluster to docker")
		config.SetActiveCluster("docker")
	}

	// Save the config
	ctx.Log.Debug("saving client config")
	if err := config.Save(); err != nil {
		ctx.Log.Error("failed to save docker cluster leaf config", "error", err)
		return fmt.Errorf("failed to save docker cluster leaf config: %w", err)
	}

	ctx.Log.Info("wrote docker cluster config", "cluster", "docker", "address", dockerCluster.Hostname)
	return nil
}

type dockerRegistrationOptions struct {
	ClusterName string
	CloudURL    string
	Tags        map[string]string
}

func dockerEnsureVolume(volumeName string) error {
	// Check if volume exists
	cmd := exec.Command("docker", "volume", "inspect", volumeName)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Create volume
	cmd = exec.Command("docker", "volume", "create", volumeName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, output)
	}
	return nil
}

func performDockerRegistrationPreStart(ctx *Context, image, volumeName string, opts dockerRegistrationOptions) error {
	// Determine cluster name
	clusterName := opts.ClusterName
	if clusterName == "" {
		// Use hostname
		hostname, err := os.Hostname()
		if err != nil {
			clusterName = "miren-cluster"
		} else {
			clusterName = hostname
		}
	}

	ctx.Info("Setting up cloud registration for cluster '%s'...", clusterName)

	// Create temporary directory for registration
	tempDir, err := os.MkdirTemp("", "miren-registration-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Perform registration using the Register function
	regOpts := RegisterOptions{
		ClusterName: clusterName,
		CloudURL:    opts.CloudURL,
		Tags:        opts.Tags,
		OutputDir:   tempDir,
	}

	if err := Register(ctx, regOpts); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Copy registration files to the docker volume
	// We use a temporary container to copy files into the volume
	registrationFile := filepath.Join(tempDir, "registration.json")
	if _, err := os.Stat(registrationFile); err != nil {
		return fmt.Errorf("registration file not found: %w", err)
	}

	ctx.Info("Copying registration to docker volume...")

	// Create a temporary container to copy files
	tempContainerName := fmt.Sprintf("miren-reg-copy-%d", time.Now().Unix())

	// Run container to create the directory structure (override entrypoint)
	runArgs := []string{
		"run", "--name", tempContainerName,
		"--entrypoint", "mkdir",
		"-v", fmt.Sprintf("%s:/var/lib/miren", volumeName),
		image,
		"-p", "/var/lib/miren/server",
	}

	cmd := exec.Command("docker", runArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create directory in volume: %w: %s", err, output)
	}

	// Ensure temp container is always cleaned up
	defer func() {
		if err := dockerRemoveContainer(tempContainerName, true); err != nil {
			ctx.Warn("Failed to remove temp container %s: %v", tempContainerName, err)
		}
	}()

	// Copy registration file to container
	cpArgs := []string{
		"cp",
		registrationFile,
		fmt.Sprintf("%s:/var/lib/miren/server/registration.json", tempContainerName),
	}

	cmd = exec.Command("docker", cpArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy registration file: %w: %s", err, output)
	}

	ctx.Completed("Registration copied to volume")

	return nil
}
