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
// 2. Sentinel Pattern (requestPoolCapacity)
//    Prevents duplicate pool updates by concurrent requests:
//      - First request inserts sentinel with inProgress=true
//      - Concurrent requests wait on sentinel's done channel
//      - First request finds pool and increments capacity, then replaces sentinel
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
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/netutil"
	"miren.dev/runtime/pkg/rpc/stream"
)

const (
	// MaxPoolSize is the maximum number of sandboxes allowed in a pool
	// This prevents runaway growth even if there are bugs in scaling logic
	MaxPoolSize = 20
)

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

type AppActivator interface {
	AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (*Lease, error)
	ReleaseLease(ctx context.Context, lease *Lease) error
	RenewLease(ctx context.Context, lease *Lease) (*Lease, error)
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

	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
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
	go la.syncLastActivity(ctx)

	return la
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

	// No RUNNING or PENDING sandboxes - need to scale up via pool
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

	pollCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()

	// Fallback ticker at 30s interval as safety net
	// If this fires, it means channel notification failed somehow
	ticker := time.NewTicker(30 * time.Second)
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
				// Try to find a sandbox with capacity
				start := rand.Int() % len(ps.sandboxes)
				for i := 0; i < len(ps.sandboxes); i++ {
					s := ps.sandboxes[(start+i)%len(ps.sandboxes)]
					if s.sandbox.Status == compute_v1alpha.RUNNING && s.tracker.HasCapacity() {
						candidateSandbox = s
						break
					}
				}
			}
		}
		a.mu.RUnlock()

		if candidateSandbox != nil {
			a.mu.Lock()
			// Double-check status and capacity (may have changed between locks)
			if candidateSandbox.sandbox.Status == compute_v1alpha.RUNNING &&
				candidateSandbox.tracker.HasCapacity() {
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
		// If so, fail fast instead of waiting for timeout
		a.mu.RLock()
		versionRef, ok := a.versions[key]
		hasPendingOrRunning := false
		var sandboxStatuses []string
		var hasSandboxes bool
		if ok {
			// Look up the pool's sandboxes
			ps, poolOk := a.poolSandboxes[versionRef.poolID]
			if poolOk && len(ps.sandboxes) > 0 {
				hasSandboxes = true
				for _, s := range ps.sandboxes {
					sandboxStatuses = append(sandboxStatuses, fmt.Sprintf("%s:%s", s.sandbox.ID, s.sandbox.Status))
					if s.sandbox.Status == compute_v1alpha.RUNNING || s.sandbox.Status == compute_v1alpha.PENDING {
						hasPendingOrRunning = true
						break
					}
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
			"has_pending_or_running", hasPendingOrRunning)

		if !hasPendingOrRunning && hasSandboxes {
			// We have sandboxes tracked but none are RUNNING or PENDING
			// This means they all failed - fail fast
			a.log.Error("all sandboxes failed while waiting",
				"app", ver.App,
				"version", ver.Version,
				"service", service,
				"sandboxes", sandboxStatuses)
			return nil, fmt.Errorf("%w: all sandboxes died during boot", ErrSandboxDiedEarly)
		}

		select {
		case <-pollCtx.Done():
			return nil, fmt.Errorf("%w: no sandbox became available within 50 seconds", ErrPoolTimeout)
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

// requestPoolCapacity finds the SandboxPool created by DeploymentLauncher and increments DesiredInstances.
// It uses retry logic with exponential backoff to handle the race where Activator receives
// a request before DeploymentLauncher has finished creating the pool.
// Uses a sentinel pattern to prevent duplicate capacity requests from concurrent callers.
func (a *localActivator) requestPoolCapacity(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (*compute_v1alpha.SandboxPool, error) {
	key := verKey{ver.ID.String(), service}

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
			for attempt := 0; attempt < maxRetries; attempt++ {
				a.mu.Lock()
				if state.pool.DesiredInstances >= MaxPoolSize {
					a.mu.Unlock()
					a.log.Warn("pool at maximum size, cannot increment further",
						"pool", state.pool.ID,
						"max_size", MaxPoolSize,
						"current", state.pool.DesiredInstances)
					return state.pool, fmt.Errorf("pool has reached maximum size of %d", MaxPoolSize)
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
							if errors.Is(getErr, entity.ErrNotFound) {
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
					if errors.Is(err, entity.ErrNotFound) {
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
			continue // Loop back to wait/increment logic
		}

		// Try to find an existing pool in the entity store with retries
		// DeploymentLauncher may have already created it, but we haven't seen it yet in our cache
		const maxRetries = 3
		const baseRetryDelay = 100 * time.Millisecond

		var foundPoolWithRev *poolWithRevision
		for attempt := 0; attempt < maxRetries; attempt++ {
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
					// Another goroutine found or created the pool while we were waiting
					a.mu.Unlock()
					continue // Loop back to main logic
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
			// Found pool created by DeploymentLauncher - increment with OCC
			foundPool := foundPoolWithRev.pool
			currentRevision := foundPoolWithRev.revision

			if foundPool.DesiredInstances >= MaxPoolSize {
				a.mu.Unlock()
				a.log.Warn("launcher-created pool at maximum size, cannot increment further",
					"pool", foundPool.ID,
					"max_size", MaxPoolSize,
					"current", foundPool.DesiredInstances)
				return foundPool, fmt.Errorf("pool has reached maximum size of %d", MaxPoolSize)
			}

			newDesired := foundPool.DesiredInstances + 1
			poolID := foundPool.ID

			// Get service concurrency and create strategy for version->pool mapping
			svcConcurrency, err := core_v1alpha.GetServiceConcurrency(ver, service)
			if err != nil {
				a.mu.Unlock()
				return nil, fmt.Errorf("failed to get service concurrency: %w", err)
			}
			strategy := concurrency.NewStrategy(&svcConcurrency)

			// Cache the pool state before releasing lock
			a.pools[key] = &poolState{
				pool:       foundPool,
				revision:   currentRevision,
				inProgress: false,
			}

			// Create version->pool mapping
			a.versions[key] = &versionPoolRef{
				ver:      ver,
				poolID:   poolID,
				service:  service,
				strategy: strategy,
			}

			// Initialize poolSandboxes entry if needed
			if _, ok := a.poolSandboxes[poolID]; !ok {
				a.poolSandboxes[poolID] = &poolSandboxes{
					pool:      foundPool,
					sandboxes: []*sandbox{},
					service:   service,
					strategy:  strategy,
				}
			}

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
				if errors.Is(err, entity.ErrNotFound) {
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

		// Pool not found after retries - DeploymentLauncher should have created it
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
		for _, refVersion := range pool.ReferencedByVersions {
			if refVersion == versionID {
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
			s.lastRenewal = time.Now()
			return lease, nil
		}
	}

	return nil, fmt.Errorf("sandbox not found")
}

func (a *localActivator) watchSandboxes(ctx context.Context) {
	// Watch for sandbox changes: update status AND discover new RUNNING sandboxes
	// This is the single source of sandbox discovery for the activator
	// Retry loop to handle transient failures
	for {
		select {
		case <-ctx.Done():
			a.log.Info("sandbox watch context cancelled")
			return
		default:
		}

		a.log.Info("starting sandbox discovery watch")

		_, err := a.eac.WatchIndex(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
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
					// Need to fetch version to get the port configuration
					sbVersion := sb.Spec.Version
					if sbVersion != "" {
						verResp, err := a.eac.Get(ctx, sbVersion.String())
						if err == nil {
							var appVer core_v1alpha.AppVersion
							appVer.Decode(verResp.Entity().Entity())

							port := int64(3000)
							if appVer.Config.Port > 0 {
								port = appVer.Config.Port
							}
							if addr, err := netutil.BuildHTTPURL(sb.Network[0].Address, port); err == nil {
								newURL = addr
							}
						}
					}
				}

				// Now acquire write lock to update shared state
				a.mu.Lock()
				oldStatus := trackedSandbox.sandbox.Status
				trackedSandbox.sandbox.Status = sb.Status

				// Re-check conditions under lock and update URL if still needed
				if newURL != "" && trackedSandbox.url == "" && len(sb.Network) > 0 {
					trackedSandbox.url = newURL
					a.log.Debug("updated sandbox URL after network assignment", "sandbox", sb.ID, "url", newURL)
				}

				// Keep STOPPED and DEAD sandboxes in tracking so fail-fast logic can see them
				// They will be cleaned up later by periodic reconciliation or when
				// new RUNNING sandboxes are discovered

				// Notify waiters when sandbox status changes to RUNNING, STOPPED, or DEAD
				// RUNNING: sandbox is ready to serve traffic
				// STOPPED: sandbox process exited, will be cleaned up by reconciliation
				// DEAD: sandbox cleaned up, only entity remains
				// Notify ALL versions that reference this pool
				if oldStatus != sb.Status && (sb.Status == compute_v1alpha.RUNNING || sb.Status == compute_v1alpha.STOPPED || sb.Status == compute_v1alpha.DEAD) {
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
				if appVer.Config.Port > 0 {
					port = appVer.Config.Port
				}

				var err error
				addr, err = netutil.BuildHTTPURL(sb.Network[0].Address, port)
				if err != nil {
					a.log.Error("failed to build HTTP URL", "error", err, "sandbox", sb.ID)
					return nil
				}
			}

			// Get service concurrency and create strategy/tracker
			svcConcurrency, err := core_v1alpha.GetServiceConcurrency(&appVer, service)
			if err != nil {
				a.log.Error("failed to get service concurrency", "error", err, "sandbox", sb.ID, "service", service)
				return nil
			}
			strategy := concurrency.NewStrategy(&svcConcurrency)
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

		// Get app version for port config
		verResp, err := a.eac.Get(ctx, sbVersion.String())
		if err != nil {
			a.log.Error("failed to get version for sandbox", "sandbox", sb.ID, "version", sbVersion, "error", err)
			continue
		}

		var appVer core_v1alpha.AppVersion
		appVer.Decode(verResp.Entity().Entity())

		// Determine port from version config or default to 3000
		port := int64(3000)
		if appVer.Config.Port > 0 {
			port = appVer.Config.Port
		}

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
		svcConcurrency, err := core_v1alpha.GetServiceConcurrency(&appVer, service)
		if err != nil {
			a.log.Error("failed to get service concurrency for sandbox", "error", err, "sandbox", sb.ID, "service", service)
			continue
		}
		strategy := concurrency.NewStrategy(&svcConcurrency)

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

		// Get service concurrency and create strategy
		svcConcurrency, err := core_v1alpha.GetServiceConcurrency(&appVer, pool.Service)
		if err != nil {
			a.log.Error("failed to get service concurrency for pool", "pool", pool.ID, "service", pool.Service, "error", err)
			continue
		}
		strategy := concurrency.NewStrategy(&svcConcurrency)

		a.mu.Lock()

		// Cache pool state (for sentinel pattern in requestPoolCapacity)
		key := verKey{versionID.String(), pool.Service}
		a.pools[key] = &poolState{
			pool:       &pool,
			revision:   ent.Revision(),
			inProgress: false,
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
