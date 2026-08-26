package appmetrics_test

import (
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

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/components/appmetrics"
	"miren.dev/runtime/internal/remotewrite"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/entity"
	entitytest "miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/testutils"
	"miren.dev/runtime/pkg/workloadidentity"
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
		received   []map[string]string
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
		for _, sample := range samples {
			received = append(received, sample.Labels)
		}
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
		for _, labels := range received {
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
