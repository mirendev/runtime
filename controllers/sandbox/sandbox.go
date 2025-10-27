package sandbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd/namespaces"
	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/containerd/errdefs"
	"github.com/mr-tron/base58"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/network"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/imagerefs"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/netutil"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
)

const (
	// defaultSandboxOOMAdj is default omm adj for sandbox container. (kubernetes#47938).
	defaultSandboxOOMAdj = -998
)

var sandboxImage = imagerefs.Pause

type containerPorts struct {
	Ports []observability.BoundPort
}

type SandboxController struct {
	Log *slog.Logger
	CC  *containerd.Client

	EAC *entityserver_v1alpha.EntityAccessClient

	Namespace string `asm:"namespace"`

	NetServ *network.ServiceManager

	Bridge string `asm:"bridge-iface"`
	Subnet *netdb.Subnet

	DataPath string `asm:"data-path"`
	Tempdir  string `asm:"tempdir"`

	LogsMaintainer *observability.LogsMaintainer

	StatusMon *observability.StatusMonitor

	Resolver   netresolve.Resolver
	Clickhouse *sql.DB `asm:"clickhouse"`
	Metrics    *Metrics

	topCtx context.Context
	cancel func()

	mu       sync.Mutex
	monitors int
	cond     *sync.Cond

	running sync.WaitGroup

	portMu      sync.Mutex
	portCond    *sync.Cond
	portMap     map[string]*containerPorts
	portMonitor *PortMonitor
}

func (c *SandboxController) Populated() error {
	c.Log = c.Log.With("module", "sandbox")
	return nil
}

func (c *SandboxController) SetPortStatus(id string, port observability.BoundPort, status observability.PortStatus) {
	c.portMu.Lock()
	defer c.portMu.Unlock()

	ports, ok := c.portMap[id]
	if !ok {
		ports = &containerPorts{}
		c.portMap[id] = ports
	}

	c.Log.Debug("setting port status", "id", id, "port", port, "status", status)

	switch status {
	case observability.PortStatusBound:
		if !slices.Contains(ports.Ports, port) {
			ports.Ports = append(ports.Ports, port)
		}
	case observability.PortStatusUnbound:
		ports.Ports = slices.DeleteFunc(ports.Ports, func(p observability.BoundPort) bool {
			return p == port
		})
	}

	c.portCond.Broadcast()
}

func (c *SandboxController) waitForPort(ctx context.Context, id string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Create a channel to signal when port is ready
	done := make(chan struct{})
	cancelled := make(chan struct{})

	go func() {
		c.portMu.Lock()
		defer c.portMu.Unlock()

		for {
			select {
			case <-cancelled:
				return
			default:
			}

			ports, ok := c.portMap[id]
			if !ok {
				ports = &containerPorts{}
				c.portMap[id] = ports
			}

			for _, p := range ports.Ports {
				if p.Port == port {
					close(done)
					return
				}
			}

			c.portCond.Wait()
		}
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		close(cancelled)
		c.portCond.Broadcast() // Wake up the waiting goroutine
		return fmt.Errorf("context cancelled while waiting for port %d: %w", port, ctx.Err())
	case <-time.After(time.Until(deadline)):
		close(cancelled)
		c.portCond.Broadcast() // Wake up the waiting goroutine
		return fmt.Errorf("timeout waiting for port %d to be bound after %v", port, timeout)
	}
}

// mapLegacyProtocol converts legacy PortProtocol values to SandboxSpecContainerPortProtocol
func mapLegacyProtocol(legacy compute.PortProtocol) compute.SandboxSpecContainerPortProtocol {
	switch legacy {
	case compute.TCP, "tcp":
		return compute.SandboxSpecContainerPortTCP
	case compute.UDP, "udp":
		return compute.SandboxSpecContainerPortUDP
	default:
		// Default to TCP for unknown protocols
		return compute.SandboxSpecContainerPortTCP
	}
}

// migrateLegacySandboxes converts sandboxes using legacy top-level fields to use Spec field
func (c *SandboxController) migrateLegacySandboxes(ctx context.Context) error {
	c.Log.Info("migrating legacy sandboxes to Spec format")

	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes for migration: %w", err)
	}

	migratedCount := 0
	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())

		// Skip if already has Spec populated (check if it has containers)
		if len(sb.Spec.Container) > 0 {
			continue
		}

		// Skip if no legacy fields to migrate
		if len(sb.Container) == 0 {
			continue
		}

		c.Log.Info("migrating sandbox to Spec format", "sandbox", sb.ID)

		// Build Spec from legacy fields
		sb.Spec.Version = sb.Version
		sb.Spec.LogEntity = sb.LogEntity
		sb.Spec.LogAttribute = sb.LogAttribute
		sb.Spec.HostNetwork = sb.HostNetwork

		// Convert Container to SandboxSpecContainer
		for _, legacyCont := range sb.Container {
			specCont := compute.SandboxSpecContainer{
				Name:       legacyCont.Name,
				Image:      legacyCont.Image,
				Privileged: legacyCont.Privileged,
				Command:    legacyCont.Command,
				Directory:  legacyCont.Directory,
				Env:        legacyCont.Env,
				OomScore:   legacyCont.OomScore,
			}

			// Convert ports
			for _, p := range legacyCont.Port {
				specCont.Port = append(specCont.Port, compute.SandboxSpecContainerPort{
					Port:     p.Port,
					Name:     p.Name,
					Protocol: mapLegacyProtocol(p.Protocol),
					Type:     p.Type,
					NodePort: p.NodePort,
				})
			}

			// Convert mounts
			for _, m := range legacyCont.Mount {
				specCont.Mount = append(specCont.Mount, compute.SandboxSpecContainerMount(m))
			}

			// Convert config files
			for _, cf := range legacyCont.ConfigFile {
				specCont.ConfigFile = append(specCont.ConfigFile, compute.SandboxSpecContainerConfigFile(cf))
			}

			sb.Spec.Container = append(sb.Spec.Container, specCont)
		}

		// Convert routes
		for _, r := range sb.Route {
			sb.Spec.Route = append(sb.Spec.Route, compute.SandboxSpecRoute(r))
		}

		// Convert volumes
		for _, v := range sb.Volume {
			sb.Spec.Volume = append(sb.Spec.Volume, compute.SandboxSpecVolume(v))
		}

		// Convert static hosts
		for _, sh := range sb.StaticHost {
			sb.Spec.StaticHost = append(sb.Spec.StaticHost, compute.SandboxSpecStaticHost(sh))
		}

		// Clear legacy fields - Spec is now the single source of truth
		sb.Container = nil
		sb.Route = nil
		sb.Volume = nil
		sb.StaticHost = nil
		sb.Version = ""
		sb.LogEntity = ""
		sb.LogAttribute = nil
		sb.HostNetwork = false

		// Update the entity with the populated Spec
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(sb.ID.String())
		rpcE.SetAttrs(entity.New(sb.Encode).Attrs())

		_, err := c.EAC.Put(ctx, &rpcE)
		if err != nil {
			c.Log.Error("failed to migrate sandbox", "sandbox", sb.ID, "err", err)
			continue
		}

		migratedCount++
		c.Log.Info("migrated sandbox to Spec format", "sandbox", sb.ID)
	}

	c.Log.Info("legacy sandbox migration complete", "migrated", migratedCount)
	return nil
}

