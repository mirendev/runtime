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
	"miren.dev/runtime/pkg/entity"
)

// DefaultLockTTL is how long a lock survives without its holder finishing. It
// backstops a deploy whose driver died without releasing. A holder that reaches
// a terminal status is stealable sooner than this.
const DefaultLockTTL = 30 * time.Minute

// lockAcquireLimit bounds the steal/retry loop. Each iteration means another
// caller beat us to a write, so the cap is a runaway backstop rather than a
// contention budget — set high deliberately.
const lockAcquireLimit = 100

// ErrLockHeld reports that a live deployment already holds the lock. It is an
// expected outcome, not a failure — match against it with errors.Is.
var ErrLockHeld = errors.New("deployment lock held")

// LockHeldError carries the blocking holder along with the error, so a caller
// several layers up can render "who is in the way" without re-reading the lock
// and risking a different answer.
type LockHeldError struct {
	Holder *Holder
}

func (e *LockHeldError) Error() string {
	return fmt.Sprintf("deployment lock for %s held by %s",
		e.Holder.AppName, e.Holder.DeploymentID)
}

func (e *LockHeldError) Is(target error) bool {
	return target == ErrLockHeld
}

// HolderFrom extracts the blocking holder from an error returned by Acquire or
// Begin, reporting false for any other error.
func HolderFrom(err error) (*Holder, bool) {
	if held, ok := errors.AsType[*LockHeldError](err); ok {
		return held.Holder, true
	}
	return nil, false
}

// Holder describes the deployment currently holding a lock.
type Holder struct {
	AppName      string
	DeploymentID string
	AcquiredAt   time.Time
	ExpiresAt    time.Time
	Revision     int64
}

// Expired reports whether the lock has outlived its TTL.
func (h *Holder) Expired(now time.Time) bool {
	return !h.ExpiresAt.After(now)
}

// StatusLookup reports the stored status of a deployment record. Acquire uses
// it so a lock whose holder has already finished can be taken immediately
// instead of waiting out the TTL.
type StatusLookup func(ctx context.Context, deploymentID string) (Status, error)

// Locks manages the expiring deployment lock stored on an app.
//
// The app is the single compare-and-swap point for acquisition. A
// revision-guarded patch ensures that two callers racing to deploy the same app
// cannot both become its lock holder.
//
// The lock is scoped to the app, not app+cluster: a coordinator's entity store
// is a loopback into its own etcd, so it only ever holds this cluster's
// deployments, and the client-supplied cluster_id is unreliable anyway (a manual
// deploy sends the cluster name, a CI/OIDC deploy sends the raw address). Keying
// on it would let those two deploys of the same app run concurrently. See
// MIR-1465.
type Locks struct {
	eac    *entityserver_v1alpha.EntityAccessClient
	log    *slog.Logger
	ttl    time.Duration
	status StatusLookup

	// now is a test seam for expiry; the zero value means time.Now.
	now clockFn

	// sleep is a test seam for the contention backoff; nil means time.Sleep.
	sleep func(time.Duration)

	// stealRace and releaseRace are test seams for the compare-and-swap windows
	// in Acquire and Release. When non-nil they run between reading the current
	// holder and the guarded write, letting a test interleave a competing writer
	// into that gap deterministically. Both are nil in production.
	//
	// They are fields rather than package globals so concurrent tests cannot
	// scribble on each other's seam, matching now and sleep above.
	stealRace   func()
	releaseRace func()
}

// lockRetryBackoff is the base delay between contended acquire attempts. Each
// retry means another caller won a write, so a brief pause keeps a burst of
// contenders from hammering the entity store; it is capped low because a deploy
// lock is rarely contended and we don't want to add real latency to the rare
// case that is.
const lockRetryBackoff = 5 * time.Millisecond

// lockRetryBackoffMax caps the linear growth so a pathological loop still yields
// promptly.
const lockRetryBackoffMax = 100 * time.Millisecond

// backoff pauses before a contended retry. attempt is 0-based; the first retry
// waits one base interval. The initial acquire (attempt 0 in Acquire's loop is
// the first try, which never calls this) pays no delay.
func (l *Locks) backoff(attempt int) {
	d := min(time.Duration(attempt)*lockRetryBackoff, lockRetryBackoffMax)
	if d <= 0 {
		return
	}
	if l.sleep != nil {
		l.sleep(d)
		return
	}
	time.Sleep(d)
}

// clockFn is the test seam shared by the types in this package. A nil clockFn
// reads the real clock, so the zero value is always usable.
type clockFn func() time.Time

