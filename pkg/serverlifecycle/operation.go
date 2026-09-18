// Package serverlifecycle restarts and upgrades the server as durable,
// idempotent operations: JSON records on disk that outlive the process they
// act on, so a caller can hand off an operation id and ask about it later.
package serverlifecycle

import (
	"fmt"
	"time"

	"miren.dev/runtime/pkg/idgen"
)

type Action string

const (
	ActionRestart Action = "restart"
	ActionUpgrade Action = "upgrade"
)

// Phase is a checkpoint: an executor resumes from the recorded phase, and
// each phase's work is safe to repeat. The list is meant to grow: the
// pre-upgrade backup today is an etcd snapshot, and the full RFD-75 bundle
// slots into the same phase and BackupRef.
type Phase string

const (
	PhasePending     Phase = "pending"
	PhaseDownloading Phase = "downloading"
	// PhaseBackingUp sits after the download so the snapshot is as fresh as
	// possible when the restart happens, and so a failed download or an
	// already-installed target never costs a snapshot.
	PhaseBackingUp  Phase = "backing_up"
	PhaseInstalling Phase = "installing"
	PhaseRestarting Phase = "restarting"
	PhaseVerifying  Phase = "verifying"
	// PhaseUpgradingRunners follows a verified server upgrade on a cluster
	// with runners. The executor hands the operation to the server here:
	// the server has the node inventory and the RPC to each runner, and it
	// is the new build, which is the one that knows the runner protocol.
	PhaseUpgradingRunners Phase = "upgrading_runners"
	PhaseRollingBack      Phase = "rolling_back"

	PhaseSucceeded  Phase = "succeeded"
	PhaseFailed     Phase = "failed"
	PhaseRolledBack Phase = "rolled_back"

	// StepSkipped is terminal and step-only: the runner was not asked to
	// upgrade, and the step's Error says why (not ready, predates managed
	// upgrades). An operation never has it.
	StepSkipped Phase = "skipped"
)

func (p Phase) Terminal() bool {
	switch p {
	case PhaseSucceeded, PhaseFailed, PhaseRolledBack, StepSkipped:
		return true
	case PhasePending, PhaseDownloading, PhaseBackingUp, PhaseInstalling, PhaseRestarting, PhaseVerifying, PhaseUpgradingRunners, PhaseRollingBack:
	}
	return false
}

