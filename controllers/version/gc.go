// Package version implements retention garbage collection for AppVersions.
//
// Every deploy mints a new AppVersion that otherwise lives forever, bloating
// the entity store and making the ephemeral GC's full-version scan grow without
// bound. This controller hard-deletes old non-active, non-ephemeral versions so
// that growth stays bounded. Ephemeral versions are skipped — they have their
// own TTL-based GC (controllers/ephemeral).
package version

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/appversion"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/sysstats"
)

// GCConfig holds configuration for app version retention GC.
type GCConfig struct {
	// CheckInterval is how often to run the GC sweep (default: 1h).
	CheckInterval time.Duration
	// RetentionPeriod keeps versions newer than this regardless of count
	// (default: 30 days).
	RetentionPeriod time.Duration
	// RetentionCount keeps this many most-recent versions per app regardless of
	// age (default: 10).
	RetentionCount int
	// DiskPressureThreshold is the storage-usage percentage at or above which a
	// sweep drops the RetentionPeriod floor and prunes each app down to its
	// active version plus RetentionCount most-recent versions. Without this, the
	// period floor pins every version younger than RetentionPeriod, so on a
	// busy cluster the downstream image GC has nothing to reclaim and the disk
	// fills. The active version and RetentionCount are always honored, even
	// under pressure. Matches the image GC's threshold (default: 80). Zero
	// disables pressure-aware pruning.
	DiskPressureThreshold float64
}

// DefaultGCConfig returns the default GC configuration.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		CheckInterval:         1 * time.Hour,
		RetentionPeriod:       30 * 24 * time.Hour,
		RetentionCount:        10,
		DiskPressureThreshold: 80.0,
	}
}

// GCResult contains information about versions processed during a GC sweep.
type GCResult struct {
	// DeletedVersions is the number of versions hard-deleted.
	DeletedVersions int
	// FailedVersions is the number of versions that failed to delete.
	FailedVersions int
	// RetainedVersions is the number of versions kept by retention policy.
	RetainedVersions int
	// SkippedLive is the number of prune candidates retained because a live
	// sandbox still references them.
	SkippedLive int
	// TotalScanned is the number of non-ephemeral versions evaluated.
	TotalScanned int
}

// GCController periodically applies retention policy to AppVersions,
// hard-deleting old non-active, non-ephemeral versions.
type GCController struct {
	Log    *slog.Logger
	EAC    *entityserver_v1alpha.EntityAccessClient
	Config GCConfig

	// DataPath is the server data directory sampled for disk usage when
	// DiskPressureThreshold is set. Empty disables pressure-aware pruning.
	DataPath string

	// storagePercent samples current storage usage (0-100). Defaults to
	// sysstats over DataPath when nil; overridden in tests.
	storagePercent func() float64

	cancel context.CancelFunc
}

// Start begins the periodic GC process.
func (c *GCController) Start(ctx context.Context) {
	c.Log.Info("starting version retention GC controller",
		"check_interval", c.Config.CheckInterval,
		"retention_period", c.Config.RetentionPeriod,
		"retention_count", c.Config.RetentionCount,
		"disk_pressure_threshold", c.Config.DiskPressureThreshold)

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
	ticker := time.NewTicker(c.Config.CheckInterval)
	defer ticker.Stop()

	// Run an initial GC on startup after a short delay.
	select {
	case <-time.After(30 * time.Second):
		c.runGCWithLogging(ctx)
	case <-ctx.Done():
		c.Log.Info("version retention GC controller stopped")
		return
	}

	for {
		select {
		case <-ticker.C:
			c.runGCWithLogging(ctx)
		case <-ctx.Done():
			c.Log.Info("version retention GC controller stopped")
			return
		}
	}
}

