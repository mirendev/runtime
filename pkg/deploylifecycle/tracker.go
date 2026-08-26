package deploylifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/rpc"
)

// updateRetryLimit bounds the read-modify-write loop. A conflict means another
// writer touched the record between our read and our write, so retrying is the
// correct response; the cap only exists to stop a pathological loop.
const updateRetryLimit = 100

// Failure summaries are stored on the attempt so history can explain a failed
// deployment without fetching logs. Keep them small: detailed and potentially
// unbounded build output belongs in the log stream, not the entity record.
const maxFailureSummaryBytes = 4 * 1024
const failureSummaryEllipsis = "…"

// Tracker is the deployment lifecycle as a set of operations, and the surface
// the build paths call. It exists so the record is a byproduct of the work
// actually happening rather than something a client narrates.
//
// Every mutation goes through it, which is what makes the state machine and the
// lock unavoidable rather than merely available.
type Tracker struct {
	store *Store
	locks *Locks
	log   *slog.Logger

	// now is a test seam; the zero value means time.Now.
	now clockFn
}

// NewTracker wires a tracker over the entity store. The lock manager is given
// the store's status lookup, so an abandoned app lock can be reconciled
// against the record it names.
func NewTracker(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Tracker {
	store := NewStore(log, eac)

	return &Tracker{
		store: store,
		locks: NewLocks(log, eac, store.Status),
		log:   log.With("module", "deploylifecycle.tracker"),
	}
}

// Store exposes the underlying store for read paths (history, lock inspection)
// that do not mutate the lifecycle.
func (t *Tracker) Store() *Store { return t.store }

// Locks exposes the lock manager for read-only inspection, such as the
// pre-flight check a client makes before uploading a build context.
func (t *Tracker) Locks() *Locks { return t.locks }

// BeginParams describes a deployment about to start.
type BeginParams struct {
	AppName   string
	AppID     entity.Id
	ClusterID string
	Operation Operation

	// AppVersion is normally empty: a forward deploy does not know its version
	// until the build produces one. Rollback knows it up front.
	AppVersion string

	GitInfo    core_v1alpha.GitInfo
	DeployedBy core_v1alpha.DeployedBy
	Subject    string
	AuthMethod string

	// SourceDeploymentID records what this deployment was derived from, for
	// rollback and redeploy provenance.
	SourceDeploymentID string
}

// Begin admits a deployment under the deploy lock and publishes its record.
//
// If another live deployment holds the lock, Begin returns a *LockHeldError
// (matching errors.Is(err, ErrLockHeld)) describing the blocker, and leaves no
// deployment in history. A private reservation exists during admission so an
// older runtime cannot mistake the compatibility lock's not-yet-published owner
// for an abandoned deployment during a rolling upgrade.
func (t *Tracker) Begin(ctx context.Context, params BeginParams) (*Record, error) {
	if params.AppName == "" {
		return nil, cond.ValidationFailure("missing-field",
			"app_name is required to begin a deployment")
	}
	if params.Operation == "" {
		params.Operation = OperationBuild
	}
	autoSource := false
	if !params.Operation.Valid() {
		return nil, cond.ValidationFailure("invalid-operation", "unknown deployment operation")
	}

	var app *core_v1alpha.App
	if params.AppID == "" {
		var resolved *core_v1alpha.App
		var err error
		if params.Operation == OperationBuild {
			resolved, _, err = t.store.EnsureApp(ctx, params.AppName)
		} else {
			resolved, _, err = t.store.AppByName(ctx, params.AppName)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to resolve deployment app: %w", err)
		}
		app = resolved
		params.AppID = app.ID
	} else if params.Operation == OperationConfigChange && params.SourceDeploymentID == "" {
		resolved, _, err := t.store.AppByName(ctx, params.AppName)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve deployment app: %w", err)
		}
		if resolved.ID != params.AppID {
			return nil, cond.ValidationFailure("deployment-app-mismatch", "deployment app reference does not match app name")
		}
		app = resolved
	}
	if params.SourceDeploymentID == "" && app != nil && app.ActiveDeployment != "" {
		params.SourceDeploymentID = string(app.ActiveDeployment)
		autoSource = true
	}

	deployedBy := params.DeployedBy
	if params.Subject == "" || params.AuthMethod == "" {
		if identity := rpc.IdentityFromContext(ctx); identity != nil && identity.Method != rpc.AuthMethodAnonymous {
			if params.Subject == "" {
				params.Subject = identity.Subject
			}
			if params.AuthMethod == "" {
				params.AuthMethod = string(identity.Method)
			}
		}
	}
	if params.Subject != "" {
		deployedBy.Subject = params.Subject
	}
	if params.AuthMethod != "" {
		deployedBy.AuthMethod = params.AuthMethod
	}
	if deployedBy.Timestamp == "" {
		deployedBy.Timestamp = t.now.Now().Format(time.RFC3339)
	}
	startedAt := t.now.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, deployedBy.Timestamp); err == nil {
		startedAt = parsed.UTC()
	}
	params.GitInfo.Repository = SourceFromGitInfo(params.GitInfo).Repository

	dep := &core_v1alpha.Deployment{
		ID:         entity.Id(idgen.GenNS("deployment")),
		App:        params.AppID,
		AppName:    params.AppName,
		ClusterId:  params.ClusterID,
		Operation:  schemaOperation(params.Operation),
		StartedAt:  startedAt,
		DeployedBy: deployedBy,
		GitInfo:    params.GitInfo,
	}
	pending := &Record{Deployment: dep}
	pending.setInProgress()
	pending.setPhase(PhasePreparing)

	if params.AppVersion != "" {
		version, err := t.store.AppVersionByID(ctx, params.AppVersion)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve deployment version: %w", err)
		}
		if version.App != "" && version.App != params.AppID {
			return nil, cond.ValidationFailure("app-version-mismatch", "app version does not belong to deployment app")
		}
		pending.setVersion(version.ID)
	}
	if params.SourceDeploymentID != "" {
		source, err := t.store.Get(ctx, params.SourceDeploymentID)
		if err != nil {
			if autoSource && errors.Is(err, cond.ErrNotFound{}) {
				params.SourceDeploymentID = ""
			} else {
				return nil, fmt.Errorf("failed to resolve source deployment: %w", err)
			}
		} else {
			if (source.Canonical() && source.AppID() != params.AppID) ||
				(!source.Canonical() && source.Deployment.AppName != params.AppName) {
				return nil, cond.ValidationFailure("source-deployment-mismatch", "source deployment does not belong to deployment app")
			}
			pending.setSourceDeployment(source.Deployment.ID)
		}
	}

	deploymentID := string(dep.ID)
	reservationRevision, err := t.store.ReserveAdmission(ctx, dep.ID)
	if err != nil {
		return nil, err
	}
	discardReservation := func(reason string) {
		if deleteErr := t.store.Delete(ctx, deploymentID); deleteErr != nil {
			t.log.Error("failed to discard deployment admission reservation",
				"deployment_id", deploymentID, "reason", reason, "error", deleteErr)
		}
	}

	if _, err := t.locks.Acquire(ctx, params.AppName, deploymentID); err != nil {
		discardReservation("lock acquisition failed")
		return nil, err
	}

	rec, err := t.store.PublishAdmission(ctx, dep, reservationRevision)
	if err != nil {
		if releaseErr := t.locks.Release(ctx, params.AppName, deploymentID); releaseErr != nil {
			t.log.Error("failed to release lock after deployment admission failed", "error", releaseErr)
		}
		discardReservation("deployment publication failed")
		return nil, err
	}

	t.log.Info("began deployment",
		"deployment_id", deploymentID,
		"app", params.AppName,
		"cluster", params.ClusterID)

	return rec, nil
}

