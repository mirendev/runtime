package commands

import (
	"fmt"
	"time"

	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

func Rollback(ctx *Context, opts struct {
	AppCentric
}) error {
	depCl, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return fmt.Errorf("failed to connect to deployment service: %w", err)
	}
	depClient := deployment_v1alpha.NewDeploymentClient(depCl)

	// List recent deployments
	result, err := depClient.ListDeployments(ctx, opts.App, ctx.ClusterName, "", 20)
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	deployments := result.Deployments()
	if len(deployments) == 0 {
		ctx.Printf("No deployments found for app '%s' on cluster '%s'\n", opts.App, ctx.ClusterName)
		return nil
	}

	// Sort by time (most recent first)
	sortDeployments(deployments)

	// Filter to only rollback candidates: succeeded or active, with a real version
	var candidates []*deployment_v1alpha.DeploymentInfo
	skippedFirst := false
	for _, dep := range deployments {
		status := dep.Status()
		// Skip statuses that aren't rollback candidates
		if status != "active" && status != "succeeded" {
			continue
		}
		// Skip the currently active deployment (the first active one we see)
		if status == "active" && !skippedFirst {
			skippedFirst = true
			continue
		}
		// Skip deployments without real versions
		version := dep.AppVersionId()
		if version == "" || version == "pending-build" {
			continue
		}
		candidates = append(candidates, dep)
	}

	if len(candidates) == 0 {
		ctx.Printf("No previous versions available to roll back to for app '%s'\n", opts.App)
		return nil
	}

	// Build picker items
	headers := []string{"VERSION", "STATUS", "WHEN", "GIT SHA", "BRANCH"}
	items := make([]ui.PickerItem, len(candidates))
	for i, dep := range candidates {
		gitSha, gitBranch, _ := formatGitInfo(dep)
		items[i] = ui.TablePickerItem{
			ItemID: dep.AppVersionId(),
			Columns: []string{
				ui.DisplayShortID(dep.AppVersionShortId(), dep.AppVersionId()),
				dep.Status(),
				formatDeploymentTime(dep),
				gitSha,
				gitBranch,
			},
		}
	}

	selected, err := ui.RunPicker(items,
		ui.WithTitle(fmt.Sprintf("Roll back %s — select a version:", opts.App)),
		ui.WithHeaders(headers),
	)
	if err != nil {
		return fmt.Errorf("picker error: %w", err)
	}
	if selected == nil {
		ctx.Printf("Rollback cancelled\n")
		return nil
	}

	selectedVersion := selected.ID()

	// Call DeployVersion with is_rollback=true
	deployResult, err := depClient.DeployVersion(ctx, opts.App, ctx.ClusterName, selectedVersion, true, nil, "", "")
	if err != nil {
		return fmt.Errorf("failed to roll back: %w", err)
	}

	if deployResult.HasError() && deployResult.Error() != "" {
		if deployResult.HasLockInfo() && deployResult.LockInfo() != nil {
			lockInfo := deployResult.LockInfo()
			ctx.Printf("\n❌ Rollback blocked:\n\n")
			ctx.Printf("Another deployment is already in progress for app '%s' on cluster '%s'.\n\n",
				lockInfo.AppName(), lockInfo.ClusterId())
			ctx.Printf("  • Started by: %s\n", lockInfo.StartedBy())
			if lockInfo.HasStartedAt() && lockInfo.StartedAt() != nil {
				startedAt := time.Unix(lockInfo.StartedAt().Seconds(), 0)
				ctx.Printf("  • Started at: %s (%s ago)\n",
					startedAt.Format("2006-01-02 15:04:05 MST"),
					time.Since(startedAt).Round(time.Second))
			}
			ctx.Printf("  • Current phase: %s\n", lockInfo.CurrentPhase())
		} else {
			ctx.Printf("\n❌ Rollback failed: %s\n", deployResult.Error())
		}
		return fmt.Errorf("rollback failed")
	}

	var deployed *deployment_v1alpha.DeploymentInfo
	if deployResult.HasDeployment() {
		deployed = deployResult.Deployment()
	}
	deployedVersion, versionDisplay := rollbackVersionIDs(deployed, selectedVersion)

	ctx.Printf("Rolling back %s to version %s\n", opts.App, versionDisplay)

	if err := awaitHealthy(ctx, opts.App, deployedVersion, versionDisplay); err != nil {
		return err
	}

	if deployResult.HasAccessInfo() && deployResult.AccessInfo() != nil {
		displayDeployVersionAccessInfo(ctx, opts.App, deployResult.AccessInfo())
	}

	return nil
}

// rollbackVersionIDs reports which version to wait on and which to show, given
// the deployment the server opened and the version the operator picked.
//
// The two can differ. The server activates a version *derived* from the selected
// one whenever the app's addon bindings or operator-set env vars have to be
// carried forward onto it (MIR-1579), and the deployment record then names the
// derived version. Waiting on the id we asked for would never see it go active,
// and the wait would fail with "never became active" — the same message that
// sent operators down the path that caused MIR-1579 in the first place.
//
// A nil deployment, or one that names no version, falls back to the selection so
// an older server that returns neither still behaves as before.
func rollbackVersionIDs(dep *deployment_v1alpha.DeploymentInfo, selectedVersion string) (deployed, display string) {
	deployed, display = selectedVersion, selectedVersion
	if dep == nil {
		return deployed, display
	}

	if id := dep.AppVersionId(); id != "" {
		deployed = id
		display = id
	}
	if dep.HasAppVersionShortId() && dep.AppVersionShortId() != "" {
		display = dep.AppVersionShortId()
	}
	return deployed, display
}
