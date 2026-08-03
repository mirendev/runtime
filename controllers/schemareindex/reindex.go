// Package schemareindex runs the automatic reindex that follows an index
// schema change, as a paced background job rather than a startup step.
//
// Reindexing used to run inline during coordinator startup under the shared
// maintenance deadline. A store large enough to exceed that deadline could
// never converge: the new schema hash is only recorded after a complete pass,
// and each restart began the scan again from the first entity, so the same
// prefix was reindexed forever and the tail was never reached. Running here
// instead, with a cursor persisted between bounded passes, means progress
// accumulates until the scan genuinely finishes — and it converges without
// needing a restart at all, which a cursor alone would not give us.
package schemareindex

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/entity"
)

// Config tunes the background reindex. Defaults are gentle: this is
// convergence insurance running behind a correct write path, not a foreground
// repair, so it trades wall-clock for staying out of the way.
type Config struct {
	// IdleInterval is how long to wait between checks when the store already
	// matches the current index schema and there is nothing to do.
	IdleInterval time.Duration
	// ActiveInterval is how long to wait between passes while a reindex is in
	// flight. Much shorter than IdleInterval so a pending reindex drains at a
	// reasonable clip instead of one pass per idle check.
	ActiveInterval time.Duration
	// MaxEntitiesPerPass caps how many entities one pass processes before it
	// checkpoints and yields. Zero means unbounded, which defeats the purpose
	// here; the controller substitutes the default.
	MaxEntitiesPerPass int
	// BatchPause is slept periodically during a pass to rate-limit write
	// pressure. Zero disables pacing.
	BatchPause time.Duration
	// PassTimeout bounds a single pass. A pass cut short still checkpoints its
	// cursor, so the next one picks up where it stopped.
	PassTimeout time.Duration
}

// DefaultConfig returns the default (gentle) configuration.
func DefaultConfig() Config {
	return Config{
		IdleInterval:       1 * time.Hour,
		ActiveInterval:     10 * time.Second,
		MaxEntitiesPerPass: 2000,
		BatchPause:         100 * time.Millisecond,
		PassTimeout:        5 * time.Minute,
	}
}

// Controller reindexes the entity store in the background whenever the running
// index schema differs from the one the store was last reindexed against. It
// works directly against the EtcdStore rather than the entity-access client,
// since it operates below the index it is repairing.
//
// The coordinator is a singleton, so there is no second writer racing the
// cursor or the hash; the state here needs no locking beyond etcd itself.
type Controller struct {
	Log   *slog.Logger
	Store *entity.EtcdStore
	// CurrentHash returns the index schema hash of the running binary,
	// injected rather than called directly because the schema registry sits
	// above the entity package.
	CurrentHash func() string
	Config      Config

	cancel context.CancelFunc
}

// Start begins the background reindex loop.
func (c *Controller) Start(ctx context.Context) {
	// Substitute defaults for anything unset. The intervals matter as much as
	// the batch size: a zero delay makes time.After fire immediately, turning
	// the loop into a hot spin that reads the index hash from etcd continuously.
	defaults := DefaultConfig()
	if c.Config.MaxEntitiesPerPass <= 0 {
		c.Config.MaxEntitiesPerPass = defaults.MaxEntitiesPerPass
	}
	if c.Config.IdleInterval <= 0 {
		c.Config.IdleInterval = defaults.IdleInterval
	}
	if c.Config.ActiveInterval <= 0 {
		c.Config.ActiveInterval = defaults.ActiveInterval
	}

	c.Log.Info("starting schema reindex controller",
		"idle_interval", c.Config.IdleInterval,
		"active_interval", c.Config.ActiveInterval,
		"max_entities_per_pass", c.Config.MaxEntitiesPerPass)

	ctx, c.cancel = context.WithCancel(ctx)
	go c.run(ctx)
}

// Stop gracefully stops the controller.
func (c *Controller) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Controller) run(ctx context.Context) {
	// Always wait before a pass, including the first: startup is busy enough
	// without a full-keyspace scan joining the boot storm, and a schema change
	// is not urgent to the second. The write path already indexes new entities
	// correctly; this only backfills the ones that predate the change.
	delay := c.Config.ActiveInterval

	for {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			c.Log.Info("schema reindex controller stopped")
			return
		}

		if c.step(ctx) {
			delay = c.Config.ActiveInterval
		} else {
			delay = c.Config.IdleInterval
		}
	}
}