// SetPhase records fine-grained progress. Phases only mean something while a
// deployment is in flight, so setting one on a settled record is a conflict.
func (t *Tracker) SetPhase(ctx context.Context, deploymentID string, phase Phase) error {
	return t.update(ctx, deploymentID, func(rec *Record) error {
		if err := CheckPhase(rec.Status(), phase); err != nil {
			return err
		}

		rec.setPhase(phase)
		return nil
	})
}

// SetAppVersion records the version the build produced. It replaces the
// "pending-build" placeholder the client used to write at creation time.
func (t *Tracker) SetAppVersion(ctx context.Context, deploymentID, appVersionID string) error {
	if appVersionID == "" {
		return cond.ValidationFailure("missing-field", "app_version_id is required")
	}

	return t.update(ctx, deploymentID, func(rec *Record) error {
		if rec.Status() != StatusInProgress {
			return cond.Conflict("deployment-status",
				fmt.Sprintf("cannot set app version on deployment in %s state", rec.Status()))
		}

		version, err := t.store.AppVersionByID(ctx, appVersionID)
		if err != nil {
			return err
		}
		if rec.AppID() != "" && version.App != "" && version.App != rec.AppID() {
			return cond.ValidationFailure("app-version-mismatch", "app version does not belong to deployment app")
		}
		rec.setVersion(version.ID)
		return nil
	})
}

