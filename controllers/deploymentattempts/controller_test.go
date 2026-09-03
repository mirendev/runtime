package deploymentattempts

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

type migrationFailStore struct {
	entity.Store
	fail entity.Id
}

type missingEntityStore struct {
	entity.Store
}

func (s *missingEntityStore) GetEntities(context.Context, []entity.Id) ([]*entity.Entity, error) {
	return []*entity.Entity{nil}, nil
}

type markerConflictStore struct {
	entity.Store
	patchCalls int
}

type countingGetStore struct {
	entity.Store
	getEntityCalls int
}

func (s *countingGetStore) GetEntity(ctx context.Context, id entity.Id) (*entity.Entity, error) {
	s.getEntityCalls++
	return s.Store.GetEntity(ctx, id)
}

func (s *markerConflictStore) PatchEntity(ctx context.Context, ent *entity.Entity, opts ...entity.EntityOption) (*entity.Entity, error) {
	if _, ok := ent.Get(core_v1alpha.CloudExportContract.MarkerID()); ok {
		s.patchCalls++
		if s.patchCalls == 1 {
			return nil, cond.Conflict("entity", ent.Id())
		}
	}
	return s.Store.PatchEntity(ctx, ent, opts...)
}

func (s *migrationFailStore) ReplaceEntity(ctx context.Context, ent *entity.Entity, opts ...entity.EntityOption) (*entity.Entity, error) {
	if ent.Id() == s.fail {
		return nil, errors.New("injected migration failure")
	}
	return s.Store.ReplaceEntity(ctx, ent, opts...)
}

func (s *migrationFailStore) PatchEntity(ctx context.Context, ent *entity.Entity, opts ...entity.EntityOption) (*entity.Entity, error) {
	if ent.Id() == s.fail {
		return nil, errors.New("injected migration failure")
	}
	return s.Store.PatchEntity(ctx, ent, opts...)
}

func TestInitialPassMigratesCanonicalGraphAndProvenance(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := &core_v1alpha.App{ID: "app/web", ActiveVersion: "app_version/v1"}
	version := &core_v1alpha.AppVersion{ID: "app_version/v1", App: app.ID, Version: "web-v1"}
	parentID := entity.Id("deployment/parent")
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	dep := &core_v1alpha.Deployment{
		ID: "deployment/legacy", AppName: "web", AppVersion: string(version.ID),
		Status: "active", Phase: "activating", SourceDeploymentId: string(parentID),
		DeployedBy:  core_v1alpha.DeployedBy{Timestamp: started.Format(time.RFC3339)},
		CompletedAt: finished.Format(time.RFC3339),
		GitInfo: core_v1alpha.GitInfo{
			Sha: "abc123", Branch: "main",
			Repository: "https://user:secret@example.com/acme/web.git?token=nope#frag",
		},
	}
	_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, parentID)))
	require.NoError(t, err)
	for _, model := range []interface{ Encode() []entity.Attr }{app, version, dep} {
		attrs := model.Encode()
		var id entity.Id
		switch v := model.(type) {
		case *core_v1alpha.App:
			id = v.ID
		case *core_v1alpha.AppVersion:
			id = v.ID
		case *core_v1alpha.Deployment:
			id = v.ID
		}
		_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, id), attrs))
		require.NoError(t, err)
	}

	controller := New(log, inmem.Store, inmem.EAC)
	controller.Config.BatchSize = 100
	for range 4 {
		require.NoError(t, controller.Step(ctx))
	}
	select {
	case <-controller.Ready():
	default:
		t.Fatal("controller did not report its first clean sweep")
	}

	rawDep, err := inmem.Store.GetEntity(ctx, dep.ID)
	require.NoError(t, err)
	var migratedDep core_v1alpha.Deployment
	migratedDep.Decode(rawDep)
	assert.Equal(t, app.ID, migratedDep.App)
	assert.Equal(t, version.ID, migratedDep.Version)
	assert.Equal(t, string(deploylifecycle.StatusSucceeded), migratedDep.Outcome)
	assert.Equal(t, "activating", migratedDep.Phase)
	assert.Equal(t, started, migratedDep.StartedAt)
	assert.Equal(t, finished.Format(time.RFC3339), migratedDep.CompletedAt)
	assert.Equal(t, parentID, migratedDep.ParentDeployment)
	assert.Empty(t, migratedDep.Operation, "migration must not invent intent")
	assert.Equal(t, "active", migratedDep.Status, "legacy representation is retained")

	rawVersion, err := inmem.Store.GetEntity(ctx, version.ID)
	require.NoError(t, err)
	var migratedVersion core_v1alpha.AppVersion
	migratedVersion.Decode(rawVersion)
	assert.Equal(t, "abc123", migratedVersion.Source.GitSha)
	assert.Equal(t, "main", migratedVersion.Source.GitBranch)
	assert.Equal(t, "https://example.com/acme/web.git", migratedVersion.Source.Repository)

	rawApp, err := inmem.Store.GetEntity(ctx, app.ID)
	require.NoError(t, err)
	var migratedApp core_v1alpha.App
	migratedApp.Decode(rawApp)
	assert.Equal(t, dep.ID, migratedApp.ActiveDeployment)

}

