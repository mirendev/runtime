package deployment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	appclient "miren.dev/runtime/api/app"
	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	ephemeralx "miren.dev/runtime/pkg/ephemeral"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
)

type DeploymentServer struct {
	Log           *slog.Logger
	EAC           *entityserver_v1alpha.EntityAccessClient
	EC            *aes.Client
	AppClient     *appclient.Client
	IngressClient *ingress.Client
	DNSHostname   string
}

var _ deployment_v1alpha.Deployment = (*DeploymentServer)(nil)

const deploymentLockTimeout = 30 * time.Minute

func NewDeploymentServer(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, ec *aes.Client, appClient *appclient.Client, dnsHostname string) (*DeploymentServer, error) {
	return &DeploymentServer{
		Log:           log.With("module", "deployment"),
		EAC:           eac,
		EC:            ec,
		AppClient:     appClient,
		IngressClient: ingress.NewClient(log, eac),
		DNSHostname:   dnsHostname,
	}, nil
}

func (d *DeploymentServer) CreateDeployment(ctx context.Context, req *deployment_v1alpha.DeploymentCreateDeployment) error {
	args := req.Args()
	results := req.Results()

	// Validate required fields
	if !args.HasAppName() || args.AppName() == "" {
		return cond.ValidationFailure("missing-field", "app_name is required")
	}
	if !args.HasClusterId() || args.ClusterId() == "" {
		return cond.ValidationFailure("missing-field", "cluster_id is required")
	}
	if !args.HasAppVersionId() || args.AppVersionId() == "" {
		return cond.ValidationFailure("missing-field", "app_version_id is required")
	}

	appName := args.AppName()
	clusterId := args.ClusterId()
	appVersionId := args.AppVersionId()

	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}

	// Check for existing in_progress deployments for this app
	existingDeployments, err := d.listDeploymentsInternal(ctx, appName, "in_progress", 1)
	if err != nil {
		d.Log.Error("Failed to check for existing deployments", "error", err)
		return cond.Error("failed to check deployment lock")
	}

	if len(existingDeployments) > 0 {
		// Found an existing in_progress deployment
		existing := existingDeployments[0].deployment
		existingEnt := existingDeployments[0].entity

		// Parse the deployment timestamp
		// Treat malformed/empty timestamps as expired to avoid blocking deployments
		deploymentTime, err := time.Parse(time.RFC3339, existing.DeployedBy.Timestamp)
		if err != nil {
			d.Log.Warn("Deployment has malformed timestamp, treating as expired",
				"deployment_id", string(existing.ID),
				"timestamp", existing.DeployedBy.Timestamp,
				"error", err)
			// Fall through to mark as failed below
			deploymentTime = time.Time{} // Zero time, will be treated as expired
		}

		// Check if the existing deployment is expired (older than deploymentLockTimeout)
		isExpired := deploymentTime.IsZero() || time.Since(deploymentTime) >= deploymentLockTimeout
		if !isExpired {
			// Deployment is still within the lock timeout - return structured lock info
			lockExpiresAt := deploymentTime.Add(deploymentLockTimeout)

			// Format user email for display
			displayEmail := existing.DeployedBy.UserEmail
			if displayEmail == "" || displayEmail == "unknown@example.com" || displayEmail == "user@example.com" {
				displayEmail = "-"
			}

			// Create structured lock info
			lockInfo := &deployment_v1alpha.DeploymentLockInfo{}
			lockInfo.SetAppName(appName)
			lockInfo.SetClusterId(clusterId)
			lockInfo.SetBlockingDeploymentId(string(existing.ID))
			lockInfo.SetBlockingDeploymentShortId(shortIDFromRPCEntity(existingEnt))
			lockInfo.SetStartedBy(displayEmail)
			lockInfo.SetStartedAt(standard.ToTimestamp(deploymentTime))
			lockInfo.SetCurrentPhase(existing.Phase)
			lockInfo.SetLockExpiresAt(standard.ToTimestamp(lockExpiresAt))

			results.SetLockInfo(lockInfo)

			// Also set a simple error message for backwards compatibility
			results.SetError("deployment blocked by existing in-progress deployment")
			return nil
		}

		// Existing deployment is expired, mark it as failed
		d.Log.Warn("Found expired in_progress deployment, marking as failed",
			"deployment_id", string(existing.ID),
			"age", time.Since(deploymentTime))

		// Update the expired deployment to failed status
		// We need to call the internal method since we're in the server, not using the client
		existing.Status = "failed"
		existing.ErrorMessage = fmt.Sprintf("Deployment timed out after %v", deploymentLockTimeout)
		existing.CompletedAt = time.Now().Format(time.RFC3339)

		// Update entity
		updateAttrs := existing.Encode()
		updateEntity := &entityserver_v1alpha.Entity{}
		updateEntity.SetId(string(existing.ID))
		updateEntity.SetAttrs(updateAttrs)

		// Get the current revision for the update
		// Note: If another process updates this entity between our Get and Put,
		// the Put will fail with a revision conflict. This is acceptable because
		// it means another process already handled this expired deployment.
		// We continue creating the new deployment either way.
		if existingEntity, err := d.EAC.Get(ctx, string(existing.ID)); err == nil {
			updateEntity.SetRevision(existingEntity.Entity().Revision())
			if _, err := d.EAC.Put(ctx, updateEntity); err != nil {
				d.Log.Error("Failed to mark expired deployment as failed", "error", err)
				// Continue anyway - we'll create the new deployment
			}
		} else {
			d.Log.Error("Failed to get expired deployment for update", "error", err)
			// Continue anyway - we'll create the new deployment
		}
	}

	// Get user info from context (will be implemented with auth integration)
	// For now, leave empty - the CLI display will show "-" for unknown users
	userId := ""
	userName := ""
	userEmail := ""

	// Create deployment entity
	now := time.Now()

	deployment := &core_v1alpha.Deployment{
		AppName:    appName,
		AppVersion: appVersionId,
		ClusterId:  clusterId,
		Status:     "in_progress",
		Phase:      "preparing",
		DeployedBy: core_v1alpha.DeployedBy{
			UserId:    userId,
			UserName:  userName,
			UserEmail: userEmail,
			Timestamp: now.Format(time.RFC3339),
		},
	}

	// Add git info if provided
	if args.HasGitInfo() && args.GitInfo() != nil {
		gitInfo := args.GitInfo()
		deployment.GitInfo = core_v1alpha.GitInfo{
			Sha:               gitInfo.Sha(),
			Branch:            gitInfo.Branch(),
			Message:           gitInfo.CommitMessage(),
			Author:            gitInfo.CommitAuthorName(),
			IsDirty:           gitInfo.IsDirty(),
			WorkingTreeHash:   gitInfo.WorkingTreeHash(),
			CommitAuthorEmail: gitInfo.CommitAuthorEmail(),
			Repository:        gitInfo.Repository(),
		}

		// Handle optional timestamp
		if gitInfo.HasCommitTimestamp() && gitInfo.CommitTimestamp() != nil {
			deployment.GitInfo.CommitTimestamp = standard.FromTimestamp(gitInfo.CommitTimestamp()).Format(time.RFC3339)
		}
	}

	// Create entity
	attrs := deployment.Encode()
	rpcEntity := &entityserver_v1alpha.Entity{}
	rpcEntity.SetAttrs(attrs)

	putResp, err := d.EAC.Put(ctx, rpcEntity)
	if err != nil {
		d.Log.Error("Failed to create deployment entity", "error", err)
		return cond.Error("failed to create deployment")
	}

	// Set the deployment ID from the entity server response
	deployment.ID = entity.Id(putResp.Id())

	// Convert to RPC response
	deploymentInfo := d.toDeploymentInfo(deployment)
	if depEnt, err := d.EAC.Get(ctx, putResp.Id()); err == nil {
		versionShortIDs := d.resolveShortIDs(ctx, []string{deployment.AppVersion})
		enrichDeploymentShortIDs(deploymentInfo, depEnt.Entity(), versionShortIDs)
	}
	results.SetDeployment(deploymentInfo)

	d.Log.Info("Created deployment",
		"deployment_id", putResp.Id(),
		"app", appName,
		"cluster", clusterId,
		"version", appVersionId,
		"user", userEmail)

	return nil
}

