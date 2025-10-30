package deployment

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
)

// Launcher watches App entities and proactively creates SandboxPools when ActiveVersion changes.
// This enables immediate startup for fixed-mode services and pool reuse across deployments.
type Launcher struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient
}

func NewLauncher(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Launcher {
	return &Launcher{
		Log: log.With("controller", "deploymentlauncher"),
		EAC: eac,
	}
}

func (l *Launcher) Init(ctx context.Context) error {
	l.Log.Info("deployment launcher initialized")
	return nil
}

func (l *Launcher) Reconcile(ctx context.Context, app *core_v1alpha.App, meta *entity.Meta) error {
	if app.ActiveVersion == "" {
		l.Log.Debug("app has no active version, skipping", "app", app.ID)
		return nil
	}

	l.Log.Info("reconciling app", "app", app.ID, "version", app.ActiveVersion)
	return l.reconcileAppVersion(ctx, app)
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

	l.Log.Info("reconciling app version",
		"app", app.ID,
		"version", ver.Version,
		"services", len(ver.Config.Services))

	// For each service, ensure a pool exists
	for _, svc := range ver.Config.Services {
		if err := l.ensurePoolForService(ctx, app, &ver, svc.Name); err != nil {
			l.Log.Error("failed to ensure pool for service",
				"app", app.ID,
				"service", svc.Name,
				"error", err)
			// Continue with other services even if one fails
			continue
		}
	}

	// TODO: Clean up old version pools (pools not referenced by current version)
	// For now, we'll rely on SandboxPoolManager's existing cleanup logic

	return nil
}

// ensurePoolForService creates or reuses a pool for the given service
func (l *Launcher) ensurePoolForService(ctx context.Context, app *core_v1alpha.App, ver *core_v1alpha.AppVersion, serviceName string) error {
	// Get service config
	svcConcurrency, err := core_v1alpha.GetServiceConcurrency(ver, serviceName)
	if err != nil {
		return fmt.Errorf("failed to get service concurrency: %w", err)
	}

	// Determine which image to use
	image := ver.ImageUrl
	for _, svc := range ver.Config.Services {
		if svc.Name == serviceName && svc.Image != "" {
			image = svc.Image
			l.Log.Info("using custom image for service",
				"service", serviceName,
				"image", image)
			break
		}
	}

	// Build the desired sandbox spec
	spec, err := l.buildSandboxSpec(ctx, app, ver, serviceName, image)
	if err != nil {
		return fmt.Errorf("failed to build sandbox spec: %w", err)
	}

	// Try to find existing pool with matching spec
	existingPool, err := l.findMatchingPool(ctx, app.ID, serviceName, spec)
	if err != nil {
		return fmt.Errorf("failed to find matching pool: %w", err)
	}

	if existingPool != nil {
		// Reuse existing pool
		l.Log.Info("reusing existing pool",
			"pool", existingPool.ID,
			"service", serviceName,
			"app", app.ID)

		// Add this version to referenced_by_versions if not already present
		if !containsRef(existingPool.ReferencedByVersions, ver.ID) {
			existingPool.ReferencedByVersions = append(existingPool.ReferencedByVersions, ver.ID)
			if err := l.updatePool(ctx, existingPool); err != nil {
				return fmt.Errorf("failed to update pool references: %w", err)
			}
		}

		return nil
	}

	// No matching pool found, create a new one
	l.Log.Info("creating new pool",
		"service", serviceName,
		"app", app.ID,
		"version", ver.Version)

	// Determine initial desired_instances based on mode
	desiredInstances := int64(0) // Default: auto mode starts at 0
	if svcConcurrency.Mode == "fixed" && svcConcurrency.NumInstances > 0 {
		desiredInstances = svcConcurrency.NumInstances
		l.Log.Info("fixed mode service, starting with desired instances",
			"service", serviceName,
			"desired_instances", desiredInstances)
	}

	pool := &compute_v1alpha.SandboxPool{
		Service:               serviceName,
		SandboxSpec:           *spec,
		DesiredInstances:      desiredInstances,
		ReferencedByVersions:  []entity.Id{ver.ID},
	}

	name := idgen.GenNS("pool")

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name: name,
			Labels: types.LabelSet(
				"app", app.ID.String(),
				"version", ver.Version,
				"service", serviceName,
			),
		}).Encode,
		entity.Ident, "pool/"+name,
		pool.Encode,
	).Attrs())

	pr, err := l.EAC.Put(ctx, &rpcE)
	if err != nil {
		return fmt.Errorf("failed to create pool entity: %w", err)
	}

	pool.ID = entity.Id(pr.Id())
	l.Log.Info("created new pool",
		"pool", pool.ID,
		"service", serviceName,
		"desired_instances", desiredInstances)

	return nil
}