func (c clockFn) Now() time.Time {
	if c == nil {
		return time.Now()
	}
	return c()
}

// NewLocks builds a lock manager. status may be nil, in which case a held lock
// is only stealable once expired.
func NewLocks(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient, status StatusLookup) *Locks {
	return &Locks{
		eac:    eac,
		log:    log.With("module", "deploylifecycle.locks"),
		ttl:    DefaultLockTTL,
		status: status,
	}
}

// Acquire takes the deploy lock for deploymentID, returning the holder it
// established.
//
// If another live deployment holds it, Acquire returns that holder along with
// ErrLockHeld. Re-acquiring a lock this same deployment already holds succeeds
// and refreshes the expiry, so a retried call is not an error.
func (l *Locks) Acquire(ctx context.Context, appName, deploymentID string) (*Holder, error) {
	if appName == "" || deploymentID == "" {
		return nil, cond.ValidationFailure("missing-field",
			"app_name and deployment_id are both required to acquire a deploy lock")
	}

	// Take the downgrade-compatible shadow first. An older binary only knows
	// about that entity, so holding it closes the cross-version admission race
	// before we touch the canonical App lock.
	legacy, shadowCreated, err := l.acquireLegacy(ctx, appName, deploymentID)
	if err != nil {
		return legacy, err
	}

	holder, err := l.acquireApp(ctx, appName, deploymentID)
	if err != nil && shadowCreated {
		// We introduced this shadow during the failed attempt, so unwind it. A
		// shadow that already belonged to deploymentID may protect an existing
		// worker retrying this call and must stay in place.
		if releaseErr := l.releaseLegacy(ctx, appName, deploymentID); releaseErr != nil {
			l.log.Error("failed to release compatibility lock after app lock acquisition failed",
				"app", appName, "deployment_id", deploymentID, "error", releaseErr)
		}
	}
	return holder, err
}

func (l *Locks) acquireApp(ctx context.Context, appName, deploymentID string) (*Holder, error) {

	for attempt := range lockAcquireLimit {
		mine := l.holderFor(appName, deploymentID)
		current, app, err := l.readApp(ctx, appName)
		if err != nil {
			return nil, fmt.Errorf("failed to acquire deploy lock: %w", err)
		}

		if current.DeploymentID == deploymentID {
			// Refresh extends the authority boundary without rewriting when the
			// lock originally began.
			mine.AcquiredAt = current.AcquiredAt
			refreshed, err := l.replace(ctx, app.ID, current.Revision, mine)
			if err == nil {
				return refreshed, nil
			}
			if errors.Is(err, cond.ErrConflict{}) {
				l.backoff(attempt + 1)
				continue
			}
			return nil, err
		}

		reason := "unclaimed"
		if current.DeploymentID != "" {
			stealable, why := l.stealable(ctx, current)
			if !stealable {
				return current, &LockHeldError{Holder: current}
			}
			reason = why
		}

		if current.DeploymentID != "" {
			l.log.Warn("stealing deploy lock",
				"app", appName,
				"from_deployment_id", current.DeploymentID,
				"to_deployment_id", deploymentID,
				"reason", reason)
		}

		if l.stealRace != nil {
			l.stealRace()
		}

		taken, err := l.replace(ctx, app.ID, current.Revision, mine)
		if err != nil {
			if errors.Is(err, cond.ErrConflict{}) {
				// Another contender moved the lock first. Re-read and decide
				// again rather than assuming we lost outright.
				l.backoff(attempt + 1)
				continue
			}
			return nil, err
		}
		l.log.Debug("acquired deploy lock",
			"app", appName, "deployment_id", deploymentID)
		return taken, nil
	}

	return nil, fmt.Errorf("gave up acquiring deploy lock for %s after %d attempts",
		appName, lockAcquireLimit)
}

// stealable reports whether a held lock may be taken over, and why.
func (l *Locks) stealable(ctx context.Context, h *Holder) (bool, string) {
	if h.Expired(l.now.Now()) {
		return true, "expired"
	}

	if h.DeploymentID == "" {
		// A lock with no owner cannot be reconciled against a record. Treat it
		// as debris rather than an indefinite block.
		return true, "no owner recorded"
	}

	if l.status == nil {
		return false, ""
	}

	status, err := l.status(ctx, h.DeploymentID)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// Absence is not positive evidence that the holder is dead. Direct lock
			// callers may still be publishing their record, so the lease remains the
			// recovery bound when there is nothing to reconcile.
			return false, ""
		}
		l.log.Warn("could not read holding deployment; treating lock as held",
			"deployment_id", h.DeploymentID, "error", err)
		return false, ""
	}

	if status.Terminal() {
		return true, "holding deployment already " + status.String()
	}

	return false, ""
}

