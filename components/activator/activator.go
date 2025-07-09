// Package activator manages application sandbox lifecycle and capacity.
//
// Architecture overview:
// - HTTPIngress requests leases from the activator to handle incoming requests
// - The activator maintains a pool of sandboxes for each app version
// - Leases represent reserved capacity (slots) within a sandbox
// - Slot-based concurrency control respects requests_per_instance limits
// - Background tasks handle sandbox retirement and min_instances enforcement
//
// Key flows:
// 1. Request arrives at HTTPIngress → tries existing lease → acquires new lease if needed
// 2. Activator finds sandbox with capacity → reserves slots → returns lease with direct URL
// 3. HTTPIngress forwards request to sandbox URL and tracks lease usage
// 4. Periodic lease renewal prevents sandbox retirement during active use
package activator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
)

// DefaultPool is the default pool name for HTTP services
const DefaultPool = "http"

// DefaultService is the default service name for HTTP services
const DefaultService = "web"

// Lease represents a claim on a portion of a sandbox's capacity.
// HTTPIngress acquires leases from the activator and tracks their usage.
type Lease struct {
	ver     *core_v1alpha.AppVersion
	sandbox *compute_v1alpha.Sandbox
	ent     *entity.Entity
	pool    string

	Size int    // Number of concurrent request slots this lease reserves
	URL  string // Direct URL to reach the sandbox
}

func (l *Lease) Version() *core_v1alpha.AppVersion {
	return l.ver
}

func (l *Lease) Sandbox() *compute_v1alpha.Sandbox {
	return l.sandbox
}

func (l *Lease) SandboxEntity() *entity.Entity {
	return l.ent
}

func (l *Lease) Pool() string {
	return l.pool
}

// AppActivator manages the lifecycle of application sandboxes and provides leases for accessing them.
// It handles scaling up/down based on demand and respects concurrency limits.
type AppActivator interface {
	// AcquireLease finds or creates a sandbox with available capacity and reserves slots
	AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, pool, service string) (*Lease, error)
	// ReleaseLease returns reserved slots back to the sandbox's available capacity
	ReleaseLease(ctx context.Context, lease *Lease) error
	// RenewLease updates the last renewal time to prevent sandbox retirement
	RenewLease(ctx context.Context, lease *Lease) (*Lease, error)
	// Deploy handles deployment of a new app version, ensuring min instances and cleaning up old versions
	Deploy(ctx context.Context, app *core_v1alpha.App, ver *core_v1alpha.AppVersion, pool string) error
}

// sandbox tracks a running instance and its capacity utilization.
// The slot system manages concurrent request handling:
// - maxSlots=0 means unlimited capacity (requests_per_instance not set)
// - maxSlots>0 enforces a specific concurrency limit
type sandbox struct {
	sandbox     *compute_v1alpha.Sandbox
	ent         *entity.Entity
	lastRenewal time.Time // Used to determine when to retire idle sandboxes
	url         string
	maxSlots    int // Total concurrent request capacity (0 = unlimited)
	inuseSlots  int // Currently reserved slots across all leases
}

// verSandboxes groups all sandboxes for a specific app version.
// It tracks the standard lease size for consistent slot allocation.
type verSandboxes struct {
	ver       *core_v1alpha.AppVersion
	sandboxes []*sandbox

	leaseSlots int // Standard slot count per lease (0 when requests_per_instance=0)
}

type verKey struct {
	ver, pool string
}

type localActivator struct {
	mu       sync.Mutex
	versions map[verKey]*verSandboxes

	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
	ec  *entityserver.Client
}

var _ AppActivator = (*localActivator)(nil)

func NewLocalActivator(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) AppActivator {
	la := &localActivator{
		log:      log.With("module", "activator"),
		eac:      eac,
		ec:       entityserver.NewClient(log, eac),
		versions: make(map[verKey]*verSandboxes),
	}

	go la.InBackground(ctx)

	return la
}

