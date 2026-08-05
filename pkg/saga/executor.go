package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/idgen"
)

// ErrExecutionNotFound is returned by Storage.Get when no execution exists for the given ID.
var ErrExecutionNotFound = errors.New("execution not found")

type executorCtxKey struct{}
type executionIDCtxKey struct{}
type actionNameCtxKey struct{}

func executorFromContext(ctx context.Context) (*Executor, bool) {
	e, ok := ctx.Value(executorCtxKey{}).(*Executor)
	return e, ok
}

func executionIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(executionIDCtxKey{}).(string)
	return id, ok
}

func actionNameFromContext(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(actionNameCtxKey{}).(string)
	return name, ok
}

// Storage persists saga execution state.
type Storage interface {
	// Save persists the execution state.
	Save(ctx context.Context, exec *Execution) error

	// Get retrieves an execution by ID.
	Get(ctx context.Context, id string) (*Execution, error)

	// ListIncomplete returns all executions that need recovery (Pending, Running, or Undoing).
	ListIncomplete(ctx context.Context) ([]*Execution, error)
}

// Executor orchestrates saga execution with durable logging.
type Executor struct {
	storage  Storage
	registry *Registry
	log      *slog.Logger
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*Executor)

// WithRegistry sets a custom registry for the executor.
// Useful for testing to avoid global state.
func WithRegistry(r *Registry) ExecutorOption {
	return func(e *Executor) {
		e.registry = r
	}
}

// WithLogger sets a custom logger for the executor.
func WithLogger(log *slog.Logger) ExecutorOption {
	return func(e *Executor) {
		e.log = log
	}
}

