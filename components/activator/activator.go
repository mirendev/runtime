package activator

// CONCURRENCY & LOCK DESIGN
//
// The activator coordinates between multiple concurrent goroutines:
// - Request threads calling AcquireLease/ReleaseLease/RenewLease (high QPS hot path)
// - Background watchSandboxes goroutine discovering new sandboxes from etcd watches
// - Background syncLastActivity goroutine updating entity store every 30s
//
// All share access to the same state maps (versions, pools, newSandboxChans), protected
// by a single RWMutex. Read locks allow concurrent capacity checks on the hot path, while
// write locks serialize state updates (adding sandboxes, updating capacity).
//
// Key Locking Patterns:
//
// 1. Double-Check Pattern (AcquireLease, checkForSandbox)
//    Prevents TOCTOU races when upgrading from read to write lock:
//      RLock → check capacity → RUnlock
//      Lock → re-check capacity → acquire if still available → Unlock
//
// 2. Sentinel Pattern (requestPoolCapacity on-demand pool creation)
//    Prevents duplicate pool creation when multiple concurrent requests for
//    a cold ephemeral URL arrive before any pool exists:
//      - First request inserts sentinel with inProgress=true
//      - Concurrent requests find the sentinel and wait on done channel
//      - First request calls PoolCreator.CreatePoolForVersion, then replaces
//        the sentinel with the real pool state (or deletes it on failure)
//      - Waiters loop back to re-check pool state after done fires
//
// 3. Channel Notification (ensurePoolAndWaitForSandbox, watchSandboxes)
//    Immediate notification when new sandboxes become available:
//      - Waiter: Lock → register notification channel → Unlock → wait on channel
//      - Watcher: Lock → add sandbox → notify all registered channels → Unlock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/netutil"
	"miren.dev/runtime/pkg/rpc/stream"
)

// extractHTTPPort extracts the HTTP port from a sandbox spec's container ports.
// Returns the port number and true if found, or 0 and false if no HTTP port exists.
func extractHTTPPort(spec *compute_v1alpha.SandboxSpec) (int64, bool) {
	if spec == nil || len(spec.Container) == 0 {
		return 0, false
	}

	// Look for HTTP port in container spec
	for _, cont := range spec.Container {
		for _, p := range cont.Port {
			if p.Type == "http" {
				return p.Port, true
			}
		}
	}

	return 0, false
}

// routePort returns the port the activator should route to. When an app bound a
// port other than the one Miren configured, the sandbox controller records the
// observed port as a bound_port on the entity; that observation wins so traffic
// reaches where the app is actually listening. bound_port is written only on
// divergence and holds a single entry, so the first one is the route target.
// Otherwise we fall back to configuredPort (the caller's spec-derived default).
func routePort(boundPorts []compute_v1alpha.BoundPort, configuredPort int64) int64 {
	if len(boundPorts) > 0 && boundPorts[0].Port > 0 {
		return boundPorts[0].Port
	}
	return configuredPort
}

