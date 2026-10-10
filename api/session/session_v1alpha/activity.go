package session_v1alpha

import (
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
)

// SelfReportedActivity never trusts a stale or unassigned Session as idle.
func (s Session) SelfReportedActivity(now time.Time) SessionActivity {
	if s.Phase != READY || s.Sandbox == "" || s.ActivityAt.IsZero() ||
		now.Before(s.ActivityAt) || now.Sub(s.ActivityAt) >= compute.ActivityFreshFor {
		return UNKNOWN
	}
	return s.Activity
}
