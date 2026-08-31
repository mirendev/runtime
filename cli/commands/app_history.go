package commands

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/theme"
	"miren.dev/runtime/pkg/ui"
)

func printAppHistoryJSON(deployments []*deployment_v1alpha.DeploymentInfo, app, cluster string) error {
	type gitInfoJSON struct {
		Sha               string `json:"sha,omitempty"`
		Branch            string `json:"branch,omitempty"`
		Repository        string `json:"repository,omitempty"`
		IsDirty           bool   `json:"is_dirty,omitempty"`
		CommitMessage     string `json:"commit_message,omitempty"`
		CommitAuthorName  string `json:"commit_author_name,omitempty"`
		CommitAuthorEmail string `json:"commit_author_email,omitempty"`
	}
	type deploymentJSON struct {
		ID                 string       `json:"id"`
		Status             string       `json:"status"`
		AppVersionID       string       `json:"app_version_id,omitempty"`
		DeployedAt         string       `json:"deployed_at,omitempty"`
		DeployedByUserName string       `json:"deployed_by_user_name,omitempty"`
		Phase              string       `json:"phase,omitempty"`
		ErrorMessage       string       `json:"error_message,omitempty"`
		GitInfo            *gitInfoJSON `json:"git_info,omitempty"`
	}

	var deps []deploymentJSON
	for _, dep := range deployments {
		d := deploymentJSON{
			ID:           dep.Id(),
			Status:       dep.Status(),
			AppVersionID: dep.AppVersionId(),
		}

		if dep.HasDeployedByUserName() && dep.DeployedByUserName() != "" {
			d.DeployedByUserName = dep.DeployedByUserName()
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
			gi := &gitInfoJSON{}
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

		deps = append(deps, d)
	}

	output := struct {
		App         string           `json:"app"`
		Cluster     string           `json:"cluster"`
		Deployments []deploymentJSON `json:"deployments"`
	}{
		App:         app,
		Cluster:     cluster,
		Deployments: deps,
	}

	return PrintJSON(output)
}

// Deployment status styles
var statusStyles = map[string]struct {
	icon  string
	style lipgloss.Style
}{
	"active":      {"✓", lipgloss.NewStyle().Foreground(theme.Success)}, // Green
	"succeeded":   {"✓", lipgloss.NewStyle().Foreground(theme.Info)},    // Blue
	"failed":      {"✗", lipgloss.NewStyle().Foreground(theme.Error)},   // Red
	"rolled_back": {"↩", lipgloss.NewStyle().Foreground(theme.Warning)}, // Yellow
	"in_progress": {"⟳", lipgloss.NewStyle().Foreground(theme.Info)},    // Blue
	"cancelled":   {"⊘", lipgloss.NewStyle().Foreground(theme.Warning)}, // Yellow
}

type historyDisplayOpts struct {
	detailed    bool
	hasIdentity bool
}

func AppHistory(ctx *Context, opts struct {
	AppCentric
	FormatOptions

	Limit    int  `short:"n" long:"limit" description:"Maximum number of deployments to show" default:"10"`
	All      bool `long:"all" description:"Show all deployments (ignore limit)"`
	Detailed bool `long:"detailed" description:"Show all columns including git information"`
}) error {
	depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return fmt.Errorf("failed to connect to deployment service: %w", err)
	}
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	limit := int32(opts.Limit)
	if opts.All {
		limit = math.MaxInt32
	}

	result, err := depClient.ListDeployments(ctx, opts.App, ctx.ClusterName, "", limit)
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	deployments := result.Deployments()

	// Sort by time (most recent first)
	sortDeployments(deployments)

	// JSON output
	if opts.IsJSON() {
		return printAppHistoryJSON(deployments, opts.App, ctx.ClusterName)
	}

	if len(deployments) == 0 {
		printNoDeploymentsMessage(ctx, opts.App)
		return nil
	}

	// Print header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Info)
	ctx.Printf("%s\n", headerStyle.Render(fmt.Sprintf("Deployment History for %s", opts.App)))
	ctx.Printf("Cluster: %s\n", ctx.ClusterName)
	ctx.Printf("\n")

	displayOpts := historyDisplayOpts{
		detailed:    opts.Detailed,
		hasIdentity: ctx.ClusterConfig != nil && (ctx.ClusterConfig.Identity != "" || ctx.ClusterConfig.CloudAuth),
	}

	// Build and render table
	headers, rows, builder := buildDeploymentTable(deployments, displayOpts)
	columns := ui.AutoSizeColumns(headers, rows, builder)
	table := ui.NewTable(ui.WithColumns(columns), ui.WithRows(rows))
	ctx.Printf("%s\n", table.Render())
	return nil
}

func printNoDeploymentsMessage(ctx *Context, app string) {
	ctx.Printf("No deployments found for app '%s' on cluster '%s'\n", app, ctx.ClusterName)
}

func sortDeployments(deployments []*deployment_v1alpha.DeploymentInfo) {
	sort.Slice(deployments, func(i, j int) bool {
		// Sort by time (most recent first)
		var iTime, jTime int64
		if deployments[i].HasDeployedAt() && deployments[i].DeployedAt() != nil {
			iTime = deployments[i].DeployedAt().Seconds()
		}
		if deployments[j].HasDeployedAt() && deployments[j].DeployedAt() != nil {
			jTime = deployments[j].DeployedAt().Seconds()
		}
		return iTime > jTime
	})
}

