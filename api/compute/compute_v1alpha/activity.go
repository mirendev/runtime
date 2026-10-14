package compute_v1alpha

import "time"

// ActivityUnknown represents an absent or expired report; consumers must treat it as active.
const ActivityUnknown ActivityState = "unknown"

const ActivityFreshFor = 2 * time.Minute

// SelfReportedActivity returns the workload's state only while its report is fresh.
// Unknown must be treated as active by consumers, including after expiry.
func (s Sandbox) SelfReportedActivity(now time.Time) ActivityState {
	if s.Status != RUNNING || s.Activity.ReportedAt.IsZero() || now.Before(s.Activity.ReportedAt) || now.Sub(s.Activity.ReportedAt) >= ActivityFreshFor {
		return ActivityUnknown
	}
	return s.Activity.State
}
