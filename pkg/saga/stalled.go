package saga

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// StalledError is the error recorded on an execution the sweep forces to
// failed. It is deliberately a sentence rather than a code: it is read by
// whoever runs `m debug saga show` on a forced record, and "this saga was never
// going to finish" is the thing they need to know.
const StalledError = "forced to failed by the stalled-saga sweep: this execution " +
	"had not changed state for longer than the stall window, so nothing was " +
	"driving it and nothing would have collected it"

// StalledConfig tunes a stalled-saga sweep.
type StalledConfig struct {
	// StaleAfter is how long an execution may sit without changing state before
	// the sweep declares it stranded. Zero disables the sweep, which is the
	// escape hatch if a cluster needs its in-flight sagas left exactly as they
	// are for an investigation.
	//
	// This is a measurement, not a guess about provenance. Save writes a
	// timestamp on every transition and every action completion, so a saga that
	// is being driven, or that is retrying its undos on a loop, keeps a recent
	// one no matter how old the run is. What sits untouched for days is what
	// nothing is driving.
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

	// Recovered is how many candidates turned out, on the re-read, to have
	// moved on under us: finished, resumed, or deleted between the page and the
	// write. Not an error and not a problem, but worth counting, because a
	// sweep reporting a lot of them is reading pages that are too old to act
	// on.
	Recovered int

	// Capped reports that MaxForces stopped the sweep before it had inspected
	// every in-flight execution. It says the sweep did not finish looking, not
	// that more transitions are certain.
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
// The sweep transitions rather than deletes, because failed composes with
// machinery that already exists in both directions and needs no guess about
// which kind of record it is looking at. An execution named after its entity is
// cleared by the next reconcile's DropIfFailed and retried, which is the
// healing we want. One with a generated name is collected by ordinary retention
// a window later, and in the meantime an operator has a record to inspect. That
// is worth the extra retention window: deleting on sight destroys the evidence
// of exactly the thing the count is a signal about.
//
// Forcing is not free on a live cluster. A convergent-ID execution forced here
// causes its owner to genuinely retry a real operation on the next pass. That
// is the intent, but it is why the window wants to be generous and why a sweep
// that forces anything at all says so at Info.
//
// The sweep is idempotent, so a caller that is interrupted or capped simply
// runs again. A nil log falls back to the default logger.
func RunStalledSweep(ctx context.Context, storage Storage, cfg StalledConfig, log *slog.Logger) (*StalledResult, error) {
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

			// A stalled child whose parent is still in flight has to stay. The
			// parent re-finds its children by deterministic ID rather than
			// re-running them, so failing one out from under a live parent
			// fails the parent for a reason that was never true.
			//
			// A stranded parent is non-terminal and so counts as live here,
			// which means a stranded tree takes one sweep per level rather
			// than one sweep: forcing the parent makes it terminal, which
			// releases its children on the next pass. Slower, and it
			// converges, which is the right trade against a check whose other
			// failure mode is failing a saga that was fine.
			if summary.ParentID != "" && parents.isLive(ctx, summary.ParentID, log) {
				result.Skipped++
				continue
			}

			forced, err := forceFailed(ctx, storage, summary.ID, cutoff)
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

			log.Info("forced stalled saga execution to failed",
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

// forceFailed transitions one execution to failed, reporting false if it turned
// out not to need it.
//
// The re-read is the point. A page is a snapshot of an index, and between
// building it and reaching this execution the saga may have been resumed,
// finished, or deleted. Writing failed from the summary alone would take a saga
// that had just started running again and declare it dead, which is the one
// way this sweep could cause the damage it exists to clean up. So the decision
// is made against the record as it is at the moment of writing: still in
// flight, and still older than the same cutoff the page was measured against.
func forceFailed(ctx context.Context, storage Storage, id string, cutoff time.Time) (bool, error) {
	exec, err := storage.Get(ctx, id)
	if errors.Is(err, ErrExecutionNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("loading execution %q: %w", id, err)
	}

	if isTerminal(exec.Status) {
		return false, nil
	}

	// A zero timestamp here is a legacy record whose age came from the entity
	// store rather than the saga, and re-reading it as an Execution threw that
	// away. It is not evidence the execution is fresh, so it does not veto a
	// decision the page already made against a real timestamp.
	if !exec.UpdatedAt.IsZero() && exec.UpdatedAt.After(cutoff) {
		return false, nil
	}

	exec.Status = StatusFailed
	exec.Error = StalledError
	exec.UpdatedAt = time.Now()

	if err := storage.Save(ctx, exec); err != nil {
		return false, fmt.Errorf("saving execution %q: %w", id, err)
	}

	return true, nil
}

// stoppedEarlyStalled reports whether a sweep that just spent its force budget
// left anything uninspected. It is stoppedEarly's counterpart for the
// in-flight walk, and the same reasoning applies in full: spending the budget
// is not the same as being cut short, a cursor alone cannot tell the two apart
// because the walk covers several status indexes in sequence, and so the
// lookahead reads forward until it finds something or the walk genuinely ends.
func stoppedEarlyStalled(ctx context.Context, storage Storage, page *IncompleteSummaryPage, forcedID string) (bool, error) {
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
