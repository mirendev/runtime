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

// executionToEntity serializes an execution into the entity representation
// shared by every Storage backend. Both backends encode the identical entity
// and differ only in how they write it, so the encoding lives here: when the
// two drifted apart before, one of them silently stopped persisting saves
// (MIR-441) and only a conformance suite caught it.
func executionToEntity(exec *Execution) (*entity.Entity, error) {
	initialInputs, err := json.Marshal(exec.InitialInputs)
	if err != nil {
		return nil, fmt.Errorf("marshaling initial inputs: %w", err)
	}

	executedActions, err := json.Marshal(exec.ExecutedActions)
	if err != nil {
		return nil, fmt.Errorf("marshaling executed actions: %w", err)
	}

	executionOrder, err := json.Marshal(exec.ExecutionOrder)
	if err != nil {
		return nil, fmt.Errorf("marshaling execution order: %w", err)
	}

	sagaEntity := &saga_v1alpha.Saga{
		ID:                entity.Id(exec.ID),
		DefinitionName:    exec.DefinitionName,
		DefinitionVersion: int64(exec.DefinitionVersion),
		ParentExecutionId: entity.Id(exec.ParentExecutionID),
		Status:            statusToEntity(exec.Status),
		InitialInputs:     initialInputs,
		ExecutedActions:   executedActions,
		ExecutionOrder:    executionOrder,
		Error:             exec.Error,
		CreatedAt:         exec.CreatedAt,
		UpdatedAt:         exec.UpdatedAt,
	}

	return entity.New(
		entity.DBId, entity.Id(exec.ID),
		sagaEntity.Encode(),
	), nil
}

// Save persists the execution state as an entity.
func (s *EntityStorage) Save(ctx context.Context, exec *Execution) error {
	ent, err := executionToEntity(exec)
	if err != nil {
		return err
	}

	// Create or update the entity. EnsureEntity is create-if-absent and
	// does NOT apply our attributes when the entity already exists, so on
	// every save after the first we must explicitly replace. Without this,
	// the saga record stays frozen at its initial pending state and later
	// status/action-progress writes are silently dropped.
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

// terminalFetchBatch is how many entities ListTerminal materializes at a time.
// Every entity carries its action-output blobs, so a store holding a large
// backlog must not be pulled into memory all at once just to read timestamps
// off it.
//
// This is not redundant with the batching GetEntities already does internally.
// That one chunks the etcd transaction ops and then still allocates and returns
// the full slice, so it bounds request size, not memory. This outer loop is
// what keeps peak memory flat.
const terminalFetchBatch = 200

// ListTerminal summarizes every execution in a terminal state.
//
// The store's index lookups are equality-only, so there is no range query over
// a timestamp that would let us ask for expired executions directly. We list
// the terminal set (IDs only, which stays cheap) and read each one's finish
// time, fetching in batches and keeping only the summary.
func (s *EntityStorage) ListTerminal(ctx context.Context) ([]TerminalExecution, error) {
	var ids []entity.Id
	seen := make(map[entity.Id]struct{})

	for _, status := range []entity.Id{
		saga_v1alpha.SagaStatusCompletedId,
		saga_v1alpha.SagaStatusFailedId,
	} {
		statusIds, err := s.store.ListIndex(ctx, entity.Ref(saga_v1alpha.SagaStatusId, status))
		if err != nil {
			return nil, fmt.Errorf("listing terminal sagas: %w", err)
		}
		// Deduplicate for the same reason recovery does: an execution can
		// transiently appear under more than one status index.
		for _, id := range statusIds {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}

	var result []TerminalExecution
	for start := 0; start < len(ids); start += terminalFetchBatch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		end := min(start+terminalFetchBatch, len(ids))
		entities, err := s.store.GetEntities(ctx, ids[start:end])
		if err != nil {
			return nil, fmt.Errorf("fetching terminal sagas: %w", err)
		}

		for _, ent := range entities {
			if ent == nil {
				// Deleted between the listing and the fetch. Nothing to report.
				continue
			}
			summary, ok := terminalSummary(ent)
			if !ok {
				s.log.Warn("terminal saga has no usable timestamp, skipping", "id", ent.Id())
				continue
			}
			result = append(result, summary)
		}
	}

	return result, nil
}

// Delete removes a saga execution entity.
func (s *EntityStorage) Delete(ctx context.Context, id string) error {
	if err := s.store.DeleteEntity(ctx, entity.Id(id)); err != nil {
		if errors.Is(err, entity.ErrEntityNotFound) || errors.Is(err, cond.ErrNotFound{}) {
			return nil
		}
		return fmt.Errorf("deleting saga entity: %w", err)
	}
	return nil
}

// terminalSummary reduces a saga entity to what retention needs, reporting
// false when the entity carries no usable timestamp at all.
//
// The finish time prefers the saga's own updated_at but falls back to the
// entity store's system timestamp, which is what makes retention safe across an
// upgrade: every execution written before saga timestamps were persisted reads
// back with a zero updated_at, and treating that as infinitely old would delete
// a saga a runner on the old binary finished seconds ago.
func terminalSummary(ent *entity.Entity) (TerminalExecution, bool) {
	var s saga_v1alpha.Saga
	s.Decode(ent)

	summary := TerminalExecution{
		ID:       string(ent.Id()),
		ParentID: string(s.ParentExecutionId),
	}

	switch {
	case !s.UpdatedAt.IsZero():
		summary.FinishedAt = s.UpdatedAt
	case !ent.GetUpdatedAt().IsZero():
		summary.FinishedAt = ent.GetUpdatedAt()
	case !ent.GetCreatedAt().IsZero():
		summary.FinishedAt = ent.GetCreatedAt()
	default:
		return TerminalExecution{}, false
	}

	return summary, true
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

// entityToExecution converts a saga entity to an Execution.
func entityToExecution(sagaEntity *saga_v1alpha.Saga) (*Execution, error) {
	exec := &Execution{
		ID:                string(sagaEntity.ID),
		DefinitionName:    sagaEntity.DefinitionName,
		DefinitionVersion: int(sagaEntity.DefinitionVersion),
		ParentExecutionID: string(sagaEntity.ParentExecutionId),
		Status:            statusFromEntity(sagaEntity.Status),
		Error:             sagaEntity.Error,
		CreatedAt:         sagaEntity.CreatedAt,
		UpdatedAt:         sagaEntity.UpdatedAt,
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