type Lease struct {
	ver     *core_v1alpha.AppVersion
	sandbox *compute_v1alpha.Sandbox
	ent     *entity.Entity
	pool    string
	service string

	Size int
	URL  string
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

// SandboxInvalidation is sent when a sandbox transitions away from RUNNING,
// signaling that any cached leases pointing to it should be invalidated.
type SandboxInvalidation struct {
	SandboxID entity.Id
}

type AppActivator interface {
	AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (*Lease, error)
	ReleaseLease(ctx context.Context, lease *Lease) error
	RenewLease(ctx context.Context, lease *Lease) (*Lease, error)

	// Invalidations returns a channel that receives notifications when
	// sandboxes become non-RUNNING. Consumers should invalidate any cached
	// leases referencing the invalidated sandbox.
	Invalidations() <-chan SandboxInvalidation

	// SetPoolCreator registers a callback for on-demand pool creation.
	// This is used by ephemeral deployments where the DeploymentLauncher
	// doesn't pre-create pools.
	SetPoolCreator(pc PoolCreator)
}

// PoolCreator creates sandbox pools on demand for versions that don't have
// one pre-created by the DeploymentLauncher (e.g., ephemeral versions).
type PoolCreator interface {
	CreatePoolForVersion(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (entity.Id, error)
}

type sandbox struct {
	sandbox     *compute_v1alpha.Sandbox
	ent         *entity.Entity
	lastRenewal time.Time
	url         string
	tracker     *concurrency.ConcurrencyTracker
}

type verKey struct {
	ver, service string
}

// versionPoolRef maps a version+service to its pool
type versionPoolRef struct {
	ver      *core_v1alpha.AppVersion
	poolID   entity.Id
	service  string
	strategy concurrency.ConcurrencyStrategy
}

// poolSandboxes tracks all sandboxes in a pool
type poolSandboxes struct {
	pool      *compute_v1alpha.SandboxPool
	sandboxes []*sandbox
	service   string
	strategy  concurrency.ConcurrencyStrategy
}

// poolState represents either a real pool or an in-progress creation sentinel
type poolState struct {
	pool       *compute_v1alpha.SandboxPool
	revision   int64 // Entity revision for optimistic concurrency control
	inProgress bool
	done       chan struct{} // Closed when creation completes (success or failure)
	err        error         // Set if creation failed
}

type localActivator struct {
	mu            sync.RWMutex
	versions      map[verKey]*versionPoolRef   // Maps version+service to pool reference
	poolSandboxes map[entity.Id]*poolSandboxes // Maps pool ID to its sandboxes
	pools         map[verKey]*poolState        // Track SandboxPool entities or creation sentinels

	// Channels to notify waiters when new sandboxes become available
	// Map key is verKey (version + service), value is list of channels to notify
	newSandboxChans map[verKey][]chan struct{}

	// invalidationCh signals httpingress when a sandbox becomes non-RUNNING
	invalidationCh chan SandboxInvalidation

	log         *slog.Logger
	eac         *entityserver_v1alpha.EntityAccessClient
	poolCreator PoolCreator
}

var _ AppActivator = (*localActivator)(nil)

func NewLocalActivator(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) AppActivator {
	la := &localActivator{
		log:             log.With("module", "activator"),
		eac:             eac,
		versions:        make(map[verKey]*versionPoolRef),
		poolSandboxes:   make(map[entity.Id]*poolSandboxes),
		pools:           make(map[verKey]*poolState),
		newSandboxChans: make(map[verKey][]chan struct{}),
		invalidationCh:  make(chan SandboxInvalidation, 64),
	}

	// Recover existing pools first (sandboxes need pools to exist)
	la.log.Info("recovering sandbox pools")
	if err := la.recoverPools(ctx); err != nil {
		la.log.Error("failed to recover pools", "error", err)
	} else {
		la.log.Info("pool recovery complete", "tracked_pools", len(la.pools))
	}

	// Recover existing sandboxes after pools
	la.log.Info("activator starting, attempting to recover existing sandboxes")
	if err := la.recoverSandboxes(ctx); err != nil {
		la.log.Error("failed to recover sandboxes", "error", err)
	} else {
		la.log.Info("activator recovery complete", "tracked_versions", len(la.versions))
	}

	go la.watchSandboxes(ctx)
	go la.watchPools(ctx)
	go la.syncLastActivity(ctx)

	return la
}

func (a *localActivator) SetPoolCreator(pc PoolCreator) {
	a.poolCreator = pc
}

func (a *localActivator) AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (*Lease, error) {
	key := verKey{ver.ID.String(), service}

	// Try to find an available sandbox with capacity (read lock for scanning)
	a.mu.RLock()
	versionRef, ok := a.versions[key]
	var candidateSandbox *sandbox
	hasPending := false
	if ok {
		// Look up the pool's sandboxes
		ps, poolOk := a.poolSandboxes[versionRef.poolID]
		if poolOk && len(ps.sandboxes) > 0 {
			a.log.Debug("checking existing sandboxes in pool", "app", ver.App, "version", ver.Version, "pool", versionRef.poolID, "sandboxes", len(ps.sandboxes))

			// Scan for a sandbox with capacity
			start := rand.Int() % len(ps.sandboxes)
			for i := 0; i < len(ps.sandboxes); i++ {
				s := ps.sandboxes[(start+i)%len(ps.sandboxes)]
				if s.sandbox.Status == compute_v1alpha.RUNNING && s.tracker.HasCapacity() && s.url != "" {
					candidateSandbox = s
					break
				}
				// Track if we have PENDING sandboxes (being created/booting)
				if s.sandbox.Status == compute_v1alpha.PENDING {
					hasPending = true
				}
			}
		}
	}
	a.mu.RUnlock()

	// If we found a candidate, acquire write lock and double-check status and capacity
	if candidateSandbox != nil {
		a.mu.Lock()
		// Double-check status and capacity (may have changed between locks)
		if candidateSandbox.sandbox.Status == compute_v1alpha.RUNNING &&
			candidateSandbox.tracker.HasCapacity() &&
			candidateSandbox.url != "" {
			leaseSize := candidateSandbox.tracker.AcquireLease()
			candidateSandbox.lastRenewal = time.Now()

			lease := &Lease{
				ver:     ver,
				sandbox: candidateSandbox.sandbox,
				ent:     candidateSandbox.ent,
				pool:    service, // Pool identifier is now the service name
				service: service,
				Size:    leaseSize,
				URL:     candidateSandbox.url,
			}
			used := candidateSandbox.tracker.Used()
			a.mu.Unlock()
			a.log.Debug("reusing sandbox", "app", ver.App, "version", ver.Version, "sandbox", candidateSandbox.sandbox.ID, "used", used)
			return lease, nil
		}
		a.mu.Unlock()
		// Status changed or capacity was taken between RLock and Lock, fall through to pool request
	}

	// No available sandboxes with capacity
	if hasPending {
		// We have PENDING sandboxes booting - wait for them instead of creating more
		a.log.Info("no running sandboxes available, but pending sandboxes exist - waiting",
			"app", ver.App,
			"version", ver.Version,
			"service", service)
		return a.waitForSandbox(ctx, ver, service, false)
	}

	// No RUNNING or PENDING sandboxes - need to scale up via pool.
	// Ephemeral preview deploys are capped at one instance by EphemeralStrategy
	// (MaxInstances=1), so requestPoolCapacity won't scale them past their single
	// sandbox — it returns the at-cap pool and the caller waits for it.
	a.log.Info("no available sandboxes, requesting capacity from pool",
		"app", ver.App,
		"version", ver.Version,
		"service", service)

	return a.waitForSandbox(ctx, ver, service, true)
}

var ErrSandboxDiedEarly = fmt.Errorf("sandbox died while booting")
var ErrPoolTimeout = fmt.Errorf("timeout waiting for sandbox from pool")

// waitForSandbox waits for a sandbox with capacity to become available.
// If incrementPool is true, it will ensure the pool exists and increment DesiredInstances.
// If incrementPool is false, it assumes PENDING sandboxes exist and just waits for them.
// The background watcher (watchSandboxes) handles discovering new sandboxes and notifying waiters.
func (a *localActivator) waitForSandbox(ctx context.Context, ver *core_v1alpha.AppVersion, service string, incrementPool bool) (*Lease, error) {
	key := verKey{ver.ID.String(), service}

	// Track the count of sandboxes we knew about BEFORE requesting new capacity.
	// This lets us distinguish between:
	// 1. Old DEAD sandboxes from previous scale-down cycles (should be ignored)
	// 2. New sandboxes created for THIS request that died (real boot failure)
	//
	// Scope the count to sandboxes belonging to THIS app version. When a pool
	// is reused across versions (MIR-1023), dead sandboxes from a previous
	// version share the pool but must not doom a freshly-deployed version
	// before it has a chance to boot its own sandbox.
	var sandboxCountBeforeIncrement int
	if incrementPool {
		a.mu.RLock()
		if versionRef, ok := a.versions[key]; ok {
			if ps, poolOk := a.poolSandboxes[versionRef.poolID]; poolOk {
				for _, s := range ps.sandboxes {
					if s.sandbox.Spec.Version == ver.ID {
						sandboxCountBeforeIncrement++
					}
				}
			}
		}
		a.mu.RUnlock()
	}

	var pool *compute_v1alpha.SandboxPool
	if incrementPool {
		// Request additional capacity from pool
		var err error
		pool, err = a.requestPoolCapacity(ctx, ver, service)
		if err != nil {
			return nil, fmt.Errorf("failed to request pool capacity: %w", err)
		}

		a.log.Info("waiting for sandbox from pool",
			"app", ver.App,
			"service", service,
			"pool_id", pool.ID,
			"desired_instances", pool.DesiredInstances)
	} else {
		a.log.Info("waiting for pending sandbox to become ready",
			"app", ver.App,
			"service", service)
	}

	// Register notification channel for this wait
	notifyChan := make(chan struct{}, 1)
	a.mu.Lock()
	a.newSandboxChans[key] = append(a.newSandboxChans[key], notifyChan)
	a.mu.Unlock()

	// Clean up the channel on exit
	defer func() {
		a.mu.Lock()
		chans := a.newSandboxChans[key]
		for i, ch := range chans {
			if ch == notifyChan {
				a.newSandboxChans[key] = append(chans[:i], chans[i+1:]...)
				break
			}
		}
		if len(a.newSandboxChans[key]) == 0 {
			delete(a.newSandboxChans, key)
		}
		a.mu.Unlock()
		close(notifyChan)
	}()

	// Cap the per-request wait at 120s. The default PortWaitTimeout is 15s and
	// a first-time image pull on a node that hasn't seen the image can run
	// 30-90s, plus container setup. The previous 50s cap was too aggressive
	// for cold starts and would return ErrPoolTimeout while sandboxes were
	// still booting. Stay under httpingress' leaseAcquisitionTimeout (2m).
	pollCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	// Fallback ticker at 60s interval as safety net
	// If this fires, it means channel notification failed somehow
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Helper to check for available sandbox
	checkForSandbox := func() (*Lease, bool) {
		a.mu.RLock()
		versionRef, ok := a.versions[key]
		var candidateSandbox *sandbox
		if ok {
			// Look up the pool's sandboxes
			ps, poolOk := a.poolSandboxes[versionRef.poolID]
			if poolOk && len(ps.sandboxes) > 0 {
				// Try to find a sandbox with capacity.
				// Require url != "" — a sandbox can briefly be tracked as RUNNING
				// before the watcher has populated its URL, and returning a Lease
				// with an empty URL would cause the proxy to fail downstream.
				// Mirrors the predicate in AcquireLease.
				start := rand.Int() % len(ps.sandboxes)
				for i := 0; i < len(ps.sandboxes); i++ {
					s := ps.sandboxes[(start+i)%len(ps.sandboxes)]
					if s.sandbox.Status == compute_v1alpha.RUNNING && s.tracker.HasCapacity() && s.url != "" {
						candidateSandbox = s
						break
					}
				}
			}
		}
		a.mu.RUnlock()

		if candidateSandbox != nil {
			a.mu.Lock()
			// Double-check status, capacity, and URL (may have changed between locks)
			if candidateSandbox.sandbox.Status == compute_v1alpha.RUNNING &&
				candidateSandbox.tracker.HasCapacity() &&
				candidateSandbox.url != "" {
				leaseSize := candidateSandbox.tracker.AcquireLease()
				candidateSandbox.lastRenewal = time.Now()
				a.mu.Unlock()

				a.log.Info("acquired lease from pool sandbox",
					"app", ver.App,
					"version", ver.Version,
					"sandbox", candidateSandbox.sandbox.ID,
					"service", service)

				return &Lease{
					ver:     ver,
					sandbox: candidateSandbox.sandbox,
					ent:     candidateSandbox.ent,
					pool:    service,
					service: service,
					Size:    leaseSize,
					URL:     candidateSandbox.url,
				}, true
			}
			a.mu.Unlock()
		}
		return nil, false
	}

	for {
		// Check for available sandbox immediately
		if lease, found := checkForSandbox(); found {
			return lease, nil
		}

		// Check if all sandboxes have failed (no RUNNING, no PENDING)
		// If so, fail fast instead of waiting for timeout.
		//
		// hasPendingOrRunning looks at the whole pool: a RUNNING sandbox from
		// any version can serve this request (pool reuse means spec matches),
		// so we shouldn't fail fast while one exists.
		//
		// currentSandboxCount / sandboxStatuses are scoped to THIS version.
		// When a pool is reused across versions (MIR-1023), dead sandboxes
		// from a previous version share the pool; they shouldn't count as
		// failures against the freshly-deployed version that hasn't even
		// had a sandbox created for it yet.
		a.mu.RLock()
		versionRef, ok := a.versions[key]
		hasPendingOrRunning := false
		var sandboxStatuses []string
		var currentSandboxCount int
		if ok {
			ps, poolOk := a.poolSandboxes[versionRef.poolID]
			if poolOk {
				for _, s := range ps.sandboxes {
					if s.sandbox.Status == compute_v1alpha.RUNNING || s.sandbox.Status == compute_v1alpha.PENDING {
						hasPendingOrRunning = true
					}
					if s.sandbox.Spec.Version != ver.ID {
						continue
					}
					sandboxStatuses = append(sandboxStatuses, fmt.Sprintf("%s:%s", s.sandbox.ID, s.sandbox.Status))
					currentSandboxCount++
				}
			}
		}
		a.mu.RUnlock()

		// Log current state for debugging
		a.log.Debug("fail-fast check",
			"app", ver.App,
			"version", ver.Version,
			"service", service,
			"tracked", ok,
			"count", len(sandboxStatuses),
			"sandboxes", sandboxStatuses,
			"has_pending_or_running", hasPendingOrRunning,
			"increment_pool", incrementPool,
			"sandbox_count_before", sandboxCountBeforeIncrement)

		// Only fail fast if:
		// 1. We have sandboxes tracked but none are RUNNING or PENDING, AND
		// 2. Either we didn't just request new capacity (incrementPool=false), OR
		//    we DID request capacity AND a new sandbox was created and then died
		//
		// This prevents false "died during boot" errors when:
		// - We just incremented the pool and are waiting for the reconciler to create a sandbox
		// - Old DEAD sandboxes from previous scale-down cycles are still in tracking
		hasNewSandboxesSinceIncrement := currentSandboxCount > sandboxCountBeforeIncrement
		shouldFailFast := !hasPendingOrRunning && currentSandboxCount > 0 &&
			(!incrementPool || hasNewSandboxesSinceIncrement)

		if shouldFailFast {
			// We have sandboxes tracked but none are RUNNING or PENDING
			// AND either we weren't scaling up, or a new sandbox was created and died
			a.log.Error("all sandboxes failed while waiting",
				"app", ver.App,
				"version", ver.Version,
				"service", service,
				"sandboxes", sandboxStatuses,
				"new_sandboxes_created", hasNewSandboxesSinceIncrement)
			return nil, fmt.Errorf("%w: all sandboxes died during boot", ErrSandboxDiedEarly)
		}

		select {
		case <-pollCtx.Done():
			return nil, fmt.Errorf("%w: no sandbox became available within 120 seconds", ErrPoolTimeout)
		case <-notifyChan:
			// Notified of new sandbox availability, loop back to check
		case <-ticker.C:
			// Fallback safety check - log warning if this catches something
			a.log.Warn("fallback ticker fired while waiting for sandbox - channel notification may have failed",
				"app", ver.App,
				"service", service)
		}
	}
}

// registerVersionPoolLocked records the version->pool mapping and ensures an
// (initially empty) poolSandboxes entry exists for the pool. The caller must
// hold a.mu for writing.
//
// It is idempotent: existing entries are left untouched, since the watcher and
// recovery own the contents of poolSandboxes[*].sandboxes. It deliberately does
// not touch a.pools — each caller sets that with its own entity revision.
//
// This consolidates the bookkeeping that requestPoolCapacity must do whenever it
// resolves a pool for a version. Without it, the on-demand creation path (used by
// ephemeral versions, which the DeploymentLauncher never pre-creates a pool for)
// would seed a.pools but not a.versions, leaving the version untracked until the
// watcher happened to discover a sandbox — so AcquireLease would report
// "tracked: false" and never hand out a lease (MIR-1198).
func (a *localActivator) registerVersionPoolLocked(
	key verKey,
	ver *core_v1alpha.AppVersion,
	pool *compute_v1alpha.SandboxPool,
	service string,
	strategy concurrency.ConcurrencyStrategy,
) {
	if _, exists := a.versions[key]; !exists {
		a.versions[key] = &versionPoolRef{
			ver:      ver,
			poolID:   pool.ID,
			service:  service,
			strategy: strategy,
		}
	}

	if _, ok := a.poolSandboxes[pool.ID]; !ok {
		a.poolSandboxes[pool.ID] = &poolSandboxes{
			pool:      pool,
			sandboxes: []*sandbox{},
			service:   service,
			strategy:  strategy,
		}
	}
}

// requestPoolCapacity finds the SandboxPool created by DeploymentLauncher and increments DesiredInstances.
// It uses retry logic with exponential backoff to handle the race where Activator receives
// a request before DeploymentLauncher has finished creating the pool.
// Uses a sentinel pattern to prevent duplicate capacity requests from concurrent callers.
func (a *localActivator) requestPoolCapacity(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (*compute_v1alpha.SandboxPool, error) {
	key := verKey{ver.ID.String(), service}

	// Resolve config and build the strategy once at entry: the cap comes from
	// strategy.MaxInstances(), and the same strategy is reused below when we
	// register a freshly-discovered pool on the cache-miss path. This is the
	// scale-up path (only reached when no sandbox has capacity), so the extra
	// entity store reads are acceptable.
	spec, err := coreutil.ResolveRuntimeConfig(ctx, a.eac, ver)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config: %w", err)
	}
	svcConcurrency, err := coreutil.GetServiceConcurrency(spec, service)
	if err != nil {
		return nil, fmt.Errorf("failed to get service concurrency: %w", err)
	}
	sc := core_v1alpha.ServiceConcurrency(svcConcurrency)
	strategy := concurrency.NewStrategyForVersion(ver, service, &sc)
	maxInstances := int64(strategy.MaxInstances())

	// poolLoop is labeled so the pool-creation retry path below can break all
	// the way back out to re-read the cache. The outer loop re-acquires a.mu
	// from scratch on every iteration (RLock immediately below), so any
	// `continue poolLoop` MUST leave the lock released.
poolLoop:
	for {
		// Check if pool exists or is being created (read lock)
		a.mu.RLock()
		state, exists := a.pools[key]
		a.mu.RUnlock()

		if exists {
			// If creation is in progress, wait for it to complete
			if state.inProgress {
				a.log.Debug("pool creation in progress, waiting", "service", service)
				select {
				case <-state.done:
					// Creation completed, check result
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				// Check if creation succeeded or failed
				if state.err != nil {
					return nil, fmt.Errorf("pool creation failed: %w", state.err)
				}
				// Creation succeeded, loop back to re-check the pool state
				// (it might already have capacity, or another racer might have incremented)
				continue
			}

			// state.pool is now a real (non-sentinel) cached pool. Ensure the
			// version->pool mapping exists before we increment and return: a pool
			// created on-demand seeds a.pools but not a.versions, so without this
			// a cached ephemeral pool would stay untracked and AcquireLease would
			// never resolve its sandbox (MIR-1198). Seeding here also self-heals
			// pools created before this fix shipped, on the next lease request.
			a.mu.Lock()
			a.registerVersionPoolLocked(key, ver, state.pool, service, strategy)
			a.mu.Unlock()

			// Check if pool is in crash cooldown before attempting to increment
			if !state.pool.CooldownUntil.IsZero() && time.Now().Before(state.pool.CooldownUntil) {
				return state.pool, fmt.Errorf("%w: application in crash cooldown until %s (consecutive crashes: %d)",
					ErrSandboxDiedEarly,
					state.pool.CooldownUntil.Format(time.RFC3339),
					state.pool.ConsecutiveCrashCount)
			}

			// Update existing pool - increment DesiredInstances with optimistic concurrency control
			// Calculate target ONCE based on the state captured at the start of this iteration
			// This ensures concurrent goroutines that all saw the same initial value
			// will calculate the same target
			a.mu.Lock()
			newDesired := state.pool.DesiredInstances + 1
			a.mu.Unlock()

			const maxRetries = 3
			poolDeleted := false
			for attempt := range maxRetries {
				a.mu.Lock()
				if state.pool.DesiredInstances >= maxInstances {
					poolIDForMaxCheck := state.pool.ID
					a.mu.Unlock()

					// Cache says we're at max, but it may be stale. Re-read from entity store
					// to detect whether the pool manager scaled down (giving us headroom) or
					// whether the pool was deleted entirely.
					freshPoolEnt, getErr := a.eac.Get(ctx, poolIDForMaxCheck.String())
					if getErr != nil {
						if errors.Is(getErr, cond.ErrNotFound{}) {
							a.log.Info("pool was deleted while at max size, clearing stale reference",
								"pool", poolIDForMaxCheck,
								"service", service)
							a.mu.Lock()
							delete(a.pools, key)
							a.mu.Unlock()
							poolDeleted = true
							break
						}
						a.log.Warn("pool at maximum size, re-read failed; returning cached pool",
							"pool", poolIDForMaxCheck,
							"max_size", maxInstances,
							"error", getErr)
						// Caller will poll waitForSandbox for an existing sandbox.
						return state.pool, nil
					}

					var freshPool compute_v1alpha.SandboxPool
					freshPool.Decode(freshPoolEnt.Entity().Entity())

					a.mu.Lock()
					if freshPool.DesiredInstances >= maxInstances {
						a.mu.Unlock()
						// The pool is legitimately at its strategy's cap (e.g. an ephemeral
						// pool at DesiredInstances=1). Don't error — the caller will wait on
						// the existing sandbox via waitForSandbox's polling loop. For Auto
						// pools at MaxPoolSize this is the runaway-prevention case; the wait
						// will eventually time out via ErrPoolTimeout.
						return &freshPool, nil
					}

					// Fresh state is below max - update cache and recalculate target
					a.log.Info("stale cache showed pool at max size, but fresh state is below max",
						"pool", poolIDForMaxCheck,
						"cached_desired", state.pool.DesiredInstances,
						"fresh_desired", freshPool.DesiredInstances)
					state.pool = &freshPool
					state.revision = freshPoolEnt.Entity().Revision()
					a.pools[key] = state
					newDesired = freshPool.DesiredInstances + 1
					a.mu.Unlock()

					continue
				}

				// Check if we've already reached our target (another goroutine may have incremented)
				if state.pool.DesiredInstances >= newDesired {
					a.mu.Unlock()
					a.log.Debug("pool capacity already at or above target, no increment needed",
						"pool", state.pool.ID,
						"current_desired", state.pool.DesiredInstances,
						"target_desired", newDesired)
					return state.pool, nil
				}

				currentRevision := state.revision
				poolID := state.pool.ID
				a.mu.Unlock()

				// Build attrs for Patch
				attrs := []entity.Attr{
					{
						ID:    entity.DBId,
						Value: entity.AnyValue(poolID),
					},
					{
						ID:    compute_v1alpha.SandboxPoolDesiredInstancesId,
						Value: entity.AnyValue(newDesired),
					},
				}

				// Use Patch with revision check for optimistic concurrency control
				patchRes, err := a.eac.Patch(ctx, attrs, currentRevision)
				if err != nil {
					// Check for revision conflict
					if errors.Is(err, cond.ErrConflict{}) {
						a.log.Debug("pool revision conflict, refetching and retrying",
							"pool", poolID,
							"attempt", attempt+1,
							"max_retries", maxRetries)

						// Fetch fresh pool state
						freshPoolEnt, getErr := a.eac.Get(ctx, poolID.String())
						if getErr != nil {
							if errors.Is(getErr, cond.ErrNotFound{}) {
								// Pool was deleted, clear cache and break out to outer loop
								a.log.Info("pool was deleted during update, clearing stale reference",
									"pool", poolID,
									"service", service)
								a.mu.Lock()
								delete(a.pools, key)
								a.mu.Unlock()
								poolDeleted = true
								break // Break out of OCC retry loop to re-query for pools
							}
							return nil, fmt.Errorf("failed to fetch fresh pool after conflict: %w", getErr)
						}

						// Decode fresh pool
						var freshPool compute_v1alpha.SandboxPool
						freshPool.Decode(freshPoolEnt.Entity().Entity())

						// Update cache with fresh state
						a.mu.Lock()
						state.pool = &freshPool
						state.revision = freshPoolEnt.Entity().Revision()
						a.pools[key] = state
						a.mu.Unlock()

						// Retry the increment with fresh state
						continue
					}

					// If pool was deleted, clear stale reference and break to re-query
					if errors.Is(err, cond.ErrNotFound{}) {
						a.log.Info("pool was deleted, clearing stale reference",
							"pool", poolID,
							"service", service)
						a.mu.Lock()
						delete(a.pools, key)
						a.mu.Unlock()
						poolDeleted = true
						// Break out of OCC retry loop to re-query for pools
						break
					}

					return nil, fmt.Errorf("failed to patch pool: %w", err)
				}

				// Success - update cache with new state
				a.mu.Lock()
				state.pool.DesiredInstances = newDesired
				state.revision = patchRes.Revision()
				a.mu.Unlock()

				a.log.Info("incremented pool capacity", "pool", poolID, "desired_instances", newDesired, "revision", patchRes.Revision())
				return state.pool, nil
			}

			// Check if we broke out because pool was deleted
			if poolDeleted {
				// Pool was deleted, continue outer loop to re-query for pools
				continue
			}

			// Max retries exceeded
			return nil, fmt.Errorf("failed to increment pool after %d retries due to conflicts", maxRetries)
		}

		// Pool doesn't exist - try to claim creation rights with sentinel
		a.mu.Lock()
		_, exists = a.pools[key]
		if exists {
			// Another goroutine claimed creation while we waited for lock
			a.mu.Unlock()
			continue poolLoop // Loop back to wait/increment logic
		}

		// Try to find an existing pool in the entity store with retries
		// DeploymentLauncher may have already created it, but we haven't seen it yet in our cache.
		//
		// Lock invariant for this loop: a.mu is held at the top of every
		// iteration. The attempt>0 backoff below releases it (line ~a.mu.Unlock)
		// and re-acquires it before the next store query, so any path that exits
		// an iteration must leave the lock in the state the next iteration (or
		// the code after the loop) expects: held to stay in this loop, released
		// to `continue poolLoop`.
		const maxRetries = 3
		const baseRetryDelay = 100 * time.Millisecond

		var foundPoolWithRev *poolWithRevision
		for attempt := range maxRetries {
			if attempt > 0 {
				// Release lock during retry delay
				a.mu.Unlock()

				// Exponential backoff: 100ms, 200ms, 400ms
				delay := baseRetryDelay * (1 << (attempt - 1))
				a.log.Debug("retrying pool lookup from store",
					"attempt", attempt+1,
					"max_retries", maxRetries,
					"service", service,
					"delay_ms", delay.Milliseconds())

				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return nil, ctx.Err()
				}

				// Re-acquire lock and check if another goroutine found/created the pool
				a.mu.Lock()
				_, exists = a.pools[key]
				if exists {
					// Another goroutine found or created the pool while we were
					// waiting. Break all the way out to the outer loop to pick it
					// up through the normal wait/increment path. A bare `continue`
					// here would re-enter *this* attempt loop with the lock
					// released, and the next iteration's a.mu.Unlock() during
					// backoff would fatally unlock an already-unlocked mutex
					// (MIR-1306).
					a.mu.Unlock()
					continue poolLoop
				}
			}

			// Try to find pool in entity store (holds lock during query)
			a.mu.Unlock()
			poolWithRev, err := a.findPoolInStore(ctx, ver.ID, service)
			a.mu.Lock()

			if err != nil {
				a.log.Warn("failed to query pool from store",
					"error", err,
					"attempt", attempt+1,
					"service", service)
				continue
			}

			if poolWithRev != nil {
				foundPoolWithRev = poolWithRev
				break
			}
		}

		if foundPoolWithRev != nil {
			// Found pool created by DeploymentLauncher - register in caches and
			// then either increment with OCC or skip if already at cap.
			foundPool := foundPoolWithRev.pool
			currentRevision := foundPoolWithRev.revision
			poolID := foundPool.ID

			// Cache the pool state and register strategy before doing anything
			// else: even when we're at cap and won't patch, the caller needs
			// to find the pool through the cache for subsequent operations.
			a.pools[key] = &poolState{
				pool:       foundPool,
				revision:   currentRevision,
				inProgress: false,
			}

			a.registerVersionPoolLocked(key, ver, foundPool, service, strategy)

			// If the pool is already at its strategy's cap, there's nothing
			// to patch (e.g. ephemeral pools seed at DesiredInstances=1 with
			// MaxInstances=1). Return the cached pool; the caller will poll
			// for the existing sandbox via waitForSandbox.
			if foundPool.DesiredInstances >= maxInstances {
				a.mu.Unlock()
				return foundPool, nil
			}

			newDesired := foundPool.DesiredInstances + 1

			a.mu.Unlock()

			// Build attrs for Patch
			attrs := []entity.Attr{
				{
					ID:    entity.DBId,
					Value: entity.AnyValue(poolID),
				},
				{
					ID:    compute_v1alpha.SandboxPoolDesiredInstancesId,
					Value: entity.AnyValue(newDesired),
				},
			}

			// Use Patch with revision check
			patchRes, err := a.eac.Patch(ctx, attrs, currentRevision)
			if err != nil {
				// On conflict or error, clear cache and let caller retry
				if errors.Is(err, cond.ErrConflict{}) {
					a.log.Warn("launcher-created pool revision conflict, clearing cache for retry",
						"pool", poolID)
					a.mu.Lock()
					delete(a.pools, key)
					a.mu.Unlock()
					continue // Retry from the beginning
				}
				// If pool was deleted, clear cache and retry
				if errors.Is(err, cond.ErrNotFound{}) {
					a.log.Info("launcher-created pool was deleted, clearing cache",
						"pool", poolID)
					a.mu.Lock()
					delete(a.pools, key)
					a.mu.Unlock()
					continue // Retry from the beginning
				}
				return nil, fmt.Errorf("failed to patch launcher-created pool: %w", err)
			}

			// Success - update cache
			a.mu.Lock()
			if state, ok := a.pools[key]; ok {
				state.pool.DesiredInstances = newDesired
				state.revision = patchRes.Revision()
			}
			a.mu.Unlock()

			a.log.Info("found launcher-created pool after retries",
				"pool", poolID,
				"service", service,
				"desired_instances", newDesired,
				"revision", patchRes.Revision())
			return foundPool, nil
		}

		// Pool not found after retries — try on-demand creation if a PoolCreator
		// is available (used for ephemeral versions that bypass DeploymentLauncher).
		if a.poolCreator != nil {
			// Use the sentinel pattern to coalesce concurrent creators.
			// Check under the already-held write lock whether another goroutine
			// has started (or completed) creation; if so, loop back to re-check.
			if existing, ok := a.pools[key]; ok {
				a.mu.Unlock()
				if existing.inProgress {
					select {
					case <-existing.done:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				continue // re-check pool state from the top
			}

			// Install the in-progress sentinel, then release the lock to do
			// the slow creation. Concurrent callers will find the sentinel
			// and wait on done rather than creating a duplicate pool.
			sentinel := &poolState{
				inProgress: true,
				done:       make(chan struct{}),
			}
			a.pools[key] = sentinel
			a.mu.Unlock()

			a.log.Info("pool not found, attempting on-demand creation",
				"service", service, "version", ver.Version, "version_id", ver.ID)

			poolID, createErr := a.poolCreator.CreatePoolForVersion(ctx, ver, service)
			var foundPool *poolWithRevision
			if createErr == nil {
				// Pool was created (or reused — poolID may be empty if an existing
				// pool matched). Query the store to populate our cache.
				foundPool, createErr = a.findPoolInStore(ctx, ver.ID, service)
				if createErr == nil && foundPool == nil {
					if poolID != "" {
						createErr = fmt.Errorf("pool created on-demand (id=%s) but not found in store", poolID)
					} else {
						createErr = fmt.Errorf("no matching pool found in store after on-demand creation")
					}
				}
			}

			// Publish the result and notify waiters
			a.mu.Lock()
			if createErr != nil {
				sentinel.err = createErr
				delete(a.pools, key)
			} else {
				a.pools[key] = &poolState{
					pool:     foundPool.pool,
					revision: foundPool.revision,
				}
				a.registerVersionPoolLocked(key, ver, foundPool.pool, service, strategy)
			}
			a.mu.Unlock()
			close(sentinel.done)

			if createErr != nil {
				return nil, fmt.Errorf("on-demand pool creation failed for version=%s service=%s: %w",
					ver.Version, service, createErr)
			}

			a.log.Info("on-demand pool created and cached",
				"pool", poolID, "service", service, "version", ver.Version)
			return foundPool.pool, nil
		}

		a.mu.Unlock()

		a.log.Error("pool not found in store after retries",
			"service", service,
			"version", ver.Version,
			"version_id", ver.ID,
			"retries", maxRetries,
			"error", "DeploymentLauncher should have created this pool")

		return nil, fmt.Errorf(
			"pool not found for app=%s version=%s service=%s after %d query attempts over ~%dms - "+
				"DeploymentLauncher should create pools when an app version is deployed. "+
				"Verify the app is deployed and check DeploymentLauncher logs",
			ver.App, ver.Version, service, maxRetries,
			int((baseRetryDelay*(1<<maxRetries))/time.Millisecond))
	}
}

type poolWithRevision struct {
	pool     *compute_v1alpha.SandboxPool
	revision int64
}

// findPoolInStore queries the entity store for a pool matching the given version and service.
// This is used to find pools created by the DeploymentLauncher controller.
// Returns nil if no matching pool is found (not an error - caller should decide whether to retry or create).
func (a *localActivator) findPoolInStore(ctx context.Context, versionID entity.Id, service string) (*poolWithRevision, error) {
	// List all sandbox pools
	poolsResp, err := a.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return nil, fmt.Errorf("failed to list pools: %w", err)
	}

	// Find pool matching version + service
	for _, ent := range poolsResp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())

		if pool.Service != service {
			continue
		}

		// Check if this pool references our version (pool reuse mechanism)
		if slices.Contains(pool.ReferencedByVersions, versionID) {
			a.log.Debug("found pool in store via referenced_by_versions",
				"pool", pool.ID,
				"service", service,
				"version", versionID,
				"num_refs", len(pool.ReferencedByVersions))
			return &poolWithRevision{
				pool:     &pool,
				revision: ent.Revision(),
			}, nil
		}
	}

	return nil, nil // Not found
}

func (a *localActivator) ReleaseLease(ctx context.Context, lease *Lease) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	versionRef, ok := a.versions[verKey{lease.ver.ID.String(), lease.service}]
	if !ok {
		return nil
	}

	ps, ok := a.poolSandboxes[versionRef.poolID]
	if !ok {
		return nil
	}

	// Release capacity via tracker (mode-specific behavior is handled by strategy)
	for _, s := range ps.sandboxes {
		if s.sandbox == lease.sandbox {
			s.tracker.ReleaseLease(lease.Size)
			break
		}
	}

	return nil
}

