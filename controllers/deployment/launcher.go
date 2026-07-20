package deployment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"miren.dev/runtime/api/addon/addon_v1alpha"
	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/controller"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
)

var launcherTracer = otel.Tracer("miren.dev/runtime/deployment/launcher")

// Launcher watches App entities and proactively creates SandboxPools when ActiveVersion changes.
// This enables immediate startup for fixed-mode services and pool reuse across deployments.
type Launcher struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient

	// DataPath is the root data directory for local storage checks.
	DataPath string

	// PoolReadyTimeout is how long to wait for new pools to have ready instances
	// before proceeding with old pool cleanup. Defaults to 60s.
	PoolReadyTimeout time.Duration

	appMu sync.Map // per-app mutexes: app ID -> *sync.Mutex

	// warnedNoDisk tracks apps we've already warned about auto-mounting local
	// storage without a disk config, so the warning fires once per app instead
	// of on every reconcile tick. Keyed by app ID.
	warnedNoDisk sync.Map
}

// PoolWithEntity wraps a SandboxPool with its entity, allowing updates without re-fetching
type PoolWithEntity struct {
	Pool   *compute_v1alpha.SandboxPool
	Entity entity.Entity
}

func NewLauncher(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Launcher {
	return &Launcher{
		Log:              log.With("module", "deployment"),
		EAC:              eac,
		PoolReadyTimeout: 60 * time.Second,
	}
}

// CreatePoolForVersion creates a sandbox pool for the given version and service
// on demand. This implements activator.PoolCreator for ephemeral versions that
// bypass the normal DeploymentLauncher reconciliation loop.
func (l *Launcher) CreatePoolForVersion(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (entity.Id, error) {
	// Serialize against Reconcile (and other on-demand creates) for the same app.
	// Both paths do a findMatchingPool → create window; without a shared lock the
	// activator and the reconciler can each miss the other's in-flight pool and
	// both create one, duplicating every sandbox (MIR-1432, the tempo two-web-pools
	// finding). Keyed on app ID, identical to Reconcile.
	val, _ := l.appMu.LoadOrStore(ver.App, &sync.Mutex{})
	mu := val.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// Resolve the app entity
	var app core_v1alpha.App
	appResp, err := l.EAC.Get(ctx, ver.App.String())
	if err != nil {
		return "", fmt.Errorf("failed to get app %s: %w", ver.App, err)
	}
	app.Decode(appResp.Entity().Entity())
	app.ID = ver.App

	// Resolve config
	spec, err := coreutil.ResolveConfig(ctx, l.EAC, ver)
	if err != nil {
		return "", fmt.Errorf("failed to resolve config for version %s: %w", ver.Version, err)
	}
	l.injectAutoMountLocalDisks(spec, &app)

	poolID, err := l.ensurePoolForService(ctx, &app, ver, spec, service)
	if err != nil {
		return "", fmt.Errorf("failed to ensure pool for version %s service %s: %w", ver.Version, service, err)
	}

	return poolID, nil
}

func (l *Launcher) Init(ctx context.Context) error {
	l.Log.Info("deployment launcher initialized")
	return nil
}

func (l *Launcher) Reconcile(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	ctx, span := launcherTracer.Start(ctx, "launcher.reconcile",
		trace.WithAttributes(attribute.String("miren.app.id", app.ID.String())))
	defer span.End()

	// Serialize reconciles for the same app to avoid races between rapid deploys.
	val, _ := l.appMu.LoadOrStore(app.ID, &sync.Mutex{})
	mu := val.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	// Re-read from store to get latest state, coalescing rapid updates.
	// The controller framework embeds the entity snapshot at dispatch time,
	// so the app passed here may have a stale ActiveVersion if multiple
	// versions were created in quick succession.
	appResp, err := l.EAC.Get(ctx, app.ID.String())
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			l.Log.Debug("app not found in store, skipping", "app", app.ID)
			return nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to get app from store: %w", err)
	}

	var current core_v1alpha.App
	current.Decode(appResp.Entity().Entity())

	if current.ActiveVersion == "" {
		l.Log.Debug("app has no active version, skipping", "app", current.ID)
		return nil
	}

	span.SetAttributes(attribute.String("miren.app.active_version", current.ActiveVersion.String()))

	ready, err := l.addonsReady(ctx, current.ID)
	if err != nil {
		l.Log.Error("failed to check addon readiness", "app", current.ID, "error", err)
	} else if !ready {
		l.Log.Info("deferring pool creation, addons not yet ready", "app", current.ID)
		return nil
	}

	err = l.reconcileAppVersion(ctx, &current)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

// addonsReady returns true if the app has no pending or provisioning addon associations.
// Apps without any addons are always considered ready.
func (l *Launcher) addonsReady(ctx context.Context, appID entity.Id) (bool, error) {
	results, err := l.EAC.List(ctx, entity.Ref(addon_v1alpha.AddonAssociationAppId, appID))
	if err != nil {
		return false, fmt.Errorf("listing addon associations: %w", err)
	}

	for _, ent := range results.Values() {
		var assoc addon_v1alpha.AddonAssociation
		assoc.Decode(ent.Entity())

		if assoc.Status == "pending" || assoc.Status == "provisioning" {
			l.Log.Info("addon not ready", "association", assoc.ID, "status", assoc.Status)
			return false, nil
		}
	}

	return true, nil
}

// AddonAssociationHandler returns a controller.HandlerFunc that watches AddonAssociation
// changes and re-triggers app reconciliation so the launcher can re-evaluate addon readiness.
func (l *Launcher) AddonAssociationHandler() controller.HandlerFunc {
	return func(ctx context.Context, event controller.Event) ([]entity.Attr, error) {
		if event.Type == controller.EventDeleted || event.Entity == nil {
			return nil, nil
		}

		var assoc addon_v1alpha.AddonAssociation
		assoc.Decode(event.Entity)

		if assoc.App == "" {
			return nil, nil
		}

		l.Log.Info("addon association changed, reconciling app",
			"association", assoc.ID, "status", assoc.Status, "app", assoc.App)

		// Fetch the app and run the same reconcile logic
		appResp, err := l.EAC.Get(ctx, assoc.App.String())
		if err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to get app: %w", err)
		}

		var app core_v1alpha.App
		app.Decode(appResp.Entity().Entity())

		if app.ActiveVersion == "" {
			return nil, nil
		}

		ready, err := l.addonsReady(ctx, app.ID)
		if err != nil {
			l.Log.Error("failed to check addon readiness", "app", app.ID, "error", err)
			return nil, nil
		}
		if !ready {
			l.Log.Info("addons still not ready, deferring", "app", app.ID)
			return nil, nil
		}

		l.Log.Info("addons ready, triggering app reconciliation", "app", app.ID)
		return nil, l.reconcileAppVersion(ctx, &app)
	}
}

// reconcileAppVersion ensures pools exist for all services in the active version
func (l *Launcher) reconcileAppVersion(ctx context.Context, app *core_v1alpha.App) error {
	// Fetch the AppVersion entity
	verResp, err := l.EAC.Get(ctx, app.ActiveVersion.String())
	if err != nil {
		return fmt.Errorf("failed to get app version: %w", err)
	}

	var ver core_v1alpha.AppVersion
	ver.Decode(verResp.Entity().Entity())

	// Resolve config from ConfigVersion if available, otherwise use inline
	spec, err := coreutil.ResolveConfig(ctx, l.EAC, &ver)
	if err != nil {
		return fmt.Errorf("failed to resolve config: %w", err)
	}
	l.injectAutoMountLocalDisks(spec, app)

	// For each service, ensure a pool exists. Collect IDs of newly created pools.
	// ensuredServices tracks services whose replacement pool was successfully
	// ensured this pass; only those are eligible for stale-pool reaping below, so
	// we never scale down a service's only running pool when its replacement
	// failed to create.
	var newPoolIDs []entity.Id
	ensuredServices := make(map[string]bool)
	for _, svc := range spec.Services {
		// For services with disks, drain old pools before creating new ones.
		// Disks require exclusive access (local disks use flock, miren disks
		// use leases), so the old sandbox must release the disk before the
		// new one can mount it.
		if serviceHasDisks(spec, svc.Name) {
			if err := l.drainStaleDiskPools(ctx, app, &ver, spec, svc.Name); err != nil {
				l.Log.Error("failed to drain old disk pools, skipping service",
					"app", app.ID,
					"service", svc.Name,
					"error", err)
				continue
			}
		}

		poolID, err := l.ensurePoolForService(ctx, app, &ver, spec, svc.Name)
		if err != nil {
			l.Log.Error("failed to ensure pool for service",
				"app", app.ID,
				"service", svc.Name,
				"error", err)
			// Continue with other services even if one fails
			continue
		}
		ensuredServices[svc.Name] = true
		if poolID != "" {
			newPoolIDs = append(newPoolIDs, poolID)
		}
	}

	// Ensure Service entities for services with non-HTTP ports.
	// These are needed for L4 (TCP/UDP) traffic routing via ipalloc → ServiceController → nftables.
	appResp, err := l.EAC.Get(ctx, app.ID.String())
	if err != nil {
		return fmt.Errorf("failed to get app metadata for service entities: %w", err)
	}
	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	var desiredServices []string
	serviceSyncFailed := false
	for _, svc := range spec.Services {
		svcID, err := l.ensureServiceForPorts(ctx, app, &appMD, spec, svc.Name)
		if err != nil {
			l.Log.Error("failed to ensure service entity",
				"app", app.ID,
				"service", svc.Name,
				"error", err)
			serviceSyncFailed = true
			continue
		}
		if svcID != "" {
			desiredServices = append(desiredServices, svc.Name)
		}
	}

	if serviceSyncFailed {
		l.Log.Warn("skipping stale service cleanup due to ensure failures", "app", app.ID)
	} else {
		l.cleanupStaleServices(ctx, app, desiredServices)
	}

	// Wait for new pools to have ready instances before killing old ones.
	// This prevents a gap where the old sandbox is dead but the new one
	// isn't serving yet, which would cause 502s.
	for _, poolID := range newPoolIDs {
		if err := l.waitForPoolReady(ctx, poolID, l.PoolReadyTimeout); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				l.Log.Warn("timed out waiting for new pool to become ready, proceeding with cleanup",
					"pool", poolID,
					"error", err)
				continue
			}
			return fmt.Errorf("failed waiting for new pool readiness %s: %w", poolID, err)
		}
	}

	// Reap stateless pools whose baked spec no longer matches the desired spec.
	// cleanupOldVersionPools below keys on version references, so it never reaps
	// a superseded pool that still references the current version — which is
	// exactly what happens when a binary change (not a new AppVersion) alters the
	// baked spec, e.g. a runtime-injected env var rename. Those husks kept running
	// alongside their replacements indefinitely (MIR-1432). Disk services already
	// get spec-based reaping via drainStaleDiskPools before create; this covers
	// the stateless services after their replacements are ready. Only reap for
	// services whose replacement was successfully ensured, so a failed create
	// never leaves a service with its old pool scaled down and no replacement.
	reaped, err := l.reapStaleStatelessPools(ctx, app, &ver, spec, ensuredServices)
	if err != nil {
		l.Log.Error("failed to reap stale stateless pools",
			"app", app.ID,
			"version", ver.ID,
			"error", err)
		// Don't fail the entire reconciliation if reaping fails
	}

	// Clean up old version pools (pools not referenced by current version)
	cleaned, err := l.cleanupOldVersionPools(ctx, app, ver.ID)
	if err != nil {
		l.Log.Error("failed to cleanup old version pools",
			"app", app.ID,
			"version", ver.ID,
			"error", err)
		// Don't fail the entire reconciliation if cleanup fails
	}

	cleaned += reaped

	// Summarize only when this reconcile actually did something. The loop runs
	// constantly; a steady-state pass that created and cleaned up nothing stays
	// silent so the journal isn't buried in no-op narration.
	// pools_started counts pools we booted this pass: both brand-new pools and
	// drained pools revived for deploy verification (newPoolIDs holds both).
	if len(newPoolIDs) > 0 || cleaned > 0 {
		l.Log.Info("reconciled app version",
			"app", app.ID,
			"version", ver.Version,
			"pools_started", len(newPoolIDs),
			"pools_cleaned_up", cleaned)
	}

	return nil
}

