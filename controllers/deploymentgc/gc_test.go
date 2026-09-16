package deploymentgc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/deploylifecycle"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

type fixture struct {
	eac   *entityserver_v1alpha.EntityAccessClient
	store *deploylifecycle.Store
	now   time.Time
	gc    *GCController
}

func newFixture(t *testing.T, config GCConfig) *fixture {
	t.Helper()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := testutils.TestLogger(t)

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	gc := &GCController{Log: log, EAC: inmem.EAC, Config: config}
	gc.now = func() time.Time { return now }
	return &fixture{
		eac:   inmem.EAC,
		store: deploylifecycle.NewStore(log, inmem.EAC),
		now:   now,
		gc:    gc,
	}
}

func (f *fixture) createApp(t *testing.T, name string) entity.Id {
	t.Helper()
	app, _, err := f.store.EnsureApp(context.Background(), name)
	require.NoError(t, err)
	return app.ID
}

// settled writes a canonical, finished deployment that started `age` ago.
func (f *fixture) settled(t *testing.T, app string, age time.Duration) string {
	t.Helper()
	rec, err := f.store.Create(context.Background(), &core_v1alpha.Deployment{
		App:       entity.Id("app/" + app),
		AppName:   app,
		Operation: string(deploylifecycle.OperationBuild),
		Outcome:   string(deploylifecycle.StatusSucceeded),
		StartedAt: f.now.Add(-age),
	})
	require.NoError(t, err)
	return string(rec.Deployment.ID)
}

// inProgress writes a canonical deployment with no outcome yet.
func (f *fixture) inProgress(t *testing.T, app string, age time.Duration) string {
	t.Helper()
	rec, err := f.store.Create(context.Background(), &core_v1alpha.Deployment{
		App:       entity.Id("app/" + app),
		AppName:   app,
		Operation: string(deploylifecycle.OperationBuild),
		StartedAt: f.now.Add(-age),
	})
	require.NoError(t, err)
	return string(rec.Deployment.ID)
}

// legacy writes a record the way pre-canonical clients did: status only, with
// the start time living in deployed_by.timestamp.
func (f *fixture) legacy(t *testing.T, app string, status deploylifecycle.Status, age time.Duration) string {
	t.Helper()
	rec, err := f.store.Create(context.Background(), &core_v1alpha.Deployment{
		AppName: app,
		Status:  string(status),
		DeployedBy: core_v1alpha.DeployedBy{
			Timestamp: f.now.Add(-age).Format(time.RFC3339),
		},
	})
	require.NoError(t, err)
	return string(rec.Deployment.ID)
}

func (f *fixture) patchApp(t *testing.T, appID entity.Id, attrs ...entity.Attr) {
	t.Helper()
	ctx := context.Background()
	res, err := f.eac.Get(ctx, appID.String())
	require.NoError(t, err)
	attrs = append([]entity.Attr{entity.Ref(entity.DBId, appID)}, attrs...)
	_, err = f.eac.Patch(ctx, attrs, res.Entity().Revision())
	require.NoError(t, err)
}

func (f *fixture) setActive(t *testing.T, appID entity.Id, deploymentID string) {
	t.Helper()
	f.patchApp(t, appID, entity.Ref(core_v1alpha.AppActiveDeploymentId, entity.Id(deploymentID)))
}

func (f *fixture) setLockHolder(t *testing.T, appID entity.Id, deploymentID string) {
	t.Helper()
	lock := core_v1alpha.DeploymentLock{
		DeploymentId: deploymentID,
		AcquiredAt:   f.now,
		ExpiresAt:    f.now.Add(time.Hour),
	}
	f.patchApp(t, appID, entity.Component(core_v1alpha.AppDeploymentLockId, lock.Encode()))
}

func (f *fixture) exists(t *testing.T, id string) bool {
	t.Helper()
	res, err := f.eac.Get(context.Background(), id)
	if err != nil {
		return false
	}
	return res.HasEntity()
}

const day = 24 * time.Hour