func TestDeploymentMigrationRediscoversProgressAfterRestart(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, id := range []entity.Id{"deployment/one", "deployment/two"} {
		dep := &core_v1alpha.Deployment{ID: id, AppName: "web", Status: "failed"}
		_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, id), dep.Encode()))
		require.NoError(t, err)
	}

	first := New(log, inmem.Store, inmem.EAC)
	first.Config.BatchSize = 1
	require.NoError(t, first.Step(ctx))
	assert.NotEmpty(t, first.cursor)

	// Process-local scan position is deliberately lost. The new controller
	// starts at the front, observes that the first record is already canonical,
	// then advances to the remaining legacy record on its next bounded step.
	second := New(log, inmem.Store, inmem.EAC)
	second.Config.BatchSize = 1
	require.NoError(t, second.Step(ctx))
	require.NoError(t, second.Step(ctx))

	canonical := 0
	for _, id := range []entity.Id{"deployment/one", "deployment/two"} {
		raw, err := inmem.Store.GetEntity(ctx, id)
		require.NoError(t, err)
		var dep core_v1alpha.Deployment
		dep.Decode(raw)
		if dep.Outcome == "failed" {
			canonical++
		}
	}
	assert.Equal(t, 2, canonical)
}

func TestVersionMigrationIgnoresDeploymentsWithoutProvenance(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	version := &core_v1alpha.AppVersion{ID: "app_version/v1", App: "app/web", Version: "web-v1"}
	deployments := []*core_v1alpha.Deployment{
		{ID: "deployment/a-no-source", Version: version.ID},
		{
			ID: "deployment/z-with-source", Version: version.ID,
			GitInfo: core_v1alpha.GitInfo{
				Sha: "abc123", Branch: "main", Repository: "https://example.com/acme/web.git",
			},
		},
	}
	_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, version.ID), version.Encode()))
	require.NoError(t, err)
	for _, deployment := range deployments {
		_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, deployment.ID), deployment.Encode()))
		require.NoError(t, err)
	}

	raw, err := inmem.Store.GetEntity(ctx, version.ID)
	require.NoError(t, err)
	controller := New(log, inmem.Store, inmem.EAC)
	require.NoError(t, controller.migrateVersion(ctx, raw))

	raw, err = inmem.Store.GetEntity(ctx, version.ID)
	require.NoError(t, err)
	var migrated core_v1alpha.AppVersion
	migrated.Decode(raw)
	assert.Equal(t, "abc123", migrated.Source.GitSha)
	assert.Equal(t, "main", migrated.Source.GitBranch)
	assert.Equal(t, "https://example.com/acme/web.git", migrated.Source.Repository)
}

func TestMigrationFailureDoesNotBlockLaterPhases(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dep := &core_v1alpha.Deployment{ID: "deployment/broken", AppName: "web", Status: "failed"}
	_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, dep.ID), dep.Encode()))
	require.NoError(t, err)

	failing := &migrationFailStore{Store: inmem.Store, fail: dep.ID}
	controller := New(log, failing, inmem.EAC)
	controller.Config.BatchSize = 100
	require.Error(t, controller.Step(ctx))
	for range 3 {
		require.NoError(t, controller.Step(ctx))
	}
	select {
	case <-controller.Ready():
		t.Fatal("controller reported readiness after a failed sweep")
	default:
	}
	assert.Equal(t, phaseDeployments, controller.phase,
		"a bad record must not prevent later phases from running")

	failing.fail = ""
	for range 4 {
		require.NoError(t, controller.Step(ctx))
	}
	select {
	case <-controller.Ready():
	default:
		t.Fatal("controller did not report readiness after a clean retry")
	}
}

