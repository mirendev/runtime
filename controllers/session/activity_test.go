package session

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestIdleParkingRequiresFreshContinuousIdle(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name           string
		activity       sessionapi.SessionActivity
		age, reportAge time.Duration
		timeout        int64
		park           bool
	}{
		{"before boundary", sessionapi.IDLE, 59 * time.Second, 0, 60, false},
		{"at boundary", sessionapi.IDLE, time.Minute, 0, 60, true},
		{"active", sessionapi.ACTIVE, 3 * time.Minute, 0, 60, false},
		{"stale", sessionapi.IDLE, 3 * time.Minute, 2 * time.Minute, 60, false},
		{"future report", sessionapi.IDLE, 3 * time.Minute, -time.Second, 60, false},
		{"disabled", sessionapi.IDLE, 3 * time.Minute, 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inm, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			c := NewController(slog.Default(), inm.EAC)
			id := entity.Id("session/test")
			_, err := inm.EAC.Create(t.Context(), entity.New(entity.DBId, id, (&sessionapi.Session{
				DesiredState: sessionapi.RUNNING, Phase: sessionapi.READY, Sandbox: "sandbox/test",
				Activity: tc.activity, ActivityAt: now.Add(-tc.reportAge), IdleSince: now.Add(-tc.age), IdleTimeoutSeconds: tc.timeout,
			}).Encode).Attrs())
			require.NoError(t, err)
			require.NoError(t, c.parkIdleSessions(t.Context(), now))
			resp, err := inm.EAC.Get(t.Context(), id.String())
			require.NoError(t, err)
			var s sessionapi.Session
			s.Decode(resp.Entity().Entity())
			if tc.park {
				require.Equal(t, sessionapi.SUSPENDED, s.DesiredState)
				require.Equal(t, sessionapi.SUSPENDING, s.Phase)
			} else {
				require.Equal(t, sessionapi.RUNNING, s.DesiredState)
			}
		})
	}
}

func TestIdleParkingDoesNotOverrideConcurrentAdmission(t *testing.T) {
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	now := time.Now()
	id := entity.Id("session/racing")
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id, (&sessionapi.Session{
		DesiredState: sessionapi.RUNNING, Phase: sessionapi.READY, Sandbox: "sandbox/test",
		Activity: sessionapi.IDLE, ActivityAt: now, IdleSince: now.Add(-time.Minute), IdleTimeoutSeconds: 60,
	}).Encode).Attrs())
	require.NoError(t, err)
	// Return the idle snapshot after a newer active report has been persisted.
	// Without revision-checked parking, the sweep would withdraw accepted work.
	inm.Store.GetEntitiesFunc = func(ctx context.Context, ids []entity.Id) ([]*entity.Entity, error) {
		inm.Store.GetEntitiesFunc = nil
		values, err := inm.Store.GetEntities(ctx, ids)
		require.NoError(t, err)
		_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id,
			(&sessionapi.Session{Activity: sessionapi.ACTIVE, ActivityAt: now.Add(time.Second)}).Encode).Attrs(), 0)
		require.NoError(t, err)
		return values, nil
	}
	c := NewController(slog.Default(), inm.EAC)
	require.NoError(t, c.parkIdleSessions(ctx, now))
	resp, err := inm.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	var s sessionapi.Session
	s.Decode(resp.Entity().Entity())
	require.Equal(t, sessionapi.RUNNING, s.DesiredState)
	require.Equal(t, sessionapi.READY, s.Phase)
	require.Equal(t, sessionapi.ACTIVE, s.Activity)
}

func TestSharedIdleParkingFreesCapacityOnlyAfterCleanup(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c := NewController(slog.Default(), inm.EAC)
	read := func(id entity.Id) *sessionapi.Session {
		t.Helper()
		resp, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		var s sessionapi.Session
		s.Decode(resp.Entity().Entity())
		return &s
	}
	reconcile := func(id entity.Id) *sessionapi.Session {
		t.Helper()
		require.NoError(t, c.Reconcile(ctx, read(id), &entity.Meta{}))
		return read(id)
	}
	create := func(id entity.Id) *sessionapi.Session {
		t.Helper()
		_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id, (&sessionapi.Session{
			App: "app/workers", Service: "web", DesiredState: sessionapi.RUNNING, MaxSessionsPerSandbox: 2,
			IdleTimeoutSeconds: 60, Spec: sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "worker:v1"}}},
		}).Encode).Attrs())
		require.NoError(t, err)
		return reconcile(id)
	}
	idle, busy := entity.Id("session/idle"), entity.Id("session/busy")
	host := create(idle).Sandbox
	require.Equal(t, host, create(busy).Sandbox)
	_, err := inm.EAC.Patch(ctx, entity.New(entity.DBId, host, (&compute.Sandbox{Status: compute.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	reconcile(idle)
	reconcile(busy)
	now := time.Now()
	for id, state := range map[entity.Id]sessionapi.SessionActivity{idle: sessionapi.IDLE, busy: sessionapi.ACTIVE} {
		_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id, (&sessionapi.Session{
			Activity: state, ActivityAt: now, IdleSince: now.Add(-time.Minute),
		}).Encode).Attrs(), 0)
		require.NoError(t, err)
	}
	require.NoError(t, c.parkIdleSessions(ctx, now))
	require.Equal(t, sessionapi.SUSPENDING, reconcile(idle).Phase)
	require.Equal(t, sessionapi.RUNNING, read(busy).DesiredState)
	require.NoError(t, c.SweepOrphans(ctx))
	slots, err := inm.EAC.List(ctx, entity.String(sessionapi.SlotSandboxId, host.String()))
	require.NoError(t, err)
	require.Len(t, slots.Values(), 2, "idle is not cleanup proof")
	resp, err := inm.EAC.Get(ctx, shared.BindingID(idle).String())
	require.NoError(t, err)
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	require.False(t, binding.DetachedAt.IsZero())
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, binding.ID, (&sessionapi.Binding{AcknowledgedAt: now}).Encode).Attrs(), 0)
	require.NoError(t, err)
	// Recovery by a new controller must finish the same durable detach.
	c = NewController(slog.Default(), inm.EAC)
	require.NoError(t, c.SweepOrphans(ctx))
	require.NoError(t, c.SweepOrphans(ctx))
	parked := reconcile(idle)
	require.Equal(t, sessionapi.INACTIVE, parked.Phase)
	require.Empty(t, parked.Sandbox)
	require.Equal(t, "worker:v1", parked.Spec.Container[0].Image)
	settled, err := inm.EAC.Get(ctx, idle.String())
	require.NoError(t, err)
	for range 3 {
		require.Equal(t, sessionapi.INACTIVE, reconcile(idle).Phase)
	}
	again, err := inm.EAC.Get(ctx, idle.String())
	require.NoError(t, err)
	require.Equal(t, settled.Entity().Revision(), again.Entity().Revision(), "settled parking must not trigger an endless watch loop")
	require.Equal(t, host, create("session/replacement").Sandbox)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, idle, (&sessionapi.Session{DesiredState: sessionapi.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	resumed := reconcile(idle)
	require.NotEqual(t, host, resumed.Sandbox, "the freed slot now belongs to the replacement")
	require.Equal(t, idle, resumed.ID)
	require.Zero(t, resumed.IdleSince)
	require.Zero(t, resumed.ActivityAt)
}