func TestRunGC_RetentionCountKeepsNewestAndPinned(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 2, RetentionPeriod: time.Hour})
	appID := f.createApp(t, "web")

	// d1 oldest .. d6 newest, all well past the retention period.
	ids := make([]string, 6)
	for i := range ids {
		ids[i] = f.settled(t, "web", time.Duration(10-i)*day)
	}
	f.setActive(t, appID, ids[0])
	f.setLockHolder(t, appID, ids[2])

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	// Kept: d6, d5 by count; d1 as active; d3 as lock holder. Pruned: d2, d4.
	require.Equal(t, 6, result.TotalScanned)
	require.Equal(t, 4, result.RetainedRecords)
	require.Equal(t, 2, result.DeletedRecords)
	require.Equal(t, 0, result.FailedRecords)

	require.True(t, f.exists(t, ids[5]), "newest kept by count")
	require.True(t, f.exists(t, ids[4]), "second newest kept by count")
	require.True(t, f.exists(t, ids[0]), "active deployment kept")
	require.True(t, f.exists(t, ids[2]), "lock holder kept")
	require.False(t, f.exists(t, ids[1]), "d2 pruned")
	require.False(t, f.exists(t, ids[3]), "d4 pruned")
}

func TestRunGC_RetentionPeriodKeepsRecent(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 1, RetentionPeriod: 30 * day})
	f.createApp(t, "web")

	newest := f.settled(t, "web", 1*day)
	recent := f.settled(t, "web", 20*day)
	old := f.settled(t, "web", 45*day)

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, result.DeletedRecords)
	require.True(t, f.exists(t, newest), "kept by count")
	require.True(t, f.exists(t, recent), "kept by age")
	require.False(t, f.exists(t, old), "outside both floors")
}

func TestRunGC_NeverDeletesInProgress(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 1, RetentionPeriod: time.Hour})
	f.createApp(t, "web")

	newest := f.settled(t, "web", 1*day)
	stale := f.inProgress(t, "web", 90*day)
	legacyStale := f.legacy(t, "web", deploylifecycle.StatusInProgress, 90*day)
	old := f.settled(t, "web", 60*day)

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, result.DeletedRecords)
	require.True(t, f.exists(t, newest))
	require.True(t, f.exists(t, stale), "in-progress record is reconciliation's to settle")
	require.True(t, f.exists(t, legacyStale), "legacy in-progress record is reconciliation's to settle")
	require.False(t, f.exists(t, old))
}

func TestRunGC_AgesLegacyRecordsByDeployedTimestamp(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 1, RetentionPeriod: 30 * day})
	f.createApp(t, "web")

	newest := f.settled(t, "web", 1*day)
	legacyRecent := f.legacy(t, "web", deploylifecycle.StatusFailed, 10*day)
	legacyOld := f.legacy(t, "web", deploylifecycle.StatusRolledBack, 45*day)

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, result.DeletedRecords)
	require.True(t, f.exists(t, newest))
	require.True(t, f.exists(t, legacyRecent), "legacy record within the period kept")
	require.False(t, f.exists(t, legacyOld), "legacy record past the period pruned")
}

func TestRunGC_KeepsLegacyActiveRecordsWhenPointerUnresolved(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 1, RetentionPeriod: time.Hour})
	f.createApp(t, "web")

	newest := f.settled(t, "web", 1*day)
	// Two legacy rows both claim active, which is the case the migration
	// declines to resolve. Neither is pinned by app.active_deployment.
	activeA := f.legacy(t, "web", deploylifecycle.StatusActive, 60*day)
	activeB := f.legacy(t, "web", deploylifecycle.StatusActive, 70*day)
	old := f.legacy(t, "web", deploylifecycle.StatusFailed, 80*day)

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, result.DeletedRecords)
	require.True(t, f.exists(t, newest))
	require.True(t, f.exists(t, activeA), "legacy active row still explains serving state")
	require.True(t, f.exists(t, activeB), "legacy active row still explains serving state")
	require.False(t, f.exists(t, old))
}

