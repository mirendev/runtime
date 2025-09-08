package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func SandboxList(ctx *Context, opts struct {
	Status string `short:"s" long:"status" description:"Filter by status (pending, not_ready, running, stopped, dead)"`
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// Get the sandbox kind attribute
	kindRes, err := eac.LookupKind(ctx, "sandbox")
	if err != nil {
		return err
	}

	// List all sandboxes
	res, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return err
	}

	if len(res.Values()) == 0 {
		if opts.IsJSON() {
			return PrintJSON([]interface{}{})
		}
		ctx.Printf("No sandboxes found\n")
		return nil
	}

	// For JSON output, just filter and return the raw sandbox structs
	if opts.IsJSON() {
		var sandboxes []compute_v1alpha.Sandbox

		for _, e := range res.Values() {
			var sandbox compute_v1alpha.Sandbox
			sandbox.Decode(e.Entity())

			// Apply status filter if specified
			if opts.Status != "" {
				status := string(sandbox.Status)
				cleanStatus := ui.CleanStatus(status)
				if cleanStatus != opts.Status {
					continue
				}
			}

			sandboxes = append(sandboxes, sandbox)
		}

		return PrintJSON(sandboxes)
	}

	// Table output - all the UI formatting logic
	var rows []ui.Row
	headers := []string{"ID", "STATUS", "VERSION", "CONTAINERS", "CREATED", "UPDATED"}
	hasResults := false

	for _, e := range res.Values() {
		// Decode the sandbox entity
		var sandbox compute_v1alpha.Sandbox
		sandbox.Decode(e.Entity())

		// Get status string
		status := string(sandbox.Status)
		if status == "" {
			status = "unknown"
		}

		// Clean status for filtering (removes "status." prefix)
		cleanStatus := ui.CleanStatus(status)

		// Filter by status if specified
		if opts.Status != "" && cleanStatus != opts.Status {
			continue
		}

		hasResults = true

		// Apply all UI formatting for table display
		rows = append(rows, ui.Row{
			ui.CleanEntityID(sandbox.ID.String()),
			ui.DisplayStatus(status),
			ui.DisplayAppVersion(sandbox.Version.String()),
			fmt.Sprintf("%d", len(sandbox.Container)),
			humanFriendlyTimestamp(time.UnixMilli(e.CreatedAt())),
			humanFriendlyTimestamp(time.UnixMilli(e.UpdatedAt())),
		})
	}

	// If no results after filtering
	if !hasResults {
		ctx.Printf("No sandboxes found matching criteria\n")
		return nil
	}

	// Create and render the table
	columns := ui.AutoSizeColumns(headers, rows)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}

// humanFriendlyTimestamp formats a timestamp into a human-friendly format like Docker's
func humanFriendlyTimestamp(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return "-"
	}

	since := time.Since(t)

	// Handle negative durations (timestamps in the future or invalid)
	if since < 0 {
		return "-"
	}

	if since < time.Minute {
		return fmt.Sprintf("%d seconds ago", int(since.Seconds()))
	} else if since < time.Hour {
		mins := int(since.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	} else if since < 24*time.Hour {
		hours := int(since.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if since < 7*24*time.Hour {
		days := int(since.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	} else if since < 30*24*time.Hour {
		weeks := int(since.Hours() / (24 * 7))
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	} else if since < 365*24*time.Hour {
		months := int(since.Hours() / (24 * 30))
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	} else {
		years := int(since.Hours() / (24 * 365))
		if years == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", years)
	}
}
