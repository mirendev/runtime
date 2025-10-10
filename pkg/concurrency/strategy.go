package concurrency

import (
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
)

// ConcurrencyTracker manages capacity state for a single sandbox
type ConcurrencyTracker struct {
	maxCapacity int
	used        int
	strategy    ConcurrencyStrategy
}

// HasCapacity checks if sandbox can accept another lease
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

// ReleaseLease frees capacity
func (t *ConcurrencyTracker) ReleaseLease(size int) {
	t.strategy.releaseCapacity(t, size)
}

// Used returns current capacity usage
func (t *ConcurrencyTracker) Used() int {
	return t.used
}

// Max returns maximum capacity
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

	// DesiredInstances returns how many instances should always run (0 = scale to zero)
	DesiredInstances() int
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

func (s *AutoStrategy) DesiredInstances() int {
	return 0 // Scale to zero when idle
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
	return 1 // Always "full" after first lease
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

func (s *FixedStrategy) DesiredInstances() int {
	return s.numInstances
}

// NewStrategy creates a strategy from ServiceConcurrency config
func NewStrategy(svc *core_v1alpha.ServiceConcurrency) ConcurrencyStrategy {
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