func (c *GCController) runGCWithLogging(ctx context.Context) {
	result, err := c.RunGC(ctx)
	if err != nil {
		c.Log.Error("version retention GC failed", "error", err)
		return
	}

	if result.DeletedVersions > 0 || result.FailedVersions > 0 {
		c.Log.Info("version retention GC complete",
			"deleted", result.DeletedVersions,
			"failed", result.FailedVersions,
			"skipped_live", result.SkippedLive,
			"retained", result.RetainedVersions,
			"total", result.TotalScanned)
	} else {
		c.Log.Debug("version retention GC complete, nothing pruned",
			"skipped_live", result.SkippedLive,
			"retained", result.RetainedVersions,
			"total", result.TotalScanned)
	}
}

type versionInfo struct {
	version   core_v1alpha.AppVersion
	createdAt time.Time
}

// RunGC applies the retention policy to all non-ephemeral AppVersions.
func (c *GCController) RunGC(ctx context.Context) (*GCResult, error) {
	result := &GCResult{}

	gcCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	activeVersions, err := c.activeVersions(gcCtx)
	if err != nil {
		return result, err
	}

	liveVersions, err := c.versionsWithLiveSandboxes(gcCtx)
	if err != nil {
		return result, err
	}

	resp, err := c.EAC.List(gcCtx, entity.Ref(entity.EntityKind, core_v1alpha.KindAppVersion))
	if err != nil {
		return result, fmt.Errorf("failed to list app versions: %w", err)
	}

	// Group non-ephemeral versions by app.
	versionsByApp := make(map[entity.Id][]versionInfo)
	for _, e := range resp.Values() {
		var av core_v1alpha.AppVersion
		av.Decode(e.Entity())

		// Ephemeral versions have their own TTL-based GC.
		if av.EphemeralLabel != "" {
			continue
		}

		result.TotalScanned++
		versionsByApp[av.App] = append(versionsByApp[av.App], versionInfo{
			version:   av,
			createdAt: time.UnixMilli(e.CreatedAt()),
		})
	}

	now := time.Now()
	retentionCutoff := now.Add(-c.Config.RetentionPeriod)

	// Under disk pressure, drop the age-based floor so retention collapses to
	// the active version plus RetentionCount. The period floor otherwise pins
	// every recent version's image, leaving the downstream image GC nothing to
	// reclaim while the disk fills.
	underPressure := c.underDiskPressure()

	for appID, versions := range versionsByApp {
		// Sort by creation time, newest first.
		sort.Slice(versions, func(i, j int) bool {
			return versions[i].createdAt.After(versions[j].createdAt)
		})

		activeVersion := activeVersions[appID]

		for i, info := range versions {
			// Always keep the active version and the most-recent N. Also keep
			// anything younger than the retention window, unless we're under
			// disk pressure and shedding the age-based floor.
			if info.version.ID == activeVersion ||
				i < c.Config.RetentionCount ||
				(!underPressure && info.createdAt.After(retentionCutoff)) {
				result.RetainedVersions++
				continue
			}

			// Never delete a version a live sandbox still references; retain it
			// for a future sweep once the sandbox is gone.
			if liveVersions[info.version.ID] {
				c.Log.Debug("retaining prune candidate with live sandbox",
					"version_id", info.version.ID, "app", appID)
				result.SkippedLive++
				continue
			}

			v := info.version

			// The active/live sets were snapshotted at the top of the sweep,
			// which can be up to a full pass stale. Re-verify the pin state
			// immediately before deleting so a rollback that makes this version
			// active, or a sandbox that starts referencing it mid-sweep, isn't
			// deleted out from under the running system.
			pinned, err := c.pinnedNow(gcCtx, v.ID, appID)
			if err != nil {
				c.Log.Warn("failed to re-check version before delete; retaining",
					"version_id", v.ID, "error", err)
				result.FailedVersions++
				continue
			}
			if pinned {
				c.Log.Debug("retaining prune candidate pinned during sweep",
					"version_id", v.ID, "app", appID)
				result.SkippedLive++
				continue
			}

			if err := appversion.Delete(gcCtx, c.EAC, &v, c.Log); err != nil {
				c.Log.Warn("failed to delete app version", "version_id", v.ID, "error", err)
				result.FailedVersions++
			} else {
				result.DeletedVersions++
			}
		}
	}

	return result, nil
}