func (d *DeploymentServer) UpdateDeploymentStatus(ctx context.Context, req *deployment_v1alpha.DeploymentUpdateDeploymentStatus) error {
	args := req.Args()
	results := req.Results()

	// Validate required fields
	if !args.HasDeploymentId() || args.DeploymentId() == "" {
		return cond.ValidationFailure("missing-field", "deployment_id is required")
	}
	if !args.HasStatus() || args.Status() == "" {
		return cond.ValidationFailure("missing-field", "status is required")
	}

	deploymentId := args.DeploymentId()
	newStatus := args.Status()

	// Validate status value
	validStatuses := map[string]bool{
		"in_progress": true,
		"active":      true,
		"succeeded":   true,
		"failed":      true,
		"rolled_back": true,
		"cancelled":   true,
	}
	if !validStatuses[newStatus] {
		return cond.ValidationFailure("invalid-status",
			"status must be one of: in_progress, active, succeeded, failed, rolled_back")
	}

	// Get existing deployment
	deploymentResp, err := d.EAC.Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}

	// Decode to Deployment struct
	var deployment core_v1alpha.Deployment
	decodeEntity(deploymentResp.Entity(), &deployment)

	if !rpc.AllowApp(ctx, deployment.AppName) {
		return rpc.AppAccessError(ctx, deployment.AppName)
	}

	// Check if deployment is in a state that can be updated
	if deployment.Status != "in_progress" {
		return cond.ValidationFailure("invalid-state",
			fmt.Sprintf("cannot update deployment in %s state", deployment.Status))
	}

	// Update deployment status
	deployment.Status = newStatus

	// Only set CompletedAt if moving to a terminal state
	if newStatus != "in_progress" {
		deployment.CompletedAt = time.Now().Format(time.RFC3339)
	}

	// Update error message if failed
	if newStatus == "failed" && args.HasErrorMessage() {
		deployment.ErrorMessage = args.ErrorMessage()
	}

	// If marking as active, mark all other active deployments for this app/cluster as succeeded
	if newStatus == "active" {
		err = d.markPreviousActiveAs(ctx, deployment.AppName, deployment.ClusterId, deploymentId, "succeeded")
		if err != nil {
			d.Log.Error("Failed to mark previous active deployments as succeeded", "error", err)
			// Don't fail the whole operation, just log the error
		}
	}

	// Update entity
	updateAttrs := deployment.Encode()
	updateEntity := &entityserver_v1alpha.Entity{}
	updateEntity.SetId(deploymentId)
	updateEntity.SetAttrs(updateAttrs)
	updateEntity.SetRevision(deploymentResp.Entity().Revision())

	_, err = d.EAC.Put(ctx, updateEntity)
	if err != nil {
		d.Log.Error("Failed to update deployment entity", "error", err)
		return cond.Error("failed to update deployment")
	}

	// Convert to RPC response
	deploymentInfo := d.toDeploymentInfo(&deployment)
	results.SetDeployment(deploymentInfo)

	d.Log.Info("Updated deployment status",
		"deployment_id", deploymentId,
		"old_status", "in_progress",
		"new_status", newStatus)

	return nil
}

func (d *DeploymentServer) UpdateDeploymentPhase(ctx context.Context, req *deployment_v1alpha.DeploymentUpdateDeploymentPhase) error {
	args := req.Args()
	results := req.Results()

	// Validate required fields
	if !args.HasDeploymentId() || args.DeploymentId() == "" {
		return cond.ValidationFailure("missing-field", "deployment_id is required")
	}
	if !args.HasPhase() || args.Phase() == "" {
		return cond.ValidationFailure("missing-field", "phase is required")
	}

	deploymentId := args.DeploymentId()
	newPhase := args.Phase()

	// Validate phase value
	validPhases := map[string]bool{
		"preparing":  true,
		"building":   true,
		"pushing":    true,
		"activating": true,
	}
	if !validPhases[newPhase] {
		return cond.ValidationFailure("invalid-phase",
			"phase must be one of: preparing, building, pushing, activating")
	}

	// Get existing deployment
	deploymentResp, err := d.EAC.Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}

	// Decode to Deployment struct
	var deployment core_v1alpha.Deployment
	decodeEntity(deploymentResp.Entity(), &deployment)

	if !rpc.AllowApp(ctx, deployment.AppName) {
		return rpc.AppAccessError(ctx, deployment.AppName)
	}

	// Check if deployment is in a state that can be updated
	if deployment.Status != "in_progress" {
		return cond.ValidationFailure("invalid-state",
			fmt.Sprintf("cannot update phase for deployment in %s state", deployment.Status))
	}

	// Update deployment phase
	deployment.Phase = newPhase

	// Update entity
	updateAttrs := deployment.Encode()
	updateEntity := &entityserver_v1alpha.Entity{}
	updateEntity.SetId(deploymentId)
	updateEntity.SetAttrs(updateAttrs)
	updateEntity.SetRevision(deploymentResp.Entity().Revision())

	_, err = d.EAC.Put(ctx, updateEntity)
	if err != nil {
		d.Log.Error("Failed to update deployment entity", "error", err)
		return cond.Error("failed to update deployment")
	}

	// Convert to RPC response
	deploymentInfo := d.toDeploymentInfo(&deployment)
	results.SetDeployment(deploymentInfo)

	d.Log.Info("Updated deployment phase",
		"deployment_id", deploymentId,
		"phase", newPhase)

	return nil
}

