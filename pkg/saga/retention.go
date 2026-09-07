package saga

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// parentLiveness answers whether a terminal execution's parent is still in
// flight, one parent at a time and remembering what it learned.
//
// This used to be a second full listing of the incomplete set, taken whenever
// any execution in the terminal set had a parent. That asked the store for
// every in-flight execution in the cluster in order to answer a question about
// at most a page's worth of parents, and on a large backlog it was the second
// unbounded read in a sweep that already had one.
//
// Reading each parent directly costs a round trip per distinct parent instead.
// Most clusters run no nested sagas at all, so most sweeps ask nothing; and
// where they do, children of one parent share the answer.
type parentLiveness struct {
	storage Storage
	live    map[string]bool
}

func newParentLiveness(storage Storage) *parentLiveness {
	return &parentLiveness{storage: storage, live: map[string]bool{}}
}

// isLive reports whether the named parent is still in flight.
//
// A parent that is absent from the store is not live: nothing remains that
// could re-find the child. A parent that cannot be read is treated as live,
// which is the safe direction. Protecting a child that did not need it costs
// one more sweep; deleting one that did turns a resumed saga into a duplicated
// one.
func (p *parentLiveness) isLive(ctx context.Context, id string, log *slog.Logger) bool {
	if live, known := p.live[id]; known {
		return live
	}

	exec, err := p.storage.Get(ctx, id)
	switch {
	case errors.Is(err, ErrExecutionNotFound):
		p.live[id] = false
	case err != nil:
		log.Warn("could not read saga parent, keeping its children for now",
			"parent", id, "error", err)
		p.live[id] = true
	default:
		p.live[id] = !isTerminal(exec.Status)
	}

	return p.live[id]
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

	parents := newParentLiveness(storage)
	cutoff := time.Now().Add(-cfg.Retention)

	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		page, err := storage.ListTerminalPage(ctx, TerminalQuery{Cursor: cursor})
		if err != nil {
			return result, err
		}

		for _, exec := range page.Executions {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			result.Scanned++

			if exec.FinishedAt.After(cutoff) {
				continue
			}

			// A finished child whose parent is still in flight has to stay. The
			// parent does not re-run a nested saga on resume, it re-finds the
			// child by deterministic ID and reuses the result, so deleting the
			// child converts a resumed saga into a duplicated one.
			if exec.ParentID != "" && parents.isLive(ctx, exec.ParentID, log) {
				result.Skipped++
				continue
			}

			if err := storage.Delete(ctx, exec.ID); err != nil {
				log.Warn("failed to delete expired saga execution",
					"id", exec.ID, "finished_at", exec.FinishedAt, "error", err)
				result.Failed++
				continue
			}
			result.Deleted++

			if cfg.MaxDeletes > 0 && result.Deleted >= cfg.MaxDeletes {
				capped, err := stoppedEarly(ctx, storage, page, exec.ID)
				if err != nil {
					return result, err
				}
				result.Capped = capped
				return result, nil
			}
		}

		cursor = page.Cursor
		if cursor == "" {
			return result, nil
		}
	}
}

// stoppedEarly reports whether a sweep that just spent its delete budget left
// anything uninspected.
//
// Spending the budget is not the same as being cut short. A sweep that deletes
// its last permitted execution and has nothing left to look at did the whole
// job, and reporting that as capped would have an operator chasing a backlog
// that is not there.
//
// Deciding takes a look ahead, because the cursor alone cannot answer it. The
// walk covers several status indexes in sequence, so a cursor can point at the
// head of a next index that turns out to be empty, and a page can come back
// empty while its cursor still has an index behind it. So the lookahead reads
// forward until it finds something or the walk genuinely ends, rather than
// reading one page and concluding from an empty one. It runs at all only in the
// exact case where the budget landed on a page boundary.
func stoppedEarly(ctx context.Context, storage Storage, page *TerminalPage, deletedID string) (bool, error) {
	if len(page.Executions) == 0 {
		return false, nil
	}

	// Anything after it in the page it stopped in is already unlooked-at.
	if deletedID != page.Executions[len(page.Executions)-1].ID {
		return true, nil
	}

	cursor := page.Cursor
	for cursor != "" {
		if err := ctx.Err(); err != nil {
			return false, err
		}

		next, err := storage.ListTerminalPage(ctx, TerminalQuery{Cursor: cursor, Limit: 1})
		if err != nil {
			return false, err
		}
		if len(next.Executions) > 0 {
			return true, nil
		}
		cursor = next.Cursor
	}

	return false, nil
}
