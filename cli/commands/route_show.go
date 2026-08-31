package commands

import (
	"errors"
	"fmt"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
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

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	ic := ingress.NewClient(ctx.Log, client)

	var route *ingress_v1alpha.HttpRoute
	var routeLabel string

	if opts.Default {
		route, err = ic.LookupDefault(ctx)
		if err != nil {
			return fmt.Errorf("failed to lookup default route: %w", err)
		}
		if route == nil {
			return fmt.Errorf("no default route configured")
		}
		routeLabel = "default"
	} else {
		route, err = ic.Lookup(ctx, opts.Host)
		if err != nil {
			return err
		}
		if route == nil {
			return fmt.Errorf("route not found: %s", opts.Host)
		}
		routeLabel = opts.Host
	}

	wafEnabled := !entity.Empty(route.WafProfile)
	var wafProfile *ingress_v1alpha.WafProfile
	if wafEnabled {
		wafProfile = &ingress_v1alpha.WafProfile{}
		if err := ic.GetEntityStore().GetById(ctx, route.WafProfile, wafProfile); err != nil {
			if !errors.Is(err, cond.ErrNotFound{}) {
				return fmt.Errorf("failed to get WAF profile: %w", err)
			}
			wafProfile = nil
		}
	}

	protected := !entity.Empty(route.AuthProvider)

	var oidcProvider *ingress_v1alpha.OidcProvider
	var pwProvider *ingress_v1alpha.PasswordProvider
	protectionType := "none"

	if protected {
		providerEnt, err := ic.GetEntityStore().GetByIdWithEntity(ctx, route.AuthProvider, &ingress_v1alpha.OidcProvider{})
		if err != nil {
			if !errors.Is(err, cond.ErrNotFound{}) {
				return fmt.Errorf("failed to get auth provider: %w", err)
			}
		} else {
			raw := providerEnt.Entity()
			switch {
			case entity.Is(raw, ingress_v1alpha.KindOidcProvider):
				oidcProvider = &ingress_v1alpha.OidcProvider{}
				oidcProvider.Decode(raw)
				if isConnector(oidcProvider) {
					protectionType = "connector"
				} else {
					protectionType = "oidc"
				}
			case entity.Is(raw, ingress_v1alpha.KindPasswordProvider):
				pwProvider = &ingress_v1alpha.PasswordProvider{}
				pwProvider.Decode(raw)
				protectionType = "password"
			}
		}
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

			Maintenance          bool   `json:"maintenance"`
			MaintenanceReason    string `json:"maintenance_reason,omitempty"`
			MaintenanceBackAt    string `json:"maintenance_back_at,omitempty"`
			MaintenanceStartedAt string `json:"maintenance_started_at,omitempty"`
			MaintenanceStartedBy string `json:"maintenance_started_by,omitempty"`
		}

		wafLevel := 0
		if wafProfile != nil {
			wafLevel = int(wafProfile.ParanoiaLevel)
		}

		r := RouteJSON{
			Host:           routeLabel,
			App:            ui.CleanEntityID(string(route.App)),
			Service:        routeService(route),
			Default:        route.Default,
			Protected:      protected,
			ProtectionType: protectionType,
			WafLevel:       wafLevel,
			RequestTimeout: route.RequestTimeout,

			Maintenance:          !route.Maintenance.Empty(),
			MaintenanceReason:    route.Maintenance.Reason,
			MaintenanceBackAt:    route.Maintenance.BackAt,
			MaintenanceStartedAt: route.Maintenance.StartedAt,
			MaintenanceStartedBy: route.Maintenance.StartedBy,
		}

		if protected {
			switch {
			case oidcProvider != nil:
				r.ProviderName = oidcProvider.Name
				if isConnector(oidcProvider) {
					r.ConnectorType = oidcProvider.ConnectorType
				} else {
					r.ProviderURL = oidcProvider.ProviderUrl
				}
				for _, m := range route.ClaimMappings {
					r.ClaimMappings = append(r.ClaimMappings, map[string]string{
						"claim":  m.Claim,
						"header": m.Header,
					})
				}
			case pwProvider != nil:
				r.ProviderName = pwProvider.Name
			default:
				r.ProviderMissing = true
			}
		}

		return PrintJSON(r)
	}

	ctx.Printf("Route: %s\n", routeLabel)
	ctx.Printf("  App:       %s\n", ui.CleanEntityID(string(route.App)))
	ctx.Printf("  Service:   %s\n", routeService(route))
	ctx.Printf("  Default:   %v\n", route.Default)
	ctx.Printf("  Protected: %v\n", protected)
	if wafProfile != nil {
		ctx.Printf("  WAF Level: %d\n", wafProfile.ParanoiaLevel)
	}
	if route.RequestTimeout != "" {
		ctx.Printf("  Timeout:   %s\n", route.RequestTimeout)
	}

	switch protectionType {
	case "oidc":
		ctx.Printf("  Type:      oidc\n")
		if oidcProvider != nil {
			ctx.Printf("  Provider:  %s (%s)\n", oidcProvider.Name, oidcProvider.ProviderUrl)
		} else {
			ctx.Printf("  Provider:  <missing — provider has been deleted>\n")
		}

		if len(route.ClaimMappings) > 0 {
			var rows []ui.Row
			for _, m := range route.ClaimMappings {
				rows = append(rows, ui.Row{m.Claim, m.Header})
			}

			headers := []string{"CLAIM", "HEADER"}
			columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0).NoTruncate(1))
			table := ui.NewTable(
				ui.WithTableTitle("Claim Mappings"),
				ui.WithColumns(columns),
				ui.WithRows(rows),
			)

			ctx.Printf("\n%s\n", table.Render())
		}
	case "password":
		ctx.Printf("  Type:      password\n")
		if pwProvider != nil {
			ctx.Printf("  Provider:  %s\n", pwProvider.Name)
		} else {
			ctx.Printf("  Provider:  <missing — provider has been deleted>\n")
		}
	case "connector":
		ctx.Printf("  Type:      connector\n")
		if oidcProvider != nil {
			ctx.Printf("  Provider:  %s (%s)\n", oidcProvider.Name, oidcProvider.ConnectorType)
		} else {
			ctx.Printf("  Provider:  <missing — provider has been deleted>\n")
		}

		if len(route.ClaimMappings) > 0 {
			var rows []ui.Row
			for _, m := range route.ClaimMappings {
				rows = append(rows, ui.Row{m.Claim, m.Header})
			}

			headers := []string{"CLAIM", "HEADER"}
			columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0).NoTruncate(1))
			table := ui.NewTable(
				ui.WithTableTitle("Claim Mappings"),
				ui.WithColumns(columns),
				ui.WithRows(rows),
			)

			ctx.Printf("\n%s\n", table.Render())
		}
	}

	if !route.Maintenance.Empty() {
		ctx.Printf("\nMaintenance: this route is serving a holding page\n")
		if route.Maintenance.Reason != "" {
			ctx.Printf("  Reason:    %s\n", route.Maintenance.Reason)
		}
		if route.Maintenance.BackAt != "" {
			ctx.Printf("  Back at:   %s\n", route.Maintenance.BackAt)
		}
		if route.Maintenance.StartedAt != "" {
			ctx.Printf("  Since:     %s\n", route.Maintenance.StartedAt)
		}
		if route.Maintenance.StartedBy != "" {
			ctx.Printf("  Set by:    %s\n", route.Maintenance.StartedBy)
		}
		ctx.Printf("  Bring it back with: miren route up %s\n", upTarget(opts.Host, opts.Default))
	}

	return nil
}
