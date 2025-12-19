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

	// Show help content in interactive mode
	if !ui.IsInteractive() {
		return nil
	}

	ctx.Printf("\n")
	showLoginDifferentAccountHelp(ctx)
	showAddAuthToServerHelp(ctx)

	return nil
}

func showLoginDifferentAccountHelp(ctx *Context) {
	ctx.Printf("%s\n", infoBold.Render("How do I login with a different account?"))
	ctx.Printf("  %s\n", infoGray.Render("miren logout"))
	ctx.Printf("  %s\n\n", infoGray.Render("miren login"))
}

func showAddAuthToServerHelp(ctx *Context) {
	ctx.Printf("%s\n", infoBold.Render("How do I add authentication to this server?"))
	ctx.Printf("  %s\n", infoGray.Render("miren login"))
	ctx.Printf("  %s\n", infoGray.Render("sudo miren server register -n <cluster-name>"))
	ctx.Printf("  %s\n", infoGray.Render("# Approve in browser when prompted"))
	ctx.Printf("  %s\n", infoGray.Render("sudo systemctl restart miren"))
}
