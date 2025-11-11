package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
)

func AppHistory(ctx *Context, opts struct {
	AppCentric

	Limit      int32  `short:"n" long:"limit" description:"Maximum number of deployments to show" default:"20"`
	Status     string `short:"s" long:"status" description:"Filter by status (active, failed, rolled_back)"`
	All        bool   `long:"all" description:"Show deployments from all clusters"`
	ShowFailed bool   `long:"show-failed" description:"Include failed deployments (shown by default)"`
	HideFailed bool   `long:"hide-failed" description:"Hide failed deployments"`
	Detailed   bool   `long:"detailed" description:"Show all columns including git information"`
}) error {
	// Connect to deployment service
	depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return fmt.Errorf("failed to connect to deployment service: %w", err)
	}
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	// Get cluster filter
	clusterId := ""
	if !opts.All {
		clusterId = ctx.ClusterName
	}

	// List deployments
	result, err := depClient.ListDeployments(ctx, opts.App, clusterId, opts.Status, opts.Limit)
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	if !result.HasDeployments() || len(result.Deployments()) == 0 {
		ctx.Printf("No deployments found for app '%s'", opts.App)
		if !opts.All {
			ctx.Printf(" on cluster '%s'", ctx.ClusterName)
		}
		if opts.Status != "" {
			ctx.Printf(" with status '%s'", opts.Status)
		}
		ctx.Printf("\n")
		return nil
	}

	// Display deployments
	deployments := result.Deployments()

	// Filter out failed deployments if requested
	if opts.HideFailed {
		var filtered []*deployment_v1alpha.DeploymentInfo
		for _, dep := range deployments {
			if dep.Status() != "failed" {
				filtered = append(filtered, dep)
			}
		}
		deployments = filtered

		// Check if all deployments were filtered out
		if len(deployments) == 0 {
			ctx.Printf("No deployments found for app '%s'", opts.App)
			if !opts.All {
				ctx.Printf(" on cluster '%s'", ctx.ClusterName)
			}
			if opts.Status != "" {
				ctx.Printf(" with status '%s'", opts.Status)
			}
			ctx.Printf(" (failed deployments hidden)\n")
			return nil
		}
	}

	// Header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	ctx.Printf("%s\n", headerStyle.Render(fmt.Sprintf("Deployment History for %s", opts.App)))
	if !opts.All {
		ctx.Printf("Cluster: %s\n", ctx.ClusterName)
	}
	ctx.Printf("\n")

	// Table header
	if opts.Detailed {
		ctx.Printf("%-12s %-8s %-25s %-20s %-15s %-17s %-15s %-33s\n",
			"STATUS", "CLUSTER", "VERSION", "DEPLOYED BY", "WHEN", "GIT SHA", "BRANCH", "COMMIT MESSAGE")
		ctx.Printf("%s\n", strings.Repeat("-", 160))
	} else {
		ctx.Printf("%-12s %-25s %-25s %-15s\n",
			"STATUS", "VERSION", "DEPLOYED BY", "WHEN")
		ctx.Printf("%s\n", strings.Repeat("-", 80))
	}

	// Status colors
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))     // Green
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))      // Red
	rolledBackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // Yellow
	inProgressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")) // Cyan

	for _, dep := range deployments {
		// Format status with color and icons
		status := dep.Status()
		var styledStatus string
		switch status {
		case "active":
			styledStatus = activeStyle.Render("✓ " + status)
		case "failed":
			styledStatus = failedStyle.Render("✗ " + status)
		case "rolled_back":
			styledStatus = rolledBackStyle.Render("↩ " + status)
		case "in_progress":
			styledStatus = inProgressStyle.Render("⟳ " + status)
		default:
			styledStatus = status
		}

		// Format timestamp
		timeStr := "unknown"
		if dep.HasDeployedAt() && dep.DeployedAt() != nil {
			deployedAt := time.Unix(dep.DeployedAt().Seconds(), 0)
			timeStr = formatRelativeTime(deployedAt)
		}

		// Format cluster
		cluster := dep.ClusterId()
		if cluster == "" {
			cluster = "default"
		}

		// Format version (handle special patterns)
		version := dep.AppVersionId()
		if strings.HasPrefix(version, "pending-") {
			version = "pending (building...)"
		} else if strings.HasPrefix(version, "failed-") {
			version = "failed (no version)"
		} else if len(version) > 25 {
			version = version[:22] + "..."
		}

		// Format user (prefer email, fallback to username, then user ID)
		user := dep.DeployedByUserEmail()
		// Replace placeholder emails with username or user ID as fallback
		if user == "" || user == "unknown@example.com" || user == "user@example.com" {
			if dep.HasDeployedByUserName() && dep.DeployedByUserName() != "" {
				user = dep.DeployedByUserName()
			} else {
				user = dep.DeployedByUserId()
			}
		}
		if len(user) > 20 {
			user = user[:17] + "..."
		}

		// Format git info
		gitSha := "-"
		gitBranch := "-"
		gitMessage := "-"

		if dep.HasGitInfo() && dep.GitInfo() != nil {
			git := dep.GitInfo()
			if git.HasSha() && git.Sha() != "" {
				gitSha = git.Sha()
				if len(gitSha) > 10 {
					gitSha = gitSha[:10]
				}
				// Append -dirty if working tree was dirty
				if git.HasIsDirty() && git.IsDirty() {
					gitSha += "-dirty"
				}
			}
			if git.HasBranch() && git.Branch() != "" {
				gitBranch = git.Branch()
				if len(gitBranch) > 15 {
					gitBranch = gitBranch[:12] + "..."
				}
			}
			if git.HasCommitMessage() && git.CommitMessage() != "" {
				gitMessage = strings.TrimSpace(git.CommitMessage())
				if idx := strings.Index(gitMessage, "\n"); idx > 0 {
					gitMessage = gitMessage[:idx]
				}
				if len(gitMessage) > 40 {
					gitMessage = gitMessage[:37] + "..."
				}
			}
		}

		if opts.Detailed {
			ctx.Printf("%-12s %-8s %-25s %-20s %-15s %-17s %-15s %-33s\n",
				styledStatus,
				cluster,
				version,
				user,
				timeStr,
				gitSha,
				gitBranch,
				gitMessage)
		} else {
			ctx.Printf("%-12s %-25s %-25s %-15s\n",
				styledStatus,
				version,
				user,
				timeStr)
		}

		// Show phase for in-progress deployments
		if status == "in_progress" && dep.HasPhase() && dep.Phase() != "" {
			phaseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Italic(true)
			ctx.Printf("             %s\n", phaseStyle.Render("Phase: "+dep.Phase()))
		}

		// Show error message if failed
		if status == "failed" && dep.HasErrorMessage() && dep.ErrorMessage() != "" {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Italic(true)
			ctx.Printf("             %s\n", errorStyle.Render("Error: "+dep.ErrorMessage()))
		}
	}

	return nil
}

// formatRelativeTime formats a time as a relative string (e.g. "2 hours ago")
func formatRelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		return fmt.Sprintf("%dh ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	default:
		return t.Format("Jan 2")
	}
}