// AcquireLease implements the core logic for finding available capacity:
// 1. Try to find an existing sandbox with available slots
// 2. If none available, create a new sandbox (respecting max_instances)
// 3. Reserve slots and return a lease
func (a *localActivator) AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, pool, service string) (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := verKey{ver.ID.String(), pool}
	vs, ok := a.versions[key]

	if !ok {
		a.log.Info("creating new sandbox for app", "app", ver.App, "version", ver.Version, "pool", pool, "key", key)
		// Check max_instances before creating first sandbox
		maxInstances := int(ver.Config.Concurrency.MaxInstances)
		if maxInstances > 0 && 1 > maxInstances {
			return nil, fmt.Errorf("cannot create sandbox: would exceed max_instances limit of %d", maxInstances)
		}
		return a.activateApp(ctx, ver, pool, service)
	}

	if len(vs.sandboxes) == 0 {
		a.log.Info("no sandboxes available, creating new sandbox for app", "app", ver.App, "version", ver.Version)
	} else {

		a.log.Debug("reusing existing sandboxes", "app", ver.App, "version", ver.Version, "sandboxes", len(vs.sandboxes))

		start := rand.Int() % len(vs.sandboxes)

		for i := 0; i < len(vs.sandboxes); i++ {
			s := vs.sandboxes[(start+i)%len(vs.sandboxes)]
			if s.inuseSlots+vs.leaseSlots <= s.maxSlots {
				s.inuseSlots += vs.leaseSlots

				a.log.Debug("reusing sandbox", "app", ver.App, "version", ver.Version, "sandbox", s.sandbox.ID, "in-use", s.inuseSlots)
				return &Lease{
					ver:     ver,
					sandbox: s.sandbox,
					ent:     s.ent,
					Size:    vs.leaseSlots,
					URL:     s.url,
				}, nil
			}
		}

		// NOTE: We could attempt to fulfill a lease of 1 slot, but if we're getting to the bottom
		// of what the sandboxes can fulfill, it's best to just boot a new sandbox anyway.

		a.log.Info("no space in existing sandboxes, creating new sandbox for app", "app", ver.App, "version", ver.Version)
	}

	// Check max_instances before creating additional sandbox
	maxInstances := int(ver.Config.Concurrency.MaxInstances)
	if maxInstances > 0 && len(vs.sandboxes) >= maxInstances {
		return nil, fmt.Errorf("cannot create sandbox: already at max_instances limit of %d", maxInstances)
	}

	return a.activateApp(ctx, ver, pool, service)
}

func (a *localActivator) activateApp(ctx context.Context, ver *core_v1alpha.AppVersion, pool, service string) (*Lease, error) {
	// Create the sandbox
	sb, err := a.createSandbox(ctx, ver, pool, service)
	if err != nil {
		return nil, err
	}

	// Get lease size from version tracking
	a.mu.Lock()
	defer a.mu.Unlock()

	key := verKey{ver.ID.String(), pool}
	vs, ok := a.versions[key]
	if !ok {
		return nil, fmt.Errorf("version tracking not found after creating sandbox")
	}

	// Mark slots as in use for the first lease
	sb.inuseSlots = vs.leaseSlots

	lease := &Lease{
		ver:     ver,
		sandbox: sb.sandbox,
		ent:     sb.ent,
		pool:    pool,
		Size:    vs.leaseSlots,
		URL:     sb.url,
	}

	return lease, nil
}

