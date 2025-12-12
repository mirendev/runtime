package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/cloudauth"
)

type cloudUserInfo struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
}

func fetchCloudUserDoctor(ctx context.Context, cloudURL, token string) (*cloudUserInfo, error) {
	meURL, err := url.JoinPath(cloudURL, "/api/v1/me")
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", meURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var info cloudUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// DoctorAuth shows authentication and user information
func DoctorAuth(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cfg, err := opts.LoadConfig()
	if err != nil {
		if errors.Is(err, clientconfig.ErrNoConfig) {
			ctx.Printf("No cluster configured\n")
			ctx.Printf("\nUse 'miren cluster add' to add a cluster\n")
			return nil
		}
		return err
	}

	clusterName := cfg.ActiveCluster()
	if opts.Cluster != "" {
		clusterName = opts.Cluster
	}

	cluster, err := cfg.GetCluster(clusterName)
	if err != nil {
		return err
	}

	hostname := cluster.Hostname
	if hostname == "" {
		return fmt.Errorf("no hostname configured for cluster %s", clusterName)
	}

	authMethod := "none"
	var claims *auth.ExtendedClaims
	var identityName string
	var userInfo *cloudUserInfo

	if cluster.Identity != "" {
		identity, err := cfg.GetIdentity(cluster.Identity)
		if err == nil && identity != nil {
			identityName = cluster.Identity

			switch identity.Type {
			case "keypair":
				privateKeyPEM, err := cfg.GetPrivateKeyPEM(identity)
				if err == nil {
					keyPair, err := cloudauth.LoadKeyPairFromPEM(privateKeyPEM)
					if err == nil {
						authServer := identity.Issuer
						if authServer == "" {
							authServer = hostname
						}
						authServer = normalizeAuthServerURL(authServer)

						token, err := clientconfig.AuthenticateWithKey(ctx, authServer, keyPair)
						if err == nil {
							claims, _ = auth.ParseUnverifiedClaims(token)
							authMethod = "keypair"

							// Fetch user info from cloud
							userInfo, _ = fetchCloudUserDoctor(ctx, authServer, token)
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
	ctx.Printf("%s     %s\n", infoLabel.Render("Cluster:"), clusterName)
	ctx.Printf("%s      %s\n", infoLabel.Render("Server:"), hostname)
	ctx.Printf("%s %s\n", infoLabel.Render("Auth Method:"), authMethod)

	if identityName != "" {
		ctx.Printf("%s    %s\n", infoLabel.Render("Identity:"), identityName)
	}

	if claims != nil || userInfo != nil {
		ctx.Printf("\n")
		// Show email from cloud user info if available
		if userInfo != nil && userInfo.User.Email != "" {
			ctx.Printf("%s       %s\n", infoLabel.Render("Email:"), userInfo.User.Email)
		}
		// Show name from cloud user info if available
		if userInfo != nil && userInfo.User.Name != "" {
			ctx.Printf("%s        %s\n", infoLabel.Render("Name:"), userInfo.User.Name)
		}
		if claims != nil {
			if claims.UserID != "" {
				ctx.Printf("%s     %s\n", infoLabel.Render("User ID:"), claims.UserID)
			}
			if claims.OrganizationID != "" {
				ctx.Printf("%s %s\n", infoLabel.Render("Organization:"), claims.OrganizationID)
			}
			if len(claims.Groups) > 0 {
				ctx.Printf("%s      %s\n", infoLabel.Render("Groups:"), strings.Join(claims.Groups, ", "))
			}
		}
	} else if authMethod == "none" {
		ctx.Printf("\n%s\n", infoGray.Render("No authentication configured for this cluster"))
	}

	return nil
}
