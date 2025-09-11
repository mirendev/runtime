//go:build darwin
// +build darwin

package upgrade

// SystemdNotifier handles systemd notification protocol (stub for Darwin)
type SystemdNotifier struct{}

// NewSystemdNotifier creates a new systemd notifier (returns nil on Darwin)
func NewSystemdNotifier() *SystemdNotifier {
	return nil
}

// IsRunningUnderSystemd always returns false on Darwin
func IsRunningUnderSystemd() bool {
	return false
}

// Notify sends a notification to systemd (no-op on Darwin)
func (n *SystemdNotifier) Notify(state string) error {
	return nil
}

// NotifyReady notifies systemd that the service is ready (no-op on Darwin)
func (n *SystemdNotifier) NotifyReady() error {
	return nil
}

// NotifyReloading notifies systemd that the service is reloading (no-op on Darwin)
func (n *SystemdNotifier) NotifyReloading() error {
	return nil
}

// NotifyStopping notifies systemd that the service is stopping (no-op on Darwin)
func (n *SystemdNotifier) NotifyStopping() error {
	return nil
}

// NotifyPIDChange notifies systemd of a PID change during upgrade (no-op on Darwin)
func (n *SystemdNotifier) NotifyPIDChange(newPID int) error {
	return nil
}

// NotifyWatchdog sends a watchdog keepalive to systemd (no-op on Darwin)
func (n *SystemdNotifier) NotifyWatchdog() error {
	return nil
}

// NotifyStatus sends a status message to systemd (no-op on Darwin)
func (n *SystemdNotifier) NotifyStatus(status string) error {
	return nil
}
