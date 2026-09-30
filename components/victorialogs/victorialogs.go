// Package victorialogs provides a component for managing a VictoriaLogs server using containerd.
// VictoriaLogs is a log storage system that uses LogsQL for querying.
package victorialogs

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/opencontainers/runtime-spec/specs-go"
	"miren.dev/runtime/components/base"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/slogout"
)

const (
	victoriaLogsContainerName = "miren-victorialogs"
	defaultHTTPPort           = 9428
	componentSpecLabel        = "dev.miren.victorialogs.spec"
	componentSpecFile         = "victorialogs.spec"
	// Bump this whenever createContainer changes an arg, mount, or other spec
	// detail not represented by desiredSpecFingerprint.
	componentSpecVersion = "1"
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
	*base.BaseComponent

	httpPort int
	config   VictoriaLogsConfig
}

func NewVictoriaLogsComponent(log *slog.Logger, cc *containerd.Client, namespace, dataPath string) *VictoriaLogsComponent {
	bc := base.NewBaseComponent(log, cc, namespace, dataPath, "victorialogs")

	c := &VictoriaLogsComponent{
		BaseComponent: bc,
	}

	// Set up callbacks for the base component
	bc.CreateTask = c.createTask
	bc.GetReadyPort = c.getReadyPort

	return c
}

func (c *VictoriaLogsComponent) createTask(ctx context.Context, container containerd.Container) (containerd.Task, error) {
	return container.NewTask(ctx, slogout.WithLogger(c.Log, "victorialogs"))
}

func (c *VictoriaLogsComponent) getReadyPort() int {
	return c.httpPort
}

