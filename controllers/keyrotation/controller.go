// Package keyrotation drives cluster-key rotation for the in-cluster secret
// store.
//
// Rotation is three steps with a strict order, because getting it wrong loses
// data rather than failing loudly:
//
//  1. Mint a key and persist the ring. Nothing may be sealed with a key that is
//     not yet on disk — a crash in that window leaves stored versions naming a
//     key the cluster no longer has, and nothing can recover them.
//  2. Re-wrap every version off the old key. Only the wrapped data key moves;
//     the ciphertext is never rewritten, so this costs a few dozen bytes per
//     version however large the secret is.
//  3. Retire the old key, once nothing references it. Retiring early is the
//     same data loss as step 1 in reverse.
//
// The in-flight state lives in a key_rotation entity so a restart resumes
// rather than restarting, and so the age-based trigger and `miren secret
// rotate-key` share one path.
package keyrotation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/secret/cluster"
	"miren.dev/runtime/pkg/secret/keyring"
)

// rewrapBatch bounds how many versions move per pass. The backfill is not
// urgent — the old key stays in the ring throughout, so everything remains
// readable — and a bounded batch keeps a rotation on a large store from
// monopolizing the entity server.
const rewrapBatch = 200

// startupDelay keeps a coordinator that is crash-looping from re-entering the
// backfill on every boot, while still resuming an interrupted rotation in
// seconds rather than at the next tick.
const startupDelay = 15 * time.Second

// Config tunes when a cluster rotates on its own.
type Config struct {
	// CheckInterval is how often the current key's age is examined.
	CheckInterval time.Duration

	// MaxKeyAge is how old the current key may get before rotation starts.
	// Zero disables the automatic trigger, leaving rotation to the operator.
	MaxKeyAge time.Duration
}

// DefaultConfig returns the built-in rotation policy.
func DefaultConfig() Config {
	return Config{
		CheckInterval: time.Hour,
		MaxKeyAge:     90 * 24 * time.Hour,
	}
}

// Controller advances key rotations and starts them when the current key ages
// out.
type Controller struct {
	Log      *slog.Logger
	EC       *entityserver.Client
	Backend  *cluster.Backend
	DataPath string
	Config   Config

	stop context.CancelFunc

	// nudge wakes the loop between ticks. An operator rotating during an
	// incident wants the old key gone, and waiting out a tick interval to see
	// the backfill even start is the wrong answer.
	nudge chan struct{}
}

// Start begins the rotation loop.
func (c *Controller) Start(ctx context.Context) {
	if c.nudge == nil {
		c.nudge = make(chan struct{}, 1)
	}
	ctx, c.stop = context.WithCancel(ctx)
	go c.run(ctx)
}

// Stop ends the rotation loop.
func (c *Controller) Stop() {
	if c.stop != nil {
		c.stop()
	}
}

func (c *Controller) run(ctx context.Context) {
	ticker := time.NewTicker(c.Config.CheckInterval)
	defer ticker.Stop()

	// Pick up a rotation left in flight by a restart. Waiting for the first
	// tick would stall a half-finished rotation for the whole interval, and a
	// restart mid-rotation is far more likely than the crash loop the delay
	// guards against. It sits in the same select as everything else so an
	// operator rotating during the startup window is not made to wait it out.
	startup := time.After(startupDelay)

	for {
		select {
		case <-startup:
			c.drain(ctx)
		case <-ticker.C:
			c.tick(ctx)
		case <-c.nudge:
			c.drain(ctx)
		case <-ctx.Done():
			c.Log.Info("key rotation controller stopped")
			return
		}
	}
}

// drain advances a rotation until it finishes. A rotation is several passes of
// rewrapping, and an operator watching `miren secret keyring` should see it
// move rather than stall between hourly ticks.
func (c *Controller) drain(ctx context.Context) {
	for c.tickHasWork(ctx) {
		if ctx.Err() != nil {
			return
		}
	}
}

// tickHasWork advances a rotation and reports whether one is still in flight.
func (c *Controller) tickHasWork(ctx context.Context) bool {
	c.tick(ctx)

	active, err := c.activeRotation(ctx)
	if err != nil {
		c.Log.Error("failed to check rotation progress", "error", err)
		return false
	}
	return active != nil
}

