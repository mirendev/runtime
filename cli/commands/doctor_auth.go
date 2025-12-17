package commands

import (
	"errors"
	"fmt"
	"strings"

	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

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

	authRes := tryAuthenticate(ctx, cfg, cluster)

	ctx.Printf("%s\n", infoBold.Render("Authentication"))
	ctx.Printf("%s\n", infoGray.Render("=============="))
	ctx.Printf("%s     %s\n", infoLabel.Render("Cluster:"), clusterName)
	ctx.Printf("%s      %s\n", infoLabel.Render("Server:"), hostname)
	ctx.Printf("%s %s\n", infoLabel.Render("Auth Method:"), authRes.Method)

	if authRes.IdentityName != "" {
		ctx.Printf("%s    %s\n", infoLabel.Render("Identity:"), authRes.IdentityName)
	}

	if authRes.Claims != nil || authRes.UserInfo != nil {
		ctx.Printf("\n")
		// Show email from cloud user info if available
		if authRes.UserInfo != nil && authRes.UserInfo.User.Email != "" {
			ctx.Printf("%s       %s\n", infoLabel.Render("Email:"), authRes.UserInfo.User.Email)
		}
		// Show name from cloud user info if available
		if authRes.UserInfo != nil && authRes.UserInfo.User.Name != "" {
			ctx.Printf("%s        %s\n", infoLabel.Render("Name:"), authRes.UserInfo.User.Name)
		}
		if authRes.Claims != nil {
			if authRes.Claims.UserID != "" {
				ctx.Printf("%s     %s\n", infoLabel.Render("User ID:"), authRes.Claims.UserID)
			}
			if authRes.Claims.OrganizationID != "" {
				ctx.Printf("%s %s\n", infoLabel.Render("Organization:"), authRes.Claims.OrganizationID)
			}
			if len(authRes.Claims.Groups) > 0 {
				ctx.Printf("%s      %s\n", infoLabel.Render("Groups:"), strings.Join(authRes.Claims.Groups, ", "))
			}
		}
	} else if authRes.Method == "none" {
		ctx.Printf("\n%s\n", infoGray.Render("No authentication configured for this cluster"))
	}

	// Interactive prompts
	if !ui.IsInteractive() {
		return nil
	}

	if cluster.Identity == "" {
		// No identity configured - offer to set up miren.cloud auth
		ctx.Printf("\n")
		ctx.Printf("Set up authentication with miren.cloud? [Y/n] ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response == "" || response == "y" || response == "yes" {
			ctx.Printf("\n")
			if err := LoginWithDefaults(ctx); err != nil {
				return err
			}
			ctx.Printf("\n%s\n", infoGreen.Render("✓ Login successful"))
		}
	} else if authRes.Method == "none" && cluster.Identity != "" {
		// Identity is configured but auth failed - offer to refresh
		ctx.Printf("\n")
		ctx.Printf("Identity not working. Refresh your login? [Y/n] ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response == "" || response == "y" || response == "yes" {
			ctx.Printf("\n%s\n", infoGray.Render("Refreshing login..."))
			ctx.Printf("\n")
			if err := LoginWithDefaults(ctx); err != nil {
				return err
			}
			ctx.Printf("\n%s\n", infoGreen.Render("✓ Login refreshed"))
		}
	}

	return nil
}