func TestRunGC_CountsPerApp(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 1, RetentionPeriod: time.Hour})
	f.createApp(t, "web")
	f.createApp(t, "api")

	webNew := f.settled(t, "web", 5*day)
	webOld := f.settled(t, "web", 6*day)
	apiNew := f.settled(t, "api", 7*day)
	apiOld := f.settled(t, "api", 8*day)

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	require.Equal(t, 2, result.DeletedRecords)
	require.True(t, f.exists(t, webNew))
	require.True(t, f.exists(t, apiNew), "api's newest kept even though it is older than web's history")
	require.False(t, f.exists(t, webOld))
	require.False(t, f.exists(t, apiOld))
}

func TestRunGC_ZeroPeriodKeepsEverything(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 1, RetentionPeriod: 0})
	f.createApp(t, "web")

	old := f.settled(t, "web", 400*day)
	older := f.settled(t, "web", 500*day)

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	require.Equal(t, 0, result.DeletedRecords)
	require.Equal(t, 0, result.TotalScanned, "a disabled sweep does not scan")
	require.True(t, f.exists(t, old))
	require.True(t, f.exists(t, older))
}

type fakeExports struct {
	landed    int64
	exporting bool
}

func (f fakeExports) LandedRevision() (int64, bool) { return f.landed, f.exporting }

func (f *fixture) revision(t *testing.T, id string) int64 {
	t.Helper()
	rec, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	return rec.Revision
}

// rewrite bumps a record's revision without changing what it says. The mock
// store numbers revisions per entity rather than store-wide, so this is how a
// test puts one record's last write after another's.
func (f *fixture) rewrite(t *testing.T, id string) {
	t.Helper()
	rec, err := f.store.Get(context.Background(), id)
	require.NoError(t, err)
	require.NoError(t, f.store.Put(context.Background(), rec))
}

func TestRunGC_HoldsCandidatesCloudHasNotLanded(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 1, RetentionPeriod: time.Hour})
	f.createApp(t, "web")

	older := f.settled(t, "web", 60*day)
	old := f.settled(t, "web", 50*day)
	newest := f.settled(t, "web", 1*day)
	f.rewrite(t, old)
	f.rewrite(t, newest)
	f.rewrite(t, newest)

	// Cloud has landed everything up to and including `older`'s last write,
	// but not the later write to `old`.
	f.gc.Exports = fakeExports{landed: f.revision(t, older), exporting: true}

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, result.DeletedRecords)
	require.Equal(t, 1, result.AwaitingExport)
	require.False(t, f.exists(t, older), "landed candidate pruned")
	require.True(t, f.exists(t, old), "unlanded candidate held for a later sweep")
	require.True(t, f.exists(t, newest))

	// Once cloud catches up, the held record goes on the next sweep.
	f.gc.Exports = fakeExports{landed: f.revision(t, newest), exporting: true}
	result, err = f.gc.RunGC(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.DeletedRecords)
	require.Equal(t, 0, result.AwaitingExport)
	require.False(t, f.exists(t, old))
}

func TestRunGC_RegisteredClusterWithNothingLandedPrunesNothing(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 1, RetentionPeriod: time.Hour})
	f.createApp(t, "web")

	old := f.settled(t, "web", 60*day)
	f.settled(t, "web", 1*day)
	f.gc.Exports = fakeExports{landed: 0, exporting: true}

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	require.Equal(t, 0, result.DeletedRecords)
	require.Equal(t, 1, result.AwaitingExport)
	require.True(t, f.exists(t, old))
}

func TestRunGC_UnregisteredClusterPrunesOnRetentionAlone(t *testing.T) {
	f := newFixture(t, GCConfig{RetentionCount: 1, RetentionPeriod: time.Hour})
	f.createApp(t, "web")

	old := f.settled(t, "web", 60*day)
	f.settled(t, "web", 1*day)
	f.gc.Exports = fakeExports{landed: 0, exporting: false}

	result, err := f.gc.RunGC(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, result.DeletedRecords)
	require.Equal(t, 0, result.AwaitingExport)
	require.False(t, f.exists(t, old))
}