func (a *localActivator) RenewLease(ctx context.Context, lease *Lease) (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	versionRef, ok := a.versions[verKey{lease.ver.ID.String(), lease.service}]
	if !ok {
		return nil, fmt.Errorf("version not found")
	}

	ps, ok := a.poolSandboxes[versionRef.poolID]
	if !ok {
		return nil, fmt.Errorf("pool not found")
	}

	for _, s := range ps.sandboxes {
		if s.sandbox == lease.sandbox {
			// Reject renewal if sandbox is no longer running.
			// This ensures httpingress invalidates cached leases for
			// stopped/dead sandboxes on the next renewal cycle.
			if s.sandbox.Status != compute_v1alpha.RUNNING {
				return nil, fmt.Errorf("sandbox %s is %s", s.sandbox.ID, s.sandbox.Status)
			}
			s.lastRenewal = time.Now()
			return lease, nil
		}
	}

	return nil, fmt.Errorf("sandbox not found")
}

func (a *localActivator) Invalidations() <-chan SandboxInvalidation {
	return a.invalidationCh
}

// resyncFromStore re-reads pools and sandboxes from the entity store and adopts
// any that the in-memory caches are missing. WatchIndex only streams new ops from
// the current revision — it does not replay existing entities on (re)connect — so a
// watch reconnect (compaction, leader change, network blip) silently drops every
// event during the gap. Without a re-sync those changes are lost until the process
// restarts, which is how an ephemeral version ends up permanently untracked even
// though a healthy sandbox exists for it (MIR-1198). recoverPools and
// recoverSandboxes are additive (create-only / dedupe by sandbox ID) and lock per
// item, so this is safe to run repeatedly against live state and from either watch.
//
// Both watchSandboxes and watchPools call this on their own reconnect, so a single
// etcd blip (which usually trips both watches at once) runs the reconcile twice,
// roughly concurrently. That's intentional: the redundant pass is idempotent and
// bounded, and accepting it is cheaper than adding cross-goroutine debounce state
// to coordinate the two watchers.
func (a *localActivator) resyncFromStore(ctx context.Context) {
	a.log.Info("re-syncing activator state from store after watch reconnect")
	if err := a.recoverPools(ctx); err != nil {
		a.log.Error("failed to re-sync pools after watch reconnect", "error", err)
	}
	if err := a.recoverSandboxes(ctx); err != nil {
		a.log.Error("failed to re-sync sandboxes after watch reconnect", "error", err)
	}

	// Recovery adopts sandboxes by appending to poolSandboxes directly, bypassing
	// the per-sandbox notify the watcher does, so wake parked waiters to re-check
	// rather than making them wait out the 60s fallback ticker.
	a.wakeAllWaiters()
}

