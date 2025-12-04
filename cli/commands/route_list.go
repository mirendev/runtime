package commands

import (
	"time"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/ui"
)

func RouteList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	ic := ingress.NewClient(ctx.Log, client)

	routes, err := ic.List(ctx)
	if err != nil {
		return err
	}

	if opts.IsJSON() {
		type RouteInfo struct {
			Host      string `json:"host"`
			App       string `json:"app"`
			Default   bool   `json:"default"`
			CreatedAt int64  `json:"created_at"`
			UpdatedAt int64  `json:"updated_at"`
		}

		var routeInfos []RouteInfo
		for _, r := range routes {
			host := r.Route.Host
			if host == "" && r.Route.Default {
				host = "(default)"
			}
			routeInfos = append(routeInfos, RouteInfo{
				Host:      host,
				App:       string(r.Route.App),
				Default:   r.Route.Default,
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
			})
		}

		return PrintJSON(routeInfos)
	}

	var rows []ui.Row
	headers := []string{"HOST", "APP", "DEFAULT", "CREATED", "UPDATED"}

	for _, r := range routes {
		route := r.Route

		// Display host or "(default)" for default routes
		host := route.Host
		if host == "" && route.Default {
			host = "(default)"
		}
		if host == "" {
			host = "-"
		}

		// Show cleaned app ID
		appDisplay := ui.CleanEntityID(string(route.App))

		// Show default status
		defaultDisplay := "-"
		if route.Default {
			defaultDisplay = "✓"
		}

		rows = append(rows, ui.Row{
			host,
			appDisplay,
			defaultDisplay,
			humanFriendlyTimestamp(time.UnixMilli(r.CreatedAt)),
			humanFriendlyTimestamp(time.UnixMilli(r.UpdatedAt)),
		})
	}

	if len(rows) == 0 {
		ctx.Printf("No routes found\n")
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
