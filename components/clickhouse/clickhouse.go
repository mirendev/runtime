// Package clickhouse provides a component for managing a ClickHouse server using containerd.
// The component uses host networking with non-default ports (8223 for HTTP,
// 9009 for native) to avoid conflicts with existing ClickHouse installations.
package clickhouse

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"text/template"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/slogout"
)

//go:embed config.xml
var configTemplate string

//go:embed users.xml
var usersTemplate string

const (
	clickhouseContainerName = "miren-clickhouse"
	clickhouseDataDir       = "/var/lib/clickhouse"
	clickhouseLogDir        = "/var/log/clickhouse-server"
	clickhouseConfigDir     = "/etc/clickhouse-server"
	defaultHTTPPort         = 8223 // Non-default port to avoid conflicts
	defaultNativePort       = 9009 // Non-default port to avoid conflicts
	defaultInterServerPort  = 9010 // Non-default port to avoid conflicts
)

var (
	clickhouseImage = imagerefs.ClickHouse
)

type ClickHouseConfig struct {
	HTTPPort        int
	NativePort      int
	InterServerPort int
	DataDir         string
	LogDir          string
	Database        string
	User            string
	Password        string
}

type ClickHouseComponent struct {
	Log *slog.Logger
	CC  *containerd.Client

	Namespace string
	DataPath  string

	mu           sync.Mutex
	container    containerd.Container
	running      bool
	httpPort     int
	nativePort   int
	interSvrPort int
}

func NewClickHouseComponent(log *slog.Logger, cc *containerd.Client, namespace, dataPath string) *ClickHouseComponent {
	return &ClickHouseComponent{
		Log:       log,
		CC:        cc,
		Namespace: namespace,
		DataPath:  dataPath,
	}
}