// reconcileSandboxesOnBoot checks all Running sandboxes and marks unhealthy ones as DEAD
// This is called during controller initialization to clean up after containerd restarts
func (c *SandboxController) reconcileSandboxesOnBoot(ctx context.Context) error {
	c.Log.Info("reconciling sandboxes on boot")

	// Create a context with timeout for the entire reconciliation
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// First, migrate any legacy sandboxes to use Spec field
	if err := c.migrateLegacySandboxes(ctx); err != nil {
		c.Log.Error("failed to migrate legacy sandboxes", "err", err)
		// Continue with reconciliation even if migration fails
	}

	// List all sandboxes marked as RUNNING
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	var unhealthySandboxes []entity.Id
	runningCount := 0
	reattachedCount := 0

	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())

		// Only check sandboxes that think they're running
		if sb.Status != compute.RUNNING {
			continue
		}
		runningCount++

		// Reattach logs to pause container
		pauseID := c.pauseContainerId(sb.ID)
		if err := c.reattachLogs(ctx, &sb, pauseID, ""); err != nil {
			c.Log.Warn("failed to reattach logs to pause container",
				"sandbox_id", sb.ID,
				"pause_container_id", pauseID,
				"error", err)
			unhealthySandboxes = append(unhealthySandboxes, sb.ID)
			continue
		}

		// Check pause container health
		if !c.isContainerHealthy(ctx, pauseID) {
			c.Log.Warn("found unhealthy sandbox during boot reconciliation",
				"sandbox_id", sb.ID,
				"pause_container_id", pauseID)
			unhealthySandboxes = append(unhealthySandboxes, sb.ID)
			continue
		}

		// Reattach logs and check subcontainers health
		allHealthy := true
		for _, container := range sb.Spec.Container {
			containerID := fmt.Sprintf("%s-%s", c.containerPrefix(sb.ID), container.Name)

			// Reattach logs for this subcontainer
			if err := c.reattachLogs(ctx, &sb, containerID, container.Name); err != nil {
				c.Log.Warn("failed to reattach logs to subcontainer",
					"sandbox_id", sb.ID,
					"container_name", container.Name,
					"container_id", containerID,
					"error", err)
				allHealthy = false
				break
			}

			if !c.isContainerHealthy(ctx, containerID) {
				c.Log.Warn("found unhealthy subcontainer during boot reconciliation",
					"sandbox_id", sb.ID,
					"container_name", container.Name,
					"container_id", containerID)
				allHealthy = false
				break
			}
		}

		if !allHealthy {
			unhealthySandboxes = append(unhealthySandboxes, sb.ID)
		} else {
			reattachedCount++
		}
	}

	// Mark unhealthy sandboxes as DEAD and clean them up
	for _, id := range unhealthySandboxes {
		c.Log.Info("marking unhealthy sandbox as DEAD and cleaning up", "id", id)

		// Try to clean up the sandbox
		err := c.Delete(ctx, id)
		if err != nil {
			c.Log.Error("failed to cleanup unhealthy sandbox", "id", id, "err", err)
			// Continue with other sandboxes even if one fails
		}
	}

	c.Log.Info("boot reconciliation complete",
		"total_running_sandboxes", runningCount,
		"reattached_sandboxes", reattachedCount,
		"unhealthy_sandboxes", len(unhealthySandboxes))

	return nil
}

// cleanupOrphanedContainers removes containers not associated with Running sandboxes
func (c *SandboxController) cleanupOrphanedContainers(ctx context.Context) error {
	c.Log.Info("cleaning up orphaned containers")

	// Create a context with timeout for the entire cleanup
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	// List all containers in the namespace
	containerList, err := c.CC.Containers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Build a set of valid container IDs from Running sandboxes
	validContainers := make(map[string]bool)

	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes for orphan check: %w", err)
	}

	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())

		// Only track containers for Running sandboxes
		if sb.Status == compute.RUNNING {
			// Add pause container
			validContainers[c.pauseContainerId(sb.ID)] = true

			// Add subcontainers
			for _, container := range sb.Spec.Container {
				validContainers[fmt.Sprintf("%s-%s", c.containerPrefix(sb.ID), container.Name)] = true
			}
		}
	}

	// Clean up orphaned containers
	orphanCount := 0
	for _, container := range containerList {
		containerID := container.ID()

		// Skip if this is a valid container
		if validContainers[containerID] {
			continue
		}

		// Check labels to see if this might be a special container we shouldn't touch
		labels, err := container.Labels(ctx)
		if err != nil {
			c.Log.Error("failed to get container labels", "id", containerID, "err", err)
			continue
		}

		// Skip if not a sandbox container (check for our labels)
		if _, ok := labels[sandboxEntityLabel]; !ok {
			c.Log.Debug("skipping non-sandbox container", "id", containerID)
			continue
		}

		c.Log.Info("found orphaned container, cleaning up", "id", containerID)
		orphanCount++

		// Try to delete any task first
		task, err := container.Task(ctx, nil)
		if err == nil && task != nil {
			_, err = task.Delete(ctx, containerd.WithProcessKill)
			if err != nil {
				c.Log.Error("failed to delete orphaned task", "id", containerID, "err", err)
			}
		}

		// Delete the container
		err = container.Delete(ctx, containerd.WithSnapshotCleanup)
		if err != nil {
			c.Log.Error("failed to delete orphaned container", "id", containerID, "err", err)
		}
	}

	c.Log.Info("orphaned container cleanup complete",
		"total_containers", len(containerList),
		"orphaned_containers", orphanCount)

	return nil
}

func (c *SandboxController) Init(ctx context.Context) error {
	c.portCond = sync.NewCond(&c.portMu)
	c.portMap = make(map[string]*containerPorts)

	// Initialize port monitor
	c.portMonitor = NewPortMonitor(c.Log, c)

	err := c.LogsMaintainer.Setup(ctx)
	if err != nil {
		return err
	}

	c.topCtx, c.cancel = context.WithCancel(ctx)

	c.cond = sync.NewCond(&c.mu)

	bc := &network.BridgeConfig{
		Name:      c.Bridge,
		Addresses: []netip.Prefix{c.Subnet.Router()},
	}

	link, err := network.SetupBridge(bc)
	if err != nil {
		return err
	}

	ep := &network.EndpointConfig{
		Bridge: bc,
	}

	err = network.ConfigureGW(link, ep)
	if err != nil {
		return err
	}

	err = network.MasqueradeEndpoint(ep)
	if err != nil {
		return err
	}

	err = c.NetServ.SetupDNS(bc)
	if err != nil {
		return err
	}

	// Reconcile sandboxes after containerd restart
	// This must happen after bridge setup but before starting normal operations
	err = c.reconcileSandboxesOnBoot(ctx)
	if err != nil {
		// Log error but don't fail Init - we want the controller to start
		// and handle new requests even if some cleanup failed
		c.Log.Error("failed to reconcile sandboxes on boot", "err", err)
	}

	// Clean up any orphaned containers
	err = c.cleanupOrphanedContainers(ctx)
	if err != nil {
		// Log error but don't fail Init
		c.Log.Error("failed to cleanup orphaned containers on boot", "err", err)
	}

	go c.Metrics.Monitor(c.topCtx)

	return nil
}

func (c *SandboxController) Close() error {
	c.cancel()

	c.mu.Lock()
	for c.monitors > 0 {
		c.cond.Wait()
	}
	c.mu.Unlock()

	var err error

	if c.portMonitor != nil {
		err = c.portMonitor.Close()
	}

	c.running.Wait()

	return err
}

const (
	sandboxVersionLabel   = "runtime.computer/sandbox-version"
	sandboxEntityLabel    = "runtime.computer/entity-id"
	sandboxVerEntityLabel = "runtime.computer/version-entity"
	sandboxKindLabel      = "runtime.computer/container-kind"
)

const (
	notFound = iota
	same
	differentVersion
	unhealthy // container exists but task is missing or dead
)

// canUpdateInPlace checks if the sandbox can be updated in place without destroying it.
func (c *SandboxController) canUpdateInPlace(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) (bool, *compute.Sandbox, error) {
	// We support the ability to update a subnet of elements of the sandbox while running.
	// For everything else, we destroy it and rebuild it fully with Create.

	oldMeta, err := c.readEntity(ctx, sb.ID)
	if err != nil {
		c.Log.Error("failed to read existing entity, trying with new definition", "id", sb.ID, "err", err)
		oldMeta = meta
	}

	var oldSb compute.Sandbox
	oldSb.Decode(oldMeta)

	// TODO: handle adding a new container without destroying the sandbox first.
	if len(sb.Spec.Container) != len(oldSb.Spec.Container) {
		return false, nil, nil
	}

	for i, container := range sb.Spec.Container {
		if container.Name != oldSb.Spec.Container[i].Name {
			return false, nil, nil
		}

		if container.Image != oldSb.Spec.Container[i].Image {
			return false, nil, nil
		}

		if container.Command != oldSb.Spec.Container[i].Command {
			return false, nil, nil
		}

		if !slices.Equal(container.Env, oldSb.Spec.Container[i].Env) {
			return false, nil, nil
		}

		if !slices.Equal(container.Mount, oldSb.Spec.Container[i].Mount) {
			return false, nil, nil
		}

		if container.Privileged != oldSb.Spec.Container[i].Privileged {
			return false, nil, nil
		}

		if container.OomScore != oldSb.Spec.Container[i].OomScore {
			return false, nil, nil
		}
		if !slices.Equal(container.Port, oldSb.Spec.Container[i].Port) {
			return false, nil, nil
		}
	}

	return true, &oldSb, nil
}

