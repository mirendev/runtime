package appmetrics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/workloadidentity"
)

func TestTargetDiscoveryBuildsDistinctReplicaTargets(t *testing.T) {
	ctx := context.Background()
	server, cleanup := testutils.NewInMemEntityServer(t)
	defer cleanup()

	appID, err := server.Client.Create(ctx, "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	configID, err := server.Client.Create(ctx, "shop-v7", &core_v1alpha.ConfigVersion{
		App: appID,
		Spec: core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{{
			Name: "web",
			Metrics: core_v1alpha.ConfigSpecServicesMetrics{
				Enabled:  true,
				Path:     "/internal/metrics",
				Port:     9100,
				Interval: "45s",
			},
		}}},
	})
	require.NoError(t, err)
	versionID, err := server.Client.Create(ctx, "v7", &core_v1alpha.AppVersion{
		App:           appID,
		Version:       "v7",
		ConfigVersion: configID,
	})
	require.NoError(t, err)
	nodeID, err := server.Client.Create(ctx, "runner-west", &compute_v1alpha.Node{RunnerId: "runner-west"})
	require.NoError(t, err)

	createSandbox := func(name, address string) (entity.Id, *entity.Entity) {
		t.Helper()
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
		response, err := server.EAC.Get(ctx, id.String())
		require.NoError(t, err)
		return id, response.Entity().Entity()
	}

	firstID, firstEntity := createSandbox("replica-1", "10.8.0.2/24")
	secondID, secondEntity := createSandbox("replica-2", "10.8.0.3/24")
	discovery := newTargetDiscovery(testutils.TestLogger(t), server.EAC, filepath.Join(t.TempDir(), "targets.json"), "cluster-123")

	first, eligible, err := discovery.targetForSandbox(ctx, firstEntity)
	require.NoError(t, err)
	require.True(t, eligible)
	second, eligible, err := discovery.targetForSandbox(ctx, secondEntity)
	require.NoError(t, err)
	require.True(t, eligible)

	assert.Equal(t, []string{"10.8.0.2:9100"}, first.Targets)
	assert.Equal(t, "/internal/metrics", first.Labels["__metrics_path__"])
	assert.Equal(t, "45s", first.Labels["__scrape_interval__"])
	assert.Equal(t, "shop", first.Labels["miren_app"])
	assert.Equal(t, "v7", first.Labels["miren_app_version"])
	assert.Equal(t, "web", first.Labels["miren_service"])
	assert.Equal(t, "runner-west", first.Labels["miren_runner"])
	assert.Equal(t, "cluster-123", first.Labels["miren_cluster"])
	assert.Equal(t, firstID.String(), first.Labels["miren_sandbox"])
	assert.Equal(t, secondID.String(), second.Labels["miren_sandbox"])
	assert.NotEqual(t, first.Labels["miren_sandbox"], second.Labels["miren_sandbox"])

	discovery.targets[firstID.String()] = first
	discovery.targets[secondID.String()] = second
	require.NoError(t, discovery.writeLocked())
	data, err := os.ReadFile(discovery.path)
	require.NoError(t, err)
	var written []fileSDGroup
	require.NoError(t, json.Unmarshal(data, &written))
	require.Len(t, written, 2)
	assert.Equal(t, first.Targets, written[0].Targets)
	assert.Equal(t, second.Targets, written[1].Targets)

	watchPath := filepath.Join(t.TempDir(), "watched-targets.json")
	watched := newTargetDiscovery(testutils.TestLogger(t), server.EAC, watchPath, "cluster-123")
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	require.NoError(t, watched.Start(watchCtx))
	defer watched.Stop()
	require.Eventually(t, func() bool { return targetCount(watchPath) == 2 }, time.Second, 10*time.Millisecond)

	_, err = server.EAC.Patch(ctx, entity.New(
		entity.Ref(entity.DBId, firstID),
		(&compute_v1alpha.Sandbox{Status: compute_v1alpha.STOPPED}).Encode,
	).Attrs(), 0)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return targetCount(watchPath) == 1 }, time.Second, 10*time.Millisecond)

	require.NoError(t, server.Client.Delete(ctx, secondID))
	require.Eventually(t, func() bool { return targetCount(watchPath) == 0 }, time.Second, 10*time.Millisecond)
}

func targetCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	var targets []fileSDGroup
	if json.Unmarshal(data, &targets) != nil {
		return -1
	}
	return len(targets)
}

func TestTargetDiscoveryRejectsIneligibleSandbox(t *testing.T) {
	discovery := newTargetDiscovery(testutils.TestLogger(t), nil, "", "cluster-123")
	for _, status := range []compute_v1alpha.SandboxStatus{
		compute_v1alpha.PENDING,
		compute_v1alpha.NOT_READY,
		compute_v1alpha.STOPPED,
		compute_v1alpha.DEAD,
	} {
		en := entity.New(
			entity.DBId, entity.Id("sandbox/test"),
			(&compute_v1alpha.Sandbox{Status: status}).Encode,
		)
		_, eligible, err := discovery.targetForSandbox(context.Background(), en)
		require.NoError(t, err)
		assert.False(t, eligible)
	}
}

func TestRemoteWriteTokenIsPrivateTelemetryIdentity(t *testing.T) {
	dir := t.TempDir()
	issuer, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:  dir,
		IssuerURL: "https://cluster.example.com",
		ClusterID: "cluster-123",
	})
	require.NoError(t, err)

	component := &Component{issuer: issuer}
	path := filepath.Join(dir, "token")
	require.NoError(t, component.refreshToken(path, "metrics.example.com"))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = issuer.VerifySystemWorkloadToken(
		strings.TrimSpace(string(data)),
		"metrics.example.com",
		workloadidentity.SystemWorkloadTelemetryWriter,
	)
	require.NoError(t, err)
}

func TestScrapeSafetyLimits(t *testing.T) {
	assert.Contains(t, scrapeConfig, "scrape_timeout: 10s")
	assert.Contains(t, scrapeConfig, "max_scrape_size: 2097152")
	assert.Contains(t, scrapeConfig, "sample_limit: 10000")
	assert.Contains(t, scrapeConfig, "label_limit: 64")
	assert.Contains(t, vmagentArgs("https://metrics.example.com/write", 8429), "-promscrape.fileSDCheckInterval=5s")
	assert.Contains(t, vmagentArgs("https://metrics.example.com/write", 8429), "-remoteWrite.forcePromProto")
}
