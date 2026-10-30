package commands

import (
	"fmt"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/ui"
	"miren.dev/runtime/pkg/userlabel"
)

// Whoami displays information about the current authenticated user
func Whoami(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	// Check if we have a configured cluster
	if ctx.ClusterConfig == nil {
		return fmt.Errorf("no cluster configured - use 'miren cluster add' to add a cluster")
	}

	// Where this cluster is reached. A cloud-routed one has no address of its
	// own — that is the point of it — so the cloud it is reached through stands
	// in, which is also the server the identity below authenticates against.
	hostname := ctx.ClusterConfig.Hostname
	if ctx.ClusterConfig.ViaCloud {
		// Same precedence the connection itself uses, from the same place, so
		// what this reports cannot drift from where the call actually went.
		if cloud, err := ctx.ClusterConfig.CloudEndpoint(ctx.ClientConfig); err == nil {
			hostname = cloud
		}
	}
	if hostname == "" {
		return fmt.Errorf("no hostname configured for cluster %s", ctx.ClusterName)
	}

	// Get JWT token if using keypair auth
	token := ""
	authMethod := "none"
	var identity *clientconfig.IdentityConfig

	if ctx.ClusterConfig.Identity != "" && ctx.ClientConfig != nil {
		var err error
		identity, err = ctx.ClientConfig.GetIdentity(ctx.ClusterConfig.Identity)
		if err == nil && identity != nil {
			switch identity.Type {
			case clientconfig.IdentityKeypair, clientconfig.IdentityToken:
				token, err = ctx.ClientConfig.TokenForIdentity(ctx, ctx.ClusterConfig.Identity, identity, hostname)
				if err != nil {
					return fmt.Errorf("failed to authenticate: %w", err)
				}
				authMethod = string(identity.Type)
			case clientconfig.IdentityCertificate:
				authMethod = "certificate"
			}
		}
	}

	// Try to parse JWT claims if we have a token
	var claims *auth.Claims
	if token != "" {
		claims, _ = auth.ParseUnverifiedClaims(token)
	}

	// Prepare output
	type WhoamiOutput struct {
		Cluster    string `json:"cluster"`
		ServerURL  string `json:"server_url"`
		AuthMethod string `json:"auth_method"`
		Identity   string `json:"identity,omitempty"`
		UserID     string `json:"user_id,omitempty"`
		UserEmail  string `json:"user_email,omitempty"`
		UserName   string `json:"user_name,omitempty"`
	}

	output := WhoamiOutput{
		Cluster:    ctx.ClusterName,
		ServerURL:  hostname,
		AuthMethod: authMethod,
	}

	if identity != nil {
		output.Identity = ctx.ClusterConfig.Identity
	}

	// Add claims data if available
	if claims != nil {
		output.UserID = claims.Subject
		output.UserEmail = claims.Email
		output.UserName = claims.Name
	}

	// Output results
	if opts.IsJSON() {
		return PrintJSON(output)
	}

	items := []ui.NamedValue{
		ui.NewNamedValue("Cluster", ctx.ClusterName),
		ui.NewNamedValue("Server", hostname),
		ui.NewNamedValue("Authentication", authMethod),
	}

	if identity != nil {
		items = append(items, ui.NewNamedValue("Identity", ctx.ClusterConfig.Identity))
	}

	if claims != nil {
		if user := userlabel.Label(claims.Name, claims.Email, ""); user != "" {
			items = append(items, ui.NewNamedValue("User", user))
		}
		items = append(items, ui.NewNamedValue("User ID", claims.Subject))
	}
	ctx.Printf("%s\n", ui.NewNamedValueList(items).Render())
	if authMethod == "none" {
		ctx.Printf("%s\n", ui.NewHint("No authentication configured for this cluster").Render())
	}

	return nil
}
