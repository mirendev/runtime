package saga

import (
	"context"
	"errors"
	"fmt"
	"time"

	"miren.dev/runtime/version"
)

// An undo that fails is retried on the execution's next recovery, and that
// used to be the whole policy. A failure that can never succeed then retried
// on every restart forever, and since each attempt refreshes the record, the
// stalled sweep never saw it go quiet. MIR-2007 was one doing that for four
// weeks before an age alert noticed.
//
// So an undo that has failed enough times, for long enough, is blocked on the
// build that gave up on it. That build stops retrying it, and the execution
// waits, visibly blocked and out of the stalled sweep's reach, for one of two
// things. A different build tries the undo once more, because a deploy is the
// usual way a permanent undo failure gets fixed. Or an operator abandons it.
//
// Both thresholds have to be met. A short outage can fail several attempts in
// a second as reconciles retry, and blocking on that would park work a
// moment's patience would finish. A slow retry cadence can take days to reach
// a count, and the count is what says it was tried more than once.
const (
	undoBlockAttempts = 3
	undoBlockAfter    = time.Hour
)

// buildIdentity names a build for undo blocking. The version string alone
// isn't enough: a release branch stamps every build with the same version, so
// a patch release carrying the fix would look like the one that gave up and
// never retry. The commit is what tells two builds apart.
func buildIdentity(info version.Info) string {
	if commit := info.KnownCommit(); commit != "" {
		return info.Version + "@" + commit
	}
	return info.Version
}

// ErrUndoBlocked matches an UndoBlockedError.
var ErrUndoBlocked = errors.New("saga undo is blocked")

// UndoBlockedError reports an execution held back because one of its undos
// kept failing and the running build already gave up on it.
type UndoBlockedError struct {
	ExecutionID string
	Action      string
	Attempts    int
	Since       time.Time
	Build       string
	Err         string
}

func (e *UndoBlockedError) Error() string {
	return fmt.Sprintf(
		"undo of %q in saga execution %q has failed %d times since %s, and build %s will not retry it; "+
			"a different build will try once more, or abandon it with `miren debug saga abandon`: %s",
		e.Action, e.ExecutionID, e.Attempts, e.Since.UTC().Format(time.RFC3339), e.Build, e.Err)
}

func (e *UndoBlockedError) Is(target error) bool { return target == ErrUndoBlocked }

// blockFailingUndo blocks exec on this build if any undo has now failed past
// both thresholds, and returns why. Nothing is saved; runUndo's own save after
// the failed pass carries it.
func (e *Executor) blockFailingUndo(exec *Execution, now time.Time) *UndoBlockedError {
	var blocked *UndoBlockedError
	for _, name := range exec.ExecutionOrder {
		result, ok := exec.ExecutedActions[name]
		if !ok || result.UndoneAt != nil || result.Error != "" || result.UndoFailingSince == nil {
			continue
		}
		if result.UndoAttempts < undoBlockAttempts || now.Sub(*result.UndoFailingSince) < undoBlockAfter {
			continue
		}
		result.UndoBlockedBuild = e.build
		if blocked == nil {
			blocked = undoBlockedError(exec, name, result)
		}
	}
	if blocked != nil {
		exec.BlockedReason = blocked.Error()
	}
	return blocked
}

// holdUndoBlock keeps an execution this build blocked from being undone
// again. Every path into runUndo admits the execution first, and admitting
// clears the block in memory, so this puts it back before anything is saved.
//
// A block a different build placed is left cleared: this build gets its one
// attempt, and blockFailingUndo puts the block back under this build if that
// attempt fails too.
func (e *Executor) holdUndoBlock(ctx context.Context, exec *Execution) error {
	blocked := e.undoBlockedHere(exec)
	if blocked == nil {
		if action, build := BlockedUndo(exec); action != "" {
			e.log.Info("retrying an undo an earlier build gave up on",
				"saga", exec.DefinitionName, "execution", exec.ID,
				"action", action, "blocked_by", build, "build", e.build)
		}
		return nil
	}

	reason := blocked.Error()
	switch {
	case exec.BlockedReason == reason:
	case exec.clearedBlock == reason:
		exec.BlockedReason = reason
	default:
		// The stored reason says something else, or nothing. Write this one
		// so the record and `debug saga show` agree on why it is waiting.
		exec.BlockedReason = reason
		if err := e.storage.Save(ctx, exec); err != nil {
			e.log.Warn("failed to record why a saga execution is blocked",
				"saga", exec.DefinitionName, "execution", exec.ID, "error", err)
		}
	}

	e.log.Debug("saga undo still blocked on this build",
		"saga", exec.DefinitionName, "execution", exec.ID, "action", blocked.Action, "build", e.build)
	return blocked
}

// undoBlockedHere reports the first undo in exec that this build gave up on
// and that still has not been done.
func (e *Executor) undoBlockedHere(exec *Execution) *UndoBlockedError {
	for _, name := range exec.ExecutionOrder {
		result, ok := exec.ExecutedActions[name]
		if !ok || result.UndoneAt != nil || result.UndoBlockedBuild != e.build {
			continue
		}
		return undoBlockedError(exec, name, result)
	}
	return nil
}

// BlockedUndo names an undo in exec that a build gave up on, and that build,
// or returns empty strings when no undo is blocked.
func BlockedUndo(exec *Execution) (action, build string) {
	for _, name := range exec.ExecutionOrder {
		result, ok := exec.ExecutedActions[name]
		if ok && result.UndoneAt == nil && result.UndoBlockedBuild != "" {
			return name, result.UndoBlockedBuild
		}
	}
	return "", ""
}

func undoBlockedError(exec *Execution, action string, result *ActionResult) *UndoBlockedError {
	blocked := &UndoBlockedError{
		ExecutionID: exec.ID,
		Action:      action,
		Attempts:    result.UndoAttempts,
		Build:       result.UndoBlockedBuild,
		Err:         result.UndoError,
	}
	if result.UndoFailingSince != nil {
		blocked.Since = *result.UndoFailingSince
	}
	return blocked
}
