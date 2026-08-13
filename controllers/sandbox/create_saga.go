package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/errdefs"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/saga"
)

// Saga action names
const (
	sagaCreateSandbox  = "create-sandbox"
	actionAllocNetwork = "alloc-network"
	actionPatchNetwork = "patch-network"
	actionCreateCtr    = "create-container"
	actionBootTask     = "boot-task"
	actionBootCtrs     = "boot-containers"
	actionAddMetrics   = "add-metrics"
	actionWaitPorts    = "wait-ports"
	actionSetRunning   = "set-running"
	actionUpdateSvcs   = "update-services"
)

// createSandboxDeps holds the dependencies injected into the saga context.
// These are retrieved via saga.Get[*createSandboxDeps](ctx) within actions.
type createSandboxDeps struct {
	entities   SandboxEntityStore
	networking SandboxNetworking
	runtime    SandboxContainerRuntime
	obs        SandboxObservability
}

// logSandboxEvent surfaces a startup diagnostic on the sandbox's own log stream
// so it shows up under `miren logs sandbox <id>` next to the container output.
// Best-effort: a fetch failure just skips the event rather than failing the saga.
func (d *createSandboxDeps) logSandboxEvent(ctx context.Context, sandboxID, line string) {
	sb, meta, err := d.entities.GetSandbox(ctx, sandboxID)
	if err != nil {
		return
	}
	d.obs.LogSandboxEvent(sb, meta.ShortId(), line)
}

// --- Action input/output types ---

type allocNetworkIn struct {
	SandboxID string `json:"sandbox_id" saga:"sandbox_id"`
}

type allocNetworkOut struct {
	Addresses []string `json:"addresses" saga:"addresses"`
}

func allocNetwork(ctx context.Context, in allocNetworkIn) (allocNetworkOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	sb, _, err := deps.entities.GetSandbox(ctx, in.SandboxID)
	if err != nil {
		return allocNetworkOut{}, fmt.Errorf("fetching sandbox: %w", err)
	}

	ep, err := deps.networking.AllocateNetwork(ctx, sb)
	if err != nil {
		return allocNetworkOut{}, fmt.Errorf("allocating network: %w", err)
	}

	addrs := make([]string, len(ep.Addresses))
	for i, a := range ep.Addresses {
		addrs[i] = a.String()
	}

	log.Debug("saga: allocated network", "sandbox", in.SandboxID, "addresses", addrs)
	return allocNetworkOut{Addresses: addrs}, nil
}

func undoAllocNetwork(ctx context.Context, _ allocNetworkIn, out allocNetworkOut) error {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)
	var undoErr error

	for _, addrStr := range out.Addresses {
		prefix, err := netip.ParsePrefix(addrStr)
		if err != nil {
			log.Error("saga undo: failed to parse address", "addr", addrStr, "error", err)
			if undoErr == nil {
				undoErr = fmt.Errorf("parsing address %q: %w", addrStr, err)
			}
			continue
		}
		if err := deps.networking.ReleaseAddr(prefix.Addr()); err != nil {
			log.Error("saga undo: failed to release IP", "addr", prefix.Addr(), "error", err)
			if undoErr == nil {
				undoErr = fmt.Errorf("releasing IP %s: %w", prefix.Addr(), err)
			}
		} else {
			log.Debug("saga undo: released IP", "addr", prefix.Addr())
		}
	}
	return undoErr
}

// --- Patch network entity ---

type patchNetworkIn struct {
	SandboxID string   `json:"sandbox_id" saga:"sandbox_id"`
	Addresses []string `json:"addresses" saga:"addresses"`
}

type patchNetworkOut struct {
	Revision int64 `json:"revision" saga:"revision"`
}

func patchNetwork(ctx context.Context, in patchNetworkIn) (patchNetworkOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)

	var networkAttrs []any
	networkAttrs = append(networkAttrs, entity.Ref(entity.DBId, entity.Id(in.SandboxID)))

	bridgeName := deps.networking.BridgeName()
	for _, addrStr := range in.Addresses {
		net := compute.Network{
			Address: addrStr,
			Subnet:  bridgeName,
		}
		networkAttrs = append(networkAttrs, entity.Component(compute.SandboxNetworkId, net.Encode()))
	}

	patchEnt := entity.New(networkAttrs...)
	revision, err := deps.entities.PatchSandbox(ctx, patchEnt.Attrs(), 0)
	if err != nil {
		return patchNetworkOut{}, fmt.Errorf("patching sandbox with network: %w", err)
	}

	return patchNetworkOut{Revision: revision}, nil
}

