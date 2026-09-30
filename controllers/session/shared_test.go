package session

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	core "miren.dev/runtime/api/core/core_v1alpha"
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestSharedSessionsManagedCapacityAndDeletion(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c := NewController(slog.Default(), inm.EAC)
	spec := sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "example:v1"}}}
	ids := []entity.Id{"session/one", "session/two", "session/three"}
	reconcile := func(id entity.Id) sessionapi.Session {
		t.Helper()
		resp, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		var s sessionapi.Session
		s.Decode(resp.Entity().Entity())
		require.NoError(t, c.Reconcile(ctx, &s, &entity.Meta{}))
		resp, err = inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		s = sessionapi.Session{}
		s.Decode(resp.Entity().Entity())
		return s
	}
	for _, id := range ids {
		_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id,
			(&sessionapi.Session{App: "app/one", Spec: spec, MaxSessionsPerSandbox: 2, DesiredState: sessionapi.RUNNING}).Encode).Attrs())
		require.NoError(t, err)
	}
	first := reconcile(ids[0])
	require.Equal(t, sessionapi.ACTIVATING, first.Phase)
	host := first.Sandbox
	sb, err := c.getSandbox(ctx, host)
	require.NoError(t, err)
	require.Equal(t, compute.PENDING, sb.Status)
	require.NotEmpty(t, sb.SessionInfo.Group)
	require.Empty(t, sb.SessionInfo.Owner)
	dedicatedMode := first
	dedicatedMode.MaxSessionsPerSandbox = 1
	require.ErrorContains(t, c.Reconcile(ctx, &dedicatedMode, &entity.Meta{}), "cannot enter dedicated mode")
	formerDedicated := first
	formerDedicated.Generation = 1
	require.ErrorContains(t, c.Reconcile(ctx, &formerDedicated, &entity.Meta{}), "cannot enter shared mode")
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, host,
		(&compute.Sandbox{Status: compute.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.Equal(t, sessionapi.READY, reconcile(ids[0]).Phase)
	require.Equal(t, host, reconcile(ids[1]).Sandbox)
	sb, err = c.getSandbox(ctx, host)
	require.NoError(t, err)
	require.Equal(t, sharedGroup(&first), sb.SessionInfo.Group)
	require.Equal(t, int64(2), sb.SessionInfo.Capacity)
	require.Equal(t, int64(1), sb.SessionInfo.Epoch, "admission must preserve the host's group and capacity")
	third := reconcile(ids[2])
	require.NotEqual(t, host, third.Sandbox, "a third Session must boot a second host")
	require.Equal(t, sessionapi.ACTIVATING, third.Phase)
	// Different per-Session specs can share a host when app, service, and key match.
	otherID := entity.Id("session/different-spec")
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, otherID,
		(&sessionapi.Session{App: "app/one", MaxSessionsPerSandbox: 2,
			Spec:         sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "other:v2"}}},
			DesiredState: sessionapi.RUNNING}).Encode).Attrs())
	require.NoError(t, err)
	require.Equal(t, third.Sandbox, reconcile(otherID).Sandbox)
	for _, tc := range []struct {
		id      entity.Id
		service string
		group   string
	}{
		{"session/other-service", "worker", ""},
		{"session/other-key", "", "queue"},
	} {
		_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, tc.id,
			(&sessionapi.Session{App: "app/one", Service: tc.service, Group: tc.group,
				MaxSessionsPerSandbox: 2, Spec: spec, DesiredState: sessionapi.RUNNING}).Encode).Attrs())
		require.NoError(t, err)
		separate := reconcile(tc.id)
		require.NotEqual(t, host, separate.Sandbox)
		require.NotEqual(t, third.Sandbox, separate.Sandbox)
	}
	otherApp := &sessionapi.Session{App: "app/another"}
	require.NotEqual(t, sharedGroup(otherApp), sharedGroup(&sessionapi.Session{App: "app/one"}))
	require.NotEqual(t, sharedGroup(&sessionapi.Session{App: "app/one", Service: "worker"}),
		sharedGroup(&sessionapi.Session{App: "app/one"}))
	require.NotEqual(t, sharedGroup(&sessionapi.Session{App: "app/one", Group: "queue"}),
		sharedGroup(&sessionapi.Session{App: "app/one"}))
	require.NotEqual(t, sharedGroup(&sessionapi.Session{App: "app/one", Service: "a\x00b", Group: "c"}),
		sharedGroup(&sessionapi.Session{App: "app/one", Service: "a", Group: "b\x00c"}))
	wrongCapacity := &sessionapi.Session{ID: "session/wrong-capacity", App: "app/one",
		MaxSessionsPerSandbox: 3, Spec: spec, DesiredState: sessionapi.RUNNING}
	require.ErrorContains(t, c.Reconcile(ctx, wrongCapacity, &entity.Meta{}), "already has capacity 2")
	for _, id := range ids[:2] {
		slots, err := inm.EAC.List(ctx, entity.String(sessionapi.SlotSessionId, id.String()))
		require.NoError(t, err)
		require.Len(t, slots.Values(), 1)
	}
	slots, err := inm.EAC.List(ctx, entity.String(sessionapi.SlotSandboxId, host.String()))
	require.NoError(t, err)
	require.Len(t, slots.Values(), 2)

	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, ids[0],
		(&sessionapi.Session{DesiredState: sessionapi.SUSPENDED}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.Equal(t, sessionapi.INACTIVE, reconcile(ids[0]).Phase)
	require.Equal(t, host, reconcile(ids[1]).Sandbox)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, ids[0],
		(&sessionapi.Session{DesiredState: sessionapi.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.Equal(t, host, reconcile(ids[0]).Sandbox)

	_, err = inm.EAC.Delete(ctx, ids[0].String())
	require.NoError(t, err)
	require.NoError(t, c.Delete(ctx, ids[0]))
	resp, err := inm.EAC.Get(ctx, shared.BindingID(ids[0]).String())
	require.NoError(t, err)
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	require.False(t, binding.DeletedAt.IsZero())
	require.NoError(t, c.SweepOrphans(ctx))
	slots, err = inm.EAC.List(ctx, entity.String(sessionapi.SlotSandboxId, host.String()))
	require.NoError(t, err)
	require.Len(t, slots.Values(), 2, "deletion holds capacity until workload cleanup is acknowledged")
	sb, err = c.getSandbox(ctx, host)
	require.NoError(t, err)
	require.Equal(t, compute.RUNNING, sb.Status)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, binding.ID,
		(&sessionapi.Binding{AcknowledgedAt: binding.DeletedAt}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.NoError(t, c.SweepOrphans(ctx))
	slots, err = inm.EAC.List(ctx, entity.String(sessionapi.SlotSandboxId, host.String()))
	require.NoError(t, err)
	require.Len(t, slots.Values(), 1)
	require.Equal(t, host, reconcile(ids[1]).Sandbox)

	_, err = inm.EAC.Delete(ctx, ids[1].String())
	require.NoError(t, err)
	require.NoError(t, NewController(slog.Default(), inm.EAC).SweepOrphans(ctx))
	resp, err = inm.EAC.Get(ctx, shared.BindingID(ids[1]).String())
	require.NoError(t, err)
	binding = sessionapi.Binding{}
	binding.Decode(resp.Entity().Entity())
	require.False(t, binding.DeletedAt.IsZero(), "sweep repairs a missed deletion event")
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, binding.ID,
		(&sessionapi.Binding{AcknowledgedAt: binding.DeletedAt}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.NoError(t, c.SweepOrphans(ctx))
	sb, err = c.getSandbox(ctx, host)
	require.NoError(t, err)
	require.Equal(t, compute.STOPPED, sb.Status)
}

func TestSharedSessionsRecoverReservationBeforeBinding(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	s := &sessionapi.Session{ID: "session/recover", App: "app/one", MaxSessionsPerSandbox: 2,
		Spec:         sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "example:v1"}}},
		DesiredState: sessionapi.RUNNING}
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, s.ID, s.Encode).Attrs())
	require.NoError(t, err)
	group := sharedGroup(s)
	host := sharedHostID(group, 0)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, shared.SlotID(host, 0),
		(&sessionapi.Slot{Session: s.ID.String(), Sandbox: host.String()}).Encode).Attrs())
	require.NoError(t, err)
	require.NoError(t, NewController(slog.Default(), inm.EAC).Reconcile(ctx, s, &entity.Meta{}))
	resp, err := inm.EAC.Get(ctx, shared.BindingID(s.ID).String())
	require.NoError(t, err)
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	require.Equal(t, host.String(), binding.Sandbox)
	slots, err := inm.EAC.List(ctx, entity.String(sessionapi.SlotSessionId, s.ID.String()))
	require.NoError(t, err)
	require.Len(t, slots.Values(), 1)
}

