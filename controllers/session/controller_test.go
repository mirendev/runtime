package session

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	storage "miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestSandboxIDsSeparateAppAndSessionNames(t *testing.T) {
	require.NotEqual(t, sandboxID("session/my/app-x", 1), sandboxID("session/my-app/x", 1))
}

func TestSessionLifecycleAndRestart(t *testing.T) {
	ctx := context.Background()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctrl := NewController(log, inm.EAC)
	id := entity.Id("session/example")
	original := &sessionapi.Session{
		ID: id, App: "app/example", Version: "app_version/v1", Service: "web", Disk: "disk/workspace",
		DesiredState: sessionapi.RUNNING,
		Spec: sessionapi.SandboxSpec{
			Version:   "app_version/v1",
			Container: []sessionapi.SandboxSpecContainer{{Name: "web", Image: "example:v1"}},
			Volume:    []sessionapi.SandboxSpecVolume{{Name: "workspace", Provider: "miren", DiskName: "superseded", MountPath: "/workspace"}},
		},
	}
	var diskEntity entityserver_v1alpha.Entity
	diskEntity.SetId(original.Disk.String())
	diskEntity.SetAttrs(entity.New(entity.DBId, original.Disk,
		(&storage.Disk{Name: "stable-workspace", CreatedBy: original.App, SizeGb: 1}).Encode).Attrs())
	_, err := inm.EAC.Put(ctx, &diskEntity)
	require.NoError(t, err)
	var e entityserver_v1alpha.Entity
	e.SetId(id.String())
	e.SetAttrs(entity.New(entity.DBId, id, original.Encode).Attrs())
	_, err = inm.EAC.Put(ctx, &e)
	require.NoError(t, err)
	get := func() *sessionapi.Session {
		resp, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		var s sessionapi.Session
		s.Decode(resp.Entity().Entity())
		return &s
	}
	reconcile := func() {
		t.Helper()
		require.NoError(t, ctrl.Reconcile(ctx, get(), &entity.Meta{}))
	}
	patchSession := func(s *sessionapi.Session) {
		t.Helper()
		_, err := inm.EAC.Patch(ctx, entity.New(entity.DBId, id, s.Encode).Attrs(), 0)
		require.NoError(t, err)
	}
	patchSandbox := func(id entity.Id, status compute.SandboxStatus) {
		t.Helper()
		_, err := inm.EAC.Patch(ctx, entity.New(entity.DBId, id, (&compute.Sandbox{Status: status}).Encode).Attrs(), 0)
		require.NoError(t, err)
	}
	ack := func(sandbox entity.Id) {
		t.Helper()
		_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, computeapi.TeardownID(sandbox),
			(&compute.SandboxTeardown{Sandbox: sandbox.String(), Session: id.String()}).Encode).Attrs())
		require.NoError(t, err)
	}

	// Concurrent reconciles (including separate controller instances after a
	// restart) all derive the same ID from the same generation.
	var wg sync.WaitGroup
	seed := get()
	for range 8 {
		wg.Go(func() {
			_ = ctrl.Reconcile(ctx, seed, &entity.Meta{})
		})
	}
	wg.Wait()
	reconcile()
	first := get()
	require.Equal(t, int64(1), first.Generation)
	require.Equal(t, sandboxID(id, 1), first.Sandbox)
	firstSB, err := ctrl.getSandbox(ctx, first.Sandbox)
	require.NoError(t, err)
	require.Equal(t, id, firstSB.SessionInfo.Owner)
	require.Equal(t, "example:v1", firstSB.Spec.Container[0].Image)
	require.Equal(t, "stable-workspace", firstSB.Spec.Volume[0].DiskName)
	require.Equal(t, original.App.String(), firstSB.Spec.LogEntity)
	stable, ok := firstSB.Spec.LogAttribute.Get("miren.session")
	require.True(t, ok)
	require.Equal(t, id.String(), stable)
	concrete, ok := firstSB.Spec.LogAttribute.Get("miren.sandbox")
	require.True(t, ok)
	require.Equal(t, first.Sandbox.String(), concrete)
	instances, err := inm.EAC.List(ctx, entity.Ref(compute.SessionInfoOwnerId, id))
	require.NoError(t, err)
	require.Len(t, instances.Values(), 1)
	reconcile() // A duplicate reconcile must not start a second sandbox.
	require.Equal(t, first.Sandbox, get().Sandbox)
	require.Equal(t, int64(1), get().Generation)

	patchSandbox(first.Sandbox, compute.DEAD)
	reconcile()
	require.Equal(t, first.Sandbox, get().Sandbox, "DEAD can precede runner teardown")
	ack(first.Sandbox)
	reconcile()
	second := get()
	require.Equal(t, int64(2), second.Generation)
	require.NotEqual(t, first.Sandbox, second.Sandbox)
	require.Len(t, second.Incarnation, 1)
	reconcile() // A late event from the old incarnation cannot replace this one.
	require.Equal(t, second.Sandbox, get().Sandbox)

	patchSandbox(second.Sandbox, compute.RUNNING)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, second.Sandbox,
		(&compute.Sandbox{Activity: compute.Activity{State: compute.IDLE, ReportedAt: time.Now()}}).Encode).Attrs(), 0)
	require.NoError(t, err)
	reconcile()
	require.Equal(t, sessionapi.IDLE, get().Activity)
	require.Equal(t, sessionapi.IDLE, get().SelfReportedActivity(time.Now()))
	require.Equal(t, sessionapi.UNKNOWN, get().SelfReportedActivity(time.Now().Add(compute.ActivityFreshFor)))

	patchSession(&sessionapi.Session{DesiredState: sessionapi.SUSPENDED})
	reconcile()
	stopped, err := ctrl.getSandbox(ctx, second.Sandbox)
	require.NoError(t, err)
	require.Equal(t, compute.RUNNING, stopped.Status)
	require.False(t, stopped.ShutdownAt.IsZero(), "drain notice precedes STOPPED")
	patchSession(&sessionapi.Session{DesiredState: sessionapi.RUNNING})
	reconcile()
	stopped, err = ctrl.getSandbox(ctx, second.Sandbox)
	require.NoError(t, err)
	require.True(t, stopped.ShutdownAt.IsZero(), "cancelled suspension removes the notice")
	patchSession(&sessionapi.Session{DesiredState: sessionapi.SUSPENDED})
	reconcile()
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, second.Sandbox,
		(&compute.Sandbox{ShutdownAt: time.Now().Add(-time.Second)}).Encode).Attrs(), 0)
	require.NoError(t, err)
	reconcile()
	stopped, err = ctrl.getSandbox(ctx, second.Sandbox)
	require.NoError(t, err)
	require.Equal(t, compute.STOPPED, stopped.Status)
	reconcile()
	require.Equal(t, second.Sandbox, get().Sandbox, "STOPPED only requests teardown")
	ack(second.Sandbox)
	reconcile()
	require.Empty(t, get().Sandbox)
	require.Equal(t, sessionapi.INACTIVE, get().Phase)
	require.Equal(t, original.Spec.Container[0].Image, get().Spec.Container[0].Image)
	require.Len(t, get().Incarnation, 2)
	settled, err := inm.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	for range 3 {
		reconcile()
		current, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		require.Equal(t, settled.Entity().Revision(), current.Entity().Revision(), "settled suspension must not write again")
	}

	// Reconstruct the controller as if the coordinator restarted before resuming.
	ctrl = NewController(log, inm.EAC)
	patchSession(&sessionapi.Session{DesiredState: sessionapi.RUNNING})
	reconcile()
	third := get()
	require.Equal(t, int64(3), third.Generation)
	require.NotEqual(t, second.Sandbox, third.Sandbox)
	thirdSB, err := ctrl.getSandbox(ctx, third.Sandbox)
	require.NoError(t, err)
	require.Equal(t, "example:v1", thirdSB.Spec.Container[0].Image)
	require.Equal(t, "stable-workspace", thirdSB.Spec.Volume[0].DiskName)
	stable, ok = thirdSB.Spec.LogAttribute.Get("miren.session")
	require.True(t, ok)
	require.Equal(t, id.String(), stable)

	_, err = inm.EAC.Delete(ctx, id.String())
	require.NoError(t, err)
	// Even if the deletion event is lost over restart, the sweep finds it.
	require.NoError(t, ctrl.SweepOrphans(ctx))
	thirdSB, err = ctrl.getSandbox(ctx, third.Sandbox)
	require.NoError(t, err)
	require.Equal(t, compute.STOPPED, thirdSB.Status)
	require.NoError(t, ctrl.Delete(ctx, id))
	_, err = inm.EAC.Get(ctx, original.Disk.String())
	require.NoError(t, err, "app-owned disk must outlive a session")
}

