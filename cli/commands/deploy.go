package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/progress/progresswriter"
	"golang.org/x/term"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/deploygating"
	"miren.dev/runtime/pkg/git"
	"miren.dev/runtime/pkg/progress/upload"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
	"miren.dev/runtime/pkg/tarx"
	"miren.dev/runtime/pkg/ui"
)

func Deploy(ctx *Context, opts struct {
	AppCentric

	Analyze       bool   `long:"analyze" description:"Analyze the app without building (show detected stack, services, etc.)"`
	Explain       bool   `short:"x" long:"explain" description:"Explain the build process"`
	ExplainFormat string `long:"explain-format" description:"Explain format" choice:"auto" choice:"plain" choice:"tty" choice:"rawjson" default:"auto"` //nolint
	Force         bool   `short:"f" long:"force" description:"Skip confirmation prompt"`
}) error {
	name := opts.App
	dir := opts.Dir

	if ctx.ClientConfig == nil {
		return fmt.Errorf("no client configuration available; run `miren login` to authenticate or install a server locally")
	}

	isInteractive := term.IsTerminal(int(os.Stdin.Fd()))

	// Check that we have at least one cluster configured
	// Check if we have an identity - if so, offer to add a cluster
	if isInteractive &&
		ctx.ClientConfig.GetClusterCount() == 0 &&
		ctx.ClientConfig.HasIdentities() {
		confirmed, err := ui.Confirm(
			ui.WithMessage("No clusters configured. Would you like to add one now?"),
			ui.WithDefault(true),
			ui.WithIndent("  "),
		)
		if err != nil || !confirmed {
			return fmt.Errorf("no clusters configured; run 'miren login' to authenticate and 'miren cluster add' to configure a cluster, or install a server locally")
		}

		if err := AddClusterInteractive(ctx); err != nil {
			return fmt.Errorf("failed to add cluster: %w", err)
		}

		// Reload the client config
		cfg, err := clientconfig.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to reload config after adding cluster: %w", err)
		}
		ctx.ClientConfig = cfg

		// Get the active cluster (the one we just added)
		clusterName := cfg.ActiveCluster()
		if clusterName == "" {
			return fmt.Errorf("no active cluster set after adding cluster")
		}

		clusterCfg, err := cfg.GetCluster(clusterName)
		if err == nil && clusterCfg != nil {
			ctx.ClusterConfig = clusterCfg
			ctx.ClusterName = clusterName
		}

		ctx.Info("")
	}

	// Re-check after potential cluster add
	if ctx.ClientConfig.GetClusterCount() == 0 {
		return fmt.Errorf("no clusters configured; run 'miren login' to authenticate and configure a cluster, or install a server locally")
	}

	// Handle --analyze flag: analyze the app without building
	if opts.Analyze {
		cl, err := ctx.RPCClient("dev.miren.runtime/build")
		if err != nil {
			return err
		}

		bc := build_v1alpha.NewBuilderClient(cl)
		return analyzeApp(ctx, bc, dir)
	}

	// Confirm deployment unless --force is used, stdin is not a TTY, or only one cluster is configured
	hasSingleCluster := ctx.ClientConfig != nil && ctx.ClientConfig.GetClusterCount() == 1
	if !opts.Force && isInteractive && !hasSingleCluster {
		message := fmt.Sprintf("Deploy app '%s' to cluster '%s'?", name, ctx.ClusterName)
		confirmed, err := ui.Confirm(
			ui.WithMessage(message),
			ui.WithDefault(true),
			ui.WithIndent("  "),
		)
		if err != nil {
			return fmt.Errorf("confirmation cancelled: %w", err)
		}
		if !confirmed {
			ctx.Printf("  deployment cancelled\n")
			return nil
		}
	}

	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	faintStyle := lipgloss.NewStyle().Faint(true)
	ctx.Printf("  ✓ %s: %s %s %s\n", greenStyle.Render("Deploying"), name, faintStyle.Render("→"), ctx.ClusterName)

	cl, err := ctx.RPCClient("dev.miren.runtime/build")
	if err != nil {
		return err
	}

	bc := build_v1alpha.NewBuilderClient(cl)

	// Check if deployment is allowed before proceeding
	remedy, err := deploygating.CheckDeployAllowed(dir)
	if err != nil {
		if remedy != "" {
			ctx.Printf("Error: %s\n%s\n\n", err, remedy)
		}
		return fmt.Errorf("deploy gate check failed: %w", err)
	}

	ctx.Log.Info("building code", "name", name, "dir", dir)

	// Capture git information before creating deployment record
	var gitInfo *git.Info
	gitInfo, gitErr := git.GetInfo(dir)
	if gitErr != nil {
		ctx.Log.Debug("Failed to get git info", "error", gitErr)
		// Don't fail deployment if git info is unavailable
	}

	// Create deployment record early in the process
	depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return fmt.Errorf("failed to connect to deployment service: %w", err)
	}
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	// Convert git.Info to deployment GitInfo
	var deploymentGitInfo *deployment_v1alpha.GitInfo
	if gitInfo != nil {
		deploymentGitInfo = &deployment_v1alpha.GitInfo{}
		deploymentGitInfo.SetSha(gitInfo.SHA)
		deploymentGitInfo.SetBranch(gitInfo.Branch)
		deploymentGitInfo.SetIsDirty(gitInfo.IsDirty)
		deploymentGitInfo.SetWorkingTreeHash(gitInfo.WorkingTreeHash)
		deploymentGitInfo.SetCommitMessage(gitInfo.CommitMessage)
		deploymentGitInfo.SetCommitAuthorName(gitInfo.CommitAuthor)
		deploymentGitInfo.SetCommitAuthorEmail(gitInfo.CommitEmail)
		deploymentGitInfo.SetRepository(gitInfo.RemoteURL)

		// Convert timestamp string to standard.Timestamp if available
		if gitInfo.CommitTimestamp != "" {
			if ts, err := time.Parse(time.RFC3339, gitInfo.CommitTimestamp); err == nil {
				deploymentGitInfo.SetCommitTimestamp(standard.ToTimestamp(ts))
			}
		}
	}

	// Create deployment as "in_progress" with a temporary app version
	createResult, err := depClient.CreateDeployment(ctx, name, ctx.ClusterName, "pending-build", deploymentGitInfo)
	if err != nil {
		return fmt.Errorf("failed to create deployment record: %w", err)
	}

	if createResult.HasError() && createResult.Error() != "" {
		// Check if we have structured lock info
		if createResult.HasLockInfo() && createResult.LockInfo() != nil {
			lockInfo := createResult.LockInfo()

			// Format the deployment lock message
			ctx.Printf("\n❌ Deployment blocked:\n\n")
			ctx.Printf("Another deployment is already in progress for app '%s' on cluster '%s'.\n\n",
				lockInfo.AppName(), lockInfo.ClusterId())

			ctx.Printf("Existing deployment details:\n")
			ctx.Printf("  • Deployment ID: %s\n", lockInfo.BlockingDeploymentId())
			ctx.Printf("  • Started by: %s\n", lockInfo.StartedBy())

			if lockInfo.HasStartedAt() && lockInfo.StartedAt() != nil {
				startedAt := time.Unix(lockInfo.StartedAt().Seconds(), 0)
				ctx.Printf("  • Started at: %s (%s ago)\n",
					startedAt.Format("2006-01-02 15:04:05 MST"),
					time.Since(startedAt).Round(time.Second))
			}

			ctx.Printf("  • Current phase: %s\n", lockInfo.CurrentPhase())

			if lockInfo.HasLockExpiresAt() && lockInfo.LockExpiresAt() != nil {
				expiresAt := time.Unix(lockInfo.LockExpiresAt().Seconds(), 0)
				timeRemaining := time.Until(expiresAt).Round(time.Second)
				ctx.Printf("  • Lock expires in: %s\n\n", timeRemaining)
			}

			// Build contact message
			if lockInfo.StartedBy() != "-" {
				ctx.Printf("Please wait for it to complete or contact %s to coordinate.\n", lockInfo.StartedBy())
			} else {
				ctx.Printf("Please wait for it to complete.\n")
			}
		} else {
			// Fall back to plain error message
			ctx.Printf("\n❌ Deployment blocked:\n\n%s\n", createResult.Error())
		}
		return fmt.Errorf("deployment blocked by lock")
	}

	if !createResult.HasDeployment() || createResult.Deployment() == nil {
		return fmt.Errorf("deployment creation returned no deployment")
	}

	deploymentId := createResult.Deployment().Id()
	ctx.Log.Info("Created deployment record", "deployment_id", deploymentId)

	// Initialize build error/log tracking
	var buildErrors []string
	var buildLogs []string

	// Helper function to update deployment phase
	updateDeploymentPhase := func(phase string) {
		if deploymentId != "" {
			_, updateErr := depClient.UpdateDeploymentPhase(ctx, deploymentId, phase)
			if updateErr != nil {
				ctx.Log.Error("Failed to update deployment phase", "error", updateErr, "phase", phase)
			}
		}
	}

	// Helper function to update deployment status on failure
	updateDeploymentOnError := func(errMsg string) {
		if deploymentId != "" {
			// Use a separate context with timeout for status updates to ensure they complete
			// even if the main context is canceled
			statusCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Collect build logs if available
			logs := strings.Join(buildLogs, "\n")
			if logs == "" && len(buildErrors) > 0 {
				logs = strings.Join(buildErrors, "\n")
			}

			_, updateErr := depClient.UpdateFailedDeployment(statusCtx, deploymentId, errMsg, logs)
			if updateErr != nil {
				// Fallback to simple status update
				_, updateErr = depClient.UpdateDeploymentStatus(statusCtx, deploymentId, "failed", errMsg)
				if updateErr != nil {
					ctx.Log.Error("Failed to update deployment status to failed", "error", updateErr)
				}
			}
		}
	}

	// Load AppConfig to get include patterns
	var includePatterns []string
	ac, err := appconfig.LoadAppConfigUnder(dir)
	if err != nil {
		updateDeploymentOnError(fmt.Sprintf("Failed to load app config: %v", err))
		return err
	}
	if ac != nil && ac.Include != nil {
		// Validate patterns before using them
		for _, pattern := range ac.Include {
			if err := tarx.ValidatePattern(pattern); err != nil {
				updateDeploymentOnError(fmt.Sprintf("Invalid include pattern: %v", err))
				return fmt.Errorf("invalid include pattern %q: %w", pattern, err)
			}
		}
		includePatterns = ac.Include
	}

	// Update phase to building
	updateDeploymentPhase("building")

	r, err := tarx.MakeTar(dir, includePatterns)
	if err != nil {
		updateDeploymentOnError(fmt.Sprintf("Failed to create tar: %v", err))
		return err
	}

	defer r.Close()

	var (
		cb      stream.SendStream[*build_v1alpha.Status]
		results *build_v1alpha.BuilderClientBuildFromTarResults
	)

	// Detect if we have a TTY - if not, force explain mode
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	useExplainMode := opts.Explain || !isTTY

	if useExplainMode {
		// In explain mode, write to stderr
		pw, err := progresswriter.NewPrinter(ctx, os.Stderr, opts.ExplainFormat)
		if err != nil {
			return err
		}

		// Add upload progress tracking in explain mode
		uploadStartTime := time.Now()
		var uploadBytes int64
		var lastPrintTime time.Time

		progressReader := upload.NewProgressReader(r, func(progress upload.Progress) {
			uploadBytes = progress.BytesRead
			// Print progress every 500ms to avoid spamming
			if time.Since(lastPrintTime) >= 500*time.Millisecond {
				lastPrintTime = time.Now()
				fmt.Fprintf(os.Stderr, "\r\033[K") // Clear to end of line
				fmt.Fprintf(os.Stderr, "Uploading artifacts: %s at %s",
					upload.FormatBytes(progress.BytesRead),
					upload.FormatSpeed(progress.BytesPerSecond))
			}
		})
		r = progressReader

		// Progress handler for explain mode
		progressHandler := func(status *client.SolveStatus) error {
			// Clear the upload progress line when buildkit starts
			if uploadBytes > 0 {
				uploadDuration := time.Since(uploadStartTime)
				avgSpeed := float64(uploadBytes) / uploadDuration.Seconds()
				fmt.Fprintf(os.Stderr, "\rUpload complete: %s in %.1fs at %s\n",
					upload.FormatBytes(uploadBytes),
					uploadDuration.Seconds(),
					upload.FormatSpeed(avgSpeed))
				uploadBytes = 0 // Only print once
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case pw.Status() <- status:
				// ok
			}
			return nil
		}

		cb = createBuildStatusCallback(ctx, nil, nil, nil, &buildErrors, nil, progressHandler)

		results, err = bc.BuildFromTar(ctx, name, stream.ServeReader(ctx, r), cb)
		if err != nil {
			// Check if this was a context cancellation (user pressed CTRL-C)
			if ctx.Err() != nil {
				ctx.Printf("\n\n❌ Deploy cancelled.\n")
				updateDeploymentOnError("Deploy cancelled by user")
				return ctx.Err()
			}
			ctx.Printf("\n\nBuild failed with the following errors:\n")
			printBuildErrors(ctx, buildErrors, nil)
			updateDeploymentOnError(fmt.Sprintf("Build failed: %v", err))
			return err
		}

		close(pw.Status())
		<-pw.Done()

		if pw.Err() != nil {
			return pw.Err()
		}
	} else {
		var (
			updateCh         = make(chan string, 1)
			transferCh       = make(chan transferUpdate, 1)
			uploadProgressCh = make(chan upload.Progress, 1)
			transfers        = map[string]transfer{}
			wg               sync.WaitGroup
		)

		defer wg.Wait()

		progressReader := upload.NewProgressReader(r, func(progress upload.Progress) {
			select {
			case uploadProgressCh <- progress:
			default:
			}
		})
		r = progressReader

		// Create a context that can be cancelled
		deployCtx, cancelDeploy := context.WithCancel(ctx)
		defer cancelDeploy()

		model := initialModel(updateCh, transferCh, uploadProgressCh)
		p := tea.NewProgram(model)

		var finalModel tea.Model
		var runErr error

		wg.Add(1)
		go func() {
			defer wg.Done()
			finalModel, runErr = p.Run()
			if runErr == nil {
				// Check if we exited due to interrupt or actual timeout
				if dm, ok := finalModel.(*deployInfo); ok && dm.interrupted {
					cancelDeploy() // Cancel the deployment context
				}
				// Note: we don't cancel on timeout phase anymore as that's handled by the UI
			} else {
				// UI died; ensure we don't keep uploading/building
				cancelDeploy()
			}
		}()

		defer p.Quit()

		// Progress handler for interactive mode
		progressHandler := func(status *client.SolveStatus) error {
			p.Send(status)
			return nil
		}

		cb = createBuildStatusCallback(deployCtx, updateCh, transferCh, transfers, &buildErrors, &buildLogs, progressHandler)

		results, err = bc.BuildFromTar(deployCtx, name, stream.ServeReader(deployCtx, r), cb)

		// Ensure the progress UI is shut down before printing
		p.Quit()
		wg.Wait()

		// Get the final model to extract phase summaries
		if m, ok := finalModel.(*deployInfo); ok && m.currentPhase == "buildkit" && err == nil {
			// Complete the buildkit phase if it's still running and we succeeded
			duration := time.Since(m.phaseStart)
			buildPhase := phaseSummary{
				name:     "Build & push image",
				duration: duration,
				details:  fmt.Sprintf("%d layers processed", m.parts),
			}

			// Only print the final build phase summary (TEA UI already showed the others)
			ctx.Printf("%s\n", renderPhaseSummary(buildPhase))

			// Update phase to pushing (build includes push in buildkit)
			updateDeploymentPhase("pushing")
		}

		if err != nil {
			// Check if this was a user interruption (via UI flag or context cancellation)
			dm, isDeploy := finalModel.(*deployInfo)
			if (isDeploy && dm.interrupted) || ctx.Err() != nil {
				ctx.Printf("\n\n❌ Deploy cancelled.\n")
				updateDeploymentOnError("Deploy cancelled by user")
				if deployCtx.Err() != nil {
					return deployCtx.Err()
				}
				return ctx.Err()
			}

			// Check if this was a buildkit startup timeout (handled by UI)
			if isDeploy && dm.currentPhase == "timeout" {
				// The UI already printed the timeout message
				updateDeploymentOnError("Buildkit startup timeout")
				return fmt.Errorf("buildkit startup timeout")
			}

			ctx.Printf("\n\nBuild failed.\n")
			printBuildErrors(ctx, buildErrors, buildLogs)
			updateDeploymentOnError(fmt.Sprintf("Build failed: %v", err))
			return err
		}

	}

	if results.Version() == "" {
		ctx.Printf("\n\nError detected in building %s. No version returned.\n", name)
		printBuildErrors(ctx, buildErrors, buildLogs)
		updateDeploymentOnError("Build failed: no version returned")
		return fmt.Errorf("build failed: no version returned")
	}

	ctx.Log.Debug("Build completed", "version", results.Version())

	// For now, use the version string as the app version identifier
	// The build service creates app_version entities but we can't easily look them up yet
	// TODO: Implement proper app version entity lookup when entity service access is available in CLI
	appVersionId := results.Version()
	if appVersionId == "" {
		updateDeploymentOnError("Build did not return a version")
		return fmt.Errorf("build did not return a version")
	}

	ctx.Log.Debug("Build completed with version", "version", appVersionId)

	// Update phase to pushing (build completed, now pushing)
	updateDeploymentPhase("pushing")

	// Update deployment with actual app version ID
	_, err = depClient.UpdateDeploymentAppVersion(ctx, deploymentId, appVersionId)
	if err != nil {
		ctx.Log.Error("Failed to update deployment app version", "error", err)
		// Continue anyway - the deployment is proceeding
	}

	// Update phase to activating
	updateDeploymentPhase("activating")

	// Mark deployment as active
	_, err = depClient.UpdateDeploymentStatus(ctx, deploymentId, "active", "")
	if err != nil {
		// Log error but don't fail - deployment is already done
		ctx.Log.Error("Failed to update deployment status", "error", err)
	}

	ctx.Printf("\n\nUpdated version %s deployed. All traffic moved to new version.\n", results.Version())

	// Show route/access information using server-provided data
	displayAccessInfo(ctx, name, results)

	return nil
}

