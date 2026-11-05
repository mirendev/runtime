package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func RouteList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := routeRPCClient(ctx)
	if err != nil {
		return err
	}

	res, err := ingress_v1alpha.NewRoutesClient(client).List(ctx, "")
	if err != nil {
		return err
	}

	routes := res.Routes()

	if opts.IsJSON() {
		type RouteInfo struct {
			Host           string `json:"host"`
			App            string `json:"app"`
			Service        string `json:"service"`
			Default        bool   `json:"default"`
			WafLevel       int    `json:"waf_level"`
			RequestTimeout string `json:"request_timeout,omitempty"`
			CreatedAt      int64  `json:"created_at"`
			UpdatedAt      int64  `json:"updated_at"`

			Maintenance          bool   `json:"maintenance"`
			MaintenanceReason    string `json:"maintenance_reason,omitempty"`
			MaintenanceBackAt    string `json:"maintenance_back_at,omitempty"`
			MaintenanceStartedAt string `json:"maintenance_started_at,omitempty"`
			MaintenanceStartedBy string `json:"maintenance_started_by,omitempty"`
		}

		var routeInfos []RouteInfo
		for _, r := range routes {
			host := r.Host()
			if host == "" && r.DefaultRoute() {
				host = "(default)"
			}
			app := r.App()
			if app != "" {
				app = "app/" + app
			}
			routeInfos = append(routeInfos, RouteInfo{
				Host:           host,
				App:            app,
				Service:        r.Service(),
				Default:        r.DefaultRoute(),
				WafLevel:       int(r.WafLevel()),
				RequestTimeout: r.RequestTimeout(),
				CreatedAt:      r.CreatedAt(),
				UpdatedAt:      r.UpdatedAt(),

				Maintenance:          r.Maintenance(),
				MaintenanceReason:    r.MaintenanceReason(),
				MaintenanceBackAt:    r.MaintenanceBackAt(),
				MaintenanceStartedAt: r.MaintenanceStartedAt(),
				MaintenanceStartedBy: r.MaintenanceStartedBy(),
			})
		}

		return PrintJSON(routeInfos)
	}

	var rows []ui.Row
	headers := []string{"ROUTE", "APP", "SERVICE", "DEFAULT", "WAF", "TIMEOUT", "SERVING", "CREATED", "UPDATED"}

	for _, route := range routes {
		host := route.Host()
		if host == "" && route.DefaultRoute() {
			host = "(default)"
		}
		if host == "" {
			host = "-"
		}

		appDisplay := route.App()

		defaultDisplay := "-"
		if route.DefaultRoute() {
			defaultDisplay = "✓"
		}

		wafDisplay := "-"
		if wafLevel := route.WafLevel(); wafLevel > 0 {
			wafDisplay = fmt.Sprintf("%d", wafLevel)
		}

		timeoutDisplay := "-"
		if route.RequestTimeout() != "" {
			timeoutDisplay = route.RequestTimeout()
		}

		servingDisplay := "✓"
		if route.Maintenance() {
			servingDisplay = "maintenance"
		}

		rows = append(rows, ui.Row{
			host,
			appDisplay,
			route.Service(),
			defaultDisplay,
			wafDisplay,
			timeoutDisplay,
			servingDisplay,
			humanFriendlyTimestamp(time.UnixMilli(route.CreatedAt())),
			humanFriendlyTimestamp(time.UnixMilli(route.UpdatedAt())),
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
