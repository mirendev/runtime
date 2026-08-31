package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

func AppStatus(ctx *Context, opts struct {
	AppCentric
	FormatOptions
}) error {
	// Connect to app service
	appCl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return fmt.Errorf("failed to connect to app service: %w", err)
	}
	appClient := app_v1alpha.NewCrudClient(appCl)

	// Get app configuration
	appResult, err := appClient.GetConfiguration(ctx, opts.App)
	if err != nil {
		return fmt.Errorf("failed to get app configuration: %w", err)
	}

	// Extract configuration if available
	var appConfig *app_v1alpha.Configuration
	if appResult.HasConfiguration() {
		appConfig = appResult.Configuration()
	}

	// Connect to deployment service
	depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return fmt.Errorf("failed to connect to deployment service: %w", err)
	}
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	// Determine which cluster to query
	// Use explicit cluster if specified with -C flag, otherwise use current context
	clusterId := ctx.ClusterName
	if opts.Cluster != "" {
		clusterId = opts.Cluster
	}

	// Get active deployment
	activeDeployment, err := depClient.GetActiveDeployment(ctx, opts.App, clusterId)
	if err != nil {
		// It's okay if there's no active deployment
		activeDeployment = nil
	}

	// Get last 5 deployments for recent activity
	recentResult, err := depClient.ListDeployments(ctx, opts.App, clusterId, "", 5)
	if err != nil {
		recentResult = nil
	}

	// JSON output
	if opts.IsJSON() {
		return printAppStatusJSON(appResult, appConfig, activeDeployment, recentResult, opts.App, clusterId)
	}

	// Define styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Info)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Muted)
	greenStyle := lipgloss.NewStyle().Foreground(theme.Success)
	blueStyle := lipgloss.NewStyle().Foreground(theme.Info)
	yellowStyle := lipgloss.NewStyle().Foreground(theme.Warning)
	redStyle := lipgloss.NewStyle().Foreground(theme.Error)

	// Print header
	ctx.Printf("%s\n\n", headerStyle.Render(fmt.Sprintf("Status for %s", opts.App)))

	// App info
	ctx.Printf("%s %s\n", labelStyle.Render("App:"), opts.App)
	ctx.Printf("%s %s\n", labelStyle.Render("Cluster:"), clusterId)

	// Version info
	if appResult.HasVersionId() && appResult.VersionId() != "" {
		versionDisplay := ui.DisplayShortID(appResult.VersionShortId(), appResult.VersionId())
		ctx.Printf("%s %s\n", labelStyle.Render("Current Version:"), versionDisplay)
	} else {
		ctx.Printf("%s %s\n", labelStyle.Render("Current Version:"), yellowStyle.Render("No version deployed"))
	}

	if role := appResult.WorkloadRole(); role != "" {
		ctx.Printf("%s %s\n", labelStyle.Render("Workload Role:"), role)
	}

	if down := appResult.MaintenanceRoutes(); len(down) > 0 {
		ctx.Printf("%s %s\n", labelStyle.Render("Maintenance:"),
			yellowStyle.Render(fmt.Sprintf("%s serving a holding page", maintenanceRouteList(down))))
	}

	// Configuration
	if appConfig != nil {
		ctx.Printf("\n%s\n", labelStyle.Render("Configuration:"))
		// Environment variables: show keys only. Values live behind the one
		// masked surface, 'miren env'. See MIR-1356.
		if appConfig.HasEnvVars() && len(appConfig.EnvVars()) > 0 {
			var keys []string
			for _, kv := range appConfig.EnvVars() {
				if kv.HasKey() {
					keys = append(keys, kv.Key())
				}
			}
			sort.Strings(keys)

			ctx.Printf("\n%s (%d)\n", labelStyle.Render("Environment Variables:"), len(keys))
			for _, key := range keys {
				ctx.Printf("  %s\n", key)
			}
			ctx.Printf("  %s\n", labelStyle.Render(fmt.Sprintf("Run 'miren env list -a %s' to view values", opts.App)))
		}
	}

	// Active deployment info
	if activeDeployment != nil && activeDeployment.HasDeployment() {
		deployment := activeDeployment.Deployment()
		ctx.Printf("\n%s\n", labelStyle.Render("Active Deployment:"))

		// Deployment ID
		ctx.Printf("  ID: %s\n", ui.DisplayShortID(deployment.ShortId(), deployment.Id()))

		// Status with color
		status := deployment.Status()
		var styledStatus string
		switch status {
		case "active":
			styledStatus = greenStyle.Render(status)
		case "succeeded":
			styledStatus = blueStyle.Render(status)
		case "failed":
			styledStatus = redStyle.Render(status)
		default:
			styledStatus = yellowStyle.Render(status)
		}
		ctx.Printf("  Status: %s\n", styledStatus)

		// Phase information for in-progress deployments
		if status == "in_progress" && deployment.HasPhase() && deployment.Phase() != "" {
			ctx.Printf("  Phase: %s\n", deployment.Phase())
		}

		// Deployed info
		user := deployment.DeployedByUserEmail()
		// Replace placeholder emails with username or user ID as fallback
		if user == "" || user == "unknown@example.com" || user == "user@example.com" {
			if deployment.HasDeployedByUserName() && deployment.DeployedByUserName() != "" {
				user = deployment.DeployedByUserName()
			} else if deployment.HasDeployedByUserId() && deployment.DeployedByUserId() != "" {
				user = deployment.DeployedByUserId()
			} else {
				user = "-"
			}
		}
		if user != "-" {
			ctx.Printf("  Deployed By: %s\n", user)
		}

		if deployment.HasDeployedAt() && deployment.DeployedAt() != nil {
			deployedAt := time.Unix(deployment.DeployedAt().Seconds(), 0)
			ctx.Printf("  Deployed: %s (%s)\n",
				deployedAt.Format("2006-01-02 15:04:05"),
				formatRelativeTime(deployedAt))
		}

		// Git info
		if deployment.HasGitInfo() && deployment.GitInfo() != nil {
			git := deployment.GitInfo()
			ctx.Printf("\n%s\n", labelStyle.Render("Git Information:"))

			if git.HasSha() && git.Sha() != "" {
				sha := git.Sha()
				if len(sha) > 8 {
					sha = sha[:8]
				}
				ctx.Printf("  Commit: %s", sha)

				if git.HasBranch() && git.Branch() != "" {
					ctx.Printf(" (%s)", git.Branch())
				}

				if git.HasIsDirty() && git.IsDirty() {
					ctx.Printf(" %s", yellowStyle.Render("[dirty]"))
				}
				ctx.Printf("\n")
			}

			if git.HasCommitMessage() && git.CommitMessage() != "" {
				msg := strings.TrimSpace(git.CommitMessage())
				if idx := strings.Index(msg, "\n"); idx > 0 {
					msg = msg[:idx] // First line only
				}
				if len(msg) > 60 {
					msg = msg[:57] + "..."
				}
				ctx.Printf("  Message: %s\n", msg)
			}

			if git.HasCommitAuthorName() && git.CommitAuthorName() != "" {
				ctx.Printf("  Author: %s", git.CommitAuthorName())
				if git.HasCommitAuthorEmail() && git.CommitAuthorEmail() != "" {
					ctx.Printf(" <%s>", git.CommitAuthorEmail())
				}
				ctx.Printf("\n")
			}

			if git.HasRepository() && git.Repository() != "" {
				ctx.Printf("  Repository: %s\n", git.Repository())
			}
		}

		// Error message if failed
		if status == "failed" && deployment.HasErrorMessage() && deployment.ErrorMessage() != "" {
			ctx.Printf("\n%s\n", labelStyle.Render("Error:"))
			ctx.Printf("  %s\n", redStyle.Render(deployment.ErrorMessage()))
		}
	} else {
		ctx.Printf("\n%s\n", yellowStyle.Render("No active deployment found"))
	}

	// Recent deployments summary
	ctx.Printf("\n%s\n", labelStyle.Render("Recent Activity:"))

	if recentResult != nil && recentResult.HasDeployments() && len(recentResult.Deployments()) > 0 {
		for i, dep := range recentResult.Deployments() {
			if i >= 3 { // Only show top 3
				break
			}

			timeStr := "unknown"
			if dep.HasDeployedAt() && dep.DeployedAt() != nil {
				deployedAt := time.Unix(dep.DeployedAt().Seconds(), 0)
				timeStr = formatRelativeTime(deployedAt)
			}

			status := dep.Status()
			var statusIcon string
			switch status {
			case "active":
				statusIcon = greenStyle.Render("✓")
			case "succeeded":
				statusIcon = blueStyle.Render("✓")
			case "failed":
				statusIcon = redStyle.Render("✗")
			default:
				statusIcon = yellowStyle.Render("○")
			}

			ctx.Printf("  %s %s - %s", statusIcon, timeStr, ui.DisplayShortID(dep.ShortId(), dep.Id()))

			if dep.HasGitInfo() && dep.GitInfo() != nil && dep.GitInfo().HasSha() {
				sha := dep.GitInfo().Sha()
				if len(sha) > 8 {
					sha = sha[:8]
				}
				ctx.Printf(" (%s)", sha)
			}
			ctx.Printf("\n")
		}

		if len(recentResult.Deployments()) > 3 {
			ctx.Printf("  %s\n", labelStyle.Render(fmt.Sprintf("... and %d more", len(recentResult.Deployments())-3)))
		}
	} else {
		ctx.Printf("  No recent deployments\n")
	}

	ctx.Printf("\nUse 'miren app history %s' for full deployment history\n", opts.App)

	return nil
}

