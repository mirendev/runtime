// Package apphealth defines the health classification strings that flow from
// app-status (ApplicationStatus / AppInfo) to consumers like `m app list` and
// the deploy poller. Keeping them in one place makes the server-derived value
// and the client-side interpretation a single shared contract.
package apphealth

const (
	// Healthy means every desired instance of the active version is RUNNING.
	Healthy = "healthy"
	// Degraded means at least one instance is serving but fewer than desired.
	Degraded = "degraded"
	// Starting means the version is active but no instance is serving yet.
	Starting = "starting"
	// Crashed means a pool is in crash cooldown.
	Crashed = "crashed"
	// Idle means the app is deliberately scaled to zero (no desired instances).
	Idle = "idle"
	// Ready means the app is deployed and available to invoke, but has no
	// long-running process to be healthy or idle. A task-only app is doing
	// exactly what it was configured to do; reporting it as idle would say it
	// went to sleep.
	Ready = "ready"
	// Unknown means there is no pool state to derive health from.
	Unknown = "unknown"
)

// State is one app's health classification and the counts behind it.
//
// It lives here rather than beside the code that derives it so that both the
// app RPC surface and the cloud health reporter can name the same value without
// either depending on the other. The derivation itself stays in one place: see
// AppInfo.ListAppHealth.
type State struct {
	Name   string
	Health string

	// Pooled reports whether the counts below came from real sandbox pools.
	//
	// It matters because an app with an active version but no pools yet reports
	// zero instances, which is a different statement from an app whose instance
	// counts do not apply at all, and only a pooled app has a scaling mode.
	// Carried on the value so callers do not each re-derive it and drift.
	Pooled           bool
	ReadyInstances   int32
	DesiredInstances int32
	ScalingMode      string

	InCooldown      bool
	CrashCount      int64
	CooldownSeconds int32
}
