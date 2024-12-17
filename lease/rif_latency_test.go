package lease

import (
	"math"
	"testing"
)

// TestLatencyTracker tests the LatencyTracker using an exponential weighted average.
func TestLatencyTracker(t *testing.T) {
	// Create a tracker with alpha=0.5 for demonstration (equal weight to recent and historical)
	tracker := NewLatencyTracker(0.5)

	// Suppose we record latencies for RIF=5: 100, 120, 90
	tracker.RecordLatency(5, 100.0) // ewa=100.0 (initial)
	tracker.RecordLatency(5, 120.0) // ewa=0.5*120 + 0.5*100 = 110.0
	tracker.RecordLatency(5, 90.0)  // ewa=0.5*90 + 0.5*110 = 100.0

	ewa5 := tracker.GetLatencyEstimate(5)
	t.Logf("EWA latency for RIF=5: %.2f ms", ewa5)
	// After three updates: we got back to 100.0 as the EWA.
	// With EWA, we don't have a "right" or "wrong" exact answer since it's a smoothing,
	// but we know what it computed. Let's just check if it's approximately correct.
	if math.Abs(ewa5-100.0) > 0.001 {
		t.Errorf("Expected ewa ≈100.0 for RIF=5, got %.2f", ewa5)
	}

	// For RIF=10: 200, 210, 190, 205
	tracker.RecordLatency(10, 200.0) // ewa=200.0 (initial)
	tracker.RecordLatency(10, 210.0) // ewa=0.5*210+0.5*200=205.0
	tracker.RecordLatency(10, 190.0) // ewa=0.5*190+0.5*205=197.5
	tracker.RecordLatency(10, 205.0) // ewa=0.5*205+0.5*197.5=201.25

	ewa10 := tracker.GetLatencyEstimate(10)
	t.Logf("EWA latency for RIF=10: %.2f ms", ewa10)
	// After these steps, we ended with 201.25 again, coincidentally the same as simple avg.
	if math.Abs(ewa10-201.25) > 0.001 {
		t.Errorf("Expected ewa ≈201.25 for RIF=10, got %.2f", ewa10)
	}

	// Check a RIF with no data
	ewaNoData := tracker.GetLatencyEstimate(99)
	t.Logf("EWA latency for RIF=99: %f (NaN expected)", ewaNoData)
	if !math.IsNaN(ewaNoData) {
		t.Errorf("Expected NaN for RIF=99, got %.2f", ewaNoData)
	}
}