// wakeAllWaiters signals every parked waitForSandbox goroutine to re-check its
// pool. resyncFromStore calls this after a reconnect reconcile: recoverSandboxes
// adopts sandboxes by appending to poolSandboxes directly, bypassing the
// per-sandbox notify the watcher does (see watchSandboxes), so without this an
// already-parked waiter wouldn't see an adopted sandbox until the 60s fallback
// ticker — and would log a misleading "channel notification may have failed" when
// it finally woke. A spurious wake is harmless: the waiter re-scans and re-parks
// if its sandbox still isn't there.
func (a *localActivator) wakeAllWaiters() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, chans := range a.newSandboxChans {
		for _, ch := range chans {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

func (a *localActivator) watchSandboxes(ctx context.Context) {
	// Watch for sandbox changes: update status AND discover new RUNNING sandboxes
	// This is the single source of sandbox discovery for the activator
	// Retry loop to handle transient failures
	firstWatch := true
	for {
		select {
		case <-ctx.Done():
			a.log.Info("sandbox watch context cancelled")
			return
		default:
		}

		// NewLocalActivator already ran the initial recovery, so only re-sync on
		// reconnects (see resyncFromStore for why this is necessary).
		if !firstWatch {
			a.resyncFromStore(ctx)
		}
		firstWatch = false

		a.log.Info("starting sandbox discovery watch")

		_, err := a.eac.WatchIndex(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox), 0, stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
			if op.IsDelete() {
				// Entity was deleted - clean up from tracking
				// The ID should still be available in the operation even without the entity
				if op.HasEntityId() {
					a.removeSandboxFromTracking(entity.Id(op.EntityId()))
				}
				return nil
			}

			en := op.Entity().Entity()
			var sb compute_v1alpha.Sandbox
			sb.Decode(en)

			// First, check if we're already tracking this sandbox (read lock for scan)
			a.mu.RLock()
			var trackedSandbox *sandbox
			var trackedPoolID entity.Id
			for poolID, ps := range a.poolSandboxes {
				for _, s := range ps.sandboxes {
					if s.sandbox.ID == sb.ID {
						trackedSandbox = s
						trackedPoolID = poolID
						break
					}
				}
				if trackedSandbox != nil {
					break
				}
			}
			a.mu.RUnlock()

			// If already tracked, first check if we need to build URL (without holding lock)
			if trackedSandbox != nil {
				var newURL string

				// Do expensive RPC/decode work without holding the lock
				// Update URL if sandbox now has a network address (e.g., PENDING -> RUNNING transition)
				if len(sb.Network) > 0 {
					// Extract HTTP port from sandbox spec (canonical source of truth)
					port, found := extractHTTPPort(&sb.Spec)
					if !found {
						port = 3000 // Default fallback
					}
					port = routePort(sb.BoundPort, port)

					if addr, err := netutil.BuildHTTPURL(sb.Network[0].Address, port); err == nil {
						newURL = addr
					}
				}

				// Now acquire write lock to update shared state
				a.mu.Lock()
				oldStatus := trackedSandbox.sandbox.Status
				oldURL := trackedSandbox.url
				trackedSandbox.sandbox.Status = sb.Status

				// Re-check conditions under lock and update URL if still needed
				if newURL != "" && trackedSandbox.url != newURL && len(sb.Network) > 0 {
					trackedSandbox.url = newURL
					a.log.Debug("updated sandbox URL when address changes", "sandbox", sb.ID, "url", newURL)
				}

				// Keep STOPPED and DEAD sandboxes in tracking so fail-fast logic can see them
				// They will be cleaned up later by periodic reconciliation or when
				// new RUNNING sandboxes are discovered

				// Notify waiters when:
				//  - sandbox status changes to RUNNING/STOPPED/DEAD, OR
				//  - a RUNNING sandbox's URL transitions from empty to populated
				//    (the sandbox may have been marked RUNNING in a prior watch
				//    event before its network address was visible; waiters that
				//    saw RUNNING but no URL were effectively stuck until this
				//    follow-up event arrives).
				statusChanged := oldStatus != sb.Status &&
					(sb.Status == compute_v1alpha.RUNNING || sb.Status == compute_v1alpha.STOPPED || sb.Status == compute_v1alpha.DEAD)
				urlBecameAvailable := sb.Status == compute_v1alpha.RUNNING && oldURL == "" && trackedSandbox.url != ""
				if statusChanged || urlBecameAvailable {
					// Notify all versions that reference this pool
					// Find all version->service mappings that use this pool
					for key, versionRef := range a.versions {
						if versionRef.poolID == trackedPoolID {
							if chans, ok := a.newSandboxChans[key]; ok {
								for _, ch := range chans {
									select {
									case ch <- struct{}{}:
									default:
									}
								}
							}
						}
					}
				}

				// Signal httpingress to invalidate cached leases for this sandbox
				// when it transitions away from RUNNING
				if oldStatus == compute_v1alpha.RUNNING && sb.Status != compute_v1alpha.RUNNING {
					select {
					case a.invalidationCh <- SandboxInvalidation{SandboxID: sb.ID}:
					default:
						a.log.Warn("invalidation channel full, dropping notification", "sandbox", sb.ID)
					}
				}

				a.mu.Unlock()

				if oldStatus != sb.Status {
					a.log.Info("sandbox status changed", "sandbox", sb.ID, "old_status", oldStatus, "new_status", sb.Status)
				}
				return nil
			}

			// Not tracked yet - check if this is a RUNNING or PENDING sandbox we should track
			// PENDING sandboxes are tracked to prevent runaway pool growth during boot
			if sb.Status != compute_v1alpha.RUNNING && sb.Status != compute_v1alpha.PENDING {
				return nil // Only track RUNNING and PENDING sandboxes
			}

			// Get service and pool from labels
			var md core_v1alpha.Metadata
			md.Decode(en)
			service, _ := md.Labels.Get("service")
			if service == "" {
				return nil // Skip sandboxes without service label
			}

			poolIDStr, _ := md.Labels.Get("pool")
			if poolIDStr == "" {
				return nil // Skip sandboxes without pool label
			}
			poolID := entity.Id(poolIDStr)

			// Get the pool entity to find all versions that reference it
			poolResp, err := a.eac.Get(ctx, poolID.String())
			if err != nil {
				a.log.Error("failed to get pool for sandbox", "sandbox", sb.ID, "pool", poolID, "error", err)
				return nil
			}

			var pool compute_v1alpha.SandboxPool
			pool.Decode(poolResp.Entity().Entity())

			// Get version from sandbox spec for concurrency config
			sbVersion := sb.Spec.Version
			if sbVersion == "" {
				return nil // Skip sandboxes without version
			}

			verResp, err := a.eac.Get(ctx, sbVersion.String())
			if err != nil {
				a.log.Error("failed to get version for sandbox", "sandbox", sb.ID, "version", sbVersion, "error", err)
				return nil
			}

			var appVer core_v1alpha.AppVersion
			appVer.Decode(verResp.Entity().Entity())

			// Resolve config from ConfigVersion if available
			spec, err := coreutil.ResolveRuntimeConfig(ctx, a.eac, &appVer)
			if err != nil {
				a.log.Error("failed to resolve config for sandbox", "error", err, "sandbox", sb.ID)
				return nil
			}

			// Build HTTP URL
			// For PENDING sandboxes, we track them even without network addresses
			// so we can notify waiters if they crash during boot
			var addr string
			if len(sb.Network) == 0 {
				if sb.Status == compute_v1alpha.PENDING {
					// PENDING sandbox without network yet - use placeholder URL
					// We're only tracking it to detect if it dies, not to route to it
					addr = ""
					a.log.Debug("tracking PENDING sandbox without network address", "sandbox", sb.ID)
				} else {
					// RUNNING sandbox without network is unexpected, skip it
					a.log.Warn("sandbox has no network addresses", "sandbox", sb.ID, "status", sb.Status)
					return nil
				}
			} else {
				port := int64(3000)
				for _, svc := range spec.Services {
					if svc.Name == "web" && svc.Port > 0 {
						port = svc.Port
						break
					}
				}
				port = routePort(sb.BoundPort, port)

				var err error
				addr, err = netutil.BuildHTTPURL(sb.Network[0].Address, port)
				if err != nil {
					a.log.Error("failed to build HTTP URL", "error", err, "sandbox", sb.ID)
					return nil
				}
			}

			// Get service concurrency and create strategy/tracker
			svcConcurrency, err := coreutil.GetServiceConcurrency(spec, service)
			if err != nil {
				a.log.Error("failed to get service concurrency", "error", err, "sandbox", sb.ID, "service", service)
				return nil
			}
			sc := core_v1alpha.ServiceConcurrency(svcConcurrency)
			strategy := concurrency.NewStrategyForVersion(&appVer, service, &sc)
			tracker := strategy.InitializeTracker()

			lsb := &sandbox{
				sandbox:     &sb,
				ent:         en,
				lastRenewal: time.Now(),
				url:         addr,
				tracker:     tracker,
			}

			a.mu.Lock()

			// Ensure poolSandboxes entry exists
			ps, ok := a.poolSandboxes[poolID]
			if !ok {
				ps = &poolSandboxes{
					pool:      &pool,
					sandboxes: []*sandbox{},
					service:   service,
					strategy:  strategy,
				}
				a.poolSandboxes[poolID] = ps
			}

			// Double-check for duplicates
			for _, existing := range ps.sandboxes {
				if existing.sandbox.ID == sb.ID {
					a.mu.Unlock()
					return nil // Already added
				}
			}
			ps.sandboxes = append(ps.sandboxes, lsb)

			// Create version->pool mappings for ALL versions referenced by this pool
			for _, versionRef := range pool.ReferencedByVersions {
				// Fetch the app version entity
				versionResp, err := a.eac.Get(ctx, versionRef.String())
				if err != nil {
					a.log.Warn("failed to get referenced version", "version", versionRef, "error", err)
					continue
				}

				var refVer core_v1alpha.AppVersion
				refVer.Decode(versionResp.Entity().Entity())

				key := verKey{refVer.ID.String(), service}
				if _, exists := a.versions[key]; !exists {
					a.versions[key] = &versionPoolRef{
						ver:      &refVer,
						poolID:   poolID,
						service:  service,
						strategy: strategy,
					}
				}

				// Notify any waiters for this version+service
				if chans, ok := a.newSandboxChans[key]; ok {
					for _, ch := range chans {
						select {
						case ch <- struct{}{}:
						default:
						}
					}
				}
			}

			a.mu.Unlock()

			a.log.Info("discovered and tracking new sandbox", "sandbox", sb.ID, "pool", poolID, "service", service, "referenced_versions", len(pool.ReferencedByVersions), "url", addr)
			return nil
		}))

		if err != nil {
			if ctx.Err() != nil {
				// Context was cancelled, exit gracefully
				a.log.Info("sandbox watch stopped due to context cancellation")
				return
			}
			a.log.Error("sandbox watch ended with error, will restart", "error", err)
			time.Sleep(5 * time.Second) // Brief delay before retry
			continue
		}

		// Watch ended without error (shouldn't happen), restart it
		a.log.Warn("sandbox watch ended unexpectedly, restarting")
		time.Sleep(5 * time.Second)
	}
}

