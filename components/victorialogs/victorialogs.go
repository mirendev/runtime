// Package victorialogs provides a component for managing a VictoriaLogs server using containerd.
// VictoriaLogs is a log storage system that uses LogsQL for querying.
package victorialogs

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
	victoriaLogsContainerName = "miren-victorialogs"
	defaultHTTPPort           = 9428
)

var (
	victoriaLogsImage = imagerefs.VictoriaLogs
)

type VictoriaLogsConfig struct {
	HTTPPort        int
	DataPath        string
	RetentionPeriod string
}

type VictoriaLogsComponent struct {
	Log *slog.Logger
	CC  *containerd.Client

	Namespace string
	DataPath  string

	mu        sync.Mutex
	container containerd.Container
	running   bool
	httpPort  int
}

func NewVictoriaLogsComponent(log *slog.Logger, cc *containerd.Client, namespace, dataPath string) *VictoriaLogsComponent {
	return &VictoriaLogsComponent{
		Log:       log,
		CC:        cc,
		Namespace: namespace,
		DataPath:  dataPath,
	}
}

func (c *VictoriaLogsComponent) Start(ctx context.Context, config VictoriaLogsConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("victorialogs component already running")
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	c.Log.Info("pulling victorialogs image", "image", victoriaLogsImage)
	image, err := c.CC.Pull(ctx, victoriaLogsImage, containerd.WithPullUnpack)
	if err != nil {
		return fmt.Errorf("failed to pull victorialogs image: %w", err)
	}

	dataPath := filepath.Join(c.DataPath, "victorialogs")

	err = os.MkdirAll(dataPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Set defaults
	if config.HTTPPort == 0 {
		config.HTTPPort = defaultHTTPPort
	}
	if config.RetentionPeriod == "" {
		config.RetentionPeriod = "30d"
	}

	c.httpPort = config.HTTPPort

	// Check if container already exists
	existingContainer, err := c.CC.LoadContainer(ctx, victoriaLogsContainerName)
	if err == nil {
		c.Log.Info("found existing victorialogs container, restarting it", "container_id", existingContainer.ID())
		return c.restartExistingContainer(ctx, existingContainer, config)
	}

	c.Log.Info("starting victorialogs with host networking", "http_port", config.HTTPPort)

	// Create container
	container, err := c.createContainer(ctx, image, dataPath, config)
	if err != nil {
		return fmt.Errorf("failed to create victorialogs container: %w", err)
	}

	c.container = container

	// Start container with structured logging
	task, err := container.NewTask(ctx, slogout.WithLogger(c.Log, "victorialogs"))
	if err != nil {
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to create victorialogs task: %w", err)
	}

	err = task.Start(ctx)
	if err != nil {
		task.Delete(ctx)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to start victorialogs task: %w", err)
	}

	// Wait for VictoriaLogs to be ready
	if err := c.waitForReady(ctx, "localhost", config.HTTPPort); err != nil {
		task.Delete(ctx)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return err
	}

	c.running = true
	c.Log.Info("victorialogs server started", "container_id", container.ID(), "http_port", config.HTTPPort)

	return nil
}

func (c *VictoriaLogsComponent) Stop(ctx context.Context) error {
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
			c.Log.Warn("failed to get victorialogs task for shutdown", "error", err)
		}

		c.deleteContainerWithRetry(ctx)

		c.container = nil
	}

	c.running = false
	c.Log.Info("victorialogs server stopped")

	return nil
}

func (c *VictoriaLogsComponent) stopTask(ctx context.Context, task containerd.Task) {
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := task.Kill(shutdownCtx, unix.SIGTERM); err != nil {
		c.Log.Error("failed to send SIGTERM to victorialogs task", "error", err)
		return
	}

	status, err := task.Wait(shutdownCtx)
	if err == nil {
		select {
		case es := <-status:
			c.Log.Info("victorialogs task exited", "code", es.ExitCode())

		case <-shutdownCtx.Done():
			c.Log.Warn("victorialogs task did not exit gracefully, sending SIGKILL")
			killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer killCancel()

			if err := task.Kill(killCtx, unix.SIGKILL); err != nil {
				c.Log.Error("failed to send SIGKILL to victorialogs task", "error", err)
			} else {
				if _, waitErr := task.Wait(killCtx); waitErr != nil {
					c.Log.Error("victorialogs task wait after SIGKILL failed", "error", waitErr)
				}
			}
		}
	}

	deleteCtx, deleteCancel := context.WithTimeout(ctx, 10*time.Second)
	defer deleteCancel()

	if _, err := task.Delete(deleteCtx); err != nil {
		c.Log.Error("failed to delete victorialogs task", "error", err)
	}
}

