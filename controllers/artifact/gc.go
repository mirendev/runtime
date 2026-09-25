package artifact

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// GCConfig holds configuration for artifact garbage collection.
type GCConfig struct {
	// CheckInterval is how often to run the GC sweep (default: 1h)
	CheckInterval time.Duration
}

// DefaultGCConfig returns the default GC configuration.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		CheckInterval: 1 * time.Hour,
	}
}

// GCResult contains information about artifacts processed during GC.
type GCResult struct {
	// ArchivedArtifacts contains IDs of artifacts transitioned to archived
	ArchivedArtifacts []entity.Id
	// FailedArtifacts contains IDs and errors for artifacts that failed to update
	FailedArtifacts map[entity.Id]error
	// TotalArtifacts is the total number of active artifacts evaluated
	TotalArtifacts int
	// RetainedArtifacts is the number of artifacts kept active
	RetainedArtifacts int
}

// GCController periodically archives artifacts that are no longer referenced by
// any AppVersion. Archiving (Status=ARCHIVED) is the signal the downstream
// image GC and blob GC key off to reclaim the image and its registry blobs.
//
// Retention is reference-driven rather than age- or count-based: an artifact is
// kept exactly as long as some AppVersion still points at it. Because artifacts
// are deduplicated by manifest digest (many versions can share one artifact),
// an artifact survives until its last referencing version is pruned. The
// version retention GC (controllers/version) is what removes those references
// over time; this controller reacts to the result.
type GCController struct {
	Log    *slog.Logger
	EAC    *entityserver_v1alpha.EntityAccessClient
	Config GCConfig

	cancel context.CancelFunc
}

// Start begins the periodic GC process.
func (c *GCController) Start(ctx context.Context) {
	c.Log.Info("starting artifact GC controller",
		"check_interval", c.Config.CheckInterval)

	ctx, c.cancel = context.WithCancel(ctx)
	go c.run(ctx)
}

// Stop gracefully stops the controller.
func (c *GCController) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

// run executes the periodic GC loop.
func (c *GCController) run(ctx context.Context) {
	ticker := time.NewTicker(c.Config.CheckInterval)
	defer ticker.Stop()

	// Run an initial GC on startup after a short delay
	select {
	case <-time.After(30 * time.Second):
		c.runGCWithLogging(ctx)
	case <-ctx.Done():
		c.Log.Info("artifact GC controller stopped")
		return
	}

	for {
		select {
		case <-ticker.C:
			c.runGCWithLogging(ctx)
		case <-ctx.Done():
			c.Log.Info("artifact GC controller stopped")
			return
		}
	}
}

// runGCWithLogging runs GC and logs results.
func (c *GCController) runGCWithLogging(ctx context.Context) {
	result, err := c.RunGC(ctx)
	if err != nil {
		c.Log.Error("artifact GC failed", "error", err)
		return
	}

	if len(result.ArchivedArtifacts) > 0 || len(result.FailedArtifacts) > 0 {
		c.Log.Info("artifact GC complete",
			"archived", len(result.ArchivedArtifacts),
			"failed", len(result.FailedArtifacts),
			"retained", result.RetainedArtifacts,
			"total", result.TotalArtifacts)

		for _, id := range result.ArchivedArtifacts {
			c.Log.Debug("archived artifact", "artifact", id)
		}
		for id, err := range result.FailedArtifacts {
			c.Log.Warn("failed to archive artifact", "artifact", id, "error", err)
		}
	} else {
		c.Log.Debug("artifact GC complete, no artifacts archived",
			"retained", result.RetainedArtifacts,
			"total", result.TotalArtifacts)
	}
}

// RunGC archives every active artifact that no AppVersion references.
func (c *GCController) RunGC(ctx context.Context) (*GCResult, error) {
	result := &GCResult{
		ArchivedArtifacts: []entity.Id{},
		FailedArtifacts:   make(map[entity.Id]error),
	}

	gcCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Build the set of artifacts still referenced by a version. This spans ALL
	// AppVersions — ephemeral ones included — since an ephemeral version's image
	// must stay available for as long as the version exists.
	referenced, err := c.referencedArtifacts(gcCtx)
	if err != nil {
		return result, err
	}

	// Only active artifacts can be archived; already-archived ones are skipped.
	resp, err := c.EAC.List(gcCtx, entity.Ref(core_v1alpha.ArtifactStatusId, core_v1alpha.ArtifactStatusActiveId))
	if err != nil {
		return result, fmt.Errorf("failed to list active artifacts: %w", err)
	}

	result.TotalArtifacts = len(resp.Values())

	for _, e := range resp.Values() {
		var art core_v1alpha.Artifact
		art.Decode(e.Entity())

		// System artifacts belong to no app by design -- the toolchain image
		// miren builds for itself is the case -- so the AppVersion set will
		// never name them. Collecting them would delete blobs out from under
		// nodes still pulling the image.
		if referenced[art.ID] || core_v1alpha.IsSystemArtifact(art.ID) {
			result.RetainedArtifacts++
			continue
		}

		if err := c.archiveArtifact(gcCtx, art, e.Revision()); err != nil {
			result.FailedArtifacts[art.ID] = err
		} else {
			result.ArchivedArtifacts = append(result.ArchivedArtifacts, art.ID)
		}
	}

	return result, nil
}

// referencedArtifacts returns the set of artifact IDs referenced by any
// AppVersion.
func (c *GCController) referencedArtifacts(ctx context.Context) (map[entity.Id]bool, error) {
	resp, err := c.EAC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindAppVersion))
	if err != nil {
		return nil, fmt.Errorf("failed to list app versions: %w", err)
	}

	referenced := make(map[entity.Id]bool)
	for _, e := range resp.Values() {
		var av core_v1alpha.AppVersion
		av.Decode(e.Entity())
		if av.Artifact != "" {
			referenced[av.Artifact] = true
		}
	}
	return referenced, nil
}

// archiveArtifact updates an artifact's status to archived.
func (c *GCController) archiveArtifact(ctx context.Context, art core_v1alpha.Artifact, revision int64) error {
	patchAttrs := entity.New(
		entity.Ref(entity.DBId, art.ID),
		(&core_v1alpha.Artifact{
			Status: core_v1alpha.ARCHIVED,
		}).Encode,
	)
	_, err := c.EAC.Patch(ctx, patchAttrs.Attrs(), revision)
	if err != nil {
		return fmt.Errorf("failed to archive artifact: %w", err)
	}

	return nil
}
