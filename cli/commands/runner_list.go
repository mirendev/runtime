package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/ui"
)

func RunnerList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient(rpc.ServiceRunner)
	if err != nil {
		return err
	}
	defer client.Close()

	rc := runner_v1alpha.NewRunnerRegistrationClient(client)

	res, err := rc.ListRunners(ctx)
	if err != nil {
		return err
	}

	runners := res.Runners()

	if opts.IsJSON() {
		type RunnerJSON struct {
			ID           string   `json:"id"`
			ShortID      string   `json:"short_id,omitempty"`
			RunnerID     string   `json:"runner_id"`
			Name         string   `json:"name"`
			Status       string   `json:"status"`
			Scheduling   string   `json:"scheduling,omitempty"`
			Cordoned     bool     `json:"cordoned"`
			Version      string   `json:"version"`
			APIAddress   string   `json:"api_address"`
			Labels       []string `json:"labels,omitempty"`
			RegisteredAt string   `json:"registered_at,omitempty"`
		}

		var output []RunnerJSON
		for _, r := range runners {
			rj := RunnerJSON{
				ID:         r.Id(),
				ShortID:    r.ShortId(),
				RunnerID:   r.RunnerId(),
				Name:       r.Name(),
				Status:     r.Status(),
				Scheduling: r.Scheduling(),
				Cordoned:   r.Scheduling() == "cordoned",
				Version:    r.Version(),
				APIAddress: r.ApiAddress(),
				Labels:     r.Labels(),
			}
			if r.HasRegisteredAt() {
				rj.RegisteredAt = standard.FromTimestamp(r.RegisteredAt()).Format(time.RFC3339)
			}
			output = append(output, rj)
		}
		return PrintJSON(output)
	}

	if len(runners) == 0 {
		ctx.Printf("No runners registered\n")
		ctx.Printf("\nUse 'miren runner token create' to create a join token\n")
		return nil
	}

	headers := []string{"ID", "NAME", "STATUS", "VERSION", "ADDRESS", "REGISTERED"}
	var rows []ui.Row

	for _, r := range runners {
		id := ui.DisplayShortID(r.ShortId(), r.Id())

		name := r.Name()
		if name == "" {
			name = r.RunnerId()
			if len(name) > 12 {
				name = name[:12]
			}
		}

		status := r.Status()
		switch status {
		case "ready", "status.ready":
			status = infoGreen.Render("ready")
		case "unknown", "status.unknown":
			status = infoGray.Render("unknown")
		case "disabled", "status.disabled":
			status = infoLabel.Render("disabled")
		case "unhealthy", "status.unhealthy":
			status = infoRed.Render("unhealthy")
		}
		if r.Scheduling() == "cordoned" {
			status += " " + infoLabel.Render("(cordoned)")
		}

		version := r.Version()
		if version == "" {
			version = "-"
		}

		addr := r.ApiAddress()
		if addr == "" {
			addr = "-"
		}

		registered := "-"
		if r.HasRegisteredAt() {
			registered = formatDuration(time.Since(standard.FromTimestamp(r.RegisteredAt()))) + " ago"
		}

		rows = append(rows, ui.Row{id, name, status, version, addr, registered})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}

func RunnerTokenList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient(rpc.ServiceRunner)
	if err != nil {
		return err
	}
	defer client.Close()

	rc := runner_v1alpha.NewRunnerRegistrationClient(client)

	res, err := rc.ListInvites(ctx)
	if err != nil {
		return err
	}

	invites := res.Invites()

	if opts.IsJSON() {
		type InviteJSON struct {
			ID              string   `json:"id"`
			Name            string   `json:"name,omitempty"`
			Status          string   `json:"status"`
			Reusable        bool     `json:"reusable"`
			Labels          []string `json:"labels,omitempty"`
			ExpiresAt       string   `json:"expires_at,omitempty"`
			CreatedAt       string   `json:"created_at"`
			ClaimedBy       string   `json:"claimed_by,omitempty"`
			ClaimedAt       string   `json:"claimed_at,omitempty"`
			EnrollmentCount int64    `json:"enrollment_count"`
		}

		var output []InviteJSON
		for _, inv := range invites {
			ij := InviteJSON{
				ID:              inv.Id(),
				Name:            inv.Name(),
				Status:          strings.TrimPrefix(inv.Status(), "status."),
				Reusable:        inv.Reusable(),
				Labels:          inv.Labels(),
				ExpiresAt:       formatExpiresAt(inv),
				CreatedAt:       standard.FromTimestamp(inv.CreatedAt()).Format(time.RFC3339),
				EnrollmentCount: inv.EnrollmentCount(),
			}
			if inv.ClaimedBy() != "" {
				ij.ClaimedBy = inv.ClaimedBy()
				ij.ClaimedAt = standard.FromTimestamp(inv.ClaimedAt()).Format(time.RFC3339)
			}
			output = append(output, ij)
		}
		return PrintJSON(output)
	}

	if len(invites) == 0 {
		ctx.Printf("No tokens\n")
		return nil
	}

	headers := []string{"ID", "NAME", "TYPE", "STATUS", "LABELS", "EXPIRES", "ENROLLED"}
	var rows []ui.Row

	for _, inv := range invites {
		id := inv.Id()
		if len(id) > 12 {
			id = id[:12]
		}

		name := inv.Name()
		if name == "" {
			name = "-"
		}

		invType := "one-time"
		if inv.Reusable() {
			invType = "reusable"
		}

		status := strings.TrimPrefix(inv.Status(), "status.")
		switch status {
		case "pending":
			status = infoLabel.Render(status)
		case "claimed":
			status = infoGreen.Render(status)
		case "revoked":
			status = infoGray.Render(status)
		case "expired":
			status = infoGray.Render(status)
		}

		labels := "-"
		if len(inv.Labels()) > 0 {
			labels = strings.Join(inv.Labels(), ", ")
		}

		expires := "never"
		if inv.HasExpiresAt() {
			expiresAt := standard.FromTimestamp(inv.ExpiresAt())
			if expiresAt.IsZero() {
				expires = "never"
			} else if time.Now().After(expiresAt) {
				expires = infoGray.Render("expired")
			} else {
				expires = formatDuration(time.Until(expiresAt))
			}
		}

		enrolled := fmt.Sprintf("%d", inv.EnrollmentCount())

		rows = append(rows, ui.Row{id, name, invType, status, labels, expires, enrolled})
	}

	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}

func formatExpiresAt(inv *runner_v1alpha.InviteInfo) string {
	if !inv.HasExpiresAt() {
		return ""
	}
	t := standard.FromTimestamp(inv.ExpiresAt())
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
