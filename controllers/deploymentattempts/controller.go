// Package deploymentattempts backfills canonical deployment attempts and
// continuously repairs the activation crash window.
package deploymentattempts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
)

const (
	phaseDeployments = "deployments"
	phaseVersions    = "app_versions"
	phaseApps        = "apps"
	phaseReconcile   = "reconcile"

	// Older CLIs embedded the complete BuildKit output in the deployment
	// entity. Keep the literal after removing the attribute from the generated
	// schema so the migration can physically evict existing payloads.
	legacyDeploymentBuildLogs = entity.Id("dev.miren.core/deployment.build_logs")
)

type Config struct {
	Interval  time.Duration
	BatchSize int64
}

func DefaultConfig() Config {
	return Config{Interval: 10 * time.Second, BatchSize: 200}
}

type Controller struct {
	Log     *slog.Logger
	Store   entity.Store
	Tracker *deploylifecycle.Tracker
	Config  Config
	cancel  context.CancelFunc

	// Canonical fields on the records are the durable migration ledger. These
	// fields only bound this process's scan; a restart begins at the front and
	// cheaply skips work that the previous process completed.
	phase                string
	cursor               string
	passFailed           bool
	initialSweepComplete func()
	initialSweepOnce     sync.Once
}

func New(log *slog.Logger, store entity.Store, eac *entityserver_v1alpha.EntityAccessClient, initialSweepComplete func()) *Controller {
	return &Controller{
		Log: log.With("module", "deployment-attempts"), Store: store,
		Tracker: deploylifecycle.NewTracker(log, eac), Config: DefaultConfig(),
		initialSweepComplete: initialSweepComplete,
	}
}

func (c *Controller) Start(ctx context.Context) {
	if c.Config.Interval <= 0 {
		c.Config.Interval = DefaultConfig().Interval
	}
	if c.Config.BatchSize <= 0 {
		c.Config.BatchSize = DefaultConfig().BatchSize
	}
	ctx, c.cancel = context.WithCancel(ctx)
	go c.run(ctx)
}

