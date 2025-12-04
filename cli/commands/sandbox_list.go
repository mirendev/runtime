package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
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

	// Get all sandbox pools to map pool ID -> service
	poolKindRes, err := eac.LookupKind(ctx, "sandbox_pool")
	if err != nil {
		return err
	}
	poolsRes, err := eac.List(ctx, poolKindRes.Attr())
	if err != nil {
		return err
	}

	// Create a map of pool ID -> service
	poolServiceMap := make(map[string]string)
	for _, e := range poolsRes.Values() {
		var pool compute_v1alpha.SandboxPool
		pool.Decode(e.Entity())
		poolServiceMap[pool.ID.String()] = pool.Service
	}

	// For JSON output, just filter and return the raw sandbox structs with pool info
	if opts.IsJSON() {
		var sandboxes []struct {
			compute_v1alpha.Sandbox
			Pool    string `json:"pool,omitempty"`
			Service string `json:"service,omitempty"`
			Address string `json:"address,omitempty"`
		}

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

			// Extract pool label from metadata
			var md core_v1alpha.Metadata
			md.Decode(e.Entity())
			poolLabel, _ := md.Labels.Get("pool")

			// Get service from pool
			service := poolServiceMap[poolLabel]

			// Get network address
			address := ""
			if len(sandbox.Network) > 0 && sandbox.Network[0].Address != "" {
				address = sandbox.Network[0].Address
			}

			sandboxes = append(sandboxes, struct {
				compute_v1alpha.Sandbox
				Pool    string `json:"pool,omitempty"`
				Service string `json:"service,omitempty"`
				Address string `json:"address,omitempty"`
			}{
				Sandbox: sandbox,
				Pool:    poolLabel,
				Service: service,
				Address: address,
			})
		}

		return PrintJSON(sandboxes)
	}

	// Table output - all the UI formatting logic
	var rows []ui.Row
	headers := []string{"ID", "VERSION", "SERVICE", "POOL", "ADDRESS", "STATUS", "CREATED", "UPDATED"}

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

		// Extract pool label from metadata
		var md core_v1alpha.Metadata
		md.Decode(e.Entity())
		poolLabel, _ := md.Labels.Get("pool")
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

		// Apply all UI formatting for table display
		rows = append(rows, ui.Row{
			ui.CleanEntityID(sandbox.ID.String()),
			ui.DisplayAppVersion(sandbox.Spec.Version.String()),
			service,
			poolLabelDisplay,
			address,
			ui.DisplayStatus(status),
			humanFriendlyTimestamp(time.UnixMilli(e.CreatedAt())),
			humanFriendlyTimestamp(time.UnixMilli(e.UpdatedAt())),
		})
	}

	if len(rows) == 0 {
		ctx.Printf("No sandboxes found\n")
		return nil
	}

	// Create and render the table
	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))
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
