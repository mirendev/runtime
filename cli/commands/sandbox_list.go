package commands

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
)

func SandboxList(ctx *Context, opts struct {
	Status string `short:"s" long:"status" description:"Filter by status (pending, not_ready, running, stopped, dead)"`
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
		ctx.Printf("No sandboxes found\n")
		return nil
	}

	// Create a tabwriter for formatted output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "ID\tSTATUS\tVERSION\tCONTAINERS\tCREATED\tUPDATED\n")

	for _, e := range res.Values() {
		// Decode the sandbox entity
		var sandbox compute_v1alpha.Sandbox
		sandbox.Decode(e.Entity())

		// Get status string
		status := string(sandbox.Status)
		if status == "" {
			status = "unknown"
		}

		// Filter by status if specified
		if opts.Status != "" && status != "status."+opts.Status {
			continue
		}

		// Get version string
		version := sandbox.Version.String()
		if version == "" {
			version = "-"
		}

		// Count containers
		containerCount := len(sandbox.Container)

		// Format created time (CreatedAt is in milliseconds)
		created := humanFriendlyTimestamp(time.UnixMilli(e.CreatedAt()))

		// Format updated time (UpdatedAt is in milliseconds)
		updated := humanFriendlyTimestamp(time.UnixMilli(e.UpdatedAt()))

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			sandbox.ID.String(),
			status,
			version,
			containerCount,
			created,
			updated,
		)
	}

	return w.Flush()
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
