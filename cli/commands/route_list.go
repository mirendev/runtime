package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
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

	resolveWAFLevel := func(route *ingress_v1alpha.HttpRoute) int {
		if entity.Empty(route.WafProfile) {
			return 0
		}
		profile, err := ic.GetWAFProfileByID(ctx, route.WafProfile)
		if err != nil || profile == nil {
			return 0
		}
		return int(profile.ParanoiaLevel)
	}

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
			host := r.Route.Host
			if host == "" && r.Route.Default {
				host = "(default)"
			}
			routeInfos = append(routeInfos, RouteInfo{
				Host:           host,
				App:            string(r.Route.App),
				Service:        routeService(r.Route),
				Default:        r.Route.Default,
				WafLevel:       resolveWAFLevel(r.Route),
				RequestTimeout: r.Route.RequestTimeout,
				CreatedAt:      r.CreatedAt,
				UpdatedAt:      r.UpdatedAt,

				Maintenance:          !r.Route.Maintenance.Empty(),
				MaintenanceReason:    r.Route.Maintenance.Reason,
				MaintenanceBackAt:    r.Route.Maintenance.BackAt,
				MaintenanceStartedAt: r.Route.Maintenance.StartedAt,
				MaintenanceStartedBy: r.Route.Maintenance.StartedBy,
			})
		}

		return PrintJSON(routeInfos)
	}

	var rows []ui.Row
	headers := []string{"HOST", "APP", "SERVICE", "DEFAULT", "WAF", "TIMEOUT", "SERVING", "CREATED", "UPDATED"}

	for _, r := range routes {
		route := r.Route

		host := route.Host
		if host == "" && route.Default {
			host = "(default)"
		}
		if host == "" {
			host = "-"
		}

		appDisplay := ui.CleanEntityID(string(route.App))

		defaultDisplay := "-"
		if route.Default {
			defaultDisplay = "✓"
		}

		wafDisplay := "-"
		if wafLevel := resolveWAFLevel(route); wafLevel > 0 {
			wafDisplay = fmt.Sprintf("%d", wafLevel)
		}

		timeoutDisplay := "-"
		if route.RequestTimeout != "" {
			timeoutDisplay = route.RequestTimeout
		}

		servingDisplay := "✓"
		if !route.Maintenance.Empty() {
			servingDisplay = "maintenance"
		}

		rows = append(rows, ui.Row{
			host,
			appDisplay,
			routeService(route),
			defaultDisplay,
			wafDisplay,
			timeoutDisplay,
			servingDisplay,
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

func routeService(route *ingress_v1alpha.HttpRoute) string {
	if route.Service == "" {
		return "web"
	}
	return route.Service
}
