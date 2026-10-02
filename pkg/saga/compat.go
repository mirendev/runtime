package saga

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrIncompatibleDefinition marks a refusal to drive an execution with a
// definition that cannot be trusted to finish or compensate it. Match it with
// errors.Is; errors.As on *IncompatibleDefinitionError gives the detail.
var ErrIncompatibleDefinition = errors.New("saga definition cannot resume this execution")

// IncompatibleDefinitionError says why an execution was refused. A refusal
// happens before any action runs or is undone and before the record is
// written, so the execution is left exactly as the previous binary left it.
type IncompatibleDefinitionError struct {
	ExecutionID     string
	Saga            string
	RecordedVersion int
	CurrentVersion  int
	ResumesFrom     []int

	// MissingActions ran under the recorded definition and are absent from
	// the current one, so nothing here could undo them.
	MissingActions []string
}

func (e *IncompatibleDefinitionError) Error() string {
	var reasons []string
	if e.RecordedVersion != e.CurrentVersion {
		reasons = append(reasons, fmt.Sprintf(
			"it was recorded at v%d and this binary's v%d does not declare it can resume that version (resumes from %v)",
			e.RecordedVersion, e.CurrentVersion, e.ResumesFrom))
	}
	if len(e.MissingActions) > 0 {
		reasons = append(reasons, fmt.Sprintf(
			"actions it already ran are no longer defined, so they cannot be compensated: %s",
			strings.Join(e.MissingActions, ", ")))
	}
	return fmt.Sprintf("refusing to resume saga %q execution %q: %s; no action was run or undone. "+
		"Running a release that can still resume it lets it finish or compensate normally; "+
		"`miren debug saga abandon` gives it up without compensating",
		e.Saga, e.ExecutionID, strings.Join(reasons, "; "))
}

func (e *IncompatibleDefinitionError) Is(target error) bool {
	return target == ErrIncompatibleDefinition
}

