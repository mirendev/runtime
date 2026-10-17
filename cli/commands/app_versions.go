package commands

import (
	"fmt"
	"sort"
	"time"

	"miren.dev/runtime/api/app"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/ui"
)

func AppVersions(ctx *Context, opts struct {
	AppCentric
	FormatOptions

	Ephemeral bool `long:"ephemeral" description:"Show only ephemeral versions"`
	Limit     int  `short:"n" long:"limit" description:"Max versions to show" default:"20"`
}) error {
	versions, err := fetchAppVersions(ctx, opts.App, opts.Ephemeral, opts.Limit)
	if err != nil {
		return err
	}
	if opts.IsJSON() {
		return printAppVersionsJSON(versions)
	}
	if opts.Ephemeral {
		return printEphemeralVersionsTable(ctx, versions)
	}
	return printAllVersionsTable(ctx, versions)
}

func fetchAppVersions(ctx *Context, name string, ephemeral bool, limit int) ([]*app_v1alpha.AppVersionSummary, error) {
	client, err := ctx.RPCClient("dev.miren.runtime/app")
	if err == nil {
		defer client.Close()
		err = requireRPCMethod(ctx, client, "listVersions")
	}
	if isMissingRPC(err) {
		return fetchAppVersionsLegacy(ctx, name, ephemeral, limit)
	}
	if err != nil {
		return nil, err
	}

	crud := app_v1alpha.NewCrudClient(client)
	result, err := crud.ListVersions(ctx, name, ephemeral, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to list app versions: %w", err)
	}
	return result.Versions(), nil
}

func fetchAppVersionsLegacy(ctx *Context, name string, ephemeral bool, limit int) ([]*app_v1alpha.AppVersionSummary, error) {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return nil, err
	}
	defer client.Close()
	appEntity, err := app.NewClient(ctx.Log, client).GetByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get app %q: %w", name, err)
	}
	ec := entityserver.NewClient(ctx.Log, entityserver_v1alpha.NewEntityAccessClient(client))
	lr, err := ec.List(ctx, entity.Ref(core_v1alpha.AppVersionAppId, appEntity.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to list app versions: %w", err)
	}
	var versions []*app_v1alpha.AppVersionSummary
	for lr.Next() {
		var av core_v1alpha.AppVersion
		if err := lr.Read(&av); err != nil {
			continue
		}
		if ephemeral && av.EphemeralLabel == "" {
			continue
		}
		status := "replaced"
		if av.ID == appEntity.ActiveVersion {
			status = "active"
		} else if av.EphemeralLabel != "" {
			status = "ephemeral"
		}
		v := &app_v1alpha.AppVersionSummary{}
		v.SetId(string(av.ID))
		v.SetVersion(av.Version)
		v.SetStatus(status)
		v.SetCreatedAt(standard.ToTimestamp(lr.Entity().GetCreatedAt()))
		v.SetEphemeralLabel(av.EphemeralLabel)
		v.SetEphemeralTtl(av.EphemeralTtl)
		if !av.EphemeralExpiresAt.IsZero() {
			v.SetExpiresAt(standard.ToTimestamp(av.EphemeralExpiresAt))
		}
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return standard.FromTimestamp(versions[i].CreatedAt()).After(standard.FromTimestamp(versions[j].CreatedAt()))
	})
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}
	return versions, nil
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
