package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	computeapi "miren.dev/runtime/api/compute"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	sessionctrl "miren.dev/runtime/controllers/session"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/x/workload"
)

func TestMetadataSessionActivityAndDetachment(t *testing.T) {
	ctx := t.Context()
	c := newTestTokenController(t)
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c.EAC = inm.EAC
	id := entity.Id("session/one")
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id, (&sessionapi.Session{
		Sandbox: entity.Id(testSandboxID), DesiredState: sessionapi.RUNNING, Phase: sessionapi.READY, MaxSessionsPerSandbox: 2,
	}).Encode).Attrs())
	require.NoError(t, err)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, shared.BindingID(id),
		(&sessionapi.Binding{Session: id.String(), Sandbox: testSandboxID}).Encode).Attrs())
	require.NoError(t, err)
	handler := c.metadataHandler()
	post := func(path string, body any) int {
		t.Helper()
		data, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(data)))
		r.RemoteAddr = testSandboxIP + ":12345"
		r.Header.Set("Authorization", "Bearer "+testSecret)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	read := func() sessionapi.Session {
		t.Helper()
		resp, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		var s sessionapi.Session
		s.Decode(resp.Entity().Entity())
		return s
	}
	report := func(state string) int {
		return post("/v1/sessions/activity", map[string]string{"session": id.String(), "state": state})
	}
	require.Equal(t, http.StatusNoContent, report("idle"))
	first := read()
	require.Equal(t, sessionapi.IDLE, first.Activity)
	require.False(t, first.IdleSince.IsZero())
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id,
		(&sessionapi.Session{ActivityAt: time.Now().Add(-40 * time.Second)}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, report("idle"))
	require.Equal(t, first.IdleSince, read().IdleSince, "heartbeats must not restart the idle timer")
	require.Equal(t, http.StatusNoContent, report("active"))
	require.Equal(t, sessionapi.ACTIVE, read().Activity)
	require.Zero(t, read().IdleSince)
	require.Equal(t, http.StatusNoContent, report("idle"))
	require.True(t, read().IdleSince.After(first.IdleSince))
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id,
		(&sessionapi.Session{DesiredState: sessionapi.SUSPENDED}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, report("active"), "a parking decision refuses new admission")
	require.Equal(t, sessionapi.IDLE, read().Activity)

	detachedAt := time.Now()
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, shared.BindingID(id),
		(&sessionapi.Binding{DetachedAt: detachedAt}).Encode).Attrs(), 0)
	require.NoError(t, err)
	snapshot, err := c.sessionsSnapshot(ctx, testSandboxID)
	require.NoError(t, err)
	require.Empty(t, snapshot.Sessions)
	require.Empty(t, snapshot.Deleted)
	require.True(t, detachedAt.Equal(snapshot.Detached[id.String()]))
	ack := func(at time.Time) int {
		return post("/v1/sessions/detachments/ack", map[string]any{"session": id.String(), "detached_at": at})
	}
	require.Equal(t, http.StatusNoContent, ack(detachedAt.Add(-time.Second)))
	resp, err := inm.EAC.Get(ctx, shared.BindingID(id).String())
	require.NoError(t, err)
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	require.Zero(t, binding.AcknowledgedAt, "a stale ack must not release a newer assignment")
	require.Equal(t, http.StatusNoContent, ack(detachedAt))
	snapshot, err = c.sessionsSnapshot(ctx, testSandboxID)
	require.NoError(t, err)
	require.Empty(t, snapshot.Detached)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id,
		(&sessionapi.Session{Sandbox: "sandbox/other", DesiredState: sessionapi.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, report("active"))
}