// checkResumable decides whether def may drive exec, before anything about
// exec is written. It checks facts rather than shapes: the version has to be
// one def has declared it can resume, and every action exec completed has to
// still exist so its undo is reachable. The second check applies even at a
// matching version, since a definition edited without a bump is exactly the
// case a version number cannot catch.
//
// An execution with nothing left to compensate and nothing in flight has no
// work a definition change could strand, so it is admitted and re-stamped at
// the current version. That is one that never started (Pending), or one
// already compensating whose every action failed or was undone. A Running
// execution with nothing recorded does not qualify: it may have crashed
// partway through an action, nested sagas included, whose work exists but
// was never recorded, and under a new version re-running "the first action"
// can mean running a different one.
//
// Admitting an execution clears any block a previous binary recorded on it.
// Every path that drives one goes through here, nested ones included, and
// each persists the cleared field with the status write that follows.
func checkResumable(def *Definition, exec *Execution) error {
	var live int
	var missing []string
	for name, result := range exec.ExecutedActions {
		// Undone or failed actions have nothing left to compensate.
		if result.UndoneAt != nil || result.Error != "" {
			continue
		}
		live++
		if _, ok := def.Actions[name]; !ok {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)

	if live == 0 && nothingInFlight(exec) {
		exec.DefinitionVersion = def.Version
		admit(exec)
		return nil
	}

	if def.CanResume(exec.DefinitionVersion) && len(missing) == 0 {
		admit(exec)
		return nil
	}

	return &IncompatibleDefinitionError{
		ExecutionID:     exec.ID,
		Saga:            def.Name,
		RecordedVersion: exec.DefinitionVersion,
		CurrentVersion:  def.Version,
		ResumesFrom:     def.ResumesFrom,
		MissingActions:  missing,
	}
}

// nothingInFlight reports whether no action of exec can be partway through.
// Pending means the run never started; Undoing, or a recorded failure, means
// forward progress already stopped at an action whose outcome was recorded.
func nothingInFlight(exec *Execution) bool {
	return exec.Status == StatusPending || exec.Status == StatusUndoing || exec.Error != ""
}

// admit clears a recorded block, remembering it so that a refusal further in,
// such as a nested saga's, can tell it is repeating itself.
func admit(exec *Execution) {
	if exec.BlockedReason != "" {
		exec.clearedBlock = exec.BlockedReason
		exec.BlockedReason = ""
	}
}

// recordBlocked persists why exec was refused, so `miren debug saga show`
// can say so and the stalled sweep knows to leave it alone.
//
// Only a changed reason is written and logged at Error. Named executions
// re-enter on every reconcile, and the record is what makes the repeat
// recognizable across restarts as well as within one process. UpdatedAt is
// left as it was: a refusal is not progress, and the record's age should
// still say when it last moved.
func (e *Executor) recordBlocked(ctx context.Context, exec *Execution, refusal error) {
	reason := refusal.Error()
	if exec.BlockedReason == reason {
		e.log.Debug("saga execution still blocked", "saga", exec.DefinitionName, "execution", exec.ID)
		return
	}

	// A parent blocked by a nested refusal is admitted, and so cleared, before
	// it reaches the child again, so the reason it carried is the one to match.
	if exec.clearedBlock == reason {
		e.log.Debug("saga execution still blocked", "saga", exec.DefinitionName, "execution", exec.ID)
	} else {
		e.log.Error("refusing to resume saga execution",
			"saga", exec.DefinitionName,
			"execution", exec.ID,
			"status", exec.Status,
			"reason", reason)
	}

	exec.BlockedReason = reason
	if err := e.storage.Save(ctx, exec); err != nil {
		e.log.Warn("failed to record why a saga execution is blocked",
			"saga", exec.DefinitionName, "execution", exec.ID, "error", err)
	}
}

// blockOnNested blocks exec on a refusal that came up from a nested saga,
// remembering which execution refused so checkNestedBlock can re-check it.
func (e *Executor) blockOnNested(ctx context.Context, exec *Execution, cause, refusal error) error {
	var inner *IncompatibleDefinitionError
	var undoBlocked *UndoBlockedError
	switch {
	case errors.As(cause, &inner):
		exec.BlockedOn = inner.ExecutionID
	case errors.As(cause, &undoBlocked):
		exec.BlockedOn = undoBlocked.ExecutionID
	}
	e.recordBlocked(ctx, exec, refusal)
	return refusal
}

// stillBlocked restates a block that has not cleared. It matches
// ErrIncompatibleDefinition so callers that retry a refusal, like the addon
// controller, keep retrying it.
type stillBlocked string

func (s stillBlocked) Error() string        { return string(s) }
func (s stillBlocked) Is(target error) bool { return target == ErrIncompatibleDefinition }

// checkNestedBlock keeps a parent from being driven while the nested
// execution that blocked it still cannot be resumed.
//
// The forward path stops before recording the action that ran the child, so
// driving the parent again re-runs that action from the top, and an action
// can do real work before it gets back to RunNested. The shared-server
// addon sagas look up, and past a staleness window delete, the very server
// their blocked child half built. Re-checking the child first means a
// parent whose child is still refused runs nothing at all.
//
// The block lifts once the child is resumable, terminal, or gone, and the
// parent then drives normally.
func (e *Executor) checkNestedBlock(ctx context.Context, exec *Execution) error {
	if exec.BlockedOn == "" {
		return nil
	}

	child, err := e.storage.Get(ctx, exec.BlockedOn)
	switch {
	case errors.Is(err, ErrExecutionNotFound):
		exec.BlockedOn = ""
		return nil
	case err != nil:
		return fmt.Errorf("re-checking nested execution %q that blocked %q: %w", exec.BlockedOn, exec.ID, err)
	case child.Status == StatusFailed,
		child.Status == StatusCompleted && child.BlockedReason == "":
		// Finished: compensated, abandoned, or done with nothing refused.
		// A completed child that is blocked is the one whose undo was
		// refused, so it falls through to the re-check like any other.
		exec.BlockedOn = ""
		return nil
	}

	// A child this build blocked on a failing undo is resumable as far as its
	// definition goes, and still will not be undone here.
	if e.undoBlockedHere(child) == nil {
		if def, ok := e.registry.Get(child.DefinitionName); ok && checkResumable(def, cloneExecution(child)) == nil {
			exec.BlockedOn = ""
			return nil
		}
	}

	// Nothing has been written since checkResumable admitted exec, so the
	// stored record still carries the reason it was blocked with. Putting
	// that back in memory is enough; saving would rewrite the whole record
	// on every retry for no change.
	if exec.clearedBlock != "" {
		exec.BlockedReason = exec.clearedBlock
		e.log.Debug("saga execution still blocked on a nested saga",
			"saga", exec.DefinitionName, "execution", exec.ID, "blocked_on", exec.BlockedOn)
		return stillBlocked(exec.clearedBlock)
	}

	reason := fmt.Sprintf("nested saga execution %q still cannot be resumed", exec.BlockedOn)
	e.recordBlocked(ctx, exec, stillBlocked(reason))
	return stillBlocked(reason)
}
