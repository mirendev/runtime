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

// ListTerminal summarizes every execution in a terminal state via EAC.
func (s *EACStorage) ListTerminal(ctx context.Context) ([]TerminalExecution, error) {
	var result []TerminalExecution
	seen := make(map[string]struct{})

	for _, status := range []entity.Id{
		saga_v1alpha.SagaStatusCompletedId,
		saga_v1alpha.SagaStatusFailedId,
	} {
		resp, err := s.eac.List(ctx, entity.Ref(saga_v1alpha.SagaStatusId, status))
		if err != nil {
			return nil, fmt.Errorf("listing sagas with status %s: %w", status, err)
		}
		for _, v := range resp.Values() {
			id := v.Id()
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}

			summary, ok := terminalSummary(v.Entity())
			if !ok {
				s.log.Warn("terminal saga has no usable timestamp, skipping", "id", id)
				continue
			}
			result = append(result, summary)
		}
	}

	return result, nil
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

// ListIncomplete returns all executions that need recovery via EAC.
func (s *EACStorage) ListIncomplete(ctx context.Context) ([]*Execution, error) {
	// Query for each incomplete status
	var allEntities []*es.Entity
	seen := make(map[string]struct{})

	for _, statusRef := range []entity.Id{
		saga_v1alpha.SagaStatusPendingId,
		saga_v1alpha.SagaStatusRunningId,
		saga_v1alpha.SagaStatusUndoingId,
	} {
		resp, err := s.eac.List(ctx, entity.Ref(
			saga_v1alpha.SagaStatusId,
			statusRef,
		))
		if err != nil {
			return nil, fmt.Errorf("listing sagas with status %s: %w", statusRef, err)
		}
		// Deduplicate by ID: a saga can appear under more than one status
		// index (e.g. a stale pending entry after transitioning to running),
		// and recovering the same execution twice causes double execution.
		for _, v := range resp.Values() {
			id := v.Id()
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			allEntities = append(allEntities, v)
		}
	}

	if len(allEntities) == 0 {
		return nil, nil
	}

	var executions []*Execution
	for _, eacEnt := range allEntities {
		ent := eacEnt.Entity()
		sagaEntity, ok := entity.As[saga_v1alpha.Saga](ent)
		if !ok {
			s.log.Warn("entity is not a saga, skipping", "id", eacEnt.Id())
			continue
		}
		exec, err := entityToExecution(sagaEntity)
		if err != nil {
			s.log.Warn("failed to convert saga entity, skipping", "id", eacEnt.Id(), "error", err)
			continue
		}
		executions = append(executions, exec)
	}

	return executions, nil
}