// Helper function to print build errors and logs
func printBuildErrors(ctx *Context, buildErrors []string, buildLogs []string) {
	if len(buildErrors) > 0 {
		ctx.Printf("\nErrors:\n")
		for _, errMsg := range buildErrors {
			ctx.Printf("  - %s\n", errMsg)
		}
	}

	if len(buildLogs) > 0 {
		ctx.Printf("\nBuild output:\n")
		for _, log := range buildLogs {
			ctx.Printf("%s\n", log)
		}
	}
}

// createBuildStatusCallback creates a callback for handling build status updates
func createBuildStatusCallback(
	ctx context.Context,
	updateCh chan<- string,
	transferCh chan<- transferUpdate,
	transfers map[string]transfer,
	buildErrors *[]string,
	buildLogs *[]string,
	progressHandler func(*client.SolveStatus) error,
) stream.SendStream[*build_v1alpha.Status] {
	return stream.Callback(func(su *build_v1alpha.Status) error {
		update := su.Update()

		switch update.Which() {
		case "buildkit":
			sj := update.Buildkit()

			var status client.SolveStatus
			if err := json.Unmarshal(sj, &status); err != nil {
				return err
			}

			// Handle transfers if we have a transfer channel
			if transferCh != nil {
				var updated bool
				for _, st := range status.Statuses {
					if st.Total != 0 {
						updated = true
						transfers[st.ID] = transfer{total: st.Total, current: st.Current}
					}
				}

				if updated {
					select {
					case <-ctx.Done():
						// UI/operation cancelled, drop the update
					case transferCh <- transferUpdate{transfers: transfers}:
						// ok
					default:
						// channel full, drop to avoid blocking
					}
				}
			}

			// Call the progress handler if provided
			if progressHandler != nil {
				if err := progressHandler(&status); err != nil {
					return err
				}
			}

			// Extract error messages from status
			for _, vertex := range status.Vertexes {
				if vertex.Error != "" {
					*buildErrors = append(*buildErrors, vertex.Error)
				}
			}

			// Collect all logs for potential output on failure
			if buildLogs != nil {
				for _, log := range status.Logs {
					if log.Data != nil {
						logStr := strings.TrimSpace(string(log.Data))
						if logStr != "" {
							*buildLogs = append(*buildLogs, logStr)
						}
					}
				}
			}

			// Fail the build if we detected any errors
			if len(*buildErrors) > 0 {
				return fmt.Errorf("build failed with %d error(s)", len(*buildErrors))
			}

			return nil
		case "message":
			msg := update.Message()
			if updateCh != nil {
				select {
				case updateCh <- msg:
					// sent successfully
				default:
					// drop if UI isn't consuming
				}
			}
		case "error":
			*buildErrors = append(*buildErrors, update.Error())
		}

		return nil
	})
}