func (a *localActivator) recoverSandboxes(ctx context.Context) error {
	resp, err := a.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	a.log.Info("recovering sandboxes on startup", "total_sandboxes", len(resp.Values()))

	recoveredCount := 0
	skippedNotRunning := 0
	skippedNoPool := 0
	recoveryTime := time.Now()

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Only recover RUNNING sandboxes
		if sb.Status != compute_v1alpha.RUNNING {
			skippedNotRunning++
			continue
		}

		// Get pool ID and service from labels
		var md core_v1alpha.Metadata
		md.Decode(ent.Entity())

		service, _ := md.Labels.Get("service")
		if service == "" {
			// Skip sandboxes without service label (e.g., buildkit)
			continue
		}

		poolIDStr, _ := md.Labels.Get("pool")
		if poolIDStr == "" {
			// Skip sandboxes without pool label (will be handled by migration)
			skippedNoPool++
			continue
		}
		poolID := entity.Id(poolIDStr)

		// Get version from sandbox spec
		sbVersion := sb.Spec.Version
		if sbVersion == "" {
			continue
		}

		// Get app version for concurrency config
		verResp, err := a.eac.Get(ctx, sbVersion.String())
		if err != nil {
			a.log.Error("failed to get version for sandbox", "sandbox", sb.ID, "version", sbVersion, "error", err)
			continue
		}

		var appVer core_v1alpha.AppVersion
		appVer.Decode(verResp.Entity().Entity())

		// Resolve config from ConfigVersion if available
		spec, err := coreutil.ResolveRuntimeConfig(ctx, a.eac, &appVer)
		if err != nil {
			a.log.Error("failed to resolve config for sandbox", "error", err, "sandbox", sb.ID)
			continue
		}

		// Extract HTTP port from sandbox spec (canonical source of truth)
		port, found := extractHTTPPort(&sb.Spec)
		if !found {
			port = 3000 // Default fallback
		}
		port = routePort(sb.BoundPort, port)

		// Build HTTP URL from address and port
		if len(sb.Network) == 0 {
			a.log.Warn("sandbox has no network addresses", "sandbox", sb.ID)
			continue
		}

		addr, err := netutil.BuildHTTPURL(sb.Network[0].Address, port)
		if err != nil {
			a.log.Error("failed to build HTTP URL", "error", err, "sandbox", sb.ID)
			continue
		}

		// Get service-specific concurrency configuration and create strategy
		svcConcurrency, err := coreutil.GetServiceConcurrency(spec, service)
		if err != nil {
			a.log.Error("failed to get service concurrency for sandbox", "error", err, "sandbox", sb.ID, "service", service)
			continue
		}
		sc := core_v1alpha.ServiceConcurrency(svcConcurrency)
		strategy := concurrency.NewStrategyForVersion(&appVer, service, &sc)

		// Initialize tracker for recovered sandbox (starts empty)
		tracker := strategy.InitializeTracker()

		// Create sandbox tracking entry
		lsb := &sandbox{
			sandbox:     &sb,
			ent:         ent.Entity(),
			lastRenewal: recoveryTime, // Set to now to give grace period
			url:         addr,
			tracker:     tracker,
		}

		// Add to poolSandboxes
		a.mu.Lock()
		ps, ok := a.poolSandboxes[poolID]
		if !ok {
			// Pool should have been recovered by recoverPools, but if not, skip this sandbox
			a.log.Warn("sandbox references unknown pool", "sandbox", sb.ID, "pool", poolID)
			a.mu.Unlock()
			continue
		}

		// Check for duplicates
		duplicate := false
		for _, existing := range ps.sandboxes {
			if existing.sandbox.ID == sb.ID {
				duplicate = true
				break
			}
		}

		if !duplicate {
			ps.sandboxes = append(ps.sandboxes, lsb)
			recoveredCount++
		}

		a.mu.Unlock()

		a.log.Info("recovered sandbox", "app", appVer.App, "version", appVer.Version, "sandbox", sb.ID, "service", service, "pool", poolID, "url", addr)
	}

	a.log.Info("sandbox recovery complete",
		"recovered", recoveredCount,
		"skipped_not_running", skippedNotRunning,
		"skipped_no_pool", skippedNoPool,
		"tracked_pools", len(a.poolSandboxes))

	return nil
}

