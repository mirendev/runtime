package commands

import (
	"fmt"
	"slices"
	"strings"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func AuthProviderAddOIDC(ctx *Context, opts struct {
	Name         string   `position:"0" usage:"Name for this identity provider" required:"true"`
	ProviderURL  string   `long:"provider-url" description:"OIDC provider URL (e.g., https://accounts.google.com)" required:"true"`
	ClientID     string   `long:"client-id" description:"OAuth2 client ID" required:"true"`
	ClientSecret string   `long:"client-secret" description:"OAuth2 client secret" required:"true"`
	Scopes       []string `long:"scope" description:"OAuth2 scopes (can be specified multiple times)"`
	Update       bool     `long:"update" description:"Overwrite an existing provider with the same name (rotates client secret)"`
	ConfigCentric
}) error {

	if opts.Name == "" {
		return fmt.Errorf("provider name is required")
	}

	if opts.ProviderURL == "" {
		return fmt.Errorf("--provider-url is required")
	}

	if opts.ClientID == "" || opts.ClientSecret == "" {
		return fmt.Errorf("--client-id and --client-secret are required")
	}

	// "openid" must be in the scope list for the OIDC handshake to succeed.
	// Users frequently forget it, so prepend it if it's missing.
	hasOpenID := slices.Contains(opts.Scopes, "openid")
	scopeList := opts.Scopes
	if !hasOpenID {
		scopeList = append([]string{"openid"}, scopeList...)
	}
	scopes := strings.Join(scopeList, " ")

	client, err := providerRPCClient(ctx)
	if err != nil {
		return err
	}

	_, err = ingress_v1alpha.NewIdentityProvidersClient(client).AddOIDC(ctx,
		opts.Name, opts.ProviderURL, opts.ClientID, opts.ClientSecret, scopeList, opts.Update)
	if err != nil {
		return fmt.Errorf("failed to create identity provider: %w", err)
	}

	items := []ui.NamedValue{
		ui.NewNamedValue("Name", opts.Name),
		ui.NewNamedValue("Type", "oidc"),
		ui.NewNamedValue("Provider URL", opts.ProviderURL),
		ui.NewNamedValue("Client ID", opts.ClientID),
		ui.NewNamedValue("Scopes", scopes),
	}

	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())
	return nil
}
