package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func RouteProtect(ctx *Context, opts struct {
	Host        string   `position:"0" usage:"Hostname for the route (e.g., example.com); omit and pass --default for the default route"`
	Default     bool     `long:"default" description:"Protect the default route (instead of a hostname)"`
	Provider    string   `long:"provider" description:"Name of the identity provider" required:"true"`
	ClaimHeader []string `long:"claim-header" description:"Claim to header mapping in format 'claim:header' (e.g., 'email:X-User-Email')"`
	FormatOptions
	ConfigCentric
}) error {
	if opts.Host == "" && !opts.Default {
		return fmt.Errorf("either a hostname or --default must be specified")
	}
	if opts.Host != "" && opts.Default {
		return fmt.Errorf("--default cannot be used with a hostname")
	}
	if opts.Provider == "" {
		return fmt.Errorf("--provider is required")
	}
	client, err := routeRPCClient(ctx)
	if err != nil {
		return err
	}
	var mappings []*ingress_v1alpha.RouteClaimMapping
	for _, mapping := range opts.ClaimHeader {
		claim, header, _ := strings.Cut(mapping, ":")
		m := &ingress_v1alpha.RouteClaimMapping{}
		m.SetClaim(strings.TrimSpace(claim))
		m.SetHeader(strings.TrimSpace(header))
		mappings = append(mappings, m)
	}
	// Provider lookup and validation belong to Routes, not the operator-only
	// IdentityProviders API. Password providers ignore even malformed mappings
	// in human output mode, but JSON mode must reject them before any mutation.
	res, err := ingress_v1alpha.NewRoutesClient(client).Protect(ctx, opts.Host, opts.Default, opts.Provider, mappings, !opts.IsJSON())
	if err != nil {
		return err
	}
	route := res.Route()
	routeLabel := opts.Host
	if opts.Default {
		routeLabel = "default"
	}
	if opts.IsJSON() {
		type RouteProtectJSON struct {
			Route         string              `json:"route"`
			Protected     bool                `json:"protected"`
			Provider      string              `json:"provider"`
			ProviderType  string              `json:"provider_type"`
			ConnectorType string              `json:"connector_type,omitempty"`
			ClaimMappings []map[string]string `json:"claim_mappings,omitempty"`
		}
		out := RouteProtectJSON{
			Route: routeLabel, Protected: true, Provider: opts.Provider,
			ProviderType: route.ProviderType(), ConnectorType: route.ConnectorType(),
		}
		for _, m := range route.ClaimMappings() {
			out.ClaimMappings = append(out.ClaimMappings, map[string]string{"claim": m.Claim(), "header": m.Header()})
		}
		return PrintJSON(out)
	}
	if route.ProviderType() == "password" && len(opts.ClaimHeader) > 0 {
		ctx.Printf("Warning: --claim-header is ignored for password providers\n")
	}
	items := []ui.NamedValue{
		ui.NewNamedValue("Route", routeLabel),
		ui.NewNamedValue("Protected", true),
		ui.NewNamedValue("Provider", opts.Provider),
		ui.NewNamedValue("Type", route.ProviderType()),
	}
	if route.ProviderType() == "connector" {
		items = append(items, ui.NewNamedValue("Connector", route.ConnectorType()))
	}
	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())
	if len(route.ClaimMappings()) > 0 {
		var rows []ui.Row
		for _, m := range route.ClaimMappings() {
			rows = append(rows, ui.Row{m.Claim(), m.Header()})
		}
		headers := []string{"CLAIM", "HEADER"}
		columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0).NoTruncate(1))
		table := ui.NewTable(ui.WithTableTitle("Claim Mappings"), ui.WithColumns(columns), ui.WithRows(rows))
		ctx.Printf("\n%s\n", table.Render())
	}
	return nil
}
