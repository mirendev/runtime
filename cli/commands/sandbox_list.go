package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/compute"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/ui"
)

// sandboxFilter narrows a sandbox listing to one app and/or service. Both
// match against the values shown in the APP and SERVICE columns, so what you
// filter on is what you see — including the "-" cases, which simply match
// nothing rather than matching everything.
type sandboxFilter struct {
	app     string
	service string
}

func (f sandboxFilter) keep(app, service string) bool {
	if f.app != "" && app != f.app {
		return false
	}

	if f.service != "" && service != f.service {
		return false
	}

	return true
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

func SandboxList(ctx *Context, opts struct {
	All     bool   `short:"a" long:"all" description:"Include dead sandboxes (excluded by default)"`
	Status  string `short:"s" long:"status" description:"Filter by status (pending, not_ready, running, stopped, dead)"`
	App     string `long:"app" description:"Only show sandboxes belonging to this app"`
	Service string `long:"service" description:"Only show sandboxes of this service (e.g. web, worker)"`
	FormatOptions
	ConfigCentric
}) error {
	filter := sandboxFilter{app: opts.App, service: opts.Service}

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

	// Get all sandbox pools to map pool ID -> service
	poolKindRes, err := eac.LookupKind(ctx, "sandbox_pool")
	if err != nil {
		return err
	}
	poolsRes, err := eac.List(ctx, poolKindRes.Attr())
	if err != nil {
		return err
	}

	// Create maps of pool ID -> service and pool ID -> short ID
	poolServiceMap := make(map[string]string)
	poolShortIdMap := make(map[string]string)
	for _, e := range poolsRes.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(e.Entity())
		poolServiceMap[pool.ID.String()] = pool.Service
		if sid := e.Entity().ShortId(); sid != "" {
			poolShortIdMap[pool.ID.String()] = sid
		}
	}

	// Build maps of version ID -> short ID and version ID -> app name (best-effort for display)
	versionShortIdMap := make(map[string]string)
	versionAppMap := make(map[string]string)
	if versionKindRes, err := eac.LookupKind(ctx, "app_version"); err == nil {
		if versionsRes, err := eac.List(ctx, versionKindRes.Attr()); err == nil {
			for _, e := range versionsRes.Values() {
				var v core_v1alpha.AppVersion
				v.Decode(e.Entity())
				if sid := e.Entity().ShortId(); sid != "" {
					versionShortIdMap[v.ID.String()] = sid
				}
				if !entity.Empty(v.App) {
					versionAppMap[v.ID.String()] = v.App.String()
				}
			}
		}
	}

	// Build a map of node entity ID -> human-readable name
	nodeKindRes, err := eac.LookupKind(ctx, "node")
	if err != nil {
		return err
	}
	nodesRes, err := eac.List(ctx, nodeKindRes.Attr())
	if err != nil {
		return err
	}
	nodeNameMap := make(map[entity.Id]string)
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
			nodeNameMap[node.ID] = name
		}
	}

	// Determine whether to exclude dead sandboxes.
	// Dead sandboxes are excluded by default unless --all is passed
	// or --status explicitly requests a dead state.
	excludeDead := !opts.All && opts.Status == ""

	// For JSON output, just filter and return the raw sandbox structs with pool info
	if opts.IsJSON() {
		var sandboxes []struct {
			compute_v1alpha.Sandbox
			App     string `json:"app,omitempty"`
			Pool    string `json:"pool,omitempty"`
			Service string `json:"service,omitempty"`
			Address string `json:"address,omitempty"`
			Runner  string `json:"runner,omitempty"`
		}

		for _, e := range res.Values() {
			var sandbox compute_v1alpha.Sandbox
			sandbox.Decode(e.Entity())

			// Extract pool label from metadata
			var md core_v1alpha.Metadata
			md.Decode(e.Entity())
			poolLabel, _ := md.Labels.Get("pool")

			// Get service from pool
			service := poolServiceMap[poolLabel]

			if !filter.keep(ui.CleanEntityID(versionAppMap[sandbox.Spec.Version.String()]), service) {
				continue
			}

			if excludeDead && compute.SandboxDead(sandbox.Status) {
				continue
			}

			// Apply status filter if specified
			if opts.Status != "" {
				status := string(sandbox.Status)
				cleanStatus := ui.CleanStatus(status)
				if cleanStatus != opts.Status {
					continue
				}
			}

			// Get network address
			address := ""
			if len(sandbox.Network) > 0 && sandbox.Network[0].Address != "" {
				address = sandbox.Network[0].Address
			}

			// Resolve runner from schedule
			var sch compute_v1alpha.Schedule
			sch.Decode(e.Entity())
			runner := ""
			if !entity.Empty(sch.Key.Node) {
				runner = nodeNameMap[sch.Key.Node]
			}

			// Resolve app name from version
			appName := ""
			if name, ok := versionAppMap[sandbox.Spec.Version.String()]; ok {
				appName = name
			}

			entry := struct {
				compute_v1alpha.Sandbox
				App     string `json:"app,omitempty"`
				Pool    string `json:"pool,omitempty"`
				Service string `json:"service,omitempty"`
				Address string `json:"address,omitempty"`
				Runner  string `json:"runner,omitempty"`
			}{
				Sandbox: sandbox,
				App:     appName,
				Pool:    poolLabel,
				Service: service,
				Address: address,
				Runner:  runner,
			}
			sandboxes = append(sandboxes, entry)
		}

		return PrintJSON(sandboxes)
	}

	// Table output - all the UI formatting logic
	var rows []ui.Row
	var deadCount int
	headers := []string{"ID", "APP", "VERSION", "SERVICE", "POOL", "ADDRESS", "RUNNER", "STATUS", "CREATED", "UPDATED"}

	for _, e := range res.Values() {
		// Decode the sandbox entity
		var sandbox compute_v1alpha.Sandbox
		sandbox.Decode(e.Entity())

		// Extract pool label from metadata
		var md core_v1alpha.Metadata
		md.Decode(e.Entity())
		poolLabel, _ := md.Labels.Get("pool")

		// Resolve app name from version
		appName := ui.CleanEntityID(versionAppMap[sandbox.Spec.Version.String()])

		// Narrowing comes before the dead check so that the "N dead hidden"
		// tally counts what this listing is actually about. Reporting the
		// cluster's dead sandboxes under `--app foo` would be a non-sequitur.
		if !filter.keep(appName, poolServiceMap[poolLabel]) {
			continue
		}

		if excludeDead && compute.SandboxDead(sandbox.Status) {
			deadCount++
			continue
		}

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

		poolLabelDisplay := poolLabel
		if poolLabelDisplay == "" {
			poolLabelDisplay = "-"
		} else {
			poolLabelDisplay = ui.CleanEntityID(poolLabelDisplay)
		}

		// Get service from pool
		service := poolServiceMap[poolLabel]
		if service == "" {
			service = "-"
		}

		// Get network address
		address := "-"
		if len(sandbox.Network) > 0 && sandbox.Network[0].Address != "" {
			address = sandbox.Network[0].Address
		}

		// Resolve runner from schedule
		var sch compute_v1alpha.Schedule
		sch.Decode(e.Entity())
		runnerName := "-"
		if !entity.Empty(sch.Key.Node) {
			if name, ok := nodeNameMap[sch.Key.Node]; ok {
				runnerName = name
			}
		}

		// Apply all UI formatting for table display
		sandboxId := ui.CleanEntityID(sandbox.ID.String())
		if !ctx.Verbose() {
			sandboxId = ui.BriefId(e.Entity())
		}

		// Resolve version display: prefer short ID
		versionDisplay := ui.DisplayAppVersion(sandbox.Spec.Version.String())
		if shortId, ok := versionShortIdMap[sandbox.Spec.Version.String()]; ok {
			versionDisplay = shortId
		}

		// Resolve pool display: prefer short ID
		if shortId, ok := poolShortIdMap[poolLabel]; ok {
			poolLabelDisplay = shortId
		}

		appDisplay := appName
		if appDisplay == "" {
			appDisplay = "-"
		}

		rows = append(rows, ui.Row{
			sandboxId,
			appDisplay,
			versionDisplay,
			service,
			poolLabelDisplay,
			address,
			runnerName,
			ui.DisplayStatus(status),
			humanFriendlyTimestamp(time.UnixMilli(e.CreatedAt())),
			humanFriendlyTimestamp(time.UnixMilli(e.UpdatedAt())),
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
