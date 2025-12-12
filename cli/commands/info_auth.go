package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/cloudauth"
)

// InfoAuth shows authentication and user information
func InfoAuth(ctx *Context, opts struct {
	ConfigCentric
}) error {
	if ctx.ClusterConfig == nil {
		ctx.Printf("No cluster configured\n")
		ctx.Printf("\nUse 'miren cluster add' to add a cluster\n")
		return nil
	}

	hostname := ctx.ClusterConfig.Hostname
	if hostname == "" {
		return fmt.Errorf("no hostname configured for cluster %s", ctx.ClusterName)
	}

	authMethod := "none"
	var claims *auth.ExtendedClaims
	var identityName string

	if ctx.ClusterConfig.Identity != "" && ctx.ClientConfig != nil {
		identity, err := ctx.ClientConfig.GetIdentity(ctx.ClusterConfig.Identity)
		if err == nil && identity != nil {
			identityName = ctx.ClusterConfig.Identity

			switch identity.Type {
			case "keypair":
				privateKeyPEM, err := ctx.ClientConfig.GetPrivateKeyPEM(identity)
				if err == nil {
					keyPair, err := cloudauth.LoadKeyPairFromPEM(privateKeyPEM)
					if err == nil {
						authServer := identity.Issuer
						if authServer == "" {
							authServer = hostname
						}

						if !strings.HasPrefix(authServer, "http://") && !strings.HasPrefix(authServer, "https://") {
							if strings.Contains(authServer, "localhost") || strings.Contains(authServer, "127.0.0.1") {
								authServer = "http://" + authServer
							} else {
								authServer = "https://" + authServer
							}
						}

						token, err := clientconfig.AuthenticateWithKey(ctx, authServer, keyPair)
						if err == nil {
							claims, _ = auth.ParseUnverifiedClaims(token)
							authMethod = "keypair"
						}
					}
				}
			case "certificate":
				authMethod = "certificate"
			}
		}
	}

	ctx.Printf("%s\n", infoBold.Render("Authentication"))
	ctx.Printf("%s\n", infoGray.Render("=============="))
	ctx.Printf("%s     %s\n", infoLabel.Render("Cluster:"), ctx.ClusterName)
	ctx.Printf("%s      %s\n", infoLabel.Render("Server:"), hostname)
	ctx.Printf("%s %s\n", infoLabel.Render("Auth Method:"), authMethod)

	if identityName != "" {
		ctx.Printf("%s    %s\n", infoLabel.Render("Identity:"), identityName)
	}

	if claims != nil {
		ctx.Printf("\n")
		ctx.Printf("%s       %s\n", infoLabel.Render("Email:"), claims.Subject)
		if claims.UserID != "" {
			ctx.Printf("%s     %s\n", infoLabel.Render("User ID:"), claims.UserID)
		}
		if claims.OrganizationID != "" {
			ctx.Printf("%s %s\n", infoLabel.Render("Organization:"), claims.OrganizationID)
		}
		if len(claims.Groups) > 0 {
			ctx.Printf("%s      %s\n", infoLabel.Render("Groups:"), strings.Join(claims.Groups, ", "))
		}
	} else if authMethod == "none" {
		ctx.Printf("\n%s\n", infoGray.Render("No authentication configured for this cluster"))
	}

	return nil
}
