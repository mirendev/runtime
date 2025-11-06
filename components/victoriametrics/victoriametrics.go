// Package victoriametrics provides a component for managing a VictoriaMetrics server using containerd.
// VictoriaMetrics is a metrics storage system that uses MetricsQL for querying.
package victoriametrics

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/slogout"
)

const (
	victoriaMetricsContainerName = "miren-victoriametrics"
	defaultHTTPPort              = 8428
)

var (
	victoriaMetricsImage = imagerefs.VictoriaMetrics
)

type VictoriaMetricsConfig struct {
	HTTPPort        int
	DataPath        string
	RetentionPeriod string
}

type VictoriaMetricsComponent struct {
	Log *slog.Logger
	CC  *containerd.Client

	Namespace string
	DataPath  string

	mu        sync.Mutex
	container containerd.Container
	running   bool
	httpPort  int
}

func NewVictoriaMetricsComponent(log *slog.Logger, cc *containerd.Client, namespace, dataPath string) *VictoriaMetricsComponent {
	return &VictoriaMetricsComponent{
		Log:       log,
		CC:        cc,
		Namespace: namespace,
		DataPath:  dataPath,
	}
}

func (c *VictoriaMetricsComponent) Start(ctx context.Context, config VictoriaMetricsConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("victoriametrics component already running")
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	c.Log.Info("pulling victoriametrics image", "image", victoriaMetricsImage)
	image, err := c.CC.Pull(ctx, victoriaMetricsImage, containerd.WithPullUnpack)
	if err != nil {
		return fmt.Errorf("failed to pull victoriametrics image: %w", err)
	}

	dataPath := filepath.Join(c.DataPath, "victoriametrics")

	err = os.MkdirAll(dataPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Set defaults
	if config.HTTPPort == 0 {
		config.HTTPPort = defaultHTTPPort
	}
	if config.RetentionPeriod == "" {
		config.RetentionPeriod = "1"
	}

	c.httpPort = config.HTTPPort

	// Check if container already exists
	existingContainer, err := c.CC.LoadContainer(ctx, victoriaMetricsContainerName)
	if err == nil {
		c.Log.Info("found existing victoriametrics container, restarting it", "container_id", existingContainer.ID())
		return c.restartExistingContainer(ctx, existingContainer, config)
	}

	c.Log.Info("starting victoriametrics with host networking", "http_port", config.HTTPPort)

	// Create container
	container, err := c.createContainer(ctx, image, dataPath, config)
	if err != nil {
		return fmt.Errorf("failed to create victoriametrics container: %w", err)
	}

	c.container = container

	// Start container with structured logging
	task, err := container.NewTask(ctx, slogout.WithLogger(c.Log, "victoriametrics"))
	if err != nil {
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to create victoriametrics task: %w", err)
	}

	err = task.Start(ctx)
	if err != nil {
		task.Delete(ctx)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to start victoriametrics task: %w", err)
	}

	// Wait for VictoriaMetrics to be ready
	if err := c.waitForReady(ctx, "localhost", config.HTTPPort); err != nil {
		task.Delete(ctx)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return err
	}

	c.running = true
	c.Log.Info("victoriametrics server started", "container_id", container.ID(), "http_port", config.HTTPPort)

	return nil
}

func (c *VictoriaMetricsComponent) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	if c.container != nil {
		task, err := c.container.Task(ctx, nil)
		if err == nil {
			c.stopTask(ctx, task)
		} else {
			c.Log.Warn("failed to get victoriametrics task for shutdown", "error", err)
		}

		c.deleteContainerWithRetry(ctx)

		c.container = nil
	}

	c.running = false
	c.Log.Info("victoriametrics server stopped")

	return nil
}

func (c *VictoriaMetricsComponent) stopTask(ctx context.Context, task containerd.Task) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := task.Kill(shutdownCtx, unix.SIGTERM); err != nil {
		c.Log.Error("failed to send SIGTERM to victoriametrics task", "error", err)
		return
	}

	status, err := task.Wait(shutdownCtx)
	if err == nil {
		select {
		case es := <-status:
			c.Log.Info("victoriametrics task exited", "code", es.ExitCode())

		case <-shutdownCtx.Done():
			c.Log.Warn("victoriametrics task did not exit gracefully, sending SIGKILL")
			killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer killCancel()

			if err := task.Kill(killCtx, unix.SIGKILL); err != nil {
				c.Log.Error("failed to send SIGKILL to victoriametrics task", "error", err)
			} else {
				statusChan, waitErr := task.Wait(killCtx)
				if waitErr != nil {
					c.Log.Error("victoriametrics task wait after SIGKILL failed", "error", waitErr)
				} else {
					select {
					case es := <-statusChan:
						c.Log.Info("victoriametrics task exited after SIGKILL", "code", es.ExitCode())
					case <-killCtx.Done():
						c.Log.Error("victoriametrics task did not exit after SIGKILL timeout")
					}
				}
			}
		}
	}

	deleteCtx, deleteCancel := context.WithTimeout(ctx, 10*time.Second)
	defer deleteCancel()

	if _, err := task.Delete(deleteCtx); err != nil {
		c.Log.Error("failed to delete victoriametrics task", "error", err)
	}
}

