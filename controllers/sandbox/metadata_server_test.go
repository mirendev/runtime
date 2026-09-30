package sandbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
)

func TestMetadataHandler_RoutesTokenAndActivityWithSharedAuthentication(t *testing.T) {
	c := newTestTokenController(t)
	inm, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	c.EAC = inm.EAC
	_, err := inm.EAC.Create(t.Context(), entity.New(entity.DBId, entity.Id(testSandboxID),
		(&compute.Sandbox{Status: compute.RUNNING}).Encode).Attrs())
	require.NoError(t, err)
	handler := c.metadataHandler()

	request := func(method, path string, authorized bool) *httptest.ResponseRecorder {
		t.Helper()
		r := authedRequest(method, path)
		if !authorized {
			r.Header.Del("Authorization")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	token := request(http.MethodGet, "/v1/token", true)
	require.Equal(t, http.StatusOK, token.Code)
	var value tokenResponse
	require.NoError(t, json.Unmarshal(token.Body.Bytes(), &value))
	require.NotEmpty(t, value.Value)

	activity := request(http.MethodGet, "/v1/activity", true)
	require.Equal(t, http.StatusOK, activity.Code)
	require.JSONEq(t, `{"shutdown_at":null}`, activity.Body.String())
	report := httptest.NewRequest(http.MethodPost, "/v1/activity", strings.NewReader(`{"state":"idle"}`))
	report.RemoteAddr = testSandboxIP + ":12345"
	report.Header.Set("Authorization", "Bearer "+testSecret)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, report)
	require.Equal(t, http.StatusNoContent, w.Code)
	resp, err := inm.EAC.Get(t.Context(), testSandboxID)
	require.NoError(t, err)
	var sb compute.Sandbox
	sb.Decode(resp.Entity().Entity())
	require.Equal(t, compute.IDLE, sb.Activity.State)
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/v1/activity", false).Code)
	require.Equal(t, http.StatusUnauthorized, request(http.MethodGet, "/v1/token", false).Code)
	require.Equal(t, http.StatusNotFound, request(http.MethodGet, "/v1/unknown", true).Code)
}
