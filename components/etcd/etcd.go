// Package etcd provides a component for managing an etcd server using containerd.
// The component uses host networking with non-default ports (12379 for client,
// 12380 for peer) to avoid conflicts with existing etcd installations.
package etcd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

const (
	etcdImage         = "gcr.io/etcd-development/etcd:v3.5.15"
	etcdContainerName = "runtime-etcd"
	etcdDataDir       = "/etcd-data"
	defaultEtcdPort   = 12379 // Non-default port to avoid conflicts
	defaultPeerPort   = 12380 // Non-default port to avoid conflicts
)

type EtcdConfig struct {
	Name         string
	DataDir      string
	ClientPort   int
	PeerPort     int
	InitialToken string
	ClusterState string
}

type EtcdComponent struct {
	Log *slog.Logger
	CC  *containerd.Client

	Namespace string
	DataPath  string

	mu         sync.Mutex
	container  containerd.Container
	running    bool
	clientPort int
	peerPort   int
}

func NewEtcdComponent(log *slog.Logger, cc *containerd.Client, namespace, dataPath string) *EtcdComponent {
	return &EtcdComponent{
		Log:       log,
		CC:        cc,
		Namespace: namespace,
		DataPath:  dataPath,
	}
}

func (e *EtcdComponent) Start(ctx context.Context, config EtcdConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("etcd component already running")
	}

	ctx = namespaces.WithNamespace(ctx, e.Namespace)

	// Pull etcd image
	e.Log.Info("pulling etcd image", "image", etcdImage)
	image, err := e.CC.Pull(ctx, etcdImage, containerd.WithPullUnpack)
	if err != nil {
		return fmt.Errorf("failed to pull etcd image: %w", err)
	}

	dataPath := filepath.Join(e.DataPath, "etcd")
	os.MkdirAll(dataPath, 0755)

	// Check if container already exists
	existingContainer, err := e.CC.LoadContainer(ctx, etcdContainerName)
	if err == nil {
		e.Log.Info("found existing etcd container, restarting it", "container_id", existingContainer.ID())
		return e.restartExistingContainer(ctx, existingContainer, config)
	}

	// Set defaults
	if config.Name == "" {
		config.Name = "etcd1"
	}
	if config.DataDir == "" {
		config.DataDir = etcdDataDir
	}
	if config.ClientPort == 0 {
		config.ClientPort = defaultEtcdPort
	}
	if config.PeerPort == 0 {
		config.PeerPort = defaultPeerPort
	}
	if config.InitialToken == "" {
		config.InitialToken = "etcd-cluster-1"
	}
	if config.ClusterState == "" {
		config.ClusterState = "new"
	}

	// Store ports for later use
	e.clientPort = config.ClientPort
	e.peerPort = config.PeerPort

	e.Log.Info("starting etcd with host networking", "client_port", config.ClientPort, "peer_port", config.PeerPort)

	// Create container
	container, err := e.createContainer(ctx, image, config)
	if err != nil {
		return fmt.Errorf("failed to create etcd container: %w", err)
	}

	e.container = container

	// Start container
	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
	if err != nil {
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to create etcd task: %w", err)
	}

	err = task.Start(ctx)
	if err != nil {
		task.Delete(ctx)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to start etcd task: %w", err)
	}

	e.running = true
	e.Log.Info("etcd server started", "container_id", container.ID(), "client_port", config.ClientPort)

	// Wait for etcd to be ready
	go e.waitForReady(ctx, "localhost", config.ClientPort)

	return nil
}

func (e *EtcdComponent) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	ctx = namespaces.WithNamespace(ctx, e.Namespace)

	if e.container != nil {
		// Stop and delete the task with timeout and graceful escalation
		task, err := e.container.Task(ctx, nil)
		if err == nil {
			e.stopTask(ctx, task)
		} else {
			e.Log.Warn("failed to get etcd task for shutdown", "error", err)
		}

		// Delete the container with retry logic
		e.deleteContainerWithRetry(ctx)

		e.container = nil
	}

	e.running = false
	e.Log.Info("etcd server stopped")

	return nil
}

func (e *EtcdComponent) stopTask(ctx context.Context, task containerd.Task) {
	// Create a timeout context for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// First attempt: graceful shutdown with SIGTERM
	if err := task.Kill(shutdownCtx, unix.SIGTERM); err != nil {
		e.Log.Error("failed to send SIGTERM to etcd task", "error", err)
		return
	}

	// Wait for graceful exit with timeout
	waitCh := make(chan error, 1)
	go func() {
		_, waitErr := task.Wait(shutdownCtx)
		waitCh <- waitErr
	}()

	select {
	case waitErr := <-waitCh:
		if waitErr != nil {
			e.Log.Warn("etcd task wait returned error", "error", waitErr)
		} else {
			e.Log.Info("etcd task exited gracefully")
		}
	case <-shutdownCtx.Done():
		// Timeout reached, escalate to SIGKILL
		e.Log.Warn("etcd task did not exit gracefully, sending SIGKILL")
		killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer killCancel()
		
		if err := task.Kill(killCtx, unix.SIGKILL); err != nil {
			e.Log.Error("failed to send SIGKILL to etcd task", "error", err)
		} else {
			// Wait for forced exit
			if _, waitErr := task.Wait(killCtx); waitErr != nil {
				e.Log.Error("etcd task wait after SIGKILL failed", "error", waitErr)
			}
		}
	}

	// Delete the task
	deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer deleteCancel()
	
	if _, err := task.Delete(deleteCtx); err != nil {
		e.Log.Error("failed to delete etcd task", "error", err)
	}
}

