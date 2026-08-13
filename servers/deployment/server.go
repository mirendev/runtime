package deployment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appclient "miren.dev/runtime/api/app"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	deployment_v1alpha "miren.dev/runtime/api/deployment/deployment_v1alpha"
	aes "miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	ephemeralx "miren.dev/runtime/pkg/ephemeral"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/secret"
)

type DeploymentServer struct {
	Log           *slog.Logger
	EAC           *entityserver_v1alpha.EntityAccessClient
	EC            *aes.Client
	AppClient     *appclient.Client
	IngressClient *ingress.Client
	DNSHostname   string

	// Secrets resolves backend-sourced variables so a ConfigVersion minted by an
	// env change records the exact secret version it saw.
	Secrets secret.Resolver

	// tracker owns the deployment record state machine and the deploy lock. The
	// build server holds its own Tracker over the same entity store, and both
	// acquire the same app-scoped lock, so a client that drives a record through
	// the deprecated RPCs still contends with a server-owned build.
	tracker *deploylifecycle.Tracker
}

var _ deployment_v1alpha.Deployment = (*DeploymentServer)(nil)

func NewDeploymentServer(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, ec *aes.Client, appClient *appclient.Client, dnsHostname string, secrets secret.Resolver) (*DeploymentServer, error) {
	return &DeploymentServer{
		Log:           log.With("module", "deployment"),
		EAC:           eac,
		EC:            ec,
		AppClient:     appClient,
		IngressClient: ingress.NewClient(log, eac),
		DNSHostname:   dnsHostname,
		Secrets:       secrets,
		tracker:       deploylifecycle.NewTracker(log, eac),
	}, nil
}

// lockBlockable is the shared result surface for the deploy paths that report a
// held lock back to the client as a structured domain outcome.
type lockBlockable interface {
	SetError(string)
	SetLockInfo(*deployment_v1alpha.DeploymentLockInfo)
}

// lockInfoFor builds the structured lock info for a held lock. The holder comes
// from the lock; the display details (phase, who started it, short id) come from
// the blocking record.
func (d *DeploymentServer) lockInfoFor(ctx context.Context, holder *deploylifecycle.Holder) *deployment_v1alpha.DeploymentLockInfo {
	info := &deployment_v1alpha.DeploymentLockInfo{}
	info.SetAppName(holder.AppName)
	info.SetBlockingDeploymentId(holder.DeploymentID)
	info.SetLockExpiresAt(standard.ToTimestamp(holder.ExpiresAt))

	displayEmail := "-"
	if rec, err := d.tracker.Store().Get(ctx, holder.DeploymentID); err == nil {
		// The lock is app-scoped, so cluster for display comes from the blocking
		// record rather than the lock.
		info.SetClusterId(rec.Deployment.ClusterId)
		info.SetCurrentPhase(rec.Deployment.Phase)
		info.SetBlockingDeploymentShortId(shortIDFromRPCEntity(rec.Entity))

		if email := rec.Deployment.DeployedBy.UserEmail; email != "" &&
			email != "unknown@example.com" && email != "user@example.com" {
			displayEmail = email
		}
		if ts, err := time.Parse(time.RFC3339, rec.Deployment.DeployedBy.Timestamp); err == nil {
			info.SetStartedAt(standard.ToTimestamp(ts))
		}
	}
	info.SetStartedBy(displayEmail)

	return info
}

// reportLockBlocked fills results with the structured "deployment blocked"
// domain outcome for a held lock.
func (d *DeploymentServer) reportLockBlocked(ctx context.Context, results lockBlockable, holder *deploylifecycle.Holder) {
	results.SetLockInfo(d.lockInfoFor(ctx, holder))
	results.SetError("deployment blocked by existing in-progress deployment")
}

