package activator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netutil"
	"miren.dev/runtime/pkg/rpc/stream"
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

type verSandboxes struct {
	ver       *core_v1alpha.AppVersion
	sandboxes []*sandbox

	strategy concurrency.ConcurrencyStrategy
}

type verKey struct {
	ver, service string
}

type localActivator struct {
	mu       sync.Mutex
	versions map[verKey]*verSandboxes
	pools    map[verKey]*compute_v1alpha.SandboxPool // Track SandboxPool entities

	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
}

var _ AppActivator = (*localActivator)(nil)

func NewLocalActivator(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) AppActivator {
	la := &localActivator{
		log:      log.With("module", "activator"),
		eac:      eac,
		versions: make(map[verKey]*verSandboxes),
		pools:    make(map[verKey]*compute_v1alpha.SandboxPool),
	}

	// Recover existing sandboxes on startup
	la.log.Info("activator starting, attempting to recover existing sandboxes")
	if err := la.recoverSandboxes(ctx); err != nil {
		la.log.Error("failed to recover sandboxes", "error", err)
	} else {
		la.log.Info("activator recovery complete", "tracked_versions", len(la.versions))
	}

	// Recover existing pools
	la.log.Info("recovering sandbox pools")
	if err := la.recoverPools(ctx); err != nil {
		la.log.Error("failed to recover pools", "error", err)
	} else {
		la.log.Info("pool recovery complete", "tracked_pools", len(la.pools))
	}

	go la.InBackground(ctx)

	return la
}

func (a *localActivator) AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (*Lease, error) {
	key := verKey{ver.ID.String(), service}

	// Get service concurrency config
	svcConcurrency := a.getServiceConcurrency(ver, service)

	// Try to find an available sandbox with capacity
	a.mu.Lock()
	vs, ok := a.versions[key]
	if ok && len(vs.sandboxes) > 0 {
		a.log.Debug("checking existing sandboxes", "app", ver.App, "version", ver.Version, "sandboxes", len(vs.sandboxes))

		// Unified lease acquisition for both fixed and auto modes using ConcurrencyTracker
		start := rand.Int() % len(vs.sandboxes)
		for i := 0; i < len(vs.sandboxes); i++ {
			s := vs.sandboxes[(start+i)%len(vs.sandboxes)]
			if s.tracker.HasCapacity() {
				leaseSize := s.tracker.AcquireLease()
				s.lastRenewal = time.Now()

				// Read lastActivity while we hold the lock
				lastActivity := s.sandbox.LastActivity
				a.mu.Unlock()

				// Update sandbox last_activity with throttling (after releasing lock)
				a.updateSandboxLastActivity(ctx, s.sandbox, s.ent, lastActivity)

				lease := &Lease{
					ver:     ver,
					sandbox: s.sandbox,
					ent:     s.ent,
					pool:    service, // Pool identifier is now the service name
					service: service,
					Size:    leaseSize,
					URL:     s.url,
				}
				used := s.tracker.Used()
				a.log.Debug("reusing sandbox", "app", ver.App, "version", ver.Version, "sandbox", s.sandbox.ID, "used", used)
				return lease, nil
			}
		}
	}
	a.mu.Unlock()

	// No available sandboxes with capacity - need to scale up via pool
	a.log.Info("no available sandboxes, requesting capacity from pool",
		"app", ver.App,
		"version", ver.Version,
		"service", service)

	return a.ensurePoolAndWaitForSandbox(ctx, ver, service, svcConcurrency)
}

var ErrSandboxDiedEarly = fmt.Errorf("sandbox died while booting")
var ErrPoolTimeout = fmt.Errorf("timeout waiting for sandbox from pool")