func (d *DeploymentServer) UpdateFailedDeployment(ctx context.Context, req *deployment_v1alpha.DeploymentUpdateFailedDeployment) error {
	args := req.Args()
	results := req.Results()

	// Validate required fields
	if !args.HasDeploymentId() || args.DeploymentId() == "" {
		return cond.ValidationFailure("missing-field", "deployment_id is required")
	}

	deploymentId := args.DeploymentId()
	errorMessage := ""
	buildLogs := ""

	if args.HasErrorMessage() {
		errorMessage = args.ErrorMessage()
	}
	if args.HasBuildLogs() {
		buildLogs = args.BuildLogs()
	}

	// Get existing deployment
	deploymentResp, err := d.EAC.Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}

	// Decode to Deployment struct
	var deployment core_v1alpha.Deployment
	decodeEntity(deploymentResp.Entity(), &deployment)

	if !rpc.AllowApp(ctx, deployment.AppName) {
		return rpc.AppAccessError(ctx, deployment.AppName)
	}

	// Update deployment with failure information
	// Don't overwrite cancelled status
	if deployment.Status != "cancelled" {
		deployment.Status = "failed"
	}
	deployment.ErrorMessage = errorMessage
	deployment.BuildLogs = buildLogs
	deployment.CompletedAt = time.Now().Format(time.RFC3339)

	// Update app version to failed pattern if it's still pending
	if string(deployment.AppVersion) == "pending-build" {
		deployment.AppVersion = fmt.Sprintf("failed-%s", deploymentId)
	}

	// Update entity
	updateAttrs := deployment.Encode()
	updateEntity := &entityserver_v1alpha.Entity{}
	updateEntity.SetId(deploymentId)
	updateEntity.SetAttrs(updateAttrs)
	updateEntity.SetRevision(deploymentResp.Entity().Revision())

	_, err = d.EAC.Put(ctx, updateEntity)
	if err != nil {
		d.Log.Error("Failed to update deployment entity", "error", err)
		return cond.Error("failed to update deployment")
	}

	// Convert to RPC response
	deploymentInfo := d.toDeploymentInfo(&deployment)
	results.SetDeployment(deploymentInfo)

	d.Log.Info("Updated failed deployment",
		"deployment_id", deploymentId,
		"app_version", string(deployment.AppVersion))

	return nil
}

func (d *DeploymentServer) ListDeployments(ctx context.Context, req *deployment_v1alpha.DeploymentListDeployments) error {
	args := req.Args()
	results := req.Results()

	// Extract filters. cluster_id is accepted for wire compatibility but no
	// longer used to filter: this cluster's store only holds its own
	// deployments (see listDeploymentsInternal / MIR-1465).
	var appName, status string
	var limit int32 = 100 // default limit

	if args.HasAppName() {
		appName = args.AppName()
	}
	if args.HasStatus() {
		status = args.Status()
	}
	if args.HasLimit() && args.Limit() > 0 {
		limit = args.Limit()
	}

	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}

	deployments, err := d.listDeploymentsInternal(ctx, appName, status, int(limit))
	if err != nil {
		return err
	}

	// Batch-resolve app version short IDs
	versionIDs := make([]string, 0, len(deployments))
	for _, dwe := range deployments {
		versionIDs = append(versionIDs, dwe.deployment.AppVersion)
	}
	versionShortIDs := d.resolveShortIDs(ctx, versionIDs)

	// Convert to deployment info list
	deploymentInfos := make([]*deployment_v1alpha.DeploymentInfo, 0, len(deployments))
	for _, dwe := range deployments {
		info := d.toDeploymentInfo(dwe.deployment)
		enrichDeploymentShortIDs(info, dwe.entity, versionShortIDs)
		deploymentInfos = append(deploymentInfos, info)
	}

	results.SetDeployments(deploymentInfos)
	return nil
}

func (d *DeploymentServer) GetDeploymentById(ctx context.Context, req *deployment_v1alpha.DeploymentGetDeploymentById) error {
	args := req.Args()
	results := req.Results()

	// Validate required fields
	if !args.HasDeploymentId() || args.DeploymentId() == "" {
		return cond.ValidationFailure("missing-field", "deployment_id is required")
	}

	deploymentId := args.DeploymentId()

	// Get deployment
	deploymentResp, err := d.EAC.Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}

	// Decode to Deployment struct
	var deployment core_v1alpha.Deployment
	decodeEntity(deploymentResp.Entity(), &deployment)

	if !rpc.AllowApp(ctx, deployment.AppName) {
		return rpc.AppAccessError(ctx, deployment.AppName)
	}

	deploymentInfo := d.toDeploymentInfo(&deployment)
	versionShortIDs := d.resolveShortIDs(ctx, []string{deployment.AppVersion})
	enrichDeploymentShortIDs(deploymentInfo, deploymentResp.Entity(), versionShortIDs)
	results.SetDeployment(deploymentInfo)

	return nil
}

func (d *DeploymentServer) UpdateDeploymentAppVersion(ctx context.Context, req *deployment_v1alpha.DeploymentUpdateDeploymentAppVersion) error {
	args := req.Args()
	results := req.Results()

	// Validate required fields
	if !args.HasDeploymentId() || args.DeploymentId() == "" {
		return cond.ValidationFailure("missing-field", "deployment_id is required")
	}
	if !args.HasAppVersionId() || args.AppVersionId() == "" {
		return cond.ValidationFailure("missing-field", "app_version_id is required")
	}

	deploymentId := args.DeploymentId()
	appVersionId := args.AppVersionId()

	// Get existing deployment
	deploymentResp, err := d.EAC.Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}

	// Decode to Deployment struct
	var deployment core_v1alpha.Deployment
	decodeEntity(deploymentResp.Entity(), &deployment)

	if !rpc.AllowApp(ctx, deployment.AppName) {
		return rpc.AppAccessError(ctx, deployment.AppName)
	}

	// Update app version
	deployment.AppVersion = appVersionId

	// Update entity
	updateAttrs := deployment.Encode()
	updateEntity := &entityserver_v1alpha.Entity{}
	updateEntity.SetId(deploymentId)
	updateEntity.SetAttrs(updateAttrs)
	updateEntity.SetRevision(deploymentResp.Entity().Revision())

	_, err = d.EAC.Put(ctx, updateEntity)
	if err != nil {
		d.Log.Error("Failed to update deployment entity", "error", err)
		return cond.Error("failed to update deployment")
	}

	// Convert to RPC response
	deploymentInfo := d.toDeploymentInfo(&deployment)
	results.SetDeployment(deploymentInfo)

	d.Log.Info("Updated deployment app version",
		"deployment_id", deploymentId,
		"app_version", appVersionId)

	return nil
}

func (d *DeploymentServer) GetActiveDeployment(ctx context.Context, req *deployment_v1alpha.DeploymentGetActiveDeployment) error {
	args := req.Args()
	results := req.Results()

	// Validate required fields
	if !args.HasAppName() || args.AppName() == "" {
		return cond.ValidationFailure("missing-field", "app_name is required")
	}
	if !args.HasClusterId() || args.ClusterId() == "" {
		return cond.ValidationFailure("missing-field", "cluster_id is required")
	}

	appName := args.AppName()
	clusterId := args.ClusterId()

	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}

	// Find active deployment
	deployments, err := d.listDeploymentsInternal(ctx, appName, "active", 1)
	if err != nil {
		return err
	}

	if len(deployments) == 0 {
		return cond.NotFound("active-deployment", fmt.Sprintf("%s/%s", appName, clusterId))
	}

	dwe := deployments[0]
	deploymentInfo := d.toDeploymentInfo(dwe.deployment)
	versionShortIDs := d.resolveShortIDs(ctx, []string{dwe.deployment.AppVersion})
	enrichDeploymentShortIDs(deploymentInfo, dwe.entity, versionShortIDs)
	results.SetDeployment(deploymentInfo)

	return nil
}