// tick advances whatever rotation is in flight, or starts one if the current
// key has aged out.
func (c *Controller) tick(ctx context.Context) {
	active, err := c.activeRotation(ctx)
	if err != nil {
		c.Log.Error("failed to look for an in-flight key rotation", "error", err)
		return
	}

	if active != nil {
		if err := c.advance(ctx, active); err != nil {
			c.Log.Error("key rotation failed to advance",
				"rotation", active.ID, "from_key", active.FromKey, "error", err)
		}
		return
	}

	due, err := c.rotationDue()
	if err != nil {
		c.Log.Error("failed to check the cluster key's age", "error", err)
		return
	}
	if !due {
		return
	}

	if err := c.begin(ctx); err != nil {
		c.Log.Error("failed to begin key rotation", "error", err)
	}
}

// rotationDue reports whether the current key has aged past the policy.
func (c *Controller) rotationDue() (bool, error) {
	if c.Config.MaxKeyAge <= 0 {
		return false, nil
	}

	current, err := c.Backend.Keyring().Current()
	if err != nil {
		return false, err
	}
	return current.Age(time.Now()) >= c.Config.MaxKeyAge, nil
}

// begin mints the new key, persists it, and records the rotation.
//
// The order here is the load-bearing part. The ring reaches disk before it
// reaches the backend, so no value is ever sealed with a key that would vanish
// on a crash. The entity is written last, so the worst a crash leaves behind is
// a ring with an unused extra key — harmless, and the next tick starts a
// rotation that adopts it.
func (c *Controller) begin(ctx context.Context) error {
	old := c.Backend.Keyring()
	oldID := old.CurrentID()

	rotated, newKey, err := old.Rotate()
	if err != nil {
		return fmt.Errorf("minting a new cluster key: %w", err)
	}

	if err := keyring.Save(keyring.Path(c.DataPath), rotated); err != nil {
		return fmt.Errorf("persisting the rotated keyring: %w", err)
	}

	c.Backend.UseKeyring(rotated)

	rec := &core_v1alpha.KeyRotation{
		FromKey: oldID,
		ToKey:   newKey.ID,
		Status:  core_v1alpha.REWRAPPING,
	}
	if _, err := c.EC.Create(ctx, idgen.GenNS("keyrot"), rec); err != nil {
		return fmt.Errorf("recording the rotation: %w", err)
	}

	c.Log.Info("began cluster key rotation", "from_key", oldID, "to_key", newKey.ID)
	return nil
}

// advance moves an in-flight rotation one step.
func (c *Controller) advance(ctx context.Context, rec *core_v1alpha.KeyRotation) error {
	switch rec.Status {
	case core_v1alpha.REWRAPPING:
		return c.rewrap(ctx, rec)
	case core_v1alpha.RETIRING:
		return c.retire(ctx, rec)
	case core_v1alpha.DONE, core_v1alpha.FAILED:
		// Terminal. activeRotation only ever hands back the two live states, so
		// reaching here means the record settled between that query and this
		// call — nothing left to advance.
	}
	return nil
}

// rewrap moves a batch of versions off the retiring key.
func (c *Controller) rewrap(ctx context.Context, rec *core_v1alpha.KeyRotation) error {
	moved, err := c.Backend.RewrapBatch(ctx, rec.FromKey, rewrapBatch)
	if moved > 0 {
		rec.Rewrapped += int64(moved)
		if uerr := c.EC.UpdateAttrs(ctx, rec.ID,
			entity.Int(core_v1alpha.KeyRotationRewrappedId, int(rec.Rewrapped)),
		); uerr != nil {
			c.Log.Warn("failed to record rewrap progress", "rotation", rec.ID, "error", uerr)
		}
	}
	if err != nil {
		return err
	}

	if moved == rewrapBatch {
		c.Log.Info("rewrapping secret versions onto the new cluster key",
			"from_key", rec.FromKey, "rewrapped", rec.Rewrapped)
		return nil
	}

	// The batch came up short, so the query is drained. Confirm against the
	// index rather than trusting that, since a version written during the pass
	// would still be on the old key.
	remaining, err := c.Backend.CountOnKey(ctx, rec.FromKey)
	if err != nil {
		return err
	}
	if remaining > 0 {
		return nil
	}

	if err := c.EC.UpdateAttrs(ctx, rec.ID,
		entity.Ref(core_v1alpha.KeyRotationStatusId, core_v1alpha.KeyRotationStatusRetiringId),
	); err != nil {
		return fmt.Errorf("marking rotation ready to retire: %w", err)
	}

	c.Log.Info("finished rewrapping onto the new cluster key",
		"from_key", rec.FromKey, "rewrapped", rec.Rewrapped)
	return nil
}