// ensurePoolForService creates or reuses a pool for the given service.
// Returns the pool ID if a new pool was created (caller should wait for it
// to become ready), or an empty ID if an existing pool was reused.
func (l *Launcher) ensurePoolForService(ctx context.Context, app *core_v1alpha.App, ver *core_v1alpha.AppVersion, spec *core_v1alpha.ConfigSpec, serviceName string) (entity.Id, error) {
	// Get service config
	svcConcurrency, err := coreutil.GetServiceConcurrency(spec, serviceName)
	if err != nil {
		return "", fmt.Errorf("failed to get service concurrency: %w", err)
	}

	// Determine which image to use
	image := ver.ImageUrl
	for _, svc := range spec.Services {
		if svc.Name == serviceName && svc.Image != "" {
			image = containerdx.NormalizeImageReference(svc.Image)
			l.Log.Info("using custom image for service",
				"service", serviceName,
				"image", image,
				"original", svc.Image)
			break
		}
	}

	// Get app metadata for label
	appResp, err := l.EAC.Get(ctx, app.ID.String())
	if err != nil {
		return "", fmt.Errorf("failed to get app metadata: %w", err)
	}

	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	// Build the desired sandbox spec
	sbSpec, err := l.buildSandboxSpec(ctx, app, ver, spec, serviceName, image)
	if err != nil {
		return "", fmt.Errorf("failed to build sandbox spec: %w", err)
	}

	// Ask the strategy for the pool's enforced minimum. Strategies that don't
	// enforce a minimum (auto, ephemeral) return 0, in which case we still seed
	// the pool with 1 so the deploy boots a sandbox immediately. Fixed-mode
	// services return their configured NumInstances and the launcher honors it.
	sc := core_v1alpha.ServiceConcurrency(svcConcurrency)
	strategy := concurrency.NewStrategyForVersion(ver, serviceName, &sc)
	desiredInstances := int64(1)
	if min := int64(strategy.MinInstances()); min > 0 {
		desiredInstances = min
	}

	// Ephemeral versions always get an isolated pool. Sharing a pool with a
	// non-ephemeral version (or another ephemeral with a different label) would
	// mean the pool's scaling behavior depends on which version is making the
	// request, which is the kind of cross-strategy mixing we want to avoid.
	var poolWithEntity *PoolWithEntity
	if ver.EphemeralLabel == "" {
		poolWithEntity, err = l.findMatchingPool(ctx, app.ID, serviceName, sbSpec)
		if err != nil {
			return "", fmt.Errorf("failed to find matching pool: %w", err)
		}
	}

	if poolWithEntity != nil {
		// Reuse existing pool — sandboxes already running, no wait needed.
		// Steady-state reuse is a no-op; only log (below) when reuse actually
		// mutates the pool.
		needsUpdate := false

		// Update the pool's sandbox spec version to track the current AppVersion
		if poolWithEntity.Pool.SandboxSpec.Version != ver.ID {
			poolWithEntity.Pool.SandboxSpec.Version = ver.ID
			needsUpdate = true
		}

		// Add this version to referenced_by_versions if not already present
		if !containsRef(poolWithEntity.Pool.ReferencedByVersions, ver.ID) {
			poolWithEntity.Pool.ReferencedByVersions = append(poolWithEntity.Pool.ReferencedByVersions, ver.ID)
			needsUpdate = true
		}

		// A deploy is an explicit operator action — reset crash cooldown so
		// stale backoff from a previous version doesn't block the new one.
		if poolWithEntity.Pool.ConsecutiveCrashCount > 0 || !poolWithEntity.Pool.CooldownUntil.IsZero() {
			l.Log.Info("resetting crash cooldown on pool reuse",
				"pool", poolWithEntity.Pool.ID,
				"previous_crash_count", poolWithEntity.Pool.ConsecutiveCrashCount)
			poolWithEntity.Pool.ConsecutiveCrashCount = 0
			poolWithEntity.Pool.CooldownUntil = time.Time{}
			needsUpdate = true
		}

		// Fixed-mode strategies enforce their configured minimum.
		if min := int64(strategy.MinInstances()); min > 0 && poolWithEntity.Pool.DesiredInstances != min {
			poolWithEntity.Pool.DesiredInstances = min
			l.Log.Info("strategy-enforced minimum, updating desired instances",
				"service", serviceName,
				"desired_instances", min)
			needsUpdate = true
		}

		// A deploy should boot the new version so we can verify it, even when
		// reusing a pool that drained to zero (an autoscale app gone idle).
		// Floor a drained pool back to 1, the same way a fresh pool seeds; the
		// autoscaler scales it down again afterward. Without this, a redeploy of
		// a scaled-to-zero app would just point the pool at the new version
		// without ever booting it, so we'd never know if it actually works.
		bootForVerification := false
		if poolWithEntity.Pool.DesiredInstances < 1 {
			poolWithEntity.Pool.DesiredInstances = 1
			l.Log.Info("flooring drained pool to one instance for deploy verification",
				"service", serviceName,
				"pool", poolWithEntity.Pool.ID)
			needsUpdate = true
			bootForVerification = true
		}

		if needsUpdate {
			l.Log.Info("reusing existing pool, applying updates",
				"pool", poolWithEntity.Pool.ID,
				"service", serviceName,
				"app", app.ID)
			if err := l.updatePool(ctx, poolWithEntity); err != nil {
				return "", fmt.Errorf("failed to update pool: %w", err)
			}
		}

		// A revived drained pool has to boot before it's ready, so return its ID
		// to gate readiness like a fresh pool. Otherwise the existing pool is
		// already running and needs no wait.
		if bootForVerification {
			return poolWithEntity.Pool.ID, nil
		}
		return "", nil
	}

	// No matching pool found, create a new one
	l.Log.Info("creating new pool",
		"service", serviceName,
		"app", app.ID,
		"version", ver.Version)

	if desiredInstances > 1 {
		l.Log.Info("strategy-enforced minimum, starting with desired instances",
			"service", serviceName,
			"desired_instances", desiredInstances)
	}

	// Use app metadata (already fetched earlier) for sandbox labels
	pool := &compute_v1alpha.SandboxPool{
		App:                  app.ID,
		Service:              serviceName,
		SandboxSpec:          *sbSpec,
		DesiredInstances:     desiredInstances,
		ReferencedByVersions: []entity.Id{ver.ID},
		SandboxLabels: types.LabelSet(
			"app", appMD.Name,
		),
		SandboxPrefix: fmt.Sprintf("%s-%s", appMD.Name, serviceName),
	}

	name := idgen.GenNS("pool")
	id := entity.Id("pool/" + name)

	pr, err := l.EAC.Create(ctx, entity.New(
		(&core_v1alpha.Metadata{
			Name: name,
			Labels: types.LabelSet(
				"app", app.ID.String(),
				"version", ver.Version,
				"service", serviceName,
			),
		}).Encode,
		entity.DBId, entity.Id("pool/"+name),
		pool.Encode,
	).Attrs())
	if err != nil {
		return "", fmt.Errorf("failed to create pool entity: %w", err)
	}

	pool.ID = id
	l.Log.Info("created new pool",
		"pool", pool.ID,
		"pr-id", pr.Id(),
		"service", serviceName,
		"desired_instances", desiredInstances)

	// Return the new pool ID — caller should wait for it to become ready
	return id, nil
}

