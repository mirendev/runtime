package runnertelemetry_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/runnertelemetry"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

// stubVerifier stands in for the cluster issuer. It records what it was asked
// so the audience and workload the handler demands can be asserted, which is
// the part that keeps a token minted for another service from being replayed
// here.
type stubVerifier struct {
	err error

	gotToken    string
	gotAudience string
	gotWorkload workloadidentity.SystemWorkload
}

func (v *stubVerifier) VerifySystemWorkloadToken(token, audience string, workload workloadidentity.SystemWorkload) (*workloadidentity.WorkloadClaims, error) {
	v.gotToken, v.gotAudience, v.gotWorkload = token, audience, workload
	if v.err != nil {
		return nil, v.err
	}
	return &workloadidentity.WorkloadClaims{
		IdentityType:   workloadidentity.IdentityTypeSystem,
		SystemWorkload: workload,
	}, nil
}

type backend struct {
	srv     *httptest.Server
	gotBody string
	gotPath string
	gotType string
	status  int
}

func newBackend(t *testing.T) *backend {
	t.Helper()
	b := &backend{status: http.StatusNoContent}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		b.gotBody = string(body)
		b.gotPath = r.URL.Path
		b.gotType = r.Header.Get("Content-Type")
		w.WriteHeader(b.status)
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *backend) address() string {
	return strings.TrimPrefix(b.srv.URL, "http://")
}

func post(h http.Handler, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, runnertelemetry.MetricsBasePath, strings.NewReader(body))
	if token != "" {
		req.Header.Set(runnertelemetry.TokenHeader, token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMetricsHandlerForwardsVerifiedBatch(t *testing.T) {
	be := newBackend(t)
	v := &stubVerifier{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), v, be.address())

	rec := post(h, "a-token", "test_metric 1 1234567890000\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "test_metric 1 1234567890000\n", be.gotBody)
	require.Equal(t, "/api/v1/import/prometheus", be.gotPath)
	// The writer sends no Content-Type, so the handler supplies the one the
	// backend expects rather than forwarding an empty header.
	require.Equal(t, "text/plain", be.gotType)

	// The audience and workload are what stop a token minted for another
	// service, or for another system workload, from being spent here.
	require.Equal(t, "a-token", v.gotToken)
	require.Equal(t, runnertelemetry.Audience, v.gotAudience)
	require.Equal(t, workloadidentity.SystemWorkloadTelemetryWriter, v.gotWorkload)
}

func TestLogsHandlerForwardsVerifiedBatch(t *testing.T) {
	be := newBackend(t)
	be.status = http.StatusOK
	h := runnertelemetry.NewLogsHandler(testLogger(), &stubVerifier{}, be.address())

	rec := post(h, "a-token", `{"_msg":"hello"}`+"\n")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, `{"_msg":"hello"}`+"\n", be.gotBody)
	require.Equal(t, "/insert/jsonline", be.gotPath)
	require.Equal(t, "application/x-ndjson", be.gotType)
}

func TestIngestRejectsMissingToken(t *testing.T) {
	be := newBackend(t)
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, be.address())

	rec := post(h, "", "test_metric 1 1\n")

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, be.gotBody, "nothing should reach the backend without a token")
}

func TestIngestRejectsInvalidToken(t *testing.T) {
	be := newBackend(t)
	h := runnertelemetry.NewMetricsHandler(testLogger(),
		&stubVerifier{err: io.ErrUnexpectedEOF}, be.address())

	rec := post(h, "a-bad-token", "test_metric 1 1\n")

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, be.gotBody, "nothing should reach the backend on a failed verification")
}

// A backend rejection has to surface rather than being flattened into success,
// since the runner decides whether to retry from what it gets back.
func TestIngestSurfacesBackendRejection(t *testing.T) {
	be := newBackend(t)
	be.status = http.StatusTooManyRequests
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, be.address())

	rec := post(h, "a-token", "test_metric 1 1\n")

	require.Equal(t, http.StatusBadGateway, rec.Code)
}

// The patterns pin one method and one exact path each. Proxying a whole prefix
// would expose the rest of both APIs, including reads and delete-series, which
// would make scoping the token meaningless.
func TestPatternsArePinnedToIngestPaths(t *testing.T) {
	require.Equal(t, "POST /_telemetry/metrics/api/v1/import/prometheus", runnertelemetry.MetricsPattern)
	require.Equal(t, "POST /_telemetry/logs/insert/jsonline", runnertelemetry.LogsPattern)

	// A runner's writer appends its backend-native suffix to the base path, so
	// the two must compose into exactly the mounted pattern.
	require.Equal(t, "POST "+runnertelemetry.MetricsBasePath+"/api/v1/import/prometheus",
		runnertelemetry.MetricsPattern)
	require.Equal(t, "POST "+runnertelemetry.LogsBasePath+"/insert/jsonline",
		runnertelemetry.LogsPattern)
}