func (d *DeploymentServer) CancelDeployment(ctx context.Context, req *deployment_v1alpha.DeploymentCancelDeployment) error {
	args := req.Args()
	results := req.Results()

	// Validate required fields
	if !args.HasDeploymentId() || args.DeploymentId() == "" {
		results.SetError("deployment_id is required")
		return nil
	}

	deploymentId := args.DeploymentId()

	// Get the deployment by ID (resolves short IDs via the entity server)
	deploymentResp, err := d.EAC.Get(ctx, deploymentId)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			results.SetError("deployment not found")
		} else {
			d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
			results.SetError("failed to get deployment")
		}
		return nil
	}

	// Use the resolved entity ID for all subsequent operations
	deploymentId = deploymentResp.Entity().Id()

	// Decode to Deployment struct
	var deployment core_v1alpha.Deployment
	decodeEntity(deploymentResp.Entity(), &deployment)

	// Enforce app scoping: scoped callers (e.g. OIDC) can only cancel deployments for their bound app
	if !rpc.AllowApp(ctx, deployment.AppName) {
		return rpc.AppAccessError(ctx, deployment.AppName)
	}

	// Verify deployment is in_progress
	if deployment.Status != "in_progress" {
		results.SetError(fmt.Sprintf("deployment is not in progress (status: %s)", deployment.Status))
		return nil
	}

	// Mark as cancelled
	deployment.Status = "cancelled"
	deployment.ErrorMessage = "Deployment cancelled by user"
	deployment.CompletedAt = time.Now().Format(time.RFC3339)

	// Update entity
	updateAttrs := deployment.Encode()
	updateEntity := &entityserver_v1alpha.Entity{}
	updateEntity.SetId(deploymentId)
	updateEntity.SetAttrs(updateAttrs)
	updateEntity.SetRevision(deploymentResp.Entity().Revision())

	_, err = d.EAC.Put(ctx, updateEntity)
	if err != nil {
		d.Log.Error("Failed to cancel deployment", "deployment_id", deploymentId, "error", err)
		results.SetError("failed to cancel deployment")
		return nil
	}

	results.SetSuccess(true)

	d.Log.Info("Cancelled deployment",
		"deployment_id", deploymentId,
		"app", deployment.AppName)

	return nil
}

