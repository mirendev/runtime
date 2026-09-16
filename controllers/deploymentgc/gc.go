// Package deploymentgc implements retention garbage collection for deployment
// records.
//
// Every deploy publishes a deployment record, and until this controller
// existed nothing ever removed a settled one, so a cluster's deploy history
// grew for as long as it ran. Miren Cloud is the durable book of record for
// that history: deployments export under the archive lifecycle, which keeps
// cloud's copy past the runtime's delete. The runtime only needs to keep
// enough recent history to serve `miren app history` and to explain the
// app's current state, so this controller bounds each app's records to the
// most-recent RetentionCount plus anything younger than RetentionPeriod.
package deploymentgc

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
)

// GCConfig holds configuration for deployment record retention GC.
type GCConfig struct {
	// CheckInterval is how often to run the GC sweep (default: 1h).
	CheckInterval time.Duration

	// RetentionPeriod keeps records newer than this regardless of count
	// (default: 30 days). Zero disables the sweep entirely, keeping every
	// record indefinitely.
	RetentionPeriod time.Duration

	// RetentionCount keeps this many most-recent records per app regardless
	// of age (default: 25).
	RetentionCount int
}

// DefaultGCConfig returns the default GC configuration.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		CheckInterval:   1 * time.Hour,
		RetentionPeriod: 30 * 24 * time.Hour,
		RetentionCount:  25,
	}
}

// GCResult contains information about records processed during a GC sweep.
type GCResult struct {
	// DeletedRecords is the number of records hard-deleted.
	DeletedRecords int
	// FailedRecords is the number of records that failed to delete.
	FailedRecords int
	// RetainedRecords is the number of records kept by retention policy.
	RetainedRecords int
	// AwaitingExport is the number of prune candidates kept because cloud
	// has not yet confirmed it holds their final state.
	AwaitingExport int
	// TotalScanned is the number of deployment records evaluated.
	TotalScanned int
}

// ExportProgress reports how much of the entity store cloud holds durably.
// Entity sync's diagnostics satisfy it.
type ExportProgress interface {
	// LandedRevision returns the highest store revision cloud has confirmed,
	// and whether export applies to this cluster at all.
	LandedRevision() (revision int64, exporting bool)
}

// GCController periodically applies retention policy to deployment records.
type GCController struct {
	Log    *slog.Logger
	EAC    *entityserver_v1alpha.EntityAccessClient
	Config GCConfig

	// StartGate, when set, holds the first sweep until it closes. The server
	// passes the same readiness signal that gates entity sync: the
	// deployment-attempt migration has finished repairing every record and
	// backfilling the cloud export marker. Deleting a record before then
	// would race the migration and, worse, remove a record the exporter has
	// not been told to watch, so cloud would never receive its final state.
	StartGate <-chan struct{}

	// Exports, when set, makes cloud custody a condition of deletion on a
	// registered cluster: a record is only pruned once its revision is at or
	// below the revision cloud has landed. Cloud keeps deployments as an
	// archive, so this is what lets the runtime forget a record without the
	// history losing it. The retention floor alone is not enough, because it
	// measures the deploy's age rather than how long cloud has had a chance
	// to see it: a cluster upgrading with years of never-exported history
	// would otherwise prune most of it before the first snapshot pinned its
	// head. Nil, or an unregistered cluster, prunes on retention alone.
	Exports ExportProgress

	// now is a test seam; nil means time.Now.
	now func() time.Time

	store  *deploylifecycle.Store
	cancel context.CancelFunc
}

// Start begins the periodic GC process.
func (c *GCController) Start(ctx context.Context) {
	if c.Config.RetentionPeriod <= 0 {
		c.Log.Info("deployment retention GC disabled; keeping deployment records indefinitely")
		return
	}

	c.Log.Info("starting deployment retention GC controller",
		"check_interval", c.Config.CheckInterval,
		"retention_period", c.Config.RetentionPeriod,
		"retention_count", c.Config.RetentionCount)

	ctx, c.cancel = context.WithCancel(ctx)
	go c.run(ctx)
}

// Stop gracefully stops the controller.
func (c *GCController) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *GCController) run(ctx context.Context) {
	if c.StartGate != nil {
		select {
		case <-c.StartGate:
		case <-ctx.Done():
			c.Log.Info("deployment retention GC controller stopped")
			return
		}
	}

	ticker := time.NewTicker(c.Config.CheckInterval)
	defer ticker.Stop()

	// Run an initial GC on startup after a short delay.
	select {
	case <-time.After(30 * time.Second):
		c.runGCWithLogging(ctx)
	case <-ctx.Done():
		c.Log.Info("deployment retention GC controller stopped")
		return
	}

	for {
		select {
		case <-ticker.C:
			c.runGCWithLogging(ctx)
		case <-ctx.Done():
			c.Log.Info("deployment retention GC controller stopped")
			return
		}
	}
}

