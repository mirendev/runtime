package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/api/oidcbinding/oidcbinding_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

const gitHubActionsIssuer = "https://token.actions.githubusercontent.com"

func AuthCIAdd(ctx *Context, opts struct {
	GitHub        string `long:"github" description:"GitHub owner/repo shorthand (sets issuer, provider, and repository claim conditions)"`
	Issuer        string `long:"issuer" description:"OIDC issuer URL"`
	Subject       string `long:"subject" description:"Glob pattern for the token subject"`
	AllowedEvents string `long:"allowed-events" description:"Comma-separated event names to allow (default: push,workflow_dispatch)"`
	AllowedRefs   string `long:"allowed-refs" description:"Glob pattern for allowed git refs"`
	Description   string `long:"description" description:"Human-readable description of this binding"`
	AppCentric
}) error {
	if opts.GitHub == "" && opts.Issuer == "" {
		return fmt.Errorf("either --github or --issuer is required")
	}

	provider := "generic"
	issuer := opts.Issuer
	subject := opts.Subject

	var claimConditions []*oidcbinding_v1alpha.ClaimCondition

	if opts.GitHub != "" {
		conditions, err := gitHubClaimConditions(opts.GitHub, opts.AllowedEvents, opts.AllowedRefs)
		if err != nil {
			return err
		}
		provider = "github"
		issuer = gitHubActionsIssuer
		claimConditions = conditions
	} else {
		// Generic OIDC provider
		if opts.AllowedEvents != "" {
			claimConditions = append(claimConditions, newClaimCondition("event_name", opts.AllowedEvents))
		}
		if opts.AllowedRefs != "" {
			claimConditions = append(claimConditions, newClaimCondition("ref", opts.AllowedRefs))
		}
	}

	client, err := ctx.RPCClient("dev.miren.runtime/oidc-bindings")
	if err != nil {
		return err
	}
	defer client.Close()

	oc := oidcbinding_v1alpha.NewOidcBindingsClient(client)

	resp, err := oc.Add(ctx, opts.App, provider, issuer, subject, claimConditions, opts.Description)
	if err != nil {
		return err
	}

	if resp.HasError() && resp.Error() != "" {
		return fmt.Errorf("%s", resp.Error())
	}

	b := resp.Binding()

	items := []ui.NamedValue{
		ui.NewNamedValue("ID", b.Id()),
		ui.NewNamedValue("App", b.App()),
		ui.NewNamedValue("Provider", b.Provider()),
		ui.NewNamedValue("Issuer", b.Issuer()),
	}
	if b.SubjectPattern() != "" {
		items = append(items, ui.NewNamedValue("Subject", b.SubjectPattern()))
	}
	if b.Description() != "" {
		items = append(items, ui.NewNamedValue("Description", b.Description()))
	}

	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())

	if b.HasClaimConditions() && len(b.ClaimConditions()) > 0 {
		var rows []ui.Row
		for _, cc := range b.ClaimConditions() {
			rows = append(rows, ui.Row{cc.Key(), cc.Pattern()})
		}
		headers := []string{"CLAIM", "PATTERN"}
		columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0).NoTruncate(1))
		table := ui.NewTable(
			ui.WithTableTitle("Claim Conditions"),
			ui.WithColumns(columns),
			ui.WithRows(rows),
		)
		ctx.Printf("\n%s\n", table.Render())
	}

	return nil
}

func AuthCIList(ctx *Context, opts struct {
	AppCentric
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/oidc-bindings")
	if err != nil {
		return err
	}
	defer client.Close()

	oc := oidcbinding_v1alpha.NewOidcBindingsClient(client)

	resp, err := oc.List(ctx, opts.App)
	if err != nil {
		return err
	}

	bindings := resp.Bindings()
	if len(bindings) == 0 {
		ctx.Printf("No CI authentication bindings found for app %s\n", opts.App)
		return nil
	}

	var rows []ui.Row
	headers := []string{"ID", "PROVIDER", "ISSUER", "SUBJECT", "CONDITIONS"}

	for _, b := range bindings {
		var conditions []string
		if b.HasClaimConditions() {
			for _, cc := range b.ClaimConditions() {
				conditions = append(conditions, cc.Key()+"="+cc.Pattern())
			}
		}
		rows = append(rows, ui.Row{
			b.Id(),
			b.Provider(),
			b.Issuer(),
			b.SubjectPattern(),
			strings.Join(conditions, "; "),
		})
	}

	columns := ui.AutoSizeColumns(headers, rows, ui.Columns().NoTruncate(0))
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}

func AuthCIRemove(ctx *Context, opts struct {
	ID string `position:"0" usage:"ID of the CI authentication binding to remove"`
	ConfigCentric
}) error {
	if opts.ID == "" {
		return fmt.Errorf("binding ID is required")
	}

	client, err := ctx.RPCClient("dev.miren.runtime/oidc-bindings")
	if err != nil {
		return err
	}
	defer client.Close()

	oc := oidcbinding_v1alpha.NewOidcBindingsClient(client)

	resp, err := oc.Remove(ctx, opts.ID)
	if err != nil {
		return err
	}

	if resp.HasError() && resp.Error() != "" {
		return fmt.Errorf("%s", resp.Error())
	}

	ctx.Printf("Removed CI authentication binding %s\n", opts.ID)
	return nil
}

// gitHubClaimConditions builds the claim conditions for a GitHub Actions
// binding from the owner/repo shorthand.
//
// These deliberately carry the repo's identity instead of a subject pattern. As
// of GitHub's immutable subject claims change (July 15, 2026), the sub claim
// embeds numeric IDs (repo:OWNER@1234/REPO@5678:ref:refs/heads/main) for any
// repo created, renamed, or transferred after the cutover, so a pattern built by
// concatenating owner/repo names silently never matches. The repository and
// repository_owner claims were left alone by that change and identify the caller
// under either subject format.
func gitHubClaimConditions(githubRepo, allowedEvents, allowedRefs string) ([]*oidcbinding_v1alpha.ClaimCondition, error) {
	owner, repo, found := strings.Cut(githubRepo, "/")
	if !found || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return nil, fmt.Errorf("--github must be in owner/repo format (e.g. acme/web-app)")
	}

	events := "push,workflow_dispatch"
	if allowedEvents != "" {
		events = allowedEvents
	}

	conditions := []*oidcbinding_v1alpha.ClaimCondition{
		newClaimCondition("repository", githubRepo),
		newClaimCondition("repository_owner", owner),
		newClaimCondition("event_name", events),
	}
	if allowedRefs != "" {
		conditions = append(conditions, newClaimCondition("ref", allowedRefs))
	}

	return conditions, nil
}

func newClaimCondition(key, pattern string) *oidcbinding_v1alpha.ClaimCondition {
	cc := &oidcbinding_v1alpha.ClaimCondition{}
	cc.SetKey(key)
	cc.SetPattern(pattern)
	return cc
}
