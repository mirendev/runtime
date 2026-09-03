package sysstats

// ResourceUsage contains basic host system resource utilization
type ResourceUsage struct {
	CPUCores       float64
	CPUPercent     float64
	MemoryBytes    int64
	MemoryPercent  float64
	StorageBytes   int64
	StoragePercent float64

	// CPUCoreCount is the number of logical CPUs. It is the denominator that
	// turns a workload's absolute CPU use into a share of the host, which is
	// what makes "178% of a core" interpretable on a 4-core box versus a 64-core
	// one.
	CPUCoreCount int

	// MemoryTotalBytes and StorageTotalBytes are the capacities the used
	// figures above are a fraction of. The percentages are already derived from
	// them, but a caller charting usage over time needs the raw ceiling too.
	MemoryTotalBytes  int64
	StorageTotalBytes int64

	// Load1 duplicates CPUCores, which is a load average despite its name and
	// is kept for the callers that already read it. Load5 and Load15 are what
	// distinguish a momentary spike from sustained pressure.
	Load1  float64
	Load5  float64
	Load15 float64

	// CPUBusyJiffies and CPUTotalJiffies are the raw cumulative counters from
	// /proc/stat. They are exposed unconverted because their ratio between two
	// samples is the host's CPU utilization, and taking a ratio cancels the
	// USER_HZ scaling factor that converting either one to seconds would have
	// to assume.
	CPUBusyJiffies  uint64
	CPUTotalJiffies uint64
}
