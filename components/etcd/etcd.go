// Package etcd provides a component for managing an etcd server using containerd.
// The component uses host networking with non-default ports (12379 for client,
// 12380 for peer) to avoid conflicts with existing etcd installations.
package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"miren.dev/runtime/components/base"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/slogout"
)

const (
	etcdContainerName   = "miren-etcd"
	etcdDataDir         = "/etcd-data"
	defaultEtcdPort     = 12379             // Non-default port to avoid conflicts
	defaultEtcdHTTPPort = 12381             // Non-default port to avoid conflicts
	defaultPeerPort     = 12380             // Non-default port to avoid conflicts
	etcdStateFile       = "etcd-state.json" // Tracks container config state
)

var (
	etcdImage = imagerefs.Etcd
)

// etcdState tracks the configuration state of the etcd container.
// This is persisted to detect when configuration changes require container recreation.
type etcdState struct {
	TLSEnabled      bool   `json:"tls_enabled"`
	ConfigVersion   int    `json:"config_version"`
	TuningSignature string `json:"tuning_signature,omitempty"`
}

// currentConfigVersion should be bumped whenever the container spec changes
// (new env vars, different args, etc.) so that upgrades recreate the container.
//
// v2: memory auto-tuning — etcd flags/env are now scaled to system RAM.
const currentConfigVersion = 2

// TLSConfig holds TLS certificate paths for etcd mTLS.
// When configured, etcd will require client certificate authentication.
type TLSConfig struct {
	CertsDir string // Directory containing ca.crt, server.crt, server.key
}

type EtcdConfig struct {
	Name           string
	DataDir        string
	ClientPort     int
	HTTPClientPort int
	PeerPort       int
	InitialToken   string
	ClusterState   string
	TLS            *TLSConfig // If set, enables mTLS for client connections

	// QuotaBackendBytes, when > 0, sets --quota-backend-bytes explicitly. When 0 the
	// value is auto-derived from system RAM (see computeTuning).
	QuotaBackendBytes int64
}

type EtcdComponent struct {
	*base.BaseComponent

	clientPort int
	peerPort   int
	tlsEnabled bool
	config     EtcdConfig

	// quotaBackendBytes is the effective backend quota the container was started with;
	// the maintenance loop uses it for the near-quota reclaim trigger.
	quotaBackendBytes int64

	// writer, when set, receives etcd health gauges from the maintenance loop. It is set
	// via SetMetricsWriter after the metrics subsystem comes up (which happens after etcd
	// and its maintenance loop have already started), so access is atomic. Nil-safe.
	writer atomic.Pointer[metrics.VictoriaMetricsWriter]

	// metricsKick nudges the maintenance loop to run a check the moment the metrics
	// writer is attached. The writer only exists after the embedded VictoriaMetrics
	// address is known, which is well after the loop's immediate startup check — so
	// without this the first health sample would be up to maintenanceInterval late
	// (and, on a cluster that restarts more often than that, effectively never). It is
	// buffered so SetMetricsWriter never blocks even if the loop is still connecting.
	metricsKick chan struct{}
}

// SetMetricsWriter configures the metrics sink for the maintenance loop's health gauges.
// Safe to call with nil or at any time (the maintenance loop reads it atomically each tick).
func (e *EtcdComponent) SetMetricsWriter(w *metrics.VictoriaMetricsWriter) {
	e.writer.Store(w)
	if w == nil {
		return
	}
	// Land a fresh sample now instead of waiting for the next periodic tick. Coalesces:
	// if a kick is already pending the loop will pick up the latest writer anyway.
	select {
	case e.metricsKick <- struct{}{}:
	default:
	}
}

func NewEtcdComponent(log *slog.Logger, cc *containerd.Client, namespace, dataPath string) *EtcdComponent {
	bc := base.NewBaseComponent(log, cc, namespace, dataPath, "etcd")
	// etcd is critical - the system cannot run without it, so use aggressive restart policy
	bc.RestartPolicy = base.AggressiveRestartPolicy()
	// etcd uses 1s dial timeout and 1s interval for readiness checks
	bc.ReadyConfig.DialTimeout = 1 * time.Second
	bc.ReadyConfig.Interval = 1 * time.Second

	e := &EtcdComponent{
		BaseComponent: bc,
		metricsKick:   make(chan struct{}, 1),
	}

	// Set up callbacks for the base component
	bc.CreateTask = e.createTask
	bc.GetReadyPort = e.getReadyPort

	return e
}

