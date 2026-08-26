package deploylifecycle

import (
	"time"

	"miren.dev/runtime/pkg/entity"
)

// Canonical reports whether any attempt-shaped field is present. Outcome cannot
// be the discriminator because it is deliberately absent while an attempt is
// in progress.
func (r *Record) Canonical() bool {
	if r == nil || r.Deployment == nil {
		return false
	}
	d := r.Deployment
	return d.Outcome != "" || d.App != "" || d.Version != "" ||
		d.SourceDeployment != "" || d.Operation != "" ||
		!d.StartedAt.IsZero() || !d.FinishedAt.IsZero()
}

// Status returns the attempt's lifecycle state. A canonical attempt with no
// outcome is still in progress; outcomes exist only after it settles. Legacy
// serving-state statuses collapse to succeeded because serving state belongs to
// app.active_deployment.
func (r *Record) Status() Status {
	if r == nil || r.Deployment == nil {
		return ""
	}
	if r.Canonical() {
		if r.Deployment.Outcome == "" {
			return StatusInProgress
		}
		return statusFromSchema(r.Deployment.Outcome)
	}
	return Status(r.Deployment.Status).Canonical()
}

func (r *Record) Operation() Operation {
	if !r.Canonical() {
		return ""
	}
	switch r.Deployment.Operation {
	case string(OperationBuild):
		return OperationBuild
	case string(OperationRedeploy):
		return OperationRedeploy
	case string(OperationRollback):
		return OperationRollback
	case string(OperationConfigChange):
		return OperationConfigChange
	default:
		return ""
	}
}

func (r *Record) Phase() Phase {
	if r == nil || r.Deployment == nil {
		return ""
	}
	return Phase(r.Deployment.Phase)
}

func (r *Record) AppID() entity.Id {
	if r.Canonical() {
		return r.Deployment.App
	}
	return ""
}

// AppVersion returns the canonical version reference, or the normalized legacy
// value for an unmigrated record.
func (r *Record) AppVersion() string {
	if r == nil || r.Deployment == nil {
		return ""
	}
	if r.Canonical() {
		return string(r.Deployment.Version)
	}
	version := r.Deployment.AppVersion
	if version == pendingBuildSentinel || isFailedSentinel(version, string(r.Deployment.ID)) {
		return ""
	}
	return version
}

func (r *Record) SourceDeploymentID() string {
	if r == nil || r.Deployment == nil {
		return ""
	}
	if r.Canonical() {
		return string(r.Deployment.SourceDeployment)
	}
	return r.Deployment.SourceDeploymentId
}

func (r *Record) StartedAt() time.Time {
	if r == nil || r.Deployment == nil {
		return time.Time{}
	}
	if r.Canonical() {
		return r.Deployment.StartedAt
	}
	t, _ := time.Parse(time.RFC3339, r.Deployment.DeployedBy.Timestamp)
	return t
}

func (r *Record) FinishedAt() time.Time {
	if r == nil || r.Deployment == nil {
		return time.Time{}
	}
	if r.Canonical() {
		return r.Deployment.FinishedAt
	}
	t, _ := time.Parse(time.RFC3339, r.Deployment.CompletedAt)
	return t
}

func statusFromSchema(outcome string) Status {
	switch outcome {
	case string(StatusSucceeded):
		return StatusSucceeded
	case string(StatusFailed):
		return StatusFailed
	case string(StatusCancelled):
		return StatusCancelled
	case string(StatusInterrupted):
		return StatusInterrupted
	default:
		// Outcomes are terminal by contract. Treat a value written by a newer
		// runtime as interrupted so this older reader never turns a settled record
		// into an inert, non-terminal lock holder.
		return StatusInterrupted
	}
}

func schemaOutcome(status Status) string {
	switch status {
	case StatusActive, StatusSucceeded, StatusRolledBack:
		return string(StatusSucceeded)
	case StatusFailed, StatusCancelled, StatusInterrupted:
		return string(status)
	case StatusInProgress:
		return ""
	default:
		return ""
	}
}

func schemaOperation(operation Operation) string {
	return string(operation)
}

// setOutcome dual-writes the canonical result and the closest legacy status.
// A successful current attempt stays legacy-active until a later activation
// supersedes it. interrupted has no legacy equivalent and degrades to failed.
func (r *Record) setOutcome(status Status) {
	outcome := schemaOutcome(status)
	if outcome == "" {
		panic("deploylifecycle: setOutcome requires a terminal attempt result")
	}
	r.Deployment.Outcome = outcome
	switch status.Canonical() {
	case StatusSucceeded:
		r.Deployment.Status = string(StatusActive)
	case StatusInterrupted:
		r.Deployment.Status = string(StatusFailed)
	case StatusFailed, StatusCancelled:
		r.Deployment.Status = string(status.Canonical())
	case StatusInProgress, StatusActive, StatusRolledBack:
		panic("deploylifecycle: canonical terminal status resolved to " + string(status.Canonical()))
	default:
		panic("deploylifecycle: unknown terminal status " + string(status))
	}
}

// setInProgress initializes only the downgrade-compatible status. Canonical
// outcome remains absent until the attempt reaches a terminal result.
func (r *Record) setInProgress() {
	r.Deployment.Outcome = ""
	r.Deployment.Status = string(StatusInProgress)
}

func (r *Record) setPhase(phase Phase) {
	r.Deployment.Phase = string(phase)
}

func (r *Record) setVersion(version entity.Id) {
	r.Deployment.Version = version
	r.Deployment.AppVersion = string(version)
}

func (r *Record) setSourceDeployment(source entity.Id) {
	r.Deployment.SourceDeployment = source
	r.Deployment.SourceDeploymentId = string(source)
}