func (c *VictoriaLogsComponent) Start(ctx context.Context, config VictoriaLogsConfig) error {
	c.LockOp()
	defer c.UnlockOp()

	if c.IsRunning() {
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
	c.config = config

	specFingerprint := desiredSpecFingerprint(image, dataPath, config)
	// Keep the spec with its data so restoring a snapshot also restores the
	// identity of the store. An older state file outside the data directory
	// cannot be trusted after a manual rollback and is intentionally ignored.
	statePath := filepath.Join(dataPath, componentSpecFile)
	savedSpec, err := os.ReadFile(statePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read victorialogs spec state: %w", err)
	}
	state := strings.Split(strings.TrimSpace(string(savedSpec)), "\n")
	previousSpec := state[0]
	previousImage := ""
	if len(state) >= 2 {
		previousImage = state[1]
	}
	var imageHistory []string
	if len(state) > 2 {
		imageHistory = slices.Clone(state[2:])
	} else if previousImage != "" {
		imageHistory = []string{previousImage}
	}

	// Check if container already exists
	existingContainer, err := c.CC.LoadContainer(ctx, victoriaLogsContainerName)
	if err == nil {
		var restartErr error
		info, matchErr := existingContainer.Info(ctx)
		if matchErr != nil {
			return fmt.Errorf("inspect existing victorialogs container: %w", matchErr)
		}
		if info.Labels[componentSpecLabel] != "" {
			previousSpec = info.Labels[componentSpecLabel]
		}
		previousImage = info.Image
		if previousImage != "" && !slices.Contains(imageHistory, previousImage) {
			imageHistory = append(imageHistory, previousImage)
		}
		matches := info.Labels[componentSpecLabel] == specFingerprint
		if matches {
			c.Log.Info("found matching victorialogs container, attempting restart", "container_id", existingContainer.ID())
			restartErr = c.restartExistingContainer(ctx, existingContainer, config)
			if restartErr == nil {
				return writeSpecState(statePath, specFingerprint, victoriaLogsImage, imageHistory...)
			}
			c.Log.Warn("restart of existing container failed, recreating", "error", restartErr)
		} else {
			c.Log.Warn("victorialogs image or spec changed; on-disk format may migrate irreversibly", "old_image", info.Image, "new_image", victoriaLogsImage, "backup", backupPath(dataPath, previousSpec), "guide", "https://miren.md/victorialogs-upgrade")
		}
		if !matches {
			// Stop before hardlinking. Deleting the container comes after the
			// backup so a failed backup can be retried on the next boot.
			task, taskErr := existingContainer.Task(ctx, nil)
			if taskErr == nil {
				taskErr = c.StopTask(ctx, task)
			}
			if taskErr != nil && !errdefs.IsNotFound(taskErr) {
				return fmt.Errorf("stopping victorialogs before backup: %w", taskErr)
			}
			if err := c.reconcileData(dataPath, previousSpec, specFingerprint, previousImage != victoriaLogsImage, isRollbackImage(imageHistory, previousImage, victoriaLogsImage)); err != nil {
				return fmt.Errorf("preserving victorialogs data before image change: %w", err)
			}
		}
		cleanupErr := c.CleanupExistingContainer(ctx, existingContainer)
		if cleanupErr != nil {
			return errors.Join(restartErr, fmt.Errorf("cleaning up existing victorialogs container: %w", cleanupErr))
		}
		c.ClearRuntimeState()
		if ctx.Err() != nil {
			return errors.Join(restartErr, ctx.Err())
		}
	} else if !errdefs.IsNotFound(err) {
		return fmt.Errorf("load victorialogs container: %w", err)
	} else if previousSpec != specFingerprint {
		entries, readErr := os.ReadDir(dataPath)
		if readErr != nil {
			return readErr
		}
		if len(entries) > 0 {
			c.Log.Warn("victorialogs image or spec changed without a container; on-disk format may migrate irreversibly", "new_image", victoriaLogsImage, "backup", backupPath(dataPath, previousSpec), "guide", "https://miren.md/victorialogs-upgrade")
			if err := c.reconcileData(dataPath, previousSpec, specFingerprint, previousImage != victoriaLogsImage, isRollbackImage(imageHistory, previousImage, victoriaLogsImage)); err != nil {
				return fmt.Errorf("preserving stopped victorialogs data: %w", err)
			}
		}
	}
	// Retain the order read before a restore: the snapshot itself predates later
	// images, but a subsequent roll-forward must not restore their old data.
	if !slices.Contains(imageHistory, victoriaLogsImage) {
		imageHistory = append(imageHistory, victoriaLogsImage)
	}

	// Persist the attempted image before it can touch the data directory. If
	// startup or migration fails, a later rollback must still recognize the
	// pre-upgrade backup even though no new container survives.
	if err := writeSpecState(statePath, specFingerprint, victoriaLogsImage, imageHistory...); err != nil {
		return fmt.Errorf("record victorialogs spec before startup: %w", err)
	}

	c.Log.Info("starting victorialogs with host networking", "http_port", config.HTTPPort)

	// Create container
	container, err := c.createContainer(ctx, image, dataPath, config, specFingerprint)
	if err != nil {
		return fmt.Errorf("failed to create victorialogs container: %w", err)
	}

	c.SetContainer(container)

	// Start container with structured logging
	task, err := c.createTask(ctx, container)
	if err != nil {
		startErr := fmt.Errorf("failed to create victorialogs task: %w", err)
		cleanupErr := c.CleanupExistingContainer(ctx, container)
		if cleanupErr == nil {
			c.ClearRuntimeState()
		}
		return errors.Join(startErr, cleanupErr)
	}

	err = task.Start(ctx)
	if err != nil {
		startErr := fmt.Errorf("failed to start victorialogs task: %w", err)
		cleanupErr := c.CleanupExistingContainer(ctx, container)
		if cleanupErr == nil {
			c.ClearRuntimeState()
		}
		return errors.Join(startErr, cleanupErr)
	}

	// Wait for VictoriaLogs to be ready
	if err := c.WaitForReady(ctx, "127.0.0.1", config.HTTPPort); err != nil {
		cleanupErr := c.CleanupExistingContainer(ctx, container)
		if cleanupErr == nil {
			c.ClearRuntimeState()
		}
		return errors.Join(err, cleanupErr)
	}

	c.SetTask(task)
	c.Log.Info("victorialogs server started", "container_id", container.ID(), "http_port", config.HTTPPort)

	// Start monitoring for unexpected exits
	c.StartExitMonitor(ctx)

	return writeSpecState(statePath, specFingerprint, victoriaLogsImage, imageHistory...)
}

func (c *VictoriaLogsComponent) HTTPEndpoint() string {
	return c.IfRunning(func() string {
		// Names the bind address literally rather than "localhost", which on a
		// dual-stack host resolves to ::1 first and would spend a failed dial
		// on every new connection to a v4-only listener.
		return fmt.Sprintf("127.0.0.1:%d", c.httpPort)
	})
}

func (c *VictoriaLogsComponent) restartExistingContainer(ctx context.Context, container containerd.Container, config VictoriaLogsConfig) error {
	c.SetContainer(container)
	c.httpPort = config.HTTPPort
	c.config = config

	task, err := container.Task(ctx, slogout.AttachLogger(c.Log, "victorialogs"))
	if err == nil {
		status, err := task.Status(ctx)
		if err != nil {
			c.Log.Warn("failed to get task status", "error", err)
		} else if status.Status == containerd.Running {
			c.Log.Info("victorialogs container is already running")
			c.SetTask(task)
			if err := c.WaitForReady(ctx, "127.0.0.1", config.HTTPPort); err != nil {
				return err
			}
			c.StartExitMonitor(ctx)
			return nil
		}

		c.Log.Info("starting existing victorialogs task")
		err = task.Start(ctx)
		if err == nil {
			c.SetTask(task)
			c.Log.Info("victorialogs server restarted", "container_id", container.ID(), "http_port", config.HTTPPort)
			if err := c.WaitForReady(ctx, "127.0.0.1", config.HTTPPort); err != nil {
				return err
			}
			c.StartExitMonitor(ctx)
			return nil
		}

		c.Log.Warn("failed to start existing task, deleting it", "error", err)
		task.Delete(ctx)
	}

	c.Log.Info("creating new task for existing container")
	task, err = c.ReplaceTask(ctx, container, c.createTask)
	if err != nil {
		return fmt.Errorf("failed to create new task for existing container: %w", err)
	}

	err = task.Start(ctx)
	if err != nil {
		task.Delete(ctx)
		return fmt.Errorf("failed to start new task for existing container: %w", err)
	}

	if err := c.WaitForReady(ctx, "127.0.0.1", config.HTTPPort); err != nil {
		task.Delete(ctx)
		return err
	}

	c.SetTask(task)
	c.Log.Info("victorialogs server restarted with new task", "container_id", container.ID(), "http_port", config.HTTPPort)

	// Start monitoring for unexpected exits
	c.StartExitMonitor(ctx)

	return nil
}

func desiredSpecFingerprint(image containerd.Image, dataPath string, config VictoriaLogsConfig) string {
	parts := []string{
		componentSpecVersion,
		image.Target().Digest.String(),
		filepath.Clean(dataPath),
		fmt.Sprintf("%d", config.HTTPPort),
		config.RetentionPeriod,
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))
}

var specFingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func writeSpecState(path, fingerprint, image string, history ...string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".victorialogs-spec-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if len(history) == 0 {
		history = []string{image}
	}
	if _, err := tmp.WriteString(fingerprint + "\n" + image + "\n" + strings.Join(history, "\n") + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func isRollbackImage(history []string, current, desired string) bool {
	currentIndex := slices.Index(history, current)
	desiredIndex := slices.Index(history, desired)
	return currentIndex >= 0 && desiredIndex >= 0 && desiredIndex < currentIndex
}

func (c *VictoriaLogsComponent) reconcileData(dataPath, previousSpec, desiredSpec string, imageChanged, rollback bool) error {
	var restore string
	if rollback {
		var err error
		restore, err = restoreBackup(dataPath, desiredSpec, victoriaLogsImage)
		if err != nil {
			return err
		}
	}
	if rollback && restore != "" {
		c.Log.Warn("restoring prior victorialogs data for requested image; later logs will be unavailable", "backup", restore)
	} else {
		if rollback {
			c.Log.Warn("no matching victorialogs snapshot for requested image; older image may not open current data", "image", victoriaLogsImage)
		}
		c.Log.Warn("creating stopped victorialogs data backup", "backup", backupPath(dataPath, previousSpec))
	}
	return preserveOrRestoreData(dataPath, previousSpec, restore, imageChanged)
}

func backupPath(dataPath, fingerprint string) string {
	if !specFingerprintPattern.MatchString(fingerprint) {
		fingerprint = "legacy"
	}
	return dataPath + ".backup-" + fingerprint
}

func restoreBackup(dataPath, desiredSpec, desiredImage string) (string, error) {
	exact := backupPath(dataPath, desiredSpec)
	if _, err := os.Stat(exact); err == nil {
		return exact, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	entries, err := os.ReadDir(filepath.Dir(dataPath))
	if err != nil {
		return "", err
	}
	prefix := filepath.Base(dataPath) + ".backup-"
	var newest string
	var newestTime time.Time
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !specFingerprintPattern.MatchString(strings.TrimPrefix(entry.Name(), prefix)) {
			continue
		}
		candidate := filepath.Join(filepath.Dir(dataPath), entry.Name())
		state, err := os.ReadFile(filepath.Join(candidate, componentSpecFile))
		if os.IsNotExist(err) {
			continue // A pre-feature snapshot has no verifiable image identity.
		}
		if err != nil {
			return "", err
		}
		parts := strings.Split(strings.TrimSpace(string(state)), "\n")
		if len(parts) < 2 || parts[1] != desiredImage {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if newest == "" || info.ModTime().After(newestTime) {
			newest, newestTime = candidate, info.ModTime()
		}
	}
	return newest, nil
}

// preserveOrRestoreData runs only after the old task has stopped. VictoriaLogs
// creates and unlinks immutable part files, so hard links preserve the stopped
// state without copying the full store. A failure must not launch a new image
// against an unprotected directory.
func preserveOrRestoreData(dataPath, previousSpec, restore string, imageChanged bool) error {
	if imageChanged && restore != "" {
		// A matching backup is a pre-upgrade snapshot. Move the upgraded data
		// aside, leaving it available for diagnosis or a forward retry.
		quarantine := dataPath + ".replaced-" + previousSpec
		if !specFingerprintPattern.MatchString(previousSpec) {
			quarantine = dataPath + ".replaced-legacy"
		}
		if _, err := os.Stat(quarantine); err == nil {
			quarantine = fmt.Sprintf("%s-%d", quarantine, time.Now().UnixNano())
		} else if !os.IsNotExist(err) {
			return err
		}
		if _, err := os.Stat(quarantine); err == nil {
			return fmt.Errorf("rollback directory already exists: %s", quarantine)
		} else if !os.IsNotExist(err) {
			return err
		}
		staged, err := hardlinkData(restore)
		if err != nil {
			return fmt.Errorf("restore victorialogs snapshot (backup must share a filesystem with the data directory): %w", err)
		}
		defer os.RemoveAll(staged)
		if err := os.Rename(dataPath, quarantine); err != nil {
			return err
		}
		if err := os.Rename(staged, dataPath); err != nil {
			return errors.Join(err, os.Rename(quarantine, dataPath))
		}
		return nil
	}

	backup := backupPath(dataPath, previousSpec)
	_, backupErr := os.Stat(backup)
	if backupErr != nil && !os.IsNotExist(backupErr) {
		return backupErr
	}
	if backupErr == nil && !imageChanged {
		return nil
	}
	tmp, err := hardlinkData(dataPath)
	if err != nil {
		return fmt.Errorf("snapshot victorialogs data (backup must share a filesystem with the data directory): %w", err)
	}
	defer os.RemoveAll(tmp)
	if backupErr == nil {
		// The same spec can be revisited after logs have changed. Keep the
		// older snapshot, but use current stopped data for this image change.
		archived := fmt.Sprintf("%s.superseded-%d", backup, time.Now().UnixNano())
		if err := os.Rename(backup, archived); err != nil {
			return err
		}
		if err := os.Rename(tmp, backup); err != nil {
			return errors.Join(err, os.Rename(archived, backup))
		}
		return nil
	}
	return os.Rename(tmp, backup)
}

func hardlinkData(source string) (string, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("victorialogs data path is not a real directory: %s", source)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(source), ".victorialogs-backup-")
	if err != nil {
		return "", err
	}
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dest := filepath.Join(tmp, rel)
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return os.Mkdir(dest, info.Mode().Perm())
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected non-regular victorialogs data file: %s", path)
		}
		return os.Link(path, dest)
	}); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