// buildSandboxSpec creates a SandboxSpec for the given service
func (l *Launcher) buildSandboxSpec(
	ctx context.Context,
	app *core_v1alpha.App,
	ver *core_v1alpha.AppVersion,
	cfgSpec *core_v1alpha.ConfigSpec,
	serviceName string,
	image string,
) (
	*compute_v1alpha.SandboxSpec,
	error,
) {
	// Get app metadata
	appResp, err := l.EAC.Get(ctx, app.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get app: %w", err)
	}

	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	sbSpec := &compute_v1alpha.SandboxSpec{
		Version:      ver.ID,
		LogEntity:    app.ID.String(),
		LogAttribute: types.LabelSet("miren.stage", "app-run", "miren.service", serviceName),
	}

	startDir := cfgSpec.StartDirectory
	if startDir == "" {
		startDir = "/app"
	}

	appEnv := appclient.RuntimeEnvWithAlias(appclient.EnvRuntimeApp, appMD.Name)
	appEnv = append(appEnv, appclient.RuntimeEnvWithAlias(appclient.EnvRuntimeVersion, ver.Version)...)

	appCont := compute_v1alpha.SandboxSpecContainer{
		Name:      "app",
		Image:     image,
		Env:       appEnv,
		Directory: startDir,
	}

	// Determine port configuration from service config
	var containerPorts []compute_v1alpha.SandboxSpecContainerPort
	portEnvValue := int64(0)
	shutdownTimeout := ""

	for _, svc := range cfgSpec.Services {
		if svc.Name == serviceName {
			if svc.Concurrency.ShutdownTimeout != "" {
				shutdownTimeout = svc.Concurrency.ShutdownTimeout
			}
			if svc.PortTimeout != "" {
				sbSpec.PortWaitTimeout = svc.PortTimeout
			}

			if len(svc.Ports) > 0 {
				// Multi-port path: map each port entry
				for _, p := range svc.Ports {
					portType := p.Type
					if portType == "" {
						portType = "http"
					}
					cp := compute_v1alpha.SandboxSpecContainerPort{
						Port:     p.Port,
						Name:     p.Name,
						Type:     portType,
						NodePort: p.NodePort,
					}
					switch p.Protocol {
					case core_v1alpha.ConfigSpecServicesPortsTCP:
						cp.Protocol = compute_v1alpha.SandboxSpecContainerPortTCP
					case core_v1alpha.ConfigSpecServicesPortsUDP:
						cp.Protocol = compute_v1alpha.SandboxSpecContainerPortUDP
					}
					containerPorts = append(containerPorts, cp)
				}

				// PORT env var: first HTTP-typed port, or first port if none is HTTP
				for _, cp := range containerPorts {
					if cp.Type == "http" {
						portEnvValue = cp.Port
						break
					}
				}
				if portEnvValue == 0 {
					portEnvValue = containerPorts[0].Port
				}

				if serviceName == "web" {
					hasHTTP := false
					for _, cp := range containerPorts {
						if cp.Type == "http" {
							hasHTTP = true
							break
						}
					}
					if !hasHTTP {
						containerPorts = append(containerPorts, compute_v1alpha.SandboxSpecContainerPort{
							Port: 3000, Name: "http", Type: "http",
						})
						portEnvValue = 3000
					}
				}
			} else {
				// Scalar port path (backward compat)
				port := svc.Port
				portName := svc.PortName
				portType := svc.PortType

				if port == 0 && serviceName == "web" {
					port = 3000
				}

				if port > 0 {
					if portName == "" {
						portName = "http"
					}
					if portType == "" {
						portType = "http"
					}
					containerPorts = []compute_v1alpha.SandboxSpecContainerPort{
						{Port: port, Name: portName, Type: portType},
					}
					portEnvValue = port
				}
			}
			break
		}
	}

	// Default to 3000 for web service if no service config matched at all
	if len(containerPorts) == 0 && serviceName == "web" {
		containerPorts = []compute_v1alpha.SandboxSpecContainerPort{
			{Port: 3000, Name: "http", Type: "http"},
		}
		portEnvValue = 3000
	}

	if len(containerPorts) > 0 {
		appCont.Port = containerPorts
	}

	// Add user-supplied config env vars, stripping any system-managed keys
	envMap := make(map[string]string)
	for _, x := range cfgSpec.Variables {
		if !isSystemEnvVar(x.Key) {
			envMap[x.Key] = x.Value
		}
	}

	// Find and merge per-service env vars (these override global vars)
	for _, svc := range cfgSpec.Services {
		if svc.Name == serviceName {
			for _, x := range svc.Env {
				if !isSystemEnvVar(x.Key) {
					envMap[x.Key] = x.Value
				}
			}
			break
		}
	}

	// Convert map to env var slice
	for k, v := range envMap {
		appCont.Env = append(appCont.Env, k+"="+v)
	}

	// Append system-managed env vars last so they cannot be overridden
	if portEnvValue > 0 {
		appCont.Env = append(appCont.Env, fmt.Sprintf("PORT=%d", portEnvValue))
	}
	if ver.AdminToken != "" {
		appCont.Env = append(appCont.Env, "ADMIN_TOKEN="+ver.AdminToken)
	}

	// Find service command
	for _, svc := range cfgSpec.Services {
		if svc.Name == serviceName && svc.Command != "" {
			if cfgSpec.Entrypoint != "" {
				appCont.Command = cfgSpec.Entrypoint + " " + svc.Command
			} else {
				appCont.Command = svc.Command
			}
			break
		}
	}

	// Add disk volumes and mounts for this service
	for _, svc := range cfgSpec.Services {
		if svc.Name == serviceName {
			// Pre-compute concurrency mode for miren disk eligibility check
			var skipMirenDisks bool
			hasMirenDisks := false
			for _, disk := range svc.Disks {
				if disk.Provider == "" || disk.Provider == core_v1alpha.ConfigSpecServicesDisksMIREN {
					hasMirenDisks = true
					break
				}
			}
			if hasMirenDisks {
				svcConcurrency, err := coreutil.GetServiceConcurrency(cfgSpec, serviceName)
				if err != nil {
					return nil, fmt.Errorf("failed to get service concurrency: %w", err)
				}

				if svcConcurrency.Mode != "fixed" {
					l.Log.Warn("skipping miren disk attachment for non-fixed service",
						"service", serviceName,
						"mode", svcConcurrency.Mode)
					skipMirenDisks = true
				}
			}

			for _, disk := range svc.Disks {
				var provider string
				switch disk.Provider {
				case core_v1alpha.ConfigSpecServicesDisksLOCAL:
					provider = "local"
				case core_v1alpha.ConfigSpecServicesDisksMIREN:
					provider = "miren"
				default:
					provider = "miren"
				}

				if skipMirenDisks && provider != "local" {
					continue
				}

				sbSpec.Volume = append(sbSpec.Volume, compute_v1alpha.SandboxSpecVolume{
					Name:         disk.Name,
					Provider:     provider,
					DiskName:     disk.Name,
					MountPath:    disk.MountPath,
					ReadOnly:     disk.ReadOnly,
					SizeGb:       disk.SizeGb,
					Filesystem:   disk.Filesystem,
					LeaseTimeout: disk.LeaseTimeout,
					Owner:        disk.Owner,
				})

				appCont.Mount = append(appCont.Mount, compute_v1alpha.SandboxSpecContainerMount{
					Source:      disk.Name,
					Destination: disk.MountPath,
				})
			}
			break
		}
	}

	if shutdownTimeout != "" {
		appCont.ShutdownTimeout = shutdownTimeout
	}

	sbSpec.Container = []compute_v1alpha.SandboxSpecContainer{appCont}

	return sbSpec, nil
}

