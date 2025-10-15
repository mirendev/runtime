package concurrency

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	assert.Equal(t, 0, strategy.DesiredInstances()) // Scale to zero
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
	assert.Equal(t, 3, strategy.DesiredInstances())
}

func TestFixedStrategy_SingleInstance(t *testing.T) {
	svc := &core_v1alpha.ServiceConcurrency{
		Mode:         "fixed",
		NumInstances: 1,
	}

	strategy := NewStrategy(svc)
	assert.Equal(t, 1, strategy.DesiredInstances())
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
	for i := 0; i < 4; i++ {
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
