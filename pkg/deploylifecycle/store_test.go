package deploylifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewStore(log, inmem.EAC)
}

// seed writes a deployment and returns its id. at controls sort order.
func seed(t *testing.T, s *Store, app, cluster string, status Status, at time.Time) string {
	t.Helper()

	rec, err := s.Create(context.Background(), &core_v1alpha.Deployment{
		AppName:   app,
		ClusterId: cluster,
		Status:    string(status),
		Phase:     string(PhasePreparing),
		DeployedBy: core_v1alpha.DeployedBy{
			Timestamp: at.Format(time.RFC3339),
		},
	})
	require.NoError(t, err)

	return string(rec.Deployment.ID)
}

// TestIndexSelection pins the index each query shape uses. The selected index
// determines which records reach the in-memory compatibility filter, so this is
// a correctness choice as well as a scan-size choice during migration.
func TestIndexSelection(t *testing.T) {
	s := newTestStore(t)

	tests := []struct {
		name  string
		query Query
		want  entity.Attr
	}{
		{
			name:  "app name",
			query: Query{AppName: "web"},
			want:  entity.String(core_v1alpha.DeploymentAppNameId, "web"),
		},
		{
			name:  "app name wins over canonical status",
			query: Query{AppName: "web", Status: StatusActive},
			want:  entity.String(core_v1alpha.DeploymentAppNameId, "web"),
		},
		{
			name:  "app name wins for in progress too",
			query: Query{AppName: "web", Status: StatusInProgress},
			want:  entity.String(core_v1alpha.DeploymentAppNameId, "web"),
		},
		{
			// Settled statuses accumulate forever, so the app index is narrower.
			name:  "app name wins over a settled status",
			query: Query{AppName: "web", Status: StatusFailed},
			want:  entity.String(core_v1alpha.DeploymentAppNameId, "web"),
		},
		{
			name:  "status only",
			query: Query{Status: StatusInProgress},
			want:  entity.String(core_v1alpha.DeploymentStatusId, "in_progress"),
		},
		{
			name:  "settled status includes legacy records",
			query: Query{Status: StatusFailed},
			want:  entity.Ref(entity.EntityKind, core_v1alpha.KindDeployment),
		},
		{
			name:  "unfiltered falls back to a kind scan",
			query: Query{},
			want:  entity.Ref(entity.EntityKind, core_v1alpha.KindDeployment),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := s.index(tc.query)
			assert.Equal(t, tc.want.ID, got.ID)
			assert.True(t, tc.want.Value.Equal(got.Value),
				"want %v, got %v", tc.want.Value, got.Value)
		})
	}
}

func TestListFiltersByAppAndStatus(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	want := seed(t, s, "web", "prod", StatusInProgress, base)
	seed(t, s, "api", "prod", StatusInProgress, base) // different app
	seed(t, s, "web", "prod", StatusActive, base)     // different status

	got, err := s.List(ctx, Query{AppName: "web", Status: StatusInProgress})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, want, string(got[0].Deployment.ID))
}

func TestSettledStatusOnlyQueryIncludesLegacyRecord(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	want := seed(t, s, "web", "prod", StatusFailed, time.Now())

	got, err := s.List(ctx, Query{Status: StatusFailed})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, want, string(got[0].Deployment.ID))
}

// A deploy from CI and a manual deploy stamp the same app with different
// cluster_id strings; List must return both, never filter one out by cluster.
func TestListDoesNotFilterByCluster(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	seed(t, s, "web", "garden", StatusSucceeded, base)                             // manual deploy
	seed(t, s, "web", "34.122.229.118:8443", StatusSucceeded, base.Add(time.Hour)) // CI/OIDC deploy

	got, err := s.List(ctx, Query{AppName: "web"})
	require.NoError(t, err)
	assert.Len(t, got, 2, "both deploys of the same app must show, regardless of cluster string")
}

func TestListSortsNewestFirstAndLimits(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	oldest := seed(t, s, "web", "prod", StatusSucceeded, base)
	middle := seed(t, s, "web", "prod", StatusSucceeded, base.Add(time.Hour))
	newest := seed(t, s, "web", "prod", StatusSucceeded, base.Add(2*time.Hour))

	got, err := s.List(ctx, Query{AppName: "web"})
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{newest, middle, oldest}, ids(got))

	limited, err := s.List(ctx, Query{AppName: "web", Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{newest, middle}, ids(limited),
		"the limit must keep the newest, not an arbitrary two")
}

