package runnertelemetry_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/metrics"
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
	err      error
	runnerID string

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
		RunnerID:       v.runnerID,
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
	return postStream(h, token, "", body)
}

func postStream(h http.Handler, token, stream, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, runnertelemetry.MetricsBasePath, strings.NewReader(body))
	if token != "" {
		req.Header.Set(runnertelemetry.TokenHeader, token)
	}
	if stream != "" {
		req.Header.Set(runnertelemetry.StreamHeader, stream)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMetricsHandlerForwardsVerifiedBatch(t *testing.T) {
	be := newBackend(t)
	v := &stubVerifier{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), v, be.address(), nil)

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
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, be.address(), nil)

	rec := post(h, "", "test_metric 1 1\n")

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, be.gotBody, "nothing should reach the backend without a token")
}

func TestIngestRejectsInvalidToken(t *testing.T) {
	be := newBackend(t)
	h := runnertelemetry.NewMetricsHandler(testLogger(),
		&stubVerifier{err: io.ErrUnexpectedEOF}, be.address(), nil)

	rec := post(h, "a-bad-token", "test_metric 1 1\n")

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, be.gotBody, "nothing should reach the backend on a failed verification")
}

// A backend rejection has to surface rather than being flattened into success,
// since the runner decides whether to retry from what it gets back.
func TestIngestSurfacesBackendRejection(t *testing.T) {
	be := newBackend(t)
	be.status = http.StatusTooManyRequests
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, be.address(), nil)

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

type recordingWriter struct {
	points []metrics.MetricPoint
	err    error
}

func (w *recordingWriter) WritePoints(_ context.Context, points []metrics.MetricPoint) error {
	w.points = append(w.points, points...)
	return w.err
}

// Operational batches join the coordinator's own series in the fanout rather
// than going to the backend unread, which is what gets them shipped.
func TestMetricsHandlerRoutesOperationalBatchToWriter(t *testing.T) {
	be := newBackend(t)
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, be.address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational,
		`go_goroutines{entity="miren/runner",miren_runner="r1"} 42 1759170000000`+"\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, be.gotBody, "operational batches do not also go to the backend directly")
	require.Len(t, op.points, 1)
	require.Equal(t, "go_goroutines", op.points[0].Name)
	require.Equal(t, 42.0, op.points[0].Value)
	require.Equal(t, map[string]string{"entity": "miren/runner", "miren_runner": "r1"}, op.points[0].Labels)
}

// The shipping sink keeps a label a point already has, so a runner-supplied
// cluster label would file the series under whatever cluster the runner named.
func TestMetricsHandlerStripsRunnerSuppliedClusterLabel(t *testing.T) {
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational,
		`go_goroutines{miren_cluster="someone-else",miren_runner="r1"} 1 1759170000000`+"\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, op.points, 1)
	require.Equal(t, map[string]string{"miren_runner": "r1"}, op.points[0].Labels)
}

// Sandbox series carry no stream marker and keep going to the backend unread,
// so they stay in the embedded store just as the coordinator's own do.
func TestMetricsHandlerForwardsUnmarkedBatchWhenOperationalConfigured(t *testing.T) {
	be := newBackend(t)
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, be.address(), op)

	rec := post(h, "a-token", "memory_usage_bytes 1 1\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "memory_usage_bytes 1 1\n", be.gotBody)
	require.Empty(t, op.points)
}

// A coordinator with nowhere to put operational series treats them like any
// other batch, which is also exactly what a coordinator that predates the
// header does.
func TestMetricsHandlerForwardsOperationalBatchWithoutWriter(t *testing.T) {
	be := newBackend(t)
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, be.address(), nil)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational, "go_goroutines 1 1\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "go_goroutines 1 1\n", be.gotBody)
}

// A line that will not parse costs that line. Refusing the batch would have
// the runner's writer retry it, bad line and all, forever.
func TestMetricsHandlerSkipsUnparseableLines(t *testing.T) {
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational,
		"go_goroutines{broken 1\n"+`go_goroutines{miren_runner="r1"} 7 1`+"\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, op.points, 1)
	require.Equal(t, 7.0, op.points[0].Value)
}

// A dotted spelling of the cluster label would slip past a check for the exact
// name and then be sanitized into it by the shipping sink, so the line is
// skipped rather than delivered.
func TestMetricsHandlerSkipsDottedClusterLabel(t *testing.T) {
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational,
		`go_goroutines{miren.cluster="someone-else",miren_runner="r1"} 1 1`+"\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, op.points)
}