func (e *EtcdComponent) deleteContainerWithRetry(ctx context.Context) {
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := e.container.Delete(deleteCtx, containerd.WithSnapshotCleanup)
		cancel()

		if err == nil {
			e.Log.Info("etcd container deleted successfully")
			return
		}

		e.Log.Error("failed to delete etcd container", "error", err, "attempt", attempt, "max_retries", maxRetries)
		
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	e.Log.Error("failed to delete etcd container after all retries, potential snapshot leak")
}

func (e *EtcdComponent) ClientEndpoint() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", e.clientPort)
}

func (e *EtcdComponent) PeerEndpoint() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", e.peerPort)
}

func (e *EtcdComponent) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

func (e *EtcdComponent) restartExistingContainer(ctx context.Context, container containerd.Container, config EtcdConfig) error {
	e.container = container

	// Store ports for later use
	e.clientPort = config.ClientPort
	e.peerPort = config.PeerPort

	// Check if there's already a running task
	task, err := container.Task(ctx, nil)
	if err == nil {
		// Task exists, check its status
		status, err := task.Status(ctx)
		if err != nil {
			e.Log.Warn("failed to get task status", "error", err)
		} else if status.Status == containerd.Running {
			e.Log.Info("etcd container is already running")
			e.running = true
			go e.waitForReady(ctx, "localhost", config.ClientPort)
			return nil
		}

		// Task exists but not running, try to start it
		e.Log.Info("starting existing etcd task")
		err = task.Start(ctx)
		if err == nil {
			e.running = true
			e.Log.Info("etcd server restarted", "container_id", container.ID(), "client_port", config.ClientPort)
			go e.waitForReady(ctx, "localhost", config.ClientPort)
			return nil
		}

		// Failed to start existing task, delete it and create new one
		e.Log.Warn("failed to start existing task, deleting it", "error", err)
		task.Delete(ctx)
	}

	// Create and start new task
	e.Log.Info("creating new task for existing container")
	task, err = container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
	if err != nil {
		return fmt.Errorf("failed to create new task for existing container: %w", err)
	}

	err = task.Start(ctx)
	if err != nil {
		task.Delete(ctx)
		return fmt.Errorf("failed to start new task for existing container: %w", err)
	}

	e.running = true
	e.Log.Info("etcd server restarted with new task", "container_id", container.ID(), "client_port", config.ClientPort)

	// Wait for etcd to be ready
	go e.waitForReady(ctx, "localhost", config.ClientPort)

	return nil
}

func (e *EtcdComponent) createContainer(ctx context.Context, image containerd.Image, config EtcdConfig) (containerd.Container, error) {
	dataPath := filepath.Join(e.DataPath, "etcd")

	// Create container spec with etcd configuration using host networking
	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithHostNamespace(specs.NetworkNamespace), // Use host network namespace
		oci.WithProcessArgs(
			"/usr/local/bin/etcd",
			"--name", config.Name,
			"--data-dir", config.DataDir,
			"--listen-client-urls", fmt.Sprintf("http://0.0.0.0:%d", config.ClientPort),
			"--advertise-client-urls", fmt.Sprintf("http://localhost:%d", config.ClientPort),
			"--listen-peer-urls", fmt.Sprintf("http://0.0.0.0:%d", config.PeerPort),
			"--initial-advertise-peer-urls", fmt.Sprintf("http://localhost:%d", config.PeerPort),
			"--initial-cluster", fmt.Sprintf("%s=http://localhost:%d", config.Name, config.PeerPort),
			"--initial-cluster-token", config.InitialToken,
			"--initial-cluster-state", config.ClusterState,
		),
		oci.WithEnv([]string{
			"ETCD_AUTO_COMPACTION_MODE=periodic",
			"ETCD_AUTO_COMPACTION_RETENTION=1h",
		}),
		oci.WithMounts([]specs.Mount{
			{
				Destination: config.DataDir,
				Type:        "bind",
				Source:      dataPath,
				Options:     []string{"rbind", "rw"},
			},
		}),
	}

	// Create container
	container, err := e.CC.NewContainer(
		ctx,
		etcdContainerName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(etcdContainerName+"-snapshot", image),
		containerd.WithNewSpec(opts...),
	)
	if err != nil {
		return nil, err
	}

	return container, nil
}

func (e *EtcdComponent) waitForReady(ctx context.Context, host string, port int) {
	endpoint := fmt.Sprintf("%s:%d", host, port)

	for i := 0; i < 30; i++ {
		conn, err := net.DialTimeout("tcp", endpoint, 1*time.Second)
		if err == nil {
			conn.Close()
			e.Log.Info("etcd server is ready", "endpoint", endpoint)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
			continue
		}
	}

	e.Log.Warn("etcd server readiness check timed out", "endpoint", endpoint)
}
