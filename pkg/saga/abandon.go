package saga

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotBlocked is returned when asked to abandon an execution nothing has
// refused to resume. Such an execution is either being driven or will be
// found by recovery or the stalled sweep, and giving it up by hand would skip
// a compensation that is still going to happen on its own.
var ErrNotBlocked = errors.New("saga execution is not blocked")

// Abandon gives up an execution a binary refused to resume, transitioning it
// to failed without running any undo. It returns the actions whose work was
// left in place, in the order they ran, so whoever abandoned it knows what to
// clean up by hand.
//
// This is the escape hatch for a refusal nobody can otherwise resolve. The
// intended path is a definition that declares it can resume older versions,
// or a release that still can, either of which lets the execution finish or
// compensate normally. A failed execution is handed back to the same rules
// the stalled sweep relies on: one named after its entity is cleared and
// retried by its next reconcile, and one with a generated name is collected
// by retention. Addon provisioning is the exception, since nothing drops its
// failed record: the parent unwinds and the association lands in error, from
// which the addon has to be destroyed and added again.
//
// A completed execution can be abandoned too when it is blocked, which
// happens to a nested child whose parent's undo was refused at it. Its work
// is still uncompensated and its parent is stuck waiting on it, so it moves
// to failed like any other abandoned execution, and UndoNested then treats it
// as done and lets the parent carry on unwinding.
func Abandon(exec *Execution, now time.Time) ([]string, error) {
	switch {
	case exec.Status == StatusFailed,
		exec.Status == StatusCompleted && exec.BlockedReason == "":
		return nil, fmt.Errorf("execution %q is already %s; there is nothing to abandon", exec.ID, exec.Status)
	}
	if exec.BlockedReason == "" {
		return nil, fmt.Errorf("%w: execution %q is %s and nothing has refused to resume it",
			ErrNotBlocked, exec.ID, exec.Status)
	}

	left := Uncompensated(exec)

	msg := "abandoned by an operator while it was blocked; compensation was skipped"
	if len(left) > 0 {
		msg += ", so work from these actions may remain: " + strings.Join(left, ", ")
	}
	exec.Error = msg + ". It had been blocked because: " + exec.BlockedReason

	exec.Status = StatusFailed
	exec.BlockedReason = ""
	exec.BlockedOn = ""
	exec.UpdatedAt = now
	return left, nil
}

// Uncompensated lists, in execution order, the actions that succeeded and
// have not been undone: the work an execution would have to compensate if it
// unwound now.
func Uncompensated(exec *Execution) []string {
	var left []string
	for _, name := range exec.ExecutionOrder {
		result, ok := exec.ExecutedActions[name]
		if !ok || result.UndoneAt != nil || result.Error != "" {
			continue
		}
		left = append(left, name)
	}
	return left
}

// SaveAtRevision saves exec only if the stored entity is still at revision,
// the one the caller read it at. It exists for operator writes from outside
// the executor, where a binary that resumed the execution in the meantime
// would otherwise have its progress overwritten by a stale copy.
func (s *EACStorage) SaveAtRevision(ctx context.Context, exec *Execution, revision int64) error {
	if revision <= 0 {
		return fmt.Errorf("saving execution %q: a revision is required", exec.ID)
	}

	ent, err := executionToEntity(exec)
	if err != nil {
		return err
	}

	// Replace rather than Put: Put falls back to creating the entity when it
	// is gone, and creation ignores the revision, so a record deleted since it
	// was read would come back. Replace fails on a missing entity instead.
	if _, err = s.eac.Replace(ctx, ent.Attrs(), revision); err != nil {
		return fmt.Errorf("saving saga entity via EAC at revision %d: %w", revision, err)
	}
	return nil
}
