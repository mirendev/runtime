package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
		RecoveryScope:     exec.RecoveryScope,
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

// ListIncompletePage returns one bounded page of executions needing recovery.
//
// The three incomplete status indexes are walked one after another rather than
// merged, because merging would mean holding all three id sets to deduplicate
// across them, which is the allocation this is here to avoid. A cursor names
// which index it is in, so a page never straddles two.
//
// Nothing deduplicates across indexes as a result. An execution listed under
// two statuses at once is a stale index entry, and the caller is the right
// place to notice: recovery refuses to drive an execution twice through its
// own claim, and retention's deletes are idempotent by contract.
func (s *EntityStorage) ListIncompletePage(ctx context.Context, q IncompleteQuery) (*IncompletePage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stage, inner, err := decodeStageCursor(q.Cursor, len(incompleteStatuses))
	if err != nil {
		return nil, err
	}

	attr := entity.Ref(saga_v1alpha.SagaStatusId, incompleteStatuses[stage])

	page, err := s.store.ListIndexEntitiesPage(ctx, attr, inner, int64(clampLimit(q.Limit)))
	if err != nil {
		return nil, fmt.Errorf("listing sagas with status %s: %w", incompleteStatuses[stage], err)
	}

	next := nextStageCursor(stage, page.Cursor, len(incompleteStatuses))

	// Empty is a short page, not an ending, exactly as it is for EACStorage.
	// This backend cannot produce an empty page with a live cursor, since a
	// dropped id holds its place as a nil and an empty id list means an empty
	// cursor, but the two backends answer one contract and should not read
	// differently.
	if len(page.Entities) == 0 {
		return &IncompletePage{Cursor: next}, nil
	}

	executions, stale := s.decodeIncomplete(page.Entities)
	logStaleIncomplete(s.log, stale)

	return &IncompletePage{Executions: executions, Cursor: next}, nil
}

// decodeIncomplete converts a page of entities into executions, dropping the
// ones whose decoded status says they are already finished and counting them.
//
// The index is a hint and the entity is the answer. An entry can outlive the
// status it was written for, and a page is two reads, so the status that
// selected an id can be out of date by the time its entity arrives.
func (s *EntityStorage) decodeIncomplete(entities []*entity.Entity) ([]*Execution, int) {
	var executions []*Execution
	var stale int

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
		if isTerminal(exec.Status) {
			stale++
			continue
		}
		executions = append(executions, exec)
	}

	return executions, stale
}

// ListIncompleteSummaryPage summarizes one bounded page of in-flight
// executions.
//
// Same indexes and cursor encoding as ListIncompletePage, keeping a summary
// rather than a whole execution. Its caller walks every in-flight execution in
// the cluster, and doing that with full payloads would cost what recovery costs
// without recovering anything.
func (s *EntityStorage) ListIncompleteSummaryPage(ctx context.Context, q IncompleteSummaryQuery) (*IncompleteSummaryPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stage, inner, err := decodeStageCursor(q.Cursor, len(incompleteStatuses))
	if err != nil {
		return nil, err
	}

	attr := entity.Ref(saga_v1alpha.SagaStatusId, incompleteStatuses[stage])

	page, err := s.store.ListIndexEntitiesPage(ctx, attr, inner, int64(clampLimit(q.Limit)))
	if err != nil {
		return nil, fmt.Errorf("listing sagas with status %s: %w", incompleteStatuses[stage], err)
	}

	next := nextStageCursor(stage, page.Cursor, len(incompleteStatuses))

	if len(page.Entities) == 0 {
		return &IncompleteSummaryPage{Cursor: next}, nil
	}

	var result []IncompleteSummary
	var staleTerminal int

	for _, ent := range page.Entities {
		if ent == nil {
			// Deleted between the listing and the fetch. Nothing to summarize.
			continue
		}
		summary, verdict := incompleteSummary(ent)
		switch verdict {
		case summaryOK:
			result = append(result, summary)
		case summaryWrongStatus:
			staleTerminal++
		case summaryNoTimestamp:
			s.log.Warn("in-flight saga has no usable timestamp, skipping", "id", ent.Id())
		}
	}

	logStaleIncomplete(s.log, staleTerminal)

	return &IncompleteSummaryPage{Executions: result, Cursor: next}, nil
}