// step runs at most one bounded pass. It reports whether work remains, so the
// caller can come back promptly instead of sleeping out a full idle interval.
// Like the stale-index sweep, it is strictly best-effort: errors are logged,
// never propagated, and never block a foreground operation.
func (c *Controller) step(ctx context.Context) bool {
	currentHash := c.CurrentHash()

	storedHash, err := c.Store.LoadIndexHash(ctx)
	if err != nil {
		c.Log.Warn("schema reindex: failed to read index hash", "error", err)
		return false
	}

	if storedHash == currentHash {
		c.Log.Debug("schema reindex: index hash unchanged, nothing to do", "hash", currentHash)
		return false
	}

	state, err := c.Store.LoadReindexState(ctx, c.Log)
	if err != nil {
		c.Log.Warn("schema reindex: failed to read reindex state", "error", err)
		return false
	}

	// A reindex in flight against a different target means the schema changed
	// again mid-reindex. Finishing the old scan would stamp a hash the store
	// doesn't actually match, so restart against the new target instead.
	if state == nil || state.TargetHash != currentHash {
		if state != nil {
			c.Log.Info("schema reindex: index schema changed mid-reindex, restarting scan",
				"previous_target", state.TargetHash,
				"new_target", currentHash,
				"discarded_progress", state.EntitiesProcessed)
		} else {
			c.Log.Info("schema reindex: index schema changed, starting reindex",
				"stored_hash", storedHash,
				"current_hash", currentHash)
		}
		state = &entity.ReindexState{TargetHash: currentHash}
	}

	passCtx := ctx
	if c.Config.PassTimeout > 0 {
		var cancel context.CancelFunc
		passCtx, cancel = context.WithTimeout(ctx, c.Config.PassTimeout)
		defer cancel()
	}

	stats, passErr := c.Store.Reindex(passCtx, c.Log, entity.ReindexOptions{
		StartKey:    state.Cursor,
		MaxEntities: c.Config.MaxEntitiesPerPass,
		BatchPause:  c.Config.BatchPause,
	})
	if stats == nil {
		stats = &entity.ReindexStats{}
	}

	// Checkpoint whatever the pass covered, even when it ended in error: the
	// cursor it reached is real progress, and dropping it is what made the old
	// startup path unable to converge. A complete pass reports no cursor, so
	// this is skipped there and the state is cleared outright below.
	if stats.NextCursor != "" {
		state.Cursor = stats.NextCursor
		state.EntitiesProcessed += stats.EntitiesProcessed
		if err := c.Store.SaveReindexState(ctx, state); err != nil {
			c.Log.Warn("schema reindex: failed to checkpoint progress", "error", err)
		}
	}

	if passErr != nil {
		// A pass cut short by a deadline or shutdown is expected and not
		// alarming; the next one resumes from the checkpoint above. Warn so a
		// persistent hard error is still visible.
		c.Log.Warn("schema reindex: pass ended early",
			"error", passErr,
			"processed_this_pass", stats.EntitiesProcessed,
			"processed_total", state.EntitiesProcessed)
		return true
	}

	if !stats.Complete {
		c.Log.Info("schema reindex: pass checkpointed",
			"processed_this_pass", stats.EntitiesProcessed,
			"processed_total", state.EntitiesProcessed)
		return true
	}

	// Reaching the end of the keyspace is not the same as having indexed
	// everything. Entities that failed are logged and skipped mid-scan, so a
	// pass can be Complete with some left un-indexed; stamping the hash there
	// would declare consistency we never achieved and, because the hash then
	// matches, nothing would ever retry them.
	//
	// The cursor has already advanced past those entities, so keeping the
	// checkpoint would not retry them either: the next pass would start after
	// them, finish clean, and stamp. Rewind to the head instead. Reporting no
	// work pending paces that retry at IdleInterval rather than
	// ActiveInterval, so an entity that fails permanently costs one rescan an
	// hour instead of a continuous loop.
	if stats.EntitiesFailed > 0 {
		c.Log.Warn("schema reindex: pass reached the end with failures, will rescan",
			"entities_failed", stats.EntitiesFailed,
			"processed_this_pass", stats.EntitiesProcessed)

		state.Cursor = ""
		if err := c.Store.SaveReindexState(ctx, state); err != nil {
			c.Log.Warn("schema reindex: failed to rewind checkpoint", "error", err)
		}
		return false
	}

	// Record the hash before clearing the cursor. A crash in between costs one
	// redundant no-op pass; the reverse order would let a partial reindex look
	// finished.
	if err := c.Store.SaveIndexHash(ctx, currentHash); err != nil {
		c.Log.Warn("schema reindex: failed to record index hash", "error", err)
		return true
	}
	if err := c.Store.ClearReindexState(ctx); err != nil {
		c.Log.Warn("schema reindex: failed to clear reindex state", "error", err)
	}

	c.Log.Info("schema reindex complete",
		"hash", currentHash,
		"entities_processed", state.EntitiesProcessed+stats.EntitiesProcessed,
		"indexes_rebuilt", stats.IndexesRebuilt)

	return false
}