func (d *DeploymentServer) DeployVersion(ctx context.Context, req *deployment_v1alpha.DeploymentDeployVersion) error {
	args := req.Args()
	results := req.Results()

	// Validate required fields
	if !args.HasAppName() || args.AppName() == "" {
		return cond.ValidationFailure("missing-field", "app_name is required")
	}
	if !args.HasClusterId() || args.ClusterId() == "" {
		return cond.ValidationFailure("missing-field", "cluster_id is required")
	}
	if !args.HasAppVersionId() || args.AppVersionId() == "" {
		return cond.ValidationFailure("missing-field", "app_version_id is required")
	}

	appName := args.AppName()
	clusterId := args.ClusterId()
	appVersionId := args.AppVersionId()
	sourceVersionId := appVersionId
	isRollback := args.HasIsRollback() && args.IsRollback()

	// Enforce app scoping: scoped callers (e.g. OIDC) can only deploy their bound app
	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}

	// Verify the AppVersion entity exists
	var appVersion core_v1alpha.AppVersion
	if err := d.EC.Get(ctx, appVersionId, &appVersion); err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			results.SetError(fmt.Sprintf("app version %q not found", appVersionId))
			return nil
		}
		d.Log.Error("Failed to look up app version", "app_version_id", appVersionId, "error", err)
		results.SetError("failed to look up app version")
		return nil
	}

	// If env vars are provided, create a derived version with merged variables
	if args.HasEnvVars() && len(args.EnvVars()) > 0 {
		derivedVersion, err := d.createDerivedVersion(ctx, &appVersion, args.EnvVars())
		if err != nil {
			d.Log.Error("Failed to create derived version with env vars", "error", err)
			results.SetError(fmt.Sprintf("failed to apply env vars: %v", err))
			return nil
		}
		appVersion = *derivedVersion
		appVersionId = derivedVersion.Version
		d.Log.Info("Created derived version with env vars",
			"original", args.AppVersionId(), "derived", appVersionId,
			"env_var_count", len(args.EnvVars()))
	}

	isEphemeral := args.HasEphemeralLabel() && args.EphemeralLabel() != ""

	if isEphemeral {
		// Ephemeral deploy: update version with ephemeral fields, skip activation.
		// No deployment lock check or deployment record — ephemeral deploys are
		// independent of the normal deployment lifecycle.
		ephLabel := args.EphemeralLabel()
		if err := ephemeralx.ValidateLabel(ephLabel); err != nil {
			results.SetError(fmt.Sprintf("invalid ephemeral label: %v", err))
			return nil
		}
		ephTTL := "24h"
		if args.HasEphemeralTtl() && args.EphemeralTtl() != "" {
			ephTTL = args.EphemeralTtl()
		}
		ttlDuration, parseErr := time.ParseDuration(ephTTL)
		if parseErr != nil {
			results.SetError(fmt.Sprintf("invalid ephemeral TTL %q: %v", ephTTL, parseErr))
			return nil
		}
		if ttlDuration <= 0 {
			results.SetError(fmt.Sprintf("invalid ephemeral TTL %q: must be greater than 0", ephTTL))
			return nil
		}

		// Replace existing ephemeral version with same label
		appEntity, appErr := d.AppClient.GetByName(ctx, appName)
		if appErr != nil {
			results.SetError(fmt.Sprintf("failed to get app: %v", appErr))
			return nil
		}

		if err := ephemeralx.ReplaceExisting(ctx, d.EAC, appEntity.ID, ephLabel, d.Log); err != nil {
			results.SetError(fmt.Sprintf("failed to replace existing ephemeral version %q: %v", ephLabel, err))
			return nil
		}
		if err := ephemeralx.EnforceLimit(ctx, d.EAC, appEntity.ID, ephemeralx.DefaultMaxEphemeral, d.Log); err != nil {
			results.SetError(fmt.Sprintf("failed to enforce ephemeral limit: %v", err))
			return nil
		}

		// Clone the source AppVersion into a new entity with ephemeral fields.
		// The original must remain unchanged — it may be the active version.
		ephVersion := appVersion // shallow copy
		ephVersion.ID = ""
		ephVersion.EphemeralLabel = ephLabel
		ephVersion.EphemeralTtl = ephTTL
		ephVersion.EphemeralExpiresAt = time.Now().Add(ttlDuration)

		ephName := appName + "-eph-" + idgen.Gen("v")
		ephVersion.Version = ephName

		ephID, createErr := d.EC.Create(ctx, ephName, &ephVersion)
		if createErr != nil {
			d.Log.Error("Failed to create ephemeral version entity", "error", createErr)
			results.SetError(fmt.Sprintf("failed to create ephemeral version: %v", createErr))
			return nil
		}

		d.Log.Info("Created ephemeral version",
			"app", appName, "version", ephName, "source_version", appVersionId,
			"label", ephLabel, "ttl", ephTTL, "expires_at", ephVersion.EphemeralExpiresAt)

		deploymentInfo := d.toDeploymentInfo(&core_v1alpha.Deployment{
			AppName:    appName,
			AppVersion: string(ephID),
			ClusterId:  clusterId,
			Status:     "active",
		})
		results.SetDeployment(deploymentInfo)

		accessInfo := d.getAccessInfo(ctx, appName, ephLabel)
		results.SetAccessInfo(&accessInfo)

		return nil
	}

	// --- Normal (non-ephemeral) deploy path below ---

	// Check for existing in_progress deployments (deployment lock)
	existingDeployments, err := d.listDeploymentsInternal(ctx, appName, "in_progress", 1)
	if err != nil {
		d.Log.Error("Failed to check for existing deployments", "error", err)
		results.SetError("failed to check deployment lock")
		return nil
	}

	if len(existingDeployments) > 0 {
		existing := existingDeployments[0].deployment
		existingEnt := existingDeployments[0].entity

		deploymentTime, parseErr := time.Parse(time.RFC3339, existing.DeployedBy.Timestamp)
		if parseErr != nil {
			deploymentTime = time.Time{}
		}

		isExpired := deploymentTime.IsZero() || time.Since(deploymentTime) >= deploymentLockTimeout
		if !isExpired {
			lockExpiresAt := deploymentTime.Add(deploymentLockTimeout)

			displayEmail := existing.DeployedBy.UserEmail
			if displayEmail == "" || displayEmail == "unknown@example.com" || displayEmail == "user@example.com" {
				displayEmail = "-"
			}

			lockInfo := &deployment_v1alpha.DeploymentLockInfo{}
			lockInfo.SetAppName(appName)
			lockInfo.SetClusterId(clusterId)
			lockInfo.SetBlockingDeploymentId(string(existing.ID))
			lockInfo.SetBlockingDeploymentShortId(shortIDFromRPCEntity(existingEnt))
			lockInfo.SetStartedBy(displayEmail)
			lockInfo.SetStartedAt(standard.ToTimestamp(deploymentTime))
			lockInfo.SetCurrentPhase(existing.Phase)
			lockInfo.SetLockExpiresAt(standard.ToTimestamp(lockExpiresAt))

			results.SetLockInfo(lockInfo)
			results.SetError("deployment blocked by existing in-progress deployment")
			return nil
		}

		// Expired lock — mark as failed and continue
		d.Log.Warn("Found expired in_progress deployment, marking as failed",
			"deployment_id", string(existing.ID),
			"age", time.Since(deploymentTime))

		existing.Status = "failed"
		existing.ErrorMessage = fmt.Sprintf("Deployment timed out after %v", deploymentLockTimeout)
		existing.CompletedAt = time.Now().Format(time.RFC3339)

		updateAttrs := existing.Encode()
		lockUpdateEntity := &entityserver_v1alpha.Entity{}
		lockUpdateEntity.SetId(string(existing.ID))
		lockUpdateEntity.SetAttrs(updateAttrs)

		if existingEntity, getErr := d.EAC.Get(ctx, string(existing.ID)); getErr == nil {
			lockUpdateEntity.SetRevision(existingEntity.Entity().Revision())
			if _, putErr := d.EAC.Put(ctx, lockUpdateEntity); putErr != nil {
				d.Log.Error("Failed to mark expired deployment as failed", "error", putErr)
			}
		}
	}

	// Find the source deployment — the most recent deployment with this app_version_id
	allDeployments, err := d.listDeploymentsInternal(ctx, appName, "", 100)
	if err != nil {
		d.Log.Error("Failed to list deployments for source lookup", "error", err)
		// Continue without source info
	}

	var sourceDeployment *core_v1alpha.Deployment
	for _, dwe := range allDeployments {
		if dwe.deployment.AppVersion == sourceVersionId {
			sourceDeployment = dwe.deployment
			break // listDeploymentsInternal returns newest first
		}
	}

	// Create new deployment entity
	now := time.Now()

	deployment := &core_v1alpha.Deployment{
		AppName:    appName,
		AppVersion: appVersionId,
		ClusterId:  clusterId,
		Status:     "in_progress",
		Phase:      "activating",
		DeployedBy: core_v1alpha.DeployedBy{
			Timestamp: now.Format(time.RFC3339),
		},
	}

	// Copy git info and source ID from the source deployment
	if sourceDeployment != nil {
		deployment.GitInfo = sourceDeployment.GitInfo
		deployment.SourceDeploymentId = string(sourceDeployment.ID)
	}

	// Create entity
	attrs := deployment.Encode()
	rpcEntity := &entityserver_v1alpha.Entity{}
	rpcEntity.SetAttrs(attrs)

	putResp, err := d.EAC.Put(ctx, rpcEntity)
	if err != nil {
		d.Log.Error("Failed to create deployment entity", "error", err)
		results.SetError("failed to create deployment")
		return nil
	}

	deployment.ID = entity.Id(putResp.Id())
	newDeploymentId := putResp.Id()

	{
		// Normal deploy: activate the version
		if err := d.AppClient.SetActiveVersion(ctx, appName, string(appVersion.ID)); err != nil {
			d.Log.Error("Failed to set active version", "error", err, "app", appName, "version_id", string(appVersion.ID))

			deployment.Status = "failed"
			deployment.ErrorMessage = fmt.Sprintf("failed to activate version: %v", err)
			deployment.CompletedAt = time.Now().Format(time.RFC3339)
			if current, getErr := d.EAC.Get(ctx, newDeploymentId); getErr == nil {
				failEntity := &entityserver_v1alpha.Entity{}
				failEntity.SetId(newDeploymentId)
				failEntity.SetAttrs(deployment.Encode())
				failEntity.SetRevision(current.Entity().Revision())
				if _, putErr := d.EAC.Put(ctx, failEntity); putErr != nil {
					d.Log.Error("Failed to mark deployment as failed", "error", putErr)
				}
			}

			results.SetError(fmt.Sprintf("failed to activate version: %v", err))
			return nil
		}

		// Activation succeeded — mark deployment active
		deployment.Status = "active"
		deployment.CompletedAt = time.Now().Format(time.RFC3339)
		if current, getErr := d.EAC.Get(ctx, newDeploymentId); getErr == nil {
			activeEntity := &entityserver_v1alpha.Entity{}
			activeEntity.SetId(newDeploymentId)
			activeEntity.SetAttrs(deployment.Encode())
			activeEntity.SetRevision(current.Entity().Revision())
			if _, putErr := d.EAC.Put(ctx, activeEntity); putErr != nil {
				d.Log.Error("Failed to update deployment status to active", "error", putErr)
			}
		}

		// Mark previous active deployments
		targetStatus := "succeeded"
		if isRollback {
			targetStatus = "rolled_back"
		}
		if err := d.markPreviousActiveAs(ctx, appName, clusterId, newDeploymentId, targetStatus); err != nil {
			d.Log.Error("Failed to mark previous active deployments", "error", err)
			// Don't fail — the new deployment is already created and active
		}
	}

	deploymentInfo := d.toDeploymentInfo(deployment)
	if depEnt, getErr := d.EAC.Get(ctx, newDeploymentId); getErr == nil {
		versionShortIDs := d.resolveShortIDs(ctx, []string{deployment.AppVersion})
		enrichDeploymentShortIDs(deploymentInfo, depEnt.Entity(), versionShortIDs)
	}
	results.SetDeployment(deploymentInfo)

	accessInfo := d.getAccessInfo(ctx, appName, "")
	results.SetAccessInfo(&accessInfo)

	d.Log.Info("Deployed version",
		"deployment_id", newDeploymentId,
		"app", appName,
		"cluster", clusterId,
		"version", appVersionId,
		"is_rollback", isRollback)

	return nil
}

