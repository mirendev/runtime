package commands

import (
	"fmt"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func RouteShow(ctx *Context, opts struct {
	Host    string `position:"0" usage:"Hostname of the route to show; omit and pass --default for the default route"`
	Default bool   `long:"default" description:"Show the default route (instead of a hostname)"`
	FormatOptions
	ConfigCentric
}) error {
	if opts.Host == "" && !opts.Default {
		return fmt.Errorf("either a hostname or --default must be specified")
	}
	if opts.Host != "" && opts.Default {
		return fmt.Errorf("--default cannot be used with a hostname")
	}
	client, err := routeRPCClient(ctx)
	if err != nil {
		return err
	}
	res, err := ingress_v1alpha.NewRoutesClient(client).Show(ctx, opts.Host, opts.Default)
	if err != nil {
		return err
	}
	route := res.Route()
	routeLabel := opts.Host
	if opts.Default {
		routeLabel = "default"
	}
	protectionType := route.ProviderType()
	if protectionType == "" {
		protectionType = "none"
	}

	if opts.IsJSON() {
		type RouteJSON struct {
			Host            string              `json:"host"`
			App             string              `json:"app"`
			Service         string              `json:"service"`
			Default         bool                `json:"default"`
			Protected       bool                `json:"protected"`
			ProtectionType  string              `json:"protection_type"`
			ProviderName    string              `json:"provider_name,omitempty"`
			ProviderURL     string              `json:"provider_url,omitempty"`
			ConnectorType   string              `json:"connector_type,omitempty"`
			ProviderMissing bool                `json:"provider_missing,omitempty"`
			ClaimMappings   []map[string]string `json:"claim_mappings,omitempty"`
			WafLevel        int                 `json:"waf_level"`
			RequestTimeout  string              `json:"request_timeout,omitempty"`
			TLSCheck        string              `json:"tls_check,omitempty"`

			Maintenance          bool   `json:"maintenance"`
			MaintenanceReason    string `json:"maintenance_reason,omitempty"`
			MaintenanceBackAt    string `json:"maintenance_back_at,omitempty"`
			MaintenanceStartedAt string `json:"maintenance_started_at,omitempty"`
			MaintenanceStartedBy string `json:"maintenance_started_by,omitempty"`
		}
		r := RouteJSON{
			Host: routeLabel, App: route.App(), Service: route.Service(), Default: route.DefaultRoute(),
			Protected: route.Protected(), ProtectionType: protectionType, WafLevel: int(route.WafLevel()),
			RequestTimeout: route.RequestTimeout(), TLSCheck: route.TlsCheck(),
			Maintenance: route.Maintenance(), MaintenanceReason: route.MaintenanceReason(),
			MaintenanceBackAt: route.MaintenanceBackAt(), MaintenanceStartedAt: route.MaintenanceStartedAt(),
			MaintenanceStartedBy: route.MaintenanceStartedBy(),
		}
		if route.Protected() {
			r.ProviderName = route.Provider()
			r.ProviderMissing = route.Provider() == ""
			switch protectionType {
			case "oidc":
				r.ProviderURL = route.ProviderUrl()
			case "connector":
				r.ConnectorType = route.ConnectorType()
			}
			if protectionType == "oidc" || protectionType == "connector" {
				for _, m := range route.ClaimMappings() {
					r.ClaimMappings = append(r.ClaimMappings, map[string]string{"claim": m.Claim(), "header": m.Header()})
				}
			}
		}
		return PrintJSON(r)
	}

	ctx.Printf("Route: %s\n", routeLabel)
	ctx.Printf("  App:       %s\n", route.App())
	ctx.Printf("  Service:   %s\n", route.Service())
	ctx.Printf("  Default:   %v\n", route.DefaultRoute())
	ctx.Printf("  Protected: %v\n", route.Protected())
	if route.WafLevel() > 0 {
		ctx.Printf("  WAF Level: %d\n", route.WafLevel())
	}
	if route.RequestTimeout() != "" {
		ctx.Printf("  Timeout:   %s\n", route.RequestTimeout())
	}
	if route.TlsCheck() != "" {
		ctx.Printf("  TLS Check: %s\n", route.TlsCheck())
	}
	switch protectionType {
	case "oidc":
		ctx.Printf("  Type:      oidc\n")
		ctx.Printf("  Provider:  %s (%s)\n", route.Provider(), route.ProviderUrl())
	case "connector":
		ctx.Printf("  Type:      connector\n")
		ctx.Printf("  Provider:  %s (%s)\n", route.Provider(), route.ConnectorType())
	case "password":
		ctx.Printf("  Type:      password\n")
		ctx.Printf("  Provider:  %s\n", route.Provider())
	}
	if (protectionType == "oidc" || protectionType == "connector") && len(route.ClaimMappings()) > 0 {
		var rows []ui.Row
		for _, m := range route.ClaimMappings() {
			rows = append(rows, ui.Row{m.Claim(), m.Header()})
		}
		headers := []string{"CLAIM", "HEADER"}
		columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0).NoTruncate(1))
		table := ui.NewTable(ui.WithTableTitle("Claim Mappings"), ui.WithColumns(columns), ui.WithRows(rows))
		ctx.Printf("\n%s\n", table.Render())
	}
	if route.Maintenance() {
		ctx.Printf("\nMaintenance: this route is serving a holding page\n")
		if route.MaintenanceReason() != "" {
			ctx.Printf("  Reason:    %s\n", route.MaintenanceReason())
		}
		if route.MaintenanceBackAt() != "" {
			ctx.Printf("  Back at:   %s\n", route.MaintenanceBackAt())
		}
		if route.MaintenanceStartedAt() != "" {
			ctx.Printf("  Since:     %s\n", route.MaintenanceStartedAt())
		}
		if route.MaintenanceStartedBy() != "" {
			ctx.Printf("  Set by:    %s\n", route.MaintenanceStartedBy())
		}
		ctx.Printf("  Bring it back with: miren route up %s\n", upTarget(opts.Host, opts.Default))
	}
	return nil
}