// buildSandboxSpec creates a SandboxSpec for the given service
func (l *Launcher) buildSandboxSpec(ctx context.Context, app *core_v1alpha.App, ver *core_v1alpha.AppVersion, serviceName string, image string) (*compute_v1alpha.SandboxSpec, error) {
	// Get app metadata
	appResp, err := l.EAC.Get(ctx, app.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get app: %w", err)
	}

	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	spec := &compute_v1alpha.SandboxSpec{
		Version:      ver.ID,
		LogEntity:    app.ID.String(),
		LogAttribute: types.LabelSet("stage", "app-run", "service", serviceName),
	}

	// Determine port from config or default to 3000
	port := int64(3000)
	if ver.Config.Port > 0 {
		port = ver.Config.Port
	}

	appCont := compute_v1alpha.SandboxSpecContainer{
		Name:  "app",
		Image: image,
		Env: []string{
			"MIREN_APP=" + appMD.Name,
			"MIREN_VERSION=" + ver.Version,
		},
		Directory: "/app",
		Port: []compute_v1alpha.SandboxSpecContainerPort{
			{
				Port: port,
				Name: "http",
				Type: "http",
			},
		},
	}

	// Add config env vars
	for _, x := range ver.Config.Variable {
		appCont.Env = append(appCont.Env, x.Key+"="+x.Value)
	}

	// Find service command
	for _, s := range ver.Config.Commands {
		if s.Service == serviceName && s.Command != "" {
			if ver.Config.Entrypoint != "" {
				appCont.Command = ver.Config.Entrypoint + " " + s.Command
			} else {
				appCont.Command = s.Command
			}
			break
		}
	}

	spec.Container = []compute_v1alpha.SandboxSpecContainer{appCont}
	return spec, nil
}

// findMatchingPool searches for an existing pool with matching spec
func (l *Launcher) findMatchingPool(ctx context.Context, appID entity.Id, serviceName string, desiredSpec *compute_v1alpha.SandboxSpec) (*compute_v1alpha.SandboxPool, error) {
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
		if l.specsMatch(&pool.SandboxSpec, desiredSpec) {
			return &pool, nil
		}
	}

	return nil, nil
}

// specsMatch compares two SandboxSpecs, ignoring the version field
func (l *Launcher) specsMatch(spec1, spec2 *compute_v1alpha.SandboxSpec) bool {
	// Quick checks first
	if len(spec1.Container) != len(spec2.Container) {
		return false
	}

	// Compare containers
	for i := range spec1.Container {
		c1 := &spec1.Container[i]
		c2 := &spec2.Container[i]

		if c1.Name != c2.Name ||
			c1.Image != c2.Image ||
			c1.Command != c2.Command ||
			c1.Directory != c2.Directory {
			return false
		}

		// Compare env vars (order-independent)
		if !envVarsEqual(c1.Env, c2.Env) {
			return false
		}

		// Compare ports
		if !portsEqual(c1.Port, c2.Port) {
			return false
		}
	}

	// All fields match (excluding version)
	return true
}

// envVarsEqual compares two env var slices in an order-independent way
func envVarsEqual(env1, env2 []string) bool {
	if len(env1) != len(env2) {
		return false
	}

	// Build map for O(n) comparison
	envMap := make(map[string]bool)
	for _, e := range env1 {
		envMap[e] = true
	}

	for _, e := range env2 {
		if !envMap[e] {
			return false
		}
	}

	return true
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
			p1.Type != p2.Type {
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
func (l *Launcher) updatePool(ctx context.Context, pool *compute_v1alpha.SandboxPool) error {
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetId(pool.ID.String())
	rpcE.SetAttrs(entity.New(pool.Encode).Attrs())

	_, err := l.EAC.Put(ctx, &rpcE)
	if err != nil {
		return fmt.Errorf("failed to update pool: %w", err)
	}

	return nil
}
