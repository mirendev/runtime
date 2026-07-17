package version

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	compute_v1alpha "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	testutils "miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

// createVersion creates an AppVersion with an explicit creation time. The
// entity store honors a pre-set CreatedAt, which lets these tests assert
// recency-based retention deterministically.
func createVersion(t *testing.T, eac *entityserver_v1alpha.EntityAccessClient, name string, av *core_v1alpha.AppVersion, createdAt time.Time) entity.Id {
	t.Helper()
	ent := entity.New(
		(&core_v1alpha.Metadata{Name: name}).Encode,
		av.Encode,
		entity.Ident, types.Keyword(av.ShortKind()+"/"+name),
	)
	ent.SetCreatedAt(createdAt)

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(ent.Attrs())
	pr, err := eac.Put(context.Background(), &rpcE)
	require.NoError(t, err)
	return entity.Id(pr.Id())
}

func setActiveVersion(t *testing.T, eac *entityserver_v1alpha.EntityAccessClient, appID, versionID entity.Id) {
	t.Helper()
	res, err := eac.Get(context.Background(), appID.String())
	require.NoError(t, err)
	patch := entity.New(
		entity.Ref(entity.DBId, appID),
		(&core_v1alpha.App{ActiveVersion: versionID}).Encode,
	)
	_, err = eac.Patch(context.Background(), patch.Attrs(), res.Entity().Revision())
	require.NoError(t, err)
}

func versionExists(t *testing.T, eac *entityserver_v1alpha.EntityAccessClient, id entity.Id) bool {
	t.Helper()
	// A hard-deleted entity surfaces as a not-found error rather than an empty
	// result, so treat any lookup failure as "gone".
	res, err := eac.Get(context.Background(), id.String())
	if err != nil {
		return false
	}
	return res.HasEntity()
}

func TestGCController_RetentionCountAndActive(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "gcapp", &core_v1alpha.App{})
	require.NoError(t, err)

	now := time.Now()
	// Five versions, v1 oldest .. v5 newest, all dated in the past so a zero
	// RetentionPeriod cutoff (now) leaves age-based retention out of it.
	ids := make([]entity.Id, 5)
	for i := range 5 {
		name := "v" + string(rune('1'+i))
		ids[i] = createVersion(t, inmem.EAC, name,
			&core_v1alpha.AppVersion{App: appID, Version: name},
			now.Add(time.Duration(i-5)*time.Hour))
	}

	// Make an old version (v2) the active one; it must be kept even though it
	// falls outside the most-recent count.
	setActiveVersion(t, inmem.EAC, appID, ids[1])

	gc := &GCController{
		Log: log,
		EAC: inmem.EAC,
		Config: GCConfig{
			CheckInterval:   time.Hour,
			RetentionCount:  2, // keep v5, v4
			RetentionPeriod: 0, // disable age-based retention
		},
	}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	// Kept: v5, v4 (count) + v2 (active). Pruned: v3, v1.
	require.Equal(t, 2, result.DeletedVersions)
	require.Equal(t, 3, result.RetainedVersions)
	require.Equal(t, 5, result.TotalScanned)

	require.True(t, versionExists(t, inmem.EAC, ids[4]), "newest kept by count")
	require.True(t, versionExists(t, inmem.EAC, ids[3]), "second-newest kept by count")
	require.True(t, versionExists(t, inmem.EAC, ids[1]), "active version kept")
	require.False(t, versionExists(t, inmem.EAC, ids[2]), "v3 pruned")
	require.False(t, versionExists(t, inmem.EAC, ids[0]), "v1 pruned")
}

func TestGCController_RetentionPeriod(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "daysapp", &core_v1alpha.App{})
	require.NoError(t, err)

	now := time.Now()
	recent := createVersion(t, inmem.EAC, "recent",
		&core_v1alpha.AppVersion{App: appID, Version: "recent"}, now.Add(-5*24*time.Hour))
	old := createVersion(t, inmem.EAC, "old",
		&core_v1alpha.AppVersion{App: appID, Version: "old"}, now.Add(-40*24*time.Hour))

	gc := &GCController{
		Log: log,
		EAC: inmem.EAC,
		Config: GCConfig{
			CheckInterval:   time.Hour,
			RetentionCount:  0,                   // disable count-based retention
			RetentionPeriod: 30 * 24 * time.Hour, // keep anything younger than 30 days
		},
	}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	require.Equal(t, 1, result.DeletedVersions)
	require.True(t, versionExists(t, inmem.EAC, recent), "version within retention window kept")
	require.False(t, versionExists(t, inmem.EAC, old), "version older than retention window pruned")
}

func TestGCController_DiskPressureDropsPeriodFloor(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "pressureapp", &core_v1alpha.App{})
	require.NoError(t, err)

	now := time.Now()
	// Four versions, all well within the retention period, so the age floor
	// alone would retain every one of them.
	ids := make([]entity.Id, 4)
	for i := range 4 {
		name := "v" + string(rune('1'+i))
		ids[i] = createVersion(t, inmem.EAC, name,
			&core_v1alpha.AppVersion{App: appID, Version: name},
			now.Add(time.Duration(i-4)*time.Hour))
	}

	newController := func(storagePercent float64) *GCController {
		return &GCController{
			Log: log,
			EAC: inmem.EAC,
			Config: GCConfig{
				CheckInterval:         time.Hour,
				RetentionCount:        1,
				RetentionPeriod:       30 * 24 * time.Hour,
				DiskPressureThreshold: 80,
			},
			storagePercent: func() float64 { return storagePercent },
		}
	}

	// Below threshold: the period floor keeps all four despite RetentionCount=1.
	result, err := newController(50).RunGC(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, result.DeletedVersions, "no pruning below disk pressure threshold")
	require.Equal(t, 4, result.RetainedVersions)

	// At/above threshold: the period floor is dropped, collapsing retention to
	// the single most-recent version (RetentionCount=1).
	result, err = newController(85).RunGC(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, result.DeletedVersions, "period floor dropped under pressure")
	require.Equal(t, 1, result.RetainedVersions)
	require.True(t, versionExists(t, inmem.EAC, ids[3]), "most-recent version kept by count under pressure")
	require.False(t, versionExists(t, inmem.EAC, ids[0]), "older recent version pruned under pressure")
}