func buildDeploymentTable(deployments []*deployment_v1alpha.DeploymentInfo, opts historyDisplayOpts) ([]string, []ui.Row, *ui.ColumnBuilder) {
	var headers []string
	var rows []ui.Row
	var builder *ui.ColumnBuilder

	if opts.detailed {
		headers = []string{"STATUS", "VERSION"}
		if opts.hasIdentity {
			headers = append(headers, "DEPLOYED BY")
		}
		headers = append(headers, "WHEN", "ID", "ERROR", "GIT SHA", "BRANCH", "COMMIT MESSAGE")
		// Find ID column index dynamically
		idColIndex := -1
		for i, h := range headers {
			if h == "ID" {
				idColIndex = i
				break
			}
		}
		builder = ui.Columns().
			NoTruncate(0, idColIndex).   // STATUS and ID
			MaxWidth(len(headers)-1, 40) // COMMIT MESSAGE
	} else {
		headers = []string{"STATUS", "VERSION"}
		if opts.hasIdentity {
			headers = append(headers, "DEPLOYED BY")
		}
		headers = append(headers, "WHEN", "GIT SHA", "BRANCH")
		builder = ui.Columns().
			NoTruncate(0) // STATUS
	}

	for _, dep := range deployments {
		row := buildDeploymentRow(dep, opts)
		rows = append(rows, row)
	}

	return headers, rows, builder
}

func buildDeploymentRow(dep *deployment_v1alpha.DeploymentInfo, opts historyDisplayOpts) ui.Row {
	status := dep.Status()

	row := ui.Row{
		formatDeploymentStatus(status),
		formatVersionWithShortID(dep.AppVersionShortId(), dep.AppVersionId(), status),
	}

	if opts.hasIdentity {
		row = append(row, formatUser(dep))
	}

	row = append(row, formatDeploymentTime(dep))

	if opts.detailed {
		gitSha, gitBranch, gitMessage := formatGitInfo(dep)
		row = append(row, ui.DisplayShortID(dep.ShortId(), dep.Id()), formatErrorInfo(dep, status), gitSha, gitBranch, gitMessage)
	} else {
		gitSha, gitBranch, _ := formatGitInfo(dep)
		row = append(row, gitSha, gitBranch)
	}

	return row
}

func formatDeploymentStatus(status string) string {
	if s, ok := statusStyles[status]; ok {
		return s.style.Render(s.icon + " " + status)
	}
	return status
}

func formatVersion(version, status string) string {
	if strings.HasPrefix(version, "pending-") {
		if status == "in_progress" {
			return "pending (building...)"
		}
		return "-"
	}
	if strings.HasPrefix(version, "failed-") {
		return "-"
	}
	return version
}

func formatVersionWithShortID(shortID, version, status string) string {
	if shortID != "" {
		return formatVersion(shortID, status)
	}
	return formatVersion(ui.DisplayAppVersion(version), status)
}

func formatDeploymentTime(dep *deployment_v1alpha.DeploymentInfo) string {
	if dep.HasDeployedAt() && dep.DeployedAt() != nil {
		return formatRelativeTime(time.Unix(dep.DeployedAt().Seconds(), 0))
	}
	return "unknown"
}

func formatUser(dep *deployment_v1alpha.DeploymentInfo) string {
	if dep.HasDeployedByUserName() && dep.DeployedByUserName() != "" {
		return dep.DeployedByUserName()
	}
	return "-"
}

func formatGitInfo(dep *deployment_v1alpha.DeploymentInfo) (sha, branch, message string) {
	sha, branch, message = "-", "-", "-"

	if !dep.HasGitInfo() || dep.GitInfo() == nil {
		return
	}

	git := dep.GitInfo()
	if git.HasSha() && git.Sha() != "" {
		sha = git.Sha()
		if len(sha) > 10 {
			sha = sha[:10]
		}
		if git.HasIsDirty() && git.IsDirty() {
			sha += "-dirty"
		}
	}
	if git.HasBranch() && git.Branch() != "" {
		branch = git.Branch()
	}
	if git.HasCommitMessage() && git.CommitMessage() != "" {
		message = strings.TrimSpace(git.CommitMessage())
		if idx := strings.Index(message, "\n"); idx > 0 {
			message = message[:idx]
		}
	}
	return
}

func formatErrorInfo(dep *deployment_v1alpha.DeploymentInfo, status string) string {
	if status == "in_progress" && dep.HasPhase() && dep.Phase() != "" {
		return statusStyles["in_progress"].style.Render(firstLine(dep.Phase()))
	}
	if dep.HasErrorMessage() && dep.ErrorMessage() != "" {
		msg := firstLine(dep.ErrorMessage())
		switch status {
		case "failed":
			return statusStyles["failed"].style.Render(msg)
		case "cancelled":
			return statusStyles["cancelled"].style.Render(msg)
		}
	}
	return "-"
}

func firstLine(s string) string {
	if idx := strings.Index(s, "\n"); idx > 0 {
		return s[:idx]
	}
	return s
}

func formatRelativeTime(t time.Time) string {
	diff := time.Since(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}
