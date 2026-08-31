package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/pkg/idgen"
)

// ErrExecutionNotFound is returned by Storage.Get when no execution exists for the given ID.
var ErrExecutionNotFound = errors.New("execution not found")

// ErrExecutionInProgress reports that this Executor is already driving the
// named execution, so this call did nothing. It is not a failure and it is not
// success: the work is still going, and the caller should come back for the
// answer rather than read outputs that have not been written yet.
var ErrExecutionInProgress = errors.New("execution already in progress")

// ErrRecoveryScopeMismatch reports that an executor tried to continue a
// durable execution owned by a different recovery scope. The caller must not
// run any of that execution's actions against its own local resources.
var ErrRecoveryScopeMismatch = errors.New("execution recovery scope mismatch")

type executorCtxKey struct{}
type executionIDCtxKey struct{}
type actionNameCtxKey struct{}
type controlContextCtxKey struct{}
type dedicatedActionContextCtxKey struct{}

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

func controlContextFromContext(ctx context.Context) context.Context {
	controlCtx, ok := ctx.Value(controlContextCtxKey{}).(context.Context)
	if !ok {
		return ctx
	}
	return controlCtx
}

func hasDedicatedActionContext(ctx context.Context) bool {
	dedicated, _ := ctx.Value(dedicatedActionContextCtxKey{}).(bool)
	return dedicated
}

// Storage persists saga execution state.
type Storage interface {
	// Save persists the execution state.
	Save(ctx context.Context, exec *Execution) error

	// Get retrieves an execution by ID.
	Get(ctx context.Context, id string) (*Execution, error)

	// ListIncomplete returns all executions that need recovery (Pending, Running, or Undoing).
	ListIncomplete(ctx context.Context) ([]*Execution, error)

	// ListTerminal returns a summary of every execution that has finished
	// (Completed or Failed). It deliberately returns summaries rather than
	// executions: retention only needs an ID and an age, and a backend holding
	// a six-figure backlog must not have to materialize every action-output
	// blob to answer.
	ListTerminal(ctx context.Context) ([]TerminalExecution, error)

	// Delete removes an execution. Deleting one that is already gone is not an
	// error, so a retried or overlapping sweep converges instead of failing.
	Delete(ctx context.Context, id string) error
}

