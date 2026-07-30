package ocireg

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/workloadidentity"
)

func registryTestIssuer(t *testing.T) *workloadidentity.Issuer {
	t.Helper()

	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:  t.TempDir(),
		IssuerURL: "https://cluster.example.com",
	})
	require.NoError(t, err)
	return issuer
}

func registryToken(
	t *testing.T,
	issuer *workloadidentity.Issuer,
	workload workloadidentity.SystemWorkload,
	audience string,
) string {
	t.Helper()

	token, err := issuer.IssueSystemWorkloadToken(workload, workloadidentity.TokenOptions{
		Audience: []string{audience},
	})
	require.NoError(t, err)
	return token
}

func TestAuthorizeRegistry(t *testing.T) {
	issuer := registryTestIssuer(t)
	log := testutils.TestLogger(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := authorizeRegistry(next, issuer, log)

	sandboxToken := registryToken(t, issuer, workloadidentity.SystemWorkloadSandboxController, Audience)
	buildKitToken := registryToken(t, issuer, workloadidentity.SystemWorkloadBuildKit, Audience)
	telemetryToken := registryToken(t, issuer, workloadidentity.SystemWorkloadTelemetryWriter, Audience)
	wrongAudienceToken := registryToken(t, issuer, workloadidentity.SystemWorkloadBuildKit, "somewhere-else")
	customerToken, err := issuer.IssueTokenWithOptions("myapp", "sb-123", workloadidentity.TokenOptions{
		Audience: []string{Audience},
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		method     string
		token      string
		wantStatus int
	}{
		{name: "missing token", method: http.MethodGet, wantStatus: http.StatusUnauthorized},
		{name: "malformed token", method: http.MethodGet, token: "not-a-jwt", wantStatus: http.StatusUnauthorized},
		{name: "wrong audience", method: http.MethodGet, token: wrongAudienceToken, wantStatus: http.StatusUnauthorized},
		{name: "customer sandbox", method: http.MethodGet, token: customerToken, wantStatus: http.StatusForbidden},
		{name: "telemetry writer", method: http.MethodGet, token: telemetryToken, wantStatus: http.StatusForbidden},
		{name: "sandbox controller pull", method: http.MethodGet, token: sandboxToken, wantStatus: http.StatusNoContent},
		{name: "sandbox controller resolve", method: http.MethodHead, token: sandboxToken, wantStatus: http.StatusNoContent},
		{name: "sandbox controller push", method: http.MethodPost, token: sandboxToken, wantStatus: http.StatusForbidden},
		{name: "buildkit pull", method: http.MethodGet, token: buildKitToken, wantStatus: http.StatusNoContent},
		{name: "buildkit push", method: http.MethodPost, token: buildKitToken, wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/v2/myapp/manifests/latest", nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusUnauthorized {
				assert.Equal(t,
					`Bearer realm="http://example.com/v2/token",service="miren-registry"`,
					rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestRegistryMuxLeavesHealthCheckUnauthenticated(t *testing.T) {
	issuer := registryTestIssuer(t)
	handler := NewRegistryHandler(t.TempDir(), testutils.TestLogger(t), nil)
	mux := newMux(handler, issuer)

	root := httptest.NewRecorder()
	mux.ServeHTTP(root, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, root.Code)

	api := httptest.NewRecorder()
	mux.ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	assert.Equal(t, http.StatusUnauthorized, api.Code)
}