// ensurePoolAndWaitForSandbox ensures a SandboxPool exists with sufficient DesiredInstances,
// then waits for a new RUNNING sandbox to become available in the pool.
func (a *localActivator) ensurePoolAndWaitForSandbox(ctx context.Context, ver *core_v1alpha.AppVersion, service string, svcConcurrency *core_v1alpha.ServiceConcurrency) (*Lease, error) {
	key := verKey{ver.ID.String(), service}

	// Ensure pool exists and has desired capacity
	pool, err := a.ensureSandboxPool(ctx, ver, service, svcConcurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure sandbox pool: %w", err)
	}

	a.log.Info("waiting for sandbox from pool",
		"app", ver.App,
		"service", service,
		"pool_id", pool.ID,
		"desired_instances", pool.DesiredInstances)

	// Watch for new sandboxes matching this pool (version + service)
	// We'll watch all sandboxes and filter by version and service labels
	watchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Determine port from config or default to 3000
	port := int64(3000)
	if ver.Config.Port > 0 {
		port = ver.Config.Port
	}

	// Channel to signal when we found a sandbox
	type sandboxResult struct {
		sandbox *sandbox
		err     error
	}
	resultCh := make(chan sandboxResult, 1)

	// Start watching for new sandboxes
	go func() {
		_, watchErr := a.eac.WatchIndex(watchCtx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
			if !op.HasEntity() {
				return nil
			}

			en := op.Entity().Entity()
			var sb compute_v1alpha.Sandbox
			sb.Decode(en)

			// Check if this sandbox matches our version and service
			if sb.Version != ver.ID {
				return nil
			}

			// Check service label
			var md core_v1alpha.Metadata
			md.Decode(en)
			serviceLabel, _ := md.Labels.Get("service")
			if serviceLabel != service {
				return nil
			}

			// Only consider RUNNING sandboxes
			if sb.Status != compute_v1alpha.RUNNING {
				return nil
			}

			// Check if we already track this sandbox
			a.mu.Lock()
			vs, ok := a.versions[key]
			if ok {
				for _, existing := range vs.sandboxes {
					if existing.sandbox.ID == sb.ID {
						a.mu.Unlock()
						return nil // Already tracking
					}
				}
			}
			a.mu.Unlock()

			// Found a new matching sandbox!
			a.log.Info("found new sandbox from pool", "sandbox", sb.ID, "service", service, "status", sb.Status)

			// Build HTTP URL
			if len(sb.Network) == 0 {
				resultCh <- sandboxResult{err: fmt.Errorf("sandbox has no network addresses")}
				return io.EOF
			}

			addr, err := netutil.BuildHTTPURL(sb.Network[0].Address, port)
			if err != nil {
				resultCh <- sandboxResult{err: fmt.Errorf("failed to build HTTP URL: %w", err)}
				return io.EOF
			}

			// Create strategy and tracker for this sandbox
			strategy := concurrency.NewStrategy(svcConcurrency)
			tracker := strategy.InitializeTracker()

			// Acquire first lease from the tracker
			_ = tracker.AcquireLease()

			lsb := &sandbox{
				sandbox:     &sb,
				ent:         en,
				lastRenewal: time.Now(),
				url:         addr,
				tracker:     tracker,
			}

			// Add to versions map
			a.mu.Lock()
			vs, ok = a.versions[key]
			if !ok {
				vs = &verSandboxes{
					ver:       ver,
					sandboxes: []*sandbox{},
					strategy:  strategy,
				}
				a.versions[key] = vs
			}
			vs.sandboxes = append(vs.sandboxes, lsb)
			a.mu.Unlock()

			// Return the lease
			resultCh <- sandboxResult{
				sandbox: lsb,
			}
			return io.EOF
		}))

		if watchErr != nil && !errors.Is(watchErr, io.EOF) && !errors.Is(watchErr, context.DeadlineExceeded) {
			resultCh <- sandboxResult{err: fmt.Errorf("watch failed: %w", watchErr)}
		}
	}()

	// Wait for result
	select {
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}
		return &Lease{
			ver:     ver,
			sandbox: result.sandbox.sandbox,
			ent:     result.sandbox.ent,
			pool:    service,
			service: service,
			Size:    result.sandbox.tracker.Used(),
			URL:     result.sandbox.url,
		}, nil
	case <-watchCtx.Done():
		return nil, fmt.Errorf("%w: no sandbox became available within 2 minutes", ErrPoolTimeout)
	}
}