func (d *DeploymentServer) SetEnvVars(ctx context.Context, req *deployment_v1alpha.DeploymentSetEnvVars) error {
	args := req.Args()
	results := req.Results()

	if !args.HasAppName() || args.AppName() == "" {
		return cond.ValidationFailure("missing-field", "app_name is required")
	}
	if !args.HasClusterId() || args.ClusterId() == "" {
		return cond.ValidationFailure("missing-field", "cluster_id is required")
	}
	if !args.HasVars() || len(args.Vars()) == 0 {
		return cond.ValidationFailure("missing-field", "vars is required")
	}

	appName := args.AppName()
	clusterId := args.ClusterId()
	service := ""
	if args.HasService() {
		service = args.Service()
	}

	// Convert RPC vars to shared helper input
	vars := make([]appclient.EnvVarInput, len(args.Vars()))
	for i, v := range args.Vars() {
		vars[i] = appclient.EnvVarInput{Key: v.Key(), Value: v.Value(), Sensitive: v.Sensitive()}
	}

	// Call shared helper to create new version
	mutResult, err := appclient.SetEnvVars(ctx, d.EC, appName, nil, vars, service)
	if err != nil {
		d.Log.Error("Failed to set env vars", "error", err, "app", appName)
		results.SetError(fmt.Sprintf("failed to set env vars: %v", err))
		return nil
	}

	results.SetVersionId(mutResult.VersionID)

	// Create deployment record and handle lock/activation (shared with DeployVersion)
	deployErr := d.createEnvVarDeployment(ctx, appName, clusterId, mutResult, results)
	if deployErr != nil {
		return deployErr
	}

	return nil
}

func (d *DeploymentServer) DeleteEnvVars(ctx context.Context, req *deployment_v1alpha.DeploymentDeleteEnvVars) error {
	args := req.Args()
	results := req.Results()

	if !args.HasAppName() || args.AppName() == "" {
		return cond.ValidationFailure("missing-field", "app_name is required")
	}
	if !args.HasClusterId() || args.ClusterId() == "" {
		return cond.ValidationFailure("missing-field", "cluster_id is required")
	}
	if !args.HasKeys() || len(args.Keys()) == 0 {
		return cond.ValidationFailure("missing-field", "keys is required")
	}

	appName := args.AppName()
	clusterId := args.ClusterId()
	service := ""
	if args.HasService() {
		service = args.Service()
	}

	// Call shared helper to create new version
	delResult, err := appclient.DeleteEnvVars(ctx, d.EC, appName, nil, args.Keys(), service)
	if err != nil {
		d.Log.Error("Failed to delete env vars", "error", err, "app", appName)
		results.SetError(fmt.Sprintf("failed to delete env vars: %v", err))
		return nil
	}

	results.SetVersionId(delResult.VersionID)
	deletedSources := delResult.DeletedSources
	results.SetDeletedSources(&deletedSources)

	// Create deployment record and handle lock/activation
	deployErr := d.createEnvVarDeployment(ctx, appName, clusterId, &delResult.MutateResult, results)
	if deployErr != nil {
		return deployErr
	}

	return nil
}

// envVarDeployResults is the subset of result setters needed by createEnvVarDeployment.
type envVarDeployResults interface {
	SetDeployment(*deployment_v1alpha.DeploymentInfo)
	SetError(string)
	SetLockInfo(*deployment_v1alpha.DeploymentLockInfo)
	SetAccessInfo(**deployment_v1alpha.AccessInfo)
}

// createEnvVarDeployment handles the deployment record creation, lock checking,
// and access info population shared by SetEnvVars and DeleteEnvVars.
func (d *DeploymentServer) createEnvVarDeployment(ctx context.Context, appName, clusterId string,
	mutResult *appclient.MutateResult, results envVarDeployResults) error {

	appVersionId := mutResult.VersionID

	// Check for existing in_progress deployments (deployment lock)
	existingDeployments, err := d.listDeploymentsInternal(ctx, appName, "in_progress", 1)
	if err != nil {
		d.Log.Error("Failed to check for existing deployments", "error", err)
		results.SetError("failed to check deployment lock")
		return nil
	}

	if len(existingDeployments) > 0 {
		existing := existingDeployments[0].deployment
		existingEnt := existingDeployments[0].entity

		deploymentTime, parseErr := time.Parse(time.RFC3339, existing.DeployedBy.Timestamp)
		if parseErr != nil {
			deploymentTime = time.Time{}
		}

		isExpired := deploymentTime.IsZero() || time.Since(deploymentTime) >= deploymentLockTimeout
		if !isExpired {
			lockExpiresAt := deploymentTime.Add(deploymentLockTimeout)

			displayEmail := existing.DeployedBy.UserEmail
			if displayEmail == "" || displayEmail == "unknown@example.com" || displayEmail == "user@example.com" {
				displayEmail = "-"
			}

			lockInfo := &deployment_v1alpha.DeploymentLockInfo{}
			lockInfo.SetAppName(appName)
			lockInfo.SetClusterId(clusterId)
			lockInfo.SetBlockingDeploymentId(string(existing.ID))
			lockInfo.SetBlockingDeploymentShortId(shortIDFromRPCEntity(existingEnt))
			lockInfo.SetStartedBy(displayEmail)
			lockInfo.SetStartedAt(standard.ToTimestamp(deploymentTime))
			lockInfo.SetCurrentPhase(existing.Phase)
			lockInfo.SetLockExpiresAt(standard.ToTimestamp(lockExpiresAt))

			results.SetLockInfo(lockInfo)
			results.SetError("deployment blocked by existing in-progress deployment")
			return nil
		}

		// Expired lock — mark as failed and continue
		d.Log.Warn("Found expired in_progress deployment, marking as failed",
			"deployment_id", string(existing.ID),
			"age", time.Since(deploymentTime))

		existing.Status = "failed"
		existing.ErrorMessage = fmt.Sprintf("Deployment timed out after %v", deploymentLockTimeout)
		existing.CompletedAt = time.Now().Format(time.RFC3339)

		updateAttrs := existing.Encode()
		updateEntity := &entityserver_v1alpha.Entity{}
		updateEntity.SetId(string(existing.ID))
		updateEntity.SetAttrs(updateAttrs)

		if existingEntity, getErr := d.EAC.Get(ctx, string(existing.ID)); getErr == nil {
			updateEntity.SetRevision(existingEntity.Entity().Revision())
			if _, putErr := d.EAC.Put(ctx, updateEntity); putErr != nil {
				d.Log.Error("Failed to mark expired deployment as failed", "error", putErr)
			}
		}
	}

	// Create new deployment entity
	now := time.Now()

	deployment := &core_v1alpha.Deployment{
		AppName:    appName,
		AppVersion: appVersionId,
		ClusterId:  clusterId,
		Status:     "active",
		Phase:      "activating",
		DeployedBy: core_v1alpha.DeployedBy{
			Timestamp: now.Format(time.RFC3339),
		},
		CompletedAt: now.Format(time.RFC3339),
	}

	// Create entity
	attrs := deployment.Encode()
	rpcEntity := &entityserver_v1alpha.Entity{}
	rpcEntity.SetAttrs(attrs)

	putResp, err := d.EAC.Put(ctx, rpcEntity)
	if err != nil {
		d.Log.Error("Failed to create deployment entity", "error", err)
		results.SetError("failed to create deployment")
		return nil
	}

	deployment.ID = entity.Id(putResp.Id())
	newDeploymentId := putResp.Id()

	// Mark previous active deployments as succeeded
	if err := d.markPreviousActiveAs(ctx, appName, clusterId, newDeploymentId, "succeeded"); err != nil {
		d.Log.Error("Failed to mark previous active deployments", "error", err)
	}

	deploymentInfo := d.toDeploymentInfo(deployment)
	if depEnt, getErr := d.EAC.Get(ctx, newDeploymentId); getErr == nil {
		versionShortIDs := d.resolveShortIDs(ctx, []string{deployment.AppVersion})
		enrichDeploymentShortIDs(deploymentInfo, depEnt.Entity(), versionShortIDs)
	}
	results.SetDeployment(deploymentInfo)

	accessInfo := d.getAccessInfo(ctx, appName, "")
	results.SetAccessInfo(&accessInfo)

	d.Log.Info("Env var deployment completed",
		"deployment_id", newDeploymentId,
		"app", appName,
		"cluster", clusterId,
		"version", appVersionId)

	return nil
}