func (c *VictoriaLogsComponent) createContainer(ctx context.Context, image containerd.Image, dataPath string, config VictoriaLogsConfig, specFingerprint string) (containerd.Container, error) {
	// Loopback only. VictoriaLogs authenticates nobody, so whatever can reach
	// this port can read and write the cluster's logs, which include anything
	// an app prints. Nothing off-host needs to reach it: the coordinator's own
	// reads and writes are local, and distributed runners ship through the
	// coordinator's authenticated listener rather than dialing here
	// (MIR-1483). Binding the wildcard would leave a firewall rule as the only
	// thing standing in front of it, which is a control that can be flushed,
	// reordered, or simply absent on a host nobody configured.
	//
	// -enableTCP6 below does not reopen this. It governs whether IPv6 may be
	// used at all; the listener is whatever this address names.
	listenAddr := fmt.Sprintf("127.0.0.1:%d", config.HTTPPort)

	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
		oci.WithHostNamespace(specs.NetworkNamespace),
		oci.WithProcessArgs(
			"/victoria-logs-prod",
			"-storageDataPath=/victoria-logs-data",
			"-retentionPeriod="+config.RetentionPeriod,
			"-httpListenAddr="+listenAddr,
			"-enableTCP6",
		),
		oci.WithHostHostsFile,
		oci.WithHostResolvconf,
		containerdx.WithRlimitNOFILE(65536),

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
		containerd.WithContainerLabels(map[string]string{componentSpecLabel: specFingerprint}),
		containerd.WithNewSnapshot(victoriaLogsContainerName+"-snapshot", image),
		containerd.WithNewSpec(opts...),
	)
	if err != nil {
		return nil, err
	}

	return container, nil
}