// ensureSandboxPool creates or updates a SandboxPool for the given app version and service.
// It increments DesiredInstances to request additional capacity.
func (a *localActivator) ensureSandboxPool(ctx context.Context, ver *core_v1alpha.AppVersion, service string, svcConcurrency *core_v1alpha.ServiceConcurrency) (*compute_v1alpha.SandboxPool, error) {
	key := verKey{ver.ID.String(), service}

	a.mu.Lock()
	existingPool, exists := a.pools[key]
	a.mu.Unlock()

	if exists {
		// Update existing pool - increment DesiredInstances
		existingPool.DesiredInstances++

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(existingPool.ID.String())
		rpcE.SetAttrs(entity.New(
			existingPool.Encode,
		).Attrs())

		_, err := a.eac.Put(ctx, &rpcE)
		if err != nil {
			return nil, fmt.Errorf("failed to update pool: %w", err)
		}

		a.mu.Lock()
		a.pools[key] = existingPool
		a.mu.Unlock()

		a.log.Info("incremented pool capacity", "pool", existingPool.ID, "desired_instances", existingPool.DesiredInstances)
		return existingPool, nil
	}

	// Create new pool
	spec, err := a.buildSandboxSpec(ctx, ver, service, svcConcurrency)
	if err != nil {
		return nil, fmt.Errorf("failed to build sandbox spec: %w", err)
	}

	pool, err := a.createSandboxPool(ctx, ver, service, svcConcurrency, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	a.mu.Lock()
	a.pools[key] = pool
	a.mu.Unlock()

	a.log.Info("created new sandbox pool", "pool", pool.ID, "service", service, "desired_instances", pool.DesiredInstances)
	return pool, nil
}

func (a *localActivator) ReleaseLease(ctx context.Context, lease *Lease) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	vs, ok := a.versions[verKey{lease.ver.ID.String(), lease.service}]
	if !ok {
		return nil
	}

	// Release capacity via tracker (mode-specific behavior is handled by strategy)
	for _, s := range vs.sandboxes {
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

	vs, ok := a.versions[verKey{lease.ver.ID.String(), lease.service}]
	if !ok {
		return nil, fmt.Errorf("version not found")
	}

	for _, s := range vs.sandboxes {
		if s.sandbox == lease.sandbox {
			s.lastRenewal = time.Now()
			return lease, nil
		}
	}

	return nil, fmt.Errorf("sandbox not found")
}

func (a *localActivator) InBackground(ctx context.Context) {
	// Watch for sandbox status changes to update our local tracking
	// Retry loop to handle transient failures
	for {
		select {
		case <-ctx.Done():
			a.log.Info("sandbox watch context cancelled")
			return
		default:
		}

		a.log.Info("starting sandbox status watch")

		_, err := a.eac.WatchIndex(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
			if !op.HasEntity() {
				return nil
			}

			en := op.Entity().Entity()
			var sb compute_v1alpha.Sandbox
			sb.Decode(en)

			// Update status in our tracking if we have this sandbox
			a.mu.Lock()
			defer a.mu.Unlock()

			for _, vs := range a.versions {
				for _, s := range vs.sandboxes {
					if s.sandbox.ID == sb.ID {
						// Update status
						oldStatus := s.sandbox.Status
						s.sandbox.Status = sb.Status

						if oldStatus != sb.Status {
							a.log.Info("sandbox status changed", "sandbox", sb.ID, "old_status", oldStatus, "new_status", sb.Status)
						}
						return nil
					}
				}
			}

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
	skippedNoVersion := 0
	recoveryTime := time.Now()

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Only recover RUNNING sandboxes
		if sb.Status != compute_v1alpha.RUNNING {
			skippedNotRunning++
			continue
		}

		// Skip sandboxes without a version (e.g., buildkit sandboxes)
		if sb.Version == "" {
			skippedNoVersion++
			continue
		}

		// Get app version to determine service
		verResp, err := a.eac.Get(ctx, sb.Version.String())
		if err != nil {
			a.log.Error("failed to get version for sandbox", "sandbox", sb.ID, "version", sb.Version, "error", err)
			continue
		}

		var appVer core_v1alpha.AppVersion
		appVer.Decode(verResp.Entity().Entity())

		// Extract service from sandbox labels
		var md core_v1alpha.Metadata
		md.Decode(ent.Entity())
		service, _ := md.Labels.Get("service")
		if service == "" {
			a.log.Warn("sandbox missing service label", "sandbox", sb.ID)
			continue
		}

		// Determine port from version config or default to 3000
		port := int64(3000)
		if appVer.Config.Port > 0 {
			port = appVer.Config.Port
		}

		// Build HTTP URL from address and port (handles CIDR and IPv6)
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
		svcConcurrency := a.getServiceConcurrency(&appVer, service)
		strategy := concurrency.NewStrategy(svcConcurrency)

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

		// Add to versions map - need mutex protection
		key := verKey{appVer.ID.String(), service}
		a.mu.Lock()
		vs, ok := a.versions[key]
		if !ok {
			vs = &verSandboxes{
				ver:       &appVer,
				sandboxes: []*sandbox{},
				strategy:  strategy,
			}
			a.versions[key] = vs
		}
		vs.sandboxes = append(vs.sandboxes, lsb)
		a.mu.Unlock()
		recoveredCount++

		a.log.Info("recovered sandbox", "app", appVer.App, "version", appVer.Version, "sandbox", sb.ID, "service", service, "url", addr)
	}

	a.log.Info("sandbox recovery complete",
		"recovered", recoveredCount,
		"skipped_not_running", skippedNotRunning,
		"skipped_no_version", skippedNoVersion,
		"tracked_keys", len(a.versions))

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

		key := verKey{versionID.String(), pool.Service}

		a.mu.Lock()
		a.pools[key] = &pool
		a.mu.Unlock()

		a.log.Info("recovered pool", "pool", pool.ID, "service", pool.Service, "version", versionID, "desired_instances", pool.DesiredInstances)
	}

	return nil
}

func (a *localActivator) getServiceConcurrency(ver *core_v1alpha.AppVersion, service string) *core_v1alpha.ServiceConcurrency {
	// Find service-specific concurrency config
	for _, svc := range ver.Config.Services {
		if svc.Name == service {
			return &svc.ServiceConcurrency
		}
	}

	// Return default config
	return &core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "15m",
	}
}

