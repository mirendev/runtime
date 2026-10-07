package appmetrics_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/klauspost/compress/snappy"
	"github.com/stretchr/testify/require"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	otlpmetrics "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/components/appmetrics"
	"miren.dev/runtime/internal/remotewrite"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/entity"
	entitytest "miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/pkg/workloadidentity"
	"miren.dev/runtime/servers/metricspush"
)

func TestManagedMetricsRemoteWriteIntegration(t *testing.T) {
	cc, err := containerd.New(containerdx.DefaultSocket)
	if err != nil {
		t.Skipf("container runtime unavailable: %v", err)
	}
	defer cc.Close()
	containerdCtx, cancelContainerd := context.WithTimeout(context.Background(), 2*time.Second)
	_, err = cc.Version(containerdCtx)
	cancelContainerd()
	if err != nil {
		t.Skipf("container runtime unavailable: %v", err)
	}
	namespace := fmt.Sprintf("miren-app-metrics-test-%d", time.Now().UnixNano())

	entities, cleanupEntities := entitytest.NewInMemEntityServer(t)
	defer cleanupEntities()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	scrapePort := testutils.GetFreePort(t)
	listener, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", scrapePort))
	require.NoError(t, err)
	scrapeServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintln(w, "# TYPE demo_requests_total counter")
		fmt.Fprintln(w, `demo_requests_total{miren_app="spoofed",miren_sandbox="spoofed",miren_cluster="spoofed"} 42`)
	})}
	go scrapeServer.Serve(listener)
	defer scrapeServer.Shutdown(context.Background())

	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:  t.TempDir(),
		IssuerURL: "https://cluster.example.com",
		ClusterID: "cluster-123",
	})
	require.NoError(t, err)

	var (
		receivedMu sync.Mutex
		received   []remotewrite.Sample
		authed     bool
	)
	remoteWrite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if _, err := issuer.VerifySystemWorkloadToken(token, "metrics.example.com", workloadidentity.SystemWorkloadTelemetryWriter); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		compressed, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body, err := snappy.Decode(nil, compressed)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		samples, err := remotewrite.Decode(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		receivedMu.Lock()
		authed = true
		received = append(received, samples...)
		receivedMu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer remoteWrite.Close()

	firstSandbox, secondSandbox := seedMetricsReplicas(t, ctx, entities, scrapePort)
	component := appmetrics.New(entitytest.TestLogger(t), cc, namespace, t.TempDir(), entities.EAC, issuer)
	component.ReadyConfig.MaxAttempts = 60
	component.ReadyConfig.Interval = 250 * time.Millisecond
	err = component.Start(ctx, appmetrics.Config{
		RemoteWriteURL: remoteWrite.URL,
		Audience:       "metrics.example.com",
		ClusterID:      "cluster-123",
		HTTPPort:       testutils.GetFreePort(t),
	})
	require.NoError(t, err)
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		require.NoError(t, component.Stop(stopCtx))
	}()

	require.Eventually(t, func() bool {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		if !authed {
			return false
		}
		sandboxes := make(map[string]bool)
		for _, sample := range received {
			labels := sample.Labels
			if labels["__name__"] != "demo_requests_total" ||
				labels["miren_app"] != "shop" ||
				labels["miren_app_version"] != "v7" ||
				labels["miren_service"] != "web" ||
				labels["miren_runner"] != "runner-west" ||
				labels["miren_cluster"] != "cluster-123" {
				continue
			}
			sandboxes[labels["miren_sandbox"]] = true
		}
		return sandboxes[firstSandbox.String()] && sandboxes[secondSandbox.String()]
	}, 90*time.Second, 500*time.Millisecond, "authenticated remote write should receive distinctly labeled samples from both replicas")

	// The runtime's own operational gauges take the push path: the control
	// process writes them to vmagent's import endpoint, stamped with the same
	// identity labels the scrape path derives from targets.json, and they must
	// come out of the same authenticated remote write.
	pushed := metrics.NewVictoriaMetricsWriter(entitytest.TestLogger(t), component.ImportURL(), 10*time.Second)
	shipping := &metrics.Labeled{
		Sink:   pushed,
		Labels: map[string]string{"miren_cluster": "cluster-123", "miren_runner": "coordinator"},
	}
	pushedAt := time.Now()
	require.NoError(t, shipping.WritePoints(ctx, []metrics.MetricPoint{{
		Name:      "go_goroutines",
		Labels:    map[string]string{"entity": "miren/control"},
		Value:     4242,
		Timestamp: pushedAt,
	}}))
	pushed.Flush()

	require.Eventually(t, func() bool {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		for _, sample := range received {
			if sample.Labels["__name__"] != "go_goroutines" {
				continue
			}
			return sample.Value == 4242 &&
				sample.TimestampMS == pushedAt.UnixMilli() &&
				sample.Labels["entity"] == "miren/control" &&
				sample.Labels["miren_cluster"] == "cluster-123" &&
				sample.Labels["miren_runner"] == "coordinator"
		}
		return false
	}, 60*time.Second, 500*time.Millisecond, "a gauge pushed to vmagent's import endpoint should arrive labeled through the same remote write")

	// Workload pushes take the relay and the ingest, and must come out the far
	// side labeled from the sandbox's identity and nothing else.
	find := func(match func(map[string]string) bool) (remotewrite.Sample, bool) {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		for _, sample := range received {
			if match(sample.Labels) {
				return sample, true
			}
		}
		return remotewrite.Sample{}, false
	}
	exerciseWorkloadPush(t, ctx, component, issuer, entities, firstSandbox, secondSandbox, find)
}