// Activate marks the deployment live and settles the one it replaced as
// succeeded. The lock is released: the deploy is over.
func (t *Tracker) Activate(ctx context.Context, deploymentID string) error {
	return t.activate(ctx, deploymentID, "", 0)
}

// ActivateAtRevision activates only if the app is still at the revision from
// which a configuration mutation was derived. A conflict lets the caller
// discard its speculative version and rebuild it from the winning state.
func (t *Tracker) ActivateAtRevision(ctx context.Context, deploymentID string, appRevision int64) error {
	return t.activate(ctx, deploymentID, "", appRevision)
}

// ActivateRollback is Activate for a rollback, which settles the deployment it
// replaced as rolled_back rather than succeeded.
func (t *Tracker) ActivateRollback(ctx context.Context, deploymentID string) error {
	return t.activate(ctx, deploymentID, StatusRolledBack, 0)
}

func (t *Tracker) activate(ctx context.Context, deploymentID string, supersede Status, appRevision int64) error {
	rec, err := t.store.Get(ctx, deploymentID)
	if err != nil {
		return err
	}
	if rec.Status() == StatusSucceeded {
		t.release(ctx, rec)
		return nil
	}
	if err := Transition(rec.Status(), StatusSucceeded); err != nil {
		return err
	}
	if rec.AppID() == "" || rec.AppVersion() == "" {
		return cond.ValidationFailure("missing-field", "cannot activate a deployment without app and version references")
	}

	// The app update is the activation. CommitActivation reads the lock and
	// serving pointers from one app revision, then guards the pointer swing with
	// that same revision. A worker that lost its lease cannot overwrite its
	// successor's lock or make itself current.
	var activationErr error
	if appRevision == 0 {
		activationErr = t.locks.CommitActivation(ctx, rec.Deployment.AppName, rec.AppID(),
			entity.Id(rec.AppVersion()), deploymentID)
	} else {
		activationErr = t.locks.CommitActivationAtRevision(ctx, rec.Deployment.AppName, rec.AppID(),
			entity.Id(rec.AppVersion()), deploymentID, appRevision)
	}
	if activationErr != nil {
		return activationErr
	}

	var settled *Record
	err = t.update(ctx, deploymentID, func(current *Record) error {
		if current.Status() == StatusSucceeded {
			settled = current
			return nil
		}
		if err := Transition(current.Status(), StatusSucceeded); err != nil {
			return err
		}
		finished := t.now.Now().UTC()
		current.setOutcome(StatusSucceeded)
		current.Deployment.FinishedAt = finished
		current.Deployment.CompletedAt = finished.Format(time.RFC3339)
		settled = current
		return nil
	})
	if err != nil {
		// The app pointers are already committed. Treat a terminal-record write as
		// repairable bookkeeping so a caller never reports a live deployment as
		// failed or tries to compensate it. Leave the app lock owned by this
		// attempt so reconciliation can settle the outcome before releasing it.
		t.log.Error("activation committed but terminal deployment write failed",
			"deployment_id", deploymentID, "error", err)
		return nil
	}
	if supersede == "" {
		supersede = StatusSucceeded
		if settled.Operation() == OperationRollback {
			supersede = StatusRolledBack
		}
	}

	// Bookkeeping past this point must not fail an activation that already
	// happened — the new version is live either way.
	if err := t.store.MarkPreviousActiveAs(ctx,
		settled.Deployment.AppName, deploymentID, supersede); err != nil {
		t.log.Error("failed to settle previously active deployments",
			"deployment_id", deploymentID, "error", err)
	}

	t.release(ctx, settled)
	return nil
}