func TestCleanSweepMarksLegacyEntitiesForExport(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	marker := core_v1alpha.CloudExportContract.MarkerID()
	dep := &core_v1alpha.Deployment{
		ID: "deployment/legacy-unmarked", AppName: "web", Status: "failed",
	}
	attrs := dep.Encode()
	attrs = slices.DeleteFunc(attrs, func(attr entity.Attr) bool { return attr.ID == marker })
	_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, dep.ID), attrs))
	require.NoError(t, err)

	controller := New(log, inmem.Store, inmem.EAC)
	controller.Config.BatchSize = 100
	for range 4 {
		require.NoError(t, controller.Step(ctx))
	}
	select {
	case <-controller.Ready():
	default:
		t.Fatal("controller did not report its clean sweep")
	}

	migrated, err := inmem.Store.GetEntity(ctx, dep.ID)
	require.NoError(t, err)
	attr, ok := migrated.Get(marker)
	require.True(t, ok)
	assert.True(t, attr.Value.Bool())
}

func TestMigrationSkipsEntityRemovedAfterIndexRead(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	dep := &core_v1alpha.Deployment{ID: "deployment/disappeared", Status: "failed"}
	_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, dep.ID), dep.Encode()))
	require.NoError(t, err)

	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), &missingEntityStore{Store: inmem.Store}, inmem.EAC)
	require.NoError(t, controller.Step(ctx))
	require.Equal(t, phaseVersions, controller.phase)
}

func TestMigrationMarkerWriteRetriesRevisionConflict(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	app := &core_v1alpha.App{ID: "app/web"}
	source := entity.New(entity.Ref(entity.DBId, app.ID), app.Encode())
	source.Remove(core_v1alpha.CloudExportContract.MarkerID())
	_, err := inmem.Store.CreateEntity(ctx, source)
	require.NoError(t, err)

	store := &markerConflictStore{Store: inmem.Store}
	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), store, inmem.EAC)
	controller.phase = phaseApps
	require.NoError(t, controller.Step(ctx))
	require.Equal(t, 2, store.patchCalls)

	stored, err := inmem.Store.GetEntity(ctx, app.ID)
	require.NoError(t, err)
	require.True(t, entity.MustGet(stored, core_v1alpha.CloudExportContract.MarkerID()).Value.Bool())
}

func TestMigrationSkipsMarkerReadForMarkedEntity(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	app := &core_v1alpha.App{ID: "app/already-marked"}
	_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, app.ID), app.Encode()))
	require.NoError(t, err)

	store := &countingGetStore{Store: inmem.Store}
	controller := New(slog.New(slog.NewTextHandler(io.Discard, nil)), store, inmem.EAC)
	controller.phase = phaseApps
	require.NoError(t, controller.Step(ctx))
	require.Zero(t, store.getEntityCalls)
}

func TestUnknownLegacyDeploymentKeepsExportGateClosed(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	marker := core_v1alpha.CloudExportContract.MarkerID()
	dep := &core_v1alpha.Deployment{
		ID: "deployment/future-legacy", AppName: "web", Status: "future-state",
	}
	attrs := dep.Encode()
	attrs = slices.DeleteFunc(attrs, func(attr entity.Attr) bool { return attr.ID == marker })
	_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, dep.ID), attrs))
	require.NoError(t, err)

	controller := New(log, inmem.Store, inmem.EAC)
	controller.Config.BatchSize = 100
	require.ErrorContains(t, controller.Step(ctx), "unknown legacy status")
	for range 3 {
		require.NoError(t, controller.Step(ctx))
	}
	select {
	case <-controller.Ready():
		t.Fatal("controller opened the export gate with an old-style deployment remaining")
	default:
	}

	unmigrated, err := inmem.Store.GetEntity(ctx, dep.ID)
	require.NoError(t, err)
	_, marked := unmigrated.Get(marker)
	assert.False(t, marked)
}

func TestMigrationLeavesRunningAttemptWithoutOutcome(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	app := &core_v1alpha.App{ID: "app/web"}
	dep := &core_v1alpha.Deployment{
		ID: "deployment/running", AppName: "web", Status: "in_progress", Phase: "building",
		DeployedBy: core_v1alpha.DeployedBy{Timestamp: started.Format(time.RFC3339)},
	}
	for id, attrs := range map[entity.Id][]entity.Attr{app.ID: app.Encode(), dep.ID: dep.Encode()} {
		_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, id), attrs))
		require.NoError(t, err)
	}

	controller := New(log, inmem.Store, inmem.EAC)
	require.NoError(t, controller.Step(ctx))

	raw, err := inmem.Store.GetEntity(ctx, dep.ID)
	require.NoError(t, err)
	var migrated core_v1alpha.Deployment
	migrated.Decode(raw)
	rec := &deploylifecycle.Record{Deployment: &migrated}
	assert.True(t, rec.Canonical())
	assert.Equal(t, deploylifecycle.StatusInProgress, rec.Status())
	assert.Empty(t, migrated.Outcome)
	assert.Equal(t, "building", migrated.Phase)
	assert.Equal(t, started, migrated.StartedAt)
	assert.Equal(t, app.ID, migrated.App)
}

