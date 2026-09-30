package commands

import (
	"fmt"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
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
		Cluster        string   `json:"cluster"`
		ServerURL      string   `json:"server_url"`
		AuthMethod     string   `json:"auth_method"`
		Identity       string   `json:"identity,omitempty"`
		UserID         string   `json:"user_id,omitempty"`
		UserEmail      string   `json:"user_email,omitempty"`
		UserName       string   `json:"user_name,omitempty"`
		OrganizationID string   `json:"organization_id,omitempty"`
		GroupIDs       []string `json:"group_ids,omitempty"`
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
		output.OrganizationID = claims.OrganizationID
		output.GroupIDs = claims.GroupIDs
	}

	// Output results
	if opts.IsJSON() {
		return PrintJSON(output)
	}

	// Human-readable output
	ctx.Info("Cluster:       %s", ctx.ClusterName)
	ctx.Info("Server:        %s", hostname)
	ctx.Info("Auth Method:   %s", authMethod)

	if identity != nil {
		ctx.Info("Identity:      %s", ctx.ClusterConfig.Identity)
	}

	if claims != nil {
		ctx.Info("")
		if user := describeUser(claims.Name, claims.Email); user != "" {
			ctx.Info("User:          %s", user)
		}
		ctx.Info("User ID:       %s", claims.Subject)
		if claims.OrganizationID != "" {
			ctx.Info("Organization:  %s", claims.OrganizationID)
		}
		if len(claims.GroupIDs) > 0 {
			ctx.Info("Group IDs:     %v", claims.GroupIDs)
		}
	} else if authMethod == "none" {
		ctx.Info("")
		ctx.Info("No authentication configured for this cluster")
	}

	return nil
}

// describeUser renders a person as "Name <email>", or whichever half is known.
// Tokens minted before cloud added these claims carry neither.
func describeUser(name, email string) string {
	switch {
	case name != "" && email != "":
		return fmt.Sprintf("%s <%s>", name, email)
	case name != "":
		return name
	default:
		return email
	}
}