func (e *EtcdComponent) createTask(ctx context.Context, container containerd.Container) (containerd.Task, error) {
	return container.NewTask(ctx, slogout.WithLogger(e.Log, "etcd",
		slogout.WithJSONParsing(), slogout.WithMaxLevel(slog.LevelInfo)))
}

func (e *EtcdComponent) getReadyPort() int {
	return e.clientPort
}

// stateFilePath returns the path to the etcd state file.
func (e *EtcdComponent) stateFilePath() string {
	return filepath.Join(e.DataPath, "etcd", etcdStateFile)
}

// loadState loads the persisted etcd state, returning nil if not found.
func (e *EtcdComponent) loadState() *etcdState {
	data, err := os.ReadFile(e.stateFilePath())
	if err != nil {
		return nil
	}
	var state etcdState
	if err := json.Unmarshal(data, &state); err != nil {
		e.Log.Warn("failed to parse etcd state file, will recreate container", "error", err)
		return nil
	}
	return &state
}

// saveState persists the etcd state to disk.
func (e *EtcdComponent) saveState(state *etcdState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal etcd state: %w", err)
	}
	if err := os.WriteFile(e.stateFilePath(), data, 0600); err != nil {
		return fmt.Errorf("failed to write etcd state file: %w", err)
	}
	return nil
}

