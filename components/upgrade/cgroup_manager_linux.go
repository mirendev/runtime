//go:build linux
// +build linux

package upgrade

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CgroupManager handles cgroup operations for process adoption
type CgroupManager struct {
	serviceCgroup string
}

// NewCgroupManager creates a new cgroup manager
func NewCgroupManager() (*CgroupManager, error) {
	// Detect our current cgroup
	cgroup, err := getCurrentServiceCgroup()
	if err != nil {
		return nil, fmt.Errorf("failed to detect service cgroup: %w", err)
	}

	if cgroup == "" {
		return nil, fmt.Errorf("not running in a systemd service cgroup")
	}

	return &CgroupManager{
		serviceCgroup: cgroup,
	}, nil
}

// AdoptProcess moves a process (and its children) into our service cgroup
func (m *CgroupManager) AdoptProcess(pid int) error {
	// For cgroup v2 (unified hierarchy)
	cgroupV2Path := filepath.Join("/sys/fs/cgroup", m.serviceCgroup, "cgroup.procs")
	if err := m.writeToFile(cgroupV2Path, strconv.Itoa(pid)); err == nil {
		return nil // Success with cgroup v2
	}

	// Fallback to cgroup v1 (legacy hierarchy)
	// Try common controller paths
	controllers := []string{"systemd", "cpu", "memory", "pids"}
	var lastErr error
	successCount := 0

	for _, controller := range controllers {
		cgroupV1Path := filepath.Join("/sys/fs/cgroup", controller, m.serviceCgroup, "cgroup.procs")
		if err := m.writeToFile(cgroupV1Path, strconv.Itoa(pid)); err == nil {
			successCount++
		} else {
			lastErr = err
		}
	}

	if successCount > 0 {
		return nil // At least some controllers succeeded
	}

	return fmt.Errorf("failed to adopt process %d: %w", pid, lastErr)
}

// AdoptChildProcesses finds and adopts all child processes of the given PID
func (m *CgroupManager) AdoptChildProcesses(parentPID int) error {
	children, err := findChildProcesses(parentPID)
	if err != nil {
		return fmt.Errorf("failed to find child processes: %w", err)
	}

	for _, childPID := range children {
		if err := m.AdoptProcess(childPID); err != nil {
			// Log but don't fail - some processes might have exited
			fmt.Fprintf(os.Stderr, "warning: failed to adopt child process %d: %v\n", childPID, err)
		}
	}

	// Also adopt the parent itself
	return m.AdoptProcess(parentPID)
}

// writeToFile writes content to a file (helper for cgroup operations)
func (m *CgroupManager) writeToFile(path, content string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()

	// Add newline for broader kernel compatibility
	if !strings.HasSuffix(content, "\n") {
		content = content + "\n"
	}
	_, err = file.WriteString(content)
	return err
}

// getCurrentServiceCgroup detects the current process's systemd service cgroup
func getCurrentServiceCgroup() (string, error) {
	// Read /proc/self/cgroup
	file, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Format: ID:controllers:path
		// Example: 0::/system.slice/miren.service
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}

		cgroupPath := parts[2]

		// Look for systemd service pattern
		if strings.Contains(cgroupPath, ".service") {
			// For cgroup v2, the path is the full path
			if parts[0] == "0" && parts[1] == "" {
				return cgroupPath, nil
			}
			// For cgroup v1 with systemd controller
			if strings.Contains(parts[1], "systemd") {
				return cgroupPath, nil
			}
		}
	}

	// If we didn't find it in the expected format, try another approach
	// Check if we're in a service by looking for INVOCATION_ID
	if os.Getenv("INVOCATION_ID") != "" {
		// We're in a systemd service, try to construct the path
		// This is a fallback and might not always work
		serviceName := getServiceNameFromEnvironment()
		if serviceName != "" {
			return fmt.Sprintf("/system.slice/%s.service", serviceName), nil
		}
	}

	return "", fmt.Errorf("not running in a systemd service")
}

// findChildProcesses finds all child processes of a given PID
func findChildProcesses(parentPID int) ([]int, error) {
	var children []int

	// Read /proc to find children
	procDir, err := os.Open("/proc")
	if err != nil {
		return nil, err
	}
	defer procDir.Close()

	entries, err := procDir.Readdir(-1)
	if err != nil {
		return nil, err
	}

	parentPIDStr := strconv.Itoa(parentPID)

	for _, entry := range entries {
		// Skip non-numeric directories
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Read the stat file to get PPID
		statPath := fmt.Sprintf("/proc/%d/stat", pid)
		statData, err := os.ReadFile(statPath)
		if err != nil {
			continue // Process might have exited
		}

		// Parse PPID from stat file
		// Format: pid (comm) state ppid ...
		statStr := string(statData)
		// Find the last ) to skip the command name which might contain spaces/parens
		lastParen := strings.LastIndex(statStr, ")")
		if lastParen == -1 {
			continue
		}

		fields := strings.Fields(statStr[lastParen+1:])
		if len(fields) < 2 {
			continue
		}

		// fields[1] is PPID (fields[0] is state)
		if fields[1] == parentPIDStr {
			children = append(children, pid)
			// Recursively find children of children
			grandchildren, _ := findChildProcesses(pid)
			children = append(children, grandchildren...)
		}
	}

	return children, nil
}

// getServiceNameFromEnvironment tries to extract service name from environment
func getServiceNameFromEnvironment() string {
	// Try to get from systemd environment variables
	// This is a heuristic and might not always work

	// Check if we can read our own cmdline to find service name
	cmdline, err := os.ReadFile("/proc/self/cmdline")
	if err != nil {
		return ""
	}

	// Look for common patterns in cmdline
	cmdlineStr := string(cmdline)
	if strings.Contains(cmdlineStr, "miren") {
		return "miren"
	}

	return ""
}

// FindContainerdPID finds the PID of containerd by socket path
func FindContainerdPID(socketPath string) (int, error) {
	// Method 1: Check all processes for the socket path in their cmdline
	procDir, err := os.Open("/proc")
	if err != nil {
		return 0, err
	}
	defer procDir.Close()

	entries, err := procDir.Readdir(-1)
	if err != nil {
		return 0, err
	}

	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Read cmdline
		cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
		cmdline, err := os.ReadFile(cmdlinePath)
		if err != nil {
			continue
		}

		cmdlineStr := string(cmdline)
		// Check if this is containerd with our socket
		if strings.Contains(cmdlineStr, "containerd") &&
			strings.Contains(cmdlineStr, socketPath) {
			return pid, nil
		}
	}

	// Method 2: Use lsof or ss to find process holding the socket
	// (would require exec.Command which we're trying to avoid)

	return 0, fmt.Errorf("containerd process not found for socket %s", socketPath)
}
