package saga

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mr-tron/base58"
)

// NestedResult wraps the outputs from a completed child saga execution.
type NestedResult struct {
	ExecutionID string
	outputs     map[string]json.RawMessage
}

// Get deserializes a named output from the child saga into target.
func (nr *NestedResult) Get(key string, target any) error {
	raw, ok := nr.outputs[key]
	if !ok {
		return fmt.Errorf("nested output %q not found", key)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("deserializing nested output %q: %w", key, err)
	}
	return nil
}

// Has returns true if the child saga produced an output with the given key.
func (nr *NestedResult) Has(key string) bool {
	_, ok := nr.outputs[key]
	return ok
}

// NestedOption configures a RunNested call.
type NestedOption func(*nestedConfig)

type nestedConfig struct {
	inputs map[string]any
	id     string
}

// WithNestedInput adds an initial input to the child saga.
func WithNestedInput(key string, value any) NestedOption {
	return func(c *nestedConfig) {
		c.inputs[key] = value
	}
}

// WithNestedID sets a specific execution ID for the child saga.
func WithNestedID(id string) NestedOption {
	return func(c *nestedConfig) {
		c.id = id
	}
}

// RunNested executes a child saga from within a parent saga action. It reuses
// the parent executor's registry and storage for durability and observability.
// The child execution's ParentExecutionID is set to the current execution.
func RunNested(ctx context.Context, sagaName string, opts ...NestedOption) (*NestedResult, error) {
	parent, ok := executorFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("RunNested called outside of a saga execution (no executor in context)")
	}

	cfg := &nestedConfig{
		inputs: make(map[string]any),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Look up definition from parent's registry
	def, ok := parent.registry.Get(sagaName)
	if !ok {
		return nil, fmt.Errorf("nested saga definition %q not found in registry", sagaName)
	}

	// Create child execution with parent link
	parentExecID, _ := executionIDFromContext(ctx)

	// Generate child execution ID — deterministic by default for idempotent recovery
	childID := cfg.id
	if childID == "" {
		actionName, _ := actionNameFromContext(ctx)
		childID = NestedExecutionID(parentExecID, sagaName, actionName)
	}
	controlCtx := controlContextFromContext(ctx)
	exec, err := parent.createChildExecution(controlCtx, def, cfg.inputs, childID, parentExecID)
	if err != nil {
		return nil, fmt.Errorf("creating nested execution: %w", err)
	}

	// A terminal child is reported, not re-driven, which is the contract
	// resume gives a top-level execution. Running a completed child again
	// would execute whatever actions the current definition has that it
	// never ran, and running a failed one, abandoned ones included, would
	// redo work that was compensated or deliberately given up.
	switch exec.Status {
	case StatusCompleted:
		return collectOutputs(def, exec), nil
	case StatusFailed:
		return nil, fmt.Errorf("nested saga %q execution %q failed: %s", sagaName, exec.ID, exec.Error)
	case StatusPending, StatusRunning, StatusUndoing:
		// Still in flight: drive it below.
	}

	// Drive the child the way resume drives a top-level execution. A child
	// interrupted while undoing, or after recording a failed action, has to
	// finish compensating; running it forward instead would skip the failed
	// action as already executed and report the child completed.
	if err := parent.resumeWithActionContext(controlCtx, ctx, def, exec); err != nil {
		return nil, err
	}
	if exec.Status != StatusCompleted {
		return nil, fmt.Errorf("nested saga %q execution %q did not complete (status %q): %s",
			sagaName, exec.ID, exec.Status, exec.Error)
	}

	return collectOutputs(def, exec), nil
}

// ResumeNested drives the calling action's own child execution of sagaName
// when a previous attempt of the action left it in flight, and reports
// found=false when there is no such child or it already finished.
//
// An action that inspects shared state before calling RunNested should call
// this first. On a re-run, whatever the child already built is visible to
// that inspection and looks like someone else's work, so the action would
// back off from or tear down its own half-finished child instead of
// resuming it. The child keeps the inputs it was created with; terminal
// children are left to the caller's normal path.
func ResumeNested(ctx context.Context, sagaName string) (*NestedResult, bool, error) {
	parent, ok := executorFromContext(ctx)
	if !ok {
		return nil, false, fmt.Errorf("ResumeNested called outside of a saga execution (no executor in context)")
	}

	parentExecID, _ := executionIDFromContext(ctx)
	actionName, _ := actionNameFromContext(ctx)
	childID := NestedExecutionID(parentExecID, sagaName, actionName)

	exec, err := parent.storage.Get(controlContextFromContext(ctx), childID)
	if errors.Is(err, ErrExecutionNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("checking for in-flight nested execution: %w", err)
	}
	if isTerminal(exec.Status) {
		return nil, false, nil
	}

	result, err := RunNested(ctx, sagaName, WithNestedID(childID))
	return result, true, err
}