// exerciseWorkloadPush drives pushes from two replicas of the seeded app through
// the relay and ingest into vmagent. Each replica authenticates with its own
// secret, standing in for the token server's source-address check.
func exerciseWorkloadPush(
	t *testing.T,
	ctx context.Context,
	component *appmetrics.Component,
	issuer *workloadidentity.Issuer,
	entities *entitytest.InMemEntityServer,
	first, second entity.Id,
	find func(func(map[string]string) bool) (remotewrite.Sample, bool),
) {
	t.Helper()

	ingest := metricspush.NewIngest(entitytest.TestLogger(t), issuer, true)
	ingest.Arm(metricspush.Backend{
		ImportURL: component.ImportURL(),
		ClusterID: "cluster-123",
		Resolver:  metricspush.NewEntityResolver(entities.EAC),
	})
	auth := func(_, secret string) (string, string, bool) {
		switch secret {
		case "first":
			return first.String(), "shop", true
		case "second":
			return second.String(), "shop", true
		}
		return "", "", false
	}
	mux := http.NewServeMux()
	metricspush.NewRelay(entitytest.TestLogger(t), auth, issuer, ingest).Register(mux)
	relay := httptest.NewServer(mux)
	defer relay.Close()

	push := func(secret, path, contentType string, body []byte) (int, string) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, relay.URL+path, bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", contentType)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(msg)
	}
	const text = "text/plain; version=0.0.4"

	// Sandbox scope: a worker's own counter, labeled exactly as a scrape would be.
	status, msg := push("first", "/v1/metrics/sandbox/metrics/job/worker", text,
		[]byte("# TYPE jobs_processed_total counter\njobs_processed_total 5\n"))
	require.Equal(t, http.StatusOK, status, msg)

	// App scope: both replicas push the same shared-state gauge.
	for _, secret := range []string{"first", "second"} {
		status, msg = push(secret, "/v1/metrics/app/metrics/job/worker", text,
			[]byte("# TYPE shop_open_orders gauge\nshop_open_orders 17\n"))
		require.Equal(t, http.StatusOK, status, msg)
	}

	// OTLP at app scope, with a dotted name vmagent should normalize.
	otlpBody, err := proto.Marshal(&colmetrics.ExportMetricsServiceRequest{ResourceMetrics: []*otlpmetrics.ResourceMetrics{{
		ScopeMetrics: []*otlpmetrics.ScopeMetrics{{Metrics: []*otlpmetrics.Metric{{
			Name: "shop.backlog",
			Data: &otlpmetrics.Metric_Gauge{Gauge: &otlpmetrics.Gauge{DataPoints: []*otlpmetrics.NumberDataPoint{{
				TimeUnixNano: uint64(time.Now().UnixNano()),
				Value:        &otlpmetrics.NumberDataPoint_AsInt{AsInt: 3},
			}}}},
		}}}},
	}}})
	require.NoError(t, err)
	status, msg = push("second", "/v1/metrics/app/otlp/v1/metrics", "application/x-protobuf", otlpBody)
	require.Equal(t, http.StatusOK, status, msg)

	// A replica cannot file samples under another app, and nothing of the
	// attempt reaches the destination.
	status, msg = push("first", "/v1/metrics/sandbox/metrics/job/worker", text,
		[]byte("forged_total{miren_app=\"bank\"} 1\n"))
	require.Equal(t, http.StatusBadRequest, status)
	require.Contains(t, msg, "miren_app")

	require.Eventually(t, func() bool {
		_, ok := find(func(l map[string]string) bool {
			return l["__name__"] == "jobs_processed_total" &&
				l["job"] == "worker" &&
				l["miren_app"] == "shop" &&
				l["miren_app_version"] == "v7" &&
				l["miren_service"] == "web" &&
				l["miren_sandbox"] == first.String() &&
				l["miren_runner"] == "runner-west" &&
				l["miren_cluster"] == "cluster-123"
		})
		return ok
	}, 60*time.Second, 500*time.Millisecond, "a sandbox-scoped push should carry the scrape path's labels")

	appScoped := func(name string) func(map[string]string) bool {
		return func(l map[string]string) bool {
			return l["__name__"] == name &&
				l["miren_app"] == "shop" &&
				l["miren_service"] == "web" &&
				l["miren_cluster"] == "cluster-123"
		}
	}
	require.Eventually(t, func() bool {
		_, gauge := find(appScoped("shop_open_orders"))
		_, otlp := find(appScoped("shop_backlog"))
		return gauge && otlp
	}, 60*time.Second, 500*time.Millisecond, "app-scoped pushes should arrive")

	for _, name := range []string{"shop_open_orders", "shop_backlog"} {
		sample, _ := find(appScoped(name))
		for _, label := range []string{"miren_sandbox", "miren_runner", "miren_app_version"} {
			require.NotContains(t, sample.Labels, label, "%s is app-scoped and must not carry %s", name, label)
		}
	}
	_, forged := find(func(l map[string]string) bool { return l["__name__"] == "forged_total" })
	require.False(t, forged, "a refused push must not reach remote write")
}