// createSandbox creates a new sandbox entity, waits for it to become running, and adds it to version tracking
func (a *localActivator) createSandbox(ctx context.Context, ver *core_v1alpha.AppVersion, pool, service string) (*sandbox, error) {
	// Get app metadata
	gr, err := a.eac.Get(ctx, ver.App.String())
	if err != nil {
		return nil, err
	}

	var app core_v1alpha.App
	app.Decode(gr.Entity().Entity())

	var appMD core_v1alpha.Metadata
	appMD.Decode(gr.Entity().Entity())

	// Determine port from config or default to 3000
	port := int64(3000)
	if ver.Config.Port > 0 {
		port = ver.Config.Port
	}
	var sb compute_v1alpha.Sandbox
	sb.Version = app.ActiveVersion
	sb.LogEntity = app.EntityId().String()
	sb.LogAttribute = types.LabelSet("stage", "app-run", "pool", pool, "service", service)

	// Build the app container
	appCont := compute_v1alpha.Container{
		Name:  "app",
		Image: ver.ImageUrl,
		Env: []string{
			"MIREN_APP=" + appMD.Name,
			"MIREN_VERSION=" + ver.Version,
		},
		Directory: "/app",
		Port: []compute_v1alpha.Port{
			{
				Port: port,
				Name: "http",
				Type: "http",
			},
		},
	}

	// Add environment variables from config
	for _, v := range ver.Config.Variable {
		appCont.Env = append(appCont.Env, v.Key+"="+v.Value)
	}

	// Set command based on service
	for _, s := range ver.Config.Commands {
		if s.Service == service && s.Command != "" {
			if ver.Config.Entrypoint != "" {
				appCont.Command = ver.Config.Entrypoint + " " + s.Command
			} else {
				appCont.Command = s.Command
			}
			break
		}
	}

	sb.Container = append(sb.Container, appCont)

	name := idgen.GenNS("sb")
	a.log.Debug("creating sandbox", "app", ver.App, "sandbox", name, "command", appCont.Command)

	// Create the sandbox entity
	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name:   name,
			Labels: types.LabelSet("app", appMD.Name),
		}).Encode,
		entity.Ident, "sandbox/"+name,
		sb.Encode,
	))

	pr, err := a.eac.Put(ctx, &rpcE)
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox entity: %w", err)
	}

	a.log.Debug("created sandbox entity", "app", ver.App, "sandbox", pr.Id())

	// Wait for sandbox to become running
	var (
		runningSB compute_v1alpha.Sandbox
		sbEnt     *entity.Entity
	)

	a.log.Debug("watching sandbox until it becomes running", "app", ver.App, "sandbox", pr.Id())

	_, _ = a.eac.WatchEntity(ctx, pr.Id(), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		var sb compute_v1alpha.Sandbox

		if op.HasEntity() {
			en := op.Entity().Entity()
			sb.Decode(en)

			if sb.Status == compute_v1alpha.RUNNING {
				runningSB = sb
				sbEnt = en
				return io.EOF // Signal completion
			}
		}

		return nil
	}))

	if runningSB.Status != compute_v1alpha.RUNNING {
		a.log.Error("sandbox did not become running", "app", ver.App, "sandbox", pr.Id(), "status", runningSB.Status)
		return nil, fmt.Errorf("sandbox did not become running: %s", runningSB.Status)
	}

	// Construct the URL
	url := fmt.Sprintf("http://%s:%d", runningSB.Network[0].Address, port)

	// Create the sandbox struct
	lsb := &sandbox{
		sandbox:     &runningSB,
		ent:         sbEnt,
		lastRenewal: time.Now(),
		url:         url,
		maxSlots:    int(ver.Config.Concurrency.RequestsPerInstance),
		inuseSlots:  0, // No slots in use initially
	}

	// Add to version tracking
	a.mu.Lock()
	defer a.mu.Unlock()

	vs := a.ensureVersionTracking(ver, pool)
	vs.sandboxes = append(vs.sandboxes, lsb)

	return lsb, nil
}

// ensureVersionTracking ensures version tracking exists for the given version and pool.
// It returns the verSandboxes struct, creating it if necessary.
// IMPORTANT: Must be called with a.mu held.
func (a *localActivator) ensureVersionTracking(ver *core_v1alpha.AppVersion, pool string) *verSandboxes {
	key := verKey{ver.ID.String(), pool}
	vs, exists := a.versions[key]
	if !exists {
		// Calculate lease slots (1/5th of requests_per_instance)
		var leaseSlots int
		if ver.Config.Concurrency.RequestsPerInstance > 0 {
			leaseSlots = max(1, int(float32(ver.Config.Concurrency.RequestsPerInstance)*0.2))
		}
		vs = &verSandboxes{
			ver:        ver,
			sandboxes:  []*sandbox{},
			leaseSlots: leaseSlots,
		}
		a.versions[key] = vs
	}
	return vs
}

func (a *localActivator) ReleaseLease(ctx context.Context, lease *Lease) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	vs, ok := a.versions[verKey{lease.ver.ID.String(), lease.pool}]
	if !ok {
		return nil
	}

	for _, s := range vs.sandboxes {
		if s.sandbox == lease.sandbox {
			s.inuseSlots -= lease.Size
			break
		}
	}

	return nil
}

