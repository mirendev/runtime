package deployment

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc/standard"
)

type DeploymentServer struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient
}

var _ deployment_v1alpha.Deployment = (*DeploymentServer)(nil)

func NewDeploymentServer(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) (*DeploymentServer, error) {
	return &DeploymentServer{
		Log: log.With("module", "deployment"),
		EAC: eac,
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

	// Check for existing in_progress deployments for this app+cluster
	existingDeployments, err := d.listDeploymentsInternal(ctx, appName, clusterId, "in_progress", 1)
	if err != nil {
		d.Log.Error("Failed to check for existing deployments", "error", err)
		return cond.Error("failed to check deployment lock")
	}

	if len(existingDeployments) > 0 {
		// Found an existing in_progress deployment
		existing := existingDeployments[0]
		
		// Parse the deployment timestamp
		deploymentTime, err := time.Parse(time.RFC3339, existing.DeployedBy.Timestamp)
		if err != nil {
			d.Log.Error("Failed to parse deployment timestamp", "error", err)
			return cond.Error("failed to parse deployment timestamp")
		}

		// Check if the existing deployment is expired (older than 30 minutes)
		if time.Since(deploymentTime) < 30*time.Minute {
			// Deployment is still within the lock timeout
			timeRemaining := 30*time.Minute - time.Since(deploymentTime)

			// Format user email for display
			displayEmail := existing.DeployedBy.UserEmail
			if displayEmail == "" || displayEmail == "unknown@example.com" || displayEmail == "user@example.com" {
				displayEmail = "-"
			}

			// Build contact message
			contactMsg := "Please wait for it to complete."
			if displayEmail != "-" {
				contactMsg = fmt.Sprintf("Please wait for it to complete or contact %s to coordinate.", displayEmail)
			}

			results.SetError(fmt.Sprintf("Another deployment is already in progress for app '%s' on cluster '%s'.\n\n"+
				"Existing deployment details:\n"+
				"  • Deployment ID: %s\n"+
				"  • Started by: %s\n"+
				"  • Started at: %s (%s ago)\n"+
				"  • Current phase: %s\n"+
				"  • Lock expires in: %s\n\n"+
				"%s",
				appName, clusterId,
				string(existing.ID),
				displayEmail,
				deploymentTime.Format("2006-01-02 15:04:05 MST"),
				time.Since(deploymentTime).Round(time.Second),
				existing.Phase,
				timeRemaining.Round(time.Second),
				contactMsg))
			return nil
		}

		// Existing deployment is expired, mark it as failed
		d.Log.Warn("Found expired in_progress deployment, marking as failed",
			"deployment_id", string(existing.ID),
			"age", time.Since(deploymentTime))

		// Update the expired deployment to failed status
		// We need to call the internal method since we're in the server, not using the client
		existing.Status = "failed"
		existing.ErrorMessage = "Deployment timed out after 30 minutes"
		existing.CompletedAt = time.Now().Format(time.RFC3339)
		
		// Update entity
		updateAttrs := existing.Encode()
		updateEntity := &entityserver_v1alpha.Entity{}
		updateEntity.SetId(string(existing.ID))
		updateEntity.SetAttrs(updateAttrs)
		
		// We don't have the revision here, so we need to get it
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
	// For now, use placeholder values
	userId := "user-" + uuid.New().String()
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
		"failed":      true,
		"rolled_back": true,
	}
	if !validStatuses[newStatus] {
		return cond.ValidationFailure("invalid-status",
			"status must be one of: in_progress, active, failed, rolled_back")
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

	// Update deployment with failure information
	deployment.Status = "failed"
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

	// Extract filters
	var appName, clusterId, status string
	var limit int32 = 100 // default limit

	if args.HasAppName() {
		appName = args.AppName()
	}
	if args.HasClusterId() {
		clusterId = args.ClusterId()
	}
	if args.HasStatus() {
		status = args.Status()
	}
	if args.HasLimit() && args.Limit() > 0 {
		limit = args.Limit()
	}

	deployments, err := d.listDeploymentsInternal(ctx, appName, clusterId, status, int(limit))
	if err != nil {
		return err
	}

	// Convert to deployment info list
	deploymentInfos := make([]*deployment_v1alpha.DeploymentInfo, 0, len(deployments))
	for _, dep := range deployments {
		deploymentInfos = append(deploymentInfos, d.toDeploymentInfo(dep))
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

	deploymentInfo := d.toDeploymentInfo(&deployment)
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

	// Find active deployment
	deployments, err := d.listDeploymentsInternal(ctx, appName, clusterId, "active", 1)
	if err != nil {
		return err
	}

	if len(deployments) == 0 {
		return cond.NotFound("active-deployment", fmt.Sprintf("%s/%s", appName, clusterId))
	}

	deployment := deployments[0]
	deploymentInfo := d.toDeploymentInfo(deployment)
	results.SetDeployment(deploymentInfo)

	return nil
}

// Internal helper methods

func (d *DeploymentServer) listDeploymentsInternal(ctx context.Context, appName, clusterId, status string, limit int) ([]*core_v1alpha.Deployment, error) {
	// List all deployments by type
	listResp, err := d.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindDeployment))
	if err != nil {
		d.Log.Error("Failed to list deployments", "error", err)
		return nil, cond.Error("failed to list deployments")
	}

	// Get the entity values
	entities := listResp.Values()

	// Decode and filter deployments
	deployments := make([]*core_v1alpha.Deployment, 0)
	for _, e := range entities {
		// List already returns full entity data with attributes, no need to fetch again
		var dep core_v1alpha.Deployment
		decodeEntity(e, &dep)

		// Apply filters
		if appName != "" && dep.AppName != appName {
			continue
		}
		if clusterId != "" && dep.ClusterId != clusterId {
			continue
		}
		if status != "" && dep.Status != status {
			continue
		}

		deployments = append(deployments, &dep)
	}

	// Sort by timestamp (newest first) using efficient sort.Slice
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].DeployedBy.Timestamp > deployments[j].DeployedBy.Timestamp
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
