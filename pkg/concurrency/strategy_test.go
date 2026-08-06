package concurrency

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
)

func TestAutoStrategy_SlotCalculations(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "15m",
	}

	strategy := NewStrategy(svc)

	tracker := strategy.InitializeTracker()
	assert.Equal(t, 10, tracker.Max())
	assert.Equal(t, 0, tracker.Used()) // Starts empty

	leaseSize := strategy.LeaseSize()
	assert.Equal(t, 2, leaseSize) // 20% of 10

	// Has capacity for 5 leases (0+2, 2+2, 4+2, 6+2, 8+2 all <= 10)
	assert.True(t, tracker.HasCapacity())
	tracker.AcquireLease()
	assert.Equal(t, 2, tracker.Used())

	assert.True(t, tracker.HasCapacity())
	tracker.AcquireLease()
	assert.Equal(t, 4, tracker.Used())

	assert.True(t, tracker.HasCapacity())
	tracker.AcquireLease()
	assert.Equal(t, 6, tracker.Used())

	assert.True(t, tracker.HasCapacity())
	tracker.AcquireLease()
	assert.Equal(t, 8, tracker.Used())

	assert.True(t, tracker.HasCapacity())
	tracker.AcquireLease()
	assert.Equal(t, 10, tracker.Used())

	assert.False(t, tracker.HasCapacity()) // 10+2 > 10

	assert.Equal(t, 15*time.Minute, strategy.ScaleDownDelay())
	assert.Equal(t, 0, strategy.MinInstances()) // Scale to zero
}

func TestAutoStrategy_MinimumLeaseSize(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 1, // 20% would be < 1
	}

	strategy := NewStrategy(svc)
	assert.Equal(t, 1, strategy.LeaseSize()) // Minimum is 1
}

func TestAutoStrategy_Defaults(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode: "auto",
		// No requests_per_instance or scale_down_delay
	}

	strategy := NewStrategy(svc)

	tracker := strategy.InitializeTracker()
	assert.Equal(t, 10, tracker.Max()) // Default requests_per_instance

	assert.Equal(t, 2*time.Minute, strategy.ScaleDownDelay()) // Default
}

func TestAutoStrategy_CustomScaleDownDelay(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:           "auto",
		ScaleDownDelay: "5m",
	}

	strategy := NewStrategy(svc)
	assert.Equal(t, 5*time.Minute, strategy.ScaleDownDelay())
}

func TestAutoStrategy_InvalidScaleDownDelay(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:           "auto",
		ScaleDownDelay: "invalid",
	}

	strategy := NewStrategy(svc)
	// Should fall back to default
	assert.Equal(t, 2*time.Minute, strategy.ScaleDownDelay())
}

func TestFixedStrategy_NoSlotTracking(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:         "fixed",
		NumInstances: 3,
	}

	strategy := NewStrategy(svc)

	tracker := strategy.InitializeTracker()
	assert.Equal(t, 1, tracker.Max())
	assert.Equal(t, 0, tracker.Used()) // Starts empty

	assert.Equal(t, 1, strategy.LeaseSize())

	// Fixed mode always has capacity (for round-robin)
	assert.True(t, tracker.HasCapacity())
	tracker.AcquireLease()
	assert.Equal(t, 1, tracker.Used())

	// Still has capacity after acquiring lease
	assert.True(t, tracker.HasCapacity())

	// ReleaseLease is a no-op for fixed mode
	tracker.ReleaseLease(1)
	assert.Equal(t, 1, tracker.Used()) // Still 1, release is no-op

	assert.Equal(t, time.Duration(0), strategy.ScaleDownDelay())
	assert.Equal(t, 3, strategy.MinInstances())
}

func TestFixedStrategy_SingleInstance(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:         "fixed",
		NumInstances: 1,
	}

	strategy := NewStrategy(svc)
	assert.Equal(t, 1, strategy.MinInstances())
}

func TestNewStrategy_ModeSelection(t *testing.T) {
	autoSvc := &core_v1alpha.ServiceConcurrency{
		Mode: "auto",
	}
	autoStrategy := NewStrategy(autoSvc)
	_, ok := autoStrategy.(*AutoStrategy)
	assert.True(t, ok, "Should create AutoStrategy for auto mode")

	fixedSvc := &core_v1alpha.ServiceConcurrency{
		Mode:         "fixed",
		NumInstances: 2,
	}
	fixedStrategy := NewStrategy(fixedSvc)
	_, ok = fixedStrategy.(*FixedStrategy)
	assert.True(t, ok, "Should create FixedStrategy for fixed mode")
}

func TestAutoStrategy_LargeRequestsPerInstance(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 100,
	}

	strategy := NewStrategy(svc)
	leaseSize := strategy.LeaseSize()
	assert.Equal(t, 20, leaseSize) // 20% of 100

	tracker := strategy.InitializeTracker()
	assert.Equal(t, 100, tracker.Max())
	assert.Equal(t, 0, tracker.Used()) // Starts empty
}