func TestDedicatedIdleResumeReplacesDrainingWorkload(t *testing.T) {
	ctx := t.Context()
	c := newTestTokenController(t)
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c.EAC = inm.EAC
	ctrl := sessionctrl.NewController(c.Log, inm.EAC)
	id := entity.Id("session/myapp/idle-resume")
	original := entity.Id(testSandboxID)
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id, (&sessionapi.Session{
		App: "app/myapp", Service: "web", Sandbox: original, Generation: 1,
		DesiredState: sessionapi.RUNNING, Phase: sessionapi.READY, IdleTimeoutSeconds: 60,
		Spec: sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "worker:v1"}}},
	}).Encode).Attrs())
	require.NoError(t, err)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, original,
		(&compute.Sandbox{Status: compute.RUNNING, SessionInfo: compute.SessionInfo{Owner: id}}).Encode).Attrs())
	require.NoError(t, err)
	read := func() *sessionapi.Session {
		t.Helper()
		resp, err := inm.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		var s sessionapi.Session
		s.Decode(resp.Entity().Entity())
		return &s
	}
	reconcile := func() {
		t.Helper()
		require.NoError(t, ctrl.Reconcile(ctx, read(), &entity.Meta{}))
	}
	startHost := func(sandbox entity.Id) (*workload.Host, context.CancelFunc) {
		t.Helper()
		c.NetServ.AddSandboxMapping(sandbox.String(), testSandboxIP, "myapp", "web")
		c.tokenSecrets.register(sandbox.String(), testSecret)
		handler := c.metadataHandler()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.RemoteAddr = testSandboxIP + ":12345"
			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(server.Close)
		host, err := workload.NewHost(workload.Config{URL: server.URL + "/v1", Secret: testSecret})
		require.NoError(t, err)
		loopCtx, cancel := context.WithCancel(ctx)
		started := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			done <- host.Run(loopCtx, func(context.Context, workload.Session) (workload.StopFunc, error) {
				started <- struct{}{}
				return func(context.Context, workload.StopReason) error { return nil }, nil
			})
		}()
		stop := func() {
			cancel()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("workload did not exit")
			}
		}
		t.Cleanup(cancel)
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("assignment did not start")
		}
		return host, stop
	}
	oldHost, stopOld := startHost(original)
	require.Eventually(t, func() bool { return read().Activity == sessionapi.IDLE }, 5*time.Second, 10*time.Millisecond)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id,
		(&sessionapi.Session{IdleSince: time.Now().Add(-time.Minute)}).Encode).Attrs(), 0)
	require.NoError(t, err)
	require.NoError(t, ctrl.SweepOrphans(ctx))
	require.Equal(t, sessionapi.SUSPENDED, read().DesiredState)
	reconcile()
	select {
	case <-oldHost.Draining():
	case <-time.After(15 * time.Second):
		t.Fatal("workload did not receive drain notice")
	}
	deadline := oldHost.ShutdownAt()
	require.True(t, deadline.After(time.Now()), "resume happens inside the drain grace window")
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, id,
		(&sessionapi.Session{DesiredState: sessionapi.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	reconcile()
	sbResp, err := inm.EAC.Get(ctx, original.String())
	require.NoError(t, err)
	var sb compute.Sandbox
	sb.Decode(sbResp.Entity().Entity())
	require.True(t, deadline.Equal(sb.ShutdownAt), "resume must preserve the latched drain notice")
	require.NotEqual(t, sessionapi.READY, read().Phase)
	_, err = oldHost.Begin(ctx, id.String())
	require.ErrorIs(t, err, workload.ErrDraining)
	stopOld()

	// Advance the deadline and emulate the runner's teardown acknowledgment.
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, original, (&compute.Sandbox{
		ShutdownAt: time.Now().Add(-time.Second), Activity: compute.Activity{State: compute.IDLE, ReportedAt: time.Now()},
	}).Encode).Attrs(), 0)
	require.NoError(t, err)
	reconcile()
	reconcile()
	require.Equal(t, original, read().Sandbox, "a stop request is not teardown proof")
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, computeapi.TeardownID(original),
		(&compute.SandboxTeardown{Sandbox: original.String(), Session: id.String()}).Encode).Attrs())
	require.NoError(t, err)
	reconcile()
	resumed := read()
	require.NotEqual(t, original, resumed.Sandbox)
	require.Equal(t, int64(2), resumed.Generation)
	require.Zero(t, resumed.ActivityAt)
	require.Zero(t, resumed.IdleSince)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, resumed.Sandbox,
		(&compute.Sandbox{Status: compute.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	reconcile()
	newHost, stopNew := startHost(resumed.Sandbox)
	defer stopNew()
	release, err := newHost.Begin(ctx, id.String())
	require.NoError(t, err, "the replacement workload must admit work after resume")
	release()
}