// createChildExecution builds and persists a new Execution linked to a parent.
func (e *Executor) createChildExecution(ctx context.Context, def *Definition, inputs map[string]any, id, parentExecID string) (*Execution, error) {
	exec, err := e.storage.Get(ctx, id)
	if err == nil {
		// Execution already exists (idempotent retry) — validate it matches the expected definition and parent.
		adoptScope, scopeErr := e.scopeForExisting(exec)
		if scopeErr != nil {
			return nil, scopeErr
		}
		if exec.DefinitionName != def.Name {
			return nil, fmt.Errorf("existing execution %s belongs to saga %q, expected %q",
				id, exec.DefinitionName, def.Name)
		}
		// Terminal children run nothing here (RunNested reports them as they
		// stand), so like resume this only vouches for ones it would drive.
		if !isTerminal(exec.Status) {
			if err := checkResumable(def, exec); err != nil {
				e.recordBlocked(ctx, exec, err)
				return nil, err
			}
		}
		if exec.ParentExecutionID != parentExecID {
			return nil, fmt.Errorf("execution %s already exists for parent %s, expected parent %s",
				id, exec.ParentExecutionID, parentExecID)
		}
		if adoptScope {
			exec.RecoveryScope = e.recoveryScope
			exec.UpdatedAt = time.Now()
			if err := e.storage.Save(ctx, exec); err != nil {
				return nil, fmt.Errorf("persisting recovery scope for nested execution %q: %w", id, err)
			}
		}
		return exec, nil
	}
	if !errors.Is(err, ErrExecutionNotFound) {
		return nil, fmt.Errorf("checking for existing execution: %w", err)
	}

	now := time.Now()
	exec = &Execution{
		ID:                id,
		DefinitionName:    def.Name,
		DefinitionVersion: def.Version,
		InitialInputs:     inputs,
		ParentExecutionID: parentExecID,
		RecoveryScope:     e.recoveryScope,
		Status:            StatusPending,
		ExecutedActions:   make(map[string]*ActionResult),
		ExecutionOrder:    []string{},
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := e.storage.Save(ctx, exec); err != nil {
		return nil, fmt.Errorf("persisting initial state: %w", err)
	}
	e.counts.Add(def.Name, EventStarted, 1)

	return exec, nil
}

// UndoNested compensates a previously completed nested saga. Call this from
// an undo handler to roll back the child saga's actions.
func UndoNested(ctx context.Context, executionID string) error {
	parent, ok := executorFromContext(ctx)
	if !ok {
		return fmt.Errorf("UndoNested called outside of a saga execution (no executor in context)")
	}

	exec, err := parent.storage.Get(ctx, executionID)
	if err != nil {
		return fmt.Errorf("loading nested execution %q: %w", executionID, err)
	}

	def, ok := parent.registry.Get(exec.DefinitionName)
	if !ok {
		return fmt.Errorf("saga definition %q not found for nested undo", exec.DefinitionName)
	}
	// A failed child is already compensated, or was abandoned, which gave up
	// compensating it on purpose. Either way there is nothing left to undo.
	if exec.Status == StatusFailed {
		return nil
	}

	if err := checkResumable(def, exec); err != nil {
		parent.recordBlocked(ctx, exec, err)
		return err
	}

	// runUndo reports a finished compensation as "saga failed", which is right
	// for whoever started a saga but wrong for a parent asking whether its
	// child was undone: it would read success as an undo error and never
	// finish unwinding. Failed is only reached once every undo succeeded.
	err = parent.runUndo(ctx, def, exec, nil)
	if exec.Status == StatusFailed {
		return nil
	}
	return err
}

// NestedExecutionID is the ID RunNested gives the child of sagaName started by
// the named action of a parent execution, absent WithNestedID. It is
// deterministic so that re-executing a parent action during recovery finds the
// prior child execution through createChildExecution's idempotency check.
func NestedExecutionID(parentExecID, sagaName, actionName string) string {
	h := sha256.Sum256([]byte(parentExecID + "\x00" + sagaName + "\x00" + actionName))
	return sagaIDKind + "/" + sagaIDName + "-" + base58.Encode(h[:16])
}