func TestAutoStrategy_CapacityBoundary(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
	}

	strategy := NewStrategy(svc)
	tracker := strategy.InitializeTracker()

	// Lease size is 2 (20% of 10)
	// Max is 10
	// Acquire 4 leases to get to 8 used
	for range 4 {
		tracker.AcquireLease()
	}
	assert.Equal(t, 8, tracker.Used())

	// At 8 used, we can fit one more lease (8+2 = 10)
	assert.True(t, tracker.HasCapacity())
	tracker.AcquireLease()
	assert.Equal(t, 10, tracker.Used())

	// At 10 used (full), definitely no capacity (10+2 > 10)
	assert.False(t, tracker.HasCapacity())
}

func TestNewStrategyForVersion_EphemeralWebRoutesToEphemeralStrategy(t *testing.T) {
	// The web service of an ephemeral version must produce an EphemeralStrategy
	// regardless of the configured ServiceConcurrency. The strategy caps the
	// pool at a single sandbox and scales it to zero when idle.
	ver := &core_v1alpha.AppVersion{
		Version:        "v1",
		EphemeralLabel: "feat-x",
	}
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 50,
		ScaleDownDelay:      "30m",
	}

	strategy := NewStrategyForVersion(ver, "web", svc)

	eph, ok := strategy.(*EphemeralStrategy)
	require.True(t, ok, "ephemeral web service must produce an EphemeralStrategy")
	assert.Equal(t, 1, eph.MaxInstances(), "ephemeral pool is capped at 1 sandbox")
	assert.Equal(t, 0, eph.MinInstances(), "ephemeral pool scales to zero when idle")
	assert.Equal(t, 30*time.Minute, eph.ScaleDownDelay(), "should inherit configured ScaleDownDelay")
}

func TestNewStrategyForVersion_EphemeralUsesDefaultsWhenSvcUnset(t *testing.T) {
	ver := &core_v1alpha.AppVersion{
		Version:        "v1",
		EphemeralLabel: "feat-x",
	}

	strategy := NewStrategyForVersion(ver, "web", nil)

	eph, ok := strategy.(*EphemeralStrategy)
	require.True(t, ok)
	assert.Equal(t, 15*time.Minute, eph.ScaleDownDelay(),
		"default ScaleDownDelay is generous enough that browsing a preview doesn't churn cold starts")
}

func TestNewStrategyForVersion_EphemeralNonWebHonorsConfig(t *testing.T) {
	// Supporting services on an ephemeral preview (databases, workers, etc.)
	// are not the URL-routed service and must honor their configured
	// ServiceConcurrency like a normal deploy. A fixed-mode db service with
	// NumInstances=3 should produce a FixedStrategy{3}, NOT EphemeralStrategy.
	ver := &core_v1alpha.AppVersion{
		Version:        "v1",
		EphemeralLabel: "feat-x",
	}
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:         "fixed",
		NumInstances: 3,
	}

	strategy := NewStrategyForVersion(ver, "db", svc)

	fixed, ok := strategy.(*FixedStrategy)
	require.True(t, ok, "non-web service on ephemeral version must honor configured strategy")
	assert.Equal(t, 3, fixed.MinInstances(),
		"fixed-mode db service must keep its configured NumInstances even on ephemeral preview")
	assert.Equal(t, 3, fixed.MaxInstances())
}

func TestMaxInstances_StrategyDefaults(t *testing.T) {
	auto := NewStrategy(&core_v1alpha.ServiceConcurrency{Mode: "auto"})
	assert.Equal(t, MaxPoolSize, auto.MaxInstances(),
		"auto-mode strategy returns the global cap")

	fixed := NewStrategy(&core_v1alpha.ServiceConcurrency{Mode: "fixed", NumInstances: 4})
	assert.Equal(t, 4, fixed.MaxInstances(),
		"fixed-mode strategy caps at its configured count")
}

func TestEphemeralStrategy_AlwaysHasCapacity(t *testing.T) {
	// There's no scaling alternative for an overloaded preview sandbox
	// (MaxInstances=1), so capacity is not tracked; all traffic routes at
	// the single sandbox. Mirrors FixedStrategy's capacity model.
	eph := &EphemeralStrategy{scaleDownDelay: 5 * time.Minute}
	tracker := eph.InitializeTracker()

	for i := range 50 {
		assert.True(t, tracker.HasCapacity(), "iteration %d should still have capacity", i)
		tracker.AcquireLease()
	}
}

func TestNewStrategyForVersion_NonEphemeralDelegates(t *testing.T) {
	ver := &core_v1alpha.AppVersion{
		Version: "v1",
	}
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:                "auto",
		RequestsPerInstance: 10,
		ScaleDownDelay:      "15m",
	}

	strategy := NewStrategyForVersion(ver, "web", svc)

	_, ok := strategy.(*AutoStrategy)
	assert.True(t, ok, "non-ephemeral auto version must produce an AutoStrategy")
}

func TestNewStrategyForVersion_NilVersionDelegates(t *testing.T) {
	// Defensive: callers without an AppVersion handy fall through to NewStrategy.
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:         "fixed",
		NumInstances: 3,
	}

	strategy := NewStrategyForVersion(nil, "web", svc)

	fixed, ok := strategy.(*FixedStrategy)
	require.True(t, ok, "nil version with fixed config must delegate to FixedStrategy")
	assert.Equal(t, 3, fixed.MinInstances())
}
