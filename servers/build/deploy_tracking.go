package build

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/rpc/stream"
)

// deployTracking threads the server-owned deployment record through a build.
//
// A nil *deployTracking means this build is not tracked — an ephemeral build, or
// an older client that still drives its own deployment record — and every method
// is then a no-op. Call sites therefore never branch on whether tracking is
// active; they call through unconditionally and a nil receiver does the right
// thing.
type deployTracking struct {
	tracker      *deploylifecycle.Tracker
	deploymentID string
	status       StatusSender
	log          *slog.Logger
}

// trackDeployment binds a deployment record to the build that owns it.
//
// An empty deploymentID yields nil, which is the "not tracked" case every
// method treats as a no-op, so callers never branch. Both build paths build
// their tracking through here: the plain path wraps its RPC stream, the saga
// path looks its sender up by stream ID. That shared construction is what keeps
// the two paths from drifting on phases, contexts, or failure policy.
func (b *Builder) trackDeployment(deploymentID string, status StatusSender) *deployTracking {
	if deploymentID == "" {
		return nil
	}
	if status == nil {
		status = noopStatusSender{}
	}
	return &deployTracking{
		tracker:      b.deploy,
		deploymentID: deploymentID,
		status:       status,
		log:          b.Log.With("deployment_id", deploymentID),
	}
}

// beginDeploy creates and locks a deployment record when the client asked the
// server to own it, and returns a tracker for the rest of the build.
//
// It returns nil (not an error) when the build is not server-tracked: no
// DeployRequest, or an ephemeral build, which has no deployment record by
// design. A lock already held by a live deployment is a real error and is
// returned as such — the build must not proceed alongside another one.
func (b *Builder) beginDeploy(
	ctx context.Context,
	appName string,
	req *build_v1alpha.DeployRequest,
	ephemeral *ephemeralOpts,
	status *stream.SendStreamClient[*build_v1alpha.Status],
) (*deployTracking, error) {
	if req == nil || (ephemeral != nil && ephemeral.label != "") {
		return nil, nil
	}

	clusterID := req.ClusterId()
	// An empty cluster_id means "not tracked", matching the saga path's
	// begin-deployment action (which skips on the same condition). Without this
	// the two paths would disagree: the plain path would create a lock-holding
	// record for an empty cluster_id while the saga path created none.
	if clusterID == "" {
		return nil, nil
	}

	rec, err := b.deploy.Begin(ctx, deploylifecycle.BeginParams{
		AppName:   appName,
		ClusterID: clusterID,
		GitInfo:   gitInfoFromRequest(req),
	})
	if err != nil {
		return nil, err
	}

	dt := b.trackDeployment(string(rec.Deployment.ID), NewRPCStatusSender(status, b.Log))

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

// activate marks the deployment live once the new version is running.
//
// The version is already live by the time this runs, so the settle uses a
// detached context and, if it still fails, releases the lock directly as a
// backstop — a live deploy must never leave the app stalled behind a record that
// will not settle.
func (t *deployTracking) activate(ctx context.Context) {
	if t == nil {
		return
	}

	settleCtx, cancel := settleContext(ctx)
	defer cancel()

	if err := t.tracker.Activate(settleCtx, t.deploymentID); err != nil {
		t.log.Error("failed to activate deployment; releasing lock as backstop", "error", err)
		if relErr := t.tracker.ReleaseLock(settleCtx, t.deploymentID); relErr != nil {
			t.log.Error("failed to release lock after activation error", "error", relErr)
		}
	}
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

	if err := t.tracker.FailIfUnsettled(settleCtx, t.deploymentID, retErr.Error(), ""); err != nil {
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
