package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

// sandboxFilter owns the CLI's presentation policy. The inventory RPC returns
// every safe summary; flags decide which of those records this invocation
// shows and which terminal records count as hidden.
type sandboxFilter struct {
	includeDead bool
	status      string
	app         string
	service     string
}

func (f sandboxFilter) describe() string {
	switch {
	case f.app != "" && f.service != "":
		return fmt.Sprintf(" for app %q, service %q", f.app, f.service)
	case f.app != "":
		return fmt.Sprintf(" for app %q", f.app)
	case f.service != "":
		return fmt.Sprintf(" for service %q", f.service)
	default:
		return ""
	}
}

func (f sandboxFilter) apply(entries []sandboxListEntry) ([]sandboxListEntry, int64) {
	filtered := make([]sandboxListEntry, 0, len(entries))
	var hiddenDead int64

	for _, entry := range entries {
		if f.app != "" && entry.App != f.app {
			continue
		}
		if f.service != "" && entry.Service != f.service {
			continue
		}

		if !f.includeDead && f.status == "" && sandboxTerminal(entry.Status) {
			hiddenDead++
			continue
		}
		if f.status != "" && entry.Status != f.status {
			continue
		}

		filtered = append(filtered, entry)
	}

	return filtered, hiddenDead
}

func sandboxTerminal(status string) bool {
	return status == "stopped" || status == "dead"
}

// sandboxListEntry is the complete public contract of `sandbox list --json`.
// Keep this an allow-list. In particular, never embed compute_v1alpha.Sandbox:
// its execution spec contains resolved container environment values.
type sandboxListEntry struct {
	ID        string `json:"id"`
	App       string `json:"app,omitempty"`
	Version   string `json:"version,omitempty"`
	Service   string `json:"service,omitempty"`
	Pool      string `json:"pool,omitempty"`
	Address   string `json:"address,omitempty"`
	Runner    string `json:"runner,omitempty"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	shortID        string
	versionShortID string
	poolShortID    string
	createdAt      time.Time
	updatedAt      time.Time
}

func SandboxList(ctx *Context, opts struct {
	All     bool   `short:"a" long:"all" description:"Include dead sandboxes (excluded by default)"`
	Status  string `short:"s" long:"status" description:"Filter by status (pending, not_ready, running, stopped, dead)"`
	App     string `long:"app" description:"Only show sandboxes belonging to this app"`
	Service string `long:"service" description:"Only show sandboxes of this service (e.g. web, worker)"`
	FormatOptions
	ConfigCentric
}) error {
	filter := sandboxFilter{
		includeDead: opts.All,
		status:      opts.Status,
		app:         opts.App,
		service:     opts.Service,
	}

	entries, err := listSandboxesRPC(ctx)
	if err != nil {
		return err
	}
	entries, deadCount := filter.apply(entries)

	if opts.IsJSON() {
		return PrintJSONTo(ctx.Stdout, entries)
	}

	rows := make([]ui.Row, 0, len(entries))
	headers := []string{"ID", "APP", "VERSION", "SERVICE", "POOL", "ADDRESS", "RUNNER", "STATUS", "CREATED", "UPDATED"}

	for _, entry := range entries {
		poolLabelDisplay := entry.Pool
		if poolLabelDisplay == "" {
			poolLabelDisplay = "-"
		} else {
			poolLabelDisplay = ui.CleanEntityID(poolLabelDisplay)
		}

		service := entry.Service
		if service == "" {
			service = "-"
		}

		address := entry.Address
		if address == "" {
			address = "-"
		}

		runnerName := entry.Runner
		if runnerName == "" {
			runnerName = "-"
		}

		sandboxID := ui.CleanEntityID(entry.ID)
		if !ctx.Verbose() && entry.shortID != "" {
			sandboxID = entry.shortID
		}

		versionDisplay := ui.DisplayAppVersion(entry.Version)
		if entry.versionShortID != "" {
			versionDisplay = entry.versionShortID
		}

		if entry.poolShortID != "" {
			poolLabelDisplay = entry.poolShortID
		}

		appDisplay := entry.App
		if appDisplay == "" {
			appDisplay = "-"
		}

		rows = append(rows, ui.Row{
			sandboxID,
			appDisplay,
			versionDisplay,
			service,
			poolLabelDisplay,
			address,
			runnerName,
			ui.DisplayStatus(entry.Status),
			humanFriendlyTimestamp(entry.createdAt),
			humanFriendlyTimestamp(entry.updatedAt),
		})
	}

	if len(rows) == 0 {
		// Naming the filters matters most when they're the reason for the empty
		// result — a typo'd app name is otherwise indistinguishable from an app
		// that genuinely has nothing running.
		ctx.Printf("No sandboxes found%s\n", filter.describe())
		if deadCount > 0 {
			ctx.Printf("%d dead sandbox(es) hidden. Use --all to show.\n", deadCount)
		}
		return nil
	}

	// Create and render the table
	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())

	if deadCount > 0 {
		ctx.Printf("\n%d dead sandbox(es) hidden. Use --all to show.\n", deadCount)
	}

	return nil
}

func listSandboxesRPC(ctx *Context) ([]sandboxListEntry, error) {
	client, err := ctx.RPCClient("dev.miren.runtime/sandboxes")
	if err != nil {
		return nil, err
	}
	defer client.Close()

	result, err := compute_v1alpha.NewSandboxesClient(client).List(ctx)
	if err != nil {
		return nil, err
	}

	entries := make([]sandboxListEntry, 0, len(result.Sandboxes()))
	for _, sb := range result.Sandboxes() {
		entries = append(entries, newSandboxListEntry(
			sb.Id(), sb.ShortId(), sb.App(), sb.Version(), sb.VersionShortId(),
			sb.Service(), sb.Pool(), sb.PoolShortId(), sb.Address(), sb.Runner(),
			sb.Status(), sb.CreatedAt(), sb.UpdatedAt(),
		))
	}

	return entries, nil
}

func newSandboxListEntry(
	id, shortID, app, version, versionShortID, service, pool, poolShortID,
	address, runner, status string,
	createdAtMillis, updatedAtMillis int64,
) sandboxListEntry {
	createdAt := time.UnixMilli(createdAtMillis)
	updatedAt := time.UnixMilli(updatedAtMillis)
	status = ui.CleanStatus(status)
	if status == "" {
		status = "unknown"
	}
	return sandboxListEntry{
		ID:             id,
		App:            app,
		Version:        version,
		Service:        service,
		Pool:           pool,
		Address:        address,
		Runner:         runner,
		Status:         status,
		CreatedAt:      createdAt.UTC().Format(time.RFC3339),
		UpdatedAt:      updatedAt.UTC().Format(time.RFC3339),
		shortID:        shortID,
		versionShortID: versionShortID,
		poolShortID:    poolShortID,
		createdAt:      createdAt,
		updatedAt:      updatedAt,
	}
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
		return fmt.Sprintf("%ds ago", int(since.Seconds()))
	} else if since < time.Hour {
		return fmt.Sprintf("%dm ago", int(since.Minutes()))
	} else if since < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(since.Hours()))
	} else if since < 7*24*time.Hour {
		return fmt.Sprintf("%dd ago", int(since.Hours()/24))
	} else if since < 30*24*time.Hour {
		return fmt.Sprintf("%dw ago", int(since.Hours()/(24*7)))
	} else if since < 365*24*time.Hour {
		return fmt.Sprintf("%dmo ago", int(since.Hours()/(24*30)))
	} else {
		return fmt.Sprintf("%dy ago", int(since.Hours()/(24*365)))
	}
}