// The shipping sink stamps the coordinator's runner ID on a point without
// one, so a point that does not name a runner, or that names the control
// process, would ship as the coordinator's own series. This is the fallback
// for a token that names no runner, where the label is all there is to go on.
func TestMetricsHandlerSkipsPointsNotLabeledAsARunner(t *testing.T) {
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational, strings.Join([]string{
		`process_start_time_seconds{entity="miren/control"} 1 1`,
		`process_start_time_seconds{entity="miren/control",miren_runner="r1"} 1 1`,
		`process_start_time_seconds{entity="miren/runner",miren_runner=""} 1 1`,
		`process_start_time_seconds{entity="miren/runner",miren_runner="r1"} 2 1`,
	}, "\n")+"\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, op.points, 1)
	require.Equal(t, 2.0, op.points[0].Value)
}

// A token that names its runner settles attribution: the label a point
// carries is overwritten, so a runner cannot file its series under another's
// name, and a point with no label is stamped rather than dropped.
func TestMetricsHandlerAttributesOperationalPointsToVerifiedRunner(t *testing.T) {
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{runnerID: "r1"}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational, strings.Join([]string{
		`go_goroutines{entity="miren/runner",miren_runner="r2"} 1 1`,
		`go_goroutines{entity="miren/runner"} 2 1`,
		`go_goroutines{entity="miren/runner",miren_runner="r1"} 3 1`,
	}, "\n")+"\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, op.points, 3)
	for _, point := range op.points {
		require.Equal(t, map[string]string{"entity": "miren/runner", "miren_runner": "r1"}, point.Labels)
	}
}

// A verified runner is still not the coordinator, so naming the control
// process is refused whatever the token says.
func TestMetricsHandlerRefusesControlEntityFromVerifiedRunner(t *testing.T) {
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{runnerID: "r1"}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational,
		`process_start_time_seconds{entity="miren/control",miren_runner="r1"} 1 1`+"\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, op.points)
}

// Relabeling and stripping the cluster label happen together on one point.
func TestMetricsHandlerStripsClusterLabelWhileAttributing(t *testing.T) {
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{runnerID: "r1"}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational,
		`go_goroutines{miren_cluster="someone-else",miren_runner="r2"} 1 1`+"\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Len(t, op.points, 1)
	require.Equal(t, map[string]string{"miren_runner": "r1"}, op.points[0].Labels)
}

// A refused write is not refused back to the runner, whose writer would keep
// the batch, grow it, and retry it forever.
func TestMetricsHandlerAcceptsWhatTheSinkRefuses(t *testing.T) {
	op := &recordingWriter{err: errors.New("buffer full")}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational, `go_goroutines{miren_runner="r1"} 1 1`+"\n")

	require.Equal(t, http.StatusNoContent, rec.Code)
}

// The fanout reports one sink's refusal without saying which, so every chunk
// is still offered: a full shipping buffer must not cost the embedded store
// the rest of the batch.
func TestMetricsHandlerKeepsFeedingChunksAfterARefusal(t *testing.T) {
	op := &chunkRecordingWriter{refuseFirst: true}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational, runnerLines(2500))

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, []int{1000, 1000, 500}, op.chunks)
}

// A backlog bigger than the coordinator will hold is truncated, not refused,
// and reaches the sink in pieces it can take.
func TestMetricsHandlerTruncatesAndChunksLargeBatch(t *testing.T) {
	op := &chunkRecordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, newBackend(t).address(), op)

	rec := postStream(h, "a-token", runnertelemetry.StreamOperational, runnerLines(12345))

	require.Equal(t, http.StatusNoContent, rec.Code)
	total := 0
	for _, n := range op.chunks {
		require.LessOrEqual(t, n, 1000)
		total += n
	}
	require.Equal(t, 10000, total)
}

func runnerLines(n int) string {
	var body strings.Builder
	for i := 0; i < n; i++ {
		body.WriteString(`go_goroutines{miren_runner="r1"} 1 1` + "\n")
	}
	return body.String()
}

type chunkRecordingWriter struct {
	chunks      []int
	refuseFirst bool
}

func (w *chunkRecordingWriter) WritePoints(_ context.Context, points []metrics.MetricPoint) error {
	w.chunks = append(w.chunks, len(points))
	if w.refuseFirst && len(w.chunks) == 1 {
		return errors.New("buffer full")
	}
	return nil
}

func TestOperationalBatchStillRequiresToken(t *testing.T) {
	op := &recordingWriter{}
	h := runnertelemetry.NewMetricsHandler(testLogger(), &stubVerifier{}, newBackend(t).address(), op)

	rec := postStream(h, "", runnertelemetry.StreamOperational, `go_goroutines{miren_runner="r1"} 1 1`+"\n")

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Empty(t, op.points)
}