// Fail records a failed deployment and releases the lock.
//
// A deployment that was cancelled stays cancelled: the operator's action is the
// more meaningful account of what happened, and the build failing afterwards is
// a consequence of it.
func (t *Tracker) Fail(ctx context.Context, deploymentID, failureSummary string) error {
	var settled *Record

	err := t.update(ctx, deploymentID, func(rec *Record) error {
		if rec.Status() == StatusCancelled {
			// A build failing after an operator cancelled it does not change
			// what happened: the cancellation is the real account. Keep that
			// reason rather than overwriting it with a downstream symptom.
			settled = rec
			return nil
		}

		if err := Transition(rec.Status(), StatusFailed); err != nil {
			return err
		}
		finished := t.now.Now().UTC()
		rec.setOutcome(StatusFailed)
		rec.Deployment.ErrorMessage = boundedFailureSummary(failureSummary)
		rec.Deployment.FinishedAt = finished
		rec.Deployment.CompletedAt = finished.Format(time.RFC3339)
		settled = rec
		return nil
	})
	if err != nil {
		return err
	}

	t.release(ctx, settled)
	return nil
}

// FailIfUnsettled records a failure only if the deployment has not already
// finished, reporting success either way.
//
// It exists for deferred error handlers, which fire on every exit path including
// the ones that follow a successful activation. Such a handler should not have
// to reason about the state machine: "record a failure unless the deploy already
// finished" is exactly what a defer wants, and a deployment that is already
// active, succeeded or cancelled has a better account of itself than a late
// error does.
func (t *Tracker) FailIfUnsettled(ctx context.Context, deploymentID, failureSummary string) error {
	err := t.Fail(ctx, deploymentID, failureSummary)
	if err == nil || !errors.Is(err, cond.ErrConflict{}) {
		return err
	}

	t.log.Debug("not recording failure against an already-settled deployment",
		"deployment_id", deploymentID, "error_message", boundedFailureSummary(failureSummary))
	return nil
}

// Cancel stops an in-flight deployment and releases the lock.
func (t *Tracker) Cancel(ctx context.Context, deploymentID, reason string) error {
	var settled *Record

	err := t.update(ctx, deploymentID, func(rec *Record) error {
		if err := Transition(rec.Status(), StatusCancelled); err != nil {
			return err
		}

		finished := t.now.Now().UTC()
		rec.setOutcome(StatusCancelled)
		rec.Deployment.FinishedAt = finished
		rec.Deployment.CompletedAt = finished.Format(time.RFC3339)
		if reason != "" {
			rec.Deployment.ErrorMessage = boundedFailureSummary(reason)
		}
		settled = rec
		return nil
	})
	if err != nil {
		return err
	}

	t.release(ctx, settled)
	return nil
}

func boundedFailureSummary(summary string) string {
	if len(summary) <= maxFailureSummaryBytes {
		return summary
	}

	cut := summary[:maxFailureSummaryBytes-len(failureSummaryEllipsis)]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + failureSummaryEllipsis
}

