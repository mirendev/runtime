package concurrency

import (
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
)

// MaxPoolSize is the default upper bound on sandboxes per pool, applied by
// strategies that have no inherent cap of their own (notably AutoStrategy).
// It exists to prevent runaway growth from buggy scaling logic.
const MaxPoolSize = 20

// ConcurrencyTracker manages capacity state for a single sandbox
type ConcurrencyTracker struct {
	maxCapacity int
	used        int
	strategy    ConcurrencyStrategy
}

func (t *ConcurrencyTracker) HasCapacity() bool {
	return t.strategy.checkCapacity(t.used, t.maxCapacity)
}

// AcquireLease allocates capacity and returns the lease size.
// Caller must check HasCapacity() before calling this method.
func (t *ConcurrencyTracker) AcquireLease() int {
	size := t.strategy.LeaseSize()
	t.used += size
	return size
}

// AcquireOverflow allocates one slot past capacity for a request admitted
// over target. Such a lease carries only that request, so it isn't batched.
func (t *ConcurrencyTracker) AcquireOverflow() int {
	t.used++
	return 1
}

func (t *ConcurrencyTracker) ReleaseLease(size int) {
	t.strategy.releaseCapacity(t, size)
}

func (t *ConcurrencyTracker) Used() int {
	return t.used
}

func (t *ConcurrencyTracker) Max() int {
	return t.maxCapacity
}

// ConcurrencyStrategy encapsulates mode-specific capacity management logic.
// Implementations of this interface are package-internal only - the lowercase
// methods (checkCapacity, releaseCapacity) enforce implementation locality.
type ConcurrencyStrategy interface {
	// InitializeTracker creates a new tracker for a sandbox
	InitializeTracker() *ConcurrencyTracker

	// LeaseSize returns how much capacity to allocate per lease (for two-tier leasing)
	LeaseSize() int

	// checkCapacity checks if sandbox can accept another lease (package-internal)
	checkCapacity(used, maxCapacity int) bool

	// releaseCapacity frees capacity (package-internal, allows mode-specific behavior)
	releaseCapacity(tracker *ConcurrencyTracker, size int)

	// ScaleDownDelay returns how long to wait before retiring idle sandbox
	ScaleDownDelay() time.Duration

	// MinInstances returns the strategy-enforced floor on Pool.DesiredInstances:
	// the sandboxpool manager will not scale the pool below this count.
	// 0 allows the pool to scale to zero when idle.
	MinInstances() int

	// MaxInstances returns the maximum sandbox count the pool may scale to.
	// Strategies without an inherent cap return MaxPoolSize.
	MaxInstances() int
}

// AutoStrategy implements auto-scaling with slot-based capacity
type AutoStrategy struct {
	requestsPerInstance int
	scaleDownDelay      time.Duration
}

func (s *AutoStrategy) InitializeTracker() *ConcurrencyTracker {
	return &ConcurrencyTracker{
		maxCapacity: s.requestsPerInstance,
		used:        0, // Start empty; first lease will increment
		strategy:    s,
	}
}

func (s *AutoStrategy) LeaseSize() int {
	// 20% batching for HTTPIngress efficiency
	size := s.requestsPerInstance * 20 / 100
	if size < 1 {
		return 1
	}
	return size
}

func (s *AutoStrategy) checkCapacity(used, maxCapacity int) bool {
	return used+s.LeaseSize() <= maxCapacity
}

func (s *AutoStrategy) releaseCapacity(tracker *ConcurrencyTracker, size int) {
	tracker.used -= size
}

func (s *AutoStrategy) ScaleDownDelay() time.Duration {
	return s.scaleDownDelay
}

func (s *AutoStrategy) MinInstances() int {
	return 0 // Scale to zero when idle
}

func (s *AutoStrategy) MaxInstances() int {
	return MaxPoolSize
}

// FixedStrategy implements fixed instance count (no slot-based capacity)
type FixedStrategy struct {
	numInstances int
}

func (s *FixedStrategy) InitializeTracker() *ConcurrencyTracker {
	return &ConcurrencyTracker{
		maxCapacity: 1,
		used:        0, // Start empty; first lease will increment to 1
		strategy:    s,
	}
}

func (s *FixedStrategy) LeaseSize() int {
	return 1 // Used to mark sandbox as active; capacity checks disabled in fixed mode
}

