// Package serverlifecycle restarts and upgrades the server as durable,
// idempotent operations: JSON records on disk that outlive the process they
// act on, so a caller can hand off an operation id and ask about it later.
package serverlifecycle

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

type Action string

const (
	ActionRestart Action = "restart"
	ActionUpgrade Action = "upgrade"
)

// Phase is a checkpoint: an executor resumes from the recorded phase, and
// each phase's work is safe to repeat.
type Phase string

const (
	PhasePending     Phase = "pending"
	PhaseDownloading Phase = "downloading"
	PhaseInstalling  Phase = "installing"
	PhaseRestarting  Phase = "restarting"
	PhaseVerifying   Phase = "verifying"
	PhaseRollingBack Phase = "rolling_back"

	PhaseSucceeded  Phase = "succeeded"
	PhaseFailed     Phase = "failed"
	PhaseRolledBack Phase = "rolled_back"
)

func (p Phase) Terminal() bool {
	switch p {
	case PhaseSucceeded, PhaseFailed, PhaseRolledBack:
		return true
	case PhasePending, PhaseDownloading, PhaseInstalling, PhaseRestarting, PhaseVerifying, PhaseRollingBack:
	}
	return false
}

// Operation is the durable record of one restart or upgrade.
type Operation struct {
	ID          string `json:"id"`
	Action      Action `json:"action"`
	RequestedBy string `json:"requested_by,omitempty"`

	// TargetVersion is as requested ("latest", "main", a tag); ResolvedVersion
	// and ResolvedCommit are what it became.
	TargetVersion   string `json:"target_version,omitempty"`
	ResolvedVersion string `json:"resolved_version,omitempty"`
	ResolvedCommit  string `json:"resolved_commit,omitempty"`
	// ArtifactType ("base" or "release"), NoRollback, and ReadyTimeoutSeconds
	// override the executor defaults for one upgrade.
	ArtifactType        string `json:"artifact_type,omitempty"`
	NoRollback          bool   `json:"no_rollback,omitempty"`
	ReadyTimeoutSeconds int    `json:"ready_timeout_seconds,omitempty"`

	Phase Phase  `json:"phase"`
	Error string `json:"error,omitempty"`
	// Progress is a short note for the current phase, e.g. a download percentage.
	Progress string `json:"progress,omitempty"`

	// A successful operation ends with NewInstanceID != PreviousInstanceID;
	// that change is how we know the restart actually happened.
	PreviousInstanceID string `json:"previous_instance_id,omitempty"`
	PreviousVersion    string `json:"previous_version,omitempty"`
	PreviousCommit     string `json:"previous_commit,omitempty"`
	NewInstanceID      string `json:"new_instance_id,omitempty"`
	NewVersion         string `json:"new_version,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func NewOperation(action Action, requestedBy string) *Operation {
	now := time.Now().UTC()
	return &Operation{
		ID:          NewID(),
		Action:      action,
		RequestedBy: requestedBy,
		Phase:       PhasePending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// NewID mints a ULID so a directory listing sorts by creation time.
func NewID() string {
	return ulid.MustNew(ulid.Now(), rand.Reader).String()
}

func (o *Operation) Done() bool {
	return o.Phase.Terminal()
}

func (o *Operation) Succeeded() bool {
	return o.Phase == PhaseSucceeded
}

func (o *Operation) validate() error {
	if o.ID == "" {
		return fmt.Errorf("operation has no id")
	}
	switch o.Action {
	case ActionRestart:
	case ActionUpgrade:
		if o.TargetVersion == "" {
			return fmt.Errorf("upgrade operation %s has no target version", o.ID)
		}
	default:
		return fmt.Errorf("operation %s has unknown action %q", o.ID, o.Action)
	}
	return nil
}