// NodeStep is one runner's part of an upgrade. The runner runs its own
// operation in its own ledger; the step mirrors what the server last saw of
// it, so the cluster record stands on its own.
type NodeStep struct {
	Name     string `json:"name"`
	RunnerID string `json:"runner_id"`
	// OperationID is the runner-side operation, minted before the request
	// so a repeated request finds it rather than starting another.
	OperationID string `json:"operation_id,omitempty"`
	// Phase is the runner operation's phase as last observed, PhasePending
	// before the runner was asked, or StepSkipped.
	Phase           Phase  `json:"phase"`
	Error           string `json:"error,omitempty"`
	Progress        string `json:"progress,omitempty"`
	PreviousVersion string `json:"previous_version,omitempty"`
	NewVersion      string `json:"new_version,omitempty"`
	// Cordoned records that the walk cordoned the node for this step, so a
	// resumed walk knows to uncordon it. An operator's cordon is left alone.
	Cordoned bool `json:"cordoned,omitempty"`

	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (s *NodeStep) Done() bool {
	return s.Phase.Terminal()
}

func (s *NodeStep) Succeeded() bool {
	return s.Phase == PhaseSucceeded
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

	// BackupRef names the data snapshot taken before an upgrade; empty when
	// none was taken. DataRestore is set on rollback and asks the restarted
	// server to put BackupRef back before it serves data; the server's answer
	// lands in it once the executor has read the RestoreResult.
	BackupRef   string       `json:"backup_ref,omitempty"`
	DataRestore *DataRestore `json:"data_restore,omitempty"`
	// Progress is a short note for the current phase, e.g. a download percentage.
	Progress string `json:"progress,omitempty"`

	// DrivenBy is the server instance that took the operation over for the
	// runner phase, and Nodes is one step per runner it found. Both are set
	// together, so DrivenBy on a record with no Nodes means a cluster with
	// nothing to walk.
	DrivenBy string      `json:"driven_by,omitempty"`
	Nodes    []*NodeStep `json:"nodes,omitempty"`

	// A successful operation ends with NewInstanceID != PreviousInstanceID;
	// that change is how we know the restart actually happened.
	PreviousInstanceID string `json:"previous_instance_id,omitempty"`
	PreviousVersion    string `json:"previous_version,omitempty"`
	PreviousCommit     string `json:"previous_commit,omitempty"`
	NewInstanceID      string `json:"new_instance_id,omitempty"`
	NewVersion         string `json:"new_version,omitempty"`
	// Components are the runtime versions (containerd, runc, ...) the server
	// reported once it was up. A base upgrade replaces those binaries next to
	// miren, and this is the record that the restarted server is on them.
	Components map[string]string `json:"components,omitempty"`

	// RollbackFrom names the instance that asked for the rollback's restart,
	// so a resumed rollback can tell a restart that already took (the
	// instance answering now is a different one) from one still to do. The
	// systemd executor outlives the restart and never resumes; the container
	// executor is that instance and dies at the restart it asks for.
	// container-boot, rolling back a build that never answered, writes its
	// own name.
	RollbackFrom string `json:"rollback_from,omitempty"`
	// BootAttempts counts container boots of the new build since the restart
	// phase, kept by container-boot; it is what catches a build that crashes
	// before the executor inside it can run.
	BootAttempts int `json:"boot_attempts,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// DataRestore is the restore request a rollback records on the operation,
// and its outcome.
type DataRestore struct {
	BackupRef string `json:"backup_ref"`
	// ForVersion and ForCommit name the build the request is for: the one
	// being rolled back to. The request is persisted before the binary is
	// swapped, and the build being rolled back from may still be crash
	// looping under systemd at that point. If it honored the request it
	// would restore, migrate the data again, and fail, leaving a settled
	// request and migrated data for the build that actually needed it.
	ForVersion string `json:"for_version,omitempty"`
	ForCommit  string `json:"for_commit,omitempty"`

	RestoredAt *time.Time `json:"restored_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// MeantFor reports whether the build identified by version and commit is
// the one this request is for. A request that could not name a build (the
// server was unreachable when the operation began) is for whoever boots.
func (r *DataRestore) MeantFor(version, commit string) bool {
	if r.ForVersion == "" && r.ForCommit == "" {
		return true
	}
	return sameBuild(r.ForVersion, r.ForCommit, version, commit)
}

// RestoreResult is what the server writes after acting on a DataRestore
// request during boot. It is a file beside the operation record rather than
// a field in it because the executor keeps its own copy of the record and
// rewrites it while the server boots; a second writer would be overwritten.
//
// A result with an Error does not settle the request: the server refuses to
// start on data it was asked to replace, and tries again on its next boot.
// Only a successful restore or an operator's Abandon ends that.
type RestoreResult struct {
	OperationID string    `json:"operation_id"`
	BackupRef   string    `json:"backup_ref"`
	RestoredAt  time.Time `json:"restored_at"`
	Error       string    `json:"error,omitempty"`
	// Abandoned records that an operator gave up on the restore and told the
	// server to start on the data as it is.
	Abandoned bool `json:"abandoned,omitempty"`
}

// Settled reports whether the request this result answers is over, one way
// or the other.
func (r *RestoreResult) Settled() bool {
	return r.Error == "" || r.Abandoned
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
	return idgen.ULID()
}

func (o *Operation) Done() bool {
	return o.Phase.Terminal()
}

func (o *Operation) Succeeded() bool {
	return o.Phase == PhaseSucceeded
}

// HandedOff reports whether the executor is finished with the operation but
// the operation is not: the server drives it from here.
func (o *Operation) HandedOff() bool {
	return o.Phase == PhaseUpgradingRunners
}

// Adopted reports whether a server has taken over a handed-off operation.
func (o *Operation) Adopted() bool {
	return o.DrivenBy != ""
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