// retire drops the old key once nothing references it.
//
// The count is re-read here rather than inherited from the rewrap step. That
// step can be arbitrarily old by now, and a key retired while a version still
// names it makes that version permanently unreadable — the one failure in this
// package with no recovery.
func (c *Controller) retire(ctx context.Context, rec *core_v1alpha.KeyRotation) error {
	remaining, err := c.Backend.CountOnKey(ctx, rec.FromKey)
	if err != nil {
		return err
	}
	if remaining > 0 {
		c.Log.Info("key still in use, returning to rewrap",
			"from_key", rec.FromKey, "remaining", remaining)
		return c.EC.UpdateAttrs(ctx, rec.ID,
			entity.Ref(core_v1alpha.KeyRotationStatusId, core_v1alpha.KeyRotationStatusRewrappingId),
		)
	}

	retired, err := c.Backend.Keyring().Retire(rec.FromKey)
	if err != nil {
		// Already gone is a success: a previous attempt got here and only the
		// status update was lost.
		c.Log.Info("retiring key is no longer in the ring, treating as done",
			"from_key", rec.FromKey, "error", err)
		return c.finish(ctx, rec)
	}

	if err := keyring.Save(keyring.Path(c.DataPath), retired); err != nil {
		return fmt.Errorf("persisting the retired keyring: %w", err)
	}
	c.Backend.UseKeyring(retired)

	c.Log.Info("retired the previous cluster key",
		"from_key", rec.FromKey, "to_key", rec.ToKey, "rewrapped", rec.Rewrapped)
	return c.finish(ctx, rec)
}

func (c *Controller) finish(ctx context.Context, rec *core_v1alpha.KeyRotation) error {
	return c.EC.UpdateAttrs(ctx, rec.ID,
		entity.Ref(core_v1alpha.KeyRotationStatusId, core_v1alpha.KeyRotationStatusDoneId),
	)
}

// activeRotation returns the rotation still in flight, if any.
func (c *Controller) activeRotation(ctx context.Context) (*core_v1alpha.KeyRotation, error) {
	for _, status := range []entity.Id{
		core_v1alpha.KeyRotationStatusRewrappingId,
		core_v1alpha.KeyRotationStatusRetiringId,
	} {
		res, err := c.EC.List(ctx, entity.Ref(core_v1alpha.KeyRotationStatusId, status))
		if err != nil {
			return nil, err
		}
		for res.Next() {
			var rec core_v1alpha.KeyRotation
			if err := res.Read(&rec); err != nil {
				return nil, err
			}
			rec.ID = res.Entity().Id()
			return &rec, nil
		}
	}
	return nil, nil
}

// Begin starts a rotation now, for an operator who does not want to wait for
// the age policy — which is every incident.
//
// It refuses while one is in flight rather than stacking, since two rotations
// would each be trying to retire a key the other still needs.
func (c *Controller) Begin(ctx context.Context) error {
	active, err := c.activeRotation(ctx)
	if err != nil {
		return err
	}
	if active != nil {
		return fmt.Errorf("a key rotation is already in progress (from %s, %d versions rewrapped)",
			active.FromKey, active.Rewrapped)
	}

	if err := c.begin(ctx); err != nil {
		return err
	}

	// Hand the backfill to the loop rather than running it here, so the caller
	// gets its answer immediately and one goroutine still owns the ring. The
	// send is non-blocking because a nudge already pending does the same job.
	select {
	case c.nudge <- struct{}{}:
	default:
	}
	return nil
}