func undoPatchNetwork(_ context.Context, _ patchNetworkIn, _ patchNetworkOut) error {
	return nil
}

// --- Create container (includes build spec) ---

type createContainerIn struct {
	SandboxID string   `json:"sandbox_id" saga:"sandbox_id"`
	Addresses []string `json:"addresses" saga:"addresses"`
	Revision  int64    `json:"revision" saga:"revision"`
}

type createContainerOut struct {
	ContainerID string `json:"container_id" saga:"container_id"`
}

func createContainer(ctx context.Context, in createContainerIn) (createContainerOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	sb, meta, err := deps.entities.GetSandbox(ctx, in.SandboxID)
	if err != nil {
		return createContainerOut{}, fmt.Errorf("fetching sandbox: %w", err)
	}

	ep, err := deps.networking.RebuildEndpointConfig(in.Addresses)
	if err != nil {
		return createContainerOut{}, fmt.Errorf("rebuilding endpoint config: %w", err)
	}

	if in.Revision != 0 {
		meta.Revision = in.Revision
	}

	opts, err := deps.runtime.BuildSpec(ctx, sb, ep, meta)
	if err != nil {
		return createContainerOut{}, fmt.Errorf("building spec: %w", err)
	}

	cid, err := deps.runtime.CreateContainer(ctx, string(sb.ID), opts...)
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			log.Debug("saga: container already exists", "sandbox", in.SandboxID)
			return createContainerOut{ContainerID: pauseContainerId(sb.ID)}, nil
		}
		return createContainerOut{}, fmt.Errorf("creating container %s: %w", sb.ID, err)
	}

	log.Debug("saga: created container", "sandbox", in.SandboxID, "container", cid)
	return createContainerOut{ContainerID: cid}, nil
}

func undoCreateContainer(ctx context.Context, _ createContainerIn, out createContainerOut) error {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	if out.ContainerID == "" {
		return nil
	}

	container, err := deps.runtime.LoadContainer(ctx, out.ContainerID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			log.Debug("saga undo: container not found, already cleaned up", "id", out.ContainerID)
			return nil
		}
		return fmt.Errorf("saga undo: loading container %s: %w", out.ContainerID, err)
	}
	deps.runtime.CleanupContainer(ctx, container)
	log.Debug("saga undo: deleted container", "id", out.ContainerID)
	return nil
}

// --- Boot task ---

type bootTaskIn struct {
	SandboxID   string   `json:"sandbox_id" saga:"sandbox_id"`
	ContainerID string   `json:"container_id" saga:"container_id"`
	Addresses   []string `json:"addresses" saga:"addresses"`
}

type bootTaskOut struct {
	TaskPID int    `json:"task_pid" saga:"task_pid"`
	Cgroups string `json:"cgroups" saga:"cgroups"`
}

func bootTask(ctx context.Context, in bootTaskIn) (bootTaskOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	sb, meta, err := deps.entities.GetSandbox(ctx, in.SandboxID)
	if err != nil {
		return bootTaskOut{}, fmt.Errorf("fetching sandbox: %w", err)
	}

	ep, err := deps.networking.RebuildEndpointConfig(in.Addresses)
	if err != nil {
		return bootTaskOut{}, fmt.Errorf("rebuilding endpoint config: %w", err)
	}

	container, err := deps.runtime.LoadContainer(ctx, in.ContainerID)
	if err != nil {
		return bootTaskOut{}, fmt.Errorf("loading container: %w", err)
	}

	task, err := deps.runtime.BootInitialTask(ctx, sb, ep, container, meta.ShortId())
	if err != nil {
		return bootTaskOut{}, fmt.Errorf("booting task: %w", err)
	}

	rootSpec, err := deps.runtime.ContainerSpec(ctx, container)
	if err != nil {
		return bootTaskOut{}, fmt.Errorf("getting container spec: %w", err)
	}

	cgroups := rootSpec.Linux.CgroupsPath
	log.Debug("saga: booted task", "sandbox", in.SandboxID, "pid", task.Pid(), "cgroups", cgroups)
	return bootTaskOut{TaskPID: int(task.Pid()), Cgroups: cgroups}, nil
}

