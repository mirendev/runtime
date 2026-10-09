package sandbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
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