// NewExecutor creates an executor with the given storage and options.
func NewExecutor(storage Storage, opts ...ExecutorOption) *Executor {
	e := &Executor{
		storage:  storage,
		registry: globalRegistry,
		log:      slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// StartBuilder provides a fluent API for starting saga executions.
type StartBuilder struct {
	executor *Executor
	defName  string
	inputs   map[string]any
	id       string
}

// Start begins building a saga execution.
func (e *Executor) Start(definitionName string) *StartBuilder {
	return &StartBuilder{
		executor: e,
		defName:  definitionName,
		inputs:   make(map[string]any),
	}
}

// Input adds an initial input value to the saga execution.
func (sb *StartBuilder) Input(key string, value any) *StartBuilder {
	sb.inputs[key] = value
	return sb
}

// WithID sets a specific execution ID (otherwise one is generated).
func (sb *StartBuilder) WithID(id string) *StartBuilder {
	sb.id = id
	return sb
}

// Execute runs the saga to completion or failure.
func (sb *StartBuilder) Execute(ctx context.Context) error {
	return sb.executor.execute(ctx, sb.defName, sb.inputs, sb.id)
}

// execute runs a saga with the given definition and inputs.
func (e *Executor) execute(ctx context.Context, defName string, inputs map[string]any, id string) error {
	// Look up definition
	def, ok := e.registry.Get(defName)
	if !ok {
		return fmt.Errorf("saga definition %q not found", defName)
	}

	// Generate ID if not provided
	if id == "" {
		id = generateID()
	}

	// Create execution
	now := time.Now()
	exec := &Execution{
		ID:                id,
		DefinitionName:    defName,
		DefinitionVersion: def.Version,
		InitialInputs:     inputs,
		Status:            StatusPending,
		ExecutedActions:   make(map[string]*ActionResult),
		ExecutionOrder:    []string{},
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// Persist initial state
	if err := e.storage.Save(ctx, exec); err != nil {
		return fmt.Errorf("persisting initial state: %w", err)
	}

	return e.runExecution(ctx, def, exec)
}

// runExecution executes or resumes a saga.
func (e *Executor) runExecution(ctx context.Context, def *Definition, exec *Execution) error {
	log := e.log.With("saga", def.Name, "execution", exec.ID)

	// Update status to running
	exec.Status = StatusRunning
	exec.UpdatedAt = time.Now()
	if err := e.storage.Save(ctx, exec); err != nil {
		return fmt.Errorf("persisting running state: %w", err)
	}

	// Inject dependencies, executor, and execution ID into context
	ctx = injectDependencies(ctx, def.dependencies)
	ctx = context.WithValue(ctx, executorCtxKey{}, e)
	ctx = context.WithValue(ctx, executionIDCtxKey{}, exec.ID)

	// Build outputs map from already-executed actions
	outputs := make(map[string]json.RawMessage)
	for actionName, result := range exec.ExecutedActions {
		if result.UndoneAt != nil {
			continue // Skip undone actions
		}
		node := def.Actions[actionName]
		if node == nil {
			continue
		}
		// Extract output keys from the result
		if err := extractOutputs(node, result.Output, outputs); err != nil {
			log.Warn("failed to extract outputs from prior action", "action", actionName, "error", err)
		}
	}

	// Execute actions in order
	for _, actionName := range def.executionOrder {
		// Check for context cancellation between actions
		if err := ctx.Err(); err != nil {
			log.Info("context cancelled, stopping execution", "error", err)
			// Leave saga in Running state - recovery will resume it later
			return fmt.Errorf("saga execution interrupted: %w", err)
		}

		// Skip already-executed actions
		if result, exists := exec.ExecutedActions[actionName]; exists && result.UndoneAt == nil {
			continue
		}

		node := def.Actions[actionName]
		if node == nil {
			return fmt.Errorf("action %q not found in definition", actionName)
		}

		// Check that dependencies are satisfied
		for _, depName := range node.Dependencies {
			if _, exists := exec.ExecutedActions[depName]; !exists {
				return fmt.Errorf("action %q dependency %q not satisfied", actionName, depName)
			}
		}

		// Build inputs for this action
		actionInputs := newInputs(exec.InitialInputs, outputs)

		log.Info("executing action", "action", actionName)

		// Execute the action
		actionCtx := context.WithValue(ctx, actionNameCtxKey{}, actionName)
		output, err := node.Action.Execute(actionCtx, actionInputs)
		now := time.Now()

		if err != nil {
			log.Error("action failed", "action", actionName, "error", err)

			// Record the failure
			exec.ExecutedActions[actionName] = &ActionResult{
				ExecutedAt: now,
				Error:      err.Error(),
			}
			exec.ExecutionOrder = append(exec.ExecutionOrder, actionName)
			exec.Error = fmt.Sprintf("action %q failed: %v", actionName, err)
			exec.UpdatedAt = now

			if saveErr := e.storage.Save(ctx, exec); saveErr != nil {
				log.Error("failed to persist failure state", "error", saveErr)
			}

			// Set StatusUndoing before compensation so recovery knows to undo
			exec.Status = StatusUndoing
			exec.UpdatedAt = time.Now()
			if saveErr := e.storage.Save(ctx, exec); saveErr != nil {
				log.Error("failed to persist undoing state", "error", saveErr)
			}

			return e.runUndo(ctx, def, exec)
		}

		// Serialize output
		outputBytes, err := json.Marshal(output)
		if err != nil {
			log.Error("failed to serialize output", "action", actionName, "error", err)
			exec.Error = fmt.Sprintf("action %q output serialization failed: %v", actionName, err)
			exec.UpdatedAt = time.Now()

			// The action ran but output can't be persisted for later recovery.
			// Immediately undo this action with the in-memory output.
			if undoErr := node.Action.Undo(ctx, actionInputs, output); undoErr != nil {
				log.Error("undo failed after serialization error", "action", actionName, "error", undoErr)
				// Record the action even though undo failed, so runUndo can retry.
				// Output is nil since serialization failed.
				exec.ExecutedActions[actionName] = &ActionResult{
					ExecutedAt: now,
				}
				exec.ExecutionOrder = append(exec.ExecutionOrder, actionName)
			} else {
				// Record as executed and undone so runUndo skips it
				undoneAt := time.Now()
				exec.ExecutedActions[actionName] = &ActionResult{
					ExecutedAt: now,
					UndoneAt:   &undoneAt,
				}
				exec.ExecutionOrder = append(exec.ExecutionOrder, actionName)
			}

			if saveErr := e.storage.Save(ctx, exec); saveErr != nil {
				log.Error("failed to persist state after serialization error", "error", saveErr)
			}

			// Set StatusUndoing before compensation so recovery knows to undo
			exec.Status = StatusUndoing
			exec.UpdatedAt = time.Now()
			if saveErr := e.storage.Save(ctx, exec); saveErr != nil {
				log.Error("failed to persist undoing state", "error", saveErr)
			}

			return e.runUndo(ctx, def, exec)
		}

		// Record success
		exec.ExecutedActions[actionName] = &ActionResult{
			Output:     outputBytes,
			ExecutedAt: now,
		}
		exec.ExecutionOrder = append(exec.ExecutionOrder, actionName)
		exec.UpdatedAt = now

		// Persist after each action - if we can't durably record progress,
		// we must compensate to guarantee a clean terminal state.
		if err := e.storage.Save(ctx, exec); err != nil {
			log.Error("failed to persist action result, triggering undo", "action", actionName, "error", err)
			exec.Error = fmt.Sprintf("failed to persist action %q result: %v", actionName, err)

			// Try to set StatusUndoing before compensation (may fail if storage is down)
			exec.Status = StatusUndoing
			exec.UpdatedAt = time.Now()
			if saveErr := e.storage.Save(ctx, exec); saveErr != nil {
				log.Error("failed to persist undoing state", "error", saveErr)
			}

			return e.runUndo(ctx, def, exec)
		}

		// Add outputs to the map for subsequent actions
		if err := extractOutputs(node, outputBytes, outputs); err != nil {
			log.Warn("failed to extract outputs", "action", actionName, "error", err)
		}

		log.Info("action completed", "action", actionName)
	}

	// All actions completed successfully
	exec.Status = StatusCompleted
	exec.UpdatedAt = time.Now()
	if err := e.storage.Save(ctx, exec); err != nil {
		return fmt.Errorf("persisting completed state: %w", err)
	}

	log.Info("saga completed successfully")
	return nil
}

// runUndo rolls back completed actions in reverse order.
func (e *Executor) runUndo(ctx context.Context, def *Definition, exec *Execution) error {
	log := e.log.With("saga", def.Name, "execution", exec.ID)

	// Update status to undoing
	exec.Status = StatusUndoing
	exec.UpdatedAt = time.Now()
	if err := e.storage.Save(ctx, exec); err != nil {
		log.Error("failed to persist undoing state", "error", err)
	}

	// Inject dependencies, executor, and execution ID into context
	ctx = injectDependencies(ctx, def.dependencies)
	ctx = context.WithValue(ctx, executorCtxKey{}, e)
	ctx = context.WithValue(ctx, executionIDCtxKey{}, exec.ID)

	// Build outputs map from executed actions
	outputs := make(map[string]json.RawMessage)
	for actionName, result := range exec.ExecutedActions {
		if result.UndoneAt != nil {
			continue
		}
		node := def.Actions[actionName]
		if node == nil {
			continue
		}
		if err := extractOutputs(node, result.Output, outputs); err != nil {
			log.Warn("failed to extract outputs for undo", "action", actionName, "error", err)
		}
	}

	// Undo in reverse execution order
	var undoErrors []error
	for i := len(exec.ExecutionOrder) - 1; i >= 0; i-- {
		// Check for context cancellation between undos
		if err := ctx.Err(); err != nil {
			log.Info("context cancelled during undo, stopping", "error", err)
			// Leave saga in Undoing state - recovery will continue later
			return fmt.Errorf("saga undo interrupted: %w", err)
		}

		actionName := exec.ExecutionOrder[i]

		result, exists := exec.ExecutedActions[actionName]
		if !exists || result.UndoneAt != nil {
			continue // Already undone or never executed
		}

		// Skip actions that failed (they have an error and no output to undo)
		if result.Error != "" {
			log.Info("skipping undo for failed action", "action", actionName, "error", result.Error)
			continue
		}

		node := def.Actions[actionName]
		if node == nil {
			log.Warn("action not found in definition during undo", "action", actionName)
			continue
		}

		// Build inputs for undo
		actionInputs := newInputs(exec.InitialInputs, outputs)

		// Deserialize the output
		var output any
		if len(result.Output) > 0 {
			if err := json.Unmarshal(result.Output, &output); err != nil {
				log.Warn("failed to deserialize output for undo", "action", actionName, "error", err)
				undoErrors = append(undoErrors, fmt.Errorf("deserialize output for undo %q: %w", actionName, err))
				continue
			}
		}

		log.Info("undoing action", "action", actionName)

		// Execute undo
		actionCtx := context.WithValue(ctx, actionNameCtxKey{}, actionName)
		if err := node.Action.Undo(actionCtx, actionInputs, output); err != nil {
			log.Error("undo failed", "action", actionName, "error", err)
			undoErrors = append(undoErrors, fmt.Errorf("undo %q: %w", actionName, err))
			// Continue with other undos even on failure
			// Don't mark as undone - recovery should retry this action
			continue
		}

		// Record successful undo
		now := time.Now()
		result.UndoneAt = &now
		exec.UpdatedAt = now

		if err := e.storage.Save(ctx, exec); err != nil {
			log.Error("failed to persist undo state", "error", err)
		}

		log.Info("action undone", "action", actionName)
	}

	if len(undoErrors) > 0 {
		// Keep StatusUndoing so recovery can retry failed undos
		exec.UpdatedAt = time.Now()
		if err := e.storage.Save(ctx, exec); err != nil {
			log.Error("failed to persist undoing state", "error", err)
		}
		log.Info("saga undo incomplete, will retry on recovery", "undo_errors", len(undoErrors))
		return fmt.Errorf("saga failed with %d undo errors: %v", len(undoErrors), undoErrors)
	}

	// All undos succeeded - mark as failed (terminal state)
	exec.Status = StatusFailed
	exec.UpdatedAt = time.Now()
	if err := e.storage.Save(ctx, exec); err != nil {
		log.Error("failed to persist failed state", "error", err)
	}

	log.Info("saga failed and rolled back")
	return fmt.Errorf("saga failed: %s", exec.Error)
}

// Recover finds and resumes incomplete sagas after a restart.
func (e *Executor) Recover(ctx context.Context) error {
	incomplete, err := e.storage.ListIncomplete(ctx)
	if err != nil {
		return fmt.Errorf("listing incomplete sagas: %w", err)
	}

	var recoverErrors []error
	for _, exec := range incomplete {
		// Skip child executions — they will be driven by their parent's recovery
		// via RunNested. Recovering them independently would cause double-execution.
		if exec.ParentExecutionID != "" {
			e.log.Info("skipping child execution (will be recovered by parent)",
				"execution", exec.ID, "parent", exec.ParentExecutionID)
			continue
		}

		def, ok := e.registry.Get(exec.DefinitionName)
		if !ok {
			// Storage is shared across executors (e.g. build and sandbox each
			// run their own), but ListIncomplete returns every executor's
			// sagas. A definition we don't recognize belongs to a different
			// executor, which will recover it from its own registry. Skip
			// quietly rather than treating it as our recovery error.
			e.log.Debug("skipping saga with unregistered definition (owned by another executor)",
				"saga", exec.DefinitionName, "execution", exec.ID)
			continue
		}

		e.log.Info("recovering saga", "saga", exec.DefinitionName, "execution", exec.ID, "status", exec.Status)

		// Check version compatibility
		if def.Version != exec.DefinitionVersion {
			e.log.Warn("saga definition version mismatch",
				"saga", exec.DefinitionName,
				"execution_version", exec.DefinitionVersion,
				"current_version", def.Version)
		}

		switch exec.Status {
		case StatusPending, StatusRunning:
			// Check if there's a failed action that needs compensation.
			// This handles the edge case where we crashed after recording failure
			// but before persisting StatusUndoing.
			if exec.Error != "" {
				e.log.Info("found failed action during recovery, starting undo",
					"saga", exec.DefinitionName, "error", exec.Error)
				recoverErrors = append(recoverErrors, e.runUndo(ctx, def, exec))
			} else {
				// Resume execution (pending means crashed before first action started)
				if err := e.runExecution(ctx, def, exec); err != nil {
					recoverErrors = append(recoverErrors, err)
				}
			}
		case StatusUndoing:
			// Resume undo
			recoverErrors = append(recoverErrors, e.runUndo(ctx, def, exec))
		case StatusCompleted, StatusFailed:
			// Terminal states; nothing to recover.
		}
	}

	if len(recoverErrors) > 0 {
		return fmt.Errorf("recovery completed with %d errors", len(recoverErrors))
	}
	return nil
}

// extractOutputs extracts output key-value pairs from an action's output.
func extractOutputs(node *ActionNode, outputBytes []byte, outputs map[string]json.RawMessage) error {
	if len(outputBytes) == 0 {
		return nil
	}

	// Parse the output as a map to extract individual fields
	var outputMap map[string]json.RawMessage
	if err := json.Unmarshal(outputBytes, &outputMap); err != nil {
		// Not a map; nothing to extract into keyed outputs.
		return nil
	}

	// Map output fields to saga keys based on the action's output mappings
	typed, ok := node.Action.(*typedAction)
	if !ok {
		return nil
	}

	for _, mapping := range typed.outMappings {
		if mapping.isEdge {
			continue // Edge fields carry no data
		}
		// Look for the field in the output map using the JSON key name
		// (which accounts for json struct tags)
		if val, exists := outputMap[mapping.jsonKey]; exists {
			outputs[mapping.sagaKey] = val
		}
	}

	return nil
}

// ExecutionOutputs loads a completed execution from storage and collects its
// outputs into a NestedResult. Useful for reading saga results without a
// capture struct.
func (e *Executor) ExecutionOutputs(ctx context.Context, executionID string) (*NestedResult, error) {
	exec, err := e.storage.Get(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("loading execution %q: %w", executionID, err)
	}

	if exec.Status != StatusCompleted {
		return nil, fmt.Errorf("execution %q has status %q, expected %q", executionID, exec.Status, StatusCompleted)
	}

	def, ok := e.registry.Get(exec.DefinitionName)
	if !ok {
		return nil, fmt.Errorf("saga definition %q not found", exec.DefinitionName)
	}

	return collectOutputs(def, exec), nil
}

// collectOutputs gathers all action outputs from a completed execution into
// a NestedResult.
func collectOutputs(def *Definition, exec *Execution) *NestedResult {
	outputs := make(map[string]json.RawMessage)
	for actionName, result := range exec.ExecutedActions {
		if result.UndoneAt != nil {
			continue
		}
		node := def.Actions[actionName]
		if node == nil {
			continue
		}
		_ = extractOutputs(node, result.Output, outputs)
	}
	return &NestedResult{
		ExecutionID: exec.ID,
		outputs:     outputs,
	}
}

// Execution IDs follow the same shape as every other entity ID in the system:
// a kind namespace, then a name carrying a short mnemonic prefix and a
// separator before the base58 body, as in sandbox/sb-… and pool/…. Sagas used
// to mint a bare idgen.Gen("saga"), which produced sagaCdjkwhnn… with the word
// running straight into lowercase base58 and no kind namespace at all.
const (
	sagaIDKind = "saga"
	sagaIDName = "sg"
)

// generateID creates a unique execution ID.
func generateID() string {
	return sagaIDKind + "/" + idgen.GenNS(sagaIDName)
}
