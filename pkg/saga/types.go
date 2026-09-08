// Package saga implements the Saga pattern for distributed operations with
// crash recovery. Each saga is a sequence of steps where each step has a
// corresponding undo operation. The framework guarantees that either all
// steps complete successfully or all completed steps are rolled back.
//
// See RFD-35 for detailed design documentation.
package saga

import (
	"time"
)

// Edge is a zero-size type used to declare ordering dependencies between saga
// actions without carrying data. Edge fields participate in the dependency
// graph (via saga struct tags) but are skipped during serialization and
// deserialization at runtime.
type Edge struct{}

// Status represents the current state of a saga execution.
type Status string

const (
	// StatusPending indicates the saga has been created but not started.
	StatusPending Status = "pending"

	// StatusRunning indicates the saga is actively executing actions.
	StatusRunning Status = "running"

	// StatusUndoing indicates the saga is rolling back due to a failure.
	StatusUndoing Status = "undoing"

	// StatusCompleted indicates all actions completed successfully.
	StatusCompleted Status = "completed"

	// StatusFailed indicates the saga failed and all undos have been attempted.
	StatusFailed Status = "failed"
)

// ActionResult stores the outcome of a single action execution.
type ActionResult struct {
	// Output is the JSON-serialized output from the action.
	Output []byte `json:"output,omitempty"`

	// ExecutedAt is when the action was executed.
	ExecutedAt time.Time `json:"executed_at"`

	// UndoneAt is when the action was undone (nil if not undone).
	UndoneAt *time.Time `json:"undone_at,omitempty"`

	// Error is set if the action failed during execution.
	Error string `json:"error,omitempty"`
}

// Execution tracks the runtime state of a saga, persisted after each step.
type Execution struct {
	// ID is the unique identifier for this execution.
	ID string `json:"id"`

	// DefinitionName references the registered saga definition.
	DefinitionName string `json:"definition_name"`

	// DefinitionVersion is the version of the definition when started.
	DefinitionVersion int `json:"definition_version"`

	// InitialInputs contains the bootstrap data for the saga.
	// All values must be JSON-serializable.
	InitialInputs map[string]any `json:"initial_inputs"`

	// Status is the current state of the execution.
	Status Status `json:"status"`

	// ExecutedActions maps action names to their results.
	ExecutedActions map[string]*ActionResult `json:"executed_actions"`

	// ExecutionOrder records the order actions were executed for reverse undo.
	ExecutionOrder []string `json:"execution_order"`

	// ParentExecutionID links this execution to a parent saga when run as a nested child.
	ParentExecutionID string `json:"parent_execution_id,omitempty"`

	// RecoveryScope is the stable identity of the executor allowed to recover
	// this execution. An empty scope preserves the unscoped behavior used by
	// executors that have no distributed ownership boundary.
	RecoveryScope string `json:"recovery_scope,omitempty"`

	// Error is set if the saga failed.
	Error string `json:"error,omitempty"`

	// CreatedAt is when the execution was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the execution was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// TerminalExecution summarizes a finished execution for retention purposes:
// which one, when it stopped changing, and whose child it is.
type TerminalExecution struct {
	// ID identifies the execution.
	ID string

	// FinishedAt is when the execution last changed state, which for a terminal
	// execution is when it finished.
	FinishedAt time.Time

	// ParentID is set when this execution ran as a nested child. Retention
	// needs it because a finished child is not independently safe to delete:
	// its parent re-finds it by deterministic ID rather than re-running it, so
	// deleting one out from under a live parent turns a resumed saga into a
	// duplicated one.
	ParentID string
}

// IncompleteQuery selects one page of incomplete executions.
//
// It is a struct rather than positional arguments because what recovery needs
// to say about a page grows: the filtering that keeps an executor from loading
// another executor's payloads is expressed here too.
type IncompleteQuery struct {
	// Cursor resumes after the last execution of a previous page. Empty starts
	// at the beginning.
	Cursor string

	// Limit caps how many executions the page materializes. Zero or less means
	// the backend's own cap, which every backend has: a page with no ceiling is
	// the unbounded read this whole design exists to remove.
	Limit int
}

// IncompletePage is one bounded page of executions needing recovery.
type IncompletePage struct {
	// Executions are the incomplete executions in this page.
	Executions []*Execution

	// Cursor resumes the walk after this page, and is empty once there is
	// nothing left.
	//
	// A short page does not mean the end. Backends that walk several status
	// indexes finish one before starting the next, and never straddle two in
	// one page, so a page can come back well under the limit with plenty still
	// to come. Only an empty cursor ends the walk.
	Cursor string
}

// TerminalQuery selects one page of terminal executions.
type TerminalQuery struct {
	// Cursor resumes after the last execution of a previous page. Empty starts
	// at the beginning.
	Cursor string

	// Limit caps how many executions the page summarizes. Zero or less means
	// the backend's own cap.
	Limit int
}

// TerminalPage is one bounded page of finished executions.
type TerminalPage struct {
	// Executions summarizes the terminal executions in this page.
	Executions []TerminalExecution

	// Cursor resumes the walk after this page, empty once the walk is done.
	// The same caveat as IncompletePage.Cursor applies: a short page is not an
	// ending, only an empty cursor is.
	Cursor string
}
