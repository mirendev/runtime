// Package buildkit provides a component for managing a persistent BuildKit daemon using containerd.
// BuildKit is used for building container images with layer caching across builds.
package buildkit

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	buildkitclient "github.com/moby/buildkit/client"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/slogout"
)

const (
	buildkitContainerName = "miren-buildkit"
	defaultGCKeepStorage  = 10 * 1024 * 1024 * 1024 // 10GB
	defaultGCKeepDuration = 7 * 24 * 60 * 60        // 7 days in seconds
)

var buildkitImage = imagerefs.BuildKit

// Config contains configuration for the BuildKit component.
type Config struct {
	// SocketDir is the directory where the Unix socket will be created (e.g., /run/miren/buildkit)
	SocketDir string

	// GCKeepStorage is the maximum bytes of cache to keep (default: 10GB)
	GCKeepStorage int64

	// GCKeepDuration is how long to keep cache entries in seconds (default: 7 days)
	GCKeepDuration int64

	// RegistryHost is the hostname for the cluster-local registry (e.g., cluster.local:5000)
	RegistryHost string
}

// Component manages a persistent BuildKit daemon as a containerd container,
// or connects to an external BuildKit daemon via Unix socket.
type Component struct {
	Log       *slog.Logger
	CC        *containerd.Client
	Namespace string
	DataPath  string

	mu         sync.Mutex
	container  containerd.Container
	running    bool
	socketPath string
	socketDir  string
	external   bool // true if connecting to external daemon (no container management)
}

// NewComponent creates a new BuildKit component that manages an embedded daemon.
func NewComponent(log *slog.Logger, cc *containerd.Client, namespace, dataPath string) *Component {
	return &Component{
		Log:       log,
		CC:        cc,
		Namespace: namespace,
		DataPath:  dataPath,
		external:  false,
	}
}

// NewExternalComponent creates a BuildKit component that connects to an external daemon.
// No container lifecycle management is performed - it only provides client access.
func NewExternalComponent(log *slog.Logger, socketPath string) *Component {
	return &Component{
		Log:        log,
		socketPath: socketPath,
		running:    true, // External daemon is assumed to be running
		external:   true,
	}
}

// Start starts the BuildKit daemon container.
// For external components, this verifies the socket is accessible.
func (c *Component) Start(ctx context.Context, config Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("buildkit component already running")
	}

	// External mode: just verify the socket is accessible
	if c.external {
		c.Log.Info("using external buildkit daemon", "socket", c.socketPath)
		if err := c.waitForReady(ctx); err != nil {
			return fmt.Errorf("external buildkit daemon not accessible at %s: %w", c.socketPath, err)
		}
		c.running = true
		return nil
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	c.Log.Info("pulling buildkit image", "image", buildkitImage)
	image, err := c.CC.Pull(ctx, buildkitImage, containerd.WithPullUnpack)
	if err != nil {
		return fmt.Errorf("failed to pull buildkit image: %w", err)
	}

	// Set up paths
	dataPath := filepath.Join(c.DataPath, "buildkit")
	socketDir := config.SocketDir
	if socketDir == "" {
		socketDir = "/run/miren/buildkit"
	}
	c.socketDir = socketDir
	c.socketPath = filepath.Join(socketDir, "buildkitd.sock")

	// Create directories
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Set defaults for GC config
	gcKeepStorage := config.GCKeepStorage
	if gcKeepStorage == 0 {
		gcKeepStorage = defaultGCKeepStorage
	}
	gcKeepDuration := config.GCKeepDuration
	if gcKeepDuration == 0 {
		gcKeepDuration = defaultGCKeepDuration
	}

	// Generate buildkitd.toml config
	configContent := c.generateConfig(gcKeepStorage, gcKeepDuration, config.RegistryHost)
	configPath := filepath.Join(dataPath, "buildkitd.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to write buildkit config: %w", err)
	}

	// Check if container already exists
	existingContainer, err := c.CC.LoadContainer(ctx, buildkitContainerName)
	if err == nil {
		c.Log.Info("found existing buildkit container, restarting it", "container_id", existingContainer.ID())
		return c.restartExistingContainer(ctx, existingContainer, dataPath)
	}

	c.Log.Info("starting buildkit daemon", "data_path", dataPath, "socket_path", c.socketPath)

	// Create container
	container, err := c.createContainer(ctx, image, dataPath, configPath)
	if err != nil {
		return fmt.Errorf("failed to create buildkit container: %w", err)
	}

	c.container = container

	// Start container with structured logging
	task, err := container.NewTask(ctx, slogout.WithLogger(c.Log, "buildkit"))
	if err != nil {
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to create buildkit task: %w", err)
	}

	if err := task.Start(ctx); err != nil {
		task.Delete(ctx)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to start buildkit task: %w", err)
	}

	// Wait for BuildKit to be ready (socket exists and is connectable)
	if err := c.waitForReady(ctx); err != nil {
		task.Delete(ctx)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return err
	}

	c.running = true
	c.Log.Info("buildkit daemon started", "container_id", container.ID(), "socket_path", c.socketPath)

	return nil
}