func (a *localActivator) recoverPools(ctx context.Context) error {
	resp, err := a.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool))
	if err != nil {
		return fmt.Errorf("failed to list sandbox pools: %w", err)
	}

	a.log.Info("recovering sandbox pools on startup", "total_pools", len(resp.Values()))

	for _, ent := range resp.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(ent.Entity())

		// Get version ID from SandboxSpec
		versionID := pool.SandboxSpec.Version
		if versionID == "" {
			a.log.Warn("pool missing version in spec", "pool", pool.ID)
			continue
		}

		// Fetch the version to get concurrency config
		verResp, err := a.eac.Get(ctx, versionID.String())
		if err != nil {
			a.log.Error("failed to get version for pool", "pool", pool.ID, "version", versionID, "error", err)
			continue
		}

		var appVer core_v1alpha.AppVersion
		appVer.Decode(verResp.Entity().Entity())

		// Resolve config from ConfigVersion if available
		spec, err := coreutil.ResolveRuntimeConfig(ctx, a.eac, &appVer)
		if err != nil {
			a.log.Error("failed to resolve config for pool", "error", err, "pool", pool.ID)
			continue
		}

		// Get service concurrency and create strategy
		svcConcurrency, err := coreutil.GetServiceConcurrency(spec, pool.Service)
		if err != nil {
			a.log.Error("failed to get service concurrency for pool", "pool", pool.ID, "service", pool.Service, "error", err)
			continue
		}
		sc := core_v1alpha.ServiceConcurrency(svcConcurrency)
		strategy := concurrency.NewStrategyForVersion(&appVer, pool.Service, &sc)

		a.mu.Lock()

		// Cache pool state (for sentinel pattern in requestPoolCapacity).
		// Create-only: at startup the map is empty so this seeds every pool, but
		// when recoverPools runs again as a live re-sync (watch reconnect) we must
		// not clobber an in-progress creation sentinel or a freshly-patched
		// revision held by a concurrent requestPoolCapacity. watchPools keeps
		// existing entries' DesiredInstances fresh; here we only fill gaps.
		//
		// Deliberately we do NOT refresh an already-cached entry here, even though
		// the missed-event window is exactly when its DesiredInstances/revision
		// could have gone stale. A refresh is unsafe: this re-sync's store read can
		// itself be older than an in-memory revision a concurrent requestPoolCapacity
		// just patched, so overwriting could move the cache backward. The stale case
		// self-heals instead — the next increment hits a revision conflict on Patch,
		// clears the entry, and re-reads (see the ErrConflict path above).
		key := verKey{versionID.String(), pool.Service}
		if _, ok := a.pools[key]; !ok {
			a.pools[key] = &poolState{
				pool:       &pool,
				revision:   ent.Revision(),
				inProgress: false,
			}
		}

		// Initialize empty poolSandboxes entry (sandboxes will be added by recoverSandboxes)
		poolID := pool.ID
		if _, ok := a.poolSandboxes[poolID]; !ok {
			a.poolSandboxes[poolID] = &poolSandboxes{
				pool:      &pool,
				sandboxes: []*sandbox{},
				service:   pool.Service,
				strategy:  strategy,
			}
		}

		// Create version->pool mappings for ALL versions referenced by this pool
		for _, versionRef := range pool.ReferencedByVersions {
			versionResp, err := a.eac.Get(ctx, versionRef.String())
			if err != nil {
				a.log.Warn("failed to get referenced version during pool recovery", "version", versionRef, "pool", poolID, "error", err)
				continue
			}

			var refVer core_v1alpha.AppVersion
			refVer.Decode(versionResp.Entity().Entity())

			refKey := verKey{refVer.ID.String(), pool.Service}
			if _, exists := a.versions[refKey]; !exists {
				a.versions[refKey] = &versionPoolRef{
					ver:      &refVer,
					poolID:   poolID,
					service:  pool.Service,
					strategy: strategy,
				}
			}
		}

		a.mu.Unlock()

		// Migrate existing sandboxes without pool labels to this pool
		if err := a.migrateOrphanedSandboxes(ctx, &pool); err != nil {
			a.log.Error("failed to migrate orphaned sandboxes to pool", "pool", pool.ID, "error", err)
		}

		a.log.Info("recovered pool", "pool", pool.ID, "service", pool.Service, "version", versionID, "desired_instances", pool.DesiredInstances, "referenced_versions", len(pool.ReferencedByVersions))
	}

	return nil
}

