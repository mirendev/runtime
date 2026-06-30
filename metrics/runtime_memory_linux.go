//go:build linux

package metrics

import (
	"os"
	"strconv"
	"strings"
)

// processResidentBytes returns the resident set size of the *control process*
// itself (the miren binary), read from /proc/self/statm.
//
// Deliberately the process RSS, not the miren.service cgroup's memory.current:
// in cgroup v2 the service total is recursive and includes every app-sandbox
// and addon-DB child cgroup, which would conflate the coordinator's own growth
// with normal app memory. The balloon we are chasing lives in the coordinator
// process (the cgroup hits ~50G while all sandboxes sum to ~2.5G), so the
// process RSS is exactly the signal we want.
func processResidentBytes() (uint64, bool) {
	// /proc/self/statm fields (in pages): size resident shared text lib data dt
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return pages * uint64(os.Getpagesize()), true
}