func TestUnpublishedSessionChildSurvivesCoordinatorRestart(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	id := entity.Id("session/crash-window")
	child := sandboxID(id, 1)
	ctrl := NewController(slog.New(slog.NewTextHandler(io.Discard, nil)), inm.EAC)
	s := &sessionapi.Session{ID: id, DesiredState: sessionapi.SUSPENDED,
		Spec: sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Name: "web", Image: "example:v1"}}}}
	var record entityserver_v1alpha.Entity
	record.SetId(id.String())
	record.SetAttrs(entity.New(entity.DBId, id, s.Encode).Attrs())
	_, err := inm.EAC.Put(ctx, &record)
	require.NoError(t, err)
	// The child was created, but the coordinator crashed before publishing the
	// Session's current pointer or generation.
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, child,
		(&compute.Sandbox{Status: compute.PENDING, SessionInfo: compute.SessionInfo{Owner: id}}).Encode).Attrs())
	require.NoError(t, err)
	reconcile := func() *sessionapi.Session {
		t.Helper()
		resp, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		var current sessionapi.Session
		current.Decode(resp.Entity().Entity())
		require.NoError(t, ctrl.Reconcile(ctx, &current, &entity.Meta{}))
		resp, err = inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		current.Decode(resp.Entity().Entity())
		return &current
	}
	require.Equal(t, sessionapi.SUSPENDING, reconcile().Phase)
	sb, err := ctrl.getSandbox(ctx, child)
	require.NoError(t, err)
	require.Equal(t, compute.STOPPED, sb.Status)
	require.Zero(t, reconcile().Generation, "STOPPED does not prove teardown")
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, computeapi.TeardownID(child),
		(&compute.SandboxTeardown{Sandbox: child.String(), Session: id.String()}).Encode).Attrs())
	require.NoError(t, err)
	require.Equal(t, sessionapi.INACTIVE, reconcile().Phase)
	require.Equal(t, int64(1), reconcile().Generation)
	_, err = inm.EAC.Delete(ctx, child.String())
	require.NoError(t, err)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id,
		(&sessionapi.Session{DesiredState: sessionapi.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	current := reconcile()
	require.Equal(t, sandboxID(id, 2), current.Sandbox, "deleted child ID must not be reused")
	require.Equal(t, child.String(), current.Incarnation[0].Sandbox)
}

func TestUnpublishedRunningChildIsAdopted(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	id := entity.Id("session/adopt")
	child := sandboxID(id, 1)
	ctrl := NewController(slog.New(slog.NewTextHandler(io.Discard, nil)), inm.EAC)
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id,
		(&sessionapi.Session{DesiredState: sessionapi.RUNNING,
			Spec: sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Name: "web", Image: "example:v1"}}}}).Encode).Attrs())
	require.NoError(t, err)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, child,
		(&compute.Sandbox{Status: compute.RUNNING, SessionInfo: compute.SessionInfo{Owner: id}}).Encode).Attrs())
	require.NoError(t, err)
	resp, err := inm.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	var s sessionapi.Session
	s.Decode(resp.Entity().Entity())
	require.NoError(t, ctrl.Reconcile(ctx, &s, &entity.Meta{}))
	resp, err = inm.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	s.Decode(resp.Entity().Entity())
	require.Equal(t, child, s.Sandbox)
	require.Equal(t, int64(1), s.Generation)
	children, err := ctrl.children(ctx, id)
	require.NoError(t, err)
	require.Len(t, children, 1)
}