// migrateOrphanedSandboxes finds RUNNING sandboxes that match a pool's version+service
// but don't have a pool label, and labels them with this pool's ID.
// This handles migration of pre-pool sandboxes into the pool system.
func (a *localActivator) migrateOrphanedSandboxes(ctx context.Context, pool *compute_v1alpha.SandboxPool) error {
	// Query sandboxes by version (using nested indexed field)
	resp, err := a.eac.List(ctx, entity.Ref(compute_v1alpha.SandboxSpecVersionId, pool.SandboxSpec.Version))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes by version: %w", err)
	}

	migratedCount := 0

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Only consider RUNNING sandboxes
		if sb.Status != compute_v1alpha.RUNNING {
			continue
		}

		// Check labels
		var md core_v1alpha.Metadata
		md.Decode(ent.Entity())

		// Must match service
		serviceLabel, _ := md.Labels.Get("service")
		if serviceLabel != pool.Service {
			continue
		}

		// Check if already has a pool label
		poolLabel, _ := md.Labels.Get("pool")
		if poolLabel != "" {
			continue // Already belongs to a pool
		}

		// This is an orphaned sandbox - label it with this pool
		a.log.Info("migrating orphaned sandbox to pool",
			"sandbox", sb.ID,
			"pool", pool.ID,
			"service", pool.Service)

		// Update the sandbox entity with pool label (add to existing labels)
		newLabels := types.LabelSet("pool", pool.ID.String())
		md.Labels = append(md.Labels, newLabels...)

		if _, err := a.eac.Patch(ctx, entity.New(
			entity.DBId, sb.ID,
			md.Encode,
		).Attrs(), 0); err != nil {
			a.log.Error("failed to label orphaned sandbox",
				"sandbox", sb.ID,
				"pool", pool.ID,
				"error", err)
			continue
		}

		migratedCount++
	}

	if migratedCount > 0 {
		a.log.Info("migration complete",
			"pool", pool.ID,
			"migrated_sandboxes", migratedCount)
	}

	return nil
}

