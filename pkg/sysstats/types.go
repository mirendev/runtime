package sysstats

// ResourceUsage contains basic host system resource utilization
type ResourceUsage struct {
	CPUCores       float64
	CPUPercent     float64
	MemoryBytes    int64
	MemoryPercent  float64
	StorageBytes   int64
	StoragePercent float64
}
