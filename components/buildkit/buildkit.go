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
	"go.opentelemetry.io/otel"

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

	// RegistryIP is the IP address for cluster.local registry (optional, can be set later via SetRegistryIP)
	RegistryIP string

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

	mu            sync.Mutex
	container     containerd.Container
	running       bool
	socketPath    string
	socketDir     string
	hostsPath     string             // path to custom /etc/hosts file for the container
	external      bool               // true if connecting to external daemon (no container management)
	monitorCancel context.CancelFunc // cancels the task exit-monitor goroutine on intentional stop
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

	// Generate custom /etc/hosts file for the container
	hostsPath := filepath.Join(dataPath, "hosts")
	c.hostsPath = hostsPath
	if err := c.writeHostsFile(config.RegistryIP); err != nil {
		return fmt.Errorf("failed to write hosts file: %w", err)
	}
	if config.RegistryIP != "" {
		c.Log.Info("updated buildkit hosts file with registry IP", "ip", config.RegistryIP)
	}

	// Check if container already exists
	existingContainer, err := c.CC.LoadContainer(ctx, buildkitContainerName)
	if err == nil {
		c.Log.Info("found existing buildkit container, restarting it", "container_id", existingContainer.ID())
		return c.restartExistingContainer(ctx, existingContainer, dataPath)
	}

	c.Log.Info("starting buildkit daemon", "data_path", dataPath, "socket_path", c.socketPath)

	// Create container
	container, err := c.createContainer(ctx, image, dataPath, configPath, hostsPath)
	if err != nil {
		return fmt.Errorf("failed to create buildkit container: %w", err)
	}

	c.container = container

	// Create the task with structured logging.
	task, err := container.NewTask(ctx, slogout.WithLogger(c.Log, "buildkit"))
	if err != nil {
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		c.container = nil
		return fmt.Errorf("failed to create buildkit task: %w", err)
	}

	if err := c.startTaskAndMonitor(ctx, task); err != nil {
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		c.container = nil
		return err
	}

	c.Log.Info("buildkit daemon started", "container_id", container.ID(), "socket_path", c.socketPath)

	return nil
}

// Stop stops the BuildKit daemon container.
// For external components, this is a no-op.
func (c *Component) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// External mode: no container to manage, just flip the flag idempotently.
	if c.external {
		if c.running {
			c.running = false
			c.Log.Info("disconnected from external buildkit daemon")
		}
		return nil
	}

	// Nothing to tear down once the container has been released. Note we can't
	// gate solely on c.running: after an unexpected daemon death the exit
	// monitor clears c.running while c.container is still set, and we must still
	// delete that container and its dead task here.
	if !c.running && c.container == nil {
		return nil
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	// Cancel the exit monitor before tearing down the task so its exit is
	// treated as an intentional shutdown rather than an unexpected death.
	if c.monitorCancel != nil {
		c.monitorCancel()
		c.monitorCancel = nil
	}

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

	return buildkitclient.New(ctx, "unix://"+socketPath,
		buildkitclient.WithTracerProvider(otel.GetTracerProvider()),
	)
}

// generateConfig renders the buildkitd.toml the embedded daemon runs with.
//
// The GC policy is the part worth reading. BuildKit prunes the build cache
// after every build, running each worker.oci.gcpolicy tier in order and
// trimming down to that tier's limit. We tune the tiers for how miren builds:
// stackbuild turns an uploaded app into an image using persistent package
// caches, and a small set of base images gets rebuilt constantly.
//
// The rule that matters: a byte cap only holds if some tier enforces it with no
// keepDuration. A keepDuration gate keeps any record used within the window,
// and it keeps that record's whole ancestry along with it, since we can't drop
// a base layer while a fresher layer still sits on top. On a busy builder that
// covers almost the entire cache, so an age-gated cap never binds and the cache
// grows forever. That's how garden hit 27GB against a 10GB cap and fed the
// MIR-1279 disk outage. So the last two tiers set no keepDuration.
//
// The tiers, in the order BuildKit runs them:
//
//  1. Cheap scratch, cleared first. Build inputs and side caches, not built
//     layers: the uploaded app source (source.local, from stackbuild's
//     llb.Local context), the persistent package caches (exec.cachemount for
//     bun/go/pip/bundler, via CacheMountFrom), and git checkouts (rare; we
//     build from tarballs). Losing these costs a slower next build and nothing
//     else, so we clear them before touching real layers once they pass 512MB
//     and 48h. Each filter is its own array element so BuildKit ORs them; a
//     single comma-joined string gets ANDed, and since a record has one type,
//     matches nothing.
//  2. Keep recently-used cache when we can, but stay under the cap.
//  3. The real ceiling: enforce the cap regardless of age, dropping the
//     least-recently-used first. This is what breaks the pinning above, since
//     removing a fresh leaf frees the ancestors under it.
//  4. Last resort: sweep internal and frontend cache too, to hold the line.
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

