package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
)

func AppStatus(ctx *Context, opts struct {
	AppCentric
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

	// Define styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	// Print header
	ctx.Printf("%s\n\n", headerStyle.Render(fmt.Sprintf("Status for %s", opts.App)))

	// App info
	ctx.Printf("%s %s\n", labelStyle.Render("App:"), opts.App)
	ctx.Printf("%s %s\n", labelStyle.Render("Cluster:"), clusterId)

	// Version info
	if appResult.HasVersionId() && appResult.VersionId() != "" {
		ctx.Printf("%s %s\n", labelStyle.Render("Current Version:"), appResult.VersionId())
	} else {
		ctx.Printf("%s %s\n", labelStyle.Render("Current Version:"), yellowStyle.Render("No version deployed"))
	}

	// Configuration
	if appConfig != nil {
		ctx.Printf("\n%s\n", labelStyle.Render("Configuration:"))
		if appConfig.HasConcurrency() && appConfig.Concurrency() > 0 {
			ctx.Printf("  Concurrency: %d\n", appConfig.Concurrency())
		}

		// Environment variables
		if appConfig.HasEnvVars() && len(appConfig.EnvVars()) > 0 {
			ctx.Printf("\n%s\n", labelStyle.Render("Environment Variables:"))
			for _, kv := range appConfig.EnvVars() {
				if kv.HasKey() && kv.HasValue() {
					// Mask sensitive values
					value := kv.Value()
					key := kv.Key()
					if isSensitiveKey(key) && len(value) > 0 {
						value = value[:1] + strings.Repeat("*", len(value)-1)
					}
					ctx.Printf("  %s=%s\n", key, value)
				}
			}
		}
	}

	// Active deployment info
	if activeDeployment != nil && activeDeployment.HasDeployment() {
		deployment := activeDeployment.Deployment()
		ctx.Printf("\n%s\n", labelStyle.Render("Active Deployment:"))

		// Deployment ID
		ctx.Printf("  ID: %s\n", deployment.Id())

		// Status with color
		status := deployment.Status()
		var styledStatus string
		switch status {
		case "active":
			styledStatus = greenStyle.Render(status)
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
		if deployment.HasDeployedByUserEmail() && deployment.DeployedByUserEmail() != "" {
			ctx.Printf("  Deployed By: %s\n", deployment.DeployedByUserEmail())
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

	// Get last 5 deployments
	recentResult, err := depClient.ListDeployments(ctx, opts.App, clusterId, "", 5)
	if err == nil && recentResult.HasDeployments() && len(recentResult.Deployments()) > 0 {
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
			case "failed":
				statusIcon = redStyle.Render("✗")
			default:
				statusIcon = yellowStyle.Render("○")
			}

			ctx.Printf("  %s %s - %s", statusIcon, timeStr, dep.Id()[:8])

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

// isSensitiveKey checks if an environment variable key might contain sensitive data
func isSensitiveKey(key string) bool {
	lowerKey := strings.ToLower(key)
	sensitivePatterns := []string{
		"password", "passwd", "pwd",
		"secret", "token", "key",
		"api_key", "apikey",
		"auth", "credential",
		"private",
	}

	for _, pattern := range sensitivePatterns {
		if strings.Contains(lowerKey, pattern) {
			return true
		}
	}
	return false
}
