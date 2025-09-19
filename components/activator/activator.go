package activator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/netip"
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
	AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, pool, service string) (*Lease, error)
	ReleaseLease(ctx context.Context, lease *Lease) error
	RenewLease(ctx context.Context, lease *Lease) (*Lease, error)
}

type sandbox struct {
	sandbox     *compute_v1alpha.Sandbox
	ent         *entity.Entity
	lastRenewal time.Time
	url         string
	maxSlots    int
	inuseSlots  int
}

type verSandboxes struct {
	ver       *core_v1alpha.AppVersion
	sandboxes []*sandbox

	leaseSlots int
}

type verKey struct {
	ver, pool, service string
}

type localActivator struct {
	mu               sync.Mutex
	versions         map[verKey]*verSandboxes
	pendingCreations map[verKey]int // Track pending sandbox creations per service

	log *slog.Logger
	eac *entityserver_v1alpha.EntityAccessClient
}

var _ AppActivator = (*localActivator)(nil)

func NewLocalActivator(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) AppActivator {
	la := &localActivator{
		log:              log.With("module", "activator"),
		eac:              eac,
		versions:         make(map[verKey]*verSandboxes),
		pendingCreations: make(map[verKey]int),
	}

	// Recover existing sandboxes on startup
	la.log.Info("activator starting, attempting to recover existing sandboxes")
	if err := la.recoverSandboxes(ctx); err != nil {
		la.log.Error("failed to recover sandboxes", "error", err)
	} else {
		la.log.Info("activator recovery complete", "tracked_versions", len(la.versions))
	}

	go la.InBackground(ctx)

	return la
}

// ensureFixedInstances ensures that fixed mode services have the required number of instances running
func (a *localActivator) ensureFixedInstances(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Track which versions/services we've seen
	seenServices := make(map[verKey]bool)

	// Check existing sandboxes
	for key, vs := range a.versions {
		svcConcurrency := a.getServiceConcurrency(vs.ver, key.service)
		if svcConcurrency.Mode != "fixed" {
			continue
		}

		seenServices[key] = true

		// Count running sandboxes
		runningCount := 0
		for _, sb := range vs.sandboxes {
			if sb.sandbox.Status == compute_v1alpha.RUNNING {
				runningCount++
			}
		}

		targetInstances := int(svcConcurrency.NumInstances)
		if targetInstances <= 0 {
			targetInstances = 1
		}

		// Account for pending creations to avoid over-provisioning
		pendingCount := a.pendingCreations[key]
		totalExpected := runningCount + pendingCount

		// Start additional instances if needed
		for i := totalExpected; i < targetInstances; i++ {
			a.log.Info("starting fixed instance", "app", vs.ver.App, "service", key.service, "current", runningCount, "pending", pendingCount, "target", targetInstances)

			// Mark as pending before releasing lock
			a.pendingCreations[key]++

			// Create sandbox in background to avoid holding lock
			go func() {
				_, err := a.activateApp(ctx, vs.ver, key.pool, key.service)

				// Update pending count after creation attempt
				a.mu.Lock()
				a.pendingCreations[key]--
				a.mu.Unlock()

				if err != nil {
					a.log.Error("failed to start fixed instance", "app", vs.ver.App, "service", key.service, "error", err)
				}
			}()
		}
	}
}

