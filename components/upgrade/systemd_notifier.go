//go:build linux
// +build linux

package upgrade

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// SystemdNotifier handles systemd notification protocol
type SystemdNotifier struct {
	socketPath string
}

// NewSystemdNotifier creates a new systemd notifier if running under systemd
func NewSystemdNotifier() *SystemdNotifier {
	// Auto-detect if we're running under systemd
	if !IsRunningUnderSystemd() {
		return nil
	}

	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		// We're under systemd but not Type=notify, still return a notifier
		// (it will be a no-op but allows consistent code flow)
		return &SystemdNotifier{}
	}

	// Handle abstract socket (starts with @)
	if socketPath[0] == '@' {
		socketPath = "\x00" + socketPath[1:]
	}

	return &SystemdNotifier{
		socketPath: socketPath,
	}
}

// IsRunningUnderSystemd detects if the process is running under systemd supervision
func IsRunningUnderSystemd() bool {
	// Method 1: Check for NOTIFY_SOCKET (Type=notify services)
	if os.Getenv("NOTIFY_SOCKET") != "" {
		return true
	}

	// Method 2: Check for INVOCATION_ID (all systemd services)
	if os.Getenv("INVOCATION_ID") != "" {
		return true
	}

	// Method 3: Check for JOURNAL_STREAM (systemd logging)
	if os.Getenv("JOURNAL_STREAM") != "" {
		return true
	}

	// Method 4: Check if parent is systemd (PID 1)
	if os.Getppid() == 1 {
		// Check if init system is systemd
		if data, err := os.ReadFile("/proc/1/comm"); err == nil {
			if string(data) == "systemd\n" {
				return true
			}
		}
	}

	// Method 5: Check cgroup for systemd service pattern
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		// Look for systemd service cgroup patterns
		cgroupStr := string(data)
		if strings.Contains(cgroupStr, "/system.slice/") ||
			strings.Contains(cgroupStr, ".service") {
			return true
		}
	}

	return false
}

// Notify sends a notification to systemd
func (n *SystemdNotifier) Notify(state string) error {
	if n == nil || n.socketPath == "" {
		return nil // Not running under systemd
	}

	conn, err := net.Dial("unixgram", n.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to systemd: %w", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte(state))
	if err != nil {
		return fmt.Errorf("failed to write to systemd: %w", err)
	}

	return nil
}

// NotifyReady notifies systemd that the service is ready
func (n *SystemdNotifier) NotifyReady() error {
	return n.Notify("READY=1\nSTATUS=Running")
}

// NotifyReloading notifies systemd that the service is reloading (upgrading)
func (n *SystemdNotifier) NotifyReloading() error {
	return n.Notify("RELOADING=1\nSTATUS=Upgrade in progress")
}

// NotifyStopping notifies systemd that the service is stopping
func (n *SystemdNotifier) NotifyStopping() error {
	return n.Notify("STOPPING=1\nSTATUS=Shutting down for upgrade")
}

// NotifyPIDChange notifies systemd of a PID change during upgrade
func (n *SystemdNotifier) NotifyPIDChange(newPID int) error {
	return n.Notify(fmt.Sprintf("READY=1\nMAINPID=%d\nSTATUS=Upgrade complete", newPID))
}

// NotifyWatchdog sends a watchdog keepalive to systemd
func (n *SystemdNotifier) NotifyWatchdog() error {
	return n.Notify("WATCHDOG=1")
}

// NotifyStatus sends a status message to systemd
func (n *SystemdNotifier) NotifyStatus(status string) error {
	return n.Notify(fmt.Sprintf("STATUS=%s", status))
}
