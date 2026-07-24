package deploylifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
)

// DefaultLockTTL is how long a lock survives without its holder finishing. It
// backstops a deploy whose driver died without releasing; a holder that reaches
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
	var held *LockHeldError
	if errors.As(err, &held) {
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

// Held reports whether this holder still owns the lock: a released tombstone has
// no owner, and an expired lock no longer protects anything.
//
// It deliberately says nothing about whether the holding deployment is still
// alive — that needs the record, so use Locks.Blocking for the full answer.
func (h *Holder) Held(now time.Time) bool {
	return h.DeploymentID != "" && !h.Expired(now)
}

// StatusLookup reports the stored status of a deployment record. Acquire uses it
// so a lock whose holder has already finished — the record says failed, the lock
// says running — can be taken immediately instead of waiting out the TTL.
type StatusLookup func(ctx context.Context, deploymentID string) (Status, error)

// Locks manages the deploy lock entity for an app.
//
// Acquisition is a compare-and-create against the entity store, so two callers
// racing to start a deploy cannot both win. That is the whole point of the type:
// the previous scheme listed in-progress deployments and then created a record,
// which admitted an interleaving where both callers saw an empty list.
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
	d := time.Duration(attempt) * lockRetryBackoff
	if d > lockRetryBackoffMax {
		d = lockRetryBackoffMax
	}
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

// WithTTL returns a copy holding locks for d instead of DefaultLockTTL.
func (l *Locks) WithTTL(d time.Duration) *Locks {
	clone := *l
	clone.ttl = d
	return &clone
}

// LockID is the deterministic entity id for an app's deploy lock. Determinism is
// what makes create-if-absent a mutual exclusion primitive: every contender
// computes the same key.
func LockID(appName string) entity.Id {
	return entity.Id(fmt.Sprintf("deploy-lock/%s", sanitizeKeyPart(appName)))
}

// sanitizeKeyPart keeps a slash in a name from splitting the key into a
// different shape.
func sanitizeKeyPart(s string) string {
	return strings.ReplaceAll(s, "/", "_")
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

	id := LockID(appName)

	for attempt := 0; attempt < lockAcquireLimit; attempt++ {
		mine := l.holderFor(appName, deploymentID)

		res, err := l.eac.Ensure(ctx, l.encode(id, mine))
		if err != nil {
			return nil, fmt.Errorf("failed to acquire deploy lock: %w", err)
		}

		if res.Created() {
			mine.Revision = res.Revision()
			l.log.Debug("acquired deploy lock",
				"app", appName, "deployment_id", deploymentID)
			return mine, nil
		}

		// Someone holds it. Whether we may take it over depends on who they are
		// and whether they are still alive.
		current, err := l.Get(ctx, appName)
		if err != nil {
			if errors.Is(err, cond.ErrNotFound{}) {
				// Released between our Ensure and our read — try again.
				l.backoff(attempt + 1)
				continue
			}
			return nil, err
		}

		if current.DeploymentID == deploymentID {
			return l.refresh(ctx, id, current, mine)
		}

		stealable, reason := l.stealable(ctx, current)
		if !stealable {
			return current, &LockHeldError{Holder: current}
		}

		l.log.Warn("stealing deploy lock",
			"app", appName,
			"from_deployment_id", current.DeploymentID,
			"to_deployment_id", deploymentID,
			"reason", reason)

		if stealRaceHook != nil {
			stealRaceHook()
		}

		taken, err := l.replace(ctx, id, current.Revision, mine)
		if err != nil {
			if errors.Is(err, cond.ErrConflict{}) {
				// Another contender moved the lock first. Re-read and decide
				// again rather than assuming we lost outright.
				l.backoff(attempt + 1)
				continue
			}
			return nil, err
		}
		return taken, nil
	}

	return nil, fmt.Errorf("gave up acquiring deploy lock for %s after %d attempts",
		appName, lockAcquireLimit)
}

// refresh extends an expiry for the deployment that already holds the lock.
func (l *Locks) refresh(ctx context.Context, id entity.Id, current, mine *Holder) (*Holder, error) {
	refreshed, err := l.replace(ctx, id, current.Revision, mine)
	if err != nil {
		if errors.Is(err, cond.ErrConflict{}) {
			// Someone rewrote the lock under us. current is still ours by
			// deployment id, so report it rather than failing the deploy.
			return current, nil
		}
		return nil, err
	}
	return refreshed, nil
}

// stealable reports whether a held lock may be taken over, and why.
func (l *Locks) stealable(ctx context.Context, h *Holder) (bool, string) {
	if h.Expired(l.now.Now()) {
		return true, "expired"
	}

	if h.DeploymentID == "" {
		// A lock with no owner cannot be reconciled against a record; treat it
		// as debris rather than an indefinite block.
		return true, "no owner recorded"
	}

	if l.status == nil {
		return false, ""
	}

	status, err := l.status(ctx, h.DeploymentID)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			// The record is gone, so nothing will ever release this lock.
			return true, "holding deployment no longer exists"
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

// stealRaceHook is a test seam. When non-nil, Acquire invokes it after deciding
// a held lock is stealable but before the guarded replace, letting a test
// interleave a concurrent steal so the replace loses its compare-and-swap. It is
// always nil in production.
var stealRaceHook func()

// releaseRaceHook is a test seam. When non-nil, Release invokes it after reading
// the holder but before writing the release, letting a white-box test
// deterministically interleave a concurrent steal into that window. It is always
// nil in production. Mirrors deleteEntityRaceHook in pkg/entity/store.go.
var releaseRaceHook func()

// Release drops the lock, but only if deploymentID still holds it.
//
// The release is a revision-guarded write rather than a delete: between reading
// the holder and acting on it, another deployment may legitimately steal the
// lock — a deployment that has just finished is exactly what makes a lock
// stealable — and an unguarded delete would then remove the successor's lock,
// letting a third deployment start alongside it. The entity server's delete
// takes no caller revision, so the write has to be a Replace to be safe.
//
// The released lock is left behind as a tombstone: owned by nobody, already
// expired. Acquire treats that as free.
func (l *Locks) Release(ctx context.Context, appName, deploymentID string) error {
	current, err := l.Get(ctx, appName)
	if err != nil {
		if errors.Is(err, cond.ErrNotFound{}) {
			return nil
		}
		return err
	}

	if current.DeploymentID != deploymentID {
		l.log.Debug("not releasing deploy lock held by another deployment",
			"app", appName,
			"holder", current.DeploymentID, "caller", deploymentID)
		return nil
	}

	if releaseRaceHook != nil {
		releaseRaceHook()
	}

	// No DeploymentID: the lock is owned by nobody once released.
	released := &Holder{
		AppName:    appName,
		AcquiredAt: current.AcquiredAt,
		ExpiresAt:  l.now.Now(),
	}

	if _, err := l.replace(ctx, LockID(appName), current.Revision, released); err != nil {
		if errors.Is(err, cond.ErrConflict{}) {
			// The lock moved between our read and our write, so it is no longer
			// ours to release. Whoever holds it now keeps it.
			l.log.Debug("deploy lock moved before release; leaving it alone",
				"app", appName, "caller", deploymentID)
			return nil
		}
		return fmt.Errorf("failed to release deploy lock: %w", err)
	}

	l.log.Debug("released deploy lock",
		"app", appName, "deployment_id", deploymentID)
	return nil
}

// Blocking returns the holder that would block a new deployment for this app, or
// nil if nothing would.
//
// This is the question callers actually have — a pre-flight check before
// uploading a build context, or the lock info rendered in a "deployment
// blocked" message — and it is not the same as "a lock entity exists". A
// released tombstone, an expired lock, and a lock whose deployment already
// finished all read as free.
func (l *Locks) Blocking(ctx context.Context, appName string) (*Holder, error) {
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

// Get returns the current holder, or cond.ErrNotFound if the lock is free.
func (l *Locks) Get(ctx context.Context, appName string) (*Holder, error) {
	id := LockID(appName)

	res, err := l.eac.Get(ctx, string(id))
	if err != nil {
		return nil, err
	}

	var stored core_v1alpha.DeploymentLock
	stored.Decode(&entityAttrs{entity: res.Entity()})

	return &Holder{
		AppName:      stored.AppName,
		DeploymentID: stored.DeploymentId,
		AcquiredAt:   stored.AcquiredAt,
		ExpiresAt:    stored.ExpiresAt,
		Revision:     res.Entity().Revision(),
	}, nil
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

func (l *Locks) encode(id entity.Id, h *Holder) []entity.Attr {
	stored := &core_v1alpha.DeploymentLock{
		AppName:      h.AppName,
		DeploymentId: h.DeploymentID,
		AcquiredAt:   h.AcquiredAt,
		ExpiresAt:    h.ExpiresAt,
	}

	return append(stored.Encode(), entity.Ref(entity.DBId, id))
}

// replace takes over the lock entity under a revision guard, so two callers that
// both judged the lock stealable cannot both succeed.
func (l *Locks) replace(ctx context.Context, id entity.Id, revision int64, h *Holder) (*Holder, error) {
	res, err := l.eac.Replace(ctx, l.encode(id, h), revision)
	if err != nil {
		return nil, err
	}

	taken := *h
	taken.Revision = res.Revision()
	return &taken, nil
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