// Stop stops the BuildKit daemon container.
// For external components, this is a no-op.
func (c *Component) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	// External mode: nothing to stop
	if c.external {
		c.running = false
		c.Log.Info("disconnected from external buildkit daemon")
		return nil
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	if c.container != nil {
		task, err := c.container.Task(ctx, nil)
		if err == nil {
			c.stopTask(ctx, task)
		} else {
			c.Log.Warn("failed to get buildkit task for shutdown", "error", err)
		}

		c.deleteContainerWithRetry(ctx)

		c.container = nil
	}

	c.running = false
	c.Log.Info("buildkit daemon stopped")

	return nil
}

// SocketPath returns the path to the BuildKit Unix socket.
func (c *Component) SocketPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.socketPath
}

// IsRunning returns whether the BuildKit daemon is running.
func (c *Component) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// Client returns a new BuildKit client connected to the daemon.
func (c *Component) Client(ctx context.Context) (*buildkitclient.Client, error) {
	c.mu.Lock()
	socketPath := c.socketPath
	running := c.running
	c.mu.Unlock()

	if !running {
		return nil, fmt.Errorf("buildkit component not running")
	}

	return buildkitclient.New(ctx, "unix://"+socketPath)
}

func (c *Component) generateConfig(gcKeepStorage, gcKeepDuration int64, registryHost string) string {
	if registryHost == "" {
		registryHost = "cluster.local:5000"
	}

	return fmt.Sprintf(`# BuildKit daemon configuration
debug = true
root = "/var/lib/buildkit"
insecure-entitlements = [ "network.host", "security.insecure" ]

[log]
  format = "text"

[dns]
  nameservers=["1.1.1.1","8.8.8.8"]
  options=["edns0"]

[grpc]
  address = [ "unix:///run/buildkit/buildkitd.sock" ]
  uid = 0
  gid = 0

[history]
  maxAge = 172800
  maxEntries = 50

[worker.oci]
  gc = true
  gckeepstorage = %d

  [[worker.oci.gcpolicy]]
    keepBytes = %d
    keepDuration = %d
    filters = ["type==source.local", "type==exec.cachemount"]

[registry."docker.io"]
  http = true

[registry."%s"]
  insecure = true
  http = true
`, gcKeepStorage, gcKeepStorage, gcKeepDuration, registryHost)
}

func (c *Component) createContainer(ctx context.Context, image containerd.Image, dataPath, configPath string) (containerd.Container, error) {
	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		// No host networking - BuildKit uses its own network namespace
		// This is more secure than host networking since BuildKit doesn't need
		// to accept external TCP connections (we use Unix sockets)
		oci.WithPrivileged, // Required for BuildKit
		oci.WithProcessArgs(
			"/usr/bin/buildkitd",
			"--config=/etc/buildkit/buildkitd.toml",
		),
		oci.WithHostHostsFile,
		oci.WithHostResolvconf,
		oci.WithMounts([]specs.Mount{
			{
				Destination: "/var/lib/buildkit",
				Type:        "bind",
				Source:      dataPath,
				Options:     []string{"rbind", "rw"},
			},
			{
				Destination: "/run/buildkit",
				Type:        "bind",
				Source:      c.socketDir,
				Options:     []string{"rbind", "rw"},
			},
			{
				Destination: "/etc/buildkit/buildkitd.toml",
				Type:        "bind",
				Source:      configPath,
				Options:     []string{"rbind", "ro"},
			},
		}),
	}

	container, err := c.CC.NewContainer(
		ctx,
		buildkitContainerName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(buildkitContainerName+"-snapshot", image),
		containerd.WithNewSpec(opts...),
	)
	if err != nil {
		return nil, err
	}

	return container, nil
}

