package lease

import (
	"math"
	"sync"
)

const DefaultAlpha = 0.2

// LatencyTracker maintains an exponential weighted average latency per RIF.
type LatencyTracker struct {
	mu    sync.Mutex
	ewas  map[int32]float64
	alpha float64
}

// NewLatencyTracker creates a new latency tracker with a given alpha for the EWA.
func NewLatencyTracker(alpha float64) *LatencyTracker {
	if alpha == 0 {
		alpha = DefaultAlpha
	}

	return &LatencyTracker{
		ewas:  make(map[int32]float64),
		alpha: alpha,
	}
}

// RecordLatency updates the EWA for the given RIF with the new latency measurement.
func (lt *LatencyTracker) RecordLatency(rif int32, latency float64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	ewa, exists := lt.ewas[rif]
	if !exists {
		lt.ewas[rif] = latency
		return
	}

	// EWA update: ewa = alpha * newValue + (1 - alpha) * oldValue
	lt.ewas[rif] = lt.alpha*latency + (1.0-lt.alpha)*ewa
}

// GetLatencyEstimate returns the current EWA for the given RIF.
// If no samples exist, it returns NaN.
func (lt *LatencyTracker) GetLatencyEstimate(rif int32) float64 {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	ewa, exists := lt.ewas[rif]
	if !exists {
		return math.NaN()
	}
	return ewa
}