func (c *VictoriaLogsComponent) deleteContainerWithRetry(ctx context.Context) {
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := c.container.Delete(deleteCtx, containerd.WithSnapshotCleanup)
		cancel()

		if err == nil {
			c.Log.Info("victorialogs container deleted successfully")
			return
		}

		c.Log.Error("failed to delete victorialogs container", "error", err, "attempt", attempt, "max_retries", maxRetries)

		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	c.Log.Error("failed to delete victorialogs container after all retries, potential snapshot leak")
}

func (c *VictoriaLogsComponent) HTTPEndpoint() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return ""
	}
	return fmt.Sprintf("localhost:%d", c.httpPort)
}

func (c *VictoriaLogsComponent) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *VictoriaLogsComponent) restartExistingContainer(ctx context.Context, container containerd.Container, config VictoriaLogsConfig) error {
	c.container = container
	c.httpPort = config.HTTPPort

	task, err := container.Task(ctx, nil)
	if err == nil {
		status, err := task.Status(ctx)
		if err != nil {
			c.Log.Warn("failed to get task status", "error", err)
		} else if status.Status == containerd.Running {
			c.Log.Info("victorialogs container is already running")
			c.running = true
			return c.waitForReady(ctx, "localhost", config.HTTPPort)
		}

		c.Log.Info("starting existing victorialogs task")
		err = task.Start(ctx)
		if err == nil {
			c.running = true
			c.Log.Info("victorialogs server restarted", "container_id", container.ID(), "http_port", config.HTTPPort)
			return c.waitForReady(ctx, "localhost", config.HTTPPort)
		}

		c.Log.Warn("failed to start existing task, deleting it", "error", err)
		task.Delete(ctx)
	}

	c.Log.Info("creating new task for existing container")
	task, err = container.NewTask(ctx, slogout.WithLogger(c.Log, "victorialogs"))
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
	c.Log.Info("victorialogs server restarted with new task", "container_id", container.ID(), "http_port", config.HTTPPort)

	return nil
}

func (c *VictoriaLogsComponent) createContainer(ctx context.Context, image containerd.Image, dataPath string, config VictoriaLogsConfig) (containerd.Container, error) {
	listenAddr := fmt.Sprintf(":%d", config.HTTPPort)

	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithHostNamespace(specs.NetworkNamespace),
		oci.WithProcessArgs(
			"/victoria-logs-prod",
			"-storageDataPath=/victoria-logs-data",
			"-retentionPeriod="+config.RetentionPeriod,
			"-httpListenAddr="+listenAddr,
		),
		oci.WithHostHostsFile,
		oci.WithHostResolvconf,

		oci.WithMounts([]specs.Mount{
			{
				Destination: "/victoria-logs-data",
				Type:        "bind",
				Source:      dataPath,
				Options:     []string{"rbind", "rw"},
			},
		}),
	}

	container, err := c.CC.NewContainer(
		ctx,
		victoriaLogsContainerName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(victoriaLogsContainerName+"-snapshot", image),
		containerd.WithNewSpec(opts...),
	)
	if err != nil {
		return nil, err
	}

	return container, nil
}

func (c *VictoriaLogsComponent) waitForReady(ctx context.Context, host string, port int) error {
	endpoint := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	for i := 0; i < 30; i++ {
		conn, err := net.DialTimeout("tcp", endpoint, 2*time.Second)
		if err == nil {
			conn.Close()
			c.Log.Info("victorialogs server is ready", "endpoint", endpoint)
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			continue
		}
	}

	c.Log.Warn("victorialogs server readiness check timed out", "endpoint", endpoint)
	return nil
}
