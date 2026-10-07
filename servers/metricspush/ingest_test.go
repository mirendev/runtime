package metricspush

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	v1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	metrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/proto"

	"miren.dev/runtime/pkg/workloadidentity"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

// stubVerifier hands back whatever claims a test sets, and records the
// audience it was asked for so a test can pin that a token minted for anything
// else is not accepted here.
type stubVerifier struct {
	claims      *workloadidentity.WorkloadClaims
	err         error
	gotAudience string
}

func (v *stubVerifier) VerifyToken(_, audience string) (*workloadidentity.WorkloadClaims, error) {
	v.gotAudience = audience
	return v.claims, v.err
}

type stubResolver map[string]Sandbox

func (r stubResolver) Resolve(_ context.Context, id string) (Sandbox, error) {
	sb, ok := r[id]
	if !ok {
		return Sandbox{}, errors.New("no such sandbox")
	}
	return sb, nil
}

type vmagent struct {
	srv    *httptest.Server
	calls  int
	path   string
	labels []string
	ctype  string
	body   []byte
	status int
	reply  string
}

func newVMAgent(t *testing.T) *vmagent {
	t.Helper()
	v := &vmagent{status: http.StatusNoContent}
	v.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.calls++
		v.path = r.URL.Path
		v.labels = r.URL.Query()["extra_label"]
		sort.Strings(v.labels)
		v.ctype = r.Header.Get("Content-Type")
		v.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(v.status)
		_, _ = io.WriteString(w, v.reply)
	}))
	t.Cleanup(v.srv.Close)
	return v
}

var bgtask = Sandbox{
	ID:      "sandbox/bg-1",
	App:     "cloud",
	Version: "v42",
	Service: "bgtask",
	Runner:  "runner-a",
}

func sandboxClaims(app, sandboxID string) *workloadidentity.WorkloadClaims {
	return &workloadidentity.WorkloadClaims{
		IdentityType: workloadidentity.IdentityTypeSandbox,
		App:          app,
		SandboxID:    sandboxID,
	}
}

type fixture struct {
	ingest   *Ingest
	verifier *stubVerifier
	vm       *vmagent
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	vm := newVMAgent(t)
	verifier := &stubVerifier{claims: sandboxClaims("cloud", bgtask.ID)}
	ingest := NewIngest(testLogger(), verifier, true)
	ingest.Arm(Backend{ImportURL: vm.srv.URL, ClusterID: "cluster-1", Resolver: stubResolver{bgtask.ID: bgtask}})
	return &fixture{ingest: ingest, verifier: verifier, vm: vm}
}

func promPush(scope Scope, body string) Push {
	return Push{
		Token:       "tok",
		Scope:       scope,
		Format:      FormatPrometheus,
		Grouping:    map[string]string{"job": "worker"},
		ContentType: "text/plain; version=0.0.4",
		Body:        []byte(body),
	}
}

func requireStatus(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	pe, ok := errors.AsType[*Error](err)
	require.True(t, ok, "expected a push error, got %v", err)
	require.Equal(t, status, pe.Status, pe.Message)
}

func TestSandboxScopeCarriesScrapeLabels(t *testing.T) {
	f := newFixture(t)

	err := f.ingest.Push(t.Context(), promPush(ScopeSandbox, "# TYPE jobs_processed_total counter\njobs_processed_total{queue=\"default\"} 12\n"))
	require.NoError(t, err)

	require.Equal(t, Audience, f.verifier.gotAudience)
	require.Equal(t, prometheusImportPath, f.vm.path)
	require.Equal(t, []string{
		"job=worker",
		"miren_app=cloud",
		"miren_app_version=v42",
		"miren_cluster=cluster-1",
		"miren_runner=runner-a",
		"miren_sandbox=sandbox/bg-1",
		"miren_service=bgtask",
	}, f.vm.labels)
	require.Contains(t, string(f.vm.body), `jobs_processed_total{queue="default"} 12`)
}

