package export_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	entityexport "miren.dev/runtime/pkg/entity/export"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestBackfillMarkerSelectsOnlyContractKinds(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	models := []struct {
		id    entity.Id
		attrs []entity.Attr
	}{
		{"app/web", (&core_v1alpha.App{ID: "app/web"}).Encode()},
		{"app_version/v1", (&core_v1alpha.AppVersion{ID: "app_version/v1", App: "app/web"}).Encode()},
		{"deployment/d1", (&core_v1alpha.Deployment{ID: "deployment/d1", App: "app/web"}).Encode()},
		{"project/default", (&core_v1alpha.Project{ID: "project/default"}).Encode()},
	}
	marker := core_v1alpha.CloudExportContract.MarkerID()
	for _, model := range models {
		source := entity.New(entity.Ref(entity.DBId, model.id), model.attrs)
		source.Remove(marker) // Simulate an entity written before export existed.
		_, err := inmem.Store.CreateEntity(ctx, source)
		require.NoError(t, err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stats, err := entityexport.BackfillMarker(ctx, log, inmem.Store, core_v1alpha.CloudExportContract, 1)
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.Scanned)
	require.Equal(t, int64(3), stats.Marked)

	for _, model := range models[:3] {
		stored, err := inmem.Store.GetEntity(ctx, model.id)
		require.NoError(t, err)
		require.True(t, entity.MustGet(stored, marker).Value.Bool())
	}
	project, err := inmem.Store.GetEntity(ctx, models[3].id)
	require.NoError(t, err)
	_, ok := project.Get(marker)
	require.False(t, ok)

	stats, err = entityexport.BackfillMarker(ctx, log, inmem.Store, core_v1alpha.CloudExportContract, 2)
	require.NoError(t, err)
	require.Zero(t, stats.Marked)
	require.Equal(t, int64(3), stats.AlreadyMarked)
}

func TestBackfillMarkerCanLeaveDeploymentSelectionToItsMigration(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	marker := core_v1alpha.CloudExportContract.MarkerID()
	deployment := &core_v1alpha.Deployment{ID: "deployment/legacy", AppName: "web", Status: "failed"}
	source := entity.New(entity.Ref(entity.DBId, deployment.ID), deployment.Encode())
	source.Remove(marker)
	_, err := inmem.Store.CreateEntity(ctx, source)
	require.NoError(t, err)

	stats, err := entityexport.BackfillMarker(
		ctx,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		inmem.Store,
		core_v1alpha.CloudExportContract,
		1,
		entityexport.ExcludingKinds(core_v1alpha.KindDeployment),
	)
	require.NoError(t, err)
	require.Zero(t, stats.Scanned)
	stored, err := inmem.Store.GetEntity(ctx, deployment.ID)
	require.NoError(t, err)
	_, marked := stored.Get(marker)
	require.False(t, marked)
}