func (a *localActivator) RenewLease(ctx context.Context, lease *Lease) (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	vs, ok := a.versions[verKey{lease.ver.ID.String(), lease.pool}]
	if !ok {
		return nil, fmt.Errorf("lease not found")
	}

	for _, s := range vs.sandboxes {
		if s.sandbox == lease.sandbox {
			s.lastRenewal = time.Now()
			break
		}
	}

	return lease, nil
}

func (a *localActivator) InBackground(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.retireUnusedSandboxes()
			a.ensureMinInstances(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (a *localActivator) retireUnusedSandboxes() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, vs := range a.versions {
		var newSandboxes []*sandbox
		minInstances := int(vs.ver.Config.Concurrency.MinInstances)

		for _, sb := range vs.sandboxes {
			scaleDownDelay := vs.ver.Config.Concurrency.ScaleDownDelay
			if scaleDownDelay == 0 {
				scaleDownDelay = 2 * time.Minute // default
			}

			// Don't retire if we're at or below min_instances
			if len(vs.sandboxes) <= minInstances {
				newSandboxes = append(newSandboxes, sb)
				continue
			}

			if time.Since(sb.lastRenewal) > scaleDownDelay {
				a.log.Debug("retiring unused sandbox", "app", vs.ver.App, "sandbox", sb.sandbox.ID)

				if sb.sandbox.Status != compute_v1alpha.RUNNING {
					continue
				}

				sb.sandbox.Status = compute_v1alpha.STOPPED

				var rpcE entityserver_v1alpha.Entity

				rpcE.SetId(sb.sandbox.ID.String())

				rpcE.SetAttrs(entity.Attrs(
					(&compute_v1alpha.Sandbox{
						Status: compute_v1alpha.STOPPED,
					}).Encode,
				))

				_, err := a.eac.Put(context.Background(), &rpcE)
				if err != nil {
					a.log.Error("failed to retire sandbox", "error", err)
				}
			} else {
				newSandboxes = append(newSandboxes, sb)
			}
		}

		vs.sandboxes = newSandboxes
	}
}

// ensureMinInstancesForVersion ensures min_instances are running for a specific version.
// IMPORTANT: Must be called with a.mu held.
func (a *localActivator) ensureMinInstancesForVersion(ctx context.Context, vs *verSandboxes, key verKey) {
	minInstances := int(vs.ver.Config.Concurrency.MinInstances)
	if minInstances <= 0 {
		return
	}

	maxInstances := int(vs.ver.Config.Concurrency.MaxInstances)
	currentCount := len(vs.sandboxes)

	// Create additional sandboxes if needed
	for i := currentCount; i < minInstances; i++ {
		// Don't exceed max_instances if set
		if maxInstances > 0 && len(vs.sandboxes) >= maxInstances {
			a.log.Warn("cannot create sandbox for min_instances: would exceed max_instances",
				"app", vs.ver.App,
				"min", minInstances,
				"max", maxInstances,
				"current", len(vs.sandboxes))
			break
		}
		a.log.Info("creating sandbox to meet min_instances",
			"app", vs.ver.App,
			"version", vs.ver.Version,
			"current", currentCount,
			"min", minInstances)

		// Unlock before creating sandbox to avoid deadlock
		a.mu.Unlock()
		// TODO: Plumb service through here instead of assuming DefaultService, perhaps service can ride along with verKey?
		//       Deploy() would need to take it as an argument as well
		_, err := a.createSandbox(ctx, vs.ver, key.pool, DefaultService)
		a.mu.Lock()

		if err != nil {
			a.log.Error("failed to create sandbox for min_instances",
				"app", vs.ver.App,
				"error", err)
			break
		}
	}
}

// ensureMinInstances maintains min_instances for versions the activator knows about.
func (a *localActivator) ensureMinInstances(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for key, vs := range a.versions {
		a.ensureMinInstancesForVersion(ctx, vs, key)
	}
}

// Deploy handles deployment of a new app version.
// It is idempotent and handles:
// - Storing the app version in the activator's internal state
// - Ensuring min_instances are running
// - Cleaning up sandboxes from old versions
func (a *localActivator) Deploy(ctx context.Context, app *core_v1alpha.App, ver *core_v1alpha.AppVersion, pool string) error {
	if app.ActiveVersion != ver.ID {
		return fmt.Errorf("app active version (%s) does not match deploying version (%s)", app.ActiveVersion, ver.ID)
	}

	a.log.Info("deploying app version",
		"app", app.ID,
		"version", ver.Version,
		"pool", pool,
		"min_instances", ver.Config.Concurrency.MinInstances)

	// Ensure the version is in our tracking map and min_instances are running
	a.mu.Lock()
	vs := a.ensureVersionTracking(ver, pool)
	key := verKey{ver.ID.String(), pool}
	a.ensureMinInstancesForVersion(ctx, vs, key)
	a.mu.Unlock()

	// Clean up old versions after new version is running
	if err := a.cleanupOldVersions(ctx, app, ver, pool); err != nil {
		a.log.Error("failed to cleanup old versions, continuing anyway",
			"app", app.ID,
			"error", err)
	}

	return nil
}

// cleanupOldVersions stops sandboxes from previous app versions
func (a *localActivator) cleanupOldVersions(ctx context.Context, app *core_v1alpha.App, currentVer *core_v1alpha.AppVersion, pool string) error {
	// Query for all app versions of this app
	versions, err := a.ec.List(ctx, entity.Ref(core_v1alpha.AppVersionAppId, app.ID))
	if err != nil {
		return fmt.Errorf("failed to list app versions: %w", err)
	}

	for versions.Next() {
		var ver core_v1alpha.AppVersion
		if err := versions.Read(&ver); err != nil {
			a.log.Error("failed to read app version", "error", err)
			continue
		}

		// Skip the current version
		if ver.ID == currentVer.ID {
			continue
		}

		a.log.Info("cleaning up old app version",
			"app", app.ID,
			"oldVersion", ver.Version,
			"currentVersion", currentVer.Version)

		// Remove from our tracking
		a.mu.Lock()
		key := verKey{ver.ID.String(), pool}
		vs, exists := a.versions[key]
		if exists {
			// Mark all sandboxes for stop
			for _, sb := range vs.sandboxes {
				if sb.sandbox.Status == compute_v1alpha.RUNNING || sb.sandbox.Status == compute_v1alpha.PENDING {
					a.log.Info("marking sandbox for stop",
						"app", app.ID,
						"version", ver.Version,
						"sandbox", sb.sandbox.ID)

					// Update sandbox status to STOPPED
					var rpcE entityserver_v1alpha.Entity
					rpcE.SetId(sb.sandbox.ID.String())
					rpcE.SetAttrs(entity.Attrs(
						(&compute_v1alpha.Sandbox{
							Status: compute_v1alpha.STOPPED,
						}).Encode,
					))

					if _, err := a.eac.Put(ctx, &rpcE); err != nil {
						a.log.Error("failed to stop sandbox",
							"sandbox", sb.sandbox.ID,
							"error", err)
					}
				}
			}
			// Remove from tracking
			delete(a.versions, key)
		}
		a.mu.Unlock()

		// Also query for any sandboxes we might not be tracking
		sandboxes, err := a.ec.List(ctx, entity.Ref(compute_v1alpha.SandboxVersionId, ver.ID))
		if err != nil {
			a.log.Error("failed to list sandboxes for old version", "error", err)
			continue
		}

		for sandboxes.Next() {
			var sb compute_v1alpha.Sandbox
			if err := sandboxes.Read(&sb); err != nil {
				a.log.Error("failed to read sandbox", "error", err)
				continue
			}

			if sb.Status == compute_v1alpha.RUNNING || sb.Status == compute_v1alpha.PENDING {
				a.log.Info("marking untracked sandbox for stop",
					"app", app.ID,
					"version", ver.Version,
					"sandbox", sb.ID)

				// Update sandbox status to STOPPED
				var rpcE entityserver_v1alpha.Entity
				rpcE.SetId(sb.ID.String())
				rpcE.SetAttrs(entity.Attrs(
					(&compute_v1alpha.Sandbox{
						Status: compute_v1alpha.STOPPED,
					}).Encode,
				))

				if _, err := a.eac.Put(ctx, &rpcE); err != nil {
					a.log.Error("failed to stop untracked sandbox",
						"sandbox", sb.ID,
						"error", err)
				}
			}
		}
	}

	return nil
}