func (c *ClickHouseComponent) Start(ctx context.Context, config ClickHouseConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("clickhouse component already running")
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	// Pull ClickHouse image
	c.Log.Info("pulling clickhouse image", "image", clickhouseImage)
	image, err := c.CC.Pull(ctx, clickhouseImage, containerd.WithPullUnpack)
	if err != nil {
		return fmt.Errorf("failed to pull clickhouse image: %w", err)
	}

	dataPath := filepath.Join(c.DataPath, "clickhouse")
	logPath := filepath.Join(c.DataPath, "clickhouse-logs")
	configPath := filepath.Join(c.DataPath, "clickhouse-config")

	err = os.MkdirAll(dataPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	if err := os.Chown(dataPath, 101, 101); err != nil {
		return fmt.Errorf("failed to set ownership for data directory: %w", err)
	}

	if err := os.Chmod(dataPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions for data directory: %w", err)
	}

	err = os.MkdirAll(logPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	if err := os.Chown(logPath, 101, 101); err != nil {
		return fmt.Errorf("failed to set ownership for log directory: %w", err)
	}

	if err := os.Chmod(logPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions for log directory: %w", err)
	}

	err = os.MkdirAll(configPath, 0755)
	if err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Generate ClickHouse configuration files
	err = c.generateConfigFiles(configPath, config)
	if err != nil {
		return fmt.Errorf("failed to generate config files: %w", err)
	}

	// Set defaults
	if config.HTTPPort == 0 {
		config.HTTPPort = defaultHTTPPort
	}
	if config.NativePort == 0 {
		config.NativePort = defaultNativePort
	}
	if config.InterServerPort == 0 {
		config.InterServerPort = defaultInterServerPort
	}
	if config.DataDir == "" {
		config.DataDir = clickhouseDataDir
	}
	if config.LogDir == "" {
		config.LogDir = clickhouseLogDir
	}
	if config.Database == "" {
		config.Database = "default"
	}
	if config.User == "" {
		config.User = "default"
	}
	// Password can be empty by default

	// Store ports for later use
	c.httpPort = config.HTTPPort
	c.nativePort = config.NativePort
	c.interSvrPort = config.InterServerPort

	// Check if container already exists
	existingContainer, err := c.CC.LoadContainer(ctx, clickhouseContainerName)
	if err == nil {
		c.Log.Info("found existing clickhouse container, restarting it", "container_id", existingContainer.ID())
		return c.restartExistingContainer(ctx, existingContainer, config)
	}

	c.Log.Info("starting clickhouse with host networking",
		"http_port", config.HTTPPort,
		"native_port", config.NativePort,
		"interserver_port", config.InterServerPort)

	// Create container
	container, err := c.createContainer(ctx, image, config)
	if err != nil {
		return fmt.Errorf("failed to create clickhouse container: %w", err)
	}

	c.container = container

	// Start container with structured logging, ignoring timestamp lines
	task, err := container.NewTask(ctx, slogout.WithLogger(c.Log, "clickhouse",
		slogout.WithIgnorePattern(`^\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2}\.\d+`)))
	if err != nil {
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to create clickhouse task: %w", err)
	}

	err = task.Start(ctx)
	if err != nil {
		task.Delete(ctx)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		return fmt.Errorf("failed to start clickhouse task: %w", err)
	}

	// Wait for ClickHouse to be ready
	c.waitForReady(ctx, "localhost", config.HTTPPort)
	c.waitForReady(ctx, "localhost", config.NativePort)

	// Create database if specified
	if config.Database != "" && config.Database != "default" {
		err = c.createDatabase(ctx, config)
		if err != nil {
			c.Log.Error("failed to create database", "database", config.Database, "error", err)
			// Don't fail the entire startup for database creation failure
		} else {
			c.Log.Info("database created successfully", "database", config.Database)
		}
	}

	c.running = true
	c.Log.Info("clickhouse server started",
		"container_id", container.ID(),
		"http_port", config.HTTPPort,
		"native_port", config.NativePort,
	)

	return nil
}

func (c *ClickHouseComponent) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	if c.container != nil {
		// Stop and delete the task with timeout and graceful escalation
		task, err := c.container.Task(ctx, nil)
		if err == nil {
			c.stopTask(ctx, task)
		} else {
			c.Log.Warn("failed to get clickhouse task for shutdown", "error", err)
		}

		// Delete the container with retry logic
		c.deleteContainerWithRetry(ctx)

		c.container = nil
	}

	c.running = false
	c.Log.Info("clickhouse server stopped")

	return nil
}

func (c *ClickHouseComponent) stopTask(ctx context.Context, task containerd.Task) {
	// Create a timeout context for graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// First attempt: graceful shutdown with SIGTERM
	if err := task.Kill(shutdownCtx, unix.SIGTERM); err != nil {
		c.Log.Error("failed to send SIGTERM to clickhouse task", "error", err)
		return
	}

	// Wait for graceful exit with timeout
	status, err := task.Wait(shutdownCtx)
	if err == nil {
		select {
		case es := <-status:
			c.Log.Info("clickhouse task exited", "code", es.ExitCode())

		case <-shutdownCtx.Done():
			// Timeout reached, escalate to SIGKILL
			c.Log.Warn("clickhouse task did not exit gracefully, sending SIGKILL")
			killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer killCancel()

			if err := task.Kill(killCtx, unix.SIGKILL); err != nil {
				c.Log.Error("failed to send SIGKILL to clickhouse task", "error", err)
			} else {
				// Wait for forced exit
				if _, waitErr := task.Wait(killCtx); waitErr != nil {
					c.Log.Error("clickhouse task wait after SIGKILL failed", "error", waitErr)
				}
			}
		}
	}

	// Delete the task
	deleteCtx, deleteCancel := context.WithTimeout(ctx, 10*time.Second)
	defer deleteCancel()

	if _, err := task.Delete(deleteCtx); err != nil {
		c.Log.Error("failed to delete clickhouse task", "error", err)
	}
}

func (c *ClickHouseComponent) deleteContainerWithRetry(ctx context.Context) {
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := c.container.Delete(deleteCtx, containerd.WithSnapshotCleanup)
		cancel()

		if err == nil {
			c.Log.Info("clickhouse container deleted successfully")
			return
		}

		c.Log.Error("failed to delete clickhouse container", "error", err, "attempt", attempt, "max_retries", maxRetries)

		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	c.Log.Error("failed to delete clickhouse container after all retries, potential snapshot leak")
}

func (c *ClickHouseComponent) HTTPEndpoint() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return ""
	}
	return fmt.Sprintf("localhost:%d", c.httpPort)
}