// ListTerminalPage summarizes one bounded page of finished executions.
//
// The store's index lookups are equality-only, so there is no range query over
// a timestamp that would let us ask for expired executions directly. We walk
// the terminal indexes and read each execution's finish time, keeping only the
// summary.
func (s *EntityStorage) ListTerminalPage(ctx context.Context, q TerminalQuery) (*TerminalPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stage, inner, err := decodeStageCursor(q.Cursor, len(terminalStatuses))
	if err != nil {
		return nil, err
	}

	attr := entity.Ref(saga_v1alpha.SagaStatusId, terminalStatuses[stage])

	page, err := s.store.ListIndexEntitiesPage(ctx, attr, inner, int64(clampLimit(q.Limit)))
	if err != nil {
		return nil, fmt.Errorf("listing terminal sagas: %w", err)
	}

	next := nextStageCursor(stage, page.Cursor, len(terminalStatuses))

	if len(page.Entities) == 0 {
		return &TerminalPage{Cursor: next}, nil
	}

	var result []TerminalExecution
	var staleNonTerminal int

	for _, ent := range page.Entities {
		if ent == nil {
			// Deleted between the listing and the fetch. Nothing to report.
			continue
		}
		summary, verdict := terminalSummary(ent)
		switch verdict {
		case summaryOK:
			result = append(result, summary)
		case summaryWrongStatus:
			staleNonTerminal++
		case summaryNoTimestamp:
			s.log.Warn("terminal saga has no usable timestamp, skipping", "id", ent.Id())
		}
	}

	logStaleTerminal(s.log, staleNonTerminal)

	return &TerminalPage{Executions: result, Cursor: next}, nil
}

