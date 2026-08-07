package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/connectors"
	"miren.dev/runtime/pkg/ui"
)

func AuthProviderAddGitHub(ctx *Context, opts struct {
	Name         string   `position:"0" usage:"Name for this identity provider" required:"true"`
	ClientID     string   `long:"client-id" description:"GitHub OAuth app client ID" required:"true"`
	ClientSecret string   `long:"client-secret" description:"GitHub OAuth app client secret" required:"true"`
	Orgs         []string `long:"org" description:"GitHub org restriction (repeatable). Use \"name\" for any-member, or \"name:team1,team2\" to require team membership and populate X-User-Groups."`
	Update       bool     `long:"update" description:"Overwrite an existing provider with the same name (rotates client secret)"`
	ConfigCentric
}) error {

	if opts.Name == "" {
		return fmt.Errorf("provider name is required")
	}

	if opts.ClientID == "" || opts.ClientSecret == "" {
		return fmt.Errorf("--client-id and --client-secret are required")
	}

	configJSON, err := buildGitHubConfigJSON(opts.Orgs)
	if err != nil {
		return err
	}

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	ic := ingress.NewClient(ctx.Log, client)

	existing, err := ic.GetOIDCProvider(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for existing auth provider: %w", err)
	}
	if existing != nil && !opts.Update {
		return fmt.Errorf("auth provider %q already exists. Pass --update to overwrite (rotates client secret)", opts.Name)
	}

	pwExisting, err := ic.GetPasswordProvider(ctx, opts.Name)
	if err != nil {
		return fmt.Errorf("failed to check for existing password provider: %w", err)
	}
	if pwExisting != nil {
		return fmt.Errorf("a password provider named %q already exists. Provider names must be unique across types", opts.Name)
	}

	provider := &ingress_v1alpha.OidcProvider{
		Name:          opts.Name,
		ConnectorType: "github",
		ClientId:      opts.ClientID,
		ClientSecret:  opts.ClientSecret,
		ConfigJson:    configJSON,
	}

	if _, err := ic.CreateOrUpdateOIDCProvider(ctx, provider); err != nil {
		return fmt.Errorf("failed to create github provider: %w", err)
	}

	items := []ui.NamedValue{
		ui.NewNamedValue("Name", opts.Name),
		ui.NewNamedValue("Type", "github"),
		ui.NewNamedValue("Client ID", opts.ClientID),
	}
	if len(opts.Orgs) > 0 {
		items = append(items, ui.NewNamedValue("Orgs", strings.Join(opts.Orgs, ", ")))
	}

	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())
	return nil
}

// buildGitHubConfigJSON marshals --org flag inputs into the config_json blob
// stored on the OidcProvider entity for github connectors.
//
// Each --org spec takes one of:
//   - "name"               authorizes any member of the org
//   - "name:team1,team2"   restricts to listed teams and populates X-User-Groups
//
// Bare "name" authorizes but emits no groups claim because Dex only surfaces
// team-prefixed entries.
func buildGitHubConfigJSON(orgs []string) (string, error) {
	var cfg struct {
		Orgs []connectors.GitHubOrg `json:"orgs,omitempty"`
	}
	for _, spec := range orgs {
		name, teamsPart, hasTeams := strings.Cut(spec, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("invalid --org %q: empty org name", spec)
		}
		org := connectors.GitHubOrg{Name: name}
		if hasTeams {
			for t := range strings.SplitSeq(teamsPart, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					org.Teams = append(org.Teams, t)
				}
			}
			if len(org.Teams) == 0 {
				return "", fmt.Errorf("invalid --org %q: \":\" present but no teams listed", spec)
			}
		}
		cfg.Orgs = append(cfg.Orgs, org)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("encode github config: %w", err)
	}
	return string(b), nil
}