// dirHasData returns true if the directory exists and contains at least one entry.
func dirHasData(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

// localDataMountPath is where the transitional local-storage auto-mount is
// exposed inside the container, and localDataDiskName is the disk/volume name
// it is registered under.
const (
	localDataMountPath = "/miren/data/local"
	localDataDiskName  = "local-data"
)

// injectAutoMountLocalDisks registers the transitional local-storage auto-mount
// as a real disk on the resolved config. For any service whose per-app host data
// directory already holds data but which declares no disk at the auto-mount path,
// it appends a "local" disk to the config. Doing this at config-resolution time
// (rather than patching the sandbox spec at build time) puts the app on the
// disk-aware deploy path: serviceHasDisks reports true, so drainStaleDiskPools
// reaps superseded pools instead of leaving a duplicate orphaned pool behind that
// keeps a live version reference and can strand ingress on a restart (MIR-1293).
//
// configureLocalVolume keys the on-disk location purely on the app ID, so naming
// the disk here does not move any existing data.
func (l *Launcher) injectAutoMountLocalDisks(spec *core_v1alpha.ConfigSpec, app *core_v1alpha.App) {
	if l.DataPath == "" {
		return
	}
	if !dirHasData(filepath.Join(l.DataPath, "data", "local", app.ID.String())) {
		return
	}

	for i := range spec.Services {
		svc := &spec.Services[i]

		// Skip if the service already declares an explicit local-provider disk
		// (all local volumes share the same per-app host directory, so any local
		// disk already exposes this data regardless of its mount path), a disk at
		// the legacy auto-mount path, or a disk under the name we would inject.
		// The last two also guard against a duplicate volume name in the spec.
		conflicts := false
		for _, d := range svc.Disks {
			if d.Provider == core_v1alpha.ConfigSpecServicesDisksLOCAL || d.MountPath == localDataMountPath || d.Name == localDataDiskName {
				conflicts = true
				break
			}
		}
		if conflicts {
			continue
		}

		// Warn once per app; this condition holds on every reconcile tick until
		// the app gets an explicit disk config, so logging each time would just
		// flood the journal.
		if _, warned := l.warnedNoDisk.LoadOrStore(app.ID, struct{}{}); !warned {
			l.Log.Warn("auto-mounting local storage for app with existing data but no disk config",
				"service", svc.Name,
				"app", app.ID)
		}

		svc.Disks = append(svc.Disks, core_v1alpha.ConfigSpecServicesDisks{
			Name:      localDataDiskName,
			Provider:  core_v1alpha.ConfigSpecServicesDisksLOCAL,
			MountPath: localDataMountPath,
		})
	}
}

// findMatchingPool searches for an existing pool with matching spec.
func (l *Launcher) findMatchingPool(ctx context.Context, appID entity.Id, serviceName string, desiredSpec *compute_v1alpha.SandboxSpec) (*PoolWithEntity, error) {
	// List all sandbox pools for this app
	poolsResp, err := l.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return nil, fmt.Errorf("failed to list pools: %w", err)
	}

	// Scan for matching pool
	for _, ent := range poolsResp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())

		// Check if this pool belongs to our app and service
		if pool.Service != serviceName {
			continue
		}

		// Get pool metadata to check app label
		var poolMeta core_v1alpha.Metadata
		poolMeta.Decode(ent.Entity())

		// Check if pool belongs to this app
		appLabel, _ := poolMeta.Labels.Get("app")
		if appLabel != appID.String() {
			continue
		}

		// Check if specs match
		reason, matches := specsMatch(&pool.SandboxSpec, desiredSpec)
		if matches {
			return &PoolWithEntity{
				Pool:   &pool,
				Entity: *ent.Entity(),
			}, nil
		}
		l.Log.Debug("Pool spec mismatch", "pool", pool.ID, "reason", reason)
	}

	return nil, nil
}

