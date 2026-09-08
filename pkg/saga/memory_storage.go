package saga

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// MemoryStorage is a simple in-memory storage implementation for testing and examples.
type MemoryStorage struct {
	mu         sync.Mutex
	executions map[string]*Execution
}

// NewMemoryStorage creates a new in-memory storage.
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		executions: make(map[string]*Execution),
	}
}

// Save persists an execution to memory.
func (m *MemoryStorage) Save(ctx context.Context, exec *Execution) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Deep copy to simulate real storage behavior
	copied := *exec
	copied.ExecutedActions = make(map[string]*ActionResult)
	for k, v := range exec.ExecutedActions {
		copiedResult := *v
		copied.ExecutedActions[k] = &copiedResult
	}
	copied.ExecutionOrder = make([]string, len(exec.ExecutionOrder))
	copy(copied.ExecutionOrder, exec.ExecutionOrder)

	m.executions[exec.ID] = &copied
	return nil
}

// Get retrieves an execution by ID.
func (m *MemoryStorage) Get(ctx context.Context, id string) (*Execution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	exec, ok := m.executions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrExecutionNotFound, id)
	}
	return exec, nil
}

// ListIncompletePage returns one bounded page of executions needing recovery:
// pending (crashed before starting), running, and undoing.
//
// The map has no order of its own, so the ids are sorted to give paging a
// stable sequence to resume in. That costs O(n) per page, which is fine for a
// backend that holds everything in memory anyway and is the price of behaving
// like the durable backends for the tests that exercise all three.
func (m *MemoryStorage) ListIncompletePage(ctx context.Context, q IncompleteQuery) (*IncompletePage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var ids []string
	for id, exec := range m.executions {
		switch exec.Status {
		case StatusPending, StatusRunning, StatusUndoing:
			ids = append(ids, id)
		case StatusCompleted, StatusFailed:
			// Terminal states are complete; skip them.
		}
	}

	ids, next := pageSortedIDs(ids, q.Cursor, clampLimit(q.Limit))

	executions := make([]*Execution, 0, len(ids))
	for _, id := range ids {
		executions = append(executions, m.executions[id])
	}

	return &IncompletePage{Executions: executions, Cursor: next}, nil
}

// ListTerminalPage summarizes one bounded page of finished executions.
func (m *MemoryStorage) ListTerminalPage(ctx context.Context, q TerminalQuery) (*TerminalPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var ids []string
	for id, exec := range m.executions {
		switch exec.Status {
		case StatusCompleted, StatusFailed:
			ids = append(ids, id)
		case StatusPending, StatusRunning, StatusUndoing:
			// Still in flight; retention does not apply.
		}
	}

	ids, next := pageSortedIDs(ids, q.Cursor, clampLimit(q.Limit))

	result := make([]TerminalExecution, 0, len(ids))
	for _, id := range ids {
		exec := m.executions[id]
		result = append(result, TerminalExecution{
			ID:         exec.ID,
			FinishedAt: exec.UpdatedAt,
			ParentID:   exec.ParentExecutionID,
		})
	}

	return &TerminalPage{Executions: result, Cursor: next}, nil
}

// pageSortedIDs slices one page out of an unordered id set, returning the page
// and the cursor that resumes after it.
//
// The cursor is the last id in the page, and is set only when something
// actually follows it. Handing one back merely because the page filled would
// give the caller a cursor to nothing every time the set ends exactly on a page
// boundary, and the caller cannot tell that from a real continuation without
// making the extra round trip that paging exists to avoid.
func pageSortedIDs(ids []string, cursor string, limit int) ([]string, string) {
	slices.Sort(ids)

	if cursor != "" {
		idx, _ := slices.BinarySearch(ids, cursor)
		for idx < len(ids) && ids[idx] <= cursor {
			idx++
		}
		ids = ids[idx:]
	}

	if len(ids) > limit {
		return ids[:limit], ids[limit-1]
	}

	return ids, ""
}

// Delete removes an execution. Deleting a missing execution is a no-op.
func (m *MemoryStorage) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.executions, id)
	return nil
}