func TestAppScopeDropsPerSandboxLabels(t *testing.T) {
	f := newFixture(t)

	err := f.ingest.Push(t.Context(), promPush(ScopeApp, "# TYPE miren_total_users gauge\nmiren_total_users 1238\n"))
	require.NoError(t, err)

	require.Equal(t, []string{
		"job=worker",
		"miren_app=cloud",
		"miren_cluster=cluster-1",
		"miren_service=bgtask",
	}, f.vm.labels)
}

// The ticket's bar: a workload cannot push samples labeled as another app. It
// cannot name one in the payload, in the grouping key, or through an OTLP
// attribute that normalizes into the reserved namespace.
func TestReservedLabelsAreRefused(t *testing.T) {
	cases := map[string]Push{
		"payload label": promPush(ScopeSandbox, "up{miren_app=\"other\"} 1\n"),
		"grouping key": func() Push {
			p := promPush(ScopeSandbox, "up 1\n")
			p.Grouping["miren_app"] = "other"
			return p
		}(),
		"app scope payload sandbox label": promPush(ScopeApp, "# TYPE depth gauge\ndepth{miren_sandbox=\"forged\"} 3\n"),
		"otlp resource attribute":         otlpPush(ScopeSandbox, gaugeRequest(kv("miren.app", "other"), nil)),
		"otlp point attribute":            otlpPush(ScopeApp, gaugeRequest(nil, kv("miren_sandbox", "forged"))),
	}
	for name, push := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			requireStatus(t, f.ingest.Push(t.Context(), push), http.StatusBadRequest)
			require.Zero(t, f.vm.calls, "a refused push must not reach vmagent")
		})
	}
}

func TestAppScopeRefusesCumulativeMetrics(t *testing.T) {
	cases := map[string]Push{
		"typed counter":     promPush(ScopeApp, "# TYPE jobs_total counter\njobs_total 3\n"),
		"untyped _total":    promPush(ScopeApp, "jobs_total 3\n"),
		"histogram":         promPush(ScopeApp, "# TYPE lat histogram\nlat_bucket{le=\"+Inf\"} 1\nlat_sum 1\nlat_count 1\n"),
		"otlp monotonic":    otlpPush(ScopeApp, sumRequest(true)),
		"otlp delta levels": otlpPush(ScopeApp, deltaSumRequest()),
	}
	for name, push := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			requireStatus(t, f.ingest.Push(t.Context(), push), http.StatusBadRequest)
			require.Zero(t, f.vm.calls)
		})
	}

	t.Run("same counter at sandbox scope", func(t *testing.T) {
		f := newFixture(t)
		require.NoError(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, "# TYPE jobs_total counter\njobs_total 3\n")))
	})
	t.Run("otlp updown counter", func(t *testing.T) {
		f := newFixture(t)
		require.NoError(t, f.ingest.Push(t.Context(), otlpPush(ScopeApp, sumRequest(false))))
	})
}

func TestPrometheusProtobufIsImportedAsText(t *testing.T) {
	f := newFixture(t)

	// The Go Pushgateway client sends delimited protobuf by default.
	var body bytes.Buffer
	family := &dto.MetricFamily{
		Name: proto.String("queue_depth"),
		Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{
			Label: []*dto.LabelPair{{Name: proto.String("queue"), Value: proto.String("mail")}},
			Gauge: &dto.Gauge{Value: proto.Float64(7)},
		}},
	}
	_, err := protodelim.MarshalTo(&body, family)
	require.NoError(t, err)

	push := promPush(ScopeApp, "")
	push.ContentType = "application/vnd.google.protobuf; proto=io.prometheus.client.MetricFamily; encoding=delimited"
	push.Body = body.Bytes()
	require.NoError(t, f.ingest.Push(t.Context(), push))

	require.Equal(t, "text/plain", f.vm.ctype)
	require.Contains(t, string(f.vm.body), `queue_depth{queue="mail"} 7`)
}

func TestPrometheusTimestampsAreRefused(t *testing.T) {
	f := newFixture(t)
	requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, "up 1 1700000000000\n")), http.StatusBadRequest)
}