// underDiskPressure reports whether current storage usage is at or above the
// configured threshold, in which case the sweep drops the RetentionPeriod floor
// and prunes each app to its active version plus RetentionCount. Returns false
// when pressure-aware pruning is disabled (threshold <= 0) or no usage source is
// available.
func (c *GCController) underDiskPressure() bool {
	if c.Config.DiskPressureThreshold <= 0 {
		return false
	}

	sample := c.storagePercent
	if sample == nil {
		if c.DataPath == "" {
			return false
		}
		sample = func() float64 {
			return sysstats.CollectSystemStats(c.DataPath).StoragePercent
		}
	}

	pct := sample()
	if pct >= c.Config.DiskPressureThreshold {
		c.Log.Info("disk pressure at or above threshold; dropping retention period floor for this sweep",
			"storage_percent", pct,
			"threshold", c.Config.DiskPressureThreshold,
			"retention_count", c.Config.RetentionCount)
		return true
	}
	return false
}

// activeVersions returns a map of app ID to its active version ID.
func (c *GCController) activeVersions(ctx context.Context) (map[entity.Id]entity.Id, error) {
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return nil, fmt.Errorf("failed to list apps: %w", err)
	}

	active := make(map[entity.Id]entity.Id)
	for _, e := range resp.Values() {
		var app core_v1alpha.App
		app.Decode(e.Entity())
		if app.ActiveVersion != "" {
			active[app.ID] = app.ActiveVersion
		}
	}
	return active, nil
}

// versionsWithLiveSandboxes returns the set of version IDs that a pending,
// not-ready, or running sandbox currently references.
func (c *GCController) versionsWithLiveSandboxes(ctx context.Context) (map[entity.Id]bool, error) {
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, compute_v1alpha.KindSandbox))
	if err != nil {
		return nil, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	live := make(map[entity.Id]bool)
	for _, e := range resp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(e.Entity())

		switch sb.Status {
		case compute_v1alpha.PENDING, compute_v1alpha.NOT_READY, compute_v1alpha.RUNNING:
			if sb.Spec.Version != "" {
				live[sb.Spec.Version] = true
			}
		case compute_v1alpha.STOPPED, compute_v1alpha.DEAD:
			// Terminal sandboxes no longer pin their version.
		}
	}
	return live, nil
}

// pinnedNow re-checks, immediately before deletion, whether a prune candidate
// has become its app's active version or picked up a live sandbox since the
// pre-sweep snapshots were taken. Both lookups are cheap point queries (a
// single Get and an indexed list) and only run for versions that already
// cleared retention, so this stays inexpensive.
func (c *GCController) pinnedNow(ctx context.Context, versionID, appID entity.Id) (bool, error) {
	appResp, err := c.EAC.Get(ctx, appID.String())
	if err != nil {
		return false, fmt.Errorf("failed to re-check app %s: %w", appID, err)
	}
	if appResp.HasEntity() {
		var app core_v1alpha.App
		app.Decode(appResp.Entity().Entity())
		if app.ActiveVersion == versionID {
			return true, nil
		}
	}

	sbResp, err := c.EAC.List(ctx, entity.Ref(compute_v1alpha.SandboxSpecVersionId, versionID))
	if err != nil {
		return false, fmt.Errorf("failed to re-check sandboxes for version %s: %w", versionID, err)
	}
	for _, e := range sbResp.Values() {
		var sb compute_v1alpha.Sandbox
		sb.Decode(e.Entity())
		switch sb.Status {
		case compute_v1alpha.PENDING, compute_v1alpha.NOT_READY, compute_v1alpha.RUNNING:
			return true, nil
		case compute_v1alpha.STOPPED, compute_v1alpha.DEAD:
			// Terminal sandboxes no longer pin their version.
		}
	}
	return false, nil
}
