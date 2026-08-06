package commands

import (
	"fmt"
	"slices"
	"strings"

	"miren.dev/runtime/api/ingress"
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

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	ic := ingress.NewClient(ctx.Log, client)

	existing, err := ic.GetOIDCProvider(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for existing identity provider: %w", err)
	}
	if existing != nil && !opts.Update {
		return fmt.Errorf("identity provider %q already exists. Pass --update to overwrite (rotates client secret)", opts.Name)
	}

	pwExisting, err := ic.GetPasswordProvider(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for existing password provider: %w", err)
	}
	if pwExisting != nil {
		return fmt.Errorf("a password provider named %q already exists. Provider names must be unique across types", opts.Name)
	}

	provider := &ingress_v1alpha.OidcProvider{
		Name:         opts.Name,
		ProviderUrl:  opts.ProviderURL,
		ClientId:     opts.ClientID,
		ClientSecret: opts.ClientSecret,
		Scopes:       scopes,
	}

	_, err = ic.CreateOrUpdateOIDCProvider(ctx, provider)
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