func TestIdentityChecks(t *testing.T) {
	t.Run("invalid token", func(t *testing.T) {
		f := newFixture(t)
		f.verifier.claims, f.verifier.err = nil, errors.New("bad signature")
		requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, "up 1\n")), http.StatusUnauthorized)
	})
	t.Run("system identity", func(t *testing.T) {
		f := newFixture(t)
		f.verifier.claims = &workloadidentity.WorkloadClaims{IdentityType: workloadidentity.IdentityTypeSystem}
		requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, "up 1\n")), http.StatusForbidden)
	})
	t.Run("unknown sandbox", func(t *testing.T) {
		f := newFixture(t)
		f.verifier.claims = sandboxClaims("cloud", "sandbox/gone")
		requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, "up 1\n")), http.StatusForbidden)
	})
	t.Run("token app disagrees with sandbox", func(t *testing.T) {
		f := newFixture(t)
		f.verifier.claims = sandboxClaims("other", bgtask.ID)
		requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, "up 1\n")), http.StatusForbidden)
		require.Zero(t, f.vm.calls)
	})
}

func TestUnarmedIngestIsUnavailable(t *testing.T) {
	f := newFixture(t)
	f.ingest.Disarm()
	requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, "up 1\n")), http.StatusServiceUnavailable)
}

// A cluster without a remote-write destination refuses pushes whatever state
// vmagent is in, and says so on the status route runners ask.
func TestIngestWithoutRemoteWrite(t *testing.T) {
	vm := newVMAgent(t)
	ingest := NewIngest(testLogger(), &stubVerifier{claims: sandboxClaims("cloud", bgtask.ID)}, false)
	ingest.Arm(Backend{ImportURL: vm.srv.URL, ClusterID: "c", Resolver: stubResolver{bgtask.ID: bgtask}})

	require.False(t, ingest.Available(t.Context()))
	requireStatus(t, ingest.Push(t.Context(), promPush(ScopeSandbox, "up 1\n")), http.StatusServiceUnavailable)
	require.Zero(t, vm.calls)

	rec := httptest.NewRecorder()
	ingest.StatusHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, IngestPath, nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	enabled := NewIngest(testLogger(), &stubVerifier{}, true)
	rec = httptest.NewRecorder()
	enabled.StatusHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, IngestPath, nil))
	require.Equal(t, http.StatusNoContent, rec.Code)

	// Configured, but managed metrics could not start: stop advertising.
	enabled.Fail()
	require.False(t, enabled.Available(t.Context()))
}

func TestVMAgentRejectionReachesWorkload(t *testing.T) {
	f := newFixture(t)
	f.vm.status, f.vm.reply = http.StatusBadRequest, "cannot parse"
	requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, "up 1\n")), http.StatusBadRequest)

	f.vm.status = http.StatusServiceUnavailable
	requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, "up 1\n")), http.StatusBadGateway)
}

func TestSeriesLimit(t *testing.T) {
	f := newFixture(t)
	var body strings.Builder
	body.WriteString("# TYPE depth gauge\n")
	for i := range maxSeriesPerPush + 1 {
		body.WriteString("depth{i=\"" + strconv.Itoa(i) + "\"} 1\n")
	}
	requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, body.String())), http.StatusRequestEntityTooLarge)
	require.Zero(t, f.vm.calls)
}

// A histogram is stored as a series per bucket plus _sum and _count, so the
// cap has to count those rather than one per histogram.
func TestSeriesLimitCountsHistogramBuckets(t *testing.T) {
	f := newFixture(t)
	var body strings.Builder
	body.WriteString("# TYPE lat histogram\n")
	// 1,000 histograms of 9 buckets (+Inf included) is 11,000 stored series.
	for i := range 1000 {
		for _, le := range []string{"0.01", "0.05", "0.1", "0.25", "0.5", "1", "2.5", "5", "+Inf"} {
			body.WriteString("lat_bucket{i=\"" + strconv.Itoa(i) + "\",le=\"" + le + "\"} 1\n")
		}
		body.WriteString("lat_sum{i=\"" + strconv.Itoa(i) + "\"} 1\nlat_count{i=\"" + strconv.Itoa(i) + "\"} 1\n")
	}
	requireStatus(t, f.ingest.Push(t.Context(), promPush(ScopeSandbox, body.String())), http.StatusRequestEntityTooLarge)
	require.Zero(t, f.vm.calls)
}

