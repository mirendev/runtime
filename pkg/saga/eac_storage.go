package saga

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	saga_v1alpha "miren.dev/runtime/api/saga/saga_v1alpha"
	"miren.dev/runtime/pkg/entity"

	es "miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
)

// EACStorage implements Storage using an EntityAccessClient RPC connection.
// This is used by runners which don't have direct entity.Store access.
type EACStorage struct {
	eac *es.EntityAccessClient
	log *slog.Logger
}

// NewEACStorage creates a storage backed by an EntityAccessClient.
func NewEACStorage(eac *es.EntityAccessClient, log *slog.Logger) *EACStorage {
	if log == nil {
		log = slog.Default()
	}
	return &EACStorage{eac: eac, log: log}
}

// Save persists the execution state as an entity via EAC.
func (s *EACStorage) Save(ctx context.Context, exec *Execution) error {
	ent, err := executionToEntity(exec)
	if err != nil {
		return err
	}

	// Put is an upsert (update-then-create): unlike Ensure, it applies our
	// attributes even when the entity already exists. Ensure is create-if-absent
	// and would silently drop every save after the first, freezing the saga at
	// its initial pending state.
	rpcEnt := &es.Entity{}
	rpcEnt.SetId(exec.ID)
	rpcEnt.SetAttrs(ent.Attrs())

	if _, err = s.eac.Put(ctx, rpcEnt); err != nil {
		return fmt.Errorf("saving saga entity via EAC: %w", err)
	}

	return nil
}

// Get retrieves an execution by ID via EAC.
func (s *EACStorage) Get(ctx context.Context, id string) (*Execution, error) {
	resp, err := s.eac.Get(ctx, id)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, fmt.Errorf("%w: %s", ErrExecutionNotFound, id)
		}
		return nil, fmt.Errorf("getting saga entity via EAC: %w", err)
	}

	ent := resp.Entity().Entity()
	sagaEntity, ok := entity.As[saga_v1alpha.Saga](ent)
	if !ok {
		return nil, fmt.Errorf("entity %s is not a saga", id)
	}

	return entityToExecution(sagaEntity)
}

// ListTerminalPage summarizes one bounded page of finished executions via EAC.
func (s *EACStorage) ListTerminalPage(ctx context.Context, q TerminalQuery) (*TerminalPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stage, inner, err := decodeStageCursor(q.Cursor, len(terminalStatuses))
	if err != nil {
		return nil, err
	}

	status := terminalStatuses[stage]

	resp, err := s.eac.ListPage(ctx, entity.Ref(saga_v1alpha.SagaStatusId, status), inner, int64(clampLimit(q.Limit)))
	if err != nil {
		return nil, fmt.Errorf("listing sagas with status %s: %w", status, err)
	}

	next := nextStageCursor(stage, resp.Cursor(), len(terminalStatuses))

	// An empty page is the shortest possible short page, not the end of the
	// walk. The server drops ids it could not resolve into entities, so a page
	// of nothing but stale index entries comes back with no values and a live
	// cursor into the rest of this index. Treating that as an exhausted index
	// would skip everything after it.
	if len(resp.Values()) == 0 {
		return &TerminalPage{Cursor: next}, nil
	}

	var result []TerminalExecution
	var staleNonTerminal int

	for _, v := range resp.Values() {
		summary, verdict := terminalSummary(v.Entity())
		switch verdict {
		case summaryOK:
			result = append(result, summary)
		case summaryWrongStatus:
			staleNonTerminal++
		case summaryNoTimestamp:
			s.log.Warn("terminal saga has no usable timestamp, skipping", "id", v.Id())
		}
	}

	logStaleTerminal(s.log, staleNonTerminal)

	return &TerminalPage{Executions: result, Cursor: next}, nil
}

// Delete removes a saga execution entity via EAC.
func (s *EACStorage) Delete(ctx context.Context, id string) error {
	if _, err := s.eac.Delete(ctx, id); err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil
		}
		return fmt.Errorf("deleting saga entity via EAC: %w", err)
	}
	return nil
}

// ListIncompletePage returns one bounded page of executions needing recovery
// via EAC.
//
// The RPC pins the ids and the entities of a page to one store revision, so the
// tear between listing an index and fetching what it named is closed on the
// server rather than guessed at here.
func (s *EACStorage) ListIncompletePage(ctx context.Context, q IncompleteQuery) (*IncompletePage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stage, inner, err := decodeStageCursor(q.Cursor, len(incompleteStatuses))
	if err != nil {
		return nil, err
	}

	status := incompleteStatuses[stage]

	resp, err := s.eac.ListPage(ctx, entity.Ref(saga_v1alpha.SagaStatusId, status), inner, int64(clampLimit(q.Limit)))
	if err != nil {
		return nil, fmt.Errorf("listing sagas with status %s: %w", status, err)
	}

	next := nextStageCursor(stage, resp.Cursor(), len(incompleteStatuses))

	// See ListTerminalPage: an empty page still carries a cursor, and dropping
	// it here would skip the rest of this status index and every saga in it
	// that still needs recovering.
	if len(resp.Values()) == 0 {
		return &IncompletePage{Cursor: next}, nil
	}

	var executions []*Execution
	var staleTerminal int

	for _, eacEnt := range resp.Values() {
		sagaEntity, ok := entity.As[saga_v1alpha.Saga](eacEnt.Entity())
		if !ok {
			s.log.Warn("entity is not a saga, skipping", "id", eacEnt.Id())
			continue
		}
		exec, err := entityToExecution(sagaEntity)
		if err != nil {
			s.log.Warn("failed to convert saga entity, skipping", "id", eacEnt.Id(), "error", err)
			continue
		}
		// The index selected this id, the entity says what it actually is, and
		// the entity wins.
		if isTerminal(exec.Status) {
			staleTerminal++
			continue
		}
		executions = append(executions, exec)
	}

	logStaleIncomplete(s.log, staleTerminal)

	return &IncompletePage{Executions: executions, Cursor: next}, nil
}

// ListIncompleteSummaryPage summarizes one bounded page of in-flight executions
// via EAC.
func (s *EACStorage) ListIncompleteSummaryPage(ctx context.Context, q IncompleteSummaryQuery) (*IncompleteSummaryPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stage, inner, err := decodeStageCursor(q.Cursor, len(incompleteStatuses))
	if err != nil {
		return nil, err
	}

	status := incompleteStatuses[stage]

	resp, err := s.eac.ListPage(ctx, entity.Ref(saga_v1alpha.SagaStatusId, status), inner, int64(clampLimit(q.Limit)))
	if err != nil {
		return nil, fmt.Errorf("listing sagas with status %s: %w", status, err)
	}

	next := nextStageCursor(stage, resp.Cursor(), len(incompleteStatuses))

	// See ListTerminalPage: an empty page still carries a cursor, and dropping
	// it here would skip the rest of this status index.
	if len(resp.Values()) == 0 {
		return &IncompleteSummaryPage{Cursor: next}, nil
	}

	var result []IncompleteSummary
	var staleTerminal int

	for _, v := range resp.Values() {
		summary, verdict := incompleteSummary(v.Entity())
		switch verdict {
		case summaryOK:
			result = append(result, summary)
		case summaryWrongStatus:
			staleTerminal++
		case summaryNoTimestamp:
			s.log.Warn("in-flight saga has no usable timestamp, skipping", "id", v.Id())
		}
	}

	logStaleIncomplete(s.log, staleTerminal)

	return &IncompleteSummaryPage{Executions: result, Cursor: next}, nil
}