// Owns reports whether deploymentID is the current, unexpired holder.
func (l *Locks) Owns(ctx context.Context, appName, deploymentID string) (bool, error) {
	holder, err := l.Get(ctx, appName)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return false, nil
		}
		return false, err
	}
	return holder.DeploymentID == deploymentID && !holder.Expired(l.now.Now()), nil
}

// Release clears the canonical App lock, then its downgrade-compatible shadow,
// but only if deploymentID still holds them. Releasing in this order keeps old
// binaries excluded until the canonical lock is safely clear.
func (l *Locks) Release(ctx context.Context, appName, deploymentID string) error {
	if err := l.releaseApp(ctx, appName, deploymentID); err != nil {
		return err
	}
	return l.releaseLegacy(ctx, appName, deploymentID)
}

// releaseApp clears the app lock, but only if deploymentID still holds it.
//
// The revision guard matters because another deployment may legitimately take
// over between our read and write. App revisions can also move for unrelated
// changes, so a conflict is re-read rather than assumed to mean the lock moved.
func (l *Locks) releaseApp(ctx context.Context, appName, deploymentID string) error {
	for attempt := range lockAcquireLimit {
		current, app, err := l.readApp(ctx, appName)
		if err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				return nil
			}
			return err
		}

		if current.DeploymentID == "" {
			return nil
		}
		if current.DeploymentID != deploymentID {
			l.log.Debug("not releasing deploy lock held by another deployment",
				"app", appName,
				"holder", current.DeploymentID, "caller", deploymentID)
			return nil
		}

		if l.releaseRace != nil {
			l.releaseRace()
		}

		if _, err := l.replace(ctx, app.ID, current.Revision, &Holder{AppName: appName}); err != nil {
			if errors.Is(err, cond.ErrConflict{}) {
				// App revisions also move for unrelated app changes. Re-read so we
				// can distinguish that from a successor taking over the lock.
				l.backoff(attempt + 1)
				continue
			}
			return fmt.Errorf("failed to release deploy lock: %w", err)
		}

		l.log.Debug("released deploy lock",
			"app", appName, "deployment_id", deploymentID)
		return nil
	}

	return fmt.Errorf("gave up releasing deploy lock for %s after %d attempts",
		appName, lockAcquireLimit)
}

// Blocking returns the holder that would block a new deployment for this app, or
// nil if nothing would.
//
// This is the question callers actually have — a pre-flight check before
// uploading a build context, or the lock info rendered in a "deployment
// blocked" message. An empty lock, an expired lock, and a lock whose
// deployment already finished all read as free.
func (l *Locks) Blocking(ctx context.Context, appName string) (*Holder, error) {
	legacy, err := l.getLegacy(ctx, appName)
	if err == nil {
		if stealable, _ := l.stealable(ctx, legacy); !stealable {
			return legacy, nil
		}
	} else if !errors.Is(err, cond.ErrNotFound{}) {
		return nil, err
	}

	current, err := l.Get(ctx, appName)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil, nil
		}
		return nil, err
	}

	if stealable, _ := l.stealable(ctx, current); stealable {
		return nil, nil
	}

	return current, nil
}

// Get returns the current holder, or cond.ErrNotFound if the app is unlocked.
func (l *Locks) Get(ctx context.Context, appName string) (*Holder, error) {
	holder, _, err := l.readApp(ctx, appName)
	if err != nil {
		return nil, err
	}
	if holder.DeploymentID == "" {
		return nil, cond.NotFound("deployment lock", appName)
	}
	return holder, nil
}

func (l *Locks) readApp(ctx context.Context, appName string) (*Holder, *core_v1alpha.App, error) {
	res, err := l.eac.Get(ctx, "app/"+appName)
	if err != nil {
		return nil, nil, err
	}

	var app core_v1alpha.App
	app.Decode(&entityAttrs{entity: res.Entity()})
	lock := app.DeploymentLock
	return &Holder{
		AppName:      appName,
		DeploymentID: lock.DeploymentId,
		AcquiredAt:   lock.AcquiredAt,
		ExpiresAt:    lock.ExpiresAt,
		Revision:     res.Entity().Revision(),
	}, &app, nil
}