func (c *SandboxController) containerPrefix(id entity.Id) string {
	cid := id.String()
	cid = strings.TrimPrefix(cid, "sandbox/")
	return "sandbox." + cid
}

func (c *SandboxController) pauseContainerId(id entity.Id) string {
	return c.containerPrefix(id) + "_pause"
}

func (c *SandboxController) checkSandbox(ctx context.Context, co *compute.Sandbox, meta *entity.Meta) (int, error) {
	c.Log.Debug("checking for existing sandbox", "id", co.ID)

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	cont, err := c.CC.LoadContainer(ctx, c.pauseContainerId(co.ID))
	if err != nil {
		if errdefs.IsNotFound(err) {
			return notFound, nil
		}

		return 0, err
	}

	labels, err := cont.Labels(ctx)
	if err != nil {
		return notFound, err
	}

	if _, ok := labels[sandboxVersionLabel]; !ok {
		return differentVersion, nil
	}

	if labels[sandboxVersionLabel] != fmt.Sprint(meta.Revision) {
		return differentVersion, nil
	}

	// Check if the pause container has a healthy task
	pauseID := c.pauseContainerId(co.ID)
	if !c.isContainerHealthy(ctx, pauseID) {
		c.Log.Warn("sandbox container exists but task is unhealthy", "id", co.ID, "pause_id", pauseID)
		return unhealthy, nil
	}

	// Check subcontainers health (from Spec)
	for _, container := range co.Spec.Container {
		containerID := fmt.Sprintf("%s-%s", c.containerPrefix(co.ID), container.Name)
		if !c.isContainerHealthy(ctx, containerID) {
			c.Log.Warn("sandbox subcontainer exists but task is unhealthy",
				"sandbox_id", co.ID,
				"container_name", container.Name,
				"container_id", containerID)
			return unhealthy, nil
		}
	}

	return same, nil
}

// isContainerHealthy checks if a container has a running task
// Returns true if the container and its task are healthy, false otherwise
func (c *SandboxController) isContainerHealthy(ctx context.Context, containerID string) bool {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	container, err := c.CC.LoadContainer(ctx, containerID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			c.Log.Debug("container not found when checking health", "id", containerID)
		} else {
			c.Log.Error("failed to load container when checking health", "id", containerID, "err", err)
		}
		return false
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			c.Log.Debug("task not found for container", "id", containerID)
		} else {
			c.Log.Error("failed to get task for container", "id", containerID, "err", err)
		}
		return false
	}

	if task == nil {
		c.Log.Debug("task is nil for container", "id", containerID)
		return false
	}

	status, err := task.Status(ctx)
	if err != nil {
		c.Log.Error("failed to get task status", "id", containerID, "err", err)
		return false
	}

	// Check if task is in a healthy state
	// Only Running and Created (starting) are considered healthy
	// Everything else (Stopped, Paused, Pausing, Unknown) is unhealthy
	switch status.Status {
	case containerd.Running:
		// Definitely healthy - task is actively running
		return true
	case containerd.Created:
		// Task created but not yet started - might still be starting up
		c.Log.Debug("task in created state, considering healthy", "id", containerID)
		return true
	case containerd.Stopped:
		// Task has stopped
		c.Log.Debug("task stopped, marking unhealthy", "id", containerID)
		return false
	case containerd.Paused, containerd.Pausing:
		// We don't expect paused sandboxes in normal operation
		c.Log.Debug("task in paused/pausing state, marking unhealthy", "id", containerID, "status", status.Status)
		return false
	default:
		// Unknown or any other status is unhealthy
		c.Log.Debug("task in unknown/unhealthy state", "id", containerID, "status", status.Status)
		return false
	}
}

// reattachLogs reattaches log consumers to a container's task after controller restart.
// This is critical to prevent stdout/stderr buffers from filling up and blocking the process.
// containerName should be empty string for the pause container, or the subcontainer name otherwise.
func (c *SandboxController) reattachLogs(ctx context.Context, sb *compute.Sandbox, containerID string, containerName string) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	container, err := c.CC.LoadContainer(ctx, containerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	// Create log consumer for this container
	sl := c.logConsumer(sb, containerName)

	// Reattach to the existing task with our log consumer
	// This drains stdout/stderr and prevents the process from blocking on writes
	task, err := container.Task(ctx, cio.NewAttach(cio.WithStreams(nil, sl, sl.Stderr())))
	if err != nil {
		return fmt.Errorf("failed to attach to task: %w", err)
	}

	if task == nil {
		return fmt.Errorf("task is nil after attach")
	}

	c.Log.Info("reattached logs to container",
		"sandbox_id", sb.ID,
		"container_id", containerID,
		"container_name", containerName)

	return nil
}

func (c *SandboxController) saveEntity(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) error {
	path := c.sandboxPath(sb, "entity.cbor")

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create entity file: %w", err)
	}

	defer f.Close()

	data, err := entity.Encode(meta)
	if err != nil {
		return fmt.Errorf("failed to encode entity: %w", err)
	}

	_, err = f.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write entity file: %w", err)
	}

	return nil
}

func (c *SandboxController) readEntity(ctx context.Context, id entity.Id) (*entity.Meta, error) {
	path := filepath.Join(c.Tempdir, "containerd", id.PathSafe(), "entity.cbor")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("entity file not found: %w", err)
		}

		return nil, fmt.Errorf("failed to open entity file: %w", err)
	}

	// Use MigrateMetaFromBytes for automatic migration from old format
	meta, err := entity.MigrateMetaFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode entity file: %w", err)
	}

	return meta, nil
}

