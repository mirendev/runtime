package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/containerd/containerd/namespaces"
	"github.com/stretchr/testify/require"
	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	coreutil "miren.dev/runtime/api/core"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/appspec"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/testutils"
)

// Run with hack/it controllers/sandbox -p 1 -run TestCheckpointNative.
// Missing CRIU/kernel privileges skip before starting services. Once capability
// succeeds, runtime failures are test failures, never converted into skips.
func TestCheckpointNative(t *testing.T) {
	if err := checkCRIU(context.Background()); err != nil {
		t.Skipf("native checkpoint unavailable: criu check: %v", err)
	}
	testCheckpointRoundTrip(t, true)
}

// Exercise actual task deletion and cold creation when a restore has lost its
// images but still has a live task (an interrupted/partially completed restore).
func TestCheckpointColdFallback(t *testing.T) {
	testCheckpointRoundTrip(t, false)
}

func testCheckpointRoundTrip(t *testing.T, native bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	d, cleanup := testutils.NewTestDeps()
	defer cleanup()
	ctx = namespaces.WithNamespace(ctx, d.Namespace)
	image := buildGoOCIImage(t, "./testdata/checkpoint", "/checkpoint", nil)
	defer image.Close()
	importer := d.NewImageImporter()
	require.NoError(t, importer.ImportImage(ctx, image, "checkpoint-test:latest"))
	c, err := newSandboxController(d)
	require.NoError(t, err)
	defer c.Close()
	require.NoError(t, c.Init(ctx))
	app, err := d.EAC.Create(ctx, entity.New(
		(&core_v1alpha.App{}).Encode, (&core_v1alpha.Metadata{Name: "checkpoint-app"}).Encode,
	).Attrs())
	require.NoError(t, err)
	ver := &core_v1alpha.AppVersion{App: entity.Id(app.Id()), ImageUrl: "checkpoint-test:latest", Version: "checkpoint-test",
		Config: core_v1alpha.Config{Services: []core_v1alpha.Services{{Name: "web", Port: 8080}}}}
	version, err := d.EAC.Create(ctx, entity.New(ver.Encode).Attrs())
	require.NoError(t, err)
	ver.ID = entity.Id(version.Id())
	cfg := coreutil.ConfigSpecFromConfig(&ver.Config)
	spec, err := appspec.Build(nil, appspec.Options{AppID: ver.App, AppName: "checkpoint-app", Version: ver,
		Config: &cfg, Service: "web", Image: ver.ImageUrl})
	require.NoError(t, err)
	sb := &compute.Sandbox{Status: compute.PENDING, Spec: *spec}
	// Drive transitions directly; the coordinator must not scale this fixture.
	pool, err := d.EAC.Create(ctx, entity.New((&compute.SandboxPool{
		App: ver.App, Service: "web", SandboxSpec: sb.Spec, ReferencedByVersions: []entity.Id{sb.Spec.Version},
	}).Encode).Attrs())
	require.NoError(t, err)
	resp, err := d.EAC.Create(ctx, entity.New(sb.Encode, (&core_v1alpha.Metadata{
		Labels: types.LabelSet("pool", pool.Id(), "service", "fixture"),
	}).Encode).Attrs())
	require.NoError(t, err)
	sb.ID = entity.Id(resp.Id())
	defer func() { require.NoError(t, c.StopSandbox(ctx, sb.ID, sb)) }()
	sb, meta, err := c.ops.GetSandbox(ctx, sb.ID.String())
	require.NoError(t, err)
	require.NoError(t, c.Create(ctx, sb, meta))
	sb = reloadSandbox(require.New(t), ctx, c, sb.ID)
	require.Equal(t, compute.RUNNING, sb.Status)
	addr, err := netip.ParsePrefix(sb.Network[0].Address)
	require.NoError(t, err)
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	type state struct {
		Identity string
		Count    int
		Offset   int64
		Journal  string
	}
	read := func() state {
		resp, err := client.Get(fmt.Sprintf("http://%s:8080/", addr.Addr()))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var s state
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&s))
		return s
	}
	first := read()
	require.Equal(t, 1, first.Count)
	for cycle := 0; cycle < 2; cycle++ {
		if native {
			_, err = c.ops.PatchSandbox(ctx, entity.New(entity.DBId, sb.ID,
				(&compute.Sandbox{Status: compute.HIBERNATING}).Encode).Attrs(), 0)
			require.NoError(t, err)
			sb, meta, err = c.ops.GetSandbox(ctx, sb.ID.String())
			require.NoError(t, err)
			require.NoError(t, c.Create(ctx, sb, meta))
			sb = reloadSandbox(require.New(t), ctx, c, sb.ID)
			require.Equal(t, compute.HIBERNATED, sb.Status, "cold fallback must not mask a failed checkpoint")
			st, err := os.Stat(c.checkpointPath(sb.ID))
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0700), st.Mode().Perm())
		}
		_, err = c.ops.PatchSandbox(ctx, entity.New(entity.DBId, sb.ID,
			(&compute.Sandbox{Status: compute.RESTORING, RestoredAt: time.Now()}).Encode).Attrs(), 0)
		require.NoError(t, err)
		sb, meta, err = c.ops.GetSandbox(ctx, sb.ID.String())
		require.NoError(t, err)
		require.NoError(t, c.Create(ctx, sb, meta))
		sb = reloadSandbox(require.New(t), ctx, c, sb.ID)
		require.Equal(t, compute.RUNNING, sb.Status)
		require.Len(t, sb.Network, 1, "cold fallback must replace the released network address")
		addr, err = netip.ParsePrefix(sb.Network[0].Address)
		require.NoError(t, err)
		after := read() // No probes are concurrent with runc restore.
		if native {
			require.Equal(t, first.Identity, after.Identity, "a cold restart is not a restore")
			require.Equal(t, cycle+2, after.Count)
			require.EqualValues(t, cycle+2, after.Offset)
			require.Equal(t, []string{"0102", "010203"}[cycle], after.Journal)
		} else {
			require.NotEqual(t, first.Identity, after.Identity)
			require.Equal(t, 1, after.Count)
			require.EqualValues(t, 1, after.Offset)
			require.Equal(t, "01", after.Journal)
			first = after
		}
	}
}