[grpc]
  address = [ "unix:///run/buildkit/buildkitd.sock" ]
  uid = 0
  gid = 0

[history]
  maxAge = 172800
  maxEntries = 50

[worker.oci]
  gc = true
  gckeepstorage = %[1]d

  # GC policy tuned for miren's build workload; see generateConfig in
  # components/buildkit/buildkit.go for the rationale.
  [[worker.oci.gcpolicy]]
    filters = [ "type==source.local", "type==exec.cachemount", "type==source.git.checkout" ]
    keepDuration = 172800
    maxUsedSpace = 512000000

  [[worker.oci.gcpolicy]]
    keepDuration = %[2]d
    maxUsedSpace = %[1]d

  [[worker.oci.gcpolicy]]
    maxUsedSpace = %[1]d

  [[worker.oci.gcpolicy]]
    all = true
    maxUsedSpace = %[1]d

[registry."docker.io"]
  http = true

[registry."%[3]s"]
  insecure = true
  http = true
`, gcKeepStorage, gcKeepDuration, registryHost)
}

// writeHostsFile creates or updates the custom /etc/hosts file for the BuildKit container.
// This includes standard localhost entries plus an optional cluster.local entry.
func (c *Component) writeHostsFile(registryIP string) error {
	content := `# BuildKit hosts file