// getAccessInfo queries routes to determine how the app can be accessed
// getAccessInfo queries routes to determine how the app can be accessed.
// When ephemeralLabel is non-empty, route hostnames are resolved to concrete
// ephemeral hostnames (wildcard routes have * replaced, normal routes get
// the label prepended).
func (d *DeploymentServer) getAccessInfo(ctx context.Context, appName string, ephemeralLabel string) *deployment_v1alpha.AccessInfo {
	info := &deployment_v1alpha.AccessInfo{}

	appEntity, err := d.AppClient.GetByName(ctx, appName)
	if err != nil {
		d.Log.Debug("could not get app for access info", "app", appName, "error", err)
		return info
	}

	routes, err := d.IngressClient.List(ctx)
	if err != nil {
		d.Log.Debug("could not list routes for access info", "error", err)
		return info
	}

	var hostnames []string
	var hasDefaultRoute bool

	for _, r := range routes {
		if r.Route.App != appEntity.ID {
			continue
		}
		if r.Route.Default {
			hasDefaultRoute = true
		}
		if r.Route.Host == "" {
			continue
		}
		host := r.Route.Host
		if ephemeralLabel != "" {
			if strings.HasPrefix(host, "*.") {
				host = ephemeralLabel + "." + host[2:]
			} else {
				host = ephemeralLabel + "." + host
			}
		}
		hostnames = append(hostnames, host)
	}

	info.SetHostnames(&hostnames)
	info.SetDefaultRoute(hasDefaultRoute)

	if d.DNSHostname != "" {
		info.SetClusterHostname(d.DNSHostname)
	}

	return info
}

// Internal helper methods

// listDeploymentsInternal lists deployments from this cluster's entity store,
// optionally filtered by app name and status. It does not filter by cluster:
// the store is a loopback into this coordinator's own etcd, so every deployment
// it holds already belongs to this cluster. The cluster_id stamped on a
// deployment is client-supplied and inconsistent (a manual deploy sends the
// cluster name, a CI/OIDC deploy sends the raw address), so filtering on it
// would hide legitimate deploys — see MIR-1465.
func (d *DeploymentServer) listDeploymentsInternal(ctx context.Context, appName, status string, limit int) ([]deploymentWithEntity, error) {
	// List all deployments by type
	listResp, err := d.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindDeployment))
	if err != nil {
		d.Log.Error("Failed to list deployments", "error", err)
		return nil, cond.Error("failed to list deployments")
	}

	// Get the entity values
	entities := listResp.Values()

	// Decode and filter deployments
	deployments := make([]deploymentWithEntity, 0)
	for _, e := range entities {
		// List already returns full entity data with attributes, no need to fetch again
		var dep core_v1alpha.Deployment
		decodeEntity(e, &dep)

		// Apply filters
		if appName != "" && dep.AppName != appName {
			continue
		}
		if status != "" && dep.Status != status {
			continue
		}

		deployments = append(deployments, deploymentWithEntity{deployment: &dep, entity: e})
	}

	// Sort by timestamp (newest first) using efficient sort.Slice
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].deployment.DeployedBy.Timestamp > deployments[j].deployment.DeployedBy.Timestamp
	})

	// Apply limit after sorting
	if limit > 0 && len(deployments) > limit {
		deployments = deployments[:limit]
	}

	return deployments, nil
}

func (d *DeploymentServer) toDeploymentInfo(deployment *core_v1alpha.Deployment) *deployment_v1alpha.DeploymentInfo {
	info := &deployment_v1alpha.DeploymentInfo{}

	info.SetId(string(deployment.ID))
	info.SetAppName(deployment.AppName)
	info.SetAppVersionId(deployment.AppVersion)
	info.SetClusterId(deployment.ClusterId)
	info.SetStatus(deployment.Status)
	info.SetPhase(deployment.Phase)
	info.SetDeployedByUserId(deployment.DeployedBy.UserId)
	info.SetDeployedByUserName(deployment.DeployedBy.UserName)
	info.SetDeployedByUserEmail(deployment.DeployedBy.UserEmail)

	// Parse timestamps
	if deployedAt, err := time.Parse(time.RFC3339, deployment.DeployedBy.Timestamp); err == nil {
		info.SetDeployedAt(standard.ToTimestamp(deployedAt))
	}
	if deployment.CompletedAt != "" {
		if completedAt, err := time.Parse(time.RFC3339, deployment.CompletedAt); err == nil {
			info.SetCompletedAt(standard.ToTimestamp(completedAt))
		}
	}

	// Add error information if available
	if deployment.ErrorMessage != "" {
		info.SetErrorMessage(deployment.ErrorMessage)
	}
	if deployment.BuildLogs != "" {
		info.SetBuildLogs(deployment.BuildLogs)
	}

	// Add source deployment ID if available (rollback/redeploy provenance)
	if deployment.SourceDeploymentId != "" {
		info.SetSourceDeploymentId(deployment.SourceDeploymentId)
	}

	// Add git info if available
	if deployment.GitInfo.Sha != "" {
		gitInfo := &deployment_v1alpha.GitInfo{}
		gitInfo.SetSha(deployment.GitInfo.Sha)
		gitInfo.SetBranch(deployment.GitInfo.Branch)
		gitInfo.SetCommitMessage(deployment.GitInfo.Message)
		gitInfo.SetCommitAuthorName(deployment.GitInfo.Author)
		gitInfo.SetIsDirty(deployment.GitInfo.IsDirty)
		gitInfo.SetWorkingTreeHash(deployment.GitInfo.WorkingTreeHash)
		gitInfo.SetCommitAuthorEmail(deployment.GitInfo.CommitAuthorEmail)
		gitInfo.SetRepository(deployment.GitInfo.Repository)

		// Handle optional timestamp
		if deployment.GitInfo.CommitTimestamp != "" {
			if ts, err := time.Parse(time.RFC3339, deployment.GitInfo.CommitTimestamp); err == nil {
				gitInfo.SetCommitTimestamp(standard.ToTimestamp(ts))
			}
		}

		info.SetGitInfo(gitInfo)
	}

	return info
}

