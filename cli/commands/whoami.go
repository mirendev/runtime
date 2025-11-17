package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/cloudauth"
)

// Whoami displays information about the current authenticated user
func Whoami(ctx *Context, opts struct {
	ConfigCentric
	JSON bool `long:"json" description:"Output as JSON"`
}) error {
	// Check if we have a configured cluster
	if ctx.ClusterConfig == nil {
		return fmt.Errorf("no cluster configured - use 'miren cluster add' to add a cluster")
	}

	// Get server hostname
	hostname := ctx.ClusterConfig.Hostname
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
		if err == nil && identity != nil && identity.Type == "keypair" && identity.PrivateKey != "" {
			keyPair, err := cloudauth.LoadKeyPairFromPEM(identity.PrivateKey)
			if err != nil {
				ctx.Warn("Failed to load keypair: %v", err)
			} else {
				// Get the auth server URL
				authServer := identity.Issuer
				if authServer == "" {
					authServer = hostname
				}

				// For local development, use HTTP
				if !strings.HasPrefix(authServer, "http://") && !strings.HasPrefix(authServer, "https://") {
					if strings.Contains(authServer, "localhost") || strings.Contains(authServer, "127.0.0.1") {
						authServer = "http://" + authServer
					} else {
						authServer = "https://" + authServer
					}
				}

				// Get JWT token
				token, err = clientconfig.AuthenticateWithKey(ctx, authServer, keyPair)
				if err != nil {
					return fmt.Errorf("failed to authenticate with keypair: %w", err)
				}
				authMethod = "keypair"
			}
		} else if identity != nil && identity.Type == "certificate" {
			authMethod = "certificate"
		}
	}

	// Try to parse JWT claims if we have a token
	var claims *auth.ExtendedClaims
	if token != "" {
		claims, _ = auth.ParseUnverifiedClaims(token)
	}

	// Prepare output
	type WhoamiOutput struct {
		Cluster    string   `json:"cluster"`
		ServerURL  string   `json:"server_url"`
		AuthMethod string   `json:"auth_method"`
		Identity   string   `json:"identity,omitempty"`
		UserID     string   `json:"user_id,omitempty"`
		UserEmail  string   `json:"user_email,omitempty"`
		Groups     []string `json:"groups,omitempty"`
		GroupIDs   []string `json:"group_ids,omitempty"`
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
		output.UserEmail = claims.Subject
		output.UserID = claims.UserID
		output.Groups = claims.Groups
		output.GroupIDs = claims.GroupIDs
	}

	// Output results
	if opts.JSON {
		encoder := json.NewEncoder(ctx.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
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
		ctx.Info("User:          %s", claims.Subject)
		ctx.Info("User ID:       %s", claims.UserID)
		if len(claims.Groups) > 0 {
			ctx.Info("Groups:        %v", claims.Groups)
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