func (c *SandboxController) updateSandbox(ctx context.Context, sb *compute.Sandbox, meta *entity.Meta) error {
	// We support the ability to update a subnet of elements of the sandbox while running.
	// For everything else, we destroy it and rebuild it fully with Create.

	canUpdate, oldSb, err := c.canUpdateInPlace(ctx, sb, meta)
	if err != nil {
		c.Log.Error("failed to check if sandbox can be updated in place", "err", err)
	} else if canUpdate {

		cont, err := c.CC.LoadContainer(ctx, c.pauseContainerId(sb.ID))
		if err != nil {
			return fmt.Errorf("failed to load existing sandbox: %w", err)
		}

		if !slices.Equal(oldSb.Labels, sb.Labels) {
			labels, err := cont.Labels(ctx)
			if err != nil {
				return fmt.Errorf("failed to get container labels: %w", err)
			}

			for _, lbl := range oldSb.Labels {
				k, _, ok := strings.Cut(lbl, "=")
				if ok {
					delete(labels, strings.TrimSpace(k))
				}
			}

			for _, lbl := range sb.Labels {
				k, v, ok := strings.Cut(lbl, "=")
				if ok {
					labels[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}

			labels[sandboxVersionLabel] = strconv.FormatInt(meta.Revision, 10)

			_, err = cont.SetLabels(ctx, labels)
			if err != nil {
				return err
			}
		}

		return c.saveEntity(ctx, sb, meta)
	}

	c.Log.Debug("destroying existing sandbox to recreate it")

	err = c.Delete(ctx, meta.Id())
	if err != nil {
		return fmt.Errorf("failed to delete existing sandbox: %w", err)
	}

	return c.createSandbox(ctx, sb, meta)
}

func (c *SandboxController) Create(ctx context.Context, co *compute.Sandbox, meta *entity.Meta) error {
	c.Log.Info("considering sandbox create or update", "id", co.ID, "status", co.Status)

	switch co.Status {
	case compute.DEAD:
		return nil
	case compute.STOPPED:
		c.Log.Debug("sandbox is stopped, verifying it is no longer running")
		return c.stopSandbox(ctx, co)
	case "", compute.PENDING, compute.RUNNING:
		searchRes, err := c.checkSandbox(ctx, co, meta)
		if err != nil {
			c.Log.Error("error checking sandbox, proceeding with create", "err", err)
		} else {
			switch searchRes {
			case same:
				c.Log.Debug("sandbox already exists, skipping create")
				return nil
			case differentVersion:
				return c.updateSandbox(ctx, co, meta)
			case unhealthy:
				c.Log.Info("sandbox container exists but is unhealthy, cleaning up and recreating", "id", co.ID)
				// Clean up the unhealthy sandbox
				err := c.Delete(ctx, co.ID)
				if err != nil {
					c.Log.Error("failed to cleanup unhealthy sandbox", "id", co.ID, "err", err)
					return fmt.Errorf("failed to cleanup unhealthy sandbox: %w", err)
				}
				// Fall through to create a new sandbox
			}
		}

		return c.createSandbox(ctx, co, meta)
	default:
		c.Log.Warn("ignoring sandbox status", "status", co.Status)
		return nil
	}
}

func (c *SandboxController) createSandbox(ctx context.Context, co *compute.Sandbox, meta *entity.Meta) error {
	c.Log.Debug("creating sandbox", "id", co.ID)

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	ep, err := c.allocateNetwork(ctx, co)
	if err != nil {
		return fmt.Errorf("failed to allocate network: %w", err)
	}

	opts, err := c.buildSpec(ctx, co, ep, meta)
	if err != nil {
		c.deallocateNetwork(ctx, ep)
		return fmt.Errorf("failed to build container spec: %w", err)
	}

	err = c.configureVolumes(ctx, co)
	if err != nil {
		c.deallocateNetwork(ctx, ep)
		return fmt.Errorf("failed to configure volumes: %w", err)
	}

	cid := c.pauseContainerId(co.ID)

	container, err := c.CC.NewContainer(ctx, cid, opts...)
	if err != nil {
		c.deallocateNetwork(ctx, ep)
		return errors.Wrapf(err, "failed to create container %s", co.ID)
	}

	defer func() {
		if err != nil {
			c.Log.Error("failed to create sandbox, cleaning up", "id", co.ID, "err", err)

			// Be sure we have at least 60 seconds to do this action.
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			// Clean up network resources if they were allocated
			c.deallocateNetwork(ctx, ep)

			// Clean up any subcontainers that might have been created
			c.destroySubContainers(ctx, co)

			// Clean up the pause container using the common cleanup function
			c.cleanupContainer(ctx, container)

			// Update sandbox status to DEAD in entity store
			co.Status = compute.DEAD
			meta.Update(co.Encode())
			c.Log.Info("marked sandbox as DEAD due to boot failure", "id", co.ID)
		}
	}()

	task, err := c.bootInitialTask(ctx, co, ep, container)
	if err != nil {
		return err
	}

	rootSpec, err := container.Spec(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container spec: %w", err)
	}

	cgroups := map[string]string{
		"": rootSpec.Linux.CgroupsPath,
	}

	waitPorts, err := c.bootContainers(ctx, co, ep, int(task.Pid()), cgroups)
	if err != nil {
		return err
	}

	le := co.Spec.LogEntity
	if le == "" {
		le = co.ID.String()
	}

	attrs := map[string]string{
		"sandbox": co.ID.String(),
	}

	if co.Spec.Version != "" {
		attrs["version"] = co.Spec.Version.String()
	}

	for _, lbl := range co.Spec.LogAttribute {
		attrs[lbl.Key] = lbl.Value
	}

	err = c.Metrics.Add(le, cgroups, attrs)
	if err != nil {
		return err
	}

	c.Log.Info("sandbox started", "id", co.ID, "namespace", c.Namespace)

	// Wait for ports with a shorter timeout (5 seconds per port)
	// Many sandboxes don't actually bind their declared ports immediately
	portTimeout := 5 * time.Second
	for _, wp := range waitPorts {
		c.Log.Info("waiting for ports to be bound", "id", cid, "port", wp.port, "timeout", portTimeout)
		if err := c.waitForPort(ctx, wp.id, wp.port, portTimeout); err != nil {
			c.Log.Warn("failed to wait for port binding", "id", cid, "port", wp.port, "error", err)
			// Continue anyway - the port might bind later or might not be critical
		}
	}

	co.Status = compute.RUNNING

	// The controller will detect the updates and sync them back
	if err := meta.Update(co.Encode()); err != nil {
		return fmt.Errorf("failed to update entity metadata: %w", err)
	}

	err = c.updateServices(ctx, co, meta, ep)
	if err != nil {
		return fmt.Errorf("failed to update services: %w", err)
	}

	return c.saveEntity(ctx, co, meta)
}

func (c *SandboxController) updateServices(
	ctx context.Context,
	co *compute.Sandbox,
	meta *entity.Meta,
	ep *network.EndpointConfig,
) error {
	sresp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindService))
	if err != nil {
		return err
	}

	md := core_v1alpha.MD(meta.Entity)

	c.Log.Debug("updating services", "id", co.ID, "labels", md.Labels, "services", len(sresp.Values()))

	for _, ent := range sresp.Values() {
		var srv network_v1alpha.Service
		srv.Decode(ent.Entity())

		if !srv.Match.Equal(md.Labels) {
			c.Log.Debug("skipping service, labels do not match", "service", srv.ID, "labels", srv.Match, "entity", md.Labels)
			continue
		}

		err = c.addEndpoint(ctx, co, ep, &srv)
		if err != nil {
			return fmt.Errorf("failed to add endpoint: %w", err)
		}
	}

	return nil
}

func (c *SandboxController) addEndpoint(
	ctx context.Context,
	sb *compute.Sandbox,
	ep *network.EndpointConfig,
	srv *network_v1alpha.Service,
) error {
	c.Log.Debug("adding endpoint to service", "service", srv.ID, "sandbox", sb.ID, "containers", len(sb.Spec.Container))

	for _, co := range sb.Spec.Container {
		for _, p := range co.Port {
			var add bool
			for _, sp := range srv.Port {
				if (sp.TargetPort != 0 && p.Port == sp.TargetPort) || p.Port == sp.Port {
					add = true
					break
				}
			}

			if !add {
				c.Log.Debug("skipping port, not in service", "port", p.Port, "service", srv.ID)
				continue
			}

			var eps network_v1alpha.Endpoints

			eps.Service = srv.ID
			eps.Endpoint = append(eps.Endpoint, network_v1alpha.Endpoint{
				Ip:   ep.Addresses[0].Addr().String(),
				Port: p.Port,
			})

			var rpcE entityserver_v1alpha.Entity
			rpcE.SetAttrs(eps.Encode())

			pr, err := c.EAC.Put(ctx, &rpcE)
			if err != nil {
				return fmt.Errorf("failed to update service: %w", err)
			}

			c.Log.Debug("updated service", "id", pr.Id(), "service", eps.Service)
		}
	}

	return nil
}

func (c *SandboxController) deleteEndpoints(ctx context.Context, sb *compute.Sandbox, sandboxIPs map[string]bool) error {
	// If no IPs found, nothing to delete
	if len(sandboxIPs) == 0 {
		c.Log.Debug("no sandbox IPs found, skipping endpoint deletion", "sandbox_id", sb.ID)
		return nil
	}

	// Get all endpoints
	endpoints, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
	if err != nil {
		return fmt.Errorf("failed to list endpoints: %w", err)
	}

	c.Log.Debug("considering endpoints for deletion", "sandbox_id", sb.ID, "endpoints_count", len(endpoints.Values()), "sandbox_ips", sandboxIPs)

	// Delete any endpoints that contain our sandbox's IPs
	for _, epEntity := range endpoints.Values() {
		var ep network_v1alpha.Endpoints
		ep.Decode(epEntity.Entity())

		// Check if any endpoint IPs match our sandbox IPs
		shouldDelete := false
		for _, endpoint := range ep.Endpoint {
			if sandboxIPs[endpoint.Ip] {
				shouldDelete = true
				break
			}
		}

		if shouldDelete {
			c.Log.Info("deleting endpoints for sandbox", "sandbox_id", sb.ID, "endpoint_id", ep.ID)
			_, err = c.EAC.Delete(ctx, ep.ID.String())
			if err != nil {
				c.Log.Error("failed to delete endpoint", "id", ep.ID, "error", err)
			}
		}
	}

	return nil
}

