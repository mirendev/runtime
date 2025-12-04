package commands

import (
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func AppList(ctx *Context, opts struct {
	FormatOptions
	ConfigCentric
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
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

	versionMap := make(map[string]*core_v1alpha.AppVersion)
	for _, e := range versionsRes.Values() {
		var version core_v1alpha.AppVersion
		version.Decode(e.Entity())
		versionMap[version.ID.String()] = &version
	}

	deploymentMap := make(map[string]*core_v1alpha.Deployment)
	for _, e := range deploymentsRes.Values() {
		var deployment core_v1alpha.Deployment
		deployment.Decode(e.Entity())

		if deployment.Status == "active" {
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
	}

	if opts.IsJSON() {
		var apps []struct {
			Name       string `json:"name"`
			Version    string `json:"version,omitempty"`
			DeployedBy string `json:"deployed_by,omitempty"`
			DeployedAt string `json:"deployed_at,omitempty"`
			GitCommit  string `json:"git_commit,omitempty"`
		}

		for _, e := range res.Values() {
			var app core_v1alpha.App
			app.Decode(e.Entity())

			var md core_v1alpha.Metadata
			md.Decode(e.Entity())

			appData := struct {
				Name       string `json:"name"`
				Version    string `json:"version,omitempty"`
				DeployedBy string `json:"deployed_by,omitempty"`
				DeployedAt string `json:"deployed_at,omitempty"`
				GitCommit  string `json:"git_commit,omitempty"`
			}{
				Name: md.Name,
			}

			if app.ActiveVersion.String() != "" {
				if version, ok := versionMap[app.ActiveVersion.String()]; ok {
					appData.Version = version.Version
				}
			}

			if deployment, ok := deploymentMap[md.Name]; ok {
				appData.DeployedBy = deployment.DeployedBy.UserEmail
				appData.DeployedAt = deployment.CompletedAt
				if deployment.GitInfo.Sha != "" {
					appData.GitCommit = deployment.GitInfo.Sha
					if len(appData.GitCommit) > 7 {
						appData.GitCommit = appData.GitCommit[:7]
					}
				}
			}

			apps = append(apps, appData)
		}

		sort.Slice(apps, func(i, j int) bool {
			return apps[i].Name < apps[j].Name
		})

		return PrintJSON(apps)
	}

	var rows []ui.Row
	headers := []string{"NAME", "VERSION", "DEPLOYED", "COMMIT"}

	for _, e := range res.Values() {
		var app core_v1alpha.App
		app.Decode(e.Entity())

		var md core_v1alpha.Metadata
		md.Decode(e.Entity())

		name := md.Name
		version := "-"
		deployed := "-"
		commit := "-"

		if app.ActiveVersion.String() != "" {
			if appVersion, ok := versionMap[app.ActiveVersion.String()]; ok {
				version = ui.DisplayAppVersion(appVersion.Version)
			}
		}

		if deployment, ok := deploymentMap[md.Name]; ok {
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

			if deployment.GitInfo.Sha != "" {
				commit = deployment.GitInfo.Sha
				if len(commit) > 7 {
					commit = commit[:7]
				}
			}
		}

		rows = append(rows, ui.Row{
			name,
			version,
			deployed,
			commit,
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