func (a *localActivator) AcquireLease(ctx context.Context, ver *core_v1alpha.AppVersion, pool, service string) (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := verKey{ver.ID.String(), pool, service}
	vs, ok := a.versions[key]

	// Get service concurrency config
	svcConcurrency := a.getServiceConcurrency(ver, service)

	if !ok {
		a.log.Info("version key not found in tracked versions",
			"app", ver.App,
			"version", ver.Version,
			"version_id", ver.ID.String(),
			"pool", pool,
			"service", service,
			"key", key,
			"tracked_keys", len(a.versions))
		// Log what keys we ARE tracking for debugging
		for k := range a.versions {
			a.log.Debug("tracked key", "key", k)
		}
		return a.activateApp(ctx, ver, pool, service)
	}

	if len(vs.sandboxes) == 0 {
		a.log.Info("no sandboxes available in version slot, creating new sandbox",
			"app", ver.App,
			"version", ver.Version,
			"key", key)
	} else {
		a.log.Debug("reusing existing sandboxes", "app", ver.App, "version", ver.Version, "sandboxes", len(vs.sandboxes))

		// For fixed mode, just round-robin across available sandboxes
		if svcConcurrency.Mode == "fixed" {
			// Find a running sandbox
			start := rand.Int() % len(vs.sandboxes)
			for i := 0; i < len(vs.sandboxes); i++ {
				s := vs.sandboxes[(start+i)%len(vs.sandboxes)]
				if s.sandbox.Status == compute_v1alpha.RUNNING {
					s.lastRenewal = time.Now()
					a.log.Debug("reusing fixed mode sandbox", "app", ver.App, "version", ver.Version, "sandbox", s.sandbox.ID, "service", service)
					return &Lease{
						ver:     ver,
						sandbox: s.sandbox,
						ent:     s.ent,
						pool:    pool,
						service: service,
						Size:    1, // Fixed mode doesn't use slots
						URL:     s.url,
					}, nil
				}
			}
			// No running sandboxes found, will create new one below
			a.log.Info("no running sandboxes for fixed mode service, creating new sandbox", "app", ver.App, "version", ver.Version, "service", service)
		} else {
			// Auto mode: use slot-based allocation
			start := rand.Int() % len(vs.sandboxes)
			for i := 0; i < len(vs.sandboxes); i++ {
				s := vs.sandboxes[(start+i)%len(vs.sandboxes)]
				if s.inuseSlots+vs.leaseSlots < s.maxSlots {
					s.inuseSlots += vs.leaseSlots
					s.lastRenewal = time.Now()

					a.log.Debug("reusing sandbox", "app", ver.App, "version", ver.Version, "sandbox", s.sandbox.ID, "in-use", s.inuseSlots)
					return &Lease{
						ver:     ver,
						sandbox: s.sandbox,
						ent:     s.ent,
						pool:    pool,
						service: service,
						Size:    vs.leaseSlots,
						URL:     s.url,
					}, nil
				}
			}
			a.log.Info("no space in existing sandboxes, creating new sandbox for app", "app", ver.App, "version", ver.Version)
		}
	}

	return a.activateApp(ctx, ver, pool, service)
}

