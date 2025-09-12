//go:build darwin
// +build darwin

package upgrade

// CgroupManager handles cgroup operations (stub for Darwin)
type CgroupManager struct{}

// NewCgroupManager creates a new cgroup manager (returns no-op stub on Darwin)
func NewCgroupManager() (*CgroupManager, error) {
	// No cgroups on Darwin; return a no-op stub
	return &CgroupManager{}, nil
}

// AdoptProcess is a no-op on Darwin
func (m *CgroupManager) AdoptProcess(pid int) error {
	return nil
}

// AdoptChildProcesses is a no-op on Darwin
func (m *CgroupManager) AdoptChildProcesses(parentPID int) error {
	return nil
}

// FindContainerdPID is a no-op on Darwin
func FindContainerdPID(socketPath string) (int, error) {
	return 0, nil
}