// specsMatch compares two SandboxSpecs, ignoring the version field
// Returns (reason, matches) where reason explains why specs don't match (empty if they do match)
func specsMatch(spec1, spec2 *compute_v1alpha.SandboxSpec) (string, bool) {
	// Quick checks first
	if len(spec1.Container) != len(spec2.Container) {
		return fmt.Sprintf("container count mismatch: %d vs %d", len(spec1.Container), len(spec2.Container)), false
	}

	// Compare containers
	for i := range spec1.Container {
		c1 := &spec1.Container[i]
		c2 := &spec2.Container[i]

		if c1.Name != c2.Name {
			return fmt.Sprintf("container[%d] name mismatch: %s vs %s", i, c1.Name, c2.Name), false
		}
		if c1.Image != c2.Image {
			return fmt.Sprintf("container[%d] image mismatch: %s vs %s", i, c1.Image, c2.Image), false
		}
		if c1.Command != c2.Command {
			return fmt.Sprintf("container[%d] command mismatch: %s vs %s", i, c1.Command, c2.Command), false
		}
		if c1.Directory != c2.Directory {
			return fmt.Sprintf("container[%d] directory mismatch: %s vs %s", i, c1.Directory, c2.Directory), false
		}
		if c1.ShutdownTimeout != c2.ShutdownTimeout {
			return fmt.Sprintf("container[%d] shutdown timeout mismatch: %s vs %s", i, c1.ShutdownTimeout, c2.ShutdownTimeout), false
		}

		// Compare env vars (order-independent)
		if !envVarsEqual(c1.Env, c2.Env) {
			return fmt.Sprintf("container[%d] environment variables mismatch", i), false
		}

		// Compare ports
		if !portsEqual(c1.Port, c2.Port) {
			return fmt.Sprintf("container[%d] ports mismatch", i), false
		}

		// Compare mounts
		if !mountsEqual(c1.Mount, c2.Mount) {
			return fmt.Sprintf("container[%d] mounts mismatch", i), false
		}
	}

	// Compare volumes
	if !volumesEqual(spec1.Volume, spec2.Volume) {
		return "volume mismatch", false
	}

	if spec1.PortWaitTimeout != spec2.PortWaitTimeout {
		return fmt.Sprintf("port wait timeout mismatch: %s vs %s", spec1.PortWaitTimeout, spec2.PortWaitTimeout), false
	}

	// All fields match (excluding version)
	return "", true
}

func volumesEqual(vols1, vols2 []compute_v1alpha.SandboxSpecVolume) bool {
	if len(vols1) != len(vols2) {
		return false
	}

	for i := range vols1 {
		v1 := &vols1[i]
		v2 := &vols2[i]

		if v1.Name != v2.Name ||
			v1.DiskName != v2.DiskName ||
			v1.Provider != v2.Provider ||
			v1.MountPath != v2.MountPath ||
			v1.SizeGb != v2.SizeGb ||
			v1.Filesystem != v2.Filesystem ||
			v1.ReadOnly != v2.ReadOnly ||
			v1.LeaseTimeout != v2.LeaseTimeout ||
			v1.Owner != v2.Owner {
			return false
		}

		if !labelsEqual(v1.Labels, v2.Labels) {
			return false
		}
	}

	return true
}

func labelsEqual(l1, l2 types.Labels) bool {
	if len(l1) != len(l2) {
		return false
	}

	for i := range l1 {
		if l1[i].Key != l2[i].Key || l1[i].Value != l2[i].Value {
			return false
		}
	}

	return true
}

func mountsEqual(mounts1, mounts2 []compute_v1alpha.SandboxSpecContainerMount) bool {
	if len(mounts1) != len(mounts2) {
		return false
	}

	for i := range mounts1 {
		if mounts1[i].Source != mounts2[i].Source ||
			mounts1[i].Destination != mounts2[i].Destination {
			return false
		}
	}

	return true
}

// envVarsEqual compares two env var slices in an order-independent way,
// ignoring the system env vars Miren injects per version — see filterSystemEnvVars.
func envVarsEqual(env1, env2 []string) bool {
	// Filter out system env vars
	filtered1 := filterSystemEnvVars(env1)
	filtered2 := filterSystemEnvVars(env2)

	if len(filtered1) != len(filtered2) {
		return false
	}

	// Build map for O(n) comparison
	envMap := make(map[string]bool)
	for _, e := range filtered1 {
		envMap[e] = true
	}

	for _, e := range filtered2 {
		if !envMap[e] {
			return false
		}
	}

	return true
}

// isSystemEnvVar returns true if the given key is a system-managed env var
// that user config must not override. The whole MIREN_ namespace is reserved
// (the injected MIREN_RUNTIME_* vars are enumerated in api/app/runtimeenv.go),
// so only the unprefixed names need explicit cases here.
func isSystemEnvVar(key string) bool {
	switch key {
	case "PORT", "ADMIN_TOKEN":
		return true
	}
	return appclient.IsReservedEnvVar(key)
}

