package build

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

// deployTracking threads the server-owned deployment record through a build.
//
// A nil *deployTracking means this is an ephemeral build (or recovery of an old
// saga that predates server-owned attempts). Every method is then a no-op.
type deployTracking struct {
	tracker      *deploylifecycle.Tracker
	eac          *entityserver_v1alpha.EntityAccessClient
	deploymentID string
	source       core_v1alpha.Source
	status       StatusSender
	log          *slog.Logger
}

// trackDeployment binds a deployment record to the build that owns it.
//
// An empty deploymentID yields nil, which every method treats as a no-op, so
// ephemeral and pre-upgrade saga paths need no branches at later call sites.
// Both build paths construct tracking here, keeping their phases, contexts, and
// failure policy aligned.
func (b *Builder) trackDeployment(deploymentID string, status StatusSender) *deployTracking {
	if deploymentID == "" {
		return nil
	}
	if status == nil {
		status = noopStatusSender{}
	}
	return &deployTracking{
		tracker:      b.deploy,
		eac:          b.EAS,
		deploymentID: deploymentID,
		status:       status,
		log:          b.Log.With("deployment_id", deploymentID),
	}
}

func (t *deployTracking) setSource(version *core_v1alpha.AppVersion) {
	if t != nil {
		version.Source = t.source
	}
}

// beginDeploy creates and locks an attempt for every non-ephemeral build. A
// lock already held by a live deployment is a real error: the build must not
// proceed alongside another one.
func (b *Builder) beginDeploy(
	ctx context.Context,
	appName string,
	req *build_v1alpha.DeployRequest,
	ephemeral *ephemeralOpts,
	status *stream.SendStreamClient[*build_v1alpha.Status],
) (*deployTracking, error) {
	if ephemeral != nil && ephemeral.label != "" {
		return nil, nil
	}

	app, err := b.ensureApp(ctx, appName)
	if err != nil {
		return nil, err
	}
	clusterID := ""
	if req != nil {
		clusterID = req.ClusterId()
	}
	gitInfo := gitInfoFromRequest(req)

	rec, err := b.deploy.Begin(ctx, deploylifecycle.BeginParams{
		AppName:   appName,
		AppID:     app.ID,
		ClusterID: clusterID,
		Operation: deploylifecycle.OperationBuild,
		GitInfo:   gitInfo,
	})
	if err != nil {
		return nil, err
	}

	dt := b.trackDeployment(string(rec.Deployment.ID), NewRPCStatusSender(status, b.Log))
	dt.source = deploylifecycle.SourceFromGitInfo(gitInfo)
	if work := deploymentContextFrom(ctx); work != nil {
		work.attach(dt)
	}

	// Announce the record and its opening phase so the client can display and
	// cancel a deployment it did not create.
	dt.emit(string(deploylifecycle.PhasePreparing))
	return dt, nil
}

// setPhase advances the record's phase and tells the client about it.
func (t *deployTracking) setPhase(ctx context.Context, phase deploylifecycle.Phase) {
	if t == nil {
		return
	}

	if err := t.tracker.SetPhase(ctx, t.deploymentID, phase); err != nil {
		t.log.Error("failed to set deployment phase", "phase", phase, "error", err)
		return
	}
	t.emit(string(phase))
}

// settleTimeout bounds the detached settle operations below.
const settleTimeout = 15 * time.Second

// settleContext detaches from ctx's cancellation. Recording the committed state
// of a deploy — the version, the activation, a failure — must complete even when
// the build's context has been cancelled by a client disconnect; otherwise the
// record is stranded in_progress with the deploy lock held until it expires.
func settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
}

// setAppVersion records the version the build produced.
func (t *deployTracking) setAppVersion(ctx context.Context, appVersionID string) {
	if t == nil {
		return
	}

	settleCtx, cancel := settleContext(ctx)
	defer cancel()

	if err := t.tracker.SetAppVersion(settleCtx, t.deploymentID, appVersionID); err != nil {
		t.log.Error("failed to record deployment app version",
			"app_version_id", appVersionID, "error", err)
	}
}

// activate swings the app's version and deployment pointers together, then
// records the successful outcome. The detached context lets that commit finish
// after the initiating client disconnects.
func (t *deployTracking) activate(ctx context.Context) error {
	if t == nil {
		return nil
	}

	settleCtx, cancel := settleContext(ctx)
	defer cancel()

	if err := t.tracker.Activate(settleCtx, t.deploymentID); err != nil {
		return err
	}
	return nil
}

// failOnError settles the deployment as failed when a build returned an error,
// and does nothing when it succeeded. It is the deferred finalizer for the build
// entry points: pass it the method's returned error and it decides.
//
// It runs its settle on a detached context, because the most common failure is
// the client disconnecting, which cancels the build's context — the very moment
// we most need the record to reach a terminal state and release the lock.
func (t *deployTracking) failOnError(ctx context.Context, retErr error) {
	if t == nil || retErr == nil {
		return
	}

	settleCtx, cancel := settleContext(ctx)
	defer cancel()

	if err := t.tracker.FailIfUnsettled(settleCtx, t.deploymentID, retErr.Error()); err != nil {
		t.log.Error("failed to record deployment failure", "error", err)
	}
}

// emit sends the deployment progress arm on the build status stream. A send
// failure is not fatal to the build: the record is authoritative, the stream is
// only a view of it, and the sender swallows its own send errors.
func (t *deployTracking) emit(phase string) {
	if t == nil || t.status == nil {
		return
	}
	t.status.SendDeployment(t.deploymentID, phase)
}

// marshalDeployGitInfo serializes the request's git info to JSON for seeding
// through the saga's string-keyed input map. Returns "" when there is none.
func marshalDeployGitInfo(req *build_v1alpha.DeployRequest) string {
	info := gitInfoFromRequest(req)
	if info == (core_v1alpha.GitInfo{}) {
		return ""
	}
	data, err := json.Marshal(info)
	if err != nil {
		return ""
	}
	return string(data)
}

// gitInfoFromRequest converts the build-service git shape into the core entity
// shape stored on the deployment record.
func gitInfoFromRequest(req *build_v1alpha.DeployRequest) core_v1alpha.GitInfo {
	if req == nil || !req.HasGitInfo() || req.GitInfo() == nil {
		return core_v1alpha.GitInfo{}
	}

	gi := req.GitInfo()

	info := core_v1alpha.GitInfo{
		Sha:               gi.Sha(),
		Branch:            gi.Branch(),
		Repository:        gi.Repository(),
		IsDirty:           gi.IsDirty(),
		WorkingTreeHash:   gi.WorkingTreeHash(),
		Message:           gi.CommitMessage(),
		Author:            gi.CommitAuthorName(),
		CommitAuthorEmail: gi.CommitAuthorEmail(),
	}

	if gi.HasCommitTimestamp() && gi.CommitTimestamp() != nil {
		info.CommitTimestamp = standard.FromTimestamp(gi.CommitTimestamp()).Format(time.RFC3339)
	}

	return info
}
