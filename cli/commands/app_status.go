package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

type AppStatusOptions struct {
	AppCentric
	FormatOptions
	Graph bool `short:"g" long:"graph" description:"Include resource usage graphs"`
}

func AppStatus(ctx *Context, opts AppStatusOptions) error {
	// Connect to app service
	appCl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return fmt.Errorf("failed to connect to app service: %w", err)
	}
	defer appCl.Close()
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
	defer depCl.Close()
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	// Target aliases resolve to a concrete cluster during context setup.
	clusterId := ctx.ClusterName

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
	health, healthErr := fetchServiceHealth(ctx, opts.App)

	// JSON output
	if opts.IsJSON() {
		return printAppStatusJSON(appResult, appConfig, activeDeployment, recentResult, opts.App, clusterId, health, healthErr)
	}

	items := []ui.NamedValue{
		ui.NewNamedValue("App", opts.App),
		ui.NewNamedValue("Cluster", clusterId),
	}
	if appResult.HasVersionId() && appResult.VersionId() != "" {
		items = append(items, ui.NewNamedValue("Current Version", ui.DisplayShortID(appResult.VersionShortId(), appResult.VersionId())))
	} else {
		items = append(items, ui.NewNamedValue("Current Version", "No version deployed"))
	}
	if source := formatSource(appResult.SourceKind(), appResult.SourceValue()); source != "" {
		items = append(items, ui.NewNamedValue("Source", source))
	}
	if down := appResult.MaintenanceRoutes(); len(down) > 0 {
		items = append(items, ui.NewNamedValue("Maintenance", fmt.Sprintf("%s serving a holding page", maintenanceRouteList(down))))
	}

	if activeDeployment != nil && activeDeployment.HasDeployment() {
		deployment := activeDeployment.Deployment()
		items = append(items,
			ui.NewNamedValue("Deployment", ui.DisplayShortID(deployment.ShortId(), deployment.Id())),
			ui.NewNamedValue("Status", deployment.Status()),
		)
		if deployment.Phase() != "" {
			items = append(items, ui.NewNamedValue("Phase", deployment.Phase()))
		}
		if deployment.ErrorMessage() != "" {
			items = append(items, ui.NewNamedValue("Error", deployment.ErrorMessage()))
		}
	} else {
		items = append(items, ui.NewNamedValue("Deployment", "No active deployment found"))
	}
	ctx.Printf("%s\n\n", ui.NewNamedValueList(items).Render())

	switch {
	case healthErr != nil:
		ctx.Printf("%s\n", ui.NewHint(fmt.Sprintf("Health unavailable: %v", healthErr)).Render())
	case len(health) == 0:
		ctx.Printf("%s\n", ui.NewHint("No service pools found").Render())
	default:
		ctx.Printf("%s", renderServiceHealth(health))
	}

	if opts.Graph {
		// Reuse the dashboard's one-shot graph renderer without taking over the
		// terminal or starting a watch loop.
		if err := appStatusGraphs(ctx, opts.AppCentric); err != nil {
			ctx.Printf("%s\n", ui.NewHint(fmt.Sprintf("Usage unavailable: %v", err)).Render())
		}
	}
	ctx.Printf("\n%s\n", ui.NewHint(fmt.Sprintf("Use 'miren app history -a %s' for deployment history; 'miren app watch -a %s' for live usage", opts.App, opts.App)).Render())

	return nil
}

func printAppStatusJSON(
	appResult *app_v1alpha.CrudClientGetConfigurationResults,
	appConfig *app_v1alpha.Configuration,
	activeDeployment *deployment_v1alpha.DeploymentClientGetActiveDeploymentResults,
	recentResult *deployment_v1alpha.DeploymentClientListDeploymentsResults,
	app, cluster string, health []serviceHealth, healthErr error,
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
	type sourceJSON struct {
		Kind  string `json:"kind"`
		Value string `json:"value,omitempty"`
	}
	type deploymentJSON struct {
		ID           string          `json:"id"`
		Status       string          `json:"status"`
		AppVersionID string          `json:"app_version_id,omitempty"`
		DeployedBy   *deployedByJSON `json:"deployed_by,omitempty"`
		DeployedAt   string          `json:"deployed_at,omitempty"`
		Phase        string          `json:"phase,omitempty"`
		ErrorMessage string          `json:"error_message,omitempty"`
		GitInfo      *gitInfo        `json:"git_info,omitempty"`
	}

	marshalDeployment := func(dep *deployment_v1alpha.DeploymentInfo) deploymentJSON {
		d := deploymentJSON{
			ID:           dep.Id(),
			Status:       dep.Status(),
			AppVersionID: dep.AppVersionId(),
		}

		d.DeployedBy = deployedByOf(dep)

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
		Source            *sourceJSON      `json:"source,omitempty"`
		WorkloadRole      string           `json:"workload_role,omitempty"`
		MaintenanceRoutes []string         `json:"maintenance_routes,omitempty"`
		Configuration     *configuration   `json:"configuration,omitempty"`
		ActiveDeployment  *deploymentJSON  `json:"active_deployment,omitempty"`
		RecentDeployments []deploymentJSON `json:"recent_deployments,omitempty"`
		Services          []serviceHealth  `json:"services,omitempty"`
		HealthError       string           `json:"health_error,omitempty"`
	}{
		App:               app,
		Cluster:           cluster,
		WorkloadRole:      appResult.WorkloadRole(),
		MaintenanceRoutes: appResult.MaintenanceRoutes(),
		Services:          health,
	}
	if healthErr != nil {
		output.HealthError = healthErr.Error()
	}

	if appResult.HasVersionId() && appResult.VersionId() != "" {
		output.CurrentVersion = appResult.VersionId()
	}
	if kind := appResult.SourceKind(); kind != "" {
		output.Source = &sourceJSON{Kind: kind, Value: appResult.SourceValue()}
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