// deallocateNetwork releases the network resources allocated for a sandbox
func (c *SandboxController) deallocateNetwork(ctx context.Context, ep *network.EndpointConfig) {
	if ep == nil {
		return
	}

	for _, addr := range ep.Addresses {
		if err := c.Subnet.ReleaseAddr(addr.Addr()); err != nil {
			c.Log.Error("failed to release IP address during cleanup", "addr", addr.Addr(), "err", err)
		} else {
			c.Log.Debug("released IP address during cleanup", "addr", addr.Addr())
		}
	}
}

func (c *SandboxController) allocateNetwork(
	ctx context.Context,
	co *compute.Sandbox,
) (*network.EndpointConfig, error) {
	if c.Bridge == "" {
		return nil, fmt.Errorf("bridge name not configured")
	}

	if c.Subnet == nil {
		return nil, fmt.Errorf("subnet not configured")
	}

	var (
		ep  *network.EndpointConfig
		err error
	)

	if len(co.Network) > 0 {
		var prefixes []netip.Prefix

		for _, net := range co.Network {
			// Parse address (handles both CIDR and plain IP formats)
			ipStr, err := netutil.ParseNetworkAddress(net.Address)
			if err != nil {
				return nil, fmt.Errorf("invalid address: %s (%w)", net.Address, err)
			}

			// Convert to netip.Addr
			addr, err := netip.ParseAddr(ipStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse IP: %s (%w)", ipStr, err)
			}

			// Convert to prefix (assume /32 for IPv4, /128 for IPv6)
			prefix := netip.PrefixFrom(addr, addr.BitLen())
			prefixes = append(prefixes, prefix)
		}

		ep, err = network.SetupOnBridge(c.Bridge, c.Subnet, prefixes)
		if err != nil {
			return nil, err
		}

	} else {
		ep, err = network.AllocateOnBridge(c.Bridge, c.Subnet)
		if err != nil {
			return nil, err
		}

		co.Network = append(co.Network, compute.Network{
			Address: ep.Addresses[0].String(),
			Subnet:  c.Bridge,
		})
	}

	c.Log.Debug("allocated network endpoint", "bridge", c.Bridge, "addresses", ep.Addresses)

	return ep, nil
}