func TestOTLPForwardsUntouched(t *testing.T) {
	f := newFixture(t)

	req := gaugeRequest(kv("service.name", "bgtask"), kv("queue", "mail"))
	raw, err := proto.Marshal(req)
	require.NoError(t, err)

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(raw)
	require.NoError(t, zw.Close())

	push := otlpPush(ScopeApp, req)
	push.ContentEncoding = "gzip"
	push.Body = gz.Bytes()
	require.NoError(t, f.ingest.Push(t.Context(), push))

	require.Equal(t, otlpImportPath, f.vm.path)
	require.Equal(t, "application/x-protobuf", f.vm.ctype)
	require.Equal(t, raw, f.vm.body)
	require.Equal(t, []string{"miren_app=cloud", "miren_cluster=cluster-1", "miren_service=bgtask"}, f.vm.labels)
}

func TestHandlerRoundTrip(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewServer(f.ingest.Handler())
	t.Cleanup(srv.Close)

	send := func(body string, scope Scope) *http.Response {
		req, err := http.NewRequest(http.MethodPost, srv.URL+IngestPath, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set(TokenHeader, "tok")
		req.Header.Set(ScopeHeader, string(scope))
		req.Header.Set(FormatHeader, string(FormatPrometheus))
		req.Header.Set(GroupingHeader, "job=worker&instance=a")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	resp := send("# TYPE depth gauge\ndepth 3\n", ScopeApp)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Contains(t, f.vm.labels, "instance=a")

	resp = send("up{miren_app=\"x\"} 1\n", ScopeSandbox)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	msg, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(msg), `label "miren_app" is reserved`)
}

func kv(k, v string) *common.KeyValue {
	return &common.KeyValue{Key: k, Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: v}}}
}

func oneMetric(res, point *common.KeyValue, build func([]*common.KeyValue) *metrics.Metric) *v1.ExportMetricsServiceRequest {
	var resAttrs, pointAttrs []*common.KeyValue
	if res != nil {
		resAttrs = append(resAttrs, res)
	}
	if point != nil {
		pointAttrs = append(pointAttrs, point)
	}
	return &v1.ExportMetricsServiceRequest{ResourceMetrics: []*metrics.ResourceMetrics{{
		Resource:     &resource.Resource{Attributes: resAttrs},
		ScopeMetrics: []*metrics.ScopeMetrics{{Metrics: []*metrics.Metric{build(pointAttrs)}}},
	}}}
}

func gaugeRequest(res, point *common.KeyValue) *v1.ExportMetricsServiceRequest {
	return oneMetric(res, point, func(attrs []*common.KeyValue) *metrics.Metric {
		return &metrics.Metric{Name: "queue.depth", Data: &metrics.Metric_Gauge{Gauge: &metrics.Gauge{
			DataPoints: []*metrics.NumberDataPoint{{Attributes: attrs, Value: &metrics.NumberDataPoint_AsInt{AsInt: 7}}},
		}}}
	})
}

func sumRequest(monotonic bool) *v1.ExportMetricsServiceRequest {
	return oneMetric(nil, nil, func(attrs []*common.KeyValue) *metrics.Metric {
		return &metrics.Metric{Name: "jobs", Data: &metrics.Metric_Sum{Sum: &metrics.Sum{
			IsMonotonic:            monotonic,
			AggregationTemporality: metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
			DataPoints:             []*metrics.NumberDataPoint{{Attributes: attrs, Value: &metrics.NumberDataPoint_AsInt{AsInt: 3}}},
		}}}
	})
}

func deltaSumRequest() *v1.ExportMetricsServiceRequest {
	req := sumRequest(false)
	req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().AggregationTemporality =
		metrics.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA
	return req
}

func otlpPush(scope Scope, req *v1.ExportMetricsServiceRequest) Push {
	body, err := proto.Marshal(req)
	if err != nil {
		panic(err)
	}
	return Push{
		Token:       "tok",
		Scope:       scope,
		Format:      FormatOTLP,
		ContentType: "application/x-protobuf",
		Body:        body,
	}
}