// Reconcile converges the crash window between the app pointer CAS and the
// attempt's terminal write, and marks abandoned attempts interrupted once their
// lock lease and grace period are both gone.
func (t *Tracker) Reconcile(ctx context.Context, deploymentID string) error {
	rec, err := t.store.Get(ctx, deploymentID)
	if err != nil || rec.Status() != StatusInProgress {
		return err
	}
	app, _, err := t.store.AppByName(ctx, rec.Deployment.AppName)
	if err != nil {
		return err
	}
	versionMatches := rec.AppVersion() != "" && app.ActiveVersion == entity.Id(rec.AppVersion())
	deploymentMatches := app.ActiveDeployment == rec.Deployment.ID
	if versionMatches && deploymentMatches {
		return t.settleReconciledSuccess(ctx, rec)
	}
	if versionMatches != deploymentMatches {
		t.log.Warn("deployment reconciliation found split app pointers",
			"deployment_id", deploymentID,
			"active_version", app.ActiveVersion,
			"active_deployment", app.ActiveDeployment)
		return nil
	}

	owned, err := t.locks.Owns(ctx, rec.Deployment.AppName, deploymentID)
	if err != nil || owned {
		return err
	}
	started := rec.StartedAt()
	if started.IsZero() && rec.Entity != nil && rec.Entity.HasCreatedAt() {
		started = time.UnixMilli(rec.Entity.CreatedAt())
	}
	if started.IsZero() || t.now.Now().Sub(started) < DefaultLockTTL {
		return nil
	}
	return t.interrupt(ctx, deploymentID)
}

func (t *Tracker) settleReconciledSuccess(ctx context.Context, rec *Record) error {
	var settled *Record
	if err := t.update(ctx, string(rec.Deployment.ID), func(current *Record) error {
		if current.Status() != StatusInProgress {
			settled = current
			return nil
		}
		finished := t.now.Now().UTC()
		current.setOutcome(StatusSucceeded)
		current.Deployment.FinishedAt = finished
		current.Deployment.CompletedAt = finished.Format(time.RFC3339)
		settled = current
		return nil
	}); err != nil {
		return err
	}
	supersede := StatusSucceeded
	if settled.Operation() == OperationRollback {
		supersede = StatusRolledBack
	}
	if err := t.store.MarkPreviousActiveAs(ctx,
		settled.Deployment.AppName, string(settled.Deployment.ID), supersede); err != nil {
		t.log.Error("failed to settle previously active deployments during reconciliation",
			"deployment_id", settled.Deployment.ID, "error", err)
	}
	t.release(ctx, settled)
	return nil
}

func (t *Tracker) interrupt(ctx context.Context, deploymentID string) error {
	var settled *Record
	if err := t.update(ctx, deploymentID, func(rec *Record) error {
		if rec.Status() != StatusInProgress {
			return nil
		}
		finished := t.now.Now().UTC()
		rec.setOutcome(StatusInterrupted)
		rec.Deployment.FinishedAt = finished
		rec.Deployment.CompletedAt = finished.Format(time.RFC3339)
		settled = rec
		return nil
	}); err != nil {
		return err
	}
	if settled != nil {
		t.release(ctx, settled)
	}
	return nil
}

// release drops the deploy lock for a settled deployment. A failure here is
// logged rather than returned: the deployment's own outcome is already recorded,
// and the lock will expire on its own.
func (t *Tracker) release(ctx context.Context, rec *Record) {
	err := t.locks.Release(ctx, rec.Deployment.AppName, string(rec.Deployment.ID))
	if err != nil {
		t.log.Error("failed to release deploy lock",
			"deployment_id", rec.Deployment.ID,
			"app", rec.Deployment.AppName,
			"error", err)
	}
}

// update applies mutate to a deployment record and writes it back, retrying the
// whole read-modify-write when another writer got there first.
func (t *Tracker) update(ctx context.Context, deploymentID string, mutate func(*Record) error) error {
	if deploymentID == "" {
		return cond.ValidationFailure("missing-field", "deployment_id is required")
	}

	for attempt := range updateRetryLimit {
		rec, err := t.store.Get(ctx, deploymentID)
		if err != nil {
			return err
		}

		if err := mutate(rec); err != nil {
			return err
		}

		err = t.store.Put(ctx, rec)
		if err == nil {
			return nil
		}

		if !errors.Is(err, cond.ErrConflict{}) {
			return err
		}

		// Someone else wrote first. Re-read and re-decide: the mutation may not
		// even be legal against the new state.
		t.log.Debug("retrying deployment update after conflict",
			"deployment_id", deploymentID, "attempt", attempt+1)
	}

	return fmt.Errorf("gave up updating deployment %s after %d attempts",
		deploymentID, updateRetryLimit)
}