func (c *GCController) runGCWithLogging(ctx context.Context) {
	result, err := c.RunGC(ctx)
	if err != nil {
		c.Log.Error("deployment retention GC failed", "error", err)
		return
	}

	if result.DeletedRecords > 0 || result.FailedRecords > 0 {
		c.Log.Info("deployment retention GC complete",
			"deleted", result.DeletedRecords,
			"failed", result.FailedRecords,
			"awaiting_export", result.AwaitingExport,
			"retained", result.RetainedRecords,
			"total", result.TotalScanned)
	} else {
		c.Log.Debug("deployment retention GC complete, nothing pruned",
			"awaiting_export", result.AwaitingExport,
			"retained", result.RetainedRecords,
			"total", result.TotalScanned)
	}
}

func (c *GCController) deployStore() *deploylifecycle.Store {
	if c.store == nil {
		c.store = deploylifecycle.NewStore(c.Log, c.EAC)
	}
	return c.store
}

func (c *GCController) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// RunGC applies the retention policy to every app's deployment records.
func (c *GCController) RunGC(ctx context.Context) (*GCResult, error) {
	result := &GCResult{}
	if c.Config.RetentionPeriod <= 0 {
		return result, nil
	}

	gcCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	pinned, err := c.pinnedDeployments(gcCtx)
	if err != nil {
		return result, err
	}

	// A kind scan is the only way to see every app's history in one pass;
	// this is the bounded scan that keeps the per-app history bounded.
	records, err := c.deployStore().List(gcCtx, deploylifecycle.Query{})
	if err != nil {
		return result, err
	}

	byApp := make(map[string][]*deploylifecycle.Record)
	for _, rec := range records {
		result.TotalScanned++
		byApp[rec.Deployment.AppName] = append(byApp[rec.Deployment.AppName], rec)
	}

	retentionCutoff := c.clock().Add(-c.Config.RetentionPeriod)

	var landed int64
	var exporting bool
	if c.Exports != nil {
		landed, exporting = c.Exports.LandedRevision()
	}

	for appName, recs := range byApp {
		// Newest first. Store.List already sorts on the record's own start
		// time, but a legacy record can carry none at all, so fall back to
		// entity creation the way reconciliation does rather than letting a
		// blank timestamp read as the oldest record in history.
		sort.SliceStable(recs, func(i, j int) bool {
			return startedAt(recs[i]).After(startedAt(recs[j]))
		})

		for i, rec := range recs {
			id := string(rec.Deployment.ID)

			// Always keep what the app's current state points at: the
			// deployment that made the active version current and whichever
			// attempt holds the deploy lock. An in-flight attempt is never
			// history yet; reconciliation owns settling it, at which point it
			// becomes an ordinary candidate on a later sweep. A record whose
			// legacy status still reads active is kept too: when the
			// migration finds more than one such record for an app it leaves
			// the pointer unset, and the server falls back to those rows to
			// say what is serving.
			//
			// There is no re-check before the delete, unlike the version GC.
			// A settled record never becomes pinned again: rollback publishes
			// a fresh attempt rather than re-activating an old one, so the
			// active pointer only ever moves to a record that was in progress
			// when we read it.
			if pinned[id] || rec.Status() == deploylifecycle.StatusInProgress ||
				rec.Deployment.Status == string(deploylifecycle.StatusActive) ||
				i < c.Config.RetentionCount ||
				startedAt(rec).After(retentionCutoff) {
				result.RetainedRecords++
				continue
			}

			// The record's revision is its last write; if cloud has landed
			// that revision it holds the final state, and a delete after it
			// only tells cloud to keep the archived row.
			if exporting && rec.Revision > landed {
				c.Log.Debug("retaining prune candidate cloud has not landed",
					"deployment_id", id, "app", appName,
					"revision", rec.Revision, "landed_revision", landed)
				result.AwaitingExport++
				continue
			}

			if err := c.deployStore().Delete(gcCtx, id); err != nil {
				c.Log.Warn("failed to delete deployment record",
					"deployment_id", id, "app", appName, "error", err)
				result.FailedRecords++
				continue
			}
			c.Log.Debug("pruned deployment record",
				"deployment_id", id, "app", appName, "started_at", startedAt(rec))
			result.DeletedRecords++
		}
	}

	return result, nil
}

// pinnedDeployments collects the deployment IDs that apps currently reference,
// so a sweep never removes the record explaining an app's live state.
func (c *GCController) pinnedDeployments(ctx context.Context) (map[string]bool, error) {
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return nil, fmt.Errorf("failed to list apps: %w", err)
	}

	pinned := make(map[string]bool)
	for _, e := range resp.Values() {
		var app core_v1alpha.App
		app.Decode(e.Entity())
		if app.ActiveDeployment != "" {
			pinned[string(app.ActiveDeployment)] = true
		}
		if app.DeploymentLock.DeploymentId != "" {
			pinned[app.DeploymentLock.DeploymentId] = true
		}
	}
	return pinned, nil
}

func startedAt(rec *deploylifecycle.Record) time.Time {
	if at := rec.StartedAt(); !at.IsZero() {
		return at
	}
	if rec.Entity != nil && rec.Entity.HasCreatedAt() {
		return time.UnixMilli(rec.Entity.CreatedAt())
	}
	return time.Time{}
}
