package sandbox

import (
	"context"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	core_v1alpha "miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/network/network_v1alpha"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
)

// sandboxForUpdateServices builds a controller with only the fields
// UpdateServices touches, so no live containerd client is needed.
func sandboxForUpdateServices(t *testing.T) (*SandboxController, *testutils.InMemEntityServer) {
	t.Helper()
	es, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctrl := &SandboxController{Log: testutils.TestLogger(t), EAC: es.EAC}
	return ctrl, es
}

// listEndpoints returns every Endpoints entity in the store, decoded.
func listEndpoints(t *testing.T, ctx context.Context, es *testutils.InMemEntityServer) []*network_v1alpha.Endpoints {
	t.Helper()
	resp, err := es.EAC.List(ctx, entity.Ref(entity.EntityKind, network_v1alpha.KindEndpoints))
	require.NoError(t, err)
	var out []*network_v1alpha.Endpoints
	for _, ent := range resp.Values() {
		var eps network_v1alpha.Endpoints
		eps.Decode(ent.Entity())
		out = append(out, &eps)
	}
	return out
}

// A resumed saga re-runs actionUpdateSvcs against surviving containers.
// addEndpoint mints a fresh entity ID per Create, so without the dedup the
// re-run would duplicate every (service, port) and double-DNAT it.
func TestUpdateServices_NoDuplicateEndpointsOnResume(t *testing.T) {
	ctx := context.Background()
	ctrl, es := sandboxForUpdateServices(t)

	svcID := entity.Id("service/test")
	const sbIP = "10.0.0.5"

	// A service matching the sandbox's metadata labels and exposing two ports.
	svc := &network_v1alpha.Service{
		ID:    svcID,
		Match: types.Labels{types.Label{Key: "app", Value: "test-app"}},
		Port: []network_v1alpha.Port{
			{Name: "web", Port: 8080},
			{Name: "admin", Port: 9090},
		},
	}
	_, err := es.EAC.Create(ctx, entity.New(
		entity.Ref(entity.DBId, svc.ID),
		svc.Encode,
	).Attrs())
	require.NoError(t, err)

	co := &compute.Sandbox{
		ID: entity.Id("sandbox/test-sb-1"),
		Spec: compute.SandboxSpec{
			Container: []compute.SandboxSpecContainer{
				{Name: "web", Port: []compute.SandboxSpecContainerPort{
					{Port: 8080},
					{Port: 9090},
				}},
			},
		},
	}
	meta := &entity.Meta{Entity: entity.New(
		entity.Ref(entity.DBId, co.ID),
		entity.Label(core_v1alpha.MetadataLabelsId, "app", "test-app"),
	)}
	ep := &network.EndpointConfig{
		Addresses: []netip.Prefix{netip.MustParsePrefix(sbIP + "/32")},
	}

	require.NoError(t, ctrl.UpdateServices(ctx, co, meta, ep))
	first := listEndpoints(t, ctx, es)
	require.Len(t, first, 2, "one Endpoints entity per matched (service, port) on first run")

	ports := map[int64]bool{}
	for _, e := range first {
		assert.Equal(t, svcID, e.Service, "endpoint must reference the service")
		require.Len(t, e.Endpoint, 1)
		assert.Equal(t, sbIP, e.Endpoint[0].Ip, "endpoint must carry the sandbox's IP")
		ports[e.Endpoint[0].Port] = true
	}
	assert.True(t, ports[8080] && ports[9090], "both ports must be registered once")

	// The regression guard: a resumed actionUpdateSvcs re-running.
	require.NoError(t, ctrl.UpdateServices(ctx, co, meta, ep))
	second := listEndpoints(t, ctx, es)
	require.Len(t, second, 2, "re-running UpdateServices must not create duplicate Endpoints")
	for _, e := range second {
		assert.Equal(t, svcID, e.Service)
		require.Len(t, e.Endpoint, 1)
		assert.Equal(t, sbIP, e.Endpoint[0].Ip)
	}
}

// Guards against an over-broad dedup: keying on the sandbox's own IP means a
// second sandbox still registers rather than being skipped and left unroutable.
func TestUpdateServices_DedupIsPerSandboxIP(t *testing.T) {
	ctx := context.Background()
	ctrl, es := sandboxForUpdateServices(t)

	svcID := entity.Id("service/shared")
	svc := &network_v1alpha.Service{
		ID:    svcID,
		Match: types.Labels{types.Label{Key: "app", Value: "test-app"}},
		Port:  []network_v1alpha.Port{{Name: "web", Port: 8080}},
	}
	_, err := es.EAC.Create(ctx, entity.New(entity.Ref(entity.DBId, svc.ID), svc.Encode).Attrs())
	require.NoError(t, err)

	mkSandbox := func(id, ip string) (*compute.Sandbox, *entity.Meta, *network.EndpointConfig) {
		co := &compute.Sandbox{
			ID: entity.Id(id),
			Spec: compute.SandboxSpec{Container: []compute.SandboxSpecContainer{
				{Name: "web", Port: []compute.SandboxSpecContainerPort{{Port: 8080}}},
			}},
		}
		meta := &entity.Meta{Entity: entity.New(
			entity.Ref(entity.DBId, co.ID),
			entity.Label(core_v1alpha.MetadataLabelsId, "app", "test-app"),
		)}
		ep := &network.EndpointConfig{Addresses: []netip.Prefix{netip.MustParsePrefix(ip + "/32")}}
		return co, meta, ep
	}

	sb1, m1, ep1 := mkSandbox("sandbox/a", "10.0.0.5")
	require.NoError(t, ctrl.UpdateServices(ctx, sb1, m1, ep1))
	require.Len(t, listEndpoints(t, ctx, es), 1, "first sandbox registers its endpoint")

	require.NoError(t, ctrl.UpdateServices(ctx, sb1, m1, ep1))
	require.Len(t, listEndpoints(t, ctx, es), 1, "re-running sb1 must not duplicate")

	sb2, m2, ep2 := mkSandbox("sandbox/b", "10.0.0.6")
	require.NoError(t, ctrl.UpdateServices(ctx, sb2, m2, ep2))
	all := listEndpoints(t, ctx, es)
	require.Len(t, all, 2, "a second sandbox on a different IP must still register its own endpoint")
	ips := map[string]bool{}
	for _, e := range all {
		require.Len(t, e.Endpoint, 1)
		ips[e.Endpoint[0].Ip] = true
	}
	assert.True(t, ips["10.0.0.5"] && ips["10.0.0.6"], "each sandbox's IP must be present exactly once")
}