func TestDeploymentMigrationEvictsEmbeddedBuildLogsFromCanonicalRecord(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Simulate a store upgraded from a schema that allowed the legacy attr.
	_, err := inmem.Store.CreateEntity(ctx, entity.New(
		entity.Ident, types.Keyword(legacyDeploymentBuildLogs),
		entity.Doc, "legacy embedded build output",
		entity.Cardinality, entity.CardinalityOne,
		entity.Type, entity.TypeStr,
	), entity.WithOverwrite)
	require.NoError(t, err)

	dep := &core_v1alpha.Deployment{
		ID: "deployment/canonical-with-logs", AppName: "web",
		Status: "failed", Outcome: "failed", StartedAt: time.Now().UTC(),
	}
	attrs := append(dep.Encode(), entity.String(legacyDeploymentBuildLogs, strings.Repeat("build output\n", 10_000)))
	_, err = inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, dep.ID), attrs))
	require.NoError(t, err)

	controller := New(log, inmem.Store, inmem.EAC)
	require.NoError(t, controller.Step(ctx))

	raw, err := inmem.Store.GetEntity(ctx, dep.ID)
	require.NoError(t, err)
	_, found := raw.Get(legacyDeploymentBuildLogs)
	assert.False(t, found, "canonical records must not retain legacy embedded logs")

	var migrated core_v1alpha.Deployment
	migrated.Decode(raw)
	assert.Equal(t, "failed", migrated.Outcome)
}

func TestReconcilePhaseDiscoversRunningAttemptDirectly(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	controller := New(log, inmem.Store, inmem.EAC)

	app := &core_v1alpha.App{ID: "app/web"}
	version := &core_v1alpha.AppVersion{ID: "app_version/v1", App: app.ID}
	for id, attrs := range map[entity.Id][]entity.Attr{app.ID: app.Encode(), version.ID: version.Encode()} {
		_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, id), attrs))
		require.NoError(t, err)
	}

	rec, err := controller.Tracker.Begin(ctx, deploylifecycle.BeginParams{AppName: "web"})
	require.NoError(t, err)
	require.NoError(t, controller.Tracker.SetAppVersion(ctx, string(rec.Deployment.ID), string(version.ID)))

	current, revision, err := controller.Tracker.Store().AppByName(ctx, "web")
	require.NoError(t, err)
	require.NoError(t, controller.Tracker.Store().SetActivePointers(
		ctx, current.ID, revision, version.ID, rec.Deployment.ID))

	controller.phase = phaseReconcile
	controller.Config.BatchSize = 100
	require.NoError(t, controller.Step(ctx))

	settled, err := controller.Tracker.Store().Get(ctx, string(rec.Deployment.ID))
	require.NoError(t, err)
	assert.Equal(t, deploylifecycle.StatusSucceeded, settled.Status())
	assert.Equal(t, "succeeded", settled.Deployment.Outcome)
}

func TestReupgradeRepairsPointerChangedByDowngradedRuntime(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	app := &core_v1alpha.App{ID: "app/web", ActiveVersion: "app_version/v2", ActiveDeployment: "deployment/old"}
	v1 := &core_v1alpha.AppVersion{ID: "app_version/v1", App: app.ID}
	v2 := &core_v1alpha.AppVersion{ID: "app_version/v2", App: app.ID}
	old := &core_v1alpha.Deployment{
		ID: "deployment/old", App: app.ID, Version: v1.ID,
		Outcome: "succeeded", AppName: "web", AppVersion: string(v1.ID), Status: "succeeded",
	}
	current := &core_v1alpha.Deployment{
		ID: "deployment/current", AppName: "web", AppVersion: string(v2.ID), Status: "active",
	}
	for id, attrs := range map[entity.Id][]entity.Attr{
		app.ID: app.Encode(), v1.ID: v1.Encode(), v2.ID: v2.Encode(),
		old.ID: old.Encode(), current.ID: current.Encode(),
	} {
		_, err := inmem.Store.CreateEntity(ctx, entity.New(entity.Ref(entity.DBId, id), attrs))
		require.NoError(t, err)
	}

	controller := New(log, inmem.Store, inmem.EAC)
	controller.Config.BatchSize = 100
	for range 3 {
		require.NoError(t, controller.Step(ctx))
	}
	raw, err := inmem.Store.GetEntity(ctx, app.ID)
	require.NoError(t, err)
	var got core_v1alpha.App
	got.Decode(raw)
	assert.Equal(t, current.ID, got.ActiveDeployment)
}