func undoBootTask(ctx context.Context, in bootTaskIn, _ bootTaskOut) error {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	sb, _, err := deps.entities.GetSandbox(ctx, in.SandboxID)
	if err == nil {
		deps.runtime.UnconfigureFirewall(sb)
	}

	container, err := deps.runtime.LoadContainer(ctx, in.ContainerID)
	if err != nil {
		log.Debug("saga undo: container not found for task kill", "id", in.ContainerID)
		return nil
	}

	task, err := container.Task(ctx, cleanupAttach())
	if err != nil {
		log.Debug("saga undo: task not found", "id", in.ContainerID)
		return nil
	}

	_, err = task.Delete(ctx, containerd.WithProcessKill)
	if err != nil {
		return fmt.Errorf("saga undo: killing task %s: %w", in.ContainerID, err)
	}
	log.Debug("saga undo: killed task", "id", in.ContainerID)
	return nil
}

// --- Boot containers ---

type bootContainersIn struct {
	SandboxID   string   `json:"sandbox_id" saga:"sandbox_id"`
	ContainerID string   `json:"container_id" saga:"container_id"`
	Addresses   []string `json:"addresses" saga:"addresses"`
	TaskPID     int      `json:"task_pid" saga:"task_pid"`
	Cgroups     string   `json:"cgroups" saga:"cgroups"`
	Revision    int64    `json:"revision" saga:"revision"`
}

type bootContainersOut struct {
	WaitPortIDs     []string          `json:"wait_port_ids" saga:"wait_port_ids"`
	WaitPortPorts   []int             `json:"wait_port_ports" saga:"wait_port_ports"`
	PortWaitTimeout string            `json:"port_wait_timeout" saga:"port_wait_timeout"`
	AllCgroups      map[string]string `json:"all_cgroups" saga:"all_cgroups"`
}

func bootContainers(ctx context.Context, in bootContainersIn) (bootContainersOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	sb, meta, err := deps.entities.GetSandbox(ctx, in.SandboxID)
	if err != nil {
		return bootContainersOut{}, fmt.Errorf("fetching sandbox: %w", err)
	}

	ep, err := deps.networking.RebuildEndpointConfig(in.Addresses)
	if err != nil {
		return bootContainersOut{}, fmt.Errorf("rebuilding endpoint config: %w", err)
	}

	cgroups := map[string]string{"": in.Cgroups}

	// Use meta from entity store, override revision from saga input if provided
	if in.Revision != 0 {
		meta.Revision = in.Revision
	}

	volumeMounts, err := deps.runtime.ConfigureVolumes(ctx, sb, meta)
	if err != nil {
		// Emit the failure on the sandbox's normal log stream so
		// operators see it via `miren logs sandbox <id>`. Volume-stage
		// failures happen before any container produces output, so the
		// log stream would otherwise be empty and the reason would only
		// be visible in journalctl on the node.
		deps.obs.LogSandboxEvent(sb, meta.ShortId(),
			fmt.Sprintf("volume configuration failed: %v", err))
		return bootContainersOut{}, fmt.Errorf("configuring volumes: %w", err)
	}

	waitPorts, err := deps.runtime.BootContainers(ctx, sb, ep, in.TaskPID, cgroups, meta, volumeMounts)
	if err != nil {
		return bootContainersOut{}, fmt.Errorf("booting containers: %w", err)
	}

	var wpIDs []string
	var wpPorts []int
	for _, wp := range waitPorts {
		wpIDs = append(wpIDs, wp.ID)
		wpPorts = append(wpPorts, wp.Port)
	}

	log.Debug("saga: booted containers", "sandbox", in.SandboxID, "ports", len(waitPorts))
	return bootContainersOut{
		WaitPortIDs:     wpIDs,
		WaitPortPorts:   wpPorts,
		PortWaitTimeout: sb.Spec.PortWaitTimeout,
		AllCgroups:      cgroups,
	}, nil
}

