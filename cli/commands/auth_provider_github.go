package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func AuthProviderAddGitHub(ctx *Context, opts struct {
	Name         string   `position:"0" usage:"Name for this identity provider" required:"true"`
	ClientID     string   `long:"client-id" description:"GitHub OAuth app client ID" required:"true"`
	ClientSecret string   `long:"client-secret" description:"GitHub OAuth app client secret" required:"true"`
	Orgs         []string `long:"org" split:"false" description:"GitHub org restriction (repeatable). Use \"name\" for any-member, or \"name:team1,team2\" to require team membership and populate X-User-Groups."`
	Update       bool     `long:"update" description:"Overwrite an existing provider with the same name (rotates client secret)"`
	ConfigCentric
}) error {

	if opts.Name == "" {
		return fmt.Errorf("provider name is required")
	}

	if opts.ClientID == "" || opts.ClientSecret == "" {
		return fmt.Errorf("--client-id and --client-secret are required")
	}

	orgs, err := parseProviderOrgs(opts.Orgs)
	if err != nil {
		return err
	}

	client, err := providerRPCClient(ctx)
	if err != nil {
		return err
	}

	if _, err := ingress_v1alpha.NewIdentityProvidersClient(client).AddGitHub(ctx,
		opts.Name, opts.ClientID, opts.ClientSecret, orgs, opts.Update); err != nil {
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

// parseProviderOrgs accepts name or name:team1,team2. Bare names authorize
// membership without emitting groups; only team-prefixed Dex entries do that.
func parseProviderOrgs(orgs []string) ([]*ingress_v1alpha.GitHubOrganization, error) {
	var result []*ingress_v1alpha.GitHubOrganization
	for _, spec := range orgs {
		name, teamsPart, hasTeams := strings.Cut(spec, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("invalid --org %q: empty org name", spec)
		}
		org := &ingress_v1alpha.GitHubOrganization{}
		org.SetName(name)
		if hasTeams {
			var teams []string
			for t := range strings.SplitSeq(teamsPart, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					teams = append(teams, t)
				}
			}
			if len(teams) == 0 {
				return nil, fmt.Errorf("invalid --org %q: \":\" present but no teams listed", spec)
			}
			org.SetTeams(teams)
		}
		result = append(result, org)
	}
	return result, nil
}
