package commands

import (
	"fmt"
	"math"
	"time"

	"miren.dev/runtime/pkg/ui"
)

// defaultRunListLimit keeps the list to something a person reads. Apps that
// invoke tasks from automation accumulate runs quickly, and an unbounded list
// of successful console sessions is a list nobody looks at.
const defaultRunListLimit = 20

func AppRuns(ctx *Context, opts struct {
	AppCentric
	FormatOptions

	Task  string `long:"task" description:"Only show runs of this task"`
	Limit int    `short:"n" long:"limit" description:"Maximum runs to show"`
}) error {
	runs, err := runsClient(ctx)
	if err != nil {
		return err
	}

	limit := opts.Limit
	if limit == 0 {
		limit = defaultRunListLimit
	}
	// Reject before narrowing. The RPC field is int32, so a value past its
	// range wraps -- 4294967296 becomes 0, which the server reads as "no limit"
	// and returns everything. Better to refuse than to silently do the opposite
	// of what was asked.
	if limit < 0 || limit > math.MaxInt32 {
		return fmt.Errorf("--limit must be between 1 and %d", math.MaxInt32)
	}

	results, err := runs.ListRuns(ctx, opts.App, opts.Task, int32(limit))
	if err != nil {
		return err
	}

	list := results.Runs()

	if opts.IsJSON() {
		type runJSON struct {
			ID        string `json:"id"`
			Task      string `json:"task"`
			Trigger   string `json:"trigger"`
			Status    string `json:"status"`
			Command   string `json:"command,omitempty"`
			ExitCode  *int32 `json:"exit_code"`
			Attempt   int32  `json:"attempt,omitempty"`
			StartedAt string `json:"started_at,omitempty"`
			EndedAt   string `json:"ended_at,omitempty"`
			Sandbox   string `json:"sandbox,omitempty"`
		}

		items := make([]runJSON, 0, len(list))
		for _, r := range list {
			item := runJSON{
				ID:      r.Id(),
				Task:    r.Task(),
				Trigger: r.Trigger(),
				Status:  r.Status(),
				Command: r.Command(),
				Attempt: r.Attempt(),
				Sandbox: r.Sandbox(),
			}
			// Null rather than 0 when no exit was observed: a timed-out run did
			// not exit cleanly, and 0 would say it did.
			if r.HasExitCode() {
				code := r.ExitCode()
				item.ExitCode = &code
			}
			if r.StartedAt() > 0 {
				item.StartedAt = time.UnixMilli(r.StartedAt()).UTC().Format(time.RFC3339)
			}
			if r.EndedAt() > 0 {
				item.EndedAt = time.UnixMilli(r.EndedAt()).UTC().Format(time.RFC3339)
			}
			items = append(items, item)
		}

		return PrintJSON(items)
	}

	if len(list) == 0 {
		ctx.Printf("No runs found\n")
		return nil
	}

	headers := []string{"ID", "TASK", "TRIGGER", "STATUS", "EXIT", "STARTED", "DURATION"}
	var rows []ui.Row
	for _, r := range list {
		exit := "-"
		if r.HasExitCode() {
			exit = fmt.Sprintf("%d", r.ExitCode())
		}

		started := "-"
		if r.StartedAt() > 0 {
			started = humanFriendlyTimestamp(time.UnixMilli(r.StartedAt()))
		}

		rows = append(rows, ui.Row{
			ui.DisplayShortID(r.ShortId(), r.Id()),
			r.Task(),
			r.Trigger(),
			r.Status(),
			exit,
			started,
			runDuration(r.StartedAt(), r.EndedAt()),
		})
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))
	table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
	ctx.Printf("%s\n", table.Render())
	return nil
}

func AppRunsCancel(ctx *Context, opts struct {
	AppCentric

	Run string `position:"0" usage:"Run to cancel" required:"true"`
}) error {
	runs, err := runsClient(ctx)
	if err != nil {
		return err
	}

	result, err := runs.CancelRun(ctx, opts.Run)
	if err != nil {
		return err
	}

	if !result.Canceled() {
		ctx.Printf("Run %s has already finished\n", opts.Run)
		return nil
	}

	// Cancellation is a request the controller acts on, so say what was asked
	// rather than claiming the run has stopped.
	ctx.Printf("Cancellation requested for %s\n", opts.Run)
	return nil
}

// runDuration renders how long a run took, or how long it has been going.
func runDuration(startedAt, endedAt int64) string {
	if startedAt == 0 {
		return "-"
	}

	start := time.UnixMilli(startedAt)
	end := time.Now()
	if endedAt > 0 {
		end = time.UnixMilli(endedAt)
	}

	d := end.Sub(start).Truncate(time.Second)
	if d < 0 {
		return "-"
	}
	return d.String()
}