func TestSharedSessionsDeletionDuringAttachKeepsCleanupNotice(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c := NewController(slog.Default(), inm.EAC)
	id := entity.Id("session/deleted-during-attach")
	host := entity.Id("sandbox/attaching")
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, shared.SlotID(host, 0),
		(&sessionapi.Slot{Session: id.String(), Sandbox: host.String()}).Encode).Attrs())
	require.NoError(t, err)
	// No host yet: a sweep must not free the capacity while attach is in flight.
	require.NoError(t, c.SweepOrphans(ctx))
	slots, err := inm.EAC.List(ctx, entity.String(sessionapi.SlotSessionId, id.String()))
	require.NoError(t, err)
	require.Len(t, slots.Values(), 1)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, host,
		(&compute.Sandbox{Status: compute.RUNNING, SessionInfo: compute.SessionInfo{Group: "group"}}).Encode).Attrs())
	require.NoError(t, err)
	require.NoError(t, c.SweepOrphans(ctx))
	resp, err := inm.EAC.Get(ctx, shared.BindingID(id).String())
	require.NoError(t, err)
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	require.Equal(t, host.String(), binding.Sandbox)
	require.False(t, binding.DeletedAt.IsZero())
	hostEntity, err := c.getSandbox(ctx, host)
	require.NoError(t, err)
	require.Equal(t, compute.RUNNING, hostEntity.Status)
}

