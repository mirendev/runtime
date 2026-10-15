package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/ui"
)

func AppVersions(ctx *Context, opts struct {
	AppCentric
	FormatOptions

	Ephemeral bool `long:"ephemeral" description:"Show only ephemeral versions"`
	Limit     int  `short:"n" long:"limit" description:"Max versions to show" default:"20"`
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}
	defer client.Close()

	crud := app_v1alpha.NewCrudClient(client)
	result, err := crud.ListVersions(ctx, opts.App, opts.Ephemeral, int64(opts.Limit))
	if err != nil {
		return fmt.Errorf("failed to list app versions: %w", err)
	}

	versions := result.Versions()
	if opts.IsJSON() {
		return printAppVersionsJSON(versions)
	}

	if opts.Ephemeral {
		return printEphemeralVersionsTable(ctx, versions)
	}
	return printAllVersionsTable(ctx, versions)
}

func printAppVersionsJSON(versions []*app_v1alpha.AppVersionSummary) error {
	type versionJSON struct {
		ID             string `json:"id"`
		Version        string `json:"version"`
		Status         string `json:"status"`
		CreatedAt      string `json:"created_at"`
		EphemeralLabel string `json:"ephemeral_label,omitempty"`
		EphemeralTTL   string `json:"ephemeral_ttl,omitempty"`
		ExpiresAt      string `json:"expires_at,omitempty"`
	}

	var items []versionJSON
	for _, v := range versions {
		item := versionJSON{
			ID:        v.Id(),
			Version:   v.Version(),
			Status:    v.Status(),
			CreatedAt: standard.FromTimestamp(v.CreatedAt()).UTC().Format(time.RFC3339),
		}
		if v.EphemeralLabel() != "" {
			item.EphemeralLabel = v.EphemeralLabel()
			item.EphemeralTTL = v.EphemeralTtl()
			if expires := standard.FromTimestamp(v.ExpiresAt()); !expires.IsZero() {
				item.ExpiresAt = expires.UTC().Format(time.RFC3339)
			}
		}
		items = append(items, item)
	}
	return PrintJSON(items)
}

func printAllVersionsTable(ctx *Context, versions []*app_v1alpha.AppVersionSummary) error {
	headers := []string{"VERSION", "STATUS", "CREATED", "LABEL"}
	var rows []ui.Row

	for _, v := range versions {
		label := ""
		if v.EphemeralLabel() != "" {
			remaining := time.Until(standard.FromTimestamp(v.ExpiresAt()))
			if remaining > 0 {
				label = fmt.Sprintf("%s (expires in %s)", v.EphemeralLabel(), formatVersionDuration(remaining))
			} else {
				label = fmt.Sprintf("%s (expired)", v.EphemeralLabel())
			}
		}

		rows = append(rows, ui.Row{
			v.Version(),
			v.Status(),
			humanFriendlyTimestamp(standard.FromTimestamp(v.CreatedAt())),
			label,
		})
	}

	if len(rows) == 0 {
		ctx.Printf("No versions found.\n")
		return nil
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)
	ctx.Printf("%s\n", table.Render())
	return nil
}

func printEphemeralVersionsTable(ctx *Context, versions []*app_v1alpha.AppVersionSummary) error {
	headers := []string{"VERSION", "LABEL", "CREATED", "EXPIRES"}
	var rows []ui.Row

	for _, v := range versions {
		expires := "-"
		if expiry := standard.FromTimestamp(v.ExpiresAt()); !expiry.IsZero() {
			expires = expiry.Format("2006-01-02 15:04:05")
		}

		rows = append(rows, ui.Row{
			v.Version(),
			v.EphemeralLabel(),
			humanFriendlyTimestamp(standard.FromTimestamp(v.CreatedAt())),
			expires,
		})
	}

	if len(rows) == 0 {
		ctx.Printf("No ephemeral versions found.\n")
		return nil
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)
	ctx.Printf("%s\n", table.Render())
	return nil
}

func formatVersionDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