// filterSystemEnvVars filters out system-managed env vars that shouldn't affect pool reuse.
// Unlike isSystemEnvVar this matches exact names rather than the MIREN_ prefix: a user's
// own MIREN_-prefixed var could never be set anyway, but a pool must still be reused across
// versions that differ only in the values Miren injects.
func filterSystemEnvVars(envVars []string) []string {
	skip := append([]string{"PORT", "ADMIN_TOKEN"}, appclient.RuntimeEnvNames...)
	// The deprecated pre-rename aliases are injected alongside the canonical names
	// (see api/app.RuntimeEnvWithAlias); skip them too so adding the aliases doesn't
	// invalidate every existing pool at once and trigger a mass recreation.
	skip = append(skip, appclient.LegacyRuntimeEnvNames...)

	filtered := []string{}
	for _, e := range envVars {
		if slices.ContainsFunc(skip, func(name string) bool {
			return strings.HasPrefix(e, name+"=")
		}) {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// portsEqual compares two port slices
func portsEqual(ports1, ports2 []compute_v1alpha.SandboxSpecContainerPort) bool {
	if len(ports1) != len(ports2) {
		return false
	}

	for i := range ports1 {
		p1 := &ports1[i]
		p2 := &ports2[i]

		if p1.Port != p2.Port ||
			p1.Name != p2.Name ||
			p1.Type != p2.Type ||
			p1.NodePort != p2.NodePort ||
			p1.Protocol != p2.Protocol {
			return false
		}
	}

	return true
}

// containsRef checks if a slice of refs contains a specific ref
func containsRef(refs []entity.Id, ref entity.Id) bool {
	for _, r := range refs {
		if r == ref {
			return true
		}
	}
	return false
}

// updatePool updates a pool entity in the store
func (l *Launcher) updatePool(ctx context.Context, poolWithEntity *PoolWithEntity) error {
	pool := poolWithEntity.Pool
	ent := poolWithEntity.Entity

	l.Log.Info("updating pool",
		"pool", pool.ID,
		"desired_instances", pool.DesiredInstances,
		"references", pool.ReferencedByVersions,
		"num_refs", len(pool.ReferencedByVersions))

	// Build new attributes from the pool
	newAttrs := pool.Encode()

	// Add critical fields that Encode() filters out
	// (Encode() uses entity.Empty() which filters out zero/empty values)

	// Always include DesiredInstances, even when 0 (for scale-down)
	if pool.DesiredInstances == 0 {
		newAttrs = append(newAttrs, entity.Int64(compute_v1alpha.SandboxPoolDesiredInstancesId, 0))
	}

	// Always include CurrentInstances, even when 0
	if pool.CurrentInstances == 0 {
		newAttrs = append(newAttrs, entity.Int64(compute_v1alpha.SandboxPoolCurrentInstancesId, 0))
	}

	// Always include ReadyInstances, even when 0
	if pool.ReadyInstances == 0 {
		newAttrs = append(newAttrs, entity.Int64(compute_v1alpha.SandboxPoolReadyInstancesId, 0))
	}

	// Always include crash cooldown fields when zeroed (e.g. after deploy reset)
	if pool.ConsecutiveCrashCount == 0 {
		newAttrs = append(newAttrs, entity.Int64(compute_v1alpha.SandboxPoolConsecutiveCrashCountId, 0))
	}
	if pool.LastCrashTime.IsZero() {
		newAttrs = append(newAttrs, entity.Time(compute_v1alpha.SandboxPoolLastCrashTimeId, time.Time{}))
	}
	if pool.CooldownUntil.IsZero() {
		newAttrs = append(newAttrs, entity.Time(compute_v1alpha.SandboxPoolCooldownUntilId, time.Time{}))
	}

	// Build the final attribute list: metadata from existing + new pool attrs
	finalAttrs := make([]entity.Attr, 0, len(ent.Attrs())+len(newAttrs))

	// Collect IDs we're replacing
	replacingIDs := make(map[entity.Id]bool)
	for _, attr := range newAttrs {
		replacingIDs[attr.ID] = true
	}
	// Always replace ReferencedByVersions (even if empty) since we're explicitly setting them.
	// Encode() won't emit attrs for an empty slice, but we still need to clear old refs.
	replacingIDs[compute_v1alpha.SandboxPoolReferencedByVersionsId] = true

	// Add existing attrs except those we're replacing
	for _, attr := range ent.Attrs() {
		if !replacingIDs[attr.ID] {
			finalAttrs = append(finalAttrs, attr)
		}
	}

	// Add all new attrs (including multi-valued ReferencedByVersions from Encode())
	finalAttrs = append(finalAttrs, newAttrs...)

	// Use Replace with the combined attributes (preserves metadata)
	_, err := l.EAC.Replace(ctx, finalAttrs, 0)
	if err != nil {
		return fmt.Errorf("failed to update pool: %w", err)
	}

	l.Log.Info("pool update successful", "pool", pool.ID)

	return nil
}

// waitForPoolReady polls until at least one sandbox in the pool is RUNNING, or the timeout expires.
// It checks both the pool's ReadyInstances field (updated by the pool controller on its
// ~10s resync cycle) and sandbox entities directly (updated immediately when sandboxes boot).
// This avoids a ~10s delay waiting for the pool controller to reconcile.
func (l *Launcher) waitForPoolReady(ctx context.Context, poolID entity.Id, timeout time.Duration) error {
	pollInterval := 500 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for {
		// Check pool's ReadyInstances first (fast path if pool controller already reconciled)
		resp, err := l.EAC.Get(ctx, poolID.String())
		if err != nil {
			return fmt.Errorf("failed to get pool %s: %w", poolID, err)
		}

		var pool compute_v1alpha.SandboxPool
		pool.Decode(resp.Entity().Entity())

		if pool.ReadyInstances > 0 {
			l.Log.Info("new pool has ready instances",
				"pool", poolID,
				"ready_instances", pool.ReadyInstances)
			return nil
		}

		// Check sandbox entities directly — they're updated immediately when
		// the sandbox boots, without waiting for the pool controller to reconcile.
		running, runErr := l.hasRunningSandboxForPool(ctx, poolID, pool.Service)
		if runErr != nil {
			l.Log.Warn("failed to query sandboxes while waiting for pool readiness",
				"pool", poolID,
				"error", runErr)
		} else if running {
			l.Log.Info("new pool has ready instances",
				"pool", poolID,
				"ready_instances", 1)
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("pool %s not ready after %s (ready_instances=%d, current_instances=%d): %w",
				poolID, timeout, pool.ReadyInstances, pool.CurrentInstances, context.DeadlineExceeded)
		}

		l.Log.Debug("waiting for pool to become ready",
			"pool", poolID,
			"ready_instances", pool.ReadyInstances,
			"current_instances", pool.CurrentInstances)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// hasRunningSandboxForPool checks if there is at least one RUNNING sandbox
// with the given pool label, by querying sandbox entities directly.
func (l *Launcher) hasRunningSandboxForPool(ctx context.Context, poolID entity.Id, service string) (bool, error) {
	resp, err := l.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return false, fmt.Errorf("list sandboxes: %w", err)
	}

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		if sb.Status != compute_v1alpha.RUNNING {
			continue
		}

		var md core_v1alpha.Metadata
		md.Decode(ent.Entity())

		poolLabel, _ := md.Labels.Get("pool")
		if poolLabel != poolID.String() {
			continue
		}

		serviceLabel, _ := md.Labels.Get("service")
		if serviceLabel != service {
			continue
		}

		return true, nil
	}

	return false, nil
}

// cleanupOldVersionPools removes old version references from pools and scales
// down unreferenced pools. It returns the number of pools it actually changed.
func (l *Launcher) cleanupOldVersionPools(ctx context.Context, app *core_v1alpha.App, currentVersionID entity.Id) (int, error) {
	// List all pools
	poolsResp, err := l.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return 0, fmt.Errorf("failed to list pools: %w", err)
	}

	cleaned := 0
	for _, ent := range poolsResp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())

		// Get pool metadata to check app label
		var poolMeta core_v1alpha.Metadata
		poolMeta.Decode(ent.Entity())

		// Check if pool belongs to this app
		appLabel, _ := poolMeta.Labels.Get("app")
		if appLabel != app.ID.String() {
			continue
		}

		// Check if this pool is being used by the current version
		isUsedByCurrentVersion := containsRef(pool.ReferencedByVersions, currentVersionID)

		if isUsedByCurrentVersion {
			// Pool is being reused by current version - keep ALL references.
			// Multiple versions may reference the same pool during rolling deployments.
			continue
		}

		// Pool is NOT being used by current version - remove old references and scale down
		updated := false

		for _, ref := range pool.ReferencedByVersions {
			// Remove any version references (they're all old versions since current version isn't using this pool)
			updated = true
			l.Log.Info("removing old version reference from unused pool",
				"pool", pool.ID,
				"service", pool.Service,
				"old_version", ref)
		}

		if !updated {
			continue
		}

		// Update pool with nil slice to ensure zero value is properly encoded
		// (empty slice []entity.Id{} might be filtered out by entity encoder)
		pool.ReferencedByVersions = nil

		// Scale to 0 since no versions reference this pool
		l.Log.Info("scaling down unreferenced pool",
			"pool", pool.ID,
			"service", pool.Service,
			"app", app.ID)
		pool.DesiredInstances = 0

		// Persist changes
		poolWithEntity := &PoolWithEntity{
			Pool:   &pool,
			Entity: *ent.Entity(),
		}
		err := l.updatePool(ctx, poolWithEntity)
		if err != nil {
			l.Log.Error("failed to update pool", "error", err, "pool", pool.ID)
			continue
		}
		cleaned++
	}

	return cleaned, nil
}