// ForceFailed transitions an in-flight execution to failed, and reports whether
// it did.
//
// The decision lives here rather than in the caller because the write has to be
// conditional on the read it was made against, and only this layer sees the
// revision. A caller that read, decided, then asked us to save could overwrite
// a runner that resumed the saga in the gap, replacing its status and action
// outputs with a stale copy marked failed.
//
// So the transition is refused, without error, whenever the record moved: gone,
// already terminal, touched since cutoff, or a changed revision. Each means
// something else is dealing with it, and the next pass looks again.
//
// The age test goes through lastChanged rather than the execution's updated_at,
// because a record written before v0.14.0 carries its age only on the entity.
func (s *EntityStorage) ForceFailed(ctx context.Context, id string, cutoff time.Time, reason string) (bool, error) {
	ent, err := s.store.GetEntity(ctx, entity.Id(id))
	if err != nil {
		if errors.Is(err, entity.ErrEntityNotFound) || errors.Is(err, cond.ErrNotFound{}) {
			return false, nil
		}
		return false, fmt.Errorf("getting saga entity: %w", err)
	}

	sagaEntity, ok := entity.As[saga_v1alpha.Saga](ent)
	if !ok {
		return false, fmt.Errorf("entity %s is not a saga", id)
	}

	if isTerminal(statusFromEntity(sagaEntity.Status)) {
		return false, nil
	}

	changed, ok := lastChanged(ent, sagaEntity)
	if !ok || changed.After(cutoff) {
		return false, nil
	}

	exec, err := entityToExecution(sagaEntity)
	if err != nil {
		return false, fmt.Errorf("decoding execution %q: %w", id, err)
	}

	exec.Status = StatusFailed
	exec.Error = reason
	exec.UpdatedAt = time.Now()

	forced, err := executionToEntity(exec)
	if err != nil {
		return false, err
	}

	if _, err := s.store.ReplaceEntity(ctx, forced, entity.WithFromRevision(ent.GetRevision())); err != nil {
		if errors.Is(err, cond.ErrConflict{}) {
			return false, nil
		}
		return false, fmt.Errorf("forcing execution %q to failed: %w", id, err)
	}

	return true, nil
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

// summaryVerdict says why a summary read rejected an entity. The two reasons
// want different handling: stale index entries arrive in bulk, while a missing
// timestamp is a one-off worth naming the entity for.
type summaryVerdict int

const (
	summaryOK summaryVerdict = iota
	// summaryWrongStatus means the decoded status is not the kind of status the
	// index that produced this entity is for, so that entry is stale. Both
	// directions land here: a terminal execution under a pending index, and a
	// running one under a completed index.
	summaryWrongStatus
	// summaryNoTimestamp means the entity carries no usable timestamp.
	summaryNoTimestamp
)

// lastChanged resolves when an execution last changed state.
//
// It prefers the saga's own updated_at and falls back to the entity store's
// system timestamps, which is what makes both sweeps safe across an upgrade:
// v0.11.1's schema had neither field, so its executions read back with a zero
// saga timestamp, and treating that as infinitely old would collect a saga
// finished seconds ago or force one that is actively running.
//
// The false return is not a "just use now". An execution we cannot date is one
// we must not act on, and both callers skip it and say so.
func lastChanged(ent *entity.Entity, s *saga_v1alpha.Saga) (time.Time, bool) {
	switch {
	case !s.UpdatedAt.IsZero():
		return s.UpdatedAt, true
	case !ent.GetUpdatedAt().IsZero():
		return ent.GetUpdatedAt(), true
	case !ent.GetCreatedAt().IsZero():
		return ent.GetCreatedAt(), true
	default:
		return time.Time{}, false
	}
}

// terminalSummary reduces a saga entity to what retention needs.
//
// The status index is a hint, not the truth: an entry can outlive the status it
// was written for, so the decoded status decides whether this is terminal.
func terminalSummary(ent *entity.Entity) (TerminalExecution, summaryVerdict) {
	var s saga_v1alpha.Saga
	s.Decode(ent)

	if !isTerminal(statusFromEntity(s.Status)) {
		return TerminalExecution{}, summaryWrongStatus
	}

	finished, ok := lastChanged(ent, &s)
	if !ok {
		return TerminalExecution{}, summaryNoTimestamp
	}

	return TerminalExecution{
		ID:         string(ent.Id()),
		ParentID:   string(s.ParentExecutionId),
		FinishedAt: finished,
	}, summaryOK
}

// incompleteSummary reduces a saga entity to what the stalled sweep needs, and
// is terminalSummary's mirror image in every respect including which way it
// distrusts the index.
func incompleteSummary(ent *entity.Entity) (IncompleteSummary, summaryVerdict) {
	var s saga_v1alpha.Saga
	s.Decode(ent)

	status := statusFromEntity(s.Status)
	if isTerminal(status) {
		return IncompleteSummary{}, summaryWrongStatus
	}

	changed, ok := lastChanged(ent, &s)
	if !ok {
		return IncompleteSummary{}, summaryNoTimestamp
	}

	return IncompleteSummary{
		ID:          string(ent.Id()),
		Status:      status,
		ParentID:    string(s.ParentExecutionId),
		LastChanged: changed,
	}, summaryOK
}

// isTerminal reports whether a status is one of the two finished states.
// ListIncomplete and ListTerminal both need it, in opposite directions.
func isTerminal(s Status) bool {
	return s == StatusCompleted || s == StatusFailed
}

// logStaleIncomplete and logStaleTerminal report index drift once per call
// rather than once per entry: the backlog runs to tens of thousands, and a
// per-entry line would drown the tier it logs in.
func logStaleIncomplete(log *slog.Logger, count int) {
	if count > 0 {
		log.Warn("incomplete status index returned terminal executions, skipping; index needs repair",
			"count", count)
	}
}

func logStaleTerminal(log *slog.Logger, count int) {
	if count > 0 {
		log.Warn("terminal status index returned non-terminal executions, skipping; index needs repair",
			"count", count)
	}
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
		RecoveryScope:     sagaEntity.RecoveryScope,
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