func (c *VictoriaMetricsComponent) deleteContainerWithRetry(ctx context.Context) {
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := c.container.Delete(deleteCtx, containerd.WithSnapshotCleanup)
		cancel()

		if err == nil {
			c.Log.Info("victoriametrics container deleted successfully")
			return
		}

		c.Log.Error("failed to delete victoriametrics container", "error", err, "attempt", attempt, "max_retries", maxRetries)

		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	c.Log.Error("failed to delete victoriametrics container after all retries, potential snapshot leak")
}

func (c *VictoriaMetricsComponent) HTTPEndpoint() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return ""
	}
	return fmt.Sprintf("localhost:%d", c.httpPort)
}

func (c *VictoriaMetricsComponent) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *VictoriaMetricsComponent) restartExistingContainer(ctx context.Context, container containerd.Container, config VictoriaMetricsConfig) error {
	c.container = container
	c.httpPort = config.HTTPPort

	task, err := container.Task(ctx, nil)
	if err == nil {
		status, err := task.Status(ctx)
		if err != nil {
			c.Log.Warn("failed to get task status", "error", err)
		} else if status.Status == containerd.Running {
			c.Log.Info("victoriametrics container is already running")
			c.running = true
			return c.waitForReady(ctx, "localhost", config.HTTPPort)
		}

		c.Log.Info("starting existing victoriametrics task")
		err = task.Start(ctx)
		if err == nil {
			c.running = true
			c.Log.Info("victoriametrics server restarted", "container_id", container.ID(), "http_port", config.HTTPPort)
			return c.waitForReady(ctx, "localhost", config.HTTPPort)
		}

		c.Log.Warn("failed to start existing task, deleting it", "error", err)
		task.Delete(ctx)
	}

	c.Log.Info("creating new task for existing container")
	task, err = container.NewTask(ctx, slogout.WithLogger(c.Log, "victoriametrics"))
	if err != nil {
		return fmt.Errorf("failed to create new task for existing container: %w", err)
	}

	err = task.Start(ctx)
	if err != nil {
		task.Delete(ctx)
		return fmt.Errorf("failed to start new task for existing container: %w", err)
	}

	if err := c.waitForReady(ctx, "localhost", config.HTTPPort); err != nil {
		task.Delete(ctx)
		return err
	}

	c.running = true
	c.Log.Info("victoriametrics server restarted with new task", "container_id", container.ID(), "http_port", config.HTTPPort)

	return nil
}

func (c *VictoriaMetricsComponent) createContainer(ctx context.Context, image containerd.Image, dataPath string, config VictoriaMetricsConfig) (containerd.Container, error) {
	listenAddr := fmt.Sprintf(":%d", config.HTTPPort)

	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithHostNamespace(specs.NetworkNamespace),
		oci.WithProcessArgs(
			"/victoria-metrics-prod",
			"-storageDataPath=/victoria-metrics-data",
			"-retentionPeriod="+config.RetentionPeriod,
			"-httpListenAddr="+listenAddr,
			"-search.latencyOffset=2s",
		),
		oci.WithHostHostsFile,
		oci.WithHostResolvconf,

		oci.WithMounts([]specs.Mount{
			{
				Destination: "/victoria-metrics-data",
				Type:        "bind",
				Source:      dataPath,
				Options:     []string{"rbind", "rw"},
			},
		}),
	}

	container, err := c.CC.NewContainer(
		ctx,
		victoriaMetricsContainerName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(victoriaMetricsContainerName+"-snapshot", image),
		containerd.WithNewSpec(opts...),
	)
	if err != nil {
		return nil, err
	}

	return container, nil
}

func (c *VictoriaMetricsComponent) waitForReady(ctx context.Context, host string, port int) error {
	endpoint := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	for i := 0; i < 30; i++ {
		conn, err := net.DialTimeout("tcp", endpoint, 2*time.Second)
		if err == nil {
			conn.Close()
			c.Log.Info("victoriametrics server is ready", "endpoint", endpoint)
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			continue
		}
	}

	c.Log.Error("victoriametrics server readiness check timed out", "endpoint", endpoint)
	return fmt.Errorf("victoriametrics server readiness check timed out after 60s on endpoint %s", endpoint)
}