func TestSessionDrainWaitsForPostNoticeActivityToBecomeIdle(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctrl := NewController(slog.New(slog.NewTextHandler(io.Discard, nil)), inm.EAC)
	id := entity.Id("sandbox/session-drain-1")
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id,
		(&compute.Sandbox{Status: compute.RUNNING, SessionInfo: compute.SessionInfo{Owner: "session/drain"},
			Activity: compute.Activity{State: compute.IDLE, ReportedAt: time.Now()}}).Encode).Attrs())
	require.NoError(t, err)
	require.NoError(t, ctrl.drainOrStop(ctx, id))
	sb, err := ctrl.getSandbox(ctx, id)
	require.NoError(t, err)
	require.Equal(t, compute.RUNNING, sb.Status)
	require.False(t, sb.ShutdownAt.IsZero())
	// A report after the notice wins even if the original idle state was fresh.
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id,
		(&compute.Sandbox{Activity: compute.Activity{State: compute.ACTIVE, ReportedAt: time.Now()},
			ShutdownAt: time.Now().Add(-time.Second)}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.NoError(t, ctrl.drainOrStop(ctx, id))
	sb, err = ctrl.getSandbox(ctx, id)
	require.NoError(t, err)
	require.Equal(t, compute.RUNNING, sb.Status)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id,
		(&compute.Sandbox{Activity: compute.Activity{State: compute.IDLE, ReportedAt: time.Now()}}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.NoError(t, ctrl.drainOrStop(ctx, id))
	sb, err = ctrl.getSandbox(ctx, id)
	require.NoError(t, err)
	require.Equal(t, compute.STOPPED, sb.Status)
	// A lost workload must not keep the Session in SUSPENDING forever.
	other := entity.Id("sandbox/session-drain-2")
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, other,
		(&compute.Sandbox{Status: compute.RUNNING, SessionInfo: compute.SessionInfo{Owner: "session/drain"},
			ShutdownAt: time.Now().Add(-3 * time.Minute),
			Activity:   compute.Activity{State: compute.ACTIVE, ReportedAt: time.Now().Add(-2*time.Minute - time.Second)}}).Encode).Attrs())
	require.NoError(t, err)
	require.NoError(t, ctrl.drainOrStop(ctx, other))
	sb, err = ctrl.getSandbox(ctx, other)
	require.NoError(t, err)
	require.Equal(t, compute.STOPPED, sb.Status)
}