// ensureServiceForPorts creates or updates a network Service entity for a service
// that has non-HTTP ports. Returns the service entity ID if one was created/updated,
// or empty string if the service doesn't need a Service entity (all ports are HTTP).
func (l *Launcher) ensureServiceForPorts(ctx context.Context, app *core_v1alpha.App, appMD *core_v1alpha.Metadata, spec *core_v1alpha.ConfigSpec, serviceName string) (string, error) {
	// Find the service's ports from ConfigSpec.
	// Handles both the ports[] array and legacy scalar Port/PortName/PortType fields.
	var ports []core_v1alpha.ConfigSpecServicesPorts
	for _, svc := range spec.Services {
		if svc.Name == serviceName {
			ports = svc.Ports
			// Backfill from scalar fields when ports[] is empty
			if len(ports) == 0 && svc.Port > 0 {
				p := core_v1alpha.ConfigSpecServicesPorts{
					Port: svc.Port,
					Name: svc.PortName,
					Type: svc.PortType,
				}
				if p.Name == "" {
					p.Name = serviceName
				}
				if p.Type == "" {
					p.Type = "http"
				}
				ports = []core_v1alpha.ConfigSpecServicesPorts{p}
			}
			break
		}
	}

	// A Service entity is needed when the service has non-HTTP ports (for
	// cluster-IP load balancing) or when any port declares a NodePort (so the
	// service controller can install nftables DNAT rules on every node).
	needsService := false
	for _, p := range ports {
		if p.Type != "http" || p.NodePort != 0 {
			needsService = true
			break
		}
	}
	if !needsService {
		return "", nil
	}

	// Map ConfigSpec ports to network Service ports
	var svcPorts []network_v1alpha.Port
	for _, p := range ports {
		np := network_v1alpha.Port{
			Port:     p.Port,
			Name:     p.Name,
			Type:     p.Type,
			NodePort: p.NodePort,
		}
		switch p.Protocol {
		case core_v1alpha.ConfigSpecServicesPortsUDP:
			np.Protocol = network_v1alpha.UDP
		case core_v1alpha.ConfigSpecServicesPortsTCP:
			// TCP is also the default for an unspecified protocol.
			fallthrough
		default:
			np.Protocol = network_v1alpha.TCP
		}
		svcPorts = append(svcPorts, np)
	}

	svcEntityID := fmt.Sprintf("svc/%s-%s", appMD.Name, serviceName)

	existing, err := l.EAC.Get(ctx, svcEntityID)
	if err != nil {
		if !errors.Is(err, cond.ErrNotFound{}) {
			return "", fmt.Errorf("failed to get service entity %s: %w", svcEntityID, err)
		}

		// Not found — create new Service entity
		svc := &network_v1alpha.Service{
			Port: svcPorts,
			Match: types.LabelSet(
				"app", appMD.Name,
			),
		}

		_, err := l.EAC.Create(ctx, entity.New(
			(&core_v1alpha.Metadata{
				Name: fmt.Sprintf("%s-%s", appMD.Name, serviceName),
				Labels: types.LabelSet(
					"app", app.ID.String(),
					"service", serviceName,
					"managed-by", "launcher",
				),
			}).Encode,
			entity.DBId, entity.Id(svcEntityID),
			svc.Encode,
		).Attrs())
		if err != nil {
			return "", fmt.Errorf("failed to create service entity %s: %w", svcEntityID, err)
		}

		l.Log.Info("created service entity",
			"service_entity", svcEntityID,
			"app", app.ID,
			"service", serviceName,
			"ports", len(svcPorts))

		return svcEntityID, nil
	}

	// Found — check if ports changed
	var existingSvc network_v1alpha.Service
	existingSvc.Decode(existing.Entity().Entity())

	if servicePortsEqual(existingSvc.Port, svcPorts) {
		return svcEntityID, nil
	}

	// Ports changed — update the Service entity, preserving existing IPs
	updatedSvc := &network_v1alpha.Service{
		Ip:   existingSvc.Ip,
		Port: svcPorts,
		Match: types.LabelSet(
			"app", appMD.Name,
		),
	}

	newAttrs := updatedSvc.Encode()

	// Preserve existing entity attrs that we're not replacing
	replacingIDs := make(map[entity.Id]bool)
	for _, attr := range newAttrs {
		replacingIDs[attr.ID] = true
	}

	var finalAttrs []entity.Attr
	for _, attr := range existing.Entity().Entity().Attrs() {
		if !replacingIDs[attr.ID] {
			finalAttrs = append(finalAttrs, attr)
		}
	}
	finalAttrs = append(finalAttrs, newAttrs...)

	_, err = l.EAC.Replace(ctx, finalAttrs, 0)
	if err != nil {
		return "", fmt.Errorf("failed to update service entity %s: %w", svcEntityID, err)
	}

	l.Log.Info("updated service entity",
		"service_entity", svcEntityID,
		"app", app.ID,
		"service", serviceName,
		"ports", len(svcPorts))

	return svcEntityID, nil
}

// servicePortsEqual compares two network Service port slices for equality
func servicePortsEqual(a, b []network_v1alpha.Port) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Port != b[i].Port ||
			a[i].Name != b[i].Name ||
			a[i].Protocol != b[i].Protocol ||
			a[i].Type != b[i].Type ||
			a[i].NodePort != b[i].NodePort {
			return false
		}
	}
	return true
}

// cleanupStaleServices removes Service entities that are no longer needed.
// This handles: services removed from config, services changed from non-HTTP to HTTP-only,
// and all ports removed from a service.
func (l *Launcher) cleanupStaleServices(ctx context.Context, app *core_v1alpha.App, desiredServices []string) {
	results, err := l.EAC.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindService))
	if err != nil {
		l.Log.Error("failed to list service entities for cleanup", "error", err)
		return
	}

	desiredSet := make(map[string]bool)
	for _, s := range desiredServices {
		desiredSet[s] = true
	}

	for _, ent := range results.Values() {
		var meta core_v1alpha.Metadata
		meta.Decode(ent.Entity())

		managedBy, _ := meta.Labels.Get("managed-by")
		if managedBy != "launcher" {
			continue
		}

		appLabel, _ := meta.Labels.Get("app")
		if appLabel != app.ID.String() {
			continue
		}

		serviceLabel, _ := meta.Labels.Get("service")
		if desiredSet[serviceLabel] {
			continue
		}

		var svc network_v1alpha.Service
		svc.Decode(ent.Entity())

		l.Log.Info("deleting stale service entity",
			"service_entity", svc.ID,
			"app", app.ID,
			"service", serviceLabel)

		if _, err := l.EAC.Delete(ctx, svc.ID.String()); err != nil {
			l.Log.Error("failed to delete stale service entity",
				"service_entity", svc.ID,
				"error", err)
		}
	}
}

// serviceHasDisks returns true if the named service in the config spec has any
// disk mounts configured.
func serviceHasDisks(spec *core_v1alpha.ConfigSpec, serviceName string) bool {
	for _, svc := range spec.Services {
		if svc.Name == serviceName {
			return len(svc.Disks) > 0
		}
	}
	return false
}