// releaseDeployLock drops the deploy lock held by a deployment that has just
// settled through one of the deprecated client-driven methods. A failure to
// release is logged, not returned: the record already reached its terminal
// state, and the lock will expire on its own.
func (d *DeploymentServer) releaseDeployLock(ctx context.Context, appName, deploymentID string) {
	if err := d.tracker.Locks().Release(ctx, appName, deploymentID); err != nil {
		d.Log.Error("failed to release deploy lock",
			"deployment_id", deploymentID, "app", appName, "error", err)
	}
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

	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}

	// If the referenced version resolves to a real AppVersion, it must belong to
	// this app — AllowApp confined the target app, not the source version, so
	// without this a caller could point a deployment at another app's built
	// version. The normal flow passes a "pending-build" placeholder here (no
	// entity yet) and attaches the real version later via
	// UpdateDeploymentAppVersion, which enforces the same ownership; so a
	// not-found version is left alone rather than rejected. Any other lookup
	// error fails closed — skipping the check on a transient store error would
	// let a cross-app version slip through.
	var appVersion core_v1alpha.AppVersion
	switch err := d.EC.Get(ctx, args.AppVersionId(), &appVersion); {
	case err == nil:
		if verifyErr := d.verifyVersionOwnedByApp(ctx, &appVersion, appName); verifyErr != nil {
			return verifyErr
		}
	case !errors.Is(err, cond.ErrNotFound{}):
		d.Log.Error("Failed to look up app version", "app_version_id", args.AppVersionId(), "error", err)
		return cond.Error("failed to look up app version")
	}

	var gitInfo core_v1alpha.GitInfo
	if args.HasGitInfo() {
		gitInfo = gitInfoFromRPC(args.GitInfo())
	}

	// Begin creates the record and takes the deploy lock atomically, replacing
	// the old list-then-create-with-timeout dance. The "pending-build" sentinel
	// the older CLI passes is normalized away — app_version is optional now.
	rec, err := d.tracker.Begin(ctx, deploylifecycle.BeginParams{
		AppName:    appName,
		ClusterID:  clusterId,
		AppVersion: normalizeAppVersion(args.AppVersionId(), ""),
		GitInfo:    gitInfo,
	})
	if err != nil {
		if holder, ok := deploylifecycle.HolderFrom(err); ok {
			d.reportLockBlocked(ctx, results, holder)
			return nil
		}
		d.Log.Error("Failed to create deployment", "error", err)
		return cond.Error("failed to create deployment")
	}

	deploymentInfo := d.toDeploymentInfo(rec.Deployment)
	versionShortIDs := d.resolveShortIDs(ctx, []string{rec.Deployment.AppVersion})
	enrichDeploymentShortIDs(deploymentInfo, rec.Entity, versionShortIDs)
	results.SetDeployment(deploymentInfo)

	d.Log.Info("Created deployment",
		"deployment_id", rec.Deployment.ID,
		"app", appName,
		"cluster", clusterId)

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

	// Validate against the single source of truth so the accepted set and the
	// error message can never drift (the old inline message omitted "cancelled").
	if _, err := deploylifecycle.ParseStatus(newStatus); err != nil {
		return err
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
		err = d.markPreviousActiveAs(ctx, deployment.AppName, deploymentId, "succeeded")
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

	// The deployment has left in_progress, so release the deploy lock this
	// record's Begin took. Release is a no-op if a newer deployment already
	// holds it.
	if newStatus != "in_progress" {
		d.releaseDeployLock(ctx, deployment.AppName, deploymentId)
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

	// Validate against the single source of truth so the accepted set and the
	// message can never drift from lifecycle.go's phase list.
	if _, err := deploylifecycle.ParsePhase(newPhase); err != nil {
		return err
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

	// The "pending-build" placeholder is left in place and normalized away on
	// read, rather than rewritten to a "failed-<id>" sentinel.

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

	// A failed (or already-cancelled) deployment holds the lock no longer.
	d.releaseDeployLock(ctx, deployment.AppName, deploymentId)

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

	// If the new version resolves to a real AppVersion, it must belong to the
	// deployment's app — otherwise a caller authorized for its own app could
	// repoint a deployment at another app's built version. A version string that
	// doesn't resolve to an entity is only a recorded label (the deprecated CLI
	// records one before any build exists), so it is left alone rather than
	// rejected; the cross-app attack always names a real, existing version. Any
	// other lookup error fails closed.
	var appVersion core_v1alpha.AppVersion
	switch err := d.EC.Get(ctx, appVersionId, &appVersion); {
	case err == nil:
		if verifyErr := d.verifyVersionOwnedByApp(ctx, &appVersion, deployment.AppName); verifyErr != nil {
			return verifyErr
		}
	case !errors.Is(err, cond.ErrNotFound{}):
		d.Log.Error("Failed to look up app version", "app_version_id", appVersionId, "error", err)
		return cond.Error("failed to look up app version")
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

func (d *DeploymentServer) GetDeployLock(ctx context.Context, req *deployment_v1alpha.DeploymentGetDeployLock) error {
	args := req.Args()
	results := req.Results()

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

	// Blocking folds in expiry and terminal-holder reconciliation, so a released
	// or dead lock reports as free rather than as a phantom block.
	holder, err := d.tracker.Locks().Blocking(ctx, appName)
	if err != nil {
		d.Log.Error("Failed to read deploy lock", "app", appName, "cluster", clusterId, "error", err)
		return cond.Error("failed to read deploy lock")
	}

	if holder == nil {
		results.SetHeld(false)
		return nil
	}

	results.SetHeld(true)
	results.SetLockInfo(d.lockInfoFor(ctx, holder))
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
		// "not found" is a domain outcome the client renders; a failure to reach
		// the store is infrastructure and surfaces as an RPC error.
		if errors.Is(err, cond.ErrNotFound{}) {
			results.SetError("deployment not found")
			return nil
		}
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.Error("failed to get deployment")
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

	// The tracker owns the transition and drops the lock, so cancellation goes
	// through the same state machine as every other settle rather than writing
	// "cancelled" straight to the entity.
	if err := d.tracker.Cancel(ctx, deploymentId, "Deployment cancelled by user"); err != nil {
		// Losing a race with the deploy finishing is a domain outcome, not an
		// infrastructure failure: by the time we wrote, it was no longer in
		// progress.
		if errors.Is(err, cond.ErrConflict{}) {
			results.SetError("deployment is no longer in progress")
			return nil
		}
		d.Log.Error("Failed to cancel deployment", "deployment_id", deploymentId, "error", err)
		return cond.Error("failed to cancel deployment")
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

	// The version must belong to the app being deployed — AllowApp only
	// confined the target app, not the source version.
	if err := d.verifyVersionOwnedByApp(ctx, &appVersion, appName); err != nil {
		return err
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

		// An ephemeral deploy off an old version needs the app's addon bindings
		// just as much as a normal one. Resolve before ReplaceExisting, which
		// deletes the preview currently holding this label: a resolution failure
		// after that point would leave the operator with neither the old preview
		// nor a new one.
		spec, needsDerived, err := d.resolveDeployConfig(ctx, appName, &appVersion, args.EnvVars())
		if err != nil {
			d.Log.Error("Failed to resolve config for ephemeral version", "app", appName, "version", appVersionId, "error", err)
			results.SetError(fmt.Sprintf("failed to resolve config: %v", err))
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

		// Only the ConfigVersion is minted here — ephVersion is the AppVersion
		// that gets created, so deriving a full version pair first would leave
		// the intermediate one orphaned. Config is blanked only alongside a new
		// ConfigVersion, so a legacy inline-config version with nothing to carry
		// keeps its inline config.
		if needsDerived {
			cvid, cvErr := d.createConfigVersion(ctx, &appVersion, spec)
			if cvErr != nil {
				d.Log.Error("Failed to create config version for ephemeral deploy", "app", appName, "error", cvErr)
				results.SetError(fmt.Sprintf("failed to apply config: %v", cvErr))
				return nil
			}
			ephVersion.ConfigVersion = cvid
			ephVersion.Config = core_v1alpha.Config{}
		}

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

	// Find the source deployment — the most recent deployment with this
	// app_version_id — for git info and rollback provenance.
	var gitInfo core_v1alpha.GitInfo
	var sourceDeploymentID string
	if allDeployments, listErr := d.listDeploymentsInternal(ctx, appName, "", 100); listErr != nil {
		d.Log.Error("Failed to list deployments for source lookup", "error", listErr)
	} else {
		for _, dwe := range allDeployments {
			if dwe.deployment.AppVersion == sourceVersionId {
				gitInfo = dwe.deployment.GitInfo
				sourceDeploymentID = string(dwe.deployment.ID)
				break // listDeploymentsInternal returns newest first
			}
		}
	}

	// Begin creates the record and takes the deploy lock atomically, contending
	// with any server-owned build for the same app+cluster.
	rec, err := d.tracker.Begin(ctx, deploylifecycle.BeginParams{
		AppName:            appName,
		ClusterID:          clusterId,
		AppVersion:         appVersionId,
		GitInfo:            gitInfo,
		SourceDeploymentID: sourceDeploymentID,
	})
	if err != nil {
		if holder, ok := deploylifecycle.HolderFrom(err); ok {
			d.reportLockBlocked(ctx, results, holder)
			return nil
		}
		d.Log.Error("Failed to create deployment", "error", err)
		results.SetError("failed to create deployment")
		return nil
	}

	deployment := rec.Deployment
	newDeploymentId := string(deployment.ID)

	// Every terminal settle below runs on a detached context: once we start
	// settling, a client that cancels the RPC must not strand the record
	// in_progress with the lock held until its TTL. The most likely moment for
	// that cancellation is right here, in the failure path.
	settleCtx, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancelSettle()

	// Work out what this version should actually run with: its own config, plus
	// the addon bindings and operator-set variables that belong to the app, plus
	// any CLI-supplied vars. When that differs from what the version already
	// stores, activate a derived version carrying the merged config instead.
	//
	// This sits after Begin so the read-merge-activate runs under the deploy
	// lock and a concurrent deploy for the same app cannot land between the read
	// and the activation. It does not close the window against the addon
	// controller, which swings active_version under its own CAS without taking
	// this lock — that needs SetActiveVersion to become a CAS too (MIR-1579
	// follow-up).
	spec, needsDerived, err := d.resolveDeployConfig(ctx, appName, &appVersion, args.EnvVars())
	if err != nil {
		d.Log.Error("Failed to resolve config for version", "app", appName, "version", appVersionId, "error", err)
		failMsg := fmt.Sprintf("failed to resolve config: %v", err)
		if failErr := d.tracker.Fail(settleCtx, newDeploymentId, failMsg, ""); failErr != nil {
			d.Log.Error("Failed to mark deployment as failed; releasing lock", "error", failErr)
			d.releaseDeployLock(settleCtx, appName, newDeploymentId)
		}
		results.SetError(failMsg)
		return nil
	}
	if needsDerived {
		derivedVersion, deriveErr := d.deriveVersion(ctx, &appVersion, spec)
		if deriveErr != nil {
			d.Log.Error("Failed to create derived version", "app", appName, "error", deriveErr)
			failMsg := fmt.Sprintf("failed to apply config: %v", deriveErr)
			if failErr := d.tracker.Fail(settleCtx, newDeploymentId, failMsg, ""); failErr != nil {
				d.Log.Error("Failed to mark deployment as failed; releasing lock", "error", failErr)
				d.releaseDeployLock(settleCtx, appName, newDeploymentId)
			}
			results.SetError(failMsg)
			return nil
		}
		appVersion = *derivedVersion
		appVersionId = derivedVersion.Version

		// The record was opened against the source version; point it at what is
		// actually going live so the deployment history and the CLI's health
		// wait both name the right version. This carries the version *name*, not
		// the entity id — that is what the record holds on every other path, and
		// what awaitHealthy on the client matches against.
		if setErr := d.tracker.SetAppVersion(ctx, newDeploymentId, derivedVersion.Version); setErr != nil {
			d.Log.Error("Failed to record derived version on deployment", "error", setErr)
		}

		d.Log.Info("Created derived version to preserve app config",
			"app", appName, "source_version", sourceVersionId, "derived_version", appVersionId,
			"variable_count", len(spec.Variables), "cli_env_var_count", len(args.EnvVars()))
	}

	// This path has no build; it goes straight to activation.
	//
	// Deploy-triggered tasks (RFD-97) deliberately do not run here. They gate
	// the build saga, which activates a *new* version; this path activates one
	// that already exists, which is a rollback or a redeploy of a known-good
	// version. Re-running a migration against a database that has already been
	// migrated forward is more likely to be wrong than right, and the platform
	// has no down-migrations to make it reversible.
	//
	// That is a decision rather than an oversight, but it is not one RFD-97
	// makes -- it is tracked as an open question there. If rollbacks should run
	// tasks, this is the hook point.
	if phaseErr := d.tracker.SetPhase(ctx, newDeploymentId, deploylifecycle.PhaseActivating); phaseErr != nil {
		d.Log.Error("Failed to set activating phase", "error", phaseErr)
	}

	if err := d.AppClient.SetActiveVersion(ctx, appName, string(appVersion.ID)); err != nil {
		d.Log.Error("Failed to set active version", "error", err, "app", appName, "version_id", string(appVersion.ID))

		failMsg := fmt.Sprintf("failed to activate version: %v", err)
		if failErr := d.tracker.Fail(settleCtx, newDeploymentId, failMsg, ""); failErr != nil {
			d.Log.Error("Failed to mark deployment as failed; releasing lock", "error", failErr)
			d.releaseDeployLock(settleCtx, appName, newDeploymentId)
		}

		results.SetError(failMsg)
		return nil
	}

	activate := d.tracker.Activate
	if isRollback {
		activate = d.tracker.ActivateRollback
	}
	// SetActiveVersion already made the version live. If the settle still fails,
	// release the lock directly so later deploys aren't blocked for the full TTL.
	if err := activate(settleCtx, newDeploymentId); err != nil {
		d.Log.Error("Failed to activate deployment; releasing lock", "error", err)
		d.releaseDeployLock(settleCtx, appName, newDeploymentId)
	}

	// Re-read so the response reflects the settled state.
	if settled, getErr := d.tracker.Store().Get(settleCtx, newDeploymentId); getErr == nil {
		deployment = settled.Deployment
		rec = settled
	}

	deploymentInfo := d.toDeploymentInfo(deployment)
	versionShortIDs := d.resolveShortIDs(ctx, []string{deployment.AppVersion})
	enrichDeploymentShortIDs(deploymentInfo, rec.Entity, versionShortIDs)
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

	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}

	clusterId := args.ClusterId()
	service := ""
	if args.HasService() {
		service = args.Service()
	}

	// Convert RPC vars to shared helper input
	vars := make([]appclient.EnvVarInput, len(args.Vars()))
	for i, v := range args.Vars() {
		vars[i] = appclient.EnvVarInput{Key: v.Key(), Value: v.Value(), Sensitive: v.Sensitive(), Backend: v.Backend()}
	}

	// Call shared helper to create new version
	mutResult, err := appclient.SetEnvVars(ctx, d.EC, d.Secrets, appName, nil, vars, service)
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

	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}

	clusterId := args.ClusterId()
	service := ""
	if args.HasService() {
		service = args.Service()
	}

	// Call shared helper to create new version
	delResult, err := appclient.DeleteEnvVars(ctx, d.EC, d.Secrets, appName, nil, args.Keys(), service)
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

	// An env-var change has no build, so the record goes straight from Begin to
	// active. Begin takes the deploy lock, contending with any server-owned
	// build for the same app+cluster.
	rec, err := d.tracker.Begin(ctx, deploylifecycle.BeginParams{
		AppName:    appName,
		ClusterID:  clusterId,
		AppVersion: appVersionId,
	})
	if err != nil {
		if holder, ok := deploylifecycle.HolderFrom(err); ok {
			d.reportLockBlocked(ctx, results, holder)
			return nil
		}
		d.Log.Error("Failed to create deployment", "error", err)
		results.SetError("failed to create deployment")
		return nil
	}

	newDeploymentId := string(rec.Deployment.ID)

	// The new version is already applied by the caller; settle immediately,
	// which marks the previous active deployment succeeded and releases the lock.
	// Settle on a detached context and release the lock as a backstop so a
	// cancelled RPC or transient store error can't strand the record in_progress
	// with the lock held (see DeployVersion for the same reasoning).
	settleCtx, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancelSettle()
	if err := d.tracker.Activate(settleCtx, newDeploymentId); err != nil {
		d.Log.Error("Failed to activate env var deployment; releasing lock", "error", err)
		d.releaseDeployLock(settleCtx, appName, newDeploymentId)
	}

	deployment := rec.Deployment
	if settled, getErr := d.tracker.Store().Get(settleCtx, newDeploymentId); getErr == nil {
		deployment = settled.Deployment
		rec = settled
	}

	deploymentInfo := d.toDeploymentInfo(deployment)
	versionShortIDs := d.resolveShortIDs(ctx, []string{deployment.AppVersion})
	enrichDeploymentShortIDs(deploymentInfo, rec.Entity, versionShortIDs)
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

// listDeploymentsInternal lists deployments from this cluster's store, filtered
// by app and status. It does not filter by cluster: the store is a loopback into
// this coordinator's own etcd, so every deployment it holds already belongs to
// this cluster, and the client-supplied cluster_id is unreliable (a manual
// deploy sends the cluster name, a CI/OIDC deploy sends the raw address), so
// filtering on it would hide legitimate deploys. See MIR-1465.
func (d *DeploymentServer) listDeploymentsInternal(ctx context.Context, appName, status string, limit int) ([]deploymentWithEntity, error) {
	// Backed by the shared indexed store, which selects an index from the
	// filters instead of scanning every deployment ever created.
	records, err := d.tracker.Store().List(ctx, deploylifecycle.Query{
		AppName: appName,
		Status:  deploylifecycle.Status(status),
		Limit:   limit,
	})
	if err != nil {
		d.Log.Error("Failed to list deployments", "error", err)
		return nil, cond.Error("failed to list deployments")
	}

	deployments := make([]deploymentWithEntity, 0, len(records))
	for _, rec := range records {
		deployments = append(deployments, deploymentWithEntity{deployment: rec.Deployment, entity: rec.Entity})
	}
	return deployments, nil
}

// normalizeAppVersion drops the legacy placeholder values older clients wrote
// into app_version before a build had produced one. The field is optional now,
// so these are reported as empty rather than migrated.
func normalizeAppVersion(version, deploymentID string) string {
	if version == "pending-build" {
		return ""
	}
	if deploymentID != "" && version == "failed-"+deploymentID {
		return ""
	}
	return version
}

// gitInfoFromRPC converts the deployment-service git shape into the core entity
// shape stored on the record.
func gitInfoFromRPC(gi *deployment_v1alpha.GitInfo) core_v1alpha.GitInfo {
	if gi == nil {
		return core_v1alpha.GitInfo{}
	}

	info := core_v1alpha.GitInfo{
		Sha:               gi.Sha(),
		Branch:            gi.Branch(),
		Message:           gi.CommitMessage(),
		Author:            gi.CommitAuthorName(),
		IsDirty:           gi.IsDirty(),
		WorkingTreeHash:   gi.WorkingTreeHash(),
		CommitAuthorEmail: gi.CommitAuthorEmail(),
		Repository:        gi.Repository(),
	}
	if gi.HasCommitTimestamp() && gi.CommitTimestamp() != nil {
		info.CommitTimestamp = standard.FromTimestamp(gi.CommitTimestamp()).Format(time.RFC3339)
	}
	return info
}

func (d *DeploymentServer) toDeploymentInfo(deployment *core_v1alpha.Deployment) *deployment_v1alpha.DeploymentInfo {
	info := &deployment_v1alpha.DeploymentInfo{}

	info.SetId(string(deployment.ID))
	info.SetAppName(deployment.AppName)
	// Normalize legacy placeholder versions to empty so history renders "—"
	// rather than "pending-build" or "failed-<id>".
	info.SetAppVersionId(normalizeAppVersion(deployment.AppVersion, string(deployment.ID)))
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

// markPreviousActiveAs settles the deployments that were active for this app,
// leaving currentDeploymentId alone. Backed by the shared store's status-indexed
// implementation.
func (d *DeploymentServer) markPreviousActiveAs(ctx context.Context, appName, currentDeploymentId, targetStatus string) error {
	return d.tracker.Store().MarkPreviousActiveAs(ctx, appName, currentDeploymentId, deploylifecycle.Status(targetStatus))
}

// decodeEntity is a helper to decode RPC entity to struct
func decodeEntity(rpcEntity *entityserver_v1alpha.Entity, target any) {
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

// verifyVersionOwnedByApp rejects deploying an AppVersion that belongs to a
// different app than appName.
//
// The rpc.AllowApp guards on the deploy methods confine the *target* app but not
// the *source version*: a caller authorized for its own app could otherwise name
// another app's built version and pull that app's image and config — and any
// secrets baked into them — into its own runtime. The comparison is on the
// canonical app entity ID, so it is independent of whether appName arrives as a
// bare name or an "app/<name>" ref.
func (d *DeploymentServer) verifyVersionOwnedByApp(ctx context.Context, version *core_v1alpha.AppVersion, appName string) error {
	// An owner-less version (App unset) is not the attack this defends against —
	// every version the build produces sets App (servers/build/build.go), and a
	// workload cannot mint entities (entityaccess is cert-only), so the cross-app
	// case always has a non-empty, mismatched App. Only reject that.
	if version.App == "" {
		return nil
	}

	var appRec core_v1alpha.App
	if err := d.EC.Get(ctx, appName, &appRec); err != nil {
		return err
	}
	if version.App != appRec.ID {
		return cond.ValidationFailure("app-version-mismatch",
			fmt.Sprintf("app version does not belong to app %q", appName))
	}
	return nil
}

// deriveVersion clones an existing AppVersion onto a new ConfigSpec, minting the
// ConfigVersion + AppVersion pair the rest of the system reads through. The
// artifact fields are carried over verbatim: the point of this path is to run
// exactly the image the base version built, under a different configuration.
//
// The pair is written the same way as the build path (servers/build/build_saga.go)
// and the env-mutation path (api/app/envvar.go): config lives in the
// ConfigVersion entity and the inline Config field is left blank.
func (d *DeploymentServer) deriveVersion(ctx context.Context, base *core_v1alpha.AppVersion, spec *core_v1alpha.ConfigSpec) (*core_v1alpha.AppVersion, error) {
	appName := strings.TrimPrefix(string(base.App), "app/")
	newVersionName := appName + "-" + idgen.Gen("v")

	cvid, err := d.createConfigVersion(ctx, base, spec)
	if err != nil {
		return nil, err
	}

	derived := &core_v1alpha.AppVersion{
		App:            base.App,
		Version:        newVersionName,
		Artifact:       base.Artifact,
		ImageUrl:       base.ImageUrl,
		ConfigVersion:  cvid,
		AdminToken:     base.AdminToken,
		Manifest:       base.Manifest,
		ManifestDigest: base.ManifestDigest,
	}

	id, err := d.EC.Create(ctx, newVersionName, derived)
	if err != nil {
		return nil, fmt.Errorf("failed to create derived version entity: %w", err)
	}
	derived.ID = id

	return derived, nil
}

// createConfigVersion writes a ConfigVersion entity holding spec, pinning any
// backend-sourced variables first so the record names the exact secret version
// it was built with. A spec with no references never needs a resolver, which
// keeps clusters with no secret backends working exactly as before.
func (d *DeploymentServer) createConfigVersion(ctx context.Context, base *core_v1alpha.AppVersion, spec *core_v1alpha.ConfigSpec) (entity.Id, error) {
	if refs := coreutil.SecretReferences(spec); len(refs) > 0 {
		if d.Secrets == nil {
			return "", fmt.Errorf("config references secret %s but this cluster has no secret backends configured", refs[0])
		}
		if err := coreutil.PinSecrets(ctx, d.Secrets, spec); err != nil {
			return "", fmt.Errorf("failed to pin secrets: %w", err)
		}
	}

	appName := strings.TrimPrefix(string(base.App), "app/")
	cvid, err := d.EC.Create(ctx, appName+"-"+idgen.Gen("c")+"-cfg", &core_v1alpha.ConfigVersion{
		App:  base.App,
		Spec: *spec,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create derived config version: %w", err)
	}
	return cvid, nil
}

// resolveDeployConfig works out the config the named version should actually run
// with, and reports whether that differs from the version's own stored config.
//
// A version is a snapshot of one build. Re-activating an old one (deploy -V, app
// rollback) must not roll back the things that belong to the app rather than to
// that build: addon-injected bindings and operator-set variables. An addon
// injects its variables once, at provision time, by minting a *successor*
// version — so the version a failed first deploy names has never contained
// DATABASE_URL, and activating it verbatim stranded the app without a database
// permanently (MIR-1579).
//
// CLI-supplied vars are applied last, so an explicit -e/-s always wins.
func (d *DeploymentServer) resolveDeployConfig(ctx context.Context, appName string,
	target *core_v1alpha.AppVersion, envVars []*deployment_v1alpha.EnvironmentVariable) (*core_v1alpha.ConfigSpec, bool, error) {

	spec, err := coreutil.ResolveConfig(ctx, d.EAC, target)
	if err != nil {
		return nil, false, fmt.Errorf("resolving config for version %s: %w", target.Version, err)
	}

	changed := false

	if live, err := d.liveConfig(ctx, appName); err != nil {
		return nil, false, err
	} else if live != nil {
		changed = coreutil.CarryForwardVars(spec, live)
	}

	for _, ev := range envVars {
		if mergeManualVar(spec, ev) {
			changed = true
		}
	}

	return spec, changed, nil
}

// liveConfig resolves the config the app is running right now, or nil when it
// has never been deployed.
func (d *DeploymentServer) liveConfig(ctx context.Context, appName string) (*core_v1alpha.ConfigSpec, error) {
	var appRec core_v1alpha.App
	if err := d.EC.Get(ctx, appName, &appRec); err != nil {
		return nil, fmt.Errorf("getting app %s: %w", appName, err)
	}
	if appRec.ActiveVersion == "" {
		return nil, nil
	}

	var active core_v1alpha.AppVersion
	if err := d.EC.GetById(ctx, appRec.ActiveVersion, &active); err != nil {
		// A dangling active_version is not a reason to refuse the deploy; the
		// version being activated is still perfectly valid on its own.
		d.Log.Warn("could not read active version for env carry-forward",
			"app", appName, "active_version", appRec.ActiveVersion, "error", err)
		return nil, nil
	}

	spec, err := coreutil.ResolveConfig(ctx, d.EAC, &active)
	if err != nil {
		d.Log.Warn("could not resolve active config for env carry-forward",
			"app", appName, "active_version", appRec.ActiveVersion, "error", err)
		return nil, nil
	}
	return spec, nil
}

// mergeManualVar applies one CLI-supplied variable to the spec as an operator
// value, reporting whether it changed anything.
//
// An existing entry is updated field by field rather than replaced, so the
// app.toml metadata on it — Required and Description — survives a -e/-s that
// shadows a declared variable. This matches mergeIntoSpec (api/app/envvar.go)
// and the metadata carry in mergeVariablesFromAppConfig (servers/build/build.go);
// replacing the struct would blank the description in `miren env list` and make
// a declared-required variable look optional to the next build's validation.
//
// Backend comes from the incoming variable rather than the one being replaced.
// The `-e KEY=VALUE` callers never set it, so this clears a secret reference the
// key used to hold, which is what mergeCliEnvVars does and what you want: a
// literal replaces the reference outright rather than being read as a new
// reference into the backend the old value named. Carrying the incoming value
// instead of hardcoding empty means a caller that does supply a reference keeps
// it, and createConfigVersion pins it like any other.
func mergeManualVar(spec *core_v1alpha.ConfigSpec, ev *deployment_v1alpha.EnvironmentVariable) bool {
	for i, existing := range spec.Variables {
		if existing.Key != ev.Key() {
			continue
		}

		updated := existing
		updated.Value = ev.Value()
		updated.Sensitive = ev.Sensitive()
		updated.Source = coreutil.SourceManual
		updated.Backend = ev.Backend()

		if updated == existing {
			return false
		}
		spec.Variables[i] = updated
		return true
	}

	spec.Variables = append(spec.Variables, core_v1alpha.ConfigSpecVariables{
		Key:       ev.Key(),
		Value:     ev.Value(),
		Sensitive: ev.Sensitive(),
		Source:    coreutil.SourceManual,
		Backend:   ev.Backend(),
	})
	return true
}
