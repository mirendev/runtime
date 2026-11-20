package commands

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/ui"
	"miren.dev/runtime/pkg/userlabel"
)

// exitWhoamiUnchecked is whoami, with no cluster configured, finding no
// signed-in identity because it couldn't check them, rather than because there
// are none.
const exitWhoamiUnchecked = 2

type whoamiIdentityJSON struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Issuer string `json:"issuer,omitempty"`
	// Status is logged_in, expired, error, or certificate.
	Status string `json:"status"`
	User   string `json:"user,omitempty"`
	Error  string `json:"error,omitempty"`
}

type whoamiPendingJSON struct {
	Identity  string    `json:"identity"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Whoami displays information about the current authenticated user: the
// configured cluster's identity when there is a cluster, and always every
// cloud login, checked with the cloud, plus any login awaiting approval.
func Whoami(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	type WhoamiOutput struct {
		Cluster    string               `json:"cluster,omitempty"`
		ServerURL  string               `json:"server_url,omitempty"`
		AuthMethod string               `json:"auth_method,omitempty"`
		Identity   string               `json:"identity,omitempty"`
		UserID     string               `json:"user_id,omitempty"`
		UserEmail  string               `json:"user_email,omitempty"`
		UserName   string               `json:"user_name,omitempty"`
		Identities []whoamiIdentityJSON `json:"identities"`
		Pending    []whoamiPendingJSON  `json:"pending"`
	}
	output := WhoamiOutput{
		Identities: cloudIdentityStatuses(ctx, ctx.ClientConfig),
		Pending:    []whoamiPendingJSON{},
	}
	for _, p := range listPendingLogins() {
		output.Pending = append(output.Pending, whoamiPendingJSON{Identity: p.Identity, URL: p.loginURL(), ExpiresAt: p.ExpiresAt})
	}

	var items []ui.NamedValue
	var clusterErr error
	if ctx.ClusterConfig != nil {
		var claims *auth.Claims
		output.Cluster = ctx.ClusterName
		output.ServerURL, output.AuthMethod, output.Identity, claims, clusterErr = clusterIdentity(ctx)
		if claims != nil {
			output.UserID = claims.Subject
			output.UserEmail = claims.Email
			output.UserName = claims.Name
		}
		if clusterErr == nil {
			items = []ui.NamedValue{
				ui.NewNamedValue("Cluster", ctx.ClusterName),
				ui.NewNamedValue("Server", output.ServerURL),
				ui.NewNamedValue("Authentication", output.AuthMethod),
			}
			if output.Identity != "" {
				items = append(items, ui.NewNamedValue("Identity", output.Identity))
			}
			if claims != nil {
				if user := userlabel.Label(claims.Name, claims.Email, ""); user != "" {
					items = append(items, ui.NewNamedValue("User", user))
				}
				items = append(items, ui.NewNamedValue("User ID", claims.Subject))
			}
		}
	}

	if opts.IsJSON() {
		if err := PrintJSON(output); err != nil {
			return err
		}
	} else {
		if len(items) > 0 {
			ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())
			if output.AuthMethod == "none" {
				ctx.Printf("%s\n", ui.NewHint("No authentication configured for this cluster").Render())
			}
			ctx.Printf("\n")
		}
		printCloudIdentities(ctx, output.Identities, output.Pending, ctx.ClusterConfig == nil)
	}

	if ctx.ClusterConfig != nil {
		return clusterErr
	}
	loggedIn := slices.ContainsFunc(output.Identities, func(id whoamiIdentityJSON) bool { return id.Status == "logged_in" })
	switch {
	case loggedIn:
		return nil
	case slices.ContainsFunc(output.Identities, func(id whoamiIdentityJSON) bool { return id.Status == "error" }):
		return ErrExitCode(exitWhoamiUnchecked)
	default:
		return ErrExitCode(1)
	}
}

// clusterIdentity reports how the configured cluster authenticates.
func clusterIdentity(ctx *Context) (hostname, authMethod, identityName string, claims *auth.Claims, err error) {
	// Where this cluster is reached. A cloud-routed one has no address of its
	// own — that is the point of it — so the cloud it is reached through stands
	// in, which is also the server the identity below authenticates against.
	hostname = ctx.ClusterConfig.Hostname
	if ctx.ClusterConfig.ViaCloud {
		// Same precedence the connection itself uses, from the same place, so
		// what this reports cannot drift from where the call actually went.
		if cloud, err := ctx.ClusterConfig.CloudEndpoint(ctx.ClientConfig); err == nil {
			hostname = cloud
		}
	}
	if hostname == "" {
		return "", "", "", nil, fmt.Errorf("no hostname configured for cluster %s", ctx.ClusterName)
	}

	authMethod = "none"
	if ctx.ClusterConfig.Identity == "" || ctx.ClientConfig == nil {
		return hostname, authMethod, "", nil, nil
	}
	identity, err := ctx.ClientConfig.GetIdentity(ctx.ClusterConfig.Identity)
	if err != nil || identity == nil {
		return hostname, authMethod, "", nil, nil
	}
	identityName = ctx.ClusterConfig.Identity
	switch identity.Type {
	case clientconfig.IdentityKeypair, clientconfig.IdentityToken:
		token, err := ctx.ClientConfig.TokenForIdentity(ctx, identityName, identity, hostname)
		if err != nil {
			return hostname, string(identity.Type), identityName, nil, fmt.Errorf("failed to authenticate: %w", err)
		}
		claims, _ = auth.ParseUnverifiedClaims(token)
		authMethod = string(identity.Type)
	case clientconfig.IdentityCertificate:
		authMethod = "certificate"
	}
	return hostname, authMethod, identityName, claims, nil
}

// cloudIdentityStatuses checks each configured identity with its cloud. The
// checks run in parallel so an unreachable cloud costs one timeout rather than
// one per identity. They aren't cancelled early: a token refresh cut off after
// the cloud rotated the token, but before it was saved, would sign the user out.
func cloudIdentityStatuses(ctx *Context, config *clientconfig.Config) []whoamiIdentityJSON {
	out := []whoamiIdentityJSON{}
	if config == nil {
		return out
	}
	for _, name := range config.GetIdentityNames() {
		identity, err := config.GetIdentity(name)
		if err != nil || identity == nil {
			continue
		}
		entry := whoamiIdentityJSON{Name: name, Type: string(identity.Type), Issuer: identity.Issuer}
		if identity.Type == clientconfig.IdentityCertificate {
			// Certificates authenticate to one cluster directly; there is no
			// cloud session to check.
			entry.Status = "certificate"
		}
		out = append(out, entry)
	}

	var wg sync.WaitGroup
	for i := range out {
		if out[i].Status != "" {
			continue
		}
		entry := &out[i]
		wg.Go(func() {
			who, err := identityUser(ctx, config, entry.Name, true)
			switch {
			case err == nil:
				entry.Status = "logged_in"
				entry.User = who
			case errors.Is(err, clientconfig.ErrLoginRequired):
				entry.Status = "expired"
			default:
				entry.Status = "error"
				entry.Error = err.Error()
			}
		})
	}
	wg.Wait()
	return out
}

func printCloudIdentities(ctx *Context, identities []whoamiIdentityJSON, pending []whoamiPendingJSON, noCluster bool) {
	switch {
	case len(identities) == 0 && noCluster:
		ctx.Info("Not logged in. Run 'miren login' to sign in.")
	case len(identities) > 0:
		headers := []string{"IDENTITY", "STATUS", "USER", "ISSUER"}
		showErrors := slices.ContainsFunc(identities, func(id whoamiIdentityJSON) bool { return id.Error != "" })
		if showErrors {
			headers = append(headers, "ERROR")
		}
		var rows []ui.Row
		for _, id := range identities {
			row := ui.Row{id.Name, id.Status, id.User, id.Issuer}
			if showErrors {
				row = append(row, id.Error)
			}
			rows = append(rows, row)
		}
		ctx.Printf("%s\n", ui.NewTable(
			ui.WithColumns(ui.AutoSizeColumns(headers, rows, ui.Columns())),
			ui.WithRows(rows),
		).Render())
		if noCluster {
			ctx.Info("")
			ctx.Info("No cluster configured; add one with 'miren cluster add'.")
		}
	}
	for _, p := range pending {
		ctx.Info("")
		ctx.Info("Login pending for '%s' until %s:", p.Identity, p.ExpiresAt.Local().Format(time.Kitchen))
		ctx.Info("  %s", p.URL)
	}
}