func (c *SandboxController) setupHosts(sb *compute.Sandbox, name string) error {
	var lines []string

	lines = append(lines, "# The following lines are managed by runtime.computer")
	lines = append(lines, fmt.Sprintf("127.0.0.1\tlocalhost localhost.localdomain %s", name))
	lines = append(lines, fmt.Sprintf("::1\tlocalhost localhost.localdomain %s", name))

	for _, addr := range sb.Spec.StaticHost {
		lines = append(lines, fmt.Sprintf("%s\t%s", addr.Ip, addr.Host))
	}
	lines = append(lines, "")

	path := c.sandboxPath(sb, "hosts")

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func (c *SandboxController) resolver() remotes.Resolver {
	headers := make(http.Header)
	headers.Set("User-Agent", "containerd/2")

	return docker.NewResolver(docker.ResolverOptions{
		Hosts: func(host string) ([]docker.RegistryHost, error) {
			switch host {
			case "cluster.local", "cluster.local:5000":
				addr, err := c.Resolver.LookupHost("cluster.local")
				if err != nil {
					return nil, fmt.Errorf("failed to resolve cluster.local: %w", err)
				}

				config := docker.RegistryHost{
					Client:       http.DefaultClient,
					Host:         addr.String() + ":5000",
					Scheme:       "http",
					Path:         "/v2",
					Capabilities: docker.HostCapabilityPull | docker.HostCapabilityResolve | docker.HostCapabilityPush,
				}

				return []docker.RegistryHost{config}, nil
			default:
				config := docker.RegistryHost{
					Client: http.DefaultClient,
					Authorizer: docker.NewDockerAuthorizer(
						docker.WithAuthHeader(headers)),
					Host:         host,
					Scheme:       "https",
					Path:         "/v2",
					Capabilities: docker.HostCapabilityPull | docker.HostCapabilityResolve | docker.HostCapabilityPush,
				}

				if host == "docker.io" {
					config.Host = "registry-1.docker.io"
				}
				return []docker.RegistryHost{config}, nil
			}
		},
	})
}

func (c *SandboxController) buildSpec(
	ctx context.Context,
	sb *compute.Sandbox,
	ep *network.EndpointConfig,
	meta *entity.Meta,
) (
	[]containerd.NewContainerOpts,
	error,
) {
	img, err := c.CC.GetImage(ctx, sandboxImage)
	if err != nil {
		// If the image is not found, we can try to pull it.
		_, err = c.CC.Pull(ctx, sandboxImage, containerd.WithPullUnpack, containerd.WithResolver(c.resolver()))
		if err != nil {
			return nil, fmt.Errorf("failed to pull image %s: %w", sandboxImage, err)
		}

		img, err = c.CC.GetImage(ctx, sandboxImage)
		if err != nil {
			// If we still can't get the image, return the error.
			return nil, fmt.Errorf("failed to get image %s: %w", sandboxImage, err)
		}
	}

	sz, err := img.Size(ctx)
	if err != nil {
		return nil, err
	}

	c.Log.Info("image ready", "ref", img.Metadata().Target.Digest, "size", sz)

	var (
		opts []containerd.NewContainerOpts
	)

	lbls := map[string]string{}

	for _, lbl := range sb.Labels {
		if key, val, ok := strings.Cut(lbl, "="); ok {
			lbls[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}

	lbls[sandboxVersionLabel] = strconv.FormatInt(meta.Revision, 10)
	lbls[sandboxEntityLabel] = sb.ID.String()
	lbls[sandboxKindLabel] = "sandbox"

	if sb.Spec.Version != "" {
		lbls[sandboxVerEntityLabel] = sb.Spec.Version.String()
	}

	// Add IP addresses from endpoint configuration
	for i, addr := range ep.Addresses {
		if i == 0 {
			lbls["runtime.computer/ip"] = addr.Addr().String()
		} else {
			lbls[fmt.Sprintf("runtime.computer/ip%d", i)] = addr.Addr().String()
		}
	}

	//if config.StaticDir != "" {
	//lbls["runtime.computer/static_dir"] = config.StaticDir
	//}

	tmpDir := filepath.Join(c.Tempdir, "containerd", sb.ID.PathSafe())
	os.MkdirAll(tmpDir, 0755)

	resolvePath := c.sandboxPath(sb, "resolv.conf")
	err = c.writeResolve(resolvePath, ep)
	if err != nil {
		return nil, err
	}

	err = c.setupHosts(sb, sb.ID.String())
	if err != nil {
		return nil, err
	}

	mounts := []specs.Mount{
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
		{
			Destination: "/sys/fs/cgroup",
			Type:        "cgroup",
			Source:      "cgroup",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
		{
			Destination: "/etc/resolv.conf",
			Type:        "bind",
			Source:      resolvePath,
			Options:     []string{"rbind", "rw"},
		},
		{
			Destination: "/etc/hosts",
			Type:        "bind",
			Source:      c.sandboxPath(sb, "hosts"),
			Options:     []string{"rbind", "rw"},
		},
	}

	// Create unique cgroup path for this sandbox
	cgroupPath := fmt.Sprintf("/miren/sandbox-%s", sb.ID.PathSafe())

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
		oci.WithDefaultUnixDevices,
		oci.WithoutMounts("/sys"),
		oci.WithMounts(mounts),
		oci.WithProcessCwd("/"),
		oci.WithAnnotations(map[string]string{
			"io.kubernetes.cri.container-type": "sandbox",
		}),
		func(ctx context.Context, c1 oci.Client, c2 *containers.Container, s *oci.Spec) error {
			s.Linux.CgroupsPath = cgroupPath
			return nil
		},
		containerdx.WithOOMScoreAdj(defaultSandboxOOMAdj, false),
	}

	if sb.Spec.HostNetwork {
		specOpts = append(specOpts, oci.WithHostNamespace(specs.NetworkNamespace))
	}

	id := sb.ID.String()

	opts = append(opts,
		containerd.WithNewSnapshot(id, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runc.v2", nil),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	return opts, nil
}

func (c *SandboxController) writeResolve(path string, ep *network.EndpointConfig) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if len(ep.Bridge.Addresses) == 0 {
		return fmt.Errorf("no nameservers available in bridge config")
	}

	for _, addr := range ep.Bridge.Addresses {
		if !addr.Addr().IsValid() {
			return fmt.Errorf("invalid nameserver address: %v", addr)
		}
		fmt.Fprintf(f, "nameserver %s\n", addr.Addr().String())
	}

	return nil
}

func (c *SandboxController) logConsumer(sb *compute.Sandbox, container string) *SandboxLogs {
	le := sb.Spec.LogEntity
	if le == "" {
		le = sb.ID.String()
	}

	lw := &observability.PersistentLogWriter{
		DB: c.Clickhouse,
	}

	attrs := map[string]string{
		"sandbox": sb.ID.String(),
	}

	if container != "" {
		attrs["container"] = container
	}

	if sb.Spec.Version != "" {
		attrs["version"] = sb.Spec.Version.String()
	}

	for _, lbl := range sb.Spec.LogAttribute {
		attrs[lbl.Key] = lbl.Value
	}

	return NewSandboxLogs(c.Log, le, attrs, lw)
}

func (c *SandboxController) bootInitialTask(
	ctx context.Context,
	sb *compute.Sandbox,
	ep *network.EndpointConfig,
	container containerd.Container,
) (containerd.Task, error) {
	c.Log.Info("booting sandbox task")

	sl := c.logConsumer(sb, "")

	task, err := container.NewTask(ctx, cio.NewCreator(
		cio.WithStreams(nil, sl, sl.Stderr())))
	if err != nil {
		return nil, err
	}

	err = network.ConfigureNetNS(c.Log, int(task.Pid()), ep)
	if err != nil {
		return nil, err
	}

	err = c.configureFirewall(sb, ep)
	if err != nil {
		return nil, err
	}

	err = task.Start(ctx)
	if err != nil {
		return nil, err
	}

	return task, nil
}

type waitPort struct {
	id   string
	port int
}

// cleanupContainer removes a container and its snapshot during failure scenarios
func (c *SandboxController) cleanupContainer(ctx context.Context, cont containerd.Container) {
	if cont == nil {
		return
	}

	containerID := cont.ID()

	c.Log.Debug("cleaning up container", "id", containerID)

	// Stop port monitoring for this container
	if c.portMonitor != nil {
		c.portMonitor.StopMonitoring(containerID)
	}

	// Try to kill and delete any task first
	task, err := cont.Task(ctx, nil)
	if err == nil && task != nil {
		task.Kill(ctx, unix.SIGKILL)
		_, err = task.Delete(ctx, containerd.WithProcessKill)
		if err != nil {
			c.Log.Debug("failed to delete task during cleanup", "id", containerID, "err", err)
		}
	}

	// Get the snapshotter info from the container before deleting it
	var snapshotKey string
	var snapshotterName string
	if info, ierr := cont.Info(ctx); ierr == nil {
		snapshotKey = info.SnapshotKey
		snapshotterName = info.Snapshotter
	}

	// Delete the container with snapshot cleanup
	err = cont.Delete(ctx, containerd.WithSnapshotCleanup)
	if err != nil && !errdefs.IsNotFound(err) {
		c.Log.Error("failed to cleanup container", "id", containerID, "err", err)
	} else {
		c.Log.Debug("cleaned up container", "id", containerID)
	}

	// Always try to explicitly delete the snapshot to be absolutely sure
	if snapshotterName != "" && snapshotKey != "" {
		snapshotter := c.CC.SnapshotService(snapshotterName)
		if snapshotter != nil {
			if err := snapshotter.Remove(ctx, snapshotKey); err != nil && !errdefs.IsNotFound(err) {
				c.Log.Debug("failed to explicitly delete snapshot", "id", containerID, "snapshot_key", snapshotKey, "err", err)
			} else if err == nil {
				c.Log.Debug("explicitly deleted snapshot", "id", containerID, "snapshot_key", snapshotKey)
			}
		}
	}
}

// cleanupContainers removes containers and their snapshots during failure scenarios
func (c *SandboxController) cleanupContainers(ctx context.Context, containers []containerd.Container) {
	for _, cont := range containers {
		if cont != nil {
			c.cleanupContainer(ctx, cont)
		}
	}
}

func (c *SandboxController) bootContainers(
	ctx context.Context,
	sb *compute.Sandbox,
	ep *network.EndpointConfig,
	sbPid int,
	cgroups map[string]string,
) ([]waitPort, error) {
	c.Log.Info("booting containers", "count", len(sb.Spec.Container))

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	var ret []waitPort
	var createdContainers []containerd.Container

	// Clean up any created containers on failure
	defer func() {
		if err := recover(); err != nil {
			c.Log.Error("panic during container boot, cleaning up", "error", err)
			c.cleanupContainers(ctx, createdContainers)
			panic(err)
		}
	}()

	for _, container := range sb.Spec.Container {
		opts, err := c.buildSubContainerSpec(ctx, sb, &container, ep, sbPid)
		if err != nil {
			c.cleanupContainers(ctx, createdContainers)
			return nil, fmt.Errorf("failed to build container spec: %w", err)
		}

		id := fmt.Sprintf("%s-%s", c.containerPrefix(sb.ID), container.Name)

		var ports []int
		for _, port := range container.Port {
			ports = append(ports, int(port.Port))
			ret = append(ret, waitPort{
				id:   id,
				port: int(port.Port),
			})
		}

		c.Log.Info("creating container", "id", id)

		cc, err := c.CC.NewContainer(ctx, id, opts...)
		if err != nil {
			c.cleanupContainers(ctx, createdContainers)
			return nil, errors.Wrapf(err, "failed to create container %s", sb.ID)
		}
		createdContainers = append(createdContainers, cc)

		spec, err := cc.Spec(ctx)
		if err != nil {
			c.cleanupContainers(ctx, createdContainers)
			return nil, fmt.Errorf("failed to get container spec: %w", err)
		}

		cgroups[container.Name] = spec.Linux.CgroupsPath

		sl := c.logConsumer(sb, container.Name)

		task, err := cc.NewTask(ctx, cio.NewCreator(
			cio.WithStreams(nil, sl, sl.Stderr())))
		if err != nil {
			c.cleanupContainers(ctx, createdContainers)
			return nil, err
		}

		err = task.Start(ctx)
		if err != nil {
			// Try to delete the task first if it was created but not started
			task.Delete(ctx, containerd.WithProcessKill)
			c.cleanupContainers(ctx, createdContainers)
			return nil, err
		}

		c.Log.Info("container started", "id", cc.ID())

		// Start port monitoring for this container if it has ports
		if len(ports) > 0 && len(ep.Addresses) > 0 {
			ip := ep.Addresses[0].Addr().String()
			c.portMonitor.MonitorContainer(id, ip, ports)
		}
	}

	return ret, nil
}

func (c *SandboxController) sandboxPath(sb *compute.Sandbox, sub ...string) string {
	parts := append(
		[]string{c.Tempdir, "containerd", sb.ID.PathSafe()},
		sub...,
	)

	return filepath.Join(parts...)
}

func (c *SandboxController) buildSubContainerSpec(
	ctx context.Context,
	sb *compute.Sandbox,
	co *compute.SandboxSpecContainer,
	ep *network.EndpointConfig,
	sbPid int,
) (
	[]containerd.NewContainerOpts,
	error,
) {
	img, err := c.CC.GetImage(ctx, co.Image)
	if err != nil {
		// If the image is not found, we can try to pull it.
		_, err = c.CC.Pull(ctx, co.Image, containerd.WithPullUnpack, containerd.WithResolver(c.resolver()))
		if err != nil {
			return nil, fmt.Errorf("failed to pull image %s: %w", co.Image, err)
		}

		img, err = c.CC.GetImage(ctx, co.Image)
		if err != nil {
			// If we still can't get the image, return the error.
			return nil, fmt.Errorf("failed to get image %s: %w", co.Image, err)
		}
	}

	sz, err := img.Size(ctx)
	if err != nil {
		return nil, err
	}

	c.Log.Info("image ready", "ref", img.Metadata().Target.Digest, "size", sz)

	var (
		opts []containerd.NewContainerOpts
	)

	id := fmt.Sprintf("%s-%s", sb.ID, co.Name)

	resolvePath := c.sandboxPath(sb, "resolv.conf")

	mounts := []specs.Mount{
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
		{
			Destination: "/sys/fs/cgroup",
			Type:        "cgroup",
			Source:      "cgroup",
			Options:     []string{"nosuid", "noexec", "nodev", "rw"},
		},
		{
			Destination: "/etc/resolv.conf",
			Type:        "bind",
			Source:      resolvePath,
			Options:     []string{"rbind", "rw"},
		},
		{
			Destination: "/etc/hosts",
			Type:        "bind",
			Source:      c.sandboxPath(sb, "hosts"),
			Options:     []string{"rbind", "rw"},
		},
	}

	// Add local storage mount if this sandbox has an associated app
	if sb.Spec.Version != "" && c.DataPath != "" {
		// Fetch the AppVersion to get the App ID
		res, err := c.EAC.Get(ctx, sb.Spec.Version.String())
		if err == nil {
			var appVer core_v1alpha.AppVersion
			appVer.Decode(res.Entity().Entity())

			if appVer.App != "" {
				// Create the local storage path for this app
				// Using 0777 for simplicity - containers run as non-root app user and need write access
				// TODO: Consider more restrictive permissions with proper UID/GID mapping or ACLs for production
				localStoragePath := filepath.Join(c.DataPath, "data", "local", appVer.App.String())
				if err = os.MkdirAll(localStoragePath, 0777); err != nil {
					c.Log.Warn("failed to create local storage directory", "path", localStoragePath, "error", err)
				} else {
					// Explicitly chmod to 0777 since MkdirAll respects umask
					if err = os.Chmod(localStoragePath, 0777); err != nil {
						c.Log.Warn("failed to set permissions on local storage directory", "path", localStoragePath, "error", err)
					}
					// Add the bind mount for local storage
					mounts = append(mounts, specs.Mount{
						Destination: "/miren/data/local",
						Type:        "bind",
						Source:      localStoragePath,
						Options:     []string{"rbind", "rprivate", "nosuid", "nodev", "rw"},
					})
					c.Log.Info("added local storage mount for container", "app", appVer.App.String(), "path", localStoragePath, "container", co.Name)
				}
			}
		} else {
			c.Log.Error("failed to fetch app version for local storage", "version", sb.Spec.Version.String(), "error", err)
		}
	}

	for _, m := range co.Mount {
		rawPath := c.sandboxPath(sb, "volumes", m.Source)
		st, err := os.Lstat(rawPath)
		if err != nil {
			return nil, fmt.Errorf("volume %s does not exist", rawPath)
		}

		for st.Mode().Type() == os.ModeSymlink {
			tgt, err := os.Readlink(rawPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read symlink %s: %w", rawPath, err)
			}

			rawPath = tgt
			st, err = os.Stat(rawPath)
			if err != nil {
				return nil, fmt.Errorf("volume %s does not exist", rawPath)
			}
		}

		mounts = append(mounts, specs.Mount{
			Destination: m.Destination,
			Type:        "bind",
			Source:      rawPath,
			Options:     []string{"rbind", "rw"},
		})
	}

	for _, cf := range co.ConfigFile {
		h, _ := blake2b.New256(nil)
		fmt.Fprint(h, cf.Path)
		fmt.Fprint(h, cf.Data)

		id := base58.Encode(h.Sum(nil))

		rawPath := c.sandboxPath(sb, id)

		var mode os.FileMode = 0644

		if cf.Mode != "" {
			m, err := strconv.ParseInt(cf.Mode, 8, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse file mode %s: %w", cf.Mode, err)
			}
			mode = os.FileMode(m)
		}

		err = os.WriteFile(rawPath, []byte(cf.Data), mode)
		if err != nil {
			return nil, fmt.Errorf("failed to write config file %s: %w", rawPath, err)
		}

		c.Log.Debug("created config file", "path", rawPath, "dest", cf.Path, "mode", mode)

		mounts = append(mounts, specs.Mount{
			Destination: cf.Path,
			Type:        "bind",
			Source:      rawPath,
			Options:     []string{"rbind", "rw"},
		})
	}

	dir := co.Directory
	if dir == "" {
		dir = "/"
	}

	specOpts := []oci.SpecOpts{
		oci.WithImageConfig(img),
		oci.WithDefaultUnixDevices,
		oci.WithoutMounts("/sys"),
		oci.WithMounts(mounts),
		oci.WithProcessCwd(dir),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.NetworkNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/net", sbPid),
		}),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.IPCNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/ipc", sbPid),
		}),
		oci.WithLinuxNamespace(specs.LinuxNamespace{
			Type: specs.TimeNamespace,
			Path: fmt.Sprintf("/proc/%d/ns/time", sbPid),
		}),
		oci.WithEnv(co.Env),
	}

	if co.Command != "" {
		specOpts = append(specOpts, oci.WithProcessArgs("/bin/sh", "-c", co.Command))
	}

	if co.OomScore != 0 {
		specOpts = append(specOpts, containerdx.WithOOMScoreAdj(int(co.OomScore), false))
	}

	if co.Privileged {
		specOpts = append(specOpts,
			oci.WithPrivileged,
			oci.WithAllDevicesAllowed,
			oci.WithWriteableCgroupfs,
			oci.WithAddedCapabilities([]string{"CAP_SYS_ADMIN"}),
		)
	}

	lbls := map[string]string{}
	lbls[sandboxEntityLabel] = sb.ID.String()

	if sb.Spec.Version != "" {
		lbls[sandboxVerEntityLabel] = sb.Spec.Version.String()
	}

	opts = append(opts,
		containerd.WithNewSnapshot(id, img),
		containerd.WithNewSpec(specOpts...),
		containerd.WithRuntime("io.containerd.runc.v2", nil),
		containerd.WithAdditionalContainerLabels(lbls),
	)

	return opts, nil
}