func (s *FixedStrategy) checkCapacity(used, maxCapacity int) bool {
	return true // Always accept for round-robin
}

func (s *FixedStrategy) releaseCapacity(tracker *ConcurrencyTracker, size int) {
	// Fixed mode doesn't track capacity - no-op
}

func (s *FixedStrategy) ScaleDownDelay() time.Duration {
	return 0 // Never scale down fixed instances
}

func (s *FixedStrategy) MinInstances() int {
	return s.numInstances
}

func (s *FixedStrategy) MaxInstances() int {
	return s.numInstances // Fixed mode never grows past its configured count.
}

// EphemeralStrategy implements scale-to-zero capped at 1 instance, for
// ephemeral preview deploys. Capacity tracking matches FixedStrategy
// (always accepts leases) since there is no scaling alternative for an
// overloaded preview sandbox: just route everything at the one instance.
// When the sole sandbox sits idle past ScaleDownDelay, the pool scales to
// zero and the next request cold-starts a fresh one.
type EphemeralStrategy struct {
	scaleDownDelay time.Duration
}

func (s *EphemeralStrategy) InitializeTracker() *ConcurrencyTracker {
	return &ConcurrencyTracker{
		maxCapacity: 1,
		used:        0,
		strategy:    s,
	}
}

func (s *EphemeralStrategy) LeaseSize() int {
	return 1 // Used to mark sandbox as active; capacity checks always pass.
}

func (s *EphemeralStrategy) checkCapacity(used, maxCapacity int) bool {
	return true // Route all traffic at the single sandbox.
}

func (s *EphemeralStrategy) releaseCapacity(tracker *ConcurrencyTracker, size int) {
	// No-op: capacity is not tracked.
}

func (s *EphemeralStrategy) ScaleDownDelay() time.Duration {
	return s.scaleDownDelay
}

func (s *EphemeralStrategy) MinInstances() int {
	return 0 // Scale to zero when idle so unvisited previews cost nothing.
}

func (s *EphemeralStrategy) MaxInstances() int {
	return 1 // Pool may never grow beyond a single sandbox.
}

// NewStrategy creates a strategy from ServiceConcurrency config
func NewStrategy(svc *core_v1alpha.ServiceConcurrency) ConcurrencyStrategy {
	if svc == nil {
		// Defensive: return safe auto mode defaults if nil
		return &AutoStrategy{
			requestsPerInstance: 10,
			scaleDownDelay:      2 * time.Minute,
		}
	}

	if svc.Mode == "fixed" {
		return &FixedStrategy{
			numInstances: int(svc.NumInstances),
		}
	}

	// Auto mode (default)
	requestsPerInstance := int(svc.RequestsPerInstance)
	if requestsPerInstance <= 0 {
		requestsPerInstance = 10 // Default
	}

	scaleDownDelay := 2 * time.Minute // Default
	if svc.ScaleDownDelay != "" {
		if duration, err := time.ParseDuration(svc.ScaleDownDelay); err == nil {
			scaleDownDelay = duration
		}
	}

	return &AutoStrategy{
		requestsPerInstance: requestsPerInstance,
		scaleDownDelay:      scaleDownDelay,
	}
}

// NewStrategyForVersion returns the concurrency strategy for a request
// targeting (ver, service). Ephemeral routing semantics apply only to the
// user-facing "web" service: that service gets EphemeralStrategy (capped at
// a single sandbox, scale-to-zero on idle), while supporting services on the
// same ephemeral version (databases, workers, etc.) honor their configured
// ServiceConcurrency like a normal deploy.
func NewStrategyForVersion(ver *core_v1alpha.AppVersion, service string, svc *core_v1alpha.ServiceConcurrency) ConcurrencyStrategy {
	if ver != nil && ver.EphemeralLabel != "" && service == "web" {
		// Default to a generous 15-minute idle window so a browser tab left
		// open doesn't churn through cold starts. Configured ScaleDownDelay
		// on the service still wins if set.
		scaleDownDelay := 15 * time.Minute
		if svc != nil && svc.ScaleDownDelay != "" {
			if d, err := time.ParseDuration(svc.ScaleDownDelay); err == nil {
				scaleDownDelay = d
			}
		}
		return &EphemeralStrategy{scaleDownDelay: scaleDownDelay}
	}
	return NewStrategy(svc)
}
