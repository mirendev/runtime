//go:build linux

package sysstats

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// CollectSystemStats gathers basic host system resource usage metrics
// similar to uptime, free -m, and df -h
func CollectSystemStats(dataPath string) ResourceUsage {
	usage := ResourceUsage{}

	// Get CPU load average (1 minute average) from /proc/loadavg
	loadavgBytes, err := os.ReadFile("/proc/loadavg")
	if err == nil {
		fields := strings.Fields(string(loadavgBytes))
		if len(fields) > 0 {
			if load1, err := strconv.ParseFloat(fields[0], 64); err == nil {
				usage.CPUCores = load1

				// Calculate percentage based on number of CPUs
				// Try to get CPU count from /proc/cpuinfo
				numCPU := getCPUCount()
				if numCPU > 0 {
					usage.CPUPercent = (load1 / float64(numCPU)) * 100
				}
			}
		}
	}

	// Get memory stats from /proc/meminfo
	meminfoBytes, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		var memTotal, memAvailable int64
		lines := strings.Split(string(meminfoBytes), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			value, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				continue
			}
			// /proc/meminfo reports in kB, convert to bytes
			valueBytes := value * 1024

			if strings.HasPrefix(line, "MemTotal:") {
				memTotal = valueBytes
			} else if strings.HasPrefix(line, "MemAvailable:") {
				memAvailable = valueBytes
			}
		}

		if memTotal > 0 {
			memUsed := memTotal - memAvailable
			usage.MemoryBytes = memUsed
			usage.MemoryPercent = float64(memUsed) / float64(memTotal) * 100
		}
	}

	// Get disk usage for the specified path using syscall.Statfs
	if dataPath != "" {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(dataPath, &stat); err == nil {
			totalBytes := stat.Blocks * uint64(stat.Bsize)
			availBytes := stat.Bavail * uint64(stat.Bsize)
			usedBytes := totalBytes - availBytes

			usage.StorageBytes = int64(usedBytes)
			if totalBytes > 0 {
				usage.StoragePercent = float64(usedBytes) / float64(totalBytes) * 100
			}
		}
	}

	return usage
}

// getCPUCount returns the number of CPUs from /proc/cpuinfo
func getCPUCount() int {
	cpuinfoBytes, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}

	count := 0
	lines := strings.Split(string(cpuinfoBytes), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "processor") {
			count++
		}
	}
	return count
}
