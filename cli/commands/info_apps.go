package commands

import (
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/ui"
)

// InfoApps shows apps and their routes
func InfoApps(ctx *Context, opts struct {
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}

	// Get default hostname for display
	defaultHost := ""
	if ctx.ClusterConfig != nil && ctx.ClusterConfig.Hostname != "" {
		defaultHost = ctx.ClusterConfig.Hostname
		// Strip port if present
		if idx := strings.Index(defaultHost, ":"); idx > 0 {
			defaultHost = defaultHost[:idx]
		}
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	// Get apps
	kindRes, err := eac.LookupKind(ctx, "app")
	if err != nil {
		return err
	}

	appsRes, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return err
	}

	// Get versions
	versionKindRes, err := eac.LookupKind(ctx, "app_version")
	if err != nil {
		return err
	}

	versionsRes, err := eac.List(ctx, versionKindRes.Attr())
	if err != nil {
		return err
	}

	// Get deployments
	deploymentKindRes, err := eac.LookupKind(ctx, "deployment")
	if err != nil {
		return err
	}

	deploymentsRes, err := eac.List(ctx, deploymentKindRes.Attr())
	if err != nil {
		return err
	}

	// Get routes
	ic := ingress.NewClient(ctx.Log, client)
	routes, err := ic.List(ctx)
	if err != nil {
		return err
	}

	// Build version map
	versionMap := make(map[string]*core_v1alpha.AppVersion)
	for _, e := range versionsRes.Values() {
		var version core_v1alpha.AppVersion
		version.Decode(e.Entity())
		versionMap[version.ID.String()] = &version
	}

	// Build deployment map (most recent deployment per app)
	deploymentMap := make(map[string]*core_v1alpha.Deployment)
	for _, e := range deploymentsRes.Values() {
		var deployment core_v1alpha.Deployment
		deployment.Decode(e.Entity())

		if existing, ok := deploymentMap[deployment.AppName]; ok {
			existingTime, _ := time.Parse(time.RFC3339, existing.CompletedAt)
			newTime, _ := time.Parse(time.RFC3339, deployment.CompletedAt)
			if newTime.After(existingTime) {
				deploymentMap[deployment.AppName] = &deployment
			}
		} else {
			deploymentMap[deployment.AppName] = &deployment
		}
	}

	// Build routes map (app name -> routes)
	routeMap := make(map[string][]string)
	for _, r := range routes {
		appName := ui.CleanEntityID(string(r.Route.App))
		host := r.Route.Host
		if host == "" && r.Route.Default {
			if defaultHost != "" {
				host = defaultHost + " (default)"
			} else {
				host = "(default)"
			}
		}
		if host != "" {
			routeMap[appName] = append(routeMap[appName], host)
		}
	}

	// Build table
	var rows []ui.Row
	headers := []string{"NAME", "VERSION", "COMMIT", "STATUS", "DEPLOYED", "ROUTES"}

	for _, e := range appsRes.Values() {
		var app core_v1alpha.App
		app.Decode(e.Entity())

		var md core_v1alpha.Metadata
		md.Decode(e.Entity())

		name := md.Name
		version := "-"
		commit := "-"
		status := "-"
		deployed := "-"
		routesDisplay := "-"

		if app.ActiveVersion.String() != "" {
			if appVersion, ok := versionMap[app.ActiveVersion.String()]; ok {
				version = ui.DisplayAppVersion(appVersion.Version)
			}
		}

		if deployment, ok := deploymentMap[md.Name]; ok {
			// Get commit SHA (short form)
			if deployment.GitInfo.Sha != "" {
				sha := deployment.GitInfo.Sha
				if len(sha) > 7 {
					sha = sha[:7]
				}
				commit = sha
			}

			// Get deployment status with color
			if deployment.Status != "" {
				switch deployment.Status {
				case "active":
					status = infoGreen.Render(deployment.Status)
				case "failed":
					status = infoRed.Render(deployment.Status)
				case "in_progress":
					status = infoLabel.Render(deployment.Status)
				default:
					status = deployment.Status
				}
			}

			var deployedParts []string

			if deployment.CompletedAt != "" {
				if t, err := time.Parse(time.RFC3339, deployment.CompletedAt); err == nil {
					deployedParts = append(deployedParts, humanFriendlyTimestamp(t))
				}
			}

			if deployment.DeployedBy.UserEmail != "" && deployment.DeployedBy.UserEmail != "user@example.com" {
				email := deployment.DeployedBy.UserEmail
				if atIdx := strings.Index(email, "@"); atIdx > 0 {
					email = email[:atIdx]
				}
				deployedParts = append(deployedParts, "by "+email)
			}

			if len(deployedParts) > 0 {
				deployed = strings.Join(deployedParts, " ")
			}
		}

		if appRoutes, ok := routeMap[md.Name]; ok && len(appRoutes) > 0 {
			routesDisplay = strings.Join(appRoutes, ", ")
		}

		rows = append(rows, ui.Row{name, version, commit, status, deployed, routesDisplay})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	ctx.Printf("%s\n", infoBold.Render("Apps"))
	ctx.Printf("%s\n", infoGray.Render("===="))

	if len(rows) == 0 {
		ctx.Printf("%s\n", infoGray.Render("No apps found"))
		return nil
	}

	ctx.Printf("%s %d total\n\n", infoLabel.Render("Count:"), len(rows))

	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}