// displayAccessInfo shows how to access the deployed app using server-provided access info
func displayAccessInfo(ctx *Context, appName string, results *build_v1alpha.BuilderClientBuildFromTarResults) {
	// Check if we have access info from the server
	if !results.HasAccessInfo() {
		ctx.Log.Debug("No access info returned from server")
		return
	}

	accessInfo := results.AccessInfo()

	// Get hostnames and default route status from server
	var hostnames []string
	if accessInfo.HasHostnames() && accessInfo.Hostnames() != nil {
		hostnames = *accessInfo.Hostnames()
	}
	hasDefaultRoute := accessInfo.DefaultRoute()

	// Get cluster address for default route display
	// Prefer the cloud-provisioned DNS hostname from the server if available
	var clusterAddr string
	if accessInfo.ClusterHostname() != "" {
		// Use the cloud-provisioned DNS hostname (e.g., cluster-abc.org-123.miren.systems)
		clusterAddr = accessInfo.ClusterHostname()
	} else if ctx.ClusterConfig != nil && ctx.ClusterConfig.Hostname != "" {
		// Fall back to the client's cluster address
		// Strip any API port (e.g. :8443) since HTTP ingress is on 443
		clusterAddr = stripPort(ctx.ClusterConfig.Hostname)
	}

	// Display access information
	if len(hostnames) > 0 {
		ctx.Printf("\nYour app is available at:\n")
		for _, host := range hostnames {
			ctx.Printf("  https://%s\n", host)
		}
		if hasDefaultRoute {
			ctx.Printf("  (also the default route)\n")
		}
	} else if hasDefaultRoute {
		if clusterAddr != "" {
			ctx.Printf("\nYour app is the default route, available at:\n")
			ctx.Printf("  https://%s\n", clusterAddr)
		} else {
			ctx.Printf("\nYour app is the default route and will receive all unmatched traffic.\n")
		}
		suggestRoute(ctx, appName, accessInfo.ClusterHostname())
	} else {
		ctx.Printf("\nNo routes configured for this app.\n")
		suggestRoute(ctx, appName, accessInfo.ClusterHostname())
		ctx.Printf("To make it the default route: miren route set-default %s\n", appName)
	}
}

