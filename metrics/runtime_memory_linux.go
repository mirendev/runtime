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
// Deliberately the process RSS, not the miren.service cgroup's memory.current.
// The service cgroup holds the coordinator, the containerd daemon, and one shim
// per container, so its total mixes the coordinator's own growth with shim churn
// that tracks how many sandboxes happen to be running.
//
// It does NOT include the containers themselves. Sandboxes are created at
// /miren/sandbox-<id> and every other containerd workload at /<namespace>/<id>;
// both are absolute paths, which runc resolves against the cgroup root, so
// /miren is a top-level cgroup sibling to system.slice rather than a child of
// the service. Apps, addon databases, etcd, buildkit and the Victoria services
// all live out there.
//
// The balloon we are chasing lives in the coordinator process itself — it hit
// ~50G while all sandboxes together were ~2.5G — so the process RSS is exactly
// the signal we want.
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