127.0.0.1	localhost
::1	localhost ip6-localhost ip6-loopback
`
	if registryIP != "" {
		content += fmt.Sprintf("%s\tcluster.local\n", registryIP)
		c.Log.Debug("added cluster.local to hosts file", "ip", registryIP)
	}

	return os.WriteFile(c.hostsPath, []byte(content), 0644)
}

// SetRegistryIP updates the hosts file with the registry IP address for cluster.local.
// This can be called after Start() once the registry IP is known.
func (c *Component) SetRegistryIP(ip string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hostsPath == "" {
		return nil // External daemon or not yet started
	}

	if err := c.writeHostsFile(ip); err != nil {
		return fmt.Errorf("failed to update hosts file: %w", err)
	}

	c.Log.Info("updated buildkit hosts file with registry IP", "ip", ip)
	return nil
}

func (c *Component) createContainer(ctx context.Context, image containerd.Image, dataPath, configPath, hostsPath string) (containerd.Container, error) {
	// Collect OTEL env vars to forward to buildkitd so the daemon can export its internal spans.
	// The daemon shares host network namespace so the collector is reachable.
	var otelEnv []string
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
		"OTEL_SERVICE_NAME",
	} {
		if v := os.Getenv(key); v != "" {
			otelEnv = append(otelEnv, key+"="+v)
		}
	}

	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithHostNamespace(specs.NetworkNamespace), // Required for DNS resolution
		oci.WithHostNamespace(specs.CgroupNamespace),  // Required for runc to access cgroups
		oci.WithPrivileged,                            // Required for BuildKit
		oci.WithProcessArgs(
			"/usr/bin/buildkitd",
			"--config=/etc/buildkit/buildkitd.toml",
		),
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
			{
				Destination: "/etc/hosts",
				Type:        "bind",
				Source:      hostsPath,
				Options:     []string{"rbind", "ro"},
			},
			{
				// Mount cgroups for runc to work properly
				Destination: "/sys/fs/cgroup",
				Type:        "bind",
				Source:      "/sys/fs/cgroup",
				Options:     []string{"rbind", "rw"},
			},
		}),
	}

	if len(otelEnv) > 0 {
		opts = append(opts, oci.WithEnv(otelEnv))
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

	// A pre-existing task belongs to a previous miren process. buildkitd's
	// in-process HTTP/2 session server is bound to the miren process that
	// launched it, so after a miren restart it can never reconnect — reusing
	// the task silently expunges all jobs ("NotFound: no such job") and every
	// build fails. Always evict any existing task and start a fresh one bound
	// to this process.
	if task, err := container.Task(ctx, nil); err == nil {
		c.Log.Info("evicting stale buildkit task before restart")
		c.stopTask(ctx, task)
	}

	c.Log.Info("creating new task for existing buildkit container")
	task, err := container.NewTask(ctx, slogout.WithLogger(c.Log, "buildkit"))
	if err != nil {
		c.container = nil
		return fmt.Errorf("failed to create new task for existing container: %w", err)
	}

	if err := c.startTaskAndMonitor(ctx, task); err != nil {
		c.container = nil
		return err
	}

	c.Log.Info("buildkit daemon restarted with new task", "container_id", container.ID())

	return nil
}

// startTaskAndMonitor starts an already-created task, waits for buildkit to
// become ready, records the running state, and spawns a goroutine that watches
// for the task exiting unexpectedly. The exit channel is established via
// task.Wait before task.Start so the exit event can never be missed. On any
// failure the task is torn down before returning, so the caller only needs to
// clean up container-level resources. Must be called with c.mu held.
func (c *Component) startTaskAndMonitor(ctx context.Context, task containerd.Task) error {
	exitCh, err := task.Wait(ctx)
	if err != nil {
		task.Delete(ctx)
		return fmt.Errorf("failed to wait on buildkit task: %w", err)
	}

	if err := task.Start(ctx); err != nil {
		task.Delete(ctx)
		return fmt.Errorf("failed to start buildkit task: %w", err)
	}

	if err := c.waitForReady(ctx); err != nil {
		c.stopTask(ctx, task)
		return err
	}

	monitorCtx, cancel := context.WithCancel(context.Background())
	c.monitorCancel = cancel
	c.running = true
	go c.monitorTask(monitorCtx, exitCh)

	return nil
}

// monitorTask watches a running buildkit task for an unexpected exit. An
// intentional shutdown (Stop or a restart force-stop) cancels monitorCtx first,
// so this returns quietly. If the task exits on its own, it clears c.running so
// IsRunning/Client reflect reality and the next build fails fast rather than
// hanging on a dead daemon.
func (c *Component) monitorTask(monitorCtx context.Context, exitCh <-chan containerd.ExitStatus) {
	select {
	case <-monitorCtx.Done():
		return
	case es := <-exitCh:
		c.mu.Lock()
		defer c.mu.Unlock()

		// A stop may have raced in between the exit firing and acquiring the
		// lock; in that case the stop path owns the state transition.
		if monitorCtx.Err() != nil || !c.running {
			return
		}

		c.running = false
		if err := es.Error(); err != nil {
			c.Log.Error("buildkit task exit wait failed, marking daemon not running", "error", err)
		} else {
			c.Log.Error("buildkit daemon exited unexpectedly, marking not running", "exit_code", es.ExitCode())
		}
	}
}

func (c *Component) waitForReady(ctx context.Context) error {
	// Wait for the Unix socket to be created and connectable
	for range 30 {
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

	c.Log.Error("buildkit daemon readiness check timed out", "socket_path", c.socketPath)
	return fmt.Errorf("buildkit daemon failed to become ready within 60s (socket: %s)", c.socketPath)
}

func (c *Component) stopTask(ctx context.Context, task containerd.Task) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// If SIGTERM can't be delivered we still fall through to Delete: a task we
	// fail to reap here otherwise leaks and blocks the next NewTask on this
	// container.
	if err := task.Kill(shutdownCtx, unix.SIGTERM); err != nil {
		c.Log.Error("failed to send SIGTERM to buildkit task", "error", err)
	} else if status, err := task.Wait(shutdownCtx); err == nil {
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

	// Always delete the task, even if the kill path above failed, so the
	// container is left in a state where a fresh task can be created. Detach
	// from the parent's cancellation so a cancelled/expired ctx can't skip
	// cleanup, but carry over the containerd namespace (Delete requires it).
	deleteBase := context.Background()
	if ns, ok := namespaces.Namespace(ctx); ok {
		deleteBase = namespaces.WithNamespace(deleteBase, ns)
	}
	deleteCtx, deleteCancel := context.WithTimeout(deleteBase, 10*time.Second)
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
