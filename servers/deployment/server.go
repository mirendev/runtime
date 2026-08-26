package deployment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appclient "miren.dev/runtime/api/app"
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
		info.SetCurrentPhase(string(rec.Phase()))
		info.SetBlockingDeploymentShortId(shortIDFromRPCEntity(rec.Entity))

		if email := rec.Deployment.DeployedBy.UserEmail; email != "" &&
			email != "unknown@example.com" && email != "user@example.com" {
			displayEmail = email
		}
		if ts := rec.StartedAt(); !ts.IsZero() {
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
	appVersionID := args.AppVersionId()
	canonicalVersionID := ""
	var appVersion core_v1alpha.AppVersion
	switch err := d.getAppVersion(ctx, appVersionID, &appVersion); {
	case err == nil:
		if verifyErr := d.verifyVersionOwnedByApp(ctx, &appVersion, appName); verifyErr != nil {
			return verifyErr
		}
		appVersionID = string(appVersion.ID)
		canonicalVersionID = appVersionID
	case !errors.Is(err, cond.ErrNotFound{}):
		d.Log.Error("Failed to look up app version", "app_version_id", appVersionID, "error", err)
		return cond.Error("failed to look up app version")
	}

	var gitInfo core_v1alpha.GitInfo
	if args.HasGitInfo() {
		gitInfo = gitInfoFromRPC(args.GitInfo())
	}

	// Begin admits the deployment under the app-scoped lock. The "pending-build"
	// sentinel an older CLI passes is normalized away because app_version is
	// optional until the build produces one.
	rec, err := d.tracker.Begin(ctx, deploylifecycle.BeginParams{
		AppName:    appName,
		ClusterID:  clusterId,
		Operation:  deploylifecycle.OperationBuild,
		AppVersion: canonicalVersionID,
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

	status, err := deploylifecycle.ParseStatus(newStatus)
	if err != nil {
		return err
	}
	if status == deploylifecycle.StatusInterrupted {
		return cond.ValidationFailure("invalid-status", "interrupted is reserved for deployment reconciliation")
	}

	rec, err := d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}
	if !rpc.AllowApp(ctx, rec.Deployment.AppName) {
		return rpc.AppAccessError(ctx, rec.Deployment.AppName)
	}
	switch status {
	case deploylifecycle.StatusInProgress:
		// Older clients reassert running after creation. Lifecycle progress and
		// lock renewal are server-owned now, so a valid self-transition remains an
		// intentionally idempotent no-op during the compatibility window.
		err = deploylifecycle.Transition(rec.Status(), deploylifecycle.StatusInProgress)
	case deploylifecycle.StatusActive, deploylifecycle.StatusSucceeded, deploylifecycle.StatusRolledBack:
		err = d.tracker.Activate(ctx, deploymentId)
	case deploylifecycle.StatusFailed:
		message := ""
		if args.HasErrorMessage() {
			message = args.ErrorMessage()
		}
		err = d.tracker.Fail(ctx, deploymentId, message)
	case deploylifecycle.StatusCancelled:
		err = d.tracker.Cancel(ctx, deploymentId, "")
	case deploylifecycle.StatusInterrupted:
		return cond.ValidationFailure("invalid-status", "interrupted is reserved for deployment reconciliation")
	}
	if err != nil {
		return err
	}
	rec, err = d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		return err
	}
	deploymentInfo := d.toDeploymentInfo(rec.Deployment)
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

	rec, err := d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}

	if !rpc.AllowApp(ctx, rec.Deployment.AppName) {
		return rpc.AppAccessError(ctx, rec.Deployment.AppName)
	}
	if err := d.tracker.SetPhase(ctx, deploymentId, deploylifecycle.Phase(newPhase)); err != nil {
		return err
	}
	rec, err = d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		return err
	}
	deploymentInfo := d.toDeploymentInfo(rec.Deployment)
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

	if args.HasErrorMessage() {
		errorMessage = args.ErrorMessage()
	}

	rec, err := d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}

	if !rpc.AllowApp(ctx, rec.Deployment.AppName) {
		return rpc.AppAccessError(ctx, rec.Deployment.AppName)
	}
	// build_logs remains in the deprecated RPC request for old clients, but the
	// complete build stream belongs in VictoriaLogs rather than this entity.
	if err := d.tracker.Fail(ctx, deploymentId, errorMessage); err != nil {
		return err
	}
	rec, err = d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		return err
	}
	deploymentInfo := d.toDeploymentInfo(rec.Deployment)
	results.SetDeployment(deploymentInfo)

	d.Log.Info("Updated failed deployment",
		"deployment_id", deploymentId,
		"app_version", rec.AppVersion())

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
		versionIDs = append(versionIDs, (&deploylifecycle.Record{Deployment: dwe.deployment}).AppVersion())
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

	rec, err := d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}

	if !rpc.AllowApp(ctx, rec.Deployment.AppName) {
		return rpc.AppAccessError(ctx, rec.Deployment.AppName)
	}
	deploymentInfo := d.toDeploymentInfo(rec.Deployment)
	versionShortIDs := d.resolveShortIDs(ctx, []string{rec.AppVersion()})
	enrichDeploymentShortIDs(deploymentInfo, rec.Entity, versionShortIDs)
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

	rec, err := d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		d.Log.Error("Failed to get deployment", "deployment_id", deploymentId, "error", err)
		return cond.NotFound("deployment", deploymentId)
	}

	if !rpc.AllowApp(ctx, rec.Deployment.AppName) {
		return rpc.AppAccessError(ctx, rec.Deployment.AppName)
	}

	// If the new version resolves to a real AppVersion, it must belong to the
	// deployment's app — otherwise a caller authorized for its own app could
	// repoint a deployment at another app's built version. A version string that
	// doesn't resolve to an entity is only a recorded label (the deprecated CLI
	// records one before any build exists), so it is left alone rather than
	// rejected; the cross-app attack always names a real, existing version. Any
	// other lookup error fails closed.
	var appVersion core_v1alpha.AppVersion
	switch err := d.getAppVersion(ctx, appVersionId, &appVersion); {
	case err == nil:
		if verifyErr := d.verifyVersionOwnedByApp(ctx, &appVersion, rec.Deployment.AppName); verifyErr != nil {
			return verifyErr
		}
		appVersionId = string(appVersion.ID)
	case !errors.Is(err, cond.ErrNotFound{}):
		d.Log.Error("Failed to look up app version", "app_version_id", appVersionId, "error", err)
		return cond.Error("failed to look up app version")
	}

	if err := d.tracker.SetAppVersion(ctx, deploymentId, appVersionId); err != nil {
		return err
	}
	rec, err = d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		return err
	}
	deploymentInfo := d.toDeploymentInfo(rec.Deployment)
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

	app, _, err := d.tracker.Store().AppByName(ctx, appName)
	if err != nil && !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	if err == nil && app.ActiveDeployment != "" {
		rec, err := d.tracker.Store().Get(ctx, string(app.ActiveDeployment))
		if err != nil {
			return err
		}
		deploymentInfo := d.toDeploymentInfo(rec.Deployment)
		versionShortIDs := d.resolveShortIDs(ctx, []string{rec.AppVersion()})
		enrichDeploymentShortIDs(deploymentInfo, rec.Entity, versionShortIDs)
		results.SetDeployment(deploymentInfo)
		return nil
	}

	// Before the app-pointer migration reaches this app, serve the one retained
	// legacy active row. Canonical writers always take the branch above.
	legacy, err := d.listDeploymentsInternal(ctx, appName, "active", 1)
	if err != nil {
		return err
	}
	if len(legacy) == 0 {
		return cond.NotFound("active-deployment", fmt.Sprintf("%s/%s", appName, clusterId))
	}
	dwe := legacy[0]
	deploymentInfo := d.toDeploymentInfo(dwe.deployment)
	versionShortIDs := d.resolveShortIDs(ctx, []string{(&deploylifecycle.Record{Deployment: dwe.deployment}).AppVersion()})
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

	rec, err := d.tracker.Store().Get(ctx, deploymentId)
	if err != nil {
		return cond.Error("failed to get deployment")
	}
	if rec.Status() != deploylifecycle.StatusInProgress {
		results.SetError(fmt.Sprintf("deployment is not in progress (status: %s)", rec.Status()))
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
	requestedVersionId := args.AppVersionId()
	appVersionId := requestedVersionId
	isRollback := args.HasIsRollback() && args.IsRollback()

	// Enforce app scoping: scoped callers (e.g. OIDC) can only deploy their bound app
	if !rpc.AllowApp(ctx, appName) {
		return rpc.AppAccessError(ctx, appName)
	}

	// Verify the AppVersion entity exists
	var appVersion core_v1alpha.AppVersion
	if err := d.getAppVersion(ctx, appVersionId, &appVersion); err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			results.SetError(fmt.Sprintf("app version %q not found", appVersionId))
			return nil
		}
		d.Log.Error("Failed to look up app version", "app_version_id", appVersionId, "error", err)
		results.SetError("failed to look up app version")
		return nil
	}
	appVersionId = string(appVersion.ID)
	sourceVersionIds := map[string]struct{}{
		requestedVersionId: {},
		appVersionId:       {},
		appVersion.Version: {},
	}

	// The version must belong to the app being deployed — AllowApp only
	// confined the target app, not the source version.
	if err := d.verifyVersionOwnedByApp(ctx, &appVersion, appName); err != nil {
		return err
	}
	if _, _, err := d.tracker.Store().AppByName(ctx, appName); err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			results.SetError(fmt.Sprintf("app %q not found", appName))
			return nil
		}
		d.Log.Error("Failed to look up app", "app", appName, "error", err)
		results.SetError("failed to look up app")
		return nil
	}
	isEphemeral := args.HasEphemeralLabel() && args.EphemeralLabel() != ""

	// Ephemeral versions do not participate in deployment attempts, so their
	// optional derivation remains wholly inside the ephemeral path.
	if isEphemeral && args.HasEnvVars() && len(args.EnvVars()) > 0 {
		derivedVersion, err := d.createDerivedVersion(ctx, &appVersion, args.EnvVars())
		if err != nil {
			d.Log.Error("Failed to create derived version with env vars", "error", err)
			results.SetError(fmt.Sprintf("failed to apply env vars: %v", err))
			return nil
		}
		appVersion = *derivedVersion
		appVersionId = string(derivedVersion.ID)
		d.Log.Info("Created derived version with env vars",
			"original", args.AppVersionId(), "derived", appVersionId,
			"env_var_count", len(args.EnvVars()))
	}

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

	// Find the source deployment — the most recent deployment with this
	// app_version_id — for git info and rollback provenance.
	var gitInfo core_v1alpha.GitInfo
	var sourceDeploymentID string
	if allDeployments, listErr := d.listDeploymentsInternal(ctx, appName, "", 100); listErr != nil {
		d.Log.Error("Failed to list deployments for source lookup", "error", listErr)
	} else {
		for _, dwe := range allDeployments {
			if _, ok := sourceVersionIds[(&deploylifecycle.Record{Deployment: dwe.deployment}).AppVersion()]; ok {
				gitInfo = dwe.deployment.GitInfo
				sourceDeploymentID = string(dwe.deployment.ID)
				break // listDeploymentsInternal returns newest first
			}
		}
	}

	// Begin creates the record and takes the deploy lock atomically, contending
	// with any server-owned build for the same app+cluster.
	operation := deploylifecycle.OperationRedeploy
	if isRollback {
		operation = deploylifecycle.OperationRollback
	}
	rec, err := d.tracker.Begin(ctx, deploylifecycle.BeginParams{
		AppName:            appName,
		ClusterID:          clusterId,
		Operation:          operation,
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
	if args.HasEnvVars() && len(args.EnvVars()) > 0 {
		derivedVersion, deriveErr := d.createDerivedVersion(ctx, &appVersion, args.EnvVars())
		if deriveErr != nil {
			_ = d.tracker.FailIfUnsettled(context.WithoutCancel(ctx), newDeploymentId, deriveErr.Error())
			results.SetError(fmt.Sprintf("failed to apply env vars: %v", deriveErr))
			return nil
		}
		appVersion = *derivedVersion
		appVersionId = string(derivedVersion.ID)
		if err := d.tracker.SetAppVersion(ctx, newDeploymentId, appVersionId); err != nil {
			_ = d.tracker.FailIfUnsettled(context.WithoutCancel(ctx), newDeploymentId, err.Error())
			results.SetError(fmt.Sprintf("failed to record derived version: %v", err))
			return nil
		}
	}

	// Every terminal settle below runs on a detached context: once we start
	// settling, a client that cancels the RPC must not strand the record
	// in_progress with the lock held until its TTL. The most likely moment for
	// that cancellation is right here, in the failure path.
	settleCtx, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancelSettle()

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

	activate := d.tracker.Activate
	if isRollback {
		activate = d.tracker.ActivateRollback
	}
	if err := activate(settleCtx, newDeploymentId); err != nil {
		d.Log.Error("Failed to activate deployment", "error", err)
		if failErr := d.tracker.Fail(settleCtx, newDeploymentId, err.Error()); failErr != nil {
			d.Log.Error("Failed to settle activation failure", "error", failErr)
		}
		results.SetError(fmt.Sprintf("failed to activate version: %v", err))
		return nil
	}

	// Re-read so the response reflects the settled state.
	if settled, getErr := d.tracker.Store().Get(settleCtx, newDeploymentId); getErr == nil {
		deployment = settled.Deployment
		rec = settled
	}

	deploymentInfo := d.toDeploymentInfo(deployment)
	versionShortIDs := d.resolveShortIDs(ctx, []string{rec.AppVersion()})
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

	rec, err := d.tracker.Begin(ctx, deploylifecycle.BeginParams{
		AppName: appName, ClusterID: clusterId, Operation: deploylifecycle.OperationConfigChange,
	})
	if err != nil {
		if holder, ok := deploylifecycle.HolderFrom(err); ok {
			d.reportLockBlocked(ctx, results, holder)
			return nil
		}
		return err
	}
	attemptID := string(rec.Deployment.ID)
	activate := d.configActivator(attemptID)
	mutResult, err := appclient.SetEnvVarsWithActivator(ctx, d.EC, d.Secrets, appName, nil, vars, service, activate)
	if err != nil {
		_ = d.tracker.FailIfUnsettled(context.WithoutCancel(ctx), attemptID, err.Error())
		d.Log.Error("Failed to set env vars", "error", err, "app", appName)
		results.SetError(fmt.Sprintf("failed to set env vars: %v", err))
		return nil
	}

	results.SetVersionId(mutResult.VersionID)

	// Create deployment record and handle lock/activation (shared with DeployVersion)
	deployErr := d.createEnvVarDeployment(ctx, appName, clusterId, mutResult, rec, results)
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

	rec, err := d.tracker.Begin(ctx, deploylifecycle.BeginParams{
		AppName: appName, ClusterID: clusterId, Operation: deploylifecycle.OperationConfigChange,
	})
	if err != nil {
		if holder, ok := deploylifecycle.HolderFrom(err); ok {
			d.reportLockBlocked(ctx, results, holder)
			return nil
		}
		return err
	}
	attemptID := string(rec.Deployment.ID)
	delResult, err := appclient.DeleteEnvVarsWithActivator(ctx, d.EC, d.Secrets, appName, nil, args.Keys(), service, d.configActivator(attemptID))
	if err != nil {
		_ = d.tracker.FailIfUnsettled(context.WithoutCancel(ctx), attemptID, err.Error())
		d.Log.Error("Failed to delete env vars", "error", err, "app", appName)
		results.SetError(fmt.Sprintf("failed to delete env vars: %v", err))
		return nil
	}

	results.SetVersionId(delResult.VersionID)
	deletedSources := delResult.DeletedSources
	results.SetDeletedSources(&deletedSources)

	// Create deployment record and handle lock/activation
	deployErr := d.createEnvVarDeployment(ctx, appName, clusterId, &delResult.MutateResult, rec, results)
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
	mutResult *appclient.MutateResult, rec *deploylifecycle.Record, results envVarDeployResults) error {

	appVersionId := mutResult.VersionID
	if mutResult.AppVersion != nil && mutResult.AppVersion.ID != "" {
		appVersionId = string(mutResult.AppVersion.ID)
	}

	newDeploymentId := string(rec.Deployment.ID)

	// The custom env-var activator already committed both app pointers and the
	// outcome. Re-read on a detached context so the response survives a client
	// cancellation racing the final store read.
	settleCtx, cancelSettle := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancelSettle()
	deployment := rec.Deployment
	if settled, getErr := d.tracker.Store().Get(settleCtx, newDeploymentId); getErr == nil {
		deployment = settled.Deployment
		rec = settled
	}

	deploymentInfo := d.toDeploymentInfo(deployment)
	versionShortIDs := d.resolveShortIDs(ctx, []string{rec.AppVersion()})
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

func (d *DeploymentServer) configActivator(attemptID string) appclient.VersionActivator {
	return func(ctx context.Context, _ *core_v1alpha.App, revision int64, version entity.Id) error {
		if err := d.tracker.SetAppVersion(ctx, attemptID, string(version)); err != nil {
			return err
		}
		if err := d.tracker.SetPhase(ctx, attemptID, deploylifecycle.PhaseActivating); err != nil {
			return err
		}
		return d.tracker.ActivateAtRevision(ctx, attemptID, revision)
	}
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
	query := deploylifecycle.Query{AppName: appName, Limit: limit}
	if status == string(deploylifecycle.StatusActive) {
		query.LegacyStatus = deploylifecycle.StatusActive
	} else {
		query.Status = deploylifecycle.Status(status)
	}
	records, err := d.tracker.Store().List(ctx, query)
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
	rec := &deploylifecycle.Record{Deployment: deployment}

	info.SetId(string(deployment.ID))
	info.SetAppName(deployment.AppName)
	// Normalize legacy placeholder versions to empty so history renders "—"
	// rather than "pending-build" or "failed-<id>".
	info.SetAppVersionId(rec.AppVersion())
	info.SetClusterId(deployment.ClusterId)
	status := rec.Status()
	// Keep the old RPC vocabulary: a succeeded attempt that currently owns the
	// app pointer is presented as active to downgrade-era clients.
	if status == deploylifecycle.StatusSucceeded {
		status = deploylifecycle.Status(deployment.Status)
		if status == "" {
			status = deploylifecycle.StatusSucceeded
		}
	}
	info.SetStatus(string(status))
	info.SetPhase(string(rec.Phase()))
	info.SetDeployedByUserId(deployment.DeployedBy.UserId)
	info.SetDeployedByUserName(deployment.DeployedBy.UserName)
	info.SetDeployedByUserEmail(deployment.DeployedBy.UserEmail)

	// Parse timestamps
	if deployedAt := rec.StartedAt(); !deployedAt.IsZero() {
		info.SetDeployedAt(standard.ToTimestamp(deployedAt))
	}
	if completedAt := rec.FinishedAt(); !completedAt.IsZero() {
		info.SetCompletedAt(standard.ToTimestamp(completedAt))
	}

	// Add error information if available
	if deployment.ErrorMessage != "" {
		info.SetErrorMessage(deployment.ErrorMessage)
	}

	// Add source deployment ID if available (rollback/redeploy provenance)
	if sourceID := rec.SourceDeploymentID(); sourceID != "" {
		info.SetSourceDeploymentId(sourceID)
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
		versionID, err := appVersionRefID(id)
		if err != nil {
			continue
		}
		resp, err := d.EAC.Get(ctx, string(versionID))
		if err != nil {
			continue
		}
		if sid := shortIDFromRPCEntity(resp.Entity()); sid != "" {
			result[id] = sid
		}
	}
	return result
}

// appVersionRefID turns every supported app-version reference into the entity
// ID shape expected by EntityAccess. Deployment records historically contain a
// mix of canonical IDs and bare version names, while operators may also supply
// a short ID. EntityAccess resolves the latter once it is scoped to the kind.
func appVersionRefID(ref string) (entity.Id, error) {
	const prefix = "app_version/"

	if ref == "" {
		return "", cond.ValidationFailure("missing-field", "app_version_id is required")
	}
	if strings.Contains(ref, "/") {
		if !strings.HasPrefix(ref, prefix) || len(ref) == len(prefix) {
			return "", cond.ValidationFailure("invalid-app-version-id",
				fmt.Sprintf("invalid app version reference %q", ref))
		}
		return entity.Id(ref), nil
	}
	return entity.Id(prefix + ref), nil
}

func (d *DeploymentServer) getAppVersion(ctx context.Context, ref string, version *core_v1alpha.AppVersion) error {
	id, err := appVersionRefID(ref)
	if err != nil {
		return err
	}
	return d.EC.GetById(ctx, id, version)
}

type deploymentWithEntity struct {
	deployment *core_v1alpha.Deployment
	entity     *entityserver_v1alpha.Entity
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

// createDerivedVersion clones an existing AppVersion with CLI env vars merged
// into its config, creates the new entity, and returns it.
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
		Source:         base.Source,
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