// shortIDFromRPCEntity extracts the db/short-id attribute from an RPC entity.
func shortIDFromRPCEntity(ent *entityserver_v1alpha.Entity) string {
	if ent == nil {
		return ""
	}
	for _, attr := range ent.Attrs() {
		if entity.Id(attr.ID) == entity.DBShortId {
			return attr.Value.String()
		}
	}
	return ""
}

// enrichDeploymentShortIDs populates the short ID fields on a DeploymentInfo
// from the deployment entity and a version short ID lookup map.
func enrichDeploymentShortIDs(info *deployment_v1alpha.DeploymentInfo, ent *entityserver_v1alpha.Entity, versionShortIDs map[string]string) {
	if sid := shortIDFromRPCEntity(ent); sid != "" {
		info.SetShortId(sid)
	}
	if versionShortIDs != nil {
		if sid, ok := versionShortIDs[info.AppVersionId()]; ok && sid != "" {
			info.SetAppVersionShortId(sid)
		}
	}
}

// resolveShortIDs batch-resolves short IDs for a set of entity IDs.
func (d *DeploymentServer) resolveShortIDs(ctx context.Context, ids []string) map[string]string {
	result := make(map[string]string, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := result[id]; ok {
			continue // already resolved
		}
		resp, err := d.EAC.Get(ctx, id)
		if err != nil {
			continue
		}
		if sid := shortIDFromRPCEntity(resp.Entity()); sid != "" {
			result[id] = sid
		}
	}
	return result
}

type deploymentWithEntity struct {
	deployment *core_v1alpha.Deployment
	entity     *entityserver_v1alpha.Entity
}

// markPreviousActiveAs marks all active deployments for the given app/cluster with the target status,
// except for the specified currentDeploymentId
func (d *DeploymentServer) markPreviousActiveAs(ctx context.Context, appName, clusterId, currentDeploymentId, targetStatus string) error {
	// List all active deployments for this app
	deployments, err := d.listDeploymentsInternal(ctx, appName, "active", 100)
	if err != nil {
		return err
	}

	// Update each active deployment (except the current one) to the target status
	for _, dwe := range deployments {
		dep := dwe.deployment
		if string(dep.ID) == currentDeploymentId {
			continue
		}

		dep.Status = targetStatus
		if dep.CompletedAt == "" {
			dep.CompletedAt = time.Now().Format(time.RFC3339)
		}

		// Update entity
		updateAttrs := dep.Encode()
		updateEntity := &entityserver_v1alpha.Entity{}
		updateEntity.SetId(string(dep.ID))
		updateEntity.SetAttrs(updateAttrs)

		// Get current entity to get revision
		currentResp, err := d.EAC.Get(ctx, string(dep.ID))
		if err != nil {
			d.Log.Error("Failed to get deployment for update", "deployment_id", dep.ID, "error", err)
			continue
		}
		updateEntity.SetRevision(currentResp.Entity().Revision())

		_, err = d.EAC.Put(ctx, updateEntity)
		if err != nil {
			d.Log.Error("Failed to mark deployment", "deployment_id", dep.ID, "target_status", targetStatus, "error", err)
			continue
		}

		d.Log.Info("Marked previous active deployment",
			"deployment_id", dep.ID,
			"target_status", targetStatus,
			"app", appName,
			"cluster", clusterId)
	}

	return nil
}

// decodeEntity is a helper to decode RPC entity to struct
func decodeEntity(rpcEntity *entityserver_v1alpha.Entity, target interface{}) {
	type decoder interface {
		Decode(entity.AttrGetter)
	}

	if d, ok := target.(decoder); ok {
		d.Decode(&rpcEntityWrapper{entity: rpcEntity})
	}
}

// rpcEntityWrapper wraps RPC entity to implement AttrGetter
type rpcEntityWrapper struct {
	entity *entityserver_v1alpha.Entity
}

func (w *rpcEntityWrapper) Get(id entity.Id) (entity.Attr, bool) {
	// Special case for db/id - synthesize it from the entity ID
	if id == entity.DBId {
		return entity.Ref(entity.DBId, entity.Id(w.entity.Id())), true
	}

	attrs := w.entity.Attrs()
	for _, attr := range attrs {
		if entity.Id(attr.ID) == id {
			return attr, true
		}
	}
	return entity.Attr{}, false
}

func (w *rpcEntityWrapper) GetAll(name entity.Id) []entity.Attr {
	var result []entity.Attr
	attrs := w.entity.Attrs()
	for _, attr := range attrs {
		if entity.Id(attr.ID) == name {
			result = append(result, attr)
		}
	}
	return result
}

func (w *rpcEntityWrapper) Attrs() []entity.Attr {
	return w.entity.Attrs()
}

// createDerivedVersion clones an existing AppVersion with CLI env vars merged
// into its config, creates the new entity, and returns it.
func (d *DeploymentServer) createDerivedVersion(ctx context.Context, base *core_v1alpha.AppVersion, envVars []*deployment_v1alpha.EnvironmentVariable) (*core_v1alpha.AppVersion, error) {
	// Extract app name from the base version's App field (e.g. "app/go-server" -> "go-server")
	appName := string(base.App)
	if idx := len("app/"); len(appName) > idx && appName[:idx] == "app/" {
		appName = appName[idx:]
	}

	newVersionName := appName + "-" + idgen.Gen("v")

	derived := &core_v1alpha.AppVersion{
		App:            base.App,
		Version:        newVersionName,
		Artifact:       base.Artifact,
		ImageUrl:       base.ImageUrl,
		Config:         base.Config,
		AdminToken:     base.AdminToken,
		Manifest:       base.Manifest,
		ManifestDigest: base.ManifestDigest,
	}

	// Merge env vars into config
	varMap := make(map[string]core_v1alpha.Variable)
	for _, v := range derived.Config.Variable {
		varMap[v.Key] = v
	}
	for _, ev := range envVars {
		varMap[ev.Key()] = core_v1alpha.Variable{
			Key:       ev.Key(),
			Value:     ev.Value(),
			Sensitive: ev.Sensitive(),
			Source:    "manual",
		}
	}
	derived.Config.Variable = make([]core_v1alpha.Variable, 0, len(varMap))
	for _, v := range varMap {
		derived.Config.Variable = append(derived.Config.Variable, v)
	}

	id, err := d.EC.Create(ctx, newVersionName, derived)
	if err != nil {
		return nil, fmt.Errorf("failed to create derived version entity: %w", err)
	}
	derived.ID = id

	return derived, nil
}