func TestListUnfilteredReturnsEverything(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	seed(t, s, "web", "prod", StatusActive, base)
	seed(t, s, "api", "staging", StatusFailed, base)

	got, err := s.List(ctx, Query{})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestListEmptyResult(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.List(ctx, Query{AppName: "nonexistent"})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestStatusLookup(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id := seed(t, s, "web", "prod", StatusInProgress, time.Now())

	status, err := s.Status(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, StatusInProgress, status)

	_, err = s.Status(ctx, "deployment/missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, cond.ErrNotFound{}),
		"a missing record must remain distinguishable from an empty status; got %T", err)
}

func TestLockReservationIsPrivateUntilPublished(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	id := entity.Id("deployment/reserved")

	revision, err := s.ReserveLockOwner(ctx, id)
	require.NoError(t, err)
	assert.Positive(t, revision)

	status, err := s.Status(ctx, string(id))
	require.NoError(t, err)
	assert.Equal(t, Status(lockReservationStatus), status)
	assert.False(t, status.Terminal(),
		"an older runtime must not steal the compatibility lock during acquisition")

	listed, err := s.List(ctx, Query{})
	require.NoError(t, err)
	assert.Empty(t, listed, "a lock reservation is not deployment history")

	dep := &core_v1alpha.Deployment{
		ID:        id,
		AppName:   "web",
		Operation: string(OperationBuild),
		StartedAt: time.Now(),
		Status:    string(StatusInProgress),
	}
	rec, err := s.PublishLockOwner(ctx, dep, revision)
	require.NoError(t, err)
	assert.Equal(t, StatusInProgress, rec.Status())

	listed, err = s.List(ctx, Query{})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, id, listed[0].Deployment.ID)
}

// The Store satisfies the lock manager's StatusLookup without an adapter, which
// is what lets an abandoned lock be reconciled against its record.
func TestStoreSatisfiesStatusLookup(t *testing.T) {
	s := newTestStore(t)
	var _ StatusLookup = s.Status
}

func TestAppVersionNormalizesLegacySentinels(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		version string
		want    string
	}{
		{name: "real version", id: "deployment/1", version: "app_version/abc", want: "app_version/abc"},
		{name: "pending-build", id: "deployment/1", version: "pending-build", want: ""},
		{name: "failed sentinel", id: "deployment/1", version: "failed-deployment/1", want: ""},
		{name: "unset", id: "deployment/1", version: "", want: ""},
		{
			name:    "failed sentinel for another deployment is left alone",
			id:      "deployment/1",
			version: "failed-deployment/2",
			want:    "failed-deployment/2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &Record{Deployment: &core_v1alpha.Deployment{
				ID:         entity.Id(tc.id),
				AppVersion: tc.version,
			}}
			assert.Equal(t, tc.want, rec.AppVersion())
		})
	}
}

func TestMarkPreviousActiveAs(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	previous := seed(t, s, "web", "garden", StatusActive, base)
	incoming := seed(t, s, "web", "garden", StatusActive, base.Add(time.Hour))
	otherApp := seed(t, s, "api", "garden", StatusActive, base)
	// Same app, a different cluster_id string (the CI/manual split). App-scoped
	// activation settles it too — it is the same app on the same coordinator.
	otherClusterString := seed(t, s, "web", "34.122.229.118:8443", StatusActive, base)

	require.NoError(t, s.MarkPreviousActiveAs(ctx, "web", incoming, StatusSucceeded))

	assertLegacyStatus(t, s, previous, StatusSucceeded)
	assertLegacyStatus(t, s, incoming, StatusActive)
	assertLegacyStatus(t, s, otherApp, StatusActive)
	assertLegacyStatus(t, s, otherClusterString, StatusSucceeded)

	rec, err := s.Get(ctx, previous)
	require.NoError(t, err)
	assert.NotEmpty(t, rec.Deployment.CompletedAt, "a settled deployment is completed")
}

func TestMarkPreviousActiveAsRolledBack(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	previous := seed(t, s, "web", "prod", StatusActive, base)
	incoming := seed(t, s, "web", "prod", StatusActive, base.Add(time.Hour))

	require.NoError(t, s.MarkPreviousActiveAs(ctx, "web", incoming, StatusRolledBack))

	assertLegacyStatus(t, s, previous, StatusRolledBack)
}

// Only active deployments are settled. An in-progress one is someone else's
// live deploy and must not be trampled.
func TestMarkPreviousActiveAsLeavesInProgressAlone(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)

	running := seed(t, s, "web", "prod", StatusInProgress, base)
	incoming := seed(t, s, "web", "prod", StatusActive, base.Add(time.Hour))

	require.NoError(t, s.MarkPreviousActiveAs(ctx, "web", incoming, StatusSucceeded))

	assertLegacyStatus(t, s, running, StatusInProgress)
}

func TestPutRejectsStaleRevision(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id := seed(t, s, "web", "prod", StatusInProgress, time.Now())

	stale, err := s.Get(ctx, id)
	require.NoError(t, err)

	fresh, err := s.Get(ctx, id)
	require.NoError(t, err)
	fresh.Deployment.Phase = string(PhaseBuilding)
	require.NoError(t, s.Put(ctx, fresh))

	stale.Deployment.Phase = string(PhasePushing)
	err = s.Put(ctx, stale)
	require.Error(t, err, "a write against a superseded revision must not silently win")
	assert.True(t, isConflict(err),
		"callers distinguish a lost race from a broken store, so it must stay a conflict through the wrap; got %v", err)
}

func ids(records []*Record) []string {
	out := make([]string, len(records))
	for i, r := range records {
		out[i] = string(r.Deployment.ID)
	}
	return out
}

func assertLegacyStatus(t *testing.T, s *Store, id string, want Status) {
	t.Helper()
	rec, err := s.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, string(want), rec.Deployment.Status, "legacy status of %s", id)
}