func (c *Controller) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Controller) run(ctx context.Context) {
	for {
		if err := c.Step(ctx); err != nil && ctx.Err() == nil {
			c.Log.Warn("deployment-attempt migration pass failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.Config.Interval):
		}
	}
}

// Step processes one bounded page. It is public so tests and operators can
// drive convergence without waiting for the loop.
func (c *Controller) Step(ctx context.Context) error {
	if c.phase == "" {
		c.phase = phaseDeployments
	}

	kind, migrate := c.phaseWork(c.phase)
	page, err := c.Store.ListIndexPage(ctx, entity.Ref(entity.EntityKind, kind), c.cursor, c.Config.BatchSize)
	if err != nil {
		return err
	}
	entities, err := c.Store.GetEntities(ctx, page.Ids)
	if err != nil {
		return err
	}
	var migrateErrs []error
	for _, ent := range entities {
		if err := migrate(ctx, ent); err != nil {
			wrapped := fmt.Errorf("migrating %s in phase %s: %w", ent.Id(), c.phase, err)
			migrateErrs = append(migrateErrs, wrapped)
			c.passFailed = true
			c.Log.Warn("deployment-attempt migration skipped a record",
				"id", ent.Id(), "phase", c.phase, "error", err)
		}
	}

	if page.Cursor != "" {
		c.cursor = page.Cursor
		return errors.Join(migrateErrs...)
	}

	c.cursor = ""
	switch c.phase {
	case "", phaseDeployments:
		c.phase = phaseVersions
	case phaseVersions:
		c.phase = phaseApps
	case phaseApps:
		c.phase = phaseReconcile
	case phaseReconcile:
		if c.passFailed {
			c.Log.Warn("deployment-attempt sweep completed with migration failures; initial-sweep gate remains closed")
			c.passFailed = false
		} else {
			c.initialSweepOnce.Do(func() {
				c.Log.Info("deployment-attempt migration and reconciliation initial pass complete")
				if c.initialSweepComplete != nil {
					c.initialSweepComplete()
				}
			})
		}
		// Keep scanning forever. This catches records written by a downgraded
		// runtime and continuously closes future activation crash windows.
		c.phase = phaseDeployments
	}
	return errors.Join(migrateErrs...)
}

func (c *Controller) phaseWork(phase string) (entity.Id, func(context.Context, *entity.Entity) error) {
	switch phase {
	case phaseVersions:
		return core_v1alpha.KindAppVersion, c.migrateVersion
	case phaseApps:
		return core_v1alpha.KindApp, c.migrateApp
	case phaseReconcile:
		return core_v1alpha.KindDeployment, c.reconcileDeployment
	default:
		return core_v1alpha.KindDeployment, c.migrateDeployment
	}
}

func (c *Controller) migrateDeployment(ctx context.Context, ent *entity.Entity) error {
	if _, ok := ent.Get(legacyDeploymentBuildLogs); ok {
		clean := ent.Clone()
		clean.Remove(legacyDeploymentBuildLogs)
		var err error
		ent, err = c.Store.ReplaceEntity(ctx, clean, entity.WithFromRevision(ent.GetRevision()))
		if err != nil {
			return fmt.Errorf("removing embedded build logs: %w", err)
		}
	}

	var dep core_v1alpha.Deployment
	dep.Decode(ent)
	rec := &deploylifecycle.Record{Deployment: &dep}
	if rec.Canonical() {
		return nil
	}

	var outcome string
	switch deploylifecycle.Status(dep.Status) {
	case deploylifecycle.StatusInProgress:
		// A running attempt has no outcome. Its canonical started_at/app refs
		// below distinguish it from an unmigrated legacy record.
	case deploylifecycle.StatusActive, deploylifecycle.StatusSucceeded, deploylifecycle.StatusRolledBack:
		outcome = string(deploylifecycle.StatusSucceeded)
	case deploylifecycle.StatusFailed:
		outcome = string(deploylifecycle.StatusFailed)
	case deploylifecycle.StatusCancelled:
		outcome = string(deploylifecycle.StatusCancelled)
	case deploylifecycle.StatusInterrupted:
		outcome = string(deploylifecycle.StatusInterrupted)
	default:
		return nil
	}

	attrs := []entity.Attr{entity.Ref(entity.DBId, dep.ID)}
	if outcome != "" {
		attrs = append(attrs,
			entity.String(core_v1alpha.DeploymentOutcomeId, outcome))
	}
	if dep.AppName != "" {
		if app, err := c.Store.GetEntity(ctx, entity.Id("app/"+dep.AppName)); err == nil {
			attrs = append(attrs, entity.Ref(core_v1alpha.DeploymentAppId, app.Id()))
		}
	}
	if version := legacyVersion(dep.AppVersion, string(dep.ID)); version != "" {
		if v := c.legacyAppVersion(ctx, version); v != nil {
			attrs = append(attrs, entity.Ref(core_v1alpha.DeploymentVersionId, v.Id()))
		}
	}
	if dep.SourceDeploymentId != "" {
		if source, err := c.Store.GetEntity(ctx, entity.Id(dep.SourceDeploymentId)); err == nil {
			attrs = append(attrs, entity.Ref(core_v1alpha.DeploymentSourceDeploymentId, source.Id()))
		}
	}
	if started, err := time.Parse(time.RFC3339, dep.DeployedBy.Timestamp); err == nil {
		attrs = append(attrs, entity.Time(core_v1alpha.DeploymentStartedAtId, started))
	}
	if finished, err := time.Parse(time.RFC3339, dep.CompletedAt); err == nil {
		attrs = append(attrs, entity.Time(core_v1alpha.DeploymentFinishedAtId, finished))
	}
	_, err := c.Store.PatchEntity(ctx, entity.New(attrs), entity.WithFromRevision(ent.GetRevision()))
	return err
}

func (c *Controller) legacyAppVersion(ctx context.Context, ref string) *entity.Entity {
	candidates := []entity.Id{entity.Id(ref)}
	if len(ref) < len("app_version/") || ref[:len("app_version/")] != "app_version/" {
		candidates = append(candidates, entity.Id("app_version/"+ref))
	}
	for _, id := range candidates {
		if version, err := c.Store.GetEntity(ctx, id); err == nil && entity.Is(version, core_v1alpha.KindAppVersion) {
			return version
		}
	}
	return nil
}

func (c *Controller) migrateVersion(ctx context.Context, ent *entity.Entity) error {
	var version core_v1alpha.AppVersion
	version.Decode(ent)
	if !version.Source.Empty() {
		return nil
	}

	ids, err := c.Store.ListIndex(ctx, entity.Ref(core_v1alpha.DeploymentVersionId, version.ID))
	if err != nil {
		return err
	}
	deployments, err := c.Store.GetEntities(ctx, ids)
	if err != nil {
		return err
	}
	var consensus core_v1alpha.Source
	haveConsensus := false
	for _, raw := range deployments {
		var dep core_v1alpha.Deployment
		dep.Decode(raw)
		source := deploylifecycle.SourceFromGitInfo(dep.GitInfo)
		if source.Empty() {
			continue
		}
		if !haveConsensus {
			consensus = source
			haveConsensus = true
		} else if consensus != source {
			return nil
		}
	}
	if !haveConsensus || consensus.Empty() {
		return nil
	}
	patch := entity.New(
		entity.Ref(entity.DBId, version.ID),
		entity.Component(core_v1alpha.AppVersionSourceId, consensus.Encode()),
	)
	_, err = c.Store.PatchEntity(ctx, patch, entity.WithFromRevision(ent.GetRevision()))
	return err
}

func (c *Controller) migrateApp(ctx context.Context, ent *entity.Entity) error {
	var app core_v1alpha.App
	app.Decode(ent)
	if app.ActiveVersion == "" {
		return nil
	}
	if app.ActiveDeployment != "" {
		if raw, err := c.Store.GetEntity(ctx, app.ActiveDeployment); err == nil {
			var current core_v1alpha.Deployment
			current.Decode(raw)
			if current.Version == app.ActiveVersion {
				return nil
			}
		}
	}
	ids, err := c.Store.ListIndex(ctx, entity.Ref(core_v1alpha.DeploymentVersionId, app.ActiveVersion))
	if err != nil {
		return err
	}
	var candidate entity.Id
	for _, id := range ids {
		raw, err := c.Store.GetEntity(ctx, id)
		if err != nil {
			continue
		}
		var dep core_v1alpha.Deployment
		dep.Decode(raw)
		if dep.App == app.ID && dep.Status == string(deploylifecycle.StatusActive) {
			if candidate != "" {
				return nil
			}
			candidate = dep.ID
		}
	}
	if candidate == "" {
		return nil
	}
	_, err = c.Store.PatchEntity(ctx, entity.New(
		entity.Ref(entity.DBId, app.ID),
		entity.Ref(core_v1alpha.AppActiveDeploymentId, candidate),
	), entity.WithFromRevision(ent.GetRevision()))
	return err
}

func (c *Controller) reconcileDeployment(ctx context.Context, ent *entity.Entity) error {
	if err := c.Tracker.Reconcile(ctx, string(ent.Id())); err != nil &&
		!errors.Is(err, cond.ErrConflict{}) && !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	return nil
}

func legacyVersion(version, deploymentID string) string {
	if version == "pending-build" || version == "failed-"+deploymentID {
		return ""
	}
	return version
}