func (c *Component) restartExistingContainer(ctx context.Context, container containerd.Container, dataPath string) error {
	c.container = container

	task, err := container.Task(ctx, nil)
	if err == nil {
		status, err := task.Status(ctx)
		if err != nil {
			c.Log.Warn("failed to get task status", "error", err)
		} else if status.Status == containerd.Running {
			c.Log.Info("buildkit container is already running")
			c.running = true
			return c.waitForReady(ctx)
		}

		c.Log.Info("starting existing buildkit task")
		if err := task.Start(ctx); err == nil {
			c.running = true
			c.Log.Info("buildkit daemon restarted", "container_id", container.ID())
			return c.waitForReady(ctx)
		}

		c.Log.Warn("failed to start existing task, deleting it", "error", err)
		task.Delete(ctx)
	}

	c.Log.Info("creating new task for existing container")
	task, err = container.NewTask(ctx, slogout.WithLogger(c.Log, "buildkit"))
	if err != nil {
		return fmt.Errorf("failed to create new task for existing container: %w", err)
	}

	if err := task.Start(ctx); err != nil {
		task.Delete(ctx)
		return fmt.Errorf("failed to start new task for existing container: %w", err)
	}

	if err := c.waitForReady(ctx); err != nil {
		task.Delete(ctx)
		return err
	}

	c.running = true
	c.Log.Info("buildkit daemon restarted with new task", "container_id", container.ID())

	return nil
}

func (c *Component) waitForReady(ctx context.Context) error {
	// Wait for the Unix socket to be created and connectable
	for i := 0; i < 30; i++ {
		// Check if socket file exists
		if _, err := os.Stat(c.socketPath); err == nil {
			// Try to connect to it
			conn, err := net.DialTimeout("unix", c.socketPath, 2*time.Second)
			if err == nil {
				conn.Close()
				c.Log.Info("buildkit daemon is ready", "socket_path", c.socketPath)
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			continue
		}
	}

	c.Log.Warn("buildkit daemon readiness check timed out", "socket_path", c.socketPath)
	return nil
}

func (c *Component) stopTask(ctx context.Context, task containerd.Task) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := task.Kill(shutdownCtx, unix.SIGTERM); err != nil {
		c.Log.Error("failed to send SIGTERM to buildkit task", "error", err)
		return
	}

	status, err := task.Wait(shutdownCtx)
	if err == nil {
		select {
		case es := <-status:
			c.Log.Info("buildkit task exited", "code", es.ExitCode())

		case <-shutdownCtx.Done():
			c.Log.Warn("buildkit task did not exit gracefully, sending SIGKILL")
			killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer killCancel()

			if err := task.Kill(killCtx, unix.SIGKILL); err != nil {
				c.Log.Error("failed to send SIGKILL to buildkit task", "error", err)
			} else {
				if _, waitErr := task.Wait(killCtx); waitErr != nil {
					c.Log.Error("buildkit task wait after SIGKILL failed", "error", waitErr)
				}
			}
		}
	}

	deleteCtx, deleteCancel := context.WithTimeout(ctx, 10*time.Second)
	defer deleteCancel()

	if _, err := task.Delete(deleteCtx); err != nil {
		c.Log.Error("failed to delete buildkit task", "error", err)
	}
}

func (c *Component) deleteContainerWithRetry(ctx context.Context) {
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := c.container.Delete(deleteCtx, containerd.WithSnapshotCleanup)
		cancel()

		if err == nil {
			c.Log.Info("buildkit container deleted successfully")
			return
		}

		c.Log.Error("failed to delete buildkit container", "error", err, "attempt", attempt, "max_retries", maxRetries)

		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	c.Log.Error("failed to delete buildkit container after all retries, potential snapshot leak")
}