// suggestRoute suggests a route command, using the cloud DNS hostname if available
func suggestRoute(ctx *Context, appName string, clusterHostname string) {
	if clusterHostname != "" {
		// Suggest a specific subdomain using the app name
		subdomain := sanitizeForSubdomain(appName)
		suggestedHost := subdomain + "." + clusterHostname
		ctx.Printf("To set a hostname, try: miren route set %s %s\n", suggestedHost, appName)
	} else {
		ctx.Printf("To set a hostname, try: miren route set <hostname> %s\n", appName)
	}
}

// sanitizeForSubdomain converts an app name to a valid subdomain label
func sanitizeForSubdomain(name string) string {
	// Convert to lowercase
	result := strings.ToLower(name)
	// Replace underscores with hyphens
	result = strings.ReplaceAll(result, "_", "-")
	// Replace any other non-alphanumeric chars with hyphens
	var sanitized strings.Builder
	for _, r := range result {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			sanitized.WriteRune(r)
		} else {
			sanitized.WriteRune('-')
		}
	}
	result = sanitized.String()
	// Remove leading/trailing hyphens
	result = strings.Trim(result, "-")
	// Collapse multiple hyphens
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	// Ensure it's not empty
	if result == "" {
		result = "app"
	}
	return result
}

// stripPort removes any port suffix from a host string
func stripPort(host string) string {
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}

