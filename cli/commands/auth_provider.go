package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func AuthProviderList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := providerRPCClient(ctx)
	if err != nil {
		return err
	}
	res, err := ingress_v1alpha.NewIdentityProvidersClient(client).List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list identity providers: %w", err)
	}
	providers := res.Providers()
	if opts.IsJSON() {
		type ProviderJSON struct {
			Name        string   `json:"name"`
			Type        string   `json:"type"`
			ProviderURL string   `json:"provider_url,omitempty"`
			ClientID    string   `json:"client_id,omitempty"`
			Scopes      []string `json:"scopes,omitempty"`
			ConfigJSON  string   `json:"config_json,omitempty"`
		}
		items := make([]ProviderJSON, 0, len(providers))
		for _, p := range providers {
			items = append(items, ProviderJSON{
				Name: p.Name(), Type: p.Kind(), ProviderURL: p.ProviderUrl(),
				ClientID: p.ClientId(), Scopes: p.Scopes(), ConfigJSON: providerConfigJSON(p),
			})
		}
		return PrintJSON(items)
	}
	if len(providers) == 0 {
		ctx.Printf("No identity providers found\n")
		return nil
	}
	var rows []ui.Row
	headers := []string{"NAME", "TYPE", "DETAILS"}
	for _, p := range providers {
		details := p.ProviderUrl()
		if p.Kind() == "github" {
			details = summarizeProviderOrgs(p.Orgs())
		}
		rows = append(rows, ui.Row{p.Name(), p.Kind(), details})
	}
	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0).NoTruncate(1))
	table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
	ctx.Printf("%s\n", table.Render())
	return nil
}

func AuthProviderShow(ctx *Context, opts struct {
	Name string `position:"0" usage:"Name of the identity provider" required:"true"`
	FormatOptions
	ConfigCentric
}) error {
	if opts.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	client, err := providerRPCClient(ctx)
	if err != nil {
		return err
	}
	res, err := ingress_v1alpha.NewIdentityProvidersClient(client).Show(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to get identity provider: %w", err)
	}
	p := res.Provider()
	if opts.IsJSON() {
		switch p.Kind() {
		case "password":
			return PrintJSON(struct {
				Name string `json:"name"`
				Type string `json:"type"`
			}{p.Name(), p.Kind()})
		case "oidc":
			return PrintJSON(struct {
				Name        string   `json:"name"`
				Type        string   `json:"type"`
				ProviderURL string   `json:"provider_url"`
				ClientID    string   `json:"client_id"`
				Scopes      []string `json:"scopes"`
			}{p.Name(), p.Kind(), p.ProviderUrl(), p.ClientId(), p.Scopes()})
		default:
			return PrintJSON(struct {
				Name       string `json:"name"`
				Type       string `json:"type"`
				ClientID   string `json:"client_id"`
				ConfigJSON string `json:"config_json,omitempty"`
			}{p.Name(), p.Kind(), p.ClientId(), providerConfigJSON(p)})
		}
	}
	items := []ui.NamedValue{
		ui.NewNamedValue("Name", p.Name()),
		ui.NewNamedValue("Type", p.Kind()),
	}
	if p.Kind() == "oidc" {
		items = append(items,
			ui.NewNamedValue("Provider URL", p.ProviderUrl()),
			ui.NewNamedValue("Client ID", p.ClientId()),
			ui.NewNamedValue("Scopes", strings.Join(p.Scopes(), " ")))
	} else if p.Kind() != "password" {
		items = append(items, ui.NewNamedValue("Client ID", p.ClientId()))
		if summary := summarizeProviderOrgs(p.Orgs()); summary != "" {
			items = append(items, ui.NewNamedValue("Config", summary))
		}
	}
	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())
	return nil
}

func AuthProviderRemove(ctx *Context, opts struct {
	Name  string `position:"0" usage:"Name of the identity provider to remove" required:"true"`
	Force bool   `long:"force" description:"Remove the provider even if it is attached to routes"`
	ConfigCentric
}) error {
	if opts.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	client, err := providerRPCClient(ctx)
	if err != nil {
		return err
	}
	ic := ingress_v1alpha.NewIdentityProvidersClient(client)
	res, err := ic.Show(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to get identity provider: %w", err)
	}
	label := res.Provider().Kind() + " provider"
	if _, err := ic.Remove(ctx, opts.Name, opts.Force); err != nil {
		return fmt.Errorf("failed to remove %s: %w", label, err)
	}
	ctx.Printf("Removed %s: %s\n", label, opts.Name)
	return nil
}