func (c *SandboxController) destroySubContainers(ctx context.Context, sb *compute.Sandbox) error {
	if len(sb.Spec.Container) == 0 {
		return nil
	}

	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	// Set up timeout for the entire operation (1 minute max)
	timeout := 60 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c.Log.Info("starting subcontainer destruction", "id", sb.ID, "containers", len(sb.Spec.Container), "timeout", timeout)

	// Track containers that need to be destroyed
	containerIds := make([]string, 0, len(sb.Spec.Container))
	for _, container := range sb.Spec.Container {
		id := fmt.Sprintf("%s-%s", c.containerPrefix(sb.ID), container.Name)
		containerIds = append(containerIds, id)
		// Stop port monitoring for this container
		if c.portMonitor != nil {
			c.portMonitor.StopMonitoring(id)
		}
	}

	// Retry loop with exponential backoff
	retryInterval := 100 * time.Millisecond
	maxRetryInterval := 2 * time.Second

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("failed to destroy subcontainers within %v timeout", timeout)
			}
			return fmt.Errorf("context cancelled while destroying subcontainers: %w", ctx.Err())
		default:
		}

		// Check if any containers still exist
		remainingContainers := make([]string, 0)
		for _, id := range containerIds {
			cont, err := c.CC.LoadContainer(ctx, id)
			if err != nil {
				if errdefs.IsNotFound(err) {
					// Container doesn't exist, consider it deleted
					c.Log.Debug("container not found, considering deleted", "id", id)
				} else {
					c.Log.Error("failed to load container", "id", id, "err", err)
				}

				continue
			}
			remainingContainers = append(remainingContainers, id)

			// Try to kill and delete the task
			task, err := cont.Task(ctx, nil)
			if err == nil {
				// First try SIGTERM
				err = task.Kill(ctx, unix.SIGTERM)
				if err != nil {
					c.Log.Debug("failed to send SIGTERM", "id", id, "err", err)
				} else {
					c.Log.Debug("sent SIGTERM to task", "id", id)
				}

				// Wait a bit then try to delete the task
				time.Sleep(50 * time.Millisecond)

				_, err = task.Delete(ctx, containerd.WithProcessKill)
				if err != nil {
					c.Log.Debug("failed to delete task", "id", id, "err", err)
				} else {
					c.Log.Debug("deleted task", "id", id)
				}
			} else {
				c.Log.Debug("no task found for container", "id", id)
			}

			// Try to delete the container
			err = cont.Delete(ctx, containerd.WithSnapshotCleanup)
			if err != nil {
				c.Log.Debug("failed to delete container", "id", id, "err", err)
			} else {
				c.Log.Debug("deleted container", "id", id)
			}
		}

		// If no containers remain, we're done
		if len(remainingContainers) == 0 {
			c.Log.Info("all subcontainers destroyed successfully", "id", sb.ID)
			return nil
		}

		// Update the list of containers to check in the next iteration
		containerIds = remainingContainers
		c.Log.Debug("waiting for containers to be destroyed", "id", sb.ID, "remaining", len(containerIds), "retry_in", retryInterval)

		// Wait before retrying
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("failed to destroy subcontainers within %v timeout, remaining: %v", timeout, containerIds)
			}
			return fmt.Errorf("context cancelled while destroying subcontainers: %w", ctx.Err())
		case <-time.After(retryInterval):
			// Exponential backoff with max limit
			retryInterval = time.Duration(float64(retryInterval) * 1.5)
			if retryInterval > maxRetryInterval {
				retryInterval = maxRetryInterval
			}
		}
	}
}