// Executor orchestrates saga execution with durable logging.
type Executor struct {
	storage       Storage
	registry      *Registry
	log           *slog.Logger
	recoveryScope string

	// inFlight names the executions this Executor is currently driving. A
	// caller that names its execution after the entity it belongs to will
	// re-enter Execute on every reconcile pass, and without this the second
	// caller would drive the same execution concurrently with the first.
	//
	// The scope really is this value, not the process: callers that build an
	// Executor per operation get an empty map each time and so get nothing from
	// this. Serializing across them is the caller's problem, and for addon and
	// sandbox work it is already solved a level up, by a reconcile controller
	// that handles one event per entity at a time. Anything driving an
	// execution from outside such a loop needs its own answer, and a durable
	// one (a lease or a compare-and-swap on the record) if it wants to hold
	// across processes rather than within one.
	inFlightMu sync.Mutex
	inFlight   map[string]struct{}
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

// WithRecoveryScope gives this executor a stable recovery identity. New
// executions persist the scope, startup recovery only considers exact scope
// matches, and named re-entry refuses to resume another scope's execution.
// The zero value leaves the executor unscoped and preserves existing behavior.
func WithRecoveryScope(scope string) ExecutorOption {
	return func(e *Executor) {
		e.recoveryScope = scope
	}
}

// NewExecutor creates an executor with the given storage and options.
func NewExecutor(storage Storage, opts ...ExecutorOption) *Executor {
	e := &Executor{
		storage:  storage,
		registry: globalRegistry,
		log:      slog.Default(),
		inFlight: make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// claim marks an execution as being driven by this Executor, reporting false if
// this Executor is already driving it.
func (e *Executor) claim(id string) bool {
	e.inFlightMu.Lock()
	defer e.inFlightMu.Unlock()

	if _, busy := e.inFlight[id]; busy {
		return false
	}
	e.inFlight[id] = struct{}{}
	return true
}

func (e *Executor) release(id string) {
	e.inFlightMu.Lock()
	defer e.inFlightMu.Unlock()
	delete(e.inFlight, id)
}

// scopeForExisting checks a named execution before this executor claims it.
// A scoped executor may adopt an old unscoped record through ordinary Execute:
// its caller has already routed the operation to this executor. Recover never
// adopts because every executor can see the shared incomplete set at startup.
func (e *Executor) scopeForExisting(exec *Execution) (adopt bool, err error) {
	switch {
	case exec.RecoveryScope == e.recoveryScope:
		return false, nil
	case exec.RecoveryScope == "" && e.recoveryScope != "":
		return true, nil
	default:
		return false, fmt.Errorf("%w: execution %q has scope %q, executor has scope %q",
			ErrRecoveryScopeMismatch, exec.ID, exec.RecoveryScope, e.recoveryScope)
	}
}

// StartBuilder provides a fluent API for starting saga executions.
type StartBuilder struct {
	executor  *Executor
	defName   string
	inputs    map[string]any
	id        string
	actionCtx context.Context
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
//
// Naming an execution makes Execute idempotent under that name: a second call
// continues the existing run rather than starting a new one. The inputs given
// here are the bootstrap data for the first call only. A later call that
// supplies different ones resumes from what the first recorded and ignores
// them, because the actions that already ran did so against the originals and
// re-deriving half a saga from new inputs would produce a run that never
// happened.
func (sb *StartBuilder) WithID(id string) *StartBuilder {
	sb.id = id
	return sb
}

// WithActionContext gives actions a cancellation boundary separate from the
// executor's control context. Persistence and compensation continue on the
// control context passed to Execute. The default is to use that same context
// for both, preserving existing saga behavior.
func (sb *StartBuilder) WithActionContext(ctx context.Context) *StartBuilder {
	sb.actionCtx = ctx
	return sb
}

// Execute runs the saga to completion or failure.
func (sb *StartBuilder) Execute(ctx context.Context) error {
	return sb.executor.execute(ctx, sb.actionCtx, sb.defName, sb.inputs, sb.id)
}

// execute runs a saga with the given definition and inputs.
func (e *Executor) execute(ctx, actionCtx context.Context, defName string, inputs map[string]any, id string) error {
	if actionCtx == nil {
		actionCtx = ctx
	} else {
		actionCtx = context.WithValue(actionCtx, dedicatedActionContextCtxKey{}, true)
	}
	// Look up definition
	def, ok := e.registry.Get(defName)
	if !ok {
		return fmt.Errorf("saga definition %q not found", defName)
	}

	// Generate ID if not provided
	if id == "" {
		id = generateID()
	}

	// Read and scope-check an existing record before claiming it. Recovery scope
	// is a safety boundary, not a local concurrency guard, so a mismatched
	// executor must not claim or execute the record.
	var existing *Execution
	var adoptScope bool
	loaded, err := e.storage.Get(ctx, id)
	switch {
	case err == nil:
		existing = loaded
		adoptScope, err = e.scopeForExisting(existing)
		if err != nil {
			return err
		}
	case errors.Is(err, ErrExecutionNotFound):
		// A new execution is created below after taking the local claim.
	default:
		return fmt.Errorf("loading execution %q: %w", id, err)
	}

	// A caller that names its execution after the entity it belongs to calls
	// this on every reconcile pass. Only one of those may drive at a time.
	if !e.claim(id) {
		e.log.Debug("execution already being driven here, skipping",
			"saga", defName, "execution", id)
		return ErrExecutionInProgress
	}
	defer e.release(id)

	// Naming an execution that already exists means continue it, not replace
	// it. Overwriting would discard the record of what a previous attempt
	// built, leaving those resources with nothing that knows to undo them.
	if existing != nil {
		if existing.DefinitionName != def.Name {
			return fmt.Errorf("execution %q belongs to saga %q, not %q", existing.ID, existing.DefinitionName, def.Name)
		}
		if adoptScope {
			existing.RecoveryScope = e.recoveryScope
			existing.UpdatedAt = time.Now()
			if err := e.storage.Save(ctx, existing); err != nil {
				return fmt.Errorf("persisting recovery scope for execution %q: %w", id, err)
			}
			e.log.Info("adopted legacy unscoped execution",
				"saga", defName, "execution", id, "recovery_scope", e.recoveryScope)
		}
		e.log.Info("continuing existing execution",
			"saga", defName, "execution", id, "status", existing.Status)
		return e.resumeWithActionContext(ctx, actionCtx, def, existing)
	}

	// Create execution
	now := time.Now()
	exec := &Execution{
		ID:                id,
		DefinitionName:    defName,
		DefinitionVersion: def.Version,
		InitialInputs:     inputs,
		RecoveryScope:     e.recoveryScope,
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

	return e.runExecution(ctx, actionCtx, def, exec)
}

// runExecution executes or resumes a saga.
func (e *Executor) runExecution(ctx, actionCtx context.Context, def *Definition, exec *Execution) error {
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
	actionCtx = injectDependencies(actionCtx, def.dependencies)
	actionCtx = context.WithValue(actionCtx, executorCtxKey{}, e)
	actionCtx = context.WithValue(actionCtx, executionIDCtxKey{}, exec.ID)
	actionCtx = context.WithValue(actionCtx, controlContextCtxKey{}, ctx)

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

		// Execute the action. Cancellation of a dedicated action context is a
		// normal saga failure, even if it lands between steps. Do not let an
		// action that ignores its context mutate more state after cancellation.
		currentActionCtx := context.WithValue(actionCtx, actionNameCtxKey{}, actionName)
		var output any
		var err error
		if actionErr := currentActionCtx.Err(); actionErr != nil {
			err = actionErr
		} else {
			output, err = node.Action.Execute(currentActionCtx, actionInputs)
		}
		now := time.Now()

		if err != nil {
			actionErr := currentActionCtx.Err()
			if hasDedicatedActionContext(currentActionCtx) && actionErr != nil && errors.Is(err, actionErr) {
				log.Info("action cancelled, starting compensation", "action", actionName, "error", err)
			} else {
				log.Error("action failed", "action", actionName, "error", err)
			}

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

		// An action may ignore cancellation and still return success after mutating
		// state. Its output must be durable before we compensate it. Only a
		// dedicated action context takes this path; cancellation of the shared
		// control context keeps the historical interruption-and-recovery behavior.
		if hasDedicatedActionContext(currentActionCtx) {
			if actionErr := currentActionCtx.Err(); actionErr != nil {
				log.Info("action completed after cancellation, starting compensation", "action", actionName, "error", actionErr)
				exec.Error = fmt.Sprintf("action %q completed after cancellation: %v", actionName, actionErr)
				exec.Status = StatusUndoing
				exec.UpdatedAt = time.Now()
				if saveErr := e.storage.Save(ctx, exec); saveErr != nil {
					log.Error("failed to persist undoing state", "error", saveErr)
				}
				return e.runUndo(ctx, def, exec)
			}
		}

		// Add outputs to the map for subsequent actions
		if err := extractOutputs(node, outputBytes, outputs); err != nil {
			log.Warn("failed to extract outputs", "action", actionName, "error", err)
		}

		log.Info("action completed", "action", actionName)
	}

	// A resumed execution may skip every already-persisted action, so the
	// per-action cancellation checkpoint above never runs. Do not let that path
	// turn a cancelled deployment into a completed saga with live side effects.
	if hasDedicatedActionContext(actionCtx) {
		if actionErr := actionCtx.Err(); actionErr != nil {
			log.Info("action context cancelled before completion, starting compensation", "error", actionErr)
			exec.Error = fmt.Sprintf("action context cancelled before completion: %v", actionErr)
			exec.Status = StatusUndoing
			exec.UpdatedAt = time.Now()
			if saveErr := e.storage.Save(ctx, exec); saveErr != nil {
				log.Error("failed to persist undoing state", "error", saveErr)
			}
			return e.runUndo(ctx, def, exec)
		}
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
		// The shared store contains every runner's incomplete sandbox sagas.
		// Scope is checked before even taking the local claim so this executor
		// cannot drive another runner's record. Empty is an exact value here,
		// not a wildcard; legacy records are adopted later by routed Execute.
		if exec.RecoveryScope != e.recoveryScope {
			e.log.Debug("skipping saga outside this recovery scope",
				"saga", exec.DefinitionName,
				"execution", exec.ID,
				"execution_scope", exec.RecoveryScope,
				"executor_scope", e.recoveryScope)
			continue
		}

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

		if !e.claim(exec.ID) {
			e.log.Debug("execution already being driven here, leaving it alone",
				"saga", exec.DefinitionName, "execution", exec.ID)
			continue
		}
		// Released through defer so an action that panics cannot strand the
		// claim. A stranded one is permanent: every later Execute under that
		// name would report the work as still in flight and never run it.
		err := func() error {
			defer e.release(exec.ID)
			return e.resume(ctx, def, exec)
		}()

		if err != nil {
			recoverErrors = append(recoverErrors, err)
		}
	}

	if len(recoverErrors) > 0 {
		return fmt.Errorf("recovery completed with %d errors", len(recoverErrors))
	}
	return nil
}

// resume continues an execution from wherever it stopped. Both Recover and a
// re-entered Execute go through here, so the two cannot drift apart on what
// "continue this" means for a given status.
//
// A caller that reaches a Failed execution gets the recorded failure back
// rather than a silent success: the saga tried, compensated, and gave up, and
// retrying that is a decision for whoever owns the operation, not something to
// do implicitly under the same name.
func (e *Executor) resume(ctx context.Context, def *Definition, exec *Execution) error {
	return e.resumeWithActionContext(ctx, ctx, def, exec)
}

func (e *Executor) resumeWithActionContext(ctx, actionCtx context.Context, def *Definition, exec *Execution) error {
	// A name that means one saga to the caller and another to the record is an
	// id collision, and resuming across it would run this definition's actions
	// against the other's recorded outputs. Naming executions after entities
	// makes collisions the thing worth guarding, so this is an error rather
	// than the warning a version skew gets.
	if def.Name != exec.DefinitionName {
		return fmt.Errorf("execution %q belongs to saga %q, not %q",
			exec.ID, exec.DefinitionName, def.Name)
	}

	if def.Version != exec.DefinitionVersion {
		e.log.Warn("saga definition version mismatch",
			"saga", exec.DefinitionName,
			"execution_version", exec.DefinitionVersion,
			"current_version", def.Version)
	}

	switch exec.Status {
	case StatusPending, StatusRunning:
		// A recorded error with a non-terminal status means we crashed after
		// noting the failure but before persisting StatusUndoing.
		if exec.Error != "" {
			e.log.Info("found failed action, starting undo",
				"saga", exec.DefinitionName, "error", exec.Error)
			return e.runUndo(ctx, def, exec)
		}
		return e.runExecution(ctx, actionCtx, def, exec)
	case StatusUndoing:
		return e.runUndo(ctx, def, exec)
	case StatusCompleted:
		return nil
	case StatusFailed:
		return fmt.Errorf("saga failed: %s", exec.Error)
	}

	// This whole change exists because a value nobody listed fell through a
	// switch in silence, so not here. An unrecognized status is a corrupt
	// record or one written by a version that knows a state this one does not,
	// and resuming is not something we can claim to have done either way.
	return fmt.Errorf("execution %q has unrecognized status %q", exec.ID, exec.Status)
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