func undoBootContainers(ctx context.Context, in bootContainersIn, _ bootContainersOut) error {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	if err := deps.runtime.DestroySubContainers(ctx, entity.Id(in.SandboxID)); err != nil {
		return fmt.Errorf("saga undo: destroying subcontainers for %s: %w", in.SandboxID, err)
	}
	log.Debug("saga undo: destroyed subcontainers", "sandbox", in.SandboxID)

	// Booting the subcontainers is what registers the sandbox for token refresh
	// (see buildSubContainerSpec), so undoing it has to release that state. A
	// failed saga marks the sandbox DEAD without ever reaching StopSandbox.
	deps.runtime.ReleaseTokenState(entity.Id(in.SandboxID))

	if err := deps.runtime.ReleaseDiskLeases(ctx, entity.Id(in.SandboxID)); err != nil {
		return fmt.Errorf("saga undo: releasing disk leases for %s: %w", in.SandboxID, err)
	}

	// Same reasoning as the token state above: BootContainers creates a Hub for
	// every attachable container, and a failed saga never reaches StopSandbox,
	// which is the only other place they are torn down. A Hub outliving its
	// container is worse than a leak -- an attaching client finds it and waits
	// on output from a container that no longer exists.
	deps.runtime.ReleaseHubs(entity.Id(in.SandboxID))
	return nil
}

// --- Add metrics ---

type addMetricsIn struct {
	SandboxID  string            `json:"sandbox_id" saga:"sandbox_id"`
	AllCgroups map[string]string `json:"all_cgroups" saga:"all_cgroups"`
}

type addMetricsOut struct {
	LogEntity string `json:"log_entity" saga:"log_entity"`
}

func addMetrics(ctx context.Context, in addMetricsIn) (addMetricsOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)

	sb, _, err := deps.entities.GetSandbox(ctx, in.SandboxID)
	if err != nil {
		return addMetricsOut{}, fmt.Errorf("fetching sandbox: %w", err)
	}

	le, attrs := sandboxMetricsIdentity(sb)

	if err := deps.obs.AddMetrics(le, in.AllCgroups, attrs); err != nil {
		return addMetricsOut{}, fmt.Errorf("adding metrics: %w", err)
	}

	return addMetricsOut{LogEntity: le}, nil
}

func undoAddMetrics(ctx context.Context, _ addMetricsIn, out addMetricsOut) error {
	deps := saga.Get[*createSandboxDeps](ctx)
	if out.LogEntity != "" {
		deps.obs.RemoveMetrics(out.LogEntity)
	}
	return nil
}

// --- Wait ports ---

type waitPortsIn struct {
	SandboxID       string   `json:"sandbox_id" saga:"sandbox_id"`
	WaitPortIDs     []string `json:"wait_port_ids" saga:"wait_port_ids"`
	WaitPortPorts   []int    `json:"wait_port_ports" saga:"wait_port_ports"`
	PortWaitTimeout string   `json:"port_wait_timeout" saga:"port_wait_timeout"`
}

// observedPort records a port the app actually bound that differs from the one
// Miren configured. It is carried from waitPorts to setRunning so the divergence
// is persisted on the sandbox entity for routing.
type observedPort struct {
	Port    int    `json:"port"`
	Address string `json:"address"`
}

type waitPortsOut struct {
	PortsReady    saga.Edge      `saga:"ports_ready"`
	ObservedPorts []observedPort `json:"observed_ports" saga:"observed_ports"`
}

