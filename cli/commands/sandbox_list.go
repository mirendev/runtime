package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
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
		if !serverPredatesSandboxInventory(err) {
			return err
		}
		entries, err = listSandboxesLegacy(ctx)
		if err != nil {
			return err
		}
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

// serverPredatesSandboxInventory distinguishes an old server from a current
// server refusing or failing the request. Falling back after an authorization
// error would turn the raw entity API into a privilege bypass.
func serverPredatesSandboxInventory(err error) bool {
	return errors.Is(err, rpc.ErrResolveLookup)
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

// listSandboxesLegacy keeps a current CLI useful against a server from before
// the sandbox inventory capability. Although it must read raw entities, it
// converts each one into the allow-listed entry immediately; rendering never
// receives the Sandbox and therefore cannot serialize its execution spec.
func listSandboxesLegacy(ctx *Context) ([]sandboxListEntry, error) {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return listSandboxesFromEntities(ctx, entityserver_v1alpha.NewEntityAccessClient(client))
}

func listSandboxesFromEntities(
	ctx context.Context,
	eac *entityserver_v1alpha.EntityAccessClient,
) ([]sandboxListEntry, error) {
	kindRes, err := eac.LookupKind(ctx, "sandbox")
	if err != nil {
		return nil, err
	}
	res, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return nil, err
	}

	poolKindRes, err := eac.LookupKind(ctx, "sandbox_pool")
	if err != nil {
		return nil, err
	}
	poolsRes, err := eac.List(ctx, poolKindRes.Attr())
	if err != nil {
		return nil, err
	}
	poolServices := make(map[string]string)
	poolShortIDs := make(map[string]string)
	for _, e := range poolsRes.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(e.Entity())
		poolServices[pool.ID.String()] = pool.Service
		if shortID := e.Entity().ShortId(); shortID != "" {
			poolShortIDs[pool.ID.String()] = shortID
		}
	}

	versionShortIDs := make(map[string]string)
	versionApps := make(map[string]string)
	if versionKindRes, err := eac.LookupKind(ctx, "app_version"); err == nil {
		if versionsRes, err := eac.List(ctx, versionKindRes.Attr()); err == nil {
			for _, e := range versionsRes.Values() {
				var version core_v1alpha.AppVersion
				version.Decode(e.Entity())
				if shortID := e.Entity().ShortId(); shortID != "" {
					versionShortIDs[version.ID.String()] = shortID
				}
				if !entity.Empty(version.App) {
					versionApps[version.ID.String()] = ui.CleanEntityID(version.App.String())
				}
			}
		}
	}

	nodeKindRes, err := eac.LookupKind(ctx, "node")
	if err != nil {
		return nil, err
	}
	nodesRes, err := eac.List(ctx, nodeKindRes.Attr())
	if err != nil {
		return nil, err
	}
	nodeNames := make(map[entity.Id]string)
	for _, e := range nodesRes.Values() {
		var node compute_v1alpha.Node
		node.Decode(e.Entity())
		name := node.Name
		if name == "" {
			name = node.RunnerId
			if len(name) > 12 {
				name = name[:12]
			}
		}
		if name != "" {
			nodeNames[node.ID] = name
		}
	}

	entries := make([]sandboxListEntry, 0, len(res.Values()))
	for _, e := range res.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(e.Entity())

		var metadata core_v1alpha.Metadata
		metadata.Decode(e.Entity())
		pool, _ := metadata.Labels.Get("pool")
		service := poolServices[pool]
		app := versionApps[sb.Spec.Version.String()]

		address := ""
		if len(sb.Network) > 0 {
			address = sb.Network[0].Address
		}

		var schedule compute_v1alpha.Schedule
		schedule.Decode(e.Entity())
		runner := ""
		if !entity.Empty(schedule.Key.Node) {
			runner = nodeNames[schedule.Key.Node]
		}

		entries = append(entries, newSandboxListEntry(
			sb.ID.String(), e.Entity().ShortId(), app, sb.Spec.Version.String(),
			versionShortIDs[sb.Spec.Version.String()], service, pool, poolShortIDs[pool],
			address, runner, string(sb.Status), e.CreatedAt(), e.UpdatedAt(),
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
