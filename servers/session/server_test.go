package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

func TestSessionsRESTLifecycleAndAppScope(t *testing.T) {
	ctx := t.Context()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	for _, name := range []string{"workers", "other"} {
		appID, err := inmem.Client.Create(ctx, name, &core_v1alpha.App{})
		require.NoError(t, err)
		configID, err := inmem.Client.Create(ctx, name+"-config", &core_v1alpha.ConfigVersion{
			App: appID, Spec: core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{Name: "web", Command: "bin/worker", Image: "example:service"}}},
		})
		require.NoError(t, err)
		versionID, err := inmem.Client.Create(ctx, name+"-v1", &core_v1alpha.AppVersion{
			App: appID, ConfigVersion: configID, ImageUrl: "example:v1", Version: "v1",
		})
		require.NoError(t, err)
		_, err = inmem.EAC.Patch(ctx, entity.New(entity.DBId, appID,
			(&core_v1alpha.App{ActiveVersion: versionID}).Encode).Attrs(), 0)
		require.NoError(t, err)
	}
	mux := http.NewServeMux()
	rpc.RegisterREST(mux, sessionapi.AdaptSessions(NewServer(slog.Default(), inmem.EAC)))
	request := func(boundApp, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if boundApp != "" {
			r = r.WithContext(rpc.ContextWithIdentity(ctx, &rpc.Identity{
				Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": boundApp},
			}))
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/apps/workers/sessions"
	require.Equal(t, http.StatusUnauthorized, request("", http.MethodGet, path, "").Code)
	require.Equal(t, http.StatusForbidden, request("other", http.MethodGet, path, "").Code)
	require.Equal(t, http.StatusBadRequest, request("workers", http.MethodPost, path,
		`{"name":"bad/name","max_sessions_per_sandbox":2}`).Code)
	require.Equal(t, http.StatusBadRequest, request("workers", http.MethodPost, path,
		`{"name":"invalid","max_sessions_per_sandbox":0}`).Code)
	for _, timeout := range []int64{-1, 86400*365 + 1} {
		require.Equal(t, http.StatusBadRequest, request("workers", http.MethodPost, path,
			fmt.Sprintf(`{"name":"invalid-idle","idle_timeout_seconds":%d}`, timeout)).Code)
	}
	created := request("workers", http.MethodPost, path,
		`{"name":"queue-1","service":"web","group":"priority","max_sessions_per_sandbox":2}`)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var result struct {
		Session struct {
			ID                    string
			DesiredState          string `json:"desired_state"`
			Version               string
			Group                 string
			MaxSessionsPerSandbox int64 `json:"max_sessions_per_sandbox"`
		} `json:"session"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &result))
	require.Equal(t, "session/workers/queue-1", result.Session.ID)
	require.Equal(t, "priority", result.Session.Group)
	require.Equal(t, int64(2), result.Session.MaxSessionsPerSandbox)
	require.Equal(t, "running", result.Session.DesiredState)
	require.Equal(t, "app_version/workers-v1", result.Session.Version)
	require.NotContains(t, created.Body.String(), "bin/worker", "REST summaries must not expose the execution spec")
	stored, err := inmem.EAC.Get(ctx, result.Session.ID)
	require.NoError(t, err)
	var session sessionapi.Session
	session.Decode(stored.Entity().Entity())
	require.Equal(t, "priority", session.Group)
	require.Equal(t, int64(300), session.IdleTimeoutSeconds, "omitted timeout defaults to five minutes")
	require.Contains(t, created.Body.String(), `"activity":"unknown"`)
	require.Equal(t, "docker.io/library/example:service", session.Spec.Container[0].Image)
	require.Equal(t, http.StatusConflict, request("workers", http.MethodPost, path,
		`{"name":"queue-1","max_sessions_per_sandbox":2}`).Code)
	require.Equal(t, http.StatusForbidden, request("other", http.MethodDelete, path+"/queue-1", "").Code)
	listed := request("workers", http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, listed.Code)
	var listing struct {
		Sessions []json.RawMessage `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &listing))
	require.Len(t, listing.Sessions, 1)
	get := request("workers", http.MethodGet, path+"/queue-1", "")
	require.Equal(t, http.StatusOK, get.Code)
	require.Contains(t, get.Body.String(), "session/workers/queue-1")
	require.Equal(t, http.StatusBadRequest, request("workers", http.MethodPut, path+"/queue-1/desired-state",
		`{"desired_state":"draining"}`).Code)
	updated := request("workers", http.MethodPut, path+"/queue-1/desired-state", `{"desired_state":"suspended"}`)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	require.Contains(t, updated.Body.String(), `"desired_state":"suspended"`)
	require.Equal(t, http.StatusOK, request("workers", http.MethodDelete, path+"/queue-1", "").Code)
	require.Equal(t, http.StatusNotFound, request("workers", http.MethodGet, path+"/queue-1", "").Code)
	require.Equal(t, http.StatusOK, request("other", http.MethodGet, "/api/v1/apps/other/sessions", "").Code)
	var after struct {
		Sessions []json.RawMessage `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(request("workers", http.MethodGet, path, "").Body.Bytes(), &after))
	require.Empty(t, after.Sessions)
	for _, timeout := range []int64{0, 47} {
		created := request("workers", http.MethodPost, path,
			fmt.Sprintf(`{"name":"custom-idle-%d","idle_timeout_seconds":%d}`, timeout, timeout))
		require.Equal(t, http.StatusOK, created.Code, created.Body.String())
		stored, err := inmem.EAC.Get(ctx, fmt.Sprintf("session/workers/custom-idle-%d", timeout))
		require.NoError(t, err)
		var s sessionapi.Session
		s.Decode(stored.Entity().Entity())
		require.Equal(t, timeout, s.IdleTimeoutSeconds, "explicit zero must not become the default timeout")
	}
}

func TestSessionsRPCAppScope(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	client := sessionapi.NewSessionsClient(rpc.LocalClient(sessionapi.AdaptSessions(NewServer(slog.Default(), inmem.EAC))))
	ctx := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{
		Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": "one"},
	})
	_, err := client.Create(ctx, "two", "job", "web", "", 1, 300)
	require.ErrorIs(t, err, rpc.ErrUnauthorized)
	_, err = client.List(ctx, "two")
	require.ErrorIs(t, err, rpc.ErrUnauthorized)
}

func TestSessionNamesDoNotCollideAcrossApps(t *testing.T) {
	first, err := sessionID("my", "app-x")
	require.NoError(t, err)
	second, err := sessionID("my-app", "x")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, entity.Id("session/my/app-x"), first)
}