func seedMetricsReplicas(t *testing.T, ctx context.Context, server *entitytest.InMemEntityServer, port int) (entity.Id, entity.Id) {
	t.Helper()
	appID, err := server.Client.Create(ctx, "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	configID, err := server.Client.Create(ctx, "shop-v7", &core_v1alpha.ConfigVersion{
		App: appID,
		Spec: core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
			Name: "web",
			Metrics: core_v1alpha.ConfigSpecServicesMetrics{
				Enabled:  true,
				Path:     "/metrics",
				Port:     int64(port),
				Interval: "30s",
			},
		}}},
	})
	require.NoError(t, err)
	versionID, err := server.Client.Create(ctx, "v7", &core_v1alpha.AppVersion{App: appID, Version: "v7", ConfigVersion: configID})
	require.NoError(t, err)
	nodeID, err := server.Client.Create(ctx, "runner-west", &compute_v1alpha.Node{RunnerId: "runner-west"})
	require.NoError(t, err)

	create := func(name, address string) entity.Id {
		id, err := server.Client.Create(ctx, name, &compute_v1alpha.Sandbox{
			Status:  compute_v1alpha.RUNNING,
			Network: []compute_v1alpha.Network{{Address: address}},
			Spec: compute_v1alpha.SandboxSpec{
				Version:   versionID,
				Container: []compute_v1alpha.SandboxSpecContainer{{Image: "shop:v7"}},
			},
		}, entityserver.WithLabels(types.LabelSet("service", "web")))
		require.NoError(t, err)
		_, err = server.EAC.Patch(ctx, entity.New(
			entity.Ref(entity.DBId, id),
			(&compute_v1alpha.Schedule{Key: compute_v1alpha.Key{Kind: compute_v1alpha.KindSandbox, Node: nodeID}}).Encode,
		).Attrs(), 0)
		require.NoError(t, err)
		return id
	}
	return create("replica-1", "127.0.0.2/8"), create("replica-2", "127.0.0.3/8")
}
