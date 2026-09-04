package sandbox

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc"
)

func TestListReturnsCompleteSafeInventory(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	r := require.New(t)

	appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{})
	r.NoError(err)
	versionID, err := inmem.Client.Create(ctx, "shop-v1", &core_v1alpha.AppVersion{
		App:     appID,
		Version: "shop-v1",
	})
	r.NoError(err)
	poolID, err := inmem.Client.Create(ctx, "shop-web", &compute_v1alpha.SandboxPool{
		App:         appID,
		Service:     "web",
		SandboxSpec: compute_v1alpha.SandboxSpec{Version: versionID},
	})
	r.NoError(err)
	nodeID, err := inmem.Client.Create(ctx, "runner-one", &compute_v1alpha.Node{Name: "runner-one"})
	r.NoError(err)

	const secretCanary = "mir1765-server-must-not-return-this"
	runningID, err := inmem.Client.Create(ctx, "shop-web-running", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Spec: compute_v1alpha.SandboxSpec{
			Version: versionID,
			Container: []compute_v1alpha.SandboxSpecContainer{{
				Name:  "app",
				Image: "example.test/shop@sha256:abc",
				Env:   []string{"DATABASE_URL=" + secretCanary},
			}},
		},
		Network: []compute_v1alpha.Network{{Address: "10.0.0.4"}},
	}, entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
	r.NoError(err)
	r.NoError(inmem.Client.UpdateAttrs(ctx, runningID,
		(&compute_v1alpha.Schedule{Key: compute_v1alpha.Key{Node: nodeID}}).Encode()))

	deadID, err := inmem.Client.Create(ctx, "shop-web-dead", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.DEAD,
		Spec:   compute_v1alpha.SandboxSpec{Version: versionID},
	}, entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
	r.NoError(err)

	server := NewServer(testutils.TestLogger(t), inmem.Client)
	client := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(server)))

	result, err := client.List(ctx)
	r.NoError(err)
	r.Len(result.Sandboxes(), 2)

	byID := make(map[string]*compute_v1alpha.SandboxInfo)
	for _, sandbox := range result.Sandboxes() {
		byID[sandbox.Id()] = sandbox
	}
	got := byID[runningID.String()]
	r.NotNil(got)
	r.Equal(runningID.String(), got.Id())
	r.Equal("shop", got.App())
	r.Equal(versionID.String(), got.Version())
	r.Equal("web", got.Service())
	r.Equal(poolID.String(), got.Pool())
	r.Equal("10.0.0.4", got.Address())
	r.Equal("runner-one", got.Runner())
	r.Equal("running", got.Status())
	r.NotZero(got.CreatedAt())
	r.NotZero(got.UpdatedAt())
	dead := byID[deadID.String()]
	r.NotNil(dead)
	r.Equal("dead", dead.Status())

	wire, err := json.Marshal(result.Sandboxes())
	r.NoError(err)
	assert.NotContains(t, string(wire), secretCanary)
	assert.NotContains(t, string(wire), `"spec"`)
	assert.NotContains(t, string(wire), `"env"`)
}
