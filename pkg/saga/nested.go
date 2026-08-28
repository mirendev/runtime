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
		childID = deriveChildID(parentExecID, sagaName, actionName)
	}
	exec, err := parent.createChildExecution(ctx, def, cfg.inputs, childID, parentExecID)
	if err != nil {
		return nil, fmt.Errorf("creating nested execution: %w", err)
	}

	// Run the child saga
	if err := parent.runExecution(ctx, def, exec); err != nil {
		return nil, err
	}

	return collectOutputs(def, exec), nil
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
		if exec.DefinitionName != def.Name || exec.DefinitionVersion != def.Version {
			return nil, fmt.Errorf("existing execution %s has definition %s@%d, expected %s@%d",
				id, exec.DefinitionName, exec.DefinitionVersion, def.Name, def.Version)
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

	return parent.runUndo(ctx, def, exec)
}

// deriveChildID produces a deterministic execution ID from the parent execution,
// the child saga name, and the calling action name. This ensures that re-executing
// a parent action during recovery produces the same child ID, enabling
// createChildExecution's idempotency check to find the prior child execution.
func deriveChildID(parentExecID, sagaName, actionName string) string {
	h := sha256.Sum256([]byte(parentExecID + "\x00" + sagaName + "\x00" + actionName))
	return sagaIDKind + "/" + sagaIDName + "-" + base58.Encode(h[:16])
}