func TestSharedSessionsReplaceHostOnlyAfterTeardown(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c := NewController(slog.Default(), inm.EAC)
	id := entity.Id("session/recover-host")
	s := &sessionapi.Session{ID: id, App: "app/one", MaxSessionsPerSandbox: 2,
		Spec:         sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "example:v1"}}},
		DesiredState: sessionapi.RUNNING}
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id, s.Encode).Attrs())
	require.NoError(t, err)
	reconcile := func() sessionapi.Session {
		t.Helper()
		resp, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		var current sessionapi.Session
		current.Decode(resp.Entity().Entity())
		require.NoError(t, c.Reconcile(ctx, &current, &entity.Meta{}))
		resp, err = inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		current = sessionapi.Session{}
		current.Decode(resp.Entity().Entity())
		return current
	}
	old := reconcile().Sandbox
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, old,
		(&compute.Sandbox{Status: compute.DEAD}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.Equal(t, sessionapi.FAILED, reconcile().Phase)
	require.Equal(t, old, reconcile().Sandbox)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, computeapi.TeardownID(old),
		(&compute.SandboxTeardown{Sandbox: old.String()}).Encode).Attrs())
	require.NoError(t, err)
	next := reconcile()
	require.NotEqual(t, old, next.Sandbox)
	require.Equal(t, sessionapi.ACTIVATING, next.Phase)
	require.NoError(t, c.SweepOrphans(ctx))
	slots, err := inm.EAC.List(ctx, entity.String(sessionapi.SlotSessionId, id.String()))
	require.NoError(t, err)
	require.Len(t, slots.Values(), 1, "old host reservation must be retired")
}

