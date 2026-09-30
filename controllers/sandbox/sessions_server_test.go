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
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestMetadataSessionsDeletionPersistsUntilAcknowledged(t *testing.T) {
	ctx := t.Context()
	c := newTestTokenController(t)
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c.EAC = inm.EAC
	handler := c.metadataHandler()
	ids := []entity.Id{"session/one", "session/two"}
	expectedImages := map[string]string{"session/one": "first:v1", "session/two": "second:v2"}
	for i, id := range ids {
		image := []string{"first:v1", "second:v2"}[i]
		_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, id,
			(&sessionapi.Session{Sandbox: entity.Id(testSandboxID), App: "app/one", Version: entity.Id("app_version/" + image),
				Service: "web", Group: "queue", MaxSessionsPerSandbox: 2, DesiredState: sessionapi.RUNNING,
				Spec: sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: image, Env: []string{"SESSION_KEY=" + image}}}}}).Encode).Attrs())
		require.NoError(t, err)
		_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, shared.BindingID(id),
			(&sessionapi.Binding{Session: id.String(), Sandbox: testSandboxID}).Encode).Attrs())
		require.NoError(t, err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = testSandboxIP + ":12345"
		r.Header.Set("Authorization", "Bearer "+testSecret)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	assertSnapshot := func(w *httptest.ResponseRecorder, sessions, deleted []string) string {
		t.Helper()
		require.Equal(t, http.StatusOK, w.Code)
		var result sessionsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, sessions, result.Sessions)
		require.Equal(t, deleted, result.Deleted)
		require.Len(t, result.SessionDetails, len(sessions))
		for _, id := range sessions {
			detail, ok := result.SessionDetails[id]
			require.True(t, ok)
			require.Equal(t, entity.Id("app/one"), detail.App)
			require.Equal(t, "web", detail.Service)
			require.Equal(t, "queue", detail.Group)
			image := expectedImages[id]
			require.Equal(t, entity.Id("app_version/"+map[string]string{"session/one": "first:v1", "session/two": "second:v2"}[id]), detail.Version)
			require.Equal(t, image, detail.Spec.Container[0].Image)
			require.Equal(t, []string{"SESSION_KEY=" + image}, detail.Spec.Container[0].Env)
		}
		require.NotEmpty(t, result.Version)
		return result.Version
	}
	version := assertSnapshot(request(http.MethodGet, "/v1/sessions", ""),
		[]string{"session/one", "session/two"}, []string{})
	// A long poll waits rather than returning an unchanged snapshot.
	wait := func(version string) <-chan *httptest.ResponseRecorder {
		t.Helper()
		ch := make(chan *httptest.ResponseRecorder, 1)
		go func() { ch <- request(http.MethodGet, "/v1/sessions?wait="+version, "") }()
		return ch
	}
	changed := wait(version)
	select {
	case <-changed:
		t.Fatal("long poll returned without a change")
	case <-time.After(100 * time.Millisecond):
	}
	_, err := inm.EAC.Create(ctx, entity.New(entity.DBId, shared.BindingID("session/unrelated"),
		(&sessionapi.Binding{Session: "session/unrelated", Sandbox: "sandbox/other"}).Encode).Attrs())
	require.NoError(t, err)
	_, err = inm.EAC.Create(ctx, entity.New(entity.DBId, entity.Id("session/unrelated"),
		(&sessionapi.Session{Sandbox: "sandbox/other", MaxSessionsPerSandbox: 2, DesiredState: sessionapi.RUNNING,
			Spec: sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "private:v1"}}}}).Encode).Attrs())
	require.NoError(t, err)
	assertSnapshot(request(http.MethodGet, "/v1/sessions", ""), []string{"session/one", "session/two"}, []string{})
	select {
	case <-changed:
		t.Fatal("another sandbox's binding woke the long poll")
	case <-time.After(100 * time.Millisecond):
	}
	// A changed per-Session spec must wake the poll even if membership is unchanged.
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, ids[1],
		(&sessionapi.Session{Spec: sessionapi.SandboxSpec{Container: []sessionapi.SandboxSpecContainer{{Image: "second:v3", Env: []string{"SESSION_KEY=second:v3"}}}}}).Encode).Attrs(), 0)
	require.NoError(t, err)
	select {
	case w := <-changed:
		require.Equal(t, http.StatusOK, w.Code)
		var result sessionsResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.NotEqual(t, version, result.Version)
		version = result.Version
		require.Equal(t, []string{"session/one", "session/two"}, result.Sessions)
		require.Equal(t, "second:v3", result.SessionDetails["session/two"].Spec.Container[0].Image)
		expectedImages["session/two"] = "second:v3"
	case <-time.After(4 * time.Second):
		t.Fatal("long poll did not deliver changed Session spec")
	}
	changed = wait(version)
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, ids[1],
		(&sessionapi.Session{DesiredState: sessionapi.SUSPENDED}).Encode).Attrs(), 0)
	require.NoError(t, err)
	select {
	case w := <-changed:
		version = assertSnapshot(w, []string{"session/one"}, []string{})
	case <-time.After(4 * time.Second):
		t.Fatal("long poll did not deliver suspension")
	}
	_, err = inm.EAC.Patch(ctx, entity.New(entity.DBId, ids[1],
		(&sessionapi.Session{DesiredState: sessionapi.RUNNING}).Encode).Attrs(), 0)
	require.NoError(t, err)
	version = assertSnapshot(request(http.MethodGet, "/v1/sessions?wait="+version, ""),
		[]string{"session/one", "session/two"}, []string{})
	require.Equal(t, http.StatusConflict,
		request(http.MethodPost, "/v1/sessions/deletions/ack", `{"session":"session/one"}`).Code)
	require.Equal(t, http.StatusNotFound,
		request(http.MethodPost, "/v1/sessions/ack", `{"session":"session/one"}`).Code)
	changed = wait(version)
	_, err = inm.EAC.Delete(ctx, ids[0].String())
	require.NoError(t, err)
	// Even if the coordinator's deletion callback is lost, polling discovers
	// the missing Session and persists the notification.
	select {
	case w := <-changed:
		version = assertSnapshot(w, []string{"session/two"}, []string{"session/one"})
	case <-time.After(4 * time.Second):
		t.Fatal("long poll did not deliver deletion")
	}
	resp, err := inm.EAC.Get(ctx, shared.BindingID(ids[0]).String())
	require.NoError(t, err)
	var binding sessionapi.Binding
	binding.Decode(resp.Entity().Entity())
	require.WithinDuration(t, time.Now(), binding.DeletedAt, time.Second)
	assertSnapshot(request(http.MethodGet, "/v1/sessions", ""), []string{"session/two"}, []string{"session/one"})
	require.Equal(t, http.StatusNoContent,
		request(http.MethodPost, "/v1/sessions/deletions/ack", `{"session":"session/one"}`).Code)
	require.Equal(t, http.StatusNoContent,
		request(http.MethodPost, "/v1/sessions/deletions/ack", `{"session":"session/one"}`).Code)
	version = assertSnapshot(request(http.MethodGet, "/v1/sessions?wait="+version, ""), []string{"session/two"}, []string{})

	// Disconnecting a waiting client must release its handler promptly.
	ctxCancel, cancel := context.WithCancel(ctx)
	r := httptest.NewRequest(http.MethodGet, "/v1/sessions?wait="+version, nil).WithContext(ctxCancel)
	r.RemoteAddr = testSandboxIP + ":12345"
	r.Header.Set("Authorization", "Bearer "+testSecret)
	done := make(chan struct{})
	go func() { handler.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled long poll did not exit")
	}

	// The secret for a different sandbox cannot acknowledge this binding.
	c.NetServ.AddSandboxMapping("sandbox/other", "10.0.0.6", "other", "web")
	c.tokenSecrets.register("sandbox/other", "other-secret")
	other := httptest.NewRequest(http.MethodPost, "/v1/sessions/deletions/ack", strings.NewReader(`{"session":"session/one"}`))
	other.RemoteAddr = "10.0.0.6:12345"
	other.Header.Set("Authorization", "Bearer other-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, other)
	require.Equal(t, http.StatusForbidden, w.Code)
}
