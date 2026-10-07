package clusterresources

// This file is the runtime half of the cluster-resources wire contract. Its
// cloud counterpart is mirendev/cloud/services/clusterresources/wire.go.
// Keep the JSON shapes in lockstep until the uplink has a shared schema home.

import "time"

const (
	Version1 uint = 1

	TypeSample = "cluster.resources.sample"
)

// Config is how often cloud wants a sample once the session opens.
type Config struct {
	IntervalSeconds int `json:"interval_seconds"`
}

// Sample is one reading of what this host is spending. It is the resource
// half of the legacy status report, field for field. Percentages are
// whole-host utilization; the byte and core totals are capacity.
type Sample struct {
	ObservedAt     time.Time `json:"observed_at"`
	CPUCores       float64   `json:"cpu_cores,omitempty"`
	CPUPercent     float64   `json:"cpu_percent"`
	MemoryBytes    int64     `json:"memory_bytes,omitempty"`
	MemoryPercent  float64   `json:"memory_percent"`
	StorageBytes   int64     `json:"storage_bytes,omitempty"`
	StoragePercent float64   `json:"storage_percent"`
}
