package saga

import (
	"context"
	"log/slog"
	"time"
)

// liveParentsOf returns the IDs of executions still in flight, but only when
// the terminal set actually contains a child. Most clusters run no nested
// sagas, and paying for an extra listing every sweep to answer a question
// nobody asked is not worth it.
//
// An in-flight parent is exactly ListIncomplete's set: the three non-terminal
// statuses and nothing else. A parent absent from the store entirely is safe,
// since nothing remains to re-find the child.
func liveParentsOf(ctx context.Context, storage Storage, terminal []TerminalExecution) (map[string]struct{}, error) {
	hasChild := false
	for _, exec := range terminal {
		if exec.ParentID != "" {
			hasChild = true
			break
		}
	}
	if !hasChild {
		return nil, nil
	}

	incomplete, err := storage.ListIncomplete(ctx)
	if err != nil {
		return nil, err
	}

	live := make(map[string]struct{}, len(incomplete))
	for _, exec := range incomplete {
		if exec != nil {
			live[exec.ID] = struct{}{}
		}
	}
	return live, nil
}

// RetentionConfig tunes a retention sweep.
type RetentionConfig struct {
	// Retention is how long a terminal execution is kept after it finished.
	// Zero disables deletion, which is the escape hatch if a cluster needs its
	// saga history frozen for an investigation.
	Retention time.Duration

	// MaxDeletes caps deletions in one sweep so an accumulated backlog drains
	// over several passes rather than one thundering herd of writes. Zero means
	// unbounded.
	MaxDeletes int
}

// RetentionResult reports what one sweep did.
type RetentionResult struct {
	// Scanned is how many terminal executions were considered.
	Scanned int

	// Deleted is how many were past the retention window and removed.
	Deleted int

	// Failed is how many deletions errored. A sweep does not abort on one bad
	// delete; the next pass retries it.
	Failed int

	// Skipped is how many expired executions were held back because they are
	// children of a saga still in flight. They become collectable as soon as
	// their parent reaches a terminal state.
	Skipped int

	// Capped reports that MaxDeletes stopped the sweep before it had inspected
	// every terminal execution. Callers should say so rather than let a
	// truncated sweep read as "everything is clean."
	//
	// It says the sweep did not finish looking, not that more deletions are
	// certain: the executions it never reached may all be inside the retention
	// window. Consuming the whole budget on the very last execution is a
	// complete sweep, not a capped one.
	Capped bool
}

// RunRetention deletes terminal executions that finished longer ago than the
// configured window, and reports what it did.
//
// The policy is one rule: a terminal execution expires on age, whether it
// succeeded or failed. Executions still in flight are never considered at any
// age, including undoing ones, which can legitimately sit unfinished for a long
// time while their undos keep failing and retrying. Those are exactly what
// recovery needs to find.
//
// The sweep is idempotent, so a caller that is interrupted or capped simply
// runs again.
//
// A nil log falls back to the default logger. Individual delete failures are
// logged rather than returned: one execution the store would not part with must
// not abandon the rest of the sweep, and the next pass retries it anyway. The
// caller only learns the count, so the ID has to be recorded here or an
// operator seeing "failed: 3" has nothing to go inspect.
func RunRetention(ctx context.Context, storage Storage, cfg RetentionConfig, log *slog.Logger) (*RetentionResult, error) {
	if log == nil {
		log = slog.Default()
	}

	result := &RetentionResult{}
	if cfg.Retention <= 0 {
		return result, nil
	}

	terminal, err := storage.ListTerminal(ctx)
	if err != nil {
		return result, err
	}

	liveParents, err := liveParentsOf(ctx, storage, terminal)
	if err != nil {
		return result, err
	}

	cutoff := time.Now().Add(-cfg.Retention)

	for i, exec := range terminal {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.Scanned++

		if exec.FinishedAt.After(cutoff) {
			continue
		}

		// A finished child whose parent is still in flight has to stay. The
		// parent does not re-run a nested saga on resume, it re-finds the child
		// by deterministic ID and reuses the result, so deleting the child
		// converts a resumed saga into a duplicated one.
		if exec.ParentID != "" {
			if _, live := liveParents[exec.ParentID]; live {
				result.Skipped++
				continue
			}
		}

		if err := storage.Delete(ctx, exec.ID); err != nil {
			log.Warn("failed to delete expired saga execution",
				"id", exec.ID, "finished_at", exec.FinishedAt, "error", err)
			result.Failed++
			continue
		}
		result.Deleted++

		if cfg.MaxDeletes > 0 && result.Deleted >= cfg.MaxDeletes {
			result.Capped = i < len(terminal)-1
			return result, nil
		}
	}

	return result, nil
}
