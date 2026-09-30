package artifact

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	testutils "miren.dev/runtime/pkg/entity/testutils"
)

func artifactStatus(t *testing.T, eac *entityserver_v1alpha.EntityAccessClient, id entity.Id) core_v1alpha.ArtifactStatus {
	t.Helper()
	res, err := eac.Get(context.Background(), id.String())
	require.NoError(t, err)
	require.True(t, res.HasEntity())
	var art core_v1alpha.Artifact
	art.Decode(res.Entity().Entity())
	return art.Status
}

func TestGCController_ArchivesUnreferenced(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "gcapp", &core_v1alpha.App{})
	require.NoError(t, err)

	// An active artifact that no version references.
	artID, err := inmem.Client.Create(ctx, "orphan", &core_v1alpha.Artifact{App: appID, Status: core_v1alpha.ACTIVE})
	require.NoError(t, err)

	gc := &GCController{Log: log, EAC: inmem.EAC, Config: GCConfig{CheckInterval: time.Hour}}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	require.Equal(t, 1, len(result.ArchivedArtifacts))
	require.Equal(t, 0, result.RetainedArtifacts)
	require.Equal(t, core_v1alpha.ARCHIVED, artifactStatus(t, inmem.EAC, artID))
}

func TestGCController_RetainsReferenced(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "gcapp", &core_v1alpha.App{})
	require.NoError(t, err)

	artID, err := inmem.Client.Create(ctx, "live", &core_v1alpha.Artifact{App: appID, Status: core_v1alpha.ACTIVE})
	require.NoError(t, err)

	// A version references the artifact, so it must stay active.
	_, err = inmem.Client.Create(ctx, "v1", &core_v1alpha.AppVersion{App: appID, Version: "v1", Artifact: artID})
	require.NoError(t, err)

	gc := &GCController{Log: log, EAC: inmem.EAC, Config: GCConfig{CheckInterval: time.Hour}}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	require.Equal(t, 0, len(result.ArchivedArtifacts))
	require.Equal(t, 1, result.RetainedArtifacts)
	require.Equal(t, core_v1alpha.ACTIVE, artifactStatus(t, inmem.EAC, artID))
}

func TestGCController_RetainsEphemeralReferenced(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "gcapp", &core_v1alpha.App{})
	require.NoError(t, err)

	artID, err := inmem.Client.Create(ctx, "eph-art", &core_v1alpha.Artifact{App: appID, Status: core_v1alpha.ACTIVE})
	require.NoError(t, err)

	// Only an ephemeral version references it — its image is still needed.
	_, err = inmem.Client.Create(ctx, "eph", &core_v1alpha.AppVersion{
		App:                appID,
		Version:            "eph",
		Artifact:           artID,
		EphemeralLabel:     "pr-1",
		EphemeralExpiresAt: time.Now().Add(48 * time.Hour),
	})
	require.NoError(t, err)

	gc := &GCController{Log: log, EAC: inmem.EAC, Config: GCConfig{CheckInterval: time.Hour}}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	require.Equal(t, 0, len(result.ArchivedArtifacts))
	require.Equal(t, core_v1alpha.ACTIVE, artifactStatus(t, inmem.EAC, artID),
		"artifact referenced by an ephemeral version must stay active")
}

func TestGCController_SkipsArchived(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "gcapp", &core_v1alpha.App{})
	require.NoError(t, err)

	// Already archived: the active-status index should not surface it at all.
	_, err = inmem.Client.Create(ctx, "done", &core_v1alpha.Artifact{App: appID, Status: core_v1alpha.ARCHIVED})
	require.NoError(t, err)

	gc := &GCController{Log: log, EAC: inmem.EAC, Config: GCConfig{CheckInterval: time.Hour}}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	require.Equal(t, 0, result.TotalArtifacts, "archived artifacts are not evaluated")
	require.Equal(t, 0, len(result.ArchivedArtifacts))
}

func TestGCController_RetainsSystemArtifacts(t *testing.T) {
	// The toolchain image miren builds for itself belongs to no app, so the
	// AppVersion set will never name it. Collecting it would delete blobs out
	// from under nodes still pulling the image.
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	sysID, err := inmem.Client.Create(ctx,
		core_v1alpha.SystemArtifactPrefix+"lbd-builder-21c0e11624c12f31",
		&core_v1alpha.Artifact{Status: core_v1alpha.ACTIVE})
	require.NoError(t, err)

	// A genuine orphan alongside it, to prove the exemption is narrow and not
	// just "artifacts with no app survive".
	orphanID, err := inmem.Client.Create(ctx, "orphan", &core_v1alpha.Artifact{Status: core_v1alpha.ACTIVE})
	require.NoError(t, err)

	gc := &GCController{Log: log, EAC: inmem.EAC, Config: GCConfig{CheckInterval: time.Hour}}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	require.Equal(t, 1, result.RetainedArtifacts)
	require.Equal(t, []entity.Id{orphanID}, result.ArchivedArtifacts)
	require.Equal(t, core_v1alpha.ACTIVE, artifactStatus(t, inmem.EAC, sysID))
	require.Equal(t, core_v1alpha.ARCHIVED, artifactStatus(t, inmem.EAC, orphanID))
}