func (c *ClickHouseComponent) NativeEndpoint() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return ""
	}
	return fmt.Sprintf("localhost:%d", c.nativePort)
}

func (c *ClickHouseComponent) InterServerEndpoint() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return ""
	}
	return fmt.Sprintf("localhost:%d", c.interSvrPort)
}

func (c *ClickHouseComponent) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *ClickHouseComponent) restartExistingContainer(ctx context.Context, container containerd.Container, config ClickHouseConfig) error {
	c.container = container

	// Store ports for later use
	c.httpPort = config.HTTPPort
	c.nativePort = config.NativePort
	c.interSvrPort = config.InterServerPort

	// Check if there's already a running task
	task, err := container.Task(ctx, nil)
	if err == nil {
		// Task exists, check its status
		status, err := task.Status(ctx)
		if err != nil {
			c.Log.Warn("failed to get task status", "error", err)
		} else if status.Status == containerd.Running {
			c.Log.Info("clickhouse container is already running")
			c.running = true
			c.waitForReady(ctx, "localhost", config.HTTPPort)
			c.waitForReady(ctx, "localhost", config.NativePort)
			// Create database if specified
			if config.Database != "" && config.Database != "default" {
				if err := c.createDatabase(ctx, config); err != nil {
					c.Log.Error("failed to create database on restart", "database", config.Database, "error", err)
				}
			}
			return nil
		}

		// Task exists but not running, try to start it
		c.Log.Info("starting existing clickhouse task")
		err = task.Start(ctx)
		if err == nil {
			c.running = true
			c.Log.Info("clickhouse server restarted", "container_id", container.ID(), "http_port", config.HTTPPort)
			c.waitForReady(ctx, "localhost", config.HTTPPort)
			c.waitForReady(ctx, "localhost", config.NativePort)
			// Create database if specified
			if config.Database != "" && config.Database != "default" {
				if err := c.createDatabase(ctx, config); err != nil {
					c.Log.Error("failed to create database on restart", "database", config.Database, "error", err)
				}
			}
			return nil
		}

		// Failed to start existing task, delete it and create new one
		c.Log.Warn("failed to start existing task, deleting it", "error", err)
		task.Delete(ctx)
	}

	// Create and start new task with structured logging, ignoring timestamp lines
	c.Log.Info("creating new task for existing container")
	task, err = container.NewTask(ctx, slogout.WithLogger(c.Log, "clickhouse",
		slogout.WithIgnorePattern(`^\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2}\.\d+`)))
	if err != nil {
		return fmt.Errorf("failed to create new task for existing container: %w", err)
	}

	err = task.Start(ctx)
	if err != nil {
		task.Delete(ctx)
		return fmt.Errorf("failed to start new task for existing container: %w", err)
	}

	// Wait for ClickHouse to be ready
	c.waitForReady(ctx, "localhost", config.HTTPPort)
	c.waitForReady(ctx, "localhost", config.NativePort)

	// Create database if specified
	if config.Database != "" && config.Database != "default" {
		err = c.createDatabase(ctx, config)
		if err != nil {
			c.Log.Error("failed to create database on restart", "database", config.Database, "error", err)
			// Don't fail the entire restart for database creation failure
		} else {
			c.Log.Info("database created successfully on restart", "database", config.Database)
		}
	}

	c.running = true
	c.Log.Info("clickhouse server restarted with new task", "container_id", container.ID(), "http_port", config.HTTPPort)

	return nil
}