func (l *Locks) holderFor(appName, deploymentID string) *Holder {
	now := l.now.Now()
	return &Holder{
		AppName:      appName,
		DeploymentID: deploymentID,
		AcquiredAt:   now,
		ExpiresAt:    now.Add(l.ttl),
	}
}

// replace updates the app's lock under a revision guard, so two callers that
// both judged it available cannot both succeed. An empty holder writes an empty
// component, which decodes as no lock while preserving patch semantics for
// unrelated app attributes.
func (l *Locks) replace(ctx context.Context, appID entity.Id, revision int64, h *Holder) (*Holder, error) {
	lock := core_v1alpha.DeploymentLock{
		DeploymentId: h.DeploymentID,
		AcquiredAt:   h.AcquiredAt,
		ExpiresAt:    h.ExpiresAt,
	}
	res, err := l.eac.Patch(ctx, []entity.Attr{
		entity.Ref(entity.DBId, appID),
		entity.Component(core_v1alpha.AppDeploymentLockId, lock.Encode()),
	}, revision)
	if err != nil {
		return nil, err
	}

	taken := *h
	taken.Revision = res.Revision()
	return &taken, nil
}

// CommitActivation swings the serving pointers while the same app revision
// still proves that deploymentID owns the lock. The lock deliberately stays
// in place until the attempt's terminal outcome is durable. That preserves a
// recovery witness if the process dies between these two entity writes.
func (l *Locks) CommitActivation(ctx context.Context, appName string, appID, versionID entity.Id, deploymentID string) error {
	return l.commitActivation(ctx, appName, appID, versionID, deploymentID, 0)
}

// CommitActivationAtRevision preserves optimistic concurrency for a version
// derived from a particular app snapshot. Unlike a regular deployment, a
// configuration mutation must be rebuilt if that snapshot has moved.
func (l *Locks) CommitActivationAtRevision(ctx context.Context, appName string, appID, versionID entity.Id,
	deploymentID string, expectedRevision int64) error {
	return l.commitActivation(ctx, appName, appID, versionID, deploymentID, expectedRevision)
}

func (l *Locks) commitActivation(ctx context.Context, appName string, appID, versionID entity.Id,
	deploymentID string, expectedRevision int64) error {
	for attempt := range updateRetryLimit {
		current, app, err := l.readApp(ctx, appName)
		if err != nil {
			return err
		}
		if app.ID != appID {
			return cond.Conflict("deployment-app", "deployment app reference no longer matches app name")
		}
		if app.ActiveVersion == versionID && app.ActiveDeployment == entity.Id(deploymentID) {
			return nil
		}
		if expectedRevision != 0 && current.Revision != expectedRevision {
			return cond.Conflict("app-revision", "app changed after deployment version was derived")
		}
		if current.DeploymentID != deploymentID || current.Expired(l.now.Now()) {
			return cond.Conflict("deployment-lock", "deployment no longer owns its app lock")
		}

		_, err = l.eac.Patch(ctx, []entity.Attr{
			entity.Ref(entity.DBId, app.ID),
			entity.Ref(core_v1alpha.AppActiveVersionId, versionID),
			entity.Ref(core_v1alpha.AppActiveDeploymentId, entity.Id(deploymentID)),
		}, current.Revision)
		if err == nil {
			return nil
		}
		if !errors.Is(err, cond.ErrConflict{}) {
			return fmt.Errorf("failed to update active deployment pointers: %w", err)
		}
		if expectedRevision != 0 {
			return err
		}
		l.backoff(attempt + 1)
	}

	return fmt.Errorf("gave up activating deployment %s after %d attempts",
		deploymentID, updateRetryLimit)
}

// entityAttrs adapts an RPC entity to the AttrGetter the generated decoders want.
type entityAttrs struct {
	entity *entityserver_v1alpha.Entity
}

func (e *entityAttrs) Get(id entity.Id) (entity.Attr, bool) {
	if id == entity.DBId {
		return entity.Ref(entity.DBId, entity.Id(e.entity.Id())), true
	}

	for _, attr := range e.entity.Attrs() {
		if attr.ID == id {
			return attr, true
		}
	}
	return entity.Attr{}, false
}

func (e *entityAttrs) GetAll(id entity.Id) []entity.Attr {
	var out []entity.Attr
	for _, attr := range e.entity.Attrs() {
		if attr.ID == id {
			out = append(out, attr)
		}
	}
	return out
}

func (e *entityAttrs) Attrs() []entity.Attr {
	return e.entity.Attrs()
}
