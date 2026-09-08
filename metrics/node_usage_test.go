package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/sysstats"
)

func TestNodeUsageCPUCoresUsed(t *testing.T) {
	sample := func(busy, total uint64) sysstats.ResourceUsage {
		return sysstats.ResourceUsage{
			CPUCoreCount:    4,
			CPUBusyJiffies:  busy,
			CPUTotalJiffies: total,
		}
	}

	r := require.New(t)
	n := &NodeUsage{}

	// The first sample has nothing to diff against. Reporting zero here would
	// show every freshly started host as idle.
	_, ok := n.cpuCoresUsed(sample(1000, 4000))
	r.False(ok, "the first sample cannot produce a rate")

	// 400 of the next 800 jiffies were busy: half of a 4-core host is 2 cores.
	cores, ok := n.cpuCoresUsed(sample(1400, 4800))
	r.True(ok)
	r.InDelta(2.0, cores, 0.001)

	// A fully saturated interval reports the whole machine, not 100%.
	cores, ok = n.cpuCoresUsed(sample(2200, 5600))
	r.True(ok)
	r.InDelta(4.0, cores, 0.001)
}

func TestNodeUsageCPUCoresUsedSkipsImpossibleDeltas(t *testing.T) {
	r := require.New(t)

	// Counters that went backwards mean the host rebooted between samples. The
	// delta spans two different boots and describes nothing real.
	n := &NodeUsage{}
	_, _ = n.cpuCoresUsed(sysstats.ResourceUsage{CPUCoreCount: 2, CPUBusyJiffies: 5000, CPUTotalJiffies: 9000})
	_, ok := n.cpuCoresUsed(sysstats.ResourceUsage{CPUCoreCount: 2, CPUBusyJiffies: 10, CPUTotalJiffies: 40})
	r.False(ok, "a counter reset is not a usage measurement")

	// Two samples taken within the same tick leave no elapsed time to divide by.
	n = &NodeUsage{}
	_, _ = n.cpuCoresUsed(sysstats.ResourceUsage{CPUCoreCount: 2, CPUBusyJiffies: 100, CPUTotalJiffies: 400})
	_, ok = n.cpuCoresUsed(sysstats.ResourceUsage{CPUCoreCount: 2, CPUBusyJiffies: 100, CPUTotalJiffies: 400})
	r.False(ok, "no elapsed jiffies means no measurable utilization")

	// An unreadable /proc/stat leaves the counters at zero, which must not be
	// mistaken for a perfectly idle host.
	n = &NodeUsage{}
	_, ok = n.cpuCoresUsed(sysstats.ResourceUsage{CPUCoreCount: 2})
	r.False(ok, "missing counters are unknown, not idle")
}