func (c *ClickHouseComponent) createContainer(ctx context.Context, image containerd.Image, config ClickHouseConfig) (containerd.Container, error) {
	dataPath := filepath.Join(c.DataPath, "clickhouse")
	logPath := filepath.Join(c.DataPath, "clickhouse-logs")
	configPath := filepath.Join(c.DataPath, "clickhouse-config")

	// Create container spec with ClickHouse configuration using host networking
	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithHostNamespace(specs.NetworkNamespace), // Use host network namespace
		oci.WithProcessArgs("clickhouse-server", "--config-file=/etc/clickhouse-server/config.xml"),
		oci.WithProcessCwd(config.DataDir),
		oci.WithUIDGID(101, 101),
		oci.WithHostHostsFile,
		oci.WithHostResolvconf,

		func(ctx context.Context, c1 oci.Client, c2 *containers.Container, s *oci.Spec) error {
			s.Process.Rlimits = append(s.Process.Rlimits, specs.POSIXRlimit{
				Type: "RLIMIT_NOFILE",
				Hard: 1048576, // 1M file descriptors
				Soft: 1048576,
			})

			return nil
		},

		oci.WithMounts([]specs.Mount{
			{
				Destination: config.DataDir,
				Type:        "bind",
				Source:      dataPath,
				Options:     []string{"rbind", "rw"},
			},
			{
				Destination: config.LogDir,
				Type:        "bind",
				Source:      logPath,
				Options:     []string{"rbind", "rw"},
			},
			{
				Destination: clickhouseConfigDir,
				Type:        "bind",
				Source:      configPath,
				Options:     []string{"rbind", "ro"},
			},

			// Binding cgroup filesystem so clickhouse can monitor resource limits
			{
				Destination: "/sys/fs/cgroup",
				Type:        "bind",
				Source:      "/sys/fs/cgroup",
				Options:     []string{"rbind", "ro"},
			},
		}),
	}

	// Create container
	container, err := c.CC.NewContainer(
		ctx,
		clickhouseContainerName,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(clickhouseContainerName+"-snapshot", image),
		containerd.WithNewSpec(opts...),
	)
	if err != nil {
		return nil, err
	}

	return container, nil
}

func (c *ClickHouseComponent) waitForReady(ctx context.Context, host string, port int) {
	endpoint := fmt.Sprintf("%s:%d", host, port)

	for i := 0; i < 60; i++ { // ClickHouse takes longer to start than etcd
		conn, err := net.DialTimeout("tcp", endpoint, 2*time.Second)
		if err == nil {
			conn.Close()
			c.Log.Info("clickhouse server is ready", "endpoint", endpoint)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
			continue
		}
	}

	c.Log.Warn("clickhouse server readiness check timed out", "endpoint", endpoint)
}

func (c *ClickHouseComponent) generateConfigFiles(configPath string, config ClickHouseConfig) error {
	// Generate main config.xml using embedded template
	configTmpl, err := template.New("config").Parse(configTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse embedded config template: %w", err)
	}

	configFile, err := os.Create(filepath.Join(configPath, "config.xml"))
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer configFile.Close()

	err = configTmpl.Execute(configFile, config)
	if err != nil {
		return fmt.Errorf("failed to execute config template: %w", err)
	}

	// Generate users.xml using embedded template
	usersTmpl, err := template.New("users").Parse(usersTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse embedded users template: %w", err)
	}

	usersFile, err := os.Create(filepath.Join(configPath, "users.xml"))
	if err != nil {
		return fmt.Errorf("failed to create users file: %w", err)
	}
	defer usersFile.Close()

	err = usersTmpl.Execute(usersFile, config)
	if err != nil {
		return fmt.Errorf("failed to execute users template: %w", err)
	}

	c.Log.Info("generated clickhouse config files", "config_path", configPath)
	return nil
}

func (c *ClickHouseComponent) createDatabase(ctx context.Context, config ClickHouseConfig) error {
	// Connect to ClickHouse using the native protocol
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{fmt.Sprintf("localhost:%d", config.NativePort)},
		Auth: clickhouse.Auth{
			Database: "default", // Connect to default database first
			Username: config.User,
			Password: config.Password,
		},
		DialTimeout: 30 * time.Second,
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}
	defer conn.Close()

	// Test connection
	if err := conn.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	// Create database
	createDBSQL := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", config.Database)
	err = conn.Exec(ctx, createDBSQL)
	if err != nil {
		return fmt.Errorf("failed to create database '%s': %w", config.Database, err)
	}

	c.Log.Info("successfully created database", "database", config.Database)
	return nil
}
