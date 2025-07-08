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
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc/stream"
)

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
}

var _ AppActivator = (*localActivator)(nil)

func NewLocalActivator(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) AppActivator {
	la := &localActivator{
		log:      log.With("module", "activator"),
		eac:      eac,
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
	gr, err := a.eac.Get(ctx, ver.App.String())
	if err != nil {
		return nil, err
	}

	var app core_v1alpha.App
	app.Decode(gr.Entity().Entity())

	var appMD core_v1alpha.Metadata
	appMD.Decode(gr.Entity().Entity())

	var sb compute_v1alpha.Sandbox
	sb.Version = app.ActiveVersion

	sb.LogEntity = app.EntityId().String()
	sb.LogAttribute = types.LabelSet("stage", "app-run", "pool", pool, "service", service)

	// Determine port from config or default to 3000
	port := int64(3000)
	if ver.Config.Port > 0 {
		port = ver.Config.Port
	}

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

	for _, x := range ver.Config.Variable {
		appCont.Env = append(appCont.Env, x.Key+"="+x.Value)
	}

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
		return nil, err
	}

	a.log.Debug("created sandbox", "app", ver.App, "sandbox", pr.Id())

	var (
		runningSB compute_v1alpha.Sandbox
		sbEnt     *entity.Entity
	)

	a.log.Debug("watching sandbox until it becomes running", "app", ver.App, "sandbox", pr.Id())

	a.eac.WatchEntity(ctx, pr.Id(), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		var sb compute_v1alpha.Sandbox

		if op.HasEntity() {
			en := op.Entity().Entity()
			sb.Decode(en)

			if sb.Status == compute_v1alpha.RUNNING {
				runningSB = sb
				sbEnt = en
				// TODO figure out a better way to signal that we're done with the watch.
				return io.EOF
			}
		}

		return nil
	}))

	if runningSB.Status != compute_v1alpha.RUNNING {
		a.log.Error("sandbox did not become running", "app", ver.App, "sandbox", pr.Id(), "status", runningSB.Status)
		return nil, fmt.Errorf("sandbox did not become running: %s", runningSB.Status)
	}

	addr := fmt.Sprintf("http://%s:%d", runningSB.Network[0].Address, port)

	// Calculate lease slot size based on concurrency configuration:
	// - If requests_per_instance > 0: each lease gets 20% of total capacity (min 1)
	// - If requests_per_instance = 0: unlimited capacity, so leaseSlots = 0
	// This ensures slot arithmetic works correctly: 0+0 <= 0 for unlimited apps
	var leaseSlots int

	if ver.Config.Concurrency.RequestsPerInstance > 0 {
		leaseSlots = int(float32(ver.Config.Concurrency.RequestsPerInstance) * 0.20)

		if leaseSlots < 1 {
			leaseSlots = 1
		}
	}
	// When RequestsPerInstance is 0 (unlimited), leaseSlots remains 0

	lsb := &sandbox{
		sandbox:     &runningSB,
		ent:         sbEnt,
		lastRenewal: time.Now(),
		url:         addr,
		maxSlots:    int(ver.Config.Concurrency.RequestsPerInstance),
		inuseSlots:  leaseSlots,
	}

	lease := &Lease{
		ver:     ver,
		sandbox: lsb.sandbox,
		ent:     lsb.ent,
		Size:    leaseSlots,
		URL:     lsb.url,
	}

	key := verKey{ver.ID.String(), pool}

	vs, ok := a.versions[key]
	if !ok {
		vs = &verSandboxes{
			ver:        ver,
			sandboxes:  []*sandbox{},
			leaseSlots: leaseSlots,
		}
		a.versions[key] = vs
	}

	vs.sandboxes = append(vs.sandboxes, lsb)

	return lease, nil
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

func (a *localActivator) ensureMinInstances(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for key, vs := range a.versions {
		minInstances := int(vs.ver.Config.Concurrency.MinInstances)
		if minInstances <= 0 {
			continue
		}

		maxInstances := int(vs.ver.Config.Concurrency.MaxInstances)

		// Count running sandboxes
		runningCount := 0
		for _, sb := range vs.sandboxes {
			if sb.sandbox.Status == compute_v1alpha.RUNNING {
				runningCount++
			}
		}

		// Create additional sandboxes if needed
		for i := runningCount; i < minInstances; i++ {
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
				"current", runningCount,
				"min", minInstances)

			// Unlock during activation to avoid deadlock
			a.mu.Unlock()
			_, err := a.activateApp(ctx, vs.ver, key.pool, "")
			a.mu.Lock()

			if err != nil {
				a.log.Error("failed to create sandbox for min_instances",
					"app", vs.ver.App,
					"error", err)
				break
			}
		}
	}
}
