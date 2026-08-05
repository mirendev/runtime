package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	saga_v1alpha "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

// EntityStorage implements Storage using the entity store.
type EntityStorage struct {
	store entity.Store
	log   *slog.Logger
}

// NewEntityStorage creates a storage backed by an entity store.
func NewEntityStorage(store entity.Store, log *slog.Logger) *EntityStorage {
	if log == nil {
		log = slog.Default()
	}
	return &EntityStorage{store: store, log: log}
}

// Save persists the execution state as an entity.
func (s *EntityStorage) Save(ctx context.Context, exec *Execution) error {
	// Serialize complex fields to JSON
	initialInputs, err := json.Marshal(exec.InitialInputs)
	if err != nil {
		return fmt.Errorf("marshaling initial inputs: %w", err)
	}

	executedActions, err := json.Marshal(exec.ExecutedActions)
	if err != nil {
		return fmt.Errorf("marshaling executed actions: %w", err)
	}

	executionOrder, err := json.Marshal(exec.ExecutionOrder)
	if err != nil {
		return fmt.Errorf("marshaling execution order: %w", err)
	}

	// Convert status
	status := statusToEntity(exec.Status)

	// Build saga entity
	sagaEntity := &saga_v1alpha.Saga{
		ID:                entity.Id(exec.ID),
		DefinitionName:    exec.DefinitionName,
		DefinitionVersion: int64(exec.DefinitionVersion),
		ParentExecutionId: entity.Id(exec.ParentExecutionID),
		Status:            status,
		InitialInputs:     initialInputs,
		ExecutedActions:   executedActions,
		ExecutionOrder:    executionOrder,
		Error:             exec.Error,
	}

	// Create or update the entity. EnsureEntity is create-if-absent and
	// does NOT apply our attributes when the entity already exists, so on
	// every save after the first we must explicitly replace. Without this,
	// the saga record stays frozen at its initial pending state and later
	// status/action-progress writes are silently dropped.
	ent := entity.New(
		entity.DBId, entity.Id(exec.ID),
		sagaEntity.Encode(),
	)

	_, created, err := s.store.EnsureEntity(ctx, ent)
	if err != nil {
		return fmt.Errorf("saving saga entity: %w", err)
	}
	if !created {
		if _, err := s.store.ReplaceEntity(ctx, ent); err != nil {
			return fmt.Errorf("updating saga entity: %w", err)
		}
	}

	return nil
}

// Get retrieves an execution by ID.
func (s *EntityStorage) Get(ctx context.Context, id string) (*Execution, error) {
	ent, err := s.store.GetEntity(ctx, entity.Id(id))
	if err != nil {
		if errors.Is(err, entity.ErrEntityNotFound) || errors.Is(err, cond.ErrNotFound{}) {
			return nil, fmt.Errorf("%w: %s", ErrExecutionNotFound, id)
		}
		return nil, fmt.Errorf("getting saga entity: %w", err)
	}

	sagaEntity, ok := entity.As[saga_v1alpha.Saga](ent)
	if !ok {
		return nil, fmt.Errorf("entity %s is not a saga", id)
	}

	return entityToExecution(sagaEntity)
}

// ListIncomplete returns all executions that need recovery.
func (s *EntityStorage) ListIncomplete(ctx context.Context) ([]*Execution, error) {
	// Query for pending sagas (crashed between initial save and status transition)
	pendingIds, err := s.store.ListIndex(ctx, entity.Ref(
		saga_v1alpha.SagaStatusId,
		saga_v1alpha.SagaStatusPendingId,
	))
	if err != nil {
		return nil, fmt.Errorf("listing pending sagas: %w", err)
	}

	// Query for running sagas
	runningIds, err := s.store.ListIndex(ctx, entity.Ref(
		saga_v1alpha.SagaStatusId,
		saga_v1alpha.SagaStatusRunningId,
	))
	if err != nil {
		return nil, fmt.Errorf("listing running sagas: %w", err)
	}

	// Query for undoing sagas
	undoingIds, err := s.store.ListIndex(ctx, entity.Ref(
		saga_v1alpha.SagaStatusId,
		saga_v1alpha.SagaStatusUndoingId,
	))
	if err != nil {
		return nil, fmt.Errorf("listing undoing sagas: %w", err)
	}

	// Combine IDs, deduplicating by ID. A saga can transiently appear under
	// more than one status index (e.g. a stale pending entry lingering after
	// the transition to running). Recovering the same execution twice causes
	// double execution: the second pass re-runs already-completed actions and
	// collides on entities the first pass created.
	seen := make(map[entity.Id]struct{})
	var allIds []entity.Id
	for _, ids := range [][]entity.Id{pendingIds, runningIds, undoingIds} {
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			allIds = append(allIds, id)
		}
	}
	if len(allIds) == 0 {
		return nil, nil
	}

	// Batch fetch all entities
	entities, err := s.store.GetEntities(ctx, allIds)
	if err != nil {
		return nil, fmt.Errorf("fetching saga entities: %w", err)
	}

	// Convert to executions
	var executions []*Execution
	for _, ent := range entities {
		if ent == nil {
			continue
		}
		sagaEntity, ok := entity.As[saga_v1alpha.Saga](ent)
		if !ok {
			s.log.Warn("entity is not a saga, skipping", "id", ent.Id())
			continue
		}
		exec, err := entityToExecution(sagaEntity)
		if err != nil {
			s.log.Warn("failed to convert saga entity, skipping", "id", ent.Id(), "error", err)
			continue
		}
		executions = append(executions, exec)
	}

	return executions, nil
}