func (c *SandboxController) Delete(ctx context.Context, id entity.Id) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	// Container exists, we need to read the entity data to properly delete it
	oldMeta, err := c.readEntity(ctx, id)
	if err != nil {
		// If entity file is missing but container exists, try to reconstruct minimal sandbox info
		if strings.Contains(err.Error(), "entity file not found") {
			// Check if the container exists
			_, err := c.CC.LoadContainer(ctx, c.pauseContainerId(id))
			if err != nil {
				if errdefs.IsNotFound(err) {
					// Container doesn't exist, consider it already deleted
					c.Log.Info("Delete called but container not found, already deleted", "id", id, "error", err)
					return nil
				}

				return err
			}
			c.Log.Warn("entity file missing but container exists, attempting cleanup", "id", id)
			// Create a minimal sandbox just with the ID to attempt cleanup
			sb := &compute.Sandbox{
				ID: id,
			}
			return c.stopSandbox(ctx, sb)
		}
		c.Log.Error("failed to read existing entity", "id", id, "err", err)
		return fmt.Errorf("failed to read existing entity: %w", err)
	}

	var oldSb compute.Sandbox
	oldSb.Decode(oldMeta)

	return c.stopSandbox(ctx, &oldSb)
}

func (c *SandboxController) stopSandbox(ctx context.Context, sb *compute.Sandbox) error {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	le := sb.Spec.LogEntity
	if le == "" {
		le = sb.ID.String()
	}

	c.Log.Debug("removing monitoring metrics", "id", sb.ID, "log_entity", le)
	err := c.Metrics.Remove(le)
	if err != nil {
		c.Log.Error("failed to remove monitoring metrics", "id", sb.ID, "error", err)
	}

	c.Log.Debug("stopping container", "id", sb.ID)
	err = c.destroySubContainers(ctx, sb)
	if err != nil {
		return fmt.Errorf("failed to destroy subcontainers: %w", err)
	}

	c.Log.Debug("deleting pause container", "id", sb.ID)

	// Collect sandbox IPs before deleting container
	sandboxIPs := make(map[string]bool)
	container, err := c.CC.LoadContainer(ctx, c.pauseContainerId(sb.ID))
	if err == nil {
		labels, err := container.Labels(ctx)
		if err != nil {
			return err
		}

		task, err := container.Task(ctx, nil)
		if err != nil {
			if !errdefs.IsNotFound(err) {
				c.Log.Error("failed to get pause task", "id", sb.ID, "err", err)
				return err
			}
			c.Log.Debug("pause task not found, continuing with container deletion", "id", sb.ID)
		} else if task != nil {
			_, err = task.Delete(ctx, containerd.WithProcessKill)
			if err != nil {
				c.Log.Error("failed to delete pause task", "id", sb.ID, "err", err)
				return err
			}
		}

		err = container.Delete(ctx, containerd.WithSnapshotCleanup)
		if err != nil {
			c.Log.Error("failed to delete pause container", "id", sb.ID, "err", err)
			return err
		}

		for l, v := range labels {
			if strings.HasPrefix(l, "runtime.computer/ip") {
				addr, err := netip.ParseAddr(v)
				if err == nil {
					sandboxIPs[v] = true
					err = c.Subnet.ReleaseAddr(addr)
					if err != nil {
						c.Log.Error("failed to release IP", "addr", addr, "err", err)
					}
				} else {
					c.Log.Error("failed to parse IP", "addr", v, "err", err)
				}

				c.Log.Debug("released IP", "addr", addr)
			}
		}

		// Ignore errors, as the directory might not exist if the container was
		// cleared up elsewhere.
		tmpDir := filepath.Join(c.Tempdir, "containerd", sb.ID.PathSafe())
		_ = os.RemoveAll(tmpDir)

		c.Log.Info("container stopped", "id", sb.ID)
	} else if !errdefs.IsNotFound(err) {
		return err
	}

	var rpcE entityserver_v1alpha.Entity

	rpcE.SetId(sb.ID.String())

	rpcE.SetAttrs(entity.New(
		(&compute.Sandbox{
			Status: compute.DEAD,
		}).Encode,
	).Attrs())

	_, err = c.EAC.Put(context.Background(), &rpcE)
	if err != nil {
		c.Log.Error("failed to retire sandbox", "error", err)
	}

	c.Log.Info("sandbox retired", "id", sb.ID, "status", compute.DEAD)

	// Clean up endpoints associated with this sandbox
	err = c.deleteEndpoints(ctx, sb, sandboxIPs)
	if err != nil {
		c.Log.Error("failed to delete endpoints for sandbox", "id", sb.ID, "error", err)
		// Continue with cleanup even if endpoint deletion fails
	}

	return nil
}

// Periodic cleans up dead sandboxes that are older than the specified time horizon
func (c *SandboxController) Periodic(ctx context.Context, timeHorizon time.Duration) error {
	c.Log.Info("running periodic cleanup of dead sandboxes", "time_horizon", timeHorizon)

	// List all sandboxes
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, compute.KindSandbox))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	now := time.Now()
	cutoffTime := now.Add(-timeHorizon)

	var deleted int
	for _, e := range resp.Values() {
		var sb compute.Sandbox
		sb.Decode(e.Entity())

		// Check if sandbox is DEAD and UpdatedAt is older than time horizon
		if sb.Status == compute.DEAD {
			updatedAt := e.Entity().GetUpdatedAt()

			c.Log.Debug("checking sandbox for cleanup",
				"id", sb.ID,
				"status", sb.Status,
				"updated_at", updatedAt.Format(time.RFC3339),
				"age", now.Sub(updatedAt).String())

			if updatedAt.Before(cutoffTime) {
				c.Log.Info("deleting old dead sandbox",
					"id", sb.ID,
					"updated_at", updatedAt.Format(time.RFC3339),
					"age", now.Sub(updatedAt).String())

				_, err := c.EAC.Delete(ctx, sb.ID.String())
				if err != nil {
					c.Log.Error("failed to delete dead sandbox", "id", sb.ID, "error", err)
					continue
				}
				deleted++
			}
		}
	}

	if deleted > 0 {
		c.Log.Info("periodic cleanup completed", "deleted_sandboxes", deleted)
	}

	return nil
}
