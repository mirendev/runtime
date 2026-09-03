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

	usage.CPUCoreCount = getCPUCount()
	usage.CPUBusyJiffies, usage.CPUTotalJiffies = readCPUJiffies()

	// Get CPU load average (1, 5 and 15 minute averages) from /proc/loadavg
	loadavgBytes, err := os.ReadFile("/proc/loadavg")
	if err == nil {
		fields := strings.Fields(string(loadavgBytes))
		if len(fields) > 0 {
			if load1, err := strconv.ParseFloat(fields[0], 64); err == nil {
				usage.CPUCores = load1
				usage.Load1 = load1

				if usage.CPUCoreCount > 0 {
					usage.CPUPercent = (load1 / float64(usage.CPUCoreCount)) * 100
				}
			}
		}
		if len(fields) > 1 {
			usage.Load5, _ = strconv.ParseFloat(fields[1], 64)
		}
		if len(fields) > 2 {
			usage.Load15, _ = strconv.ParseFloat(fields[2], 64)
		}
	}

	// Get memory stats from /proc/meminfo
	meminfoBytes, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		var memTotal, memAvailable int64
		lines := strings.SplitSeq(string(meminfoBytes), "\n")
		for line := range lines {
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
			usage.MemoryTotalBytes = memTotal
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
			usage.StorageTotalBytes = int64(totalBytes)
			if totalBytes > 0 {
				usage.StoragePercent = float64(usedBytes) / float64(totalBytes) * 100
			}
		}
	}

	return usage
}

// readCPUJiffies returns the host's cumulative busy and total CPU time from the
// aggregate "cpu" line of /proc/stat.
//
// Busy excludes idle and iowait: a core waiting on disk is not a core doing
// work, and counting it as busy would make every I/O-bound host look pegged.
// The units are USER_HZ ticks, which this deliberately does not convert --
// callers take the ratio of two samples, and the ratio is unitless.
//
// Both values are zero when /proc/stat is unreadable or malformed, which the
// caller must treat as "unknown" rather than "idle".
func readCPUJiffies() (busy, total uint64) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		// The first line is the all-CPU aggregate, named exactly "cpu"; the
		// per-core lines that follow are "cpu0", "cpu1" and so on.
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}

		// Columns are: user nice system idle iowait irq softirq steal, then
		// guest and guest_nice. The trailing two are stopped at deliberately:
		// the kernel already includes guest time inside user and guest_nice
		// inside nice, so adding them would count a virtualization host's guest
		// time twice.
		values := fields[1:]
		if len(values) > 8 {
			values = values[:8]
		}

		for i, f := range values {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return 0, 0
			}
			total += v
			if i != 3 && i != 4 {
				busy += v
			}
		}

		return busy, total
	}

	return 0, 0
}

// getCPUCount returns the number of CPUs from /proc/cpuinfo
func getCPUCount() int {
	cpuinfoBytes, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}

	count := 0
	lines := strings.SplitSeq(string(cpuinfoBytes), "\n")
	for line := range lines {
		if strings.HasPrefix(line, "processor") {
			count++
		}
	}
	return count
}