// Styles for analyze output
var (
	analyzeTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("3")) // yellow

	analyzeLabelStyle = lipgloss.NewStyle().
				Faint(true).
				Width(10).
				Align(lipgloss.Right)

	analyzeValueStyle = lipgloss.NewStyle().
				Bold(true)

	// Badge styles for different event kinds
	badgeFile = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")). // blue
			Bold(true)
	badgePackage = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")). // green
			Bold(true)
	badgeFramework = lipgloss.NewStyle().
			Foreground(lipgloss.Color("13")). // magenta
			Bold(true)
	badgeConfig = lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")). // yellow
			Bold(true)
	badgeDir = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")). // cyan
			Bold(true)
	badgeScript = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")). // orange
			Bold(true)
)

func eventKindBadge(kind string) string {
	badge := fmt.Sprintf("[%s]", kind)
	switch kind {
	case "file":
		return badgeFile.Render(badge)
	case "package":
		return badgePackage.Render(badge)
	case "framework":
		return badgeFramework.Render(badge)
	case "config":
		return badgeConfig.Render(badge)
	case "dir":
		return badgeDir.Render(badge)
	case "script":
		return badgeScript.Render(badge)
	default:
		return lipgloss.NewStyle().Faint(true).Render(badge)
	}
}

// analyzeApp calls the AnalyzeApp API and displays the results
func analyzeApp(ctx *Context, bc *build_v1alpha.BuilderClient, dir string) error {
	if dir == "" || dir == "." {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	ctx.Printf("Analyzing app in %s...\n\n", dir)

	// Load AppConfig to get include patterns
	var includePatterns []string
	ac, err := appconfig.LoadAppConfigUnder(dir)
	if err != nil {
		return fmt.Errorf("failed to load app config: %w", err)
	}
	if ac != nil && ac.Include != nil {
		for _, pattern := range ac.Include {
			if err := tarx.ValidatePattern(pattern); err != nil {
				return fmt.Errorf("invalid include pattern %q: %w", pattern, err)
			}
		}
		includePatterns = ac.Include
	}

	r, err := tarx.MakeTar(dir, includePatterns)
	if err != nil {
		return fmt.Errorf("failed to create tar: %w", err)
	}

	defer r.Close()

	result, err := bc.AnalyzeApp(ctx, stream.ServeReader(ctx, r))
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	analysisResult := result.Result()
	if analysisResult == nil {
		return fmt.Errorf("no analysis result returned")
	}

	// Stack
	ctx.Printf("%s %s\n",
		analyzeLabelStyle.Render("Stack:"),
		analyzeValueStyle.Render(analysisResult.Stack()))

	// App name (if from app.toml)
	if analysisResult.AppName() != "" {
		ctx.Printf("%s %s\n",
			analyzeLabelStyle.Render("App Name:"),
			analyzeValueStyle.Render(analysisResult.AppName()))
	}

	// Working directory
	ctx.Printf("%s %s\n",
		analyzeLabelStyle.Render("Directory:"),
		analysisResult.WorkingDir())

	// Entrypoint
	if analysisResult.Entrypoint() != "" {
		ctx.Printf("%s %s\n",
			analyzeLabelStyle.Render("Entrypoint:"),
			analyzeValueStyle.Render(analysisResult.Entrypoint()))
	}

	// Dockerfile (if using dockerfile stack)
	if analysisResult.BuildDockerfile() != "" {
		ctx.Printf("%s %s\n",
			analyzeLabelStyle.Render("Dockerfile:"),
			analysisResult.BuildDockerfile())
	}

	// Services
	if analysisResult.HasServices() && analysisResult.Services() != nil {
		services := *analysisResult.Services()
		if len(services) > 0 {
			ctx.Printf("\n%s\n", analyzeTitleStyle.Render("Services"))
			for _, svc := range services {
				sourceInfo := ""
				if svc.Source() != "" {
					sourceInfo = lipgloss.NewStyle().Faint(true).Render(fmt.Sprintf(" (%s)", svc.Source()))
				}

				command := svc.Command()
				if command == "" {
					// Service uses Dockerfile CMD (image default)
					command = lipgloss.NewStyle().Faint(true).Italic(true).Render("image default")
				}

				ctx.Printf("  %s: %s%s\n",
					analyzeValueStyle.Render(svc.Name()),
					command,
					sourceInfo)
			}
		}
	}

	// Environment variables (keys only)
	if analysisResult.HasEnvVars() && analysisResult.EnvVars() != nil {
		envVars := *analysisResult.EnvVars()
		if len(envVars) > 0 {
			ctx.Printf("\n%s\n", analyzeTitleStyle.Render("Environment Variables"))
			for _, key := range envVars {
				ctx.Printf("  • %s\n", key)
			}
		}
	}

	// Detection events
	if analysisResult.HasEvents() && analysisResult.Events() != nil {
		events := *analysisResult.Events()
		if len(events) > 0 {
			ctx.Printf("\n%s\n", analyzeTitleStyle.Render("Detection"))
			for _, event := range events {
				badge := eventKindBadge(event.Kind())
				if event.Name() != "" {
					ctx.Printf("  %s %s: %s\n",
						badge,
						analyzeValueStyle.Render(event.Name()),
						event.Message())
				} else {
					ctx.Printf("  %s %s\n", badge, event.Message())
				}
			}
		}
	}

	return nil
}
