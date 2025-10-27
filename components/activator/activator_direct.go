package activator

// This file contains the pre-pool direct sandbox creation logic.
// It is used when UseSandboxPools=false, providing the original activator behavior
// before the SandboxPool integration (PR #256).

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/concurrency"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netutil"
)

// acquireLeaseDirectMode tries to reuse existing sandboxes or creates a new one
func (a *localActivator) acquireLeaseDirectMode(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (*Lease, error) {
	key := verKey{ver.ID.String(), service}

	// Try to find an available sandbox with capacity (read lock for scanning)
	a.mu.RLock()
	vs, ok := a.versions[key]
	var candidateSandbox *sandbox
	if ok && len(vs.sandboxes) > 0 {
		a.log.Debug("checking existing sandboxes", "app", ver.App, "version", ver.Version, "sandboxes", len(vs.sandboxes))

		// Scan for a sandbox with capacity
		start := rand.Int() % len(vs.sandboxes)
		for i := 0; i < len(vs.sandboxes); i++ {
			s := vs.sandboxes[(start+i)%len(vs.sandboxes)]
			if s.sandbox.Status == compute_v1alpha.RUNNING && s.tracker.HasCapacity() {
				candidateSandbox = s
				break
			}
		}
	}
	a.mu.RUnlock()

	// If we found a candidate, acquire write lock and double-check capacity
	if candidateSandbox != nil {
		a.mu.Lock()
		// Double-check capacity still available (may have changed between locks)
		if candidateSandbox.tracker.HasCapacity() {
			leaseSize := candidateSandbox.tracker.AcquireLease()
			candidateSandbox.lastRenewal = time.Now()

			lease := &Lease{
				ver:     ver,
				sandbox: candidateSandbox.sandbox,
				ent:     candidateSandbox.ent,
				pool:    service,
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
	}

	// No available sandboxes - need to create a new one
	a.log.Info("no available sandboxes, creating new one",
		"app", ver.App,
		"version", ver.Version,
		"service", service)

	return a.activateApp(ctx, ver, service)
}

// ensureFixedInstances ensures that fixed mode services have the required number of instances running
func (a *localActivator) ensureFixedInstances(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Track which versions/services we've seen
	seenServices := make(map[verKey]bool)

	// Check existing sandboxes
	for key, vs := range a.versions {
		// Skip non-fixed mode services
		targetInstances := vs.strategy.DesiredInstances()
		if targetInstances == 0 {
			// Auto mode (scale to zero) - skip
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

		// Account for pending creations to avoid over-provisioning
		pendingCount := a.pendingCreations[key]
		totalExpected := runningCount + pendingCount

		// Start additional instances if needed
		for i := totalExpected; i < targetInstances; i++ {
			a.log.Info("starting fixed instance", "app", vs.ver.App, "service", key.service, "current", runningCount, "pending", pendingCount, "target", targetInstances)

			// Mark as pending before releasing lock
			a.pendingCreations[key]++

			// Create sandbox in background to avoid holding lock
			go func(k verKey, v *core_v1alpha.AppVersion) {
				_, err := a.activateApp(ctx, v, k.service)

				// Update pending count after creation attempt
				a.mu.Lock()
				a.pendingCreations[k]--
				a.mu.Unlock()

				if err != nil {
					a.log.Error("failed to start fixed instance", "app", v.App, "service", k.service, "error", err)
				}
			}(key, vs.ver)
		}
	}
}

// activateApp creates a new sandbox directly (pre-pool behavior)
func (a *localActivator) activateApp(ctx context.Context, ver *core_v1alpha.AppVersion, service string) (*Lease, error) {
	gr, err := a.eac.Get(ctx, ver.App.String())
	if err != nil {
		return nil, err
	}

	var app core_v1alpha.App
	app.Decode(gr.Entity().Entity())

	var appMD core_v1alpha.Metadata
	appMD.Decode(gr.Entity().Entity())

	// Build sandbox spec
	spec, err := a.buildSandboxSpec(ctx, ver, service)
	if err != nil {
		return nil, fmt.Errorf("failed to build sandbox spec: %w", err)
	}

	// Create sandbox entity
	sbName := idgen.GenNS("sb")

	sb := compute_v1alpha.Sandbox{
		Status:  compute_v1alpha.PENDING,
		Version: ver.ID,
		Spec:    *spec,
	}

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.New(
		(&core_v1alpha.Metadata{
			Name: sbName,
			Labels: types.LabelSet(
				"service", service,
			),
		}).Encode,
		entity.Ident, entity.MustKeyword("sandbox/"+sbName),
		sb.Encode,
	).Attrs())

	pr, err := a.eac.Put(ctx, &rpcE)
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox entity: %w", err)
	}

	sb.ID = entity.Id(pr.Id())

	a.log.Info("created sandbox", "sandbox", sb.ID, "app", ver.App, "service", service)

	// Wait for sandbox to become RUNNING and get network address
	pollCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return nil, fmt.Errorf("timeout waiting for sandbox to start")
		case <-ticker.C:
			resp, err := a.eac.Get(pollCtx, sb.ID.String())
			if err != nil {
				a.log.Error("failed to get sandbox", "sandbox", sb.ID, "error", err)
				continue
			}

			var currentSB compute_v1alpha.Sandbox
			currentSB.Decode(resp.Entity().Entity())

			if currentSB.Status == compute_v1alpha.RUNNING && len(currentSB.Network) > 0 {
				// Sandbox is ready
				port := int64(3000)
				if ver.Config.Port > 0 {
					port = ver.Config.Port
				}

				addr, err := netutil.BuildHTTPURL(currentSB.Network[0].Address, port)
				if err != nil {
					return nil, fmt.Errorf("failed to build HTTP URL: %w", err)
				}

				// Get service concurrency and create strategy/tracker
				svcConcurrency, err := core_v1alpha.GetServiceConcurrency(ver, service)
				if err != nil {
					return nil, fmt.Errorf("failed to get service concurrency: %w", err)
				}
				strategy := concurrency.NewStrategy(&svcConcurrency)
				tracker := strategy.InitializeTracker()

				// Acquire initial lease
				leaseSize := tracker.AcquireLease()

				lsb := &sandbox{
					sandbox:     &currentSB,
					ent:         resp.Entity().Entity(),
					lastRenewal: time.Now(),
					url:         addr,
					tracker:     tracker,
				}

				// Add to versions map
				key := verKey{ver.ID.String(), service}
				a.mu.Lock()
				vs, ok := a.versions[key]
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

				a.log.Info("sandbox ready", "sandbox", currentSB.ID, "url", addr, "service", service)

				return &Lease{
					ver:     ver,
					sandbox: &currentSB,
					ent:     resp.Entity().Entity(),
					pool:    service,
					service: service,
					Size:    leaseSize,
					URL:     addr,
				}, nil
			}

			if currentSB.Status == compute_v1alpha.DEAD {
				return nil, ErrSandboxDiedEarly
			}

			// Still pending, continue waiting
		}
	}
}

