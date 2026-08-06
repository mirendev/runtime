package deploylifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
)

// updateRetryLimit bounds the read-modify-write loop. A conflict means another
// writer touched the record between our read and our write, so retrying is the
// correct response; the cap only exists to stop a pathological loop.
const updateRetryLimit = 100

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
// the store's status lookup, so an abandoned lock can be reconciled against the
// record it claims to be holding for.
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
	ClusterID string

	// AppVersion is normally empty: a forward deploy does not know its version
	// until the build produces one. Rollback knows it up front.
	AppVersion string

	GitInfo    core_v1alpha.GitInfo
	DeployedBy core_v1alpha.DeployedBy

	// SourceDeploymentID records what this deployment was derived from, for
	// rollback and redeploy provenance.
	SourceDeploymentID string
}

// Begin creates the deployment record and takes the deploy lock for it.
//
// If another live deployment holds the lock, Begin returns a *LockHeldError
// (matching errors.Is(err, ErrLockHeld)) describing the blocker, and leaves no
// record behind.
func (t *Tracker) Begin(ctx context.Context, params BeginParams) (*Record, error) {
	if params.AppName == "" || params.ClusterID == "" {
		return nil, cond.ValidationFailure("missing-field",
			"app_name and cluster_id are required to begin a deployment")
	}

	deployedBy := params.DeployedBy
	if deployedBy.Timestamp == "" {
		deployedBy.Timestamp = t.now.Now().Format(time.RFC3339)
	}

	rec, err := t.store.Create(ctx, &core_v1alpha.Deployment{
		AppName:            params.AppName,
		ClusterId:          params.ClusterID,
		AppVersion:         params.AppVersion,
		Status:             string(StatusInProgress),
		Phase:              string(PhasePreparing),
		DeployedBy:         deployedBy,
		GitInfo:            params.GitInfo,
		SourceDeploymentId: params.SourceDeploymentID,
	})
	if err != nil {
		return nil, err
	}

	deploymentID := string(rec.Deployment.ID)

	if _, err := t.locks.Acquire(ctx, params.AppName, deploymentID); err != nil {
		// We hold no lock, so this record describes a deploy that will never
		// run. Leaving it would show up in history as a deployment stuck
		// in_progress forever.
		t.discard(ctx, rec)
		return nil, err
	}

	t.log.Info("began deployment",
		"deployment_id", deploymentID,
		"app", params.AppName,
		"cluster", params.ClusterID)

	return rec, nil
}

// discard removes a record that never became a real deployment.
func (t *Tracker) discard(ctx context.Context, rec *Record) {
	if err := t.store.Delete(ctx, string(rec.Deployment.ID)); err != nil {
		t.log.Error("failed to discard deployment record that never acquired its lock",
			"deployment_id", rec.Deployment.ID, "error", err)
	}
}

// SetPhase records fine-grained progress. Phases only mean something while a
// deployment is in flight, so setting one on a settled record is a conflict.
func (t *Tracker) SetPhase(ctx context.Context, deploymentID string, phase Phase) error {
	return t.update(ctx, deploymentID, func(rec *Record) error {
		if err := CheckPhase(rec.Status(), phase); err != nil {
			return err
		}

		rec.Deployment.Phase = string(phase)
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

		rec.Deployment.AppVersion = appVersionID
		return nil
	})
}

// Activate marks the deployment live and settles the one it replaced as
// succeeded. The lock is released: the deploy is over.
func (t *Tracker) Activate(ctx context.Context, deploymentID string) error {
	return t.activate(ctx, deploymentID, StatusSucceeded)
}

// ActivateRollback is Activate for a rollback, which settles the deployment it
// replaced as rolled_back rather than succeeded.
func (t *Tracker) ActivateRollback(ctx context.Context, deploymentID string) error {
	return t.activate(ctx, deploymentID, StatusRolledBack)
}

func (t *Tracker) activate(ctx context.Context, deploymentID string, supersede Status) error {
	var settled *Record

	err := t.update(ctx, deploymentID, func(rec *Record) error {
		// The version is required here rather than at creation: this is the
		// point where a deployment claims to be running a specific image.
		if rec.AppVersion() == "" {
			return cond.ValidationFailure("missing-field",
				"cannot activate a deployment with no app version")
		}

		if err := Transition(rec.Status(), StatusActive); err != nil {
			return err
		}

		rec.Deployment.Status = string(StatusActive)
		rec.Deployment.CompletedAt = t.now.Now().Format(time.RFC3339)
		settled = rec
		return nil
	})
	if err != nil {
		return err
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
func (t *Tracker) Fail(ctx context.Context, deploymentID, errorMessage, buildLogs string) error {
	var settled *Record

	err := t.update(ctx, deploymentID, func(rec *Record) error {
		if rec.Status() == StatusCancelled {
			// A build failing after an operator cancelled it does not change
			// what happened: the cancellation is the real account. Keep that
			// reason and preserve the logs for diagnosis rather than
			// overwriting the message with a downstream symptom.
			rec.Deployment.BuildLogs = buildLogs
			settled = rec
			return nil
		}

		if err := Transition(rec.Status(), StatusFailed); err != nil {
			return err
		}
		rec.Deployment.Status = string(StatusFailed)
		rec.Deployment.ErrorMessage = errorMessage
		rec.Deployment.BuildLogs = buildLogs
		rec.Deployment.CompletedAt = t.now.Now().Format(time.RFC3339)
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
func (t *Tracker) FailIfUnsettled(ctx context.Context, deploymentID, errorMessage, buildLogs string) error {
	err := t.Fail(ctx, deploymentID, errorMessage, buildLogs)
	if err == nil || !errors.Is(err, cond.ErrConflict{}) {
		return err
	}

	t.log.Debug("not recording failure against an already-settled deployment",
		"deployment_id", deploymentID, "error_message", errorMessage)
	return nil
}

// Cancel stops an in-flight deployment and releases the lock.
func (t *Tracker) Cancel(ctx context.Context, deploymentID, reason string) error {
	var settled *Record

	err := t.update(ctx, deploymentID, func(rec *Record) error {
		if err := Transition(rec.Status(), StatusCancelled); err != nil {
			return err
		}

		rec.Deployment.Status = string(StatusCancelled)
		rec.Deployment.CompletedAt = t.now.Now().Format(time.RFC3339)
		if reason != "" {
			rec.Deployment.ErrorMessage = reason
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

// ReleaseLock frees the deploy lock held by a deployment without changing the
// record, looking the app+cluster up from the record itself.
//
// It is a backstop for a caller whose activation already made the version live
// but whose record settle failed: releasing the lock keeps a record that will
// never settle from stalling every later deploy of that app+cluster for the
// full lock TTL. Release is a no-op if a newer deployment already holds the lock.
func (t *Tracker) ReleaseLock(ctx context.Context, deploymentID string) error {
	rec, err := t.store.Get(ctx, deploymentID)
	if err != nil {
		return err
	}
	return t.locks.Release(ctx, rec.Deployment.AppName, deploymentID)
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