func (a *localActivator) createSandboxPool(ctx context.Context, ver *core_v1alpha.AppVersion, service string, svcConcurrency *core_v1alpha.ServiceConcurrency, spec *compute_v1alpha.SandboxSpec) (*compute_v1alpha.SandboxPool, error) {
	// Calculate pool parameters based on mode
	var maxSlots int64
	var leaseSlots int64
	var scaleDownDelay time.Duration

	if svcConcurrency.Mode == "fixed" {
		// Fixed mode: no slot tracking, just instance count
		maxSlots = 1
		leaseSlots = 1
		scaleDownDelay = 0 // Never scale down
	} else {
		// Auto mode: slot-based capacity management
		if svcConcurrency.RequestsPerInstance <= 0 {
			maxSlots = 10 // default
		} else {
			maxSlots = svcConcurrency.RequestsPerInstance
		}

		leaseSlots = int64(float32(maxSlots) * 0.20)
		if leaseSlots < 1 {
			leaseSlots = 1
		}

		// Scale down delay (default 15 minutes)
		scaleDownDelay = 15 * time.Minute
		if d := svcConcurrency.ScaleDownDelay; d != "" {
			if parsed, err := time.ParseDuration(d); err == nil {
				scaleDownDelay = parsed
			} else {
				a.log.Warn("invalid ScaleDownDelay, using default", "value", d, "error", err)
			}
		}
	}

	pool := compute_v1alpha.SandboxPool{
		Service:          service,
		SandboxSpec:      *spec,
		Mode:             compute_v1alpha.SandboxPoolMode(svcConcurrency.Mode),
		MaxSlots:         maxSlots,
		LeaseSlots:       leaseSlots,
		ScaleDownDelay:   scaleDownDelay,
		DesiredInstances: 1, // Start with 1 instance
	}

	// Set num_instances for fixed mode
	if svcConcurrency.Mode == "fixed" {
		if svcConcurrency.NumInstances > 0 {
			pool.DesiredInstances = svcConcurrency.NumInstances
		}
	}

	name := idgen.GenNS("pool")

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name: name,
			Labels: types.LabelSet(
				"app", ver.App.String(),
				"version", ver.Version,
				"service", service,
			),
		}).Encode,
		entity.Ident, "pool/"+name,
		pool.Encode,
	).Attrs())

	pr, err := a.eac.Put(ctx, &rpcE)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool entity: %w", err)
	}

	pool.ID = entity.Id(pr.Id())
	return &pool, nil
}

func (a *localActivator) buildSandboxSpec(ctx context.Context, ver *core_v1alpha.AppVersion, service string, svcConcurrency *core_v1alpha.ServiceConcurrency) (*compute_v1alpha.SandboxSpec, error) {
	// Get app metadata
	appResp, err := a.eac.Get(ctx, ver.App.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get app: %w", err)
	}

	var appMD core_v1alpha.Metadata
	appMD.Decode(appResp.Entity().Entity())

	spec := &compute_v1alpha.SandboxSpec{
		Version:      ver.ID,
		LogEntity:    ver.App.String(),
		LogAttribute: types.LabelSet("stage", "app-run", "service", service),
	}

	// Determine port from config or default to 3000
	port := int64(3000)
	if ver.Config.Port > 0 {
		port = ver.Config.Port
	}

	appCont := compute_v1alpha.SandboxSpecContainer{
		Name:  "app",
		Image: ver.ImageUrl,
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

	spec.Container = []compute_v1alpha.SandboxSpecContainer{appCont}

	return spec, nil
}

// updateSandboxLastActivity updates the last_activity timestamp on a sandbox entity
// with throttling to avoid excessive writes to etcd (~30s granularity)
// lastActivity should be passed by the caller who already holds a.mu
func (a *localActivator) updateSandboxLastActivity(ctx context.Context, sb *compute_v1alpha.Sandbox, sbEnt *entity.Entity, lastActivity time.Time) {
	now := time.Now()

	// Only update if > 30 seconds since last update
	if !lastActivity.IsZero() && now.Sub(lastActivity) < 30*time.Second {
		return
	}

	// Update in background to avoid blocking lease acquisition
	go func() {
		updateCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(sbEnt.Id().String())
		rpcE.SetAttrs(entity.New(
			(&compute_v1alpha.Sandbox{
				LastActivity: now,
			}).Encode,
		).Attrs())

		if _, err := a.eac.Put(updateCtx, &rpcE); err != nil {
			a.log.Error("failed to update sandbox last_activity", "sandbox", sbEnt.Id(), "error", err)
		}
	}()
}