// drainStaleDiskPools finds pools for the given app+service whose spec
// does not match the current desired spec, scales them to 0, and waits for
// their sandboxes to stop. This ensures disks are released before a new
// pool tries to mount them.
func (l *Launcher) drainStaleDiskPools(
	ctx context.Context,
	app *core_v1alpha.App,
	ver *core_v1alpha.AppVersion,
	cfgSpec *core_v1alpha.ConfigSpec,
	serviceName string,
) error {
	// Determine which image to use (same logic as ensurePoolForService)
	image := ver.ImageUrl
	for _, svc := range cfgSpec.Services {
		if svc.Name == serviceName && svc.Image != "" {
			image = containerdx.NormalizeImageReference(svc.Image)
			break
		}
	}

	desiredSpec, err := l.buildSandboxSpec(ctx, app, ver, cfgSpec, serviceName, image)
	if err != nil {
		return fmt.Errorf("build sandbox spec: %w", err)
	}

	stalePools, err := l.findStalePoolsForService(ctx, app.ID, serviceName, desiredSpec)
	if err != nil {
		return fmt.Errorf("find stale pools: %w", err)
	}

	if len(stalePools) == 0 {
		return nil
	}

	l.Log.Info("draining stale disk pools before creating new pool",
		"app", app.ID,
		"service", serviceName,
		"stale_pools", len(stalePools))

	for _, pwe := range stalePools {
		pool := pwe.Pool

		// Remove version references and scale to 0
		pool.ReferencedByVersions = nil
		pool.DesiredInstances = 0

		l.Log.Info("scaling down stale disk pool",
			"pool", pool.ID,
			"service", pool.Service)

		if err := l.updatePool(ctx, pwe); err != nil {
			return fmt.Errorf("update pool %s: %w", pool.ID, err)
		}
	}

	// Wait for all stale pools to fully drain so disk leases are released.
	for _, pwe := range stalePools {
		if err := l.waitForPoolDrained(ctx, pwe.Pool.ID, pwe.Pool.Service, l.PoolReadyTimeout); err != nil {
			return fmt.Errorf("waiting for pool %s to drain: %w", pwe.Pool.ID, err)
		}
	}

	return nil
}

// reapStaleStatelessPools scales to 0 any pool for a stateless service whose
// baked spec no longer matches the desired spec. It is the stateless twin of
// drainStaleDiskPools, but runs *after* the new pool is ready rather than before
// create: stateless pools hold no exclusive lease, so we want create → wait →
// reap to avoid a serving gap, and there's no drain to block on.
//
// Disk services are skipped here — they already went through drainStaleDiskPools
// before create. Only services present in ensuredServices are reaped: if a
// service's replacement pool failed to be ensured this pass, we leave its old
// pool running rather than scale down the only instance with no replacement.
// Returns the number of pools it scaled down.
func (l *Launcher) reapStaleStatelessPools(
	ctx context.Context,
	app *core_v1alpha.App,
	ver *core_v1alpha.AppVersion,
	cfgSpec *core_v1alpha.ConfigSpec,
	ensuredServices map[string]bool,
) (int, error) {
	reaped := 0
	for _, svc := range cfgSpec.Services {
		if serviceHasDisks(cfgSpec, svc.Name) {
			continue
		}
		if !ensuredServices[svc.Name] {
			continue
		}

		// Determine which image to use (same logic as ensurePoolForService)
		image := ver.ImageUrl
		if svc.Image != "" {
			image = containerdx.NormalizeImageReference(svc.Image)
		}

		desiredSpec, err := l.buildSandboxSpec(ctx, app, ver, cfgSpec, svc.Name, image)
		if err != nil {
			return reaped, fmt.Errorf("build sandbox spec for service %s: %w", svc.Name, err)
		}

		stalePools, err := l.findStalePoolsForService(ctx, app.ID, svc.Name, desiredSpec)
		if err != nil {
			return reaped, fmt.Errorf("find stale pools for service %s: %w", svc.Name, err)
		}

		for _, pwe := range stalePools {
			pool := pwe.Pool

			// findStalePoolsForService keeps returning a pool while its sandboxes
			// drain (the disk path re-waits on it). We don't wait, so a pool that's
			// already at the target state needs no further action — skipping it
			// avoids re-logging and re-writing the same husk every tick until its
			// sandboxes finish stopping.
			if pool.DesiredInstances == 0 && len(pool.ReferencedByVersions) == 0 {
				continue
			}

			// Remove version references and scale to 0. Clearing refs also keeps
			// cleanupOldVersionPools from redundantly touching the same pool.
			pool.ReferencedByVersions = nil
			pool.DesiredInstances = 0

			l.Log.Info("reaping stale stateless pool",
				"pool", pool.ID,
				"service", pool.Service,
				"app", app.ID)

			if err := l.updatePool(ctx, pwe); err != nil {
				return reaped, fmt.Errorf("update pool %s: %w", pool.ID, err)
			}
			reaped++
		}
	}

	return reaped, nil
}

// findStalePoolsForService returns pools for the given app+service whose spec
// does NOT match the desired spec. These are old-version pools that should be
// drained before starting new sandboxes.
func (l *Launcher) findStalePoolsForService(
	ctx context.Context,
	appID entity.Id,
	serviceName string,
	desiredSpec *compute_v1alpha.SandboxSpec,
) ([]*PoolWithEntity, error) {
	poolsResp, err := l.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return nil, fmt.Errorf("list pools: %w", err)
	}

	var stale []*PoolWithEntity
	for _, ent := range poolsResp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())

		if pool.Service != serviceName {
			continue
		}

		var poolMeta core_v1alpha.Metadata
		poolMeta.Decode(ent.Entity())

		appLabel, _ := poolMeta.Labels.Get("app")
		if appLabel != appID.String() {
			continue
		}

		// Skip pools that already match the desired spec
		if _, matches := specsMatch(&pool.SandboxSpec, desiredSpec); matches {
			continue
		}

		// Skip pools already scaled to 0 with no references AND no active
		// sandboxes. A previous drain attempt may have set DesiredInstances=0
		// but timed out before all sandboxes stopped — we still need to wait
		// for those to finish.
		if pool.DesiredInstances == 0 && len(pool.ReferencedByVersions) == 0 {
			hasActive, err := l.hasActiveSandboxForPool(ctx, pool.ID, serviceName)
			if err != nil {
				return nil, fmt.Errorf("check active sandboxes for pool %s: %w", pool.ID, err)
			}
			if !hasActive {
				continue
			}
		}

		entCopy := *ent.Entity()
		stale = append(stale, &PoolWithEntity{Pool: &pool, Entity: entCopy})
	}

	return stale, nil
}

// waitForPoolDrained polls until a pool has no RUNNING or PENDING sandboxes,
// indicating that its resources (including disk leases) have been released.
func (l *Launcher) waitForPoolDrained(ctx context.Context, poolID entity.Id, service string, timeout time.Duration) error {
	pollInterval := 500 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for {
		hasActive, err := l.hasActiveSandboxForPool(ctx, poolID, service)
		if err != nil {
			return fmt.Errorf("check sandboxes for pool %s: %w", poolID, err)
		}

		if !hasActive {
			l.Log.Info("stale pool fully drained", "pool", poolID)
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("pool %s still has active sandboxes after %s: %w",
				poolID, timeout, context.DeadlineExceeded)
		}

		l.Log.Debug("waiting for stale pool to drain",
			"pool", poolID)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// hasActiveSandboxForPool returns true if there are any RUNNING, PENDING, or
// NOT_READY sandboxes for the given pool. NOT_READY sandboxes may still hold
// disk leases, so we must wait for them to fully stop.
func (l *Launcher) hasActiveSandboxForPool(ctx context.Context, poolID entity.Id, service string) (bool, error) {
	resp, err := l.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return false, fmt.Errorf("list sandboxes: %w", err)
	}

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		switch sb.Status {
		case compute_v1alpha.RUNNING, compute_v1alpha.PENDING, compute_v1alpha.NOT_READY:
			// Active — may still hold disk resources
		case compute_v1alpha.STOPPED, compute_v1alpha.DEAD:
			// Terminal — no longer holds resources.
			fallthrough
		default:
			continue
		}

		var md core_v1alpha.Metadata
		md.Decode(ent.Entity())

		poolLabel, _ := md.Labels.Get("pool")
		if poolLabel != poolID.String() {
			continue
		}

		serviceLabel, _ := md.Labels.Get("service")
		if serviceLabel != service {
			continue
		}

		return true, nil
	}

	return false, nil
}