var ErrSandboxDiedEarly = fmt.Errorf("sandbox died while booting")

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
			Labels: types.LabelSet("app", appMD.Name, "pool", pool, "service", service),
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

	a.log.Debug("watching sandbox", "app", ver.App, "sandbox", pr.Id())

	var localErr error

	a.eac.WatchEntity(ctx, pr.Id(), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		var sb compute_v1alpha.Sandbox

		if op.HasEntity() {
			en := op.Entity().Entity()
			sb.Decode(en)

			runningSB = sb
			sbEnt = en

			switch sb.Status {
			case compute_v1alpha.RUNNING:
				a.log.Info("sandbox is running", "app", ver.App, "sandbox", pr.Id(), "status", sb.Status)
				// TODO figure out a better way to signal that we're done with the watch.
				return io.EOF
			case compute_v1alpha.STOPPED, compute_v1alpha.DEAD:
				a.log.Info("sandbox failed to start while waiting for activator", "app", ver.App, "sandbox", pr.Id(), "status", sb.Status)
				localErr = fmt.Errorf("%w: sandbox failed to start: %s (%s)", ErrSandboxDiedEarly, pr.Id(), sb.Status)
				return io.EOF
			default:
				a.log.Debug("sandbox status update", "app", ver.App, "sandbox", pr.Id(), "status", sb.Status)
			}
		}

		return nil
	}))

	if runningSB.Status != compute_v1alpha.RUNNING {
		a.log.Error("sandbox did not start successfully",
			"app", ver.App,
			"sandbox", pr.Id(),
			"error", "sandbox did not reach RUNNING status")
		if localErr == nil {
			localErr = fmt.Errorf("sandbox did not reach RUNNING status: %s", pr.Id())
		}
		return nil, localErr
	}

	// Parse the address to extract just the IP from potential CIDR notation
	ip := runningSB.Network[0].Address
	if prefix, err := netip.ParsePrefix(ip); err == nil {
		// New format: extract IP from CIDR
		ip = prefix.Addr().String()
	} else if _, err := netip.ParseAddr(ip); err != nil {
		// Not a valid IP either, return error
		return nil, fmt.Errorf("invalid address format: %s", ip)
	}
	// If it's already a plain IP (old format), use as-is
	addr := fmt.Sprintf("http://%s:%d", ip, port)

	// Get service-specific concurrency configuration
	svcConcurrency := a.getServiceConcurrency(ver, service)

	var leaseSlots int
	var maxSlots int

	if svcConcurrency.Mode == "fixed" {
		// For fixed mode, we don't use slots - the sandbox handles one instance
		leaseSlots = 1
		maxSlots = 1
	} else {
		// For auto mode, use requests_per_instance as max slots
		if svcConcurrency.RequestsPerInstance <= 0 {
			maxSlots = 10 // default
		} else {
			maxSlots = int(svcConcurrency.RequestsPerInstance)
		}

		// Lease slots is 20% of max slots
		leaseSlots = int(float32(maxSlots) * 0.20)
		if leaseSlots < 1 {
			leaseSlots = 1
		}
	}

	lsb := &sandbox{
		sandbox:     &runningSB,
		ent:         sbEnt,
		lastRenewal: time.Now(),
		url:         addr,
		maxSlots:    maxSlots,
		inuseSlots:  leaseSlots,
	}

	lease := &Lease{
		ver:     ver,
		sandbox: lsb.sandbox,
		ent:     lsb.ent,
		pool:    pool,
		service: service,
		Size:    leaseSlots,
		URL:     lsb.url,
	}

	key := verKey{ver.ID.String(), pool, service}

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

	vs, ok := a.versions[verKey{lease.ver.ID.String(), lease.pool, lease.service}]
	if !ok {
		return nil
	}

	// Get service concurrency config
	svcConcurrency := a.getServiceConcurrency(lease.ver, lease.service)

	// Only adjust slots for auto mode
	if svcConcurrency.Mode != "fixed" {
		for _, s := range vs.sandboxes {
			if s.sandbox == lease.sandbox {
				s.inuseSlots -= lease.Size
				break
			}
		}
	}

	return nil
}

func (a *localActivator) RenewLease(ctx context.Context, lease *Lease) (*Lease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	vs, ok := a.versions[verKey{lease.ver.ID.String(), lease.pool, lease.service}]
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

// removeSandbox removes a sandbox from tracking across all version keys
func (a *localActivator) removeSandbox(sandboxID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	removedCount := 0
	for key, vs := range a.versions {
		originalCount := len(vs.sandboxes)
		newSandboxes := make([]*sandbox, 0, originalCount)

		for _, sb := range vs.sandboxes {
			if sb.sandbox.ID.String() != sandboxID {
				newSandboxes = append(newSandboxes, sb)
			} else {
				removedCount++
				a.log.Info("removing sandbox from tracking",
					"sandbox_id", sandboxID,
					"app", vs.ver.App,
					"version", vs.ver.Version,
					"pool", key.pool,
					"service", key.service,
					"status", sb.sandbox.Status)
			}
		}

		vs.sandboxes = newSandboxes

		// Clean up empty version entries
		if len(vs.sandboxes) == 0 && a.pendingCreations[key] == 0 {
			delete(a.versions, key)
			a.log.Debug("removed empty version entry", "key", key)
		}
	}

	if removedCount > 0 {
		a.log.Info("sandbox removed from tracking", "sandbox_id", sandboxID, "removed_from_keys", removedCount)
	}
}

// watchSandboxes monitors sandbox status changes and removes non-RUNNING sandboxes
func (a *localActivator) watchSandboxes(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			a.log.Info("sandbox watch context cancelled")
			return
		default:
		}

		a.log.Info("starting sandbox watch")

		// Watch all sandbox entities for status changes
		_, err := a.eac.WatchIndex(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
			switch op.OperationType() {
			case entityserver_v1alpha.EntityOperationDelete:
				if op.EntityId() != "" {
					a.log.Debug("sandbox entity deleted", "id", op.EntityId())
					a.removeSandbox(op.EntityId())
				}
				return nil

			case entityserver_v1alpha.EntityOperationCreate, entityserver_v1alpha.EntityOperationUpdate:
				if op.HasEntity() {
					var sb compute_v1alpha.Sandbox
					sb.Decode(op.Entity().Entity())

					// Remove sandbox if it's not in RUNNING state
					if sb.Status != compute_v1alpha.RUNNING {
						a.log.Debug("sandbox status changed to non-RUNNING",
							"sandbox_id", sb.ID,
							"status", sb.Status)
						a.removeSandbox(sb.ID.String())
					}
				}
				return nil

			default:
				// Unknown operation type, log for debugging
				a.log.Warn("unknown entity operation type", "operation", op.Operation())
				return nil
			}
		}))

		if err != nil {
			if ctx.Err() != nil {
				// Context was cancelled, exit gracefully
				a.log.Info("sandbox watch stopped due to context cancellation")
				return
			}
			a.log.Error("sandbox watch ended with error, will restart", "error", err)
			// Wait a bit before restarting to avoid tight loop on persistent errors
			select {
			case <-time.After(5 * time.Second):
				// Continue to restart the watch
			case <-ctx.Done():
				return
			}
		}
	}
}