func (e *EtcdComponent) Start(ctx context.Context, config EtcdConfig) error {
	e.LockOp()
	defer e.UnlockOp()

	if e.IsRunning() {
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
	err = os.MkdirAll(dataPath, 0700)
	if err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Scale etcd's memory-related config to the size of the node it runs on so it
	// behaves as a ~10%-of-RAM co-tenant rather than growing to fill the node. The
	// backend quota honors an explicit config override when set.
	tuning := computeTuning(detectSystemRAMBytes(), config.QuotaBackendBytes)
	tuningSignature := tuning.signature()
	e.quotaBackendBytes = tuning.QuotaBackendBytes

	// Check if the container config has changed since last start. If so, we
	// must recreate the container since env vars and args are baked into the spec.
	requestedTLSEnabled := config.TLS != nil
	prevState := e.loadState()

	var configChanged bool
	if prevState == nil {
		// No state file. If TLS is requested we must recreate to be sure it's
		// enabled; if not, the existing container is probably fine.
		configChanged = requestedTLSEnabled
	} else {
		configChanged = prevState.TLSEnabled != requestedTLSEnabled ||
			prevState.ConfigVersion != currentConfigVersion ||
			prevState.TuningSignature != tuningSignature
	}

	// Check if container already exists
	existingContainer, err := e.CC.LoadContainer(ctx, etcdContainerName)
	if err == nil {
		if configChanged {
			e.Log.Info("etcd container config changed, recreating container",
				"previous_tls", prevState != nil && prevState.TLSEnabled,
				"requested_tls", requestedTLSEnabled,
				"previous_config_version", func() int {
					if prevState != nil {
						return prevState.ConfigVersion
					}
					return 0
				}(),
				"current_config_version", currentConfigVersion,
				"tuning_changed", prevState != nil && prevState.TuningSignature != tuningSignature)
			e.CleanupExistingContainer(ctx, existingContainer)
		} else {
			e.Log.Info("found existing etcd container, attempting restart", "container_id", existingContainer.ID())
			err = e.restartExistingContainer(ctx, existingContainer, config)
			if err == nil {
				e.StartMaintenanceLoop(ctx)
				return nil
			}
			// If restart failed (e.g., port mismatch), try deleting the container and creating fresh
			e.Log.Warn("restart of existing container failed, recreating", "error", err)
			e.CleanupExistingContainer(ctx, existingContainer)
		}
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
	if config.HTTPClientPort == 0 {
		config.HTTPClientPort = defaultEtcdHTTPPort
	}
	if config.PeerPort == 0 {
		config.PeerPort = defaultPeerPort
	}
	if config.ClusterState == "" {
		config.ClusterState = "new"
	}

	// Store ports and TLS state for later use
	e.clientPort = config.ClientPort
	e.peerPort = config.PeerPort
	e.tlsEnabled = config.TLS != nil
	e.config = config

	e.Log.Info("starting etcd with host networking", "client_port", config.ClientPort, "peer_port", config.PeerPort, "tls", e.tlsEnabled)

	// Create container
	container, err := e.createContainer(ctx, image, config, tuning)
	if err != nil {
		return fmt.Errorf("failed to create etcd container: %w", err)
	}

	e.SetContainer(container)

	// Start container with structured logging for JSON output
	task, err := e.createTask(ctx, container)
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

	e.SetTask(task)
	e.Log.Info("etcd server started", "container_id", container.ID(), "client_port", config.ClientPort)

	// Wait for etcd to be ready before returning
	if err := e.WaitForReady(ctx, "localhost", config.ClientPort); err != nil {
		e.Log.Warn("etcd readiness check failed, continuing anyway", "error", err)
	}

	// Start monitoring for unexpected exits
	e.StartExitMonitor(ctx)

	// Start periodic maintenance (health logging + auto-defrag)
	e.StartMaintenanceLoop(ctx)

	// Persist the TLS state and tuning signature so we can detect config changes
	// (including a node RAM change that alters tuning) on restart.
	if err := e.saveState(&etcdState{
		TLSEnabled:      e.tlsEnabled,
		ConfigVersion:   currentConfigVersion,
		TuningSignature: tuningSignature,
	}); err != nil {
		e.Log.Warn("failed to save etcd state", "error", err)
	}

	return nil
}

func (e *EtcdComponent) ClientEndpoint() string {
	return e.IfRunning(func() string {
		scheme := "http"
		if e.tlsEnabled {
			scheme = "https"
		}
		return fmt.Sprintf("%s://localhost:%d", scheme, e.clientPort)
	})
}

// TLSEnabled returns whether TLS is enabled for client connections.
func (e *EtcdComponent) TLSEnabled() bool {
	return e.tlsEnabled
}

func (e *EtcdComponent) PeerEndpoint() string {
	return e.IfRunning(func() string {
		return fmt.Sprintf("http://localhost:%d", e.peerPort)
	})
}

func (e *EtcdComponent) restartExistingContainer(ctx context.Context, container containerd.Container, config EtcdConfig) error {
	e.SetContainer(container)
	e.config = config

	// Store ports and TLS state for later use
	e.clientPort = config.ClientPort
	e.peerPort = config.PeerPort
	e.tlsEnabled = config.TLS != nil

	// Check if there's already a running task
	task, err := container.Task(ctx, nil)
	if err == nil {
		// Task exists, check its status
		status, err := task.Status(ctx)
		if err != nil {
			e.Log.Warn("failed to get task status", "error", err)
		} else if status.Status == containerd.Running {
			e.Log.Info("etcd container is already running")
			e.SetTask(task)
			if err := e.WaitForReady(ctx, "localhost", config.ClientPort); err != nil {
				e.Log.Warn("etcd readiness check failed, continuing anyway", "error", err)
			}
			e.StartExitMonitor(ctx)
			return nil
		}

		// Task exists but not running, try to start it
		e.Log.Info("starting existing etcd task")
		err = task.Start(ctx)
		if err == nil {
			e.SetTask(task)
			e.Log.Info("etcd server restarted", "container_id", container.ID(), "client_port", config.ClientPort)
			if err := e.WaitForReady(ctx, "localhost", config.ClientPort); err != nil {
				e.Log.Warn("etcd readiness check failed, continuing anyway", "error", err)
			}
			e.StartExitMonitor(ctx)
			return nil
		}

		// Failed to start existing task, delete it and create new one
		e.Log.Warn("failed to start existing task, deleting it", "error", err)
		task.Delete(ctx)
	}

	// Create and start new task with structured logging for JSON output
	e.Log.Info("creating new task for existing container")
	task, err = e.createTask(ctx, container)
	if err != nil {
		return fmt.Errorf("failed to create new task for existing container: %w", err)
	}

	err = task.Start(ctx)
	if err != nil {
		task.Delete(ctx)
		return fmt.Errorf("failed to start new task for existing container: %w", err)
	}

	e.SetTask(task)
	e.Log.Info("etcd server restarted with new task", "container_id", container.ID(), "client_port", config.ClientPort)

	// Wait for etcd to be ready
	if err := e.WaitForReady(ctx, "localhost", config.ClientPort); err != nil {
		e.Log.Warn("etcd readiness check failed, continuing anyway", "error", err)
	}

	// Start monitoring for unexpected exits
	e.StartExitMonitor(ctx)

	return nil
}

func (e *EtcdComponent) createContainer(ctx context.Context, image containerd.Image, config EtcdConfig, tuning etcdTuning) (containerd.Container, error) {
	dataPath := filepath.Join(e.DataPath, "etcd")

	// Determine URL scheme based on TLS config
	scheme := "http"
	if config.TLS != nil {
		scheme = "https"
	}

	e.Log.Info("etcd memory auto-tuning",
		"system_ram_bytes", tuning.SystemRAMBytes,
		"budget_bytes", tuning.BudgetBytes,
		"quota_backend_bytes", tuning.QuotaBackendBytes,
		"gomemlimit_bytes", tuning.GoMemLimitBytes,
		"gogc", tuning.GOGC,
		"auto_compaction_retention", tuning.AutoCompactionReten,
		"snapshot_count", tuning.SnapshotCount,
		"snapshot_catchup_entries", tuning.SnapshotCatchupEntries,
		"max_concurrent_streams", tuning.MaxConcurrentStreams,
		"compaction_batch_limit", tuning.CompactionBatchLimit)

	args := []string{
		"/usr/local/bin/etcd",
		"--name", config.Name,
		"--data-dir", config.DataDir,
		"--listen-client-urls", fmt.Sprintf("%s://0.0.0.0:%d", scheme, config.ClientPort),
		"--listen-client-http-urls", fmt.Sprintf("http://0.0.0.0:%d", config.HTTPClientPort),
		"--advertise-client-urls", fmt.Sprintf("%s://localhost:%d", scheme, config.ClientPort),
		"--listen-peer-urls", fmt.Sprintf("http://0.0.0.0:%d", config.PeerPort),
		"--initial-advertise-peer-urls", fmt.Sprintf("http://localhost:%d", config.PeerPort),
		"--initial-cluster", fmt.Sprintf("%s=http://localhost:%d", config.Name, config.PeerPort),
		"--initial-cluster-state", config.ClusterState,
	}

	args = append(args, tuning.args()...)

	if config.InitialToken != "" {
		args = append(args, "--initial-cluster-token", config.InitialToken)
	}

	// Add TLS flags when configured
	if config.TLS != nil {
		args = append(args,
			"--cert-file", "/certs/server.crt",
			"--key-file", "/certs/server.key",
			"--client-cert-auth",
			"--trusted-ca-file", "/certs/ca.crt",
		)
	}

	// Build mounts list
	mounts := []specs.Mount{
		{
			Destination: config.DataDir,
			Type:        "bind",
			Source:      dataPath,
			Options:     []string{"rbind", "rw"},
		},
	}

	// Add certs mount when TLS is configured
	if config.TLS != nil {
		mounts = append(mounts, specs.Mount{
			Destination: "/certs",
			Type:        "bind",
			Source:      config.TLS.CertsDir,
			Options:     []string{"rbind", "ro"},
		})
	}

	// Create container spec with etcd configuration using host networking
	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithHostNamespace(specs.NetworkNamespace), // Use host network namespace
		oci.WithHostHostsFile,
		oci.WithHostResolvconf,
		oci.WithProcessArgs(args...),
		oci.WithEnv(tuning.envVars()),
		oci.WithMounts(mounts),
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