func waitPorts(ctx context.Context, in waitPortsIn) (waitPortsOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	portTimeout := resolvePortWaitTimeout(in.PortWaitTimeout)
	if len(in.WaitPortIDs) != len(in.WaitPortPorts) {
		return waitPortsOut{}, fmt.Errorf(
			"wait port ids/ports mismatch: %d != %d",
			len(in.WaitPortIDs), len(in.WaitPortPorts),
		)
	}

	var observed []observedPort
	var remapped bool
	for i := range in.WaitPortIDs {
		id := in.WaitPortIDs[i]
		port := in.WaitPortPorts[i]

		log.Info("saga: waiting for port", "sandbox", in.SandboxID, "port", port)
		err := deps.runtime.WaitForPort(ctx, id, port, portTimeout)
		if err == nil {
			continue // configured port bound — the normal case
		}
		if ctx.Err() != nil {
			// We're shutting down, not looking at a port mismatch — skip
			// diagnosis so we don't emit misleading "listening elsewhere" events.
			return waitPortsOut{}, err
		}

		// The configured port never bound within the timeout. See what the app
		// actually listened on: route to it if it bound a single other port, or
		// fail with a message that names the real port.
		routable, loopback, ok := deps.runtime.DiagnoseListening(id)
		if alt, single := singleAlternativePort(routable, port); ok && single {
			if remapped {
				// A second configured port diverged. Routing only follows one
				// observed port, so don't guess — fail loudly instead.
				msg := fmt.Sprintf("more than one configured port came up on a different port; "+
					"can't safely auto-route (latest was :%d)", alt)
				deps.logSandboxEvent(ctx, in.SandboxID, msg)
				return waitPortsOut{}, fmt.Errorf("port %d not reachable: %s", port, msg)
			}
			remapped = true
			log.Warn("saga: app bound a port other than the configured one; routing to it",
				"sandbox", in.SandboxID, "configured_port", port, "observed_port", alt)
			deps.logSandboxEvent(ctx, in.SandboxID, fmt.Sprintf(
				"app is listening on :%d, not the configured :%d; routing to :%d. "+
					"Set $PORT or [services.web] port to :%d to silence this.",
				alt, port, alt, alt))
			observed = append(observed, observedPort{Port: alt})
			continue
		}

		msg := describePortFailure(port, routable, loopback)
		deps.logSandboxEvent(ctx, in.SandboxID, msg)
		return waitPortsOut{}, fmt.Errorf("port %d not reachable: %s", port, msg)
	}

	return waitPortsOut{ObservedPorts: observed}, nil
}