func TestSharedSessionsRetiringHostRejectsNewAdmission(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c := NewController(slog.Default(), inm.EAC)
	s := &sessionapi.Session{ID: "session/new", App: "app/one", MaxSessionsPerSandbox: 2,
		Spec:         sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "example:v1"}}},
		DesiredState: sessionapi.RUNNING}
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, s.ID, s.Encode).Attrs())
	require.NoError(t, err)
	group := sharedGroup(s)
	retiring := sharedHostID(group, 0)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, retiring,
		(&compute.Sandbox{Status: compute.RUNNING,
			SessionInfo: compute.SessionInfo{Group: group, Capacity: 2, ClosingAt: time.Now()}}).Encode).Attrs())
	require.NoError(t, err)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, retiring,
		(&compute.Sandbox{Status: compute.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.NoError(t, c.Reconcile(ctx, s, &entity.Meta{}))
	resp, err := inm.EAC.Get(ctx, shared.BindingID(s.ID).String())
	require.NoError(t, err)
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	require.Equal(t, sharedHostID(group, 1).String(), binding.Sandbox)
	require.NoError(t, c.SweepOrphans(ctx))
	host, err := c.getSandbox(ctx, retiring)
	require.NoError(t, err)
	require.Equal(t, compute.STOPPED, host.Status)
}

func TestSharedSessionDoesNotJoinOldVersionHost(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c := NewController(slog.Default(), inm.EAC)
	app, err := inm.Client.Create(ctx, "one", &core.App{ActiveVersion: "app_version/v2"})
	require.NoError(t, err)
	s := &sessionapi.Session{ID: "session/new", App: app, Version: "app_version/v2", MaxSessionsPerSandbox: 2,
		Spec:         sessionapi.SandboxSpec{Version: "app_version/v2", Container: []sessionapi.SandboxSpecContainer{{Image: "example:v2"}}},
		DesiredState: sessionapi.RUNNING}
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, s.ID, s.Encode).Attrs())
	require.NoError(t, err)
	group := sharedGroup(s)
	old := sharedHostID(group, 0)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, old,
		(&compute.Sandbox{Status: compute.RUNNING, SessionInfo: compute.SessionInfo{Group: group, Capacity: 2},
			Spec: compute.SandboxSpec{Version: "app_version/v1"}}).Encode).Attrs())
	require.NoError(t, err)
	require.NoError(t, c.Reconcile(ctx, s, &entity.Meta{}))
	resp, err := inm.EAC.Get(ctx, shared.BindingID(s.ID).String())
	require.NoError(t, err)
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	require.Equal(t, sharedHostID(group, 1).String(), binding.Sandbox)
}

func TestSharedSessionsConcurrentAdmissionHonorsCapacity(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c := NewController(slog.Default(), inm.EAC)
	const count = 8
	var sessions [count]sessionapi.Session
	for i := range sessions {
		id := entity.Id(fmt.Sprintf("session/concurrent-%d", i))
		_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id,
			(&sessionapi.Session{App: "app/one", MaxSessionsPerSandbox: 2,
				Spec:         sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "example:v1"}}},
				DesiredState: sessionapi.RUNNING}).Encode).Attrs())
		require.NoError(t, err)
		resp, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		sessions[i].Decode(resp.Entity().Entity())
	}
	var wg sync.WaitGroup
	errs := make([]error, count)
	for i := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = c.Reconcile(ctx, &sessions[i], &entity.Meta{})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	bindings, err := inm.EAC.List(ctx, entity.Ref(entity.EntityKind, sessionapi.KindBinding))
	require.NoError(t, err)
	require.Len(t, bindings.Values(), count)
	counts := make(map[string]int)
	for _, e := range bindings.Values() {
		var binding sessionapi.Binding
		binding.Decode(e.Entity())
		counts[binding.Sandbox]++
	}
	for host, n := range counts {
		require.LessOrEqual(t, n, 2, "host %s exceeded capacity", host)
	}
}

func TestSharedSessionConcurrentReconcileReservesOneSlot(t *testing.T) {
	ctx := t.Context()
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c := NewController(slog.Default(), inm.EAC)
	id := entity.Id("session/same")
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id,
		(&sessionapi.Session{App: "app/one", MaxSessionsPerSandbox: 2,
			Spec:         sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "example:v1"}}},
			DesiredState: sessionapi.RUNNING}).Encode).Attrs())
	require.NoError(t, err)
	resp, err := inm.EAC.Get(ctx, id.String())
	require.NoError(t, err)
	var s sessionapi.Session
	s.Decode(resp.Entity().Entity())

	const concurrent = 8
	var wg sync.WaitGroup
	errs := make([]error, concurrent)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = c.Reconcile(ctx, &s, &entity.Meta{})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	slots, err := inm.EAC.List(ctx, entity.String(sessionapi.SlotSessionId, id.String()))
	require.NoError(t, err)
	require.Len(t, slots.Values(), 1, "one Session must not consume both slots in a capacity-two host")
}