func (a *localActivator) InBackground(ctx context.Context) {
	// Start watching sandboxes for status changes
	go a.watchSandboxes(ctx)

	retireTicker := time.NewTicker(20 * time.Second)
	defer retireTicker.Stop()

	fixedTicker := time.NewTicker(30 * time.Second)
	defer fixedTicker.Stop()

	for {
		select {
		case <-retireTicker.C:
			a.retireUnusedSandboxes()
		case <-fixedTicker.C:
			a.ensureFixedInstances(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// getLabel extracts a label value from metadata, returning defaultValue if not found
func getLabel(metadata *core_v1alpha.Metadata, key string, defaultValue string) string {
	for _, label := range metadata.Labels {
		if label.Key == key {
			return label.Value
		}
	}
	return defaultValue
}

// getServiceConcurrency returns the concurrency configuration for a specific service
// If no service-specific config is found, it falls back to defaults based on service name
func (a *localActivator) getServiceConcurrency(ver *core_v1alpha.AppVersion, service string) *core_v1alpha.ServiceConcurrency {
	// Look for service-specific configuration
	for _, svc := range ver.Config.Services {
		if svc.Name == service {
			return &svc.ServiceConcurrency
		}
	}

	// Check for legacy global concurrency configuration
	if ver.Config.Concurrency.Fixed > 0 || ver.Config.Concurrency.Auto > 0 {
		// Use global config for backward compatibility
		if ver.Config.Concurrency.Fixed > 0 {
			return &core_v1alpha.ServiceConcurrency{
				Mode:                "auto",
				RequestsPerInstance: ver.Config.Concurrency.Fixed,
				ScaleDownDelay:      "2m", // Legacy default
			}
		}
		// Handle auto mode from legacy config if needed
	}

	// Apply defaults based on service name
	if service == "web" {
		return &core_v1alpha.ServiceConcurrency{
			Mode:                "auto",
			RequestsPerInstance: 10,
			ScaleDownDelay:      "15m",
		}
	}

	// Default for all other services is fixed with 1 instance
	return &core_v1alpha.ServiceConcurrency{
		Mode:         "fixed",
		NumInstances: 1,
	}
}

func (a *localActivator) recoverSandboxes(ctx context.Context) error {
	// List all sandboxes
	resp, err := a.eac.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	a.log.Info("recovering sandboxes on startup", "total_sandboxes", len(resp.Values()))

	recoveryTime := time.Now()
	runningCount := 0
	skippedNoVersion := 0
	skippedNotRunning := 0
	recoveredCount := 0

	for _, ent := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(ent.Entity())

		// Only recover running sandboxes
		if sb.Status != compute_v1alpha.RUNNING {
			skippedNotRunning++
			a.log.Debug("skipping non-running sandbox", "sandbox", sb.ID, "status", sb.Status)
			continue
		}
		runningCount++

		// Skip sandboxes without a version reference
		if sb.Version.String() == "" {
			skippedNoVersion++
			a.log.Debug("skipping sandbox without version", "sandbox", sb.ID)
			continue
		}

		// Get the app version details
		verResp, err := a.eac.Get(ctx, sb.Version.String())
		if err != nil {
			a.log.Error("failed to get app version", "version", sb.Version, "error", err)
			continue
		}

		var appVer core_v1alpha.AppVersion
		appVer.Decode(verResp.Entity().Entity())

		// Extract pool and service from sandbox labels - default to "default" if not found
		var metadata core_v1alpha.Metadata
		metadata.Decode(ent.Entity())
		pool := getLabel(&metadata, "pool", "default")
		service := getLabel(&metadata, "service", "web") // Default to web if not found

		// Calculate the URL
		port := int64(3000)
		if appVer.Config.Port > 0 {
			port = appVer.Config.Port
		}

		// Skip if no network address assigned yet
		if len(sb.Network) == 0 || sb.Network[0].Address == "" {
			a.log.Debug("skipping sandbox without network address", "sandbox", sb.ID)
			continue
		}

		// Parse the address to extract just the IP from potential CIDR notation
		ipAddr := sb.Network[0].Address
		if prefix, err := netip.ParsePrefix(ipAddr); err == nil {
			// New format: extract IP from CIDR
			ipAddr = prefix.Addr().String()
		} else if _, err := netip.ParseAddr(ipAddr); err != nil {
			// Not a valid IP either, skip this sandbox
			a.log.Error("invalid address format", "address", ipAddr, "sandbox", sb.ID)
			continue
		}
		// If it's already a plain IP (old format), use as-is
		addr := fmt.Sprintf("http://%s:%d", ipAddr, port)

		// Get service-specific concurrency configuration
		svcConcurrency := a.getServiceConcurrency(&appVer, service)

		// Calculate lease slots
		var leaseSlots int
		var maxSlots int

		if svcConcurrency.Mode == "fixed" {
			leaseSlots = 1
			maxSlots = 1
		} else {
			if svcConcurrency.RequestsPerInstance <= 0 {
				maxSlots = 10 // default
			} else {
				maxSlots = int(svcConcurrency.RequestsPerInstance)
			}

			leaseSlots = int(float32(maxSlots) * 0.20)
			if leaseSlots < 1 {
				leaseSlots = 1
			}
		}

		// Create sandbox tracking entry
		lsb := &sandbox{
			sandbox:     &sb,
			ent:         ent.Entity(),
			lastRenewal: recoveryTime, // Set to now to give grace period
			url:         addr,
			maxSlots:    maxSlots,
			inuseSlots:  0, // Start with no slots in use
		}

		// Add to versions map
		key := verKey{appVer.ID.String(), pool, service}
		vs, ok := a.versions[key]
		if !ok {
			vs = &verSandboxes{
				ver:        &appVer,
				sandboxes:  []*sandbox{},
				leaseSlots: leaseSlots,
			}
			a.versions[key] = vs
		}
		vs.sandboxes = append(vs.sandboxes, lsb)
		recoveredCount++

		a.log.Info("recovered sandbox", "app", appVer.App, "version", appVer.Version, "sandbox", sb.ID, "pool", pool, "service", service, "url", addr)
	}

	a.log.Info("sandbox recovery summary",
		"total", len(resp.Values()),
		"running", runningCount,
		"recovered", recoveredCount,
		"skipped_not_running", skippedNotRunning,
		"skipped_no_version", skippedNoVersion,
		"tracked_keys", len(a.versions))

	return nil
}

func (a *localActivator) retireUnusedSandboxes() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for key, vs := range a.versions {
		// Get service-specific concurrency config
		svcConcurrency := a.getServiceConcurrency(vs.ver, key.service)

		// Never retire fixed mode sandboxes
		if svcConcurrency.Mode == "fixed" {
			continue
		}

		// Calculate scale down delay for auto mode
		scaleDownDelay := 2 * time.Minute // default
		if svcConcurrency.ScaleDownDelay != "" {
			if duration, err := time.ParseDuration(svcConcurrency.ScaleDownDelay); err == nil {
				scaleDownDelay = duration
			}
		}

		var newSandboxes []*sandbox

		for _, sb := range vs.sandboxes {
			if time.Since(sb.lastRenewal) > scaleDownDelay {
				a.log.Debug("retiring unused sandbox", "app", vs.ver.App, "sandbox", sb.sandbox.ID, "service", key.service, "idle_time", time.Since(sb.lastRenewal))

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
