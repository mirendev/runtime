// Package indexgc runs a bounded, best-effort background sweep that removes
// entity-store index (collection) entries the backing entity no longer
// justifies: the entity is gone, or it still exists but no longer carries the
// value the entry indexes.
//
// It drains the backlog those leaks left and keeps a cluster self-healing
// without anyone running `miren debug reindex` by hand. Mismatched entries in
// particular cannot drain any other way, since no write path removes an indexed
// value the entity has already stopped carrying. It deliberately works off the
// read path: foreground reads stay pure and never issue deletes.
package indexgc

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/entity"
)

// defaultInitialDelay keeps the first sweep clear of the boot storm and any
// startup-time additive reindex before it begins deleting.
const defaultInitialDelay = 1 * time.Minute

// GCConfig tunes the background sweep. Defaults are intentionally gentle: this
// is convergence insurance, not active firefighting, so it drains slowly and
// stays out of the way.
type GCConfig struct {
	// CheckInterval is how often to run a sweep. A sweep resolves the entities
	// behind every entry it scans whether or not it deletes anything, so a
	// drained store pays a full read of itself on every tick. That cost, not
	// the delete rate, is what sets this.
	CheckInterval time.Duration
	// MaxDeletesPerSweep caps deletions per sweep so a large backlog drains over
	// several sweeps rather than one thundering pass. Zero means unbounded.
	//
	// Deletes are not the sweep's dominant cost, since every sweep resolves the
	// entities behind the entries it scans regardless, so throttling them hard
	// mostly stretches the drain.
	MaxDeletesPerSweep int
	// BatchPause is slept periodically during deletion to rate-limit write
	// pressure. Zero disables pacing.
	BatchPause time.Duration
	// SweepTimeout bounds a single sweep. A truncated sweep is mostly fine: the
	// pass is idempotent and the next one starts over, so finished work sticks.
	//
	// The caveat is that it restarts from the head of the keyspace, so a store
	// too large for one sweep to scan would never reach its tail. A truncated
	// sweep logs at Warn, which is the signal this needs a resume cursor.
	SweepTimeout time.Duration
	// InitialDelay is how long to wait before the first sweep. Zero means
	// defaultInitialDelay; it is configurable so a test can drive the schedule.
	InitialDelay time.Duration
}

// initialDelay resolves the configured first-sweep delay.
func (c GCConfig) initialDelay() time.Duration {
	if c.InitialDelay > 0 {
		return c.InitialDelay
	}
	return defaultInitialDelay
}

// DefaultGCConfig returns the default (gentle) configuration. At six hours and
// 5000 deletes a sweep, a backlog in the low hundreds of thousands drains in
// about a week, which is the right pace for entries that are bloat rather than
// a correctness problem.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		CheckInterval:      6 * time.Hour,
		MaxDeletesPerSweep: 5000,
		BatchPause:         1 * time.Second,
		SweepTimeout:       10 * time.Minute,
	}
}

// GCController periodically removes stale collection entries from the entity
// store. It operates directly on the EtcdStore's CAS-guarded cleanup rather than
// going through the entity-access client, since it works below the index it is
// repairing.
type GCController struct {
	Log    *slog.Logger
	Store  *entity.EtcdStore
	Config GCConfig

	cancel context.CancelFunc
	// stopped closes when the sweep loop has returned, so Stop can be observed
	// rather than merely requested.
	stopped chan struct{}
}

// Start begins the periodic sweep.
func (c *GCController) Start(ctx context.Context) {
	c.Log.Info("starting stale index GC controller",
		"check_interval", c.Config.CheckInterval,
		"max_deletes_per_sweep", c.Config.MaxDeletesPerSweep)

	ctx, c.cancel = context.WithCancel(ctx)
	c.stopped = make(chan struct{})
	go func() {
		defer close(c.stopped)
		c.run(ctx)
	}()
}

// Stop cancels the sweep loop and waits for it to return, so a stopped
// controller is known to have stopped issuing deletes. Safe on a controller that
// was never started, and safe to call more than once.
func (c *GCController) Stop() {
	if c.cancel == nil {
		return
	}
	c.cancel()
	<-c.stopped
}

func (c *GCController) run(ctx context.Context) {
	// Wait out the initial delay before the first sweep, then start the
	// periodic ticker. Constructing the ticker after the delay (not before)
	// keeps the first interval a true CheckInterval and avoids a back-to-back
	// double-fire if initialDelay is ever tuned longer than CheckInterval.
	select {
	case <-time.After(c.Config.initialDelay()):
		c.sweep(ctx)
	case <-ctx.Done():
		c.Log.Info("stale index GC controller stopped")
		return
	}

	ticker := time.NewTicker(c.Config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.sweep(ctx)
		case <-ctx.Done():
			c.Log.Info("stale index GC controller stopped")
			return
		}
	}
}

// sweep runs one bounded cleanup pass. It is strictly best-effort: errors are
// logged, never propagated, and never block a foreground operation.
func (c *GCController) sweep(ctx context.Context) {
	sweepCtx := ctx
	if c.Config.SweepTimeout > 0 {
		var cancel context.CancelFunc
		sweepCtx, cancel = context.WithTimeout(ctx, c.Config.SweepTimeout)
		defer cancel()
	}

	stats, err := c.Store.CleanupStaleCollectionEntries(sweepCtx, c.Log, entity.CleanupOptions{
		MaxDeletes: c.Config.MaxDeletesPerSweep,
		BatchPause: c.Config.BatchPause,
	})
	if err != nil {
		// A truncated scan (deadline/shutdown) is expected and not alarming; the
		// next sweep resumes. Log at Warn so a persistent hard error is still
		// visible.
		c.Log.Warn("stale index GC sweep ended early", "error", err,
			"scanned", stats.CollectionEntriesScanned,
			"removed", stats.StaleEntriesRemoved)
		return
	}

	if stats.StaleEntriesFound > 0 || stats.StaleEntriesRemoved > 0 {
		c.Log.Info("stale index GC sweep complete",
			"scanned", stats.CollectionEntriesScanned,
			"found", stats.StaleEntriesFound,
			"orphaned", stats.OrphanedEntriesFound,
			"mismatched", stats.MismatchedEntriesFound,
			"removed", stats.StaleEntriesRemoved,
			"cas_conflicts", stats.CASConflicts,
			"by_collection", stats.RemovedByCollection)
	} else {
		c.Log.Debug("stale index GC sweep complete, nothing stale",
			"scanned", stats.CollectionEntriesScanned)
	}
}