func TestGCController_SkipsEphemeral(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "ephapp", &core_v1alpha.App{})
	require.NoError(t, err)

	now := time.Now()
	// An old ephemeral version that would be pruned if it were normal.
	eph := createVersion(t, inmem.EAC, "eph",
		&core_v1alpha.AppVersion{
			App:                appID,
			Version:            "eph",
			EphemeralLabel:     "pr-1",
			EphemeralExpiresAt: now.Add(48 * time.Hour),
		}, now.Add(-90*24*time.Hour))

	gc := &GCController{
		Log:    log,
		EAC:    inmem.EAC,
		Config: GCConfig{CheckInterval: time.Hour, RetentionCount: 0, RetentionPeriod: 0},
	}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	require.Equal(t, 0, result.TotalScanned, "ephemeral versions are not scanned")
	require.Equal(t, 0, result.DeletedVersions)
	require.True(t, versionExists(t, inmem.EAC, eph), "ephemeral version untouched by retention GC")
}

func TestGCController_SkipsLiveSandbox(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "liveapp", &core_v1alpha.App{})
	require.NoError(t, err)

	now := time.Now()
	// Two prunable old versions.
	withSandbox := createVersion(t, inmem.EAC, "with-sb",
		&core_v1alpha.AppVersion{App: appID, Version: "with-sb"}, now.Add(-90*24*time.Hour))
	noSandbox := createVersion(t, inmem.EAC, "no-sb",
		&core_v1alpha.AppVersion{App: appID, Version: "no-sb"}, now.Add(-91*24*time.Hour))

	// A running sandbox pins withSandbox.
	sb := &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING}
	sb.Spec.Version = withSandbox
	_, err = inmem.Client.Create(ctx, "sb1", sb)
	require.NoError(t, err)

	gc := &GCController{
		Log:    log,
		EAC:    inmem.EAC,
		Config: GCConfig{CheckInterval: time.Hour, RetentionCount: 0, RetentionPeriod: 0},
	}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	require.Equal(t, 1, result.DeletedVersions)
	require.Equal(t, 1, result.SkippedLive)
	require.True(t, versionExists(t, inmem.EAC, withSandbox), "version with live sandbox retained")
	require.False(t, versionExists(t, inmem.EAC, noSandbox), "version without sandbox pruned")
}

func TestGCController_PinnedNow(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	gc := &GCController{Log: testutils.TestLogger(t), EAC: inmem.EAC, Config: DefaultGCConfig()}

	appID, err := inmem.Client.Create(ctx, "pinapp", &core_v1alpha.App{})
	require.NoError(t, err)

	now := time.Now()
	vActive := createVersion(t, inmem.EAC, "v-active", &core_v1alpha.AppVersion{App: appID, Version: "v-active"}, now)
	vLive := createVersion(t, inmem.EAC, "v-live", &core_v1alpha.AppVersion{App: appID, Version: "v-live"}, now)
	vFree := createVersion(t, inmem.EAC, "v-free", &core_v1alpha.AppVersion{App: appID, Version: "v-free"}, now)

	setActiveVersion(t, inmem.EAC, appID, vActive)

	sb := &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING}
	sb.Spec.Version = vLive
	_, err = inmem.Client.Create(ctx, "sb-live", sb)
	require.NoError(t, err)

	pinned, err := gc.pinnedNow(ctx, vActive, appID)
	require.NoError(t, err)
	require.True(t, pinned, "active version is pinned")

	pinned, err = gc.pinnedNow(ctx, vLive, appID)
	require.NoError(t, err)
	require.True(t, pinned, "version with a live sandbox is pinned")

	pinned, err = gc.pinnedNow(ctx, vFree, appID)
	require.NoError(t, err)
	require.False(t, pinned, "unreferenced version is not pinned")
}

func TestGCController_DeletesConfigVersion(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()
	log := testutils.TestLogger(t)

	appID, err := inmem.Client.Create(ctx, "cfgapp", &core_v1alpha.App{})
	require.NoError(t, err)

	cvID, err := inmem.Client.Create(ctx, "old-cfg", &core_v1alpha.ConfigVersion{})
	require.NoError(t, err)

	old := createVersion(t, inmem.EAC, "old",
		&core_v1alpha.AppVersion{App: appID, Version: "old", ConfigVersion: cvID},
		time.Now().Add(-90*24*time.Hour))

	gc := &GCController{
		Log:    log,
		EAC:    inmem.EAC,
		Config: GCConfig{CheckInterval: time.Hour, RetentionCount: 0, RetentionPeriod: 0},
	}

	result, err := gc.RunGC(ctx)
	require.NoError(t, err)

	require.Equal(t, 1, result.DeletedVersions)
	require.False(t, versionExists(t, inmem.EAC, old), "version pruned")
	require.False(t, versionExists(t, inmem.EAC, cvID), "config version cascade-deleted")
}