func printAppStatusJSON(
	appResult *app_v1alpha.CrudClientGetConfigurationResults,
	appConfig *app_v1alpha.Configuration,
	activeDeployment *deployment_v1alpha.DeploymentClientGetActiveDeploymentResults,
	recentResult *deployment_v1alpha.DeploymentClientListDeploymentsResults,
	app, cluster string,
) error {
	type gitInfo struct {
		Sha               string `json:"sha,omitempty"`
		Branch            string `json:"branch,omitempty"`
		Repository        string `json:"repository,omitempty"`
		IsDirty           bool   `json:"is_dirty,omitempty"`
		CommitMessage     string `json:"commit_message,omitempty"`
		CommitAuthorName  string `json:"commit_author_name,omitempty"`
		CommitAuthorEmail string `json:"commit_author_email,omitempty"`
	}
	type configuration struct {
		EnvVars []string `json:"env_vars,omitempty"`
	}
	type deploymentJSON struct {
		ID           string   `json:"id"`
		Status       string   `json:"status"`
		AppVersionID string   `json:"app_version_id,omitempty"`
		DeployedBy   string   `json:"deployed_by,omitempty"`
		DeployedAt   string   `json:"deployed_at,omitempty"`
		Phase        string   `json:"phase,omitempty"`
		ErrorMessage string   `json:"error_message,omitempty"`
		GitInfo      *gitInfo `json:"git_info,omitempty"`
	}

	marshalDeployment := func(dep *deployment_v1alpha.DeploymentInfo) deploymentJSON {
		d := deploymentJSON{
			ID:           dep.Id(),
			Status:       dep.Status(),
			AppVersionID: dep.AppVersionId(),
		}

		// Deployed by: prefer username, fall back to email, then user ID
		if dep.HasDeployedByUserName() && dep.DeployedByUserName() != "" {
			d.DeployedBy = dep.DeployedByUserName()
		} else if dep.HasDeployedByUserEmail() && dep.DeployedByUserEmail() != "" &&
			dep.DeployedByUserEmail() != "unknown@example.com" && dep.DeployedByUserEmail() != "user@example.com" {
			d.DeployedBy = dep.DeployedByUserEmail()
		} else if dep.HasDeployedByUserId() && dep.DeployedByUserId() != "" {
			d.DeployedBy = dep.DeployedByUserId()
		}

		if dep.HasDeployedAt() && dep.DeployedAt() != nil {
			d.DeployedAt = time.Unix(dep.DeployedAt().Seconds(), 0).UTC().Format(time.RFC3339)
		}

		if dep.HasPhase() && dep.Phase() != "" {
			d.Phase = dep.Phase()
		}

		if dep.HasErrorMessage() && dep.ErrorMessage() != "" {
			d.ErrorMessage = dep.ErrorMessage()
		}

		if dep.HasGitInfo() && dep.GitInfo() != nil {
			git := dep.GitInfo()
			gi := &gitInfo{}
			hasData := false
			if git.HasSha() && git.Sha() != "" {
				gi.Sha = git.Sha()
				hasData = true
			}
			if git.HasBranch() && git.Branch() != "" {
				gi.Branch = git.Branch()
				hasData = true
			}
			if git.HasRepository() && git.Repository() != "" {
				gi.Repository = git.Repository()
				hasData = true
			}
			if git.HasIsDirty() && git.IsDirty() {
				gi.IsDirty = true
				hasData = true
			}
			if git.HasCommitMessage() && git.CommitMessage() != "" {
				gi.CommitMessage = git.CommitMessage()
				hasData = true
			}
			if git.HasCommitAuthorName() && git.CommitAuthorName() != "" {
				gi.CommitAuthorName = git.CommitAuthorName()
				hasData = true
			}
			if git.HasCommitAuthorEmail() && git.CommitAuthorEmail() != "" {
				gi.CommitAuthorEmail = git.CommitAuthorEmail()
				hasData = true
			}
			if hasData {
				d.GitInfo = gi
			}
		}

		return d
	}

	output := struct {
		App               string           `json:"app"`
		Cluster           string           `json:"cluster"`
		CurrentVersion    string           `json:"current_version,omitempty"`
		WorkloadRole      string           `json:"workload_role,omitempty"`
		MaintenanceRoutes []string         `json:"maintenance_routes,omitempty"`
		Configuration     *configuration   `json:"configuration,omitempty"`
		ActiveDeployment  *deploymentJSON  `json:"active_deployment,omitempty"`
		RecentDeployments []deploymentJSON `json:"recent_deployments,omitempty"`
	}{
		App:               app,
		Cluster:           cluster,
		WorkloadRole:      appResult.WorkloadRole(),
		MaintenanceRoutes: appResult.MaintenanceRoutes(),
	}

	if appResult.HasVersionId() && appResult.VersionId() != "" {
		output.CurrentVersion = appResult.VersionId()
	}

	if appConfig != nil {
		cfg := &configuration{}
		if appConfig.HasEnvVars() && len(appConfig.EnvVars()) > 0 {
			for _, kv := range appConfig.EnvVars() {
				if kv.HasKey() {
					cfg.EnvVars = append(cfg.EnvVars, kv.Key())
				}
			}
			sort.Strings(cfg.EnvVars)
		}
		if len(cfg.EnvVars) > 0 {
			output.Configuration = cfg
		}
	}

	if activeDeployment != nil && activeDeployment.HasDeployment() {
		d := marshalDeployment(activeDeployment.Deployment())
		output.ActiveDeployment = &d
	}

	if recentResult != nil && recentResult.HasDeployments() {
		for _, dep := range recentResult.Deployments() {
			output.RecentDeployments = append(output.RecentDeployments, marshalDeployment(dep))
		}
	}

	return PrintJSON(output)
}

// maintenanceRouteList names the routes in maintenance for the status line. The
// default route has no hostname of its own, so it's named rather than shown as
// a blank.
func maintenanceRouteList(hosts []string) string {
	named := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h == "" {
			h = "the default route"
		}
		named = append(named, h)
	}

	return strings.Join(named, ", ")
}