// statusToEntity converts saga.Status to the entity enum value.
func statusToEntity(s Status) saga_v1alpha.SagaStatus {
	switch s {
	case StatusPending:
		return saga_v1alpha.PENDING
	case StatusRunning:
		return saga_v1alpha.RUNNING
	case StatusUndoing:
		return saga_v1alpha.UNDOING
	case StatusCompleted:
		return saga_v1alpha.COMPLETED
	case StatusFailed:
		return saga_v1alpha.FAILED
	default:
		return saga_v1alpha.PENDING
	}
}

// statusFromEntity converts the entity enum value to saga.Status.
func statusFromEntity(s saga_v1alpha.SagaStatus) Status {
	switch s {
	case saga_v1alpha.PENDING:
		return StatusPending
	case saga_v1alpha.RUNNING:
		return StatusRunning
	case saga_v1alpha.UNDOING:
		return StatusUndoing
	case saga_v1alpha.COMPLETED:
		return StatusCompleted
	case saga_v1alpha.FAILED:
		return StatusFailed
	default:
		return StatusPending
	}
}

// StatusIndexAttr returns the entity index attribute that selects executions in
// the given status, for callers querying the entity store directly rather than
// through a Storage (the CLI's `debug saga` commands). It reports false for an
// unrecognized status rather than guessing, since a wrong index silently
// returns the wrong sagas.
func StatusIndexAttr(s Status) (entity.Attr, bool) {
	var id entity.Id

	switch s {
	case StatusPending:
		id = saga_v1alpha.SagaStatusPendingId
	case StatusRunning:
		id = saga_v1alpha.SagaStatusRunningId
	case StatusUndoing:
		id = saga_v1alpha.SagaStatusUndoingId
	case StatusCompleted:
		id = saga_v1alpha.SagaStatusCompletedId
	case StatusFailed:
		id = saga_v1alpha.SagaStatusFailedId
	default:
		return entity.Attr{}, false
	}

	return entity.Ref(saga_v1alpha.SagaStatusId, id), true
}

// ExecutionFromEntity converts a decoded saga entity into an Execution,
// deserializing the JSON-encoded inputs, action results, and execution order.
// Exposed so tools that read saga entities directly (the CLI's `debug saga`
// commands) decode them the same way the executor does.
//
// Note that CreatedAt and UpdatedAt are not persisted on the entity and so are
// left zero here; callers that need them should read the entity store's own
// creation and update metadata.
func ExecutionFromEntity(sagaEntity *saga_v1alpha.Saga) (*Execution, error) {
	return entityToExecution(sagaEntity)
}

// entityToExecution converts a saga entity to an Execution.
func entityToExecution(sagaEntity *saga_v1alpha.Saga) (*Execution, error) {
	exec := &Execution{
		ID:                string(sagaEntity.ID),
		DefinitionName:    sagaEntity.DefinitionName,
		DefinitionVersion: int(sagaEntity.DefinitionVersion),
		ParentExecutionID: string(sagaEntity.ParentExecutionId),
		Status:            statusFromEntity(sagaEntity.Status),
		Error:             sagaEntity.Error,
	}

	// Deserialize initial inputs
	if len(sagaEntity.InitialInputs) > 0 {
		if err := json.Unmarshal(sagaEntity.InitialInputs, &exec.InitialInputs); err != nil {
			return nil, fmt.Errorf("unmarshaling initial inputs: %w", err)
		}
	} else {
		exec.InitialInputs = make(map[string]any)
	}

	// Deserialize executed actions
	if len(sagaEntity.ExecutedActions) > 0 {
		if err := json.Unmarshal(sagaEntity.ExecutedActions, &exec.ExecutedActions); err != nil {
			return nil, fmt.Errorf("unmarshaling executed actions: %w", err)
		}
	} else {
		exec.ExecutedActions = make(map[string]*ActionResult)
	}

	// Deserialize execution order
	if len(sagaEntity.ExecutionOrder) > 0 {
		if err := json.Unmarshal(sagaEntity.ExecutionOrder, &exec.ExecutionOrder); err != nil {
			return nil, fmt.Errorf("unmarshaling execution order: %w", err)
		}
	}

	return exec, nil
}