// retireUnusedSandboxes removes idle sandboxes based on last activity
func (a *localActivator) retireUnusedSandboxes() {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()

	for key, vs := range a.versions {
		// Get scale-down delay from strategy
		scaleDownDelay := vs.strategy.ScaleDownDelay()

		// Determine if this is a fixed mode service
		targetInstances := vs.strategy.DesiredInstances()
		isFixedMode := targetInstances > 0

		var toRemove []entity.Id

		for _, s := range vs.sandboxes {
			// Don't retire if it's been active recently
			if now.Sub(s.lastRenewal) < scaleDownDelay {
				continue
			}

			// For fixed mode, maintain minimum instance count
			if isFixedMode {
				runningCount := 0
				for _, sb := range vs.sandboxes {
					if sb.sandbox.Status == compute_v1alpha.RUNNING {
						runningCount++
					}
				}
				if runningCount <= targetInstances {
					// Don't scale below target
					continue
				}
			}

			// For auto mode, only retire if no active leases
			if !isFixedMode && s.tracker.Used() > 0 {
				continue
			}

			a.log.Info("retiring idle sandbox",
				"sandbox", s.sandbox.ID,
				"app", vs.ver.App,
				"service", key.service,
				"idle_duration", now.Sub(s.lastRenewal))

			toRemove = append(toRemove, s.sandbox.ID)

			// Mark sandbox as STOPPED in entity store
			go func(sbID entity.Id) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				var rpcE entityserver_v1alpha.Entity
				rpcE.SetId(sbID.String())
				rpcE.SetAttrs(entity.New(
					(&compute_v1alpha.Sandbox{
						Status: compute_v1alpha.STOPPED,
					}).Encode,
				).Attrs())

				if _, err := a.eac.Put(ctx, &rpcE); err != nil {
					a.log.Error("failed to stop sandbox", "sandbox", sbID, "error", err)
				}
			}(s.sandbox.ID)
		}

		// Remove from tracking
		for _, sbID := range toRemove {
			a.removeSandbox(sbID.String())
		}
	}
}

// removeSandbox removes a sandbox from tracking structures
func (a *localActivator) removeSandbox(sandboxID string) {
	for key, vs := range a.versions {
		for i, s := range vs.sandboxes {
			if s.sandbox.ID.String() == sandboxID {
				// Remove from slice
				vs.sandboxes = append(vs.sandboxes[:i], vs.sandboxes[i+1:]...)

				// If no sandboxes remain, clean up the version entry
				if len(vs.sandboxes) == 0 {
					delete(a.versions, key)
				}
				return
			}
		}
	}
}

// InBackground runs periodic background tasks for direct mode
func (a *localActivator) InBackground(ctx context.Context) {
	// Start sandbox watcher
	go a.watchSandboxes(ctx)

	// Background loop for fixed instances and retirement
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.log.Info("activator background loop stopped")
			return
		case <-ticker.C:
			// Ensure fixed instances are maintained
			a.ensureFixedInstances(ctx)

			// Retire unused sandboxes
			a.retireUnusedSandboxes()
		}
	}
}
