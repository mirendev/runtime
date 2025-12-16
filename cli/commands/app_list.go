package commands

import (
	"errors"
	"net"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/ui"
)

func AppList(ctx *Context, opts struct {
	FormatOptions
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

	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	defer client.Close()

	// Get default hostname for routes display
	defaultHost := ""
	if cluster.Hostname != "" {
		defaultHost = cluster.Hostname
		// Strip port if present (handles IPv6)
		if h, _, err := net.SplitHostPort(defaultHost); err == nil {
			defaultHost = h
		}
	}

	eac := entityserver_v1alpha.NewEntityAccessClient(client)

	kindRes, err := eac.LookupKind(ctx, "app")
	if err != nil {
		return err
	}

	res, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return err
	}

	versionKindRes, err := eac.LookupKind(ctx, "app_version")
	if err != nil {
		return err
	}

	versionsRes, err := eac.List(ctx, versionKindRes.Attr())
	if err != nil {
		return err
	}

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
		v := new(core_v1alpha.AppVersion)
		v.Decode(e.Entity())
		versionMap[v.ID.String()] = v
	}

	// Build deployment map (most recent deployment per app)
	deploymentMap := make(map[string]*core_v1alpha.Deployment)
	for _, e := range deploymentsRes.Values() {
		d := new(core_v1alpha.Deployment)
		d.Decode(e.Entity())

		if existing, ok := deploymentMap[d.AppName]; ok {
			existingTime, existingErr := time.Parse(time.RFC3339, existing.CompletedAt)
			newTime, newErr := time.Parse(time.RFC3339, d.CompletedAt)

			// Replace if: new has valid time and (existing invalid OR new is later)
			if newErr == nil && (existingErr != nil || newTime.After(existingTime)) {
				deploymentMap[d.AppName] = d
			}
		} else {
			deploymentMap[d.AppName] = d
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

	if opts.IsJSON() {
		var apps []struct {
			Name       string   `json:"name"`
			Version    string   `json:"version,omitempty"`
			Status     string   `json:"status,omitempty"`
			DeployedBy string   `json:"deployed_by,omitempty"`
			DeployedAt string   `json:"deployed_at,omitempty"`
			GitCommit  string   `json:"git_commit,omitempty"`
			Routes     []string `json:"routes,omitempty"`
		}

		for _, e := range res.Values() {
			var app core_v1alpha.App
			app.Decode(e.Entity())

			var md core_v1alpha.Metadata
			md.Decode(e.Entity())

			appData := struct {
				Name       string   `json:"name"`
				Version    string   `json:"version,omitempty"`
				Status     string   `json:"status,omitempty"`
				DeployedBy string   `json:"deployed_by,omitempty"`
				DeployedAt string   `json:"deployed_at,omitempty"`
				GitCommit  string   `json:"git_commit,omitempty"`
				Routes     []string `json:"routes,omitempty"`
			}{
				Name: md.Name,
			}

			if app.ActiveVersion.String() != "" {
				if version, ok := versionMap[app.ActiveVersion.String()]; ok {
					appData.Version = version.Version
				}
			}

			if deployment, ok := deploymentMap[md.Name]; ok {
				appData.Status = deployment.Status
				appData.DeployedBy = deployment.DeployedBy.UserEmail
				appData.DeployedAt = deployment.CompletedAt
				if deployment.GitInfo.Sha != "" {
					appData.GitCommit = deployment.GitInfo.Sha
					if len(appData.GitCommit) > 7 {
						appData.GitCommit = appData.GitCommit[:7]
					}
				}
			}

			if appRoutes, ok := routeMap[md.Name]; ok {
				appData.Routes = appRoutes
			}

			apps = append(apps, appData)
		}

		sort.Slice(apps, func(i, j int) bool {
			return apps[i].Name < apps[j].Name
		})

		return PrintJSON(apps)
	}

	var rows []ui.Row
	headers := []string{"NAME", "VERSION", "COMMIT", "STATUS", "DEPLOYED", "ROUTES"}

	for _, e := range res.Values() {
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
				commit = deployment.GitInfo.Sha
				if len(commit) > 7 {
					commit = commit[:7]
				}
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

		rows = append(rows, ui.Row{
			name,
			version,
			commit,
			status,
			deployed,
			routesDisplay,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	if len(rows) == 0 {
		ctx.Printf("No apps found\n")
		return nil
	}

	columns := ui.AutoSizeColumns(headers, rows, nil)
	table := ui.NewTable(
		ui.WithColumns(columns),
		ui.WithRows(rows),
	)

	ctx.Printf("%s\n", table.Render())
	return nil
}