// singleAlternativePort returns the lone routable port the app bound that isn't
// the configured one, and true only when exactly one such candidate exists.
// Ambiguity (zero or several alternatives) is left for describePortFailure.
func singleAlternativePort(routable []int, configured int) (int, bool) {
	var candidates []int
	for _, p := range routable {
		if p != configured {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return 0, false
}

// describePortFailure turns the observed listening state into an actionable
// message explaining why the configured port never became reachable.
func describePortFailure(configured int, routable, loopback []int) string {
	others := make([]int, 0, len(routable))
	for _, p := range routable {
		if p != configured {
			others = append(others, p)
		}
	}

	switch {
	case len(others) > 0:
		return fmt.Sprintf(
			"app is listening on %s but Miren expects :%d (set via $PORT or [services.web] port)",
			formatPorts(others), configured)
	case len(loopback) > 0:
		return fmt.Sprintf(
			"app is listening on loopback only (%s); bind 0.0.0.0 so Miren can reach it "+
				"(expected :%d via $PORT or [services.web] port)",
			formatPorts(loopback), configured)
	default:
		return fmt.Sprintf("nothing is listening on :%d after the port timeout", configured)
	}
}

func formatPorts(ports []int) string {
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = fmt.Sprintf(":%d", p)
	}
	return strings.Join(parts, ", ")
}

func undoWaitPorts(_ context.Context, _ waitPortsIn, _ waitPortsOut) error {
	return nil
}

// --- Set running ---

type setRunningIn struct {
	SandboxID     string         `json:"sandbox_id" saga:"sandbox_id"`
	PortsReady    saga.Edge      `saga:"ports_ready"`
	ObservedPorts []observedPort `json:"observed_ports" saga:"observed_ports"`
}

type setRunningOut struct{}

func setRunning(ctx context.Context, in setRunningIn) (setRunningOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	sb, meta, err := deps.entities.GetSandbox(ctx, in.SandboxID)
	if err != nil {
		return setRunningOut{}, fmt.Errorf("fetching sandbox status: %w", err)
	}
	if sb.Status == compute.DEAD || sb.Status == compute.STOPPED {
		log.Info("saga: sandbox already in terminal state",
			"id", in.SandboxID, "status", sb.Status)
		return setRunningOut{}, nil
	}

	// Patch only the fields we mean to change. Encoding a whole
	// compute.Sandbox{Status: RUNNING} would also emit defaults like
	// hostNetwork=false, clobbering real values on the entity.
	patchAttrs := entity.New(
		entity.Ref(entity.DBId, entity.Id(in.SandboxID)),
		func() []entity.Attr {
			attrs := []entity.Attr{
				entity.Ref(compute.SandboxStatusId, compute.SandboxStatusRunningId),
			}
			for _, op := range in.ObservedPorts {
				bp := compute.BoundPort{Port: int64(op.Port), Address: op.Address}
				attrs = append(attrs, entity.Component(compute.SandboxBoundPortId, bp.Encode()))
			}
			return attrs
		},
	)
	_, err = deps.entities.PatchSandbox(ctx, patchAttrs.Attrs(), meta.Revision)
	if err != nil {
		return setRunningOut{}, fmt.Errorf("setting status to RUNNING: %w", err)
	}

	log.Info("saga: sandbox set to RUNNING", "id", in.SandboxID)
	return setRunningOut{}, nil
}

func undoSetRunning(_ context.Context, _ setRunningIn, _ setRunningOut) error {
	// The controller marks the sandbox DEAD after saga failure;
	// the undo actions only handle resource cleanup.
	return nil
}

// --- Update services ---

type updateServicesIn struct {
	SandboxID  string    `json:"sandbox_id" saga:"sandbox_id"`
	Addresses  []string  `json:"addresses" saga:"addresses"`
	Revision   int64     `json:"revision" saga:"revision"`
	PortsReady saga.Edge `saga:"ports_ready"`
}

type updateServicesOut struct{}

func updateServices(ctx context.Context, in updateServicesIn) (updateServicesOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)

	sb, meta, err := deps.entities.GetSandbox(ctx, in.SandboxID)
	if err != nil {
		return updateServicesOut{}, fmt.Errorf("fetching sandbox: %w", err)
	}

	ep, err := deps.networking.RebuildEndpointConfig(in.Addresses)
	if err != nil {
		return updateServicesOut{}, fmt.Errorf("rebuilding endpoint config: %w", err)
	}

	if in.Revision != 0 {
		meta.Revision = in.Revision
	}

	if err := deps.obs.UpdateServices(ctx, sb, meta, ep); err != nil {
		return updateServicesOut{}, fmt.Errorf("updating services: %w", err)
	}

	return updateServicesOut{}, nil
}

func undoUpdateServices(_ context.Context, _ updateServicesIn, _ updateServicesOut) error {
	return nil
}

// registerCreateSandboxSaga registers the saga definition for sandbox creation.
func registerCreateSandboxSaga(
	registry *saga.Registry,
	entities SandboxEntityStore,
	networking SandboxNetworking,
	runtime SandboxContainerRuntime,
	obs SandboxObservability,
	log *slog.Logger,
) error {
	deps := &createSandboxDeps{
		entities:   entities,
		networking: networking,
		runtime:    runtime,
		obs:        obs,
	}

	return saga.Define(sagaCreateSandbox).
		Using(deps).
		Using(log).
		Action(actionAllocNetwork, allocNetwork).Undo(undoAllocNetwork).
		Action(actionPatchNetwork, patchNetwork).Undo(undoPatchNetwork).
		Action(actionCreateCtr, createContainer).Undo(undoCreateContainer).
		Action(actionBootTask, bootTask).Undo(undoBootTask).
		Action(actionBootCtrs, bootContainers).Undo(undoBootContainers).
		Action(actionAddMetrics, addMetrics).Undo(undoAddMetrics).
		Action(actionWaitPorts, waitPorts).Undo(undoWaitPorts).
		Action(actionSetRunning, setRunning).Undo(undoSetRunning).
		Action(actionUpdateSvcs, updateServices).Undo(undoUpdateServices).
		RegisterTo(registry)
}
