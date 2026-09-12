package saga

import (
	"context"
	"log/slog"
	"time"
)

// StalledError is recorded on a forced execution, so it stays distinguishable
// from one that genuinely ran and failed.
const StalledError = "forced to failed by the stalled-saga sweep: this execution " +
	"had not changed state for longer than the stall window, so nothing was " +
	"driving it and nothing would have collected it"

// StalledStorage is what the sweep needs of a storage: less than Storage, plus
// a write conditional on the record not having changed since it was read.
//
// Separate from Storage because EACStorage cannot honour it. The entity-access
// put RPC returns a revision but does not accept one, so a conditional write is
// not expressible over it. Narrowing the parameter is what makes the sweep's
// coordinator-only reach a compile error rather than a comment.
type StalledStorage interface {
	Get(ctx context.Context, id string) (*Execution, error)

	ListIncompleteSummaryPage(ctx context.Context, q IncompleteSummaryQuery) (*IncompleteSummaryPage, error)

	// ForceFailed transitions an in-flight execution to failed if it is still
	// in flight and still untouched since cutoff, and reports whether it did.
	//
	// Deciding and writing are one operation on purpose: a runner resuming an
	// aged saga in the gap between them would lose its progress to a stale copy.
	ForceFailed(ctx context.Context, id string, cutoff time.Time, reason string) (bool, error)
}

// StalledConfig tunes a stalled-saga sweep.
type StalledConfig struct {
	// StaleAfter is how long an execution may sit without changing state before
	// the sweep declares it stranded. Every transition and action completion
	// writes a timestamp, so this measures being driven rather than guessing at
	// it. Zero disables the sweep.
	StaleAfter time.Duration

	// MaxForces caps transitions in one sweep so a backlog drains over several
	// passes rather than one thundering herd of writes. Zero means unbounded.
	MaxForces int
}

// StalledResult reports what one sweep did.
type StalledResult struct {
	// Scanned is how many in-flight executions were considered.
	Scanned int

	// Forced is how many were past the stall window and transitioned to failed.
	Forced int

	// Failed is how many transitions errored. A sweep does not abort on one bad
	// write; the next pass retries it.
	Failed int

	// Skipped is how many stalled executions were held back because they are
	// children of a saga still in flight. They become forceable as soon as
	// their parent stops being live.
	Skipped int

	// Recovered is how many refused the transition because they had moved on
	// between the page that named them and the write. Not an error; a lot of
	// them means the sweep is acting on pages that are too old.
	Recovered int

	// Capped reports that MaxForces stopped the sweep before it had inspected
	// everything, not that more transitions are certain.
	Capped bool
}

// RunStalledSweep forces stranded executions to failed, and reports what it did.
//
// An execution can end up in a state where two independent things are true:
// nothing will ever resume it, and nothing will ever delete it. Convergence
// works by name, so an execution with a generated name has nobody who will ever
// reconstruct that name to continue it; and RunRetention deliberately collects
// only terminal executions, because a pending or undoing one is exactly what
// recovery is supposed to find. Between those two rules a record sits in the
// store forever, and every recovery pass pays to read it (MIR-1788).
//
// Transitioning rather than deleting hands the record back to rules that
// already exist, without having to tell the two kinds apart: one named after
// its entity is cleared by the next reconcile's DropIfFailed and retried, one
// with a generated name is collected by retention a window later. That retry is
// real work on a live cluster, which is why the window is generous and why a
// sweep that forces anything logs it.
//
// The sweep selects candidates but decides nothing: a page is a snapshot, so
// ForceFailed re-decides against the record as it is when it writes. It is
// idempotent, so a caller that is interrupted or capped runs again. A nil log
// falls back to the default logger.
func RunStalledSweep(ctx context.Context, storage StalledStorage, cfg StalledConfig, log *slog.Logger) (*StalledResult, error) {
	if log == nil {
		log = slog.Default()
	}

	result := &StalledResult{}
	if cfg.StaleAfter <= 0 {
		return result, nil
	}

	parents := newParentLiveness(storage)
	cutoff := time.Now().Add(-cfg.StaleAfter)

	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		page, err := storage.ListIncompleteSummaryPage(ctx, IncompleteSummaryQuery{Cursor: cursor})
		if err != nil {
			return result, err
		}

		for _, summary := range page.Executions {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			result.Scanned++

			if summary.LastChanged.After(cutoff) {
				continue
			}

			// A stalled child whose parent is still in flight has to stay: the
			// parent re-finds its children by ID rather than re-running them,
			// so failing one under a live parent fails the parent for a reason
			// that was never true. A stranded parent is non-terminal and so
			// reads as live, which means a stranded tree converges at least one
			// level per sweep. Often more: a parent forced earlier in the same
			// walk is already terminal by the time its child is read.
			if summary.ParentID != "" && parents.isLive(ctx, summary.ParentID, log) {
				result.Skipped++
				continue
			}

			forced, err := storage.ForceFailed(ctx, summary.ID, cutoff, StalledError)
			switch {
			case err != nil:
				log.Warn("failed to force stalled saga execution",
					"id", summary.ID, "status", summary.Status,
					"last_changed", summary.LastChanged, "error", err)
				result.Failed++
				continue
			case !forced:
				result.Recovered++
				continue
			}

			// Debug rather than Info: draining a backlog puts a full sweep's
			// budget of these into the tier an operator reads, every tick, for
			// hours. Retention logs per record only when a delete fails, and
			// the sweep's summary carries the count worth noticing. The id is
			// one -v away.
			log.Debug("forced stalled saga execution to failed",
				"id", summary.ID, "status", summary.Status,
				"last_changed", summary.LastChanged)
			result.Forced++

			if cfg.MaxForces > 0 && result.Forced >= cfg.MaxForces {
				capped, err := stoppedEarlyStalled(ctx, storage, page, summary.ID)
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

// stoppedEarlyStalled is stoppedEarly for the in-flight walk. See there for why
// the lookahead reads forward rather than trusting one empty page.
func stoppedEarlyStalled(ctx context.Context, storage StalledStorage, page *IncompleteSummaryPage, forcedID string) (bool, error) {
	if len(page.Executions) == 0 {
		return false, nil
	}

	if forcedID != page.Executions[len(page.Executions)-1].ID {
		return true, nil
	}

	cursor := page.Cursor
	for cursor != "" {
		if err := ctx.Err(); err != nil {
			return false, err
		}

		next, err := storage.ListIncompleteSummaryPage(ctx, IncompleteSummaryQuery{Cursor: cursor, Limit: 1})
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