// syncLastActivity periodically syncs in-memory lastRenewal timestamps to
// sandbox.LastActivity in the entity store. This enables the pool manager to
// make accurate scale-down decisions based on actual lease activity.
//
// Runs every 30 seconds, updating LastActivity for any sandbox where:
// - lastRenewal is newer than the persisted LastActivity
// - It's been > 30s since the last update (throttling)
func (a *localActivator) syncLastActivity(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	a.log.Info("last activity sync started")

	for {
		select {
		case <-ctx.Done():
			a.log.Info("last activity sync stopped")
			return
		case <-ticker.C:
			a.syncLastActivityOnce(ctx)
		}
	}
}

func (a *localActivator) syncLastActivityOnce(ctx context.Context) {
	now := time.Now()

	// Collect sandboxes that need updates (read lock for scan)
	type update struct {
		sandboxID    entity.Id
		lastRenewal  time.Time
		lastActivity time.Time
	}
	var updates []update

	a.mu.RLock()
	for _, ps := range a.poolSandboxes {
		for _, s := range ps.sandboxes {
			// Only update if lastRenewal is newer and it's been > 30s since last update
			if s.lastRenewal.After(s.sandbox.LastActivity) &&
				(s.sandbox.LastActivity.IsZero() || now.Sub(s.sandbox.LastActivity) > 30*time.Second) {
				updates = append(updates, update{
					sandboxID:    s.sandbox.ID,
					lastRenewal:  s.lastRenewal,
					lastActivity: s.sandbox.LastActivity,
				})
			}
		}
	}
	a.mu.RUnlock()

	// Perform updates without holding lock
	if len(updates) > 0 {
		a.log.Debug("syncing last activity", "count", len(updates))
	}

	for _, u := range updates {
		updateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)

		_, err := a.eac.Patch(updateCtx, entity.New(
			entity.DBId, u.sandboxID,
			(&compute_v1alpha.Sandbox{
				LastActivity: u.lastRenewal,
			}).Encode,
		).Attrs(), 0)
		if err != nil {
			if errors.Is(err, cond.ErrConflict{}) {
				// Conflict - another updater modified LastActivity, skip
				a.log.Debug("skipping last_activity sync due to conflict",
					"sandbox", u.sandboxID)
			} else if errors.Is(err, cond.ErrNotFound{}) {
				// Sandbox deleted - remove from tracking
				a.log.Info("sandbox not found during last_activity sync, removing from tracking",
					"sandbox", u.sandboxID)
				a.removeSandboxFromTracking(u.sandboxID)
			}
		} else {
			// Update our in-memory copy to reflect the sync
			a.mu.Lock()
			for _, ps := range a.poolSandboxes {
				for _, s := range ps.sandboxes {
					if s.sandbox.ID == u.sandboxID {
						s.sandbox.LastActivity = u.lastRenewal
						break
					}
				}
			}
			a.mu.Unlock()
		}

		cancel()
	}
}

// removeSandboxFromTracking removes a sandbox from all internal tracking maps.
// This should be called when a sandbox entity is deleted from the store or becomes permanently unavailable.
func (a *localActivator) removeSandboxFromTracking(sandboxID entity.Id) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Find and remove the sandbox from poolSandboxes
	for poolID, ps := range a.poolSandboxes {
		for i, s := range ps.sandboxes {
			if s.sandbox.ID == sandboxID {
				// Remove sandbox from slice
				ps.sandboxes = slices.Delete(ps.sandboxes, i, i+1)
				a.log.Info("removed sandbox from tracking",
					"sandbox", sandboxID,
					"pool", poolID,
					"remaining_sandboxes", len(ps.sandboxes))
				return
			}
		}
	}
}

// removePoolFromTracking removes a pool and all related cache entries.
// This should be called when a pool entity is deleted from the store.
func (a *localActivator) removePoolFromTracking(poolID entity.Id) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Remove poolSandboxes entry
	if _, ok := a.poolSandboxes[poolID]; ok {
		delete(a.poolSandboxes, poolID)
		a.log.Info("removed poolSandboxes entry for deleted pool", "pool", poolID)
	}

	// Remove all versions entries that point to this pool
	for key, versionRef := range a.versions {
		if versionRef.poolID == poolID {
			delete(a.versions, key)
			a.log.Info("removed stale version->pool mapping for deleted pool",
				"version", key.ver,
				"service", key.service,
				"pool", poolID)
		}
	}

	// Remove all pools entries that reference this pool
	for key, state := range a.pools {
		if state.pool != nil && state.pool.ID == poolID {
			delete(a.pools, key)
			a.log.Info("removed stale pool state for deleted pool",
				"version", key.ver,
				"service", key.service,
				"pool", poolID)
		}
	}
}

// evictStaleVersionBindingsLocked drops version->pool bindings that point at
// freshPool but whose version freshPool no longer references. When the launcher
// supersedes a pool it dereferences it (ReferencedByVersions=nil,
// DesiredInstances=0) without deleting the entity, so the pool still exists and
// removePoolFromTracking never fires. Without this, a binding established at
// recovery stays pinned to the drained pool and never re-resolves to the
// canonical one that still references the version, which is how ingress gets
// stranded on a superseded pool across a restart (MIR-1293). Evicting the stale
// entry lets the next AcquireLease re-resolve the key via findPoolInStore, which
// selects the pool that still references the version.
//
// Callers must hold a.mu.
func (a *localActivator) evictStaleVersionBindingsLocked(freshPool *compute_v1alpha.SandboxPool) {
	references := func(versionID string) bool {
		for _, ref := range freshPool.ReferencedByVersions {
			if ref.String() == versionID {
				return true
			}
		}
		return false
	}

	for key, versionRef := range a.versions {
		if versionRef.poolID == freshPool.ID && !references(key.ver) {
			delete(a.versions, key)
			a.log.Info("evicted stale version->pool mapping after pool dereferenced version",
				"version", key.ver,
				"service", key.service,
				"pool", freshPool.ID)
		}
	}

	for key, state := range a.pools {
		if state.pool != nil && state.pool.ID == freshPool.ID && !references(key.ver) {
			delete(a.pools, key)
			a.log.Info("evicted stale pool state after pool dereferenced version",
				"version", key.ver,
				"service", key.service,
				"pool", freshPool.ID)
		}
	}
}

// watchPools watches for pool entity changes and keeps the in-memory cache in sync.
// Handles deletions (cleanup stale entries) and updates (refresh DesiredInstances, etc.).
func (a *localActivator) watchPools(ctx context.Context) {
	firstWatch := true
	for {
		select {
		case <-ctx.Done():
			a.log.Info("pool watch context cancelled")
			return
		default:
		}

		// Like watchSandboxes, the pool watch streams only new ops from the current
		// revision, so re-sync from the store on reconnect to recover pool/version
		// mappings missed while the watch was down (see resyncFromStore).
		if !firstWatch {
			a.resyncFromStore(ctx)
		}
		firstWatch = false

		a.log.Info("starting pool watch")

		_, err := a.eac.WatchIndex(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandboxPool), 0, stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
			if op.IsDelete() {
				// Pool was deleted - clean up all related cache entries
				if op.HasEntityId() {
					poolID := entity.Id(op.EntityId())
					a.log.Info("pool deleted, cleaning up cache", "pool", poolID)
					a.removePoolFromTracking(poolID)
				}
				return nil
			}

			if op.IsUpdate() && op.HasEntity() {
				var freshPool compute_v1alpha.SandboxPool
				freshPool.Decode(op.Entity().Entity())
				freshRevision := op.Entity().Revision()

				a.mu.Lock()

				// Update pools cache entries that reference this pool
				for key, state := range a.pools {
					if state.pool != nil && state.pool.ID == freshPool.ID {
						oldDesired := state.pool.DesiredInstances
						state.pool = &freshPool
						state.revision = freshRevision
						a.pools[key] = state
						if oldDesired != freshPool.DesiredInstances {
							a.log.Info("pool watch updated DesiredInstances",
								"pool", freshPool.ID,
								"old_desired", oldDesired,
								"new_desired", freshPool.DesiredInstances)
						}
					}
				}

				// Update poolSandboxes pool pointer
				if ps, ok := a.poolSandboxes[freshPool.ID]; ok {
					ps.pool = &freshPool
				}

				// Drop any binding still pinned to this pool for a version it no
				// longer references (e.g. the launcher just drained a superseded
				// pool), so the next lease request re-resolves to the canonical pool.
				a.evictStaleVersionBindingsLocked(&freshPool)

				a.mu.Unlock()
			}

			return nil
		}))

		if ctx.Err() != nil {
			a.log.Info("pool watch stopped due to context cancellation")
			return
		}
		if err != nil {
			a.log.Error("pool watch ended with error, will restart", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		a.log.Warn("pool watch ended without error, will restart")
		time.Sleep(5 * time.Second)
	}
}
