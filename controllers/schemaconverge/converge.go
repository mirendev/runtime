// Package schemaconverge gently rewrites historical entity representations to
// the canonical forms described by the running schema.
package schemaconverge

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/entity"
)

const failedPassesBeforeIdle = 2

type Config struct {
	IdleInterval       time.Duration
	ActiveInterval     time.Duration
	MaxEntitiesPerPass int
	BatchPause         time.Duration
	PassTimeout        time.Duration
}

func DefaultConfig() Config {
	return Config{
		IdleInterval:       time.Hour,
		ActiveInterval:     10 * time.Second,
		MaxEntitiesPerPass: 2000,
		BatchPause:         100 * time.Millisecond,
		PassTimeout:        5 * time.Minute,
	}
}

// Controller runs one bounded convergence pass at a time. The coordinator is
// a singleton, so persisted progress needs no ownership lock beyond etcd.
type Controller struct {
	Log         *slog.Logger
	Store       *entity.EtcdStore
	CurrentPlan func() (entity.ConvergencePlan, error)
	Config      Config

	cancel context.CancelFunc
}

func (c *Controller) Start(ctx context.Context) {
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

	c.Log.Info("starting schema convergence controller",
		"idle_interval", c.Config.IdleInterval,
		"active_interval", c.Config.ActiveInterval,
		"max_entities_per_pass", c.Config.MaxEntitiesPerPass)

	ctx, c.cancel = context.WithCancel(ctx)
	go c.run(ctx)
}

func (c *Controller) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Controller) run(ctx context.Context) {
	delay := c.Config.ActiveInterval
	for {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			c.Log.Info("schema convergence controller stopped")
			return
		}

		if c.step(ctx) {
			delay = c.Config.ActiveInterval
		} else {
			delay = c.Config.IdleInterval
		}
	}
}

// step runs at most one bounded pass and reports whether work remains.
func (c *Controller) step(ctx context.Context) bool {
	plan, err := c.CurrentPlan()
	if err != nil {
		c.Log.Warn("schema convergence: failed to build plan", "error", err)
		return false
	}
	currentHash := plan.Hash()

	storedHash, err := c.Store.LoadConvergenceHash(ctx)
	if err != nil {
		c.Log.Warn("schema convergence: failed to read completed hash", "error", err)
		return false
	}
	if storedHash == currentHash {
		c.Log.Debug("schema convergence: target unchanged, nothing to do", "hash", currentHash)
		return false
	}

	state, err := c.Store.LoadConvergenceState(ctx, c.Log)
	if err != nil {
		c.Log.Warn("schema convergence: failed to read state", "error", err)
		return false
	}
	if state == nil || state.TargetHash != currentHash {
		if state != nil {
			c.Log.Info("schema convergence: target changed mid-scan, restarting",
				"previous_target", state.TargetHash,
				"new_target", currentHash,
				"discarded_progress", state.EntitiesProcessed)
		} else {
			c.Log.Info("schema convergence: new target, starting scan",
				"stored_hash", storedHash,
				"current_hash", currentHash,
				"rules", len(plan.Rules))
		}
		state = &entity.ConvergenceState{TargetHash: currentHash}
	}

	passCtx := ctx
	if c.Config.PassTimeout > 0 {
		var cancel context.CancelFunc
		passCtx, cancel = context.WithTimeout(ctx, c.Config.PassTimeout)
		defer cancel()
	}

	stats, passErr := c.Store.Converge(passCtx, c.Log, plan, entity.ConvergenceOptions{
		StartKey:    state.Cursor,
		MaxEntities: c.Config.MaxEntitiesPerPass,
		BatchPause:  c.Config.BatchPause,
	})
	if stats == nil {
		stats = &entity.ConvergenceStats{}
	}

	if stats.NextCursor != "" {
		state.Cursor = stats.NextCursor
		state.EntitiesProcessed += stats.EntitiesProcessed
		state.EntitiesRewritten += stats.EntitiesRewritten
		state.ValuesRewritten += stats.ValuesRewritten
		if err := c.Store.SaveConvergenceState(ctx, state); err != nil {
			c.Log.Warn("schema convergence: failed to checkpoint progress", "error", err)
		}
	}

	if passErr != nil {
		c.Log.Warn("schema convergence: pass ended early",
			"error", passErr,
			"processed_this_pass", stats.EntitiesProcessed,
			"processed_total", state.EntitiesProcessed)
		return true
	}
	if !stats.Complete {
		c.Log.Info("schema convergence: pass checkpointed",
			"processed_this_pass", stats.EntitiesProcessed,
			"rewritten_this_pass", stats.EntitiesRewritten,
			"processed_total", state.EntitiesProcessed)
		return true
	}

	if stats.EntitiesFailed > 0 || stats.EntitiesDeferred > 0 {
		c.Log.Warn("schema convergence: pass reached the end with unfinished entities, will rescan",
			"entities_failed", stats.EntitiesFailed,
			"entities_deferred", stats.EntitiesDeferred)
		state.Cursor = ""
		state.EntitiesProcessed += stats.EntitiesProcessed
		state.EntitiesRewritten += stats.EntitiesRewritten
		state.ValuesRewritten += stats.ValuesRewritten
		if stats.EntitiesFailed > 0 {
			state.ConsecutiveFailedPasses++
		} else {
			state.ConsecutiveFailedPasses = 0
		}
		if err := c.Store.SaveConvergenceState(ctx, state); err != nil {
			c.Log.Warn("schema convergence: failed to rewind checkpoint", "error", err)
		}

		// A deferred entity usually lost a revision race with a foreground
		// writer, so retry it promptly. Repeated decode or validation failures
		// are persistent more often than not; after one quick retry, fall back
		// to the idle interval instead of scanning the full keyspace forever.
		if stats.EntitiesDeferred > 0 {
			return true
		}
		return state.ConsecutiveFailedPasses < failedPassesBeforeIdle
	}

	if err := c.Store.SaveConvergenceHash(ctx, currentHash); err != nil {
		c.Log.Warn("schema convergence: failed to record completed hash", "error", err)
		return true
	}
	if err := c.Store.ClearConvergenceState(ctx); err != nil {
		c.Log.Warn("schema convergence: failed to clear state", "error", err)
	}

	c.Log.Info("schema convergence complete",
		"hash", currentHash,
		"entities_processed", state.EntitiesProcessed+stats.EntitiesProcessed,
		"entities_rewritten", state.EntitiesRewritten+stats.EntitiesRewritten,
		"values_rewritten", state.ValuesRewritten+stats.ValuesRewritten)
	return false
}
