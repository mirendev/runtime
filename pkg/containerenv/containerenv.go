// Package containerenv detects whether the current process is running inside a
// container (Docker, Podman, or a Kubernetes pod). The miren server uses this to
// advertise to miren.cloud that it's containerized, since a containerized server
// is effectively never reachable directly from the internet and must keep Miren
// Anywhere (POP routing) on.
package containerenv

import (
	"os"
	"slices"
	"strings"
	"sync"
)

// dockerEnvPath and podmanEnvPath are the marker files each runtime drops into
// the container filesystem. Their presence is a definitive signal.
var (
	dockerEnvPath = "/.dockerenv"
	podmanEnvPath = "/run/.containerenv"
)

// cgroupPaths are read as a fallback when the marker files are absent. Container
// runtimes leave recognizable tokens in the cgroup hierarchy of PID 1 and the
// current process.
var cgroupPaths = []string{"/proc/1/cgroup", "/proc/self/cgroup"}

// cgroupMarkers are substrings that indicate a containerized cgroup hierarchy.
var cgroupMarkers = []string{"docker", "containerd", "podman", "kubepods", "libpod"}

var (
	once     sync.Once
	inCached bool
)

// InContainer reports whether the process is running inside a container. The
// result is computed once and cached — it can't change over the process
// lifetime.
func InContainer() bool {
	once.Do(func() {
		inCached = detect()
	})
	return inCached
}

// detect performs the actual probing. It's separate from InContainer so tests
// can exercise it directly without the sync.Once cache.
func detect() bool {
	if fileExists(dockerEnvPath) || fileExists(podmanEnvPath) {
		return true
	}
	return slices.ContainsFunc(cgroupPaths, cgroupIndicatesContainer)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func cgroupIndicatesContainer(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	contents := string(data)
	for _, marker := range cgroupMarkers {
		if strings.Contains(contents, marker) {
			return true
		}
	}
	return false
}
