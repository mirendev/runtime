package usage

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/pkg/addon"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc"
	sandboxserver "miren.dev/runtime/servers/sandbox"
)

type usageAuthorizer struct {
	action string
	admin  bool
	fail   func(rpc.ResourceRequest) error
}

func (usageAuthorizer) Authorize(context.Context, *rpc.Identity, string, string) error {
	return nil
}

func (a usageAuthorizer) AuthorizeResource(_ context.Context, _ *rpc.Identity, r rpc.ResourceRequest) error {
	if a.fail != nil {
		return a.fail(r)
	}
	if a.admin || (r.Resource == "app" && r.Instance == "visible" && r.Action == a.action) {
		return nil
	}
	return rpc.ErrUnauthorized
}

// The REST-only methods have no generated clients. Feed their generated
// adapters JSON arguments, just as the HTTP gateway does.
type usageRESTCall struct {
	rpc.Call
	t      *testing.T
	args   string
	result any
}

func (c *usageRESTCall) Args(v any)    { require.NoError(c.t, json.Unmarshal([]byte(c.args), v)) }
func (c *usageRESTCall) Results(v any) { c.result = v }

func TestUsageVisibilityPublicSurfaces(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	ids := map[string]string{}
	for _, name := range []string{"visible", "hidden"} {
		appID, err := inmem.Client.Create(ctx, name, &core_v1alpha.App{CreatedBy: name})
		require.NoError(t, err)
		version, err := inmem.Client.Create(ctx, name, &core_v1alpha.AppVersion{App: appID})
		require.NoError(t, err)
		id, err := inmem.Client.Create(ctx, name, &compute_v1alpha.Sandbox{
			Status: compute_v1alpha.RUNNING,
			Spec:   compute_v1alpha.SandboxSpec{Version: version, LogAttribute: types.Labels{{Key: "miren.stage", Value: "app-run"}}},
		})
		require.NoError(t, err)
		ids[name] = id.String()
	}
	// A forged app label cannot grant access to an otherwise unowned sandbox.
	id, err := inmem.Client.Create(ctx, "orphan", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Spec:   compute_v1alpha.SandboxSpec{LogAttribute: types.Labels{{Key: "miren.stage", Value: "app-run"}}},
	}, entityserver.WithLabels(types.Labels{{Key: "app", Value: "visible"}}))
	require.NoError(t, err)
	ids["orphan"] = id.String()
	brokenID, err := inmem.Client.Create(ctx, "broken", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Spec:   compute_v1alpha.SandboxSpec{LogAttribute: types.Labels{{Key: "miren.stage", Value: "app-run"}}},
	}, entityserver.WithLabels(types.Labels{{Key: "pool", Value: "sandbox_pool/missing"}, {Key: "app", Value: "visible"}}))
	require.NoError(t, err)

	store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		var rows []metrics.Result
		for _, name := range []string{"visible", "hidden", "historical"} {
			labels := map[string]string{labelApp: name, labelKind: "app"}
			if strings.Contains(query, "by ("+labelSandbox+")") {
				labels = map[string]string{labelSandbox: ids[name]}
			}
			value := "100"
			if name == "visible" {
				value = "2"
			}
			rows = append(rows, metrics.Result{Metric: labels, Value: []any{float64(1), value}})
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(metrics.QueryResult{Status: "success", Data: metrics.Data{ResultType: "vector", Result: rows}}))
	}))
	t.Cleanup(store.Close)
	s := NewServer(slog.Default(), inmem.Client, metrics.NewVictoriaMetricsReader(slog.Default(), store.URL, time.Second))
	iface := usage_v1alpha.AdaptResourceUsage(s)
	client := usage_v1alpha.NewResourceUsageClient(rpc.LocalClient(iface))
	caller := func(action string, admin bool) context.Context {
		return rpc.ContextWithAuthorization(ctx, usageAuthorizer{action: action, admin: admin}, "resourceusage", "originalmethod")
	}
	var ord usage_v1alpha.Ordering
	ord.SetLimit(1)
	listCtx := caller("list", false)
	apps, err := client.ListApps(listCtx, nil, nil, &ord)
	require.NoError(t, err)
	require.EqualValues(t, 1, apps.TotalCount())
	require.Len(t, apps.Apps(), 1)
	require.Equal(t, "visible", apps.Apps()[0].App())
	require.Equal(t, float64(2), apps.Cluster().CpuCores())
	require.EqualValues(t, 2, apps.Cluster().MemoryBytes())
	sandboxes, err := client.ListSandboxes(listCtx, nil, nil, &ord)
	require.NoError(t, err)
	require.EqualValues(t, 1, sandboxes.TotalCount())
	require.Len(t, sandboxes.Sandboxes(), 1)
	require.Equal(t, ids["visible"], sandboxes.Sandboxes()[0].Ref().Sandbox())
	require.Equal(t, float64(2), sandboxes.Cluster().CpuCores())
	_, err = client.GetApp(listCtx, "visible", nil, nil)
	require.Error(t, err, "list must not imply read")
	_, err = client.GetSandbox(listCtx, ids["visible"], nil, nil)
	require.Error(t, err)
	readCtx := caller("read", false)
	_, err = client.GetApp(readCtx, "visible", nil, nil)
	require.NoError(t, err, "direct read must not require list")
	_, err = client.GetSandbox(readCtx, ids["visible"], nil, nil)
	require.NoError(t, err)
	_, err = client.GetApp(readCtx, "hidden", nil, nil)
	require.Error(t, err)
	_, err = client.GetApp(readCtx, "historical", nil, nil)
	require.Error(t, err)
	_, err = client.GetSandbox(readCtx, ids["orphan"], nil, nil)
	require.Error(t, err)
	_, err = client.GetSandbox(readCtx, brokenID.String(), nil, nil)
	require.Error(t, err)
	empty, err := client.ListApps(readCtx, nil, nil, nil)
	require.NoError(t, err)
	require.Zero(t, empty.TotalCount())
	admin, err := client.ListApps(caller("", true), nil, nil, &ord)
	require.NoError(t, err)
	require.EqualValues(t, 3, admin.TotalCount())
	require.Len(t, admin.Apps(), 1)
	require.Equal(t, float64(202), admin.Cluster().CpuCores())
	_, err = client.GetApp(caller("", true), "historical", nil, nil)
	require.NoError(t, err)
	_, err = client.GetSandbox(caller("", true), brokenID.String(), nil, nil)
	require.NoError(t, err)
	_, err = client.GetSandbox(ctx, brokenID.String(), nil, nil)
	require.NoError(t, err, "trusted unconfigured calls retain recovery access")

	for _, phase := range []string{"preflight", "row", "detail"} {
		t.Run("global failure "+phase, func(t *testing.T) {
			failure := errors.New("authorization service unavailable")
			failureCtx := rpc.ContextWithAuthorization(ctx, usageAuthorizer{fail: func(r rpc.ResourceRequest) error {
				if phase == "preflight" || r.Instance != "" || r.Resource == "cluster" {
					return failure
				}
				return nil
			}}, "resourceusage", "originalmethod")
			if phase == "detail" {
				_, err := client.GetApp(failureCtx, "visible", nil, nil)
				require.ErrorContains(t, err, failure.Error())
				_, err = client.GetSandbox(failureCtx, ids["visible"], nil, nil)
				require.ErrorContains(t, err, failure.Error())
			} else {
				_, err := client.ListApps(failureCtx, nil, nil, nil)
				require.ErrorContains(t, err, failure.Error())
				_, err = client.ListSandboxes(failureCtx, nil, nil, nil)
				require.ErrorContains(t, err, failure.Error())
				if phase == "preflight" {
					var selector usage_v1alpha.Selector
					selector.SetNode("missing-node")
					_, err = client.ListApps(failureCtx, &selector, nil, nil)
					require.ErrorContains(t, err, failure.Error())
					_, err = client.ListSandboxes(failureCtx, &selector, nil, nil)
					require.ErrorContains(t, err, failure.Error())
				}
			}
		})
	}

	for _, method := range iface.Methods() {
		var args string
		switch method.Name {
		case "httpListApps", "httpListSandboxes":
			args = `{"limit":1}`
		case "httpGetApp":
			args = `{"app":"visible"}`
		case "httpGetSandbox":
			args = `{"sandbox":"` + ids["visible"] + `"}`
		default:
			continue
		}
		t.Run(method.Name, func(t *testing.T) {
			call := &usageRESTCall{t: t, args: args}
			if strings.Contains(method.Name, "List") {
				require.NoError(t, method.Handler(listCtx, call))
				data, err := json.Marshal(call.result)
				require.NoError(t, err)
				require.NotContains(t, string(data), "hidden")
				require.NotContains(t, string(data), `"app":"historical"`)
				var result struct {
					TotalCount int `json:"total_count"`
					Cluster    struct {
						CPU float64 `json:"cpu_cores"`
					} `json:"cluster"`
				}
				require.NoError(t, json.Unmarshal(data, &result))
				require.Equal(t, 1, result.TotalCount)
				require.Equal(t, float64(2), result.Cluster.CPU)
			} else {
				require.Error(t, method.Handler(listCtx, call))
				require.NoError(t, method.Handler(readCtx, &usageRESTCall{t: t, args: args}))
			}
		})
	}
}

func TestSandboxUsageUsesVerifiedPoolOwner(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	visible, err := inmem.Client.Create(ctx, "visible", &core_v1alpha.App{CreatedBy: "owner"})
	require.NoError(t, err)
	hidden, err := inmem.Client.Create(ctx, "hidden", &core_v1alpha.App{})
	require.NoError(t, err)
	version, err := inmem.Client.Create(ctx, "version", &core_v1alpha.AppVersion{App: visible})
	require.NoError(t, err)
	pool, err := inmem.Client.Create(ctx, "pool", &compute_v1alpha.SandboxPool{App: hidden})
	require.NoError(t, err)
	s := NewServer(slog.Default(), inmem.Client, nil)
	client := usage_v1alpha.NewResourceUsageClient(rpc.LocalClient(usage_v1alpha.AdaptResourceUsage(s)))
	readCtx := rpc.ContextWithAuthorization(ctx, usageAuthorizer{action: "read"}, "resourceusage", "getSandbox")
	for i, poolID := range []string{pool.String(), "sandbox_pool/missing"} {
		id, err := inmem.Client.Create(ctx, []string{"pooled", "broken"}[i], &compute_v1alpha.Sandbox{
			Status: compute_v1alpha.RUNNING,
			Spec:   compute_v1alpha.SandboxSpec{Version: version},
		}, entityserver.WithLabels(types.Labels{{Key: "pool", Value: poolID}, {Key: "app", Value: "visible"}}))
		require.NoError(t, err)
		_, err = client.GetSandbox(readCtx, id.String(), nil, nil)
		require.Error(t, err, "neither a version nor a label may override a pool")
		detail, err := client.GetSandbox(ctx, id.String(), nil, nil)
		require.NoError(t, err)
		if poolID == pool.String() {
			require.Equal(t, "hidden", detail.Usage().Ref().App())
		} else {
			require.Equal(t, "visible", detail.Usage().Ref().App(), "unverified labels remain display-only")
			require.Empty(t, detail.Usage().Ref().AppId())
		}
	}
}

func TestLegacyAddonDisplayDoesNotGrantAccess(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	_, err := inmem.Client.Create(ctx, "visible", &core_v1alpha.App{CreatedBy: "alice"})
	require.NoError(t, err)
	labels := types.LabelSet("addon", "postgresql", "app", "visible", "server", "database")
	fw := addon.NewProviderFramework(slog.Default(), inmem.Client, inmem.EAC, nil)
	poolID, err := fw.CreateSandboxPool(ctx, addon.CreateSandboxPoolSpec{Name: "database", Image: "postgres:17", Labels: labels})
	require.NoError(t, err)
	var pool compute_v1alpha.SandboxPool
	require.NoError(t, inmem.Client.GetById(ctx, poolID, &pool))
	require.Empty(t, pool.App, "exercise the legacy addon ownership shape")
	id, err := inmem.Client.Create(ctx, "database", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING, Spec: pool.SandboxSpec,
	}, entityserver.WithLabels(append(labels, types.Label{Key: "pool", Value: poolID.String()})))
	require.NoError(t, err)
	usage := usage_v1alpha.NewResourceUsageClient(rpc.LocalClient(usage_v1alpha.AdaptResourceUsage(NewServer(slog.Default(), inmem.Client, nil))))
	var selector usage_v1alpha.Selector
	selector.SetApp("visible")
	selector.SetKind("addon")
	operator := rpc.ContextWithAuthorization(ctx, usageAuthorizer{admin: true}, "resourceusage", "listsandboxes")
	listed, err := usage.ListSandboxes(operator, &selector, nil, nil)
	require.NoError(t, err)
	require.Len(t, listed.Sandboxes(), 1, "operator app/kind filtering must retain the database")
	require.Equal(t, "visible", listed.Sandboxes()[0].Ref().App())
	require.Empty(t, listed.Sandboxes()[0].Ref().AppId())
	owner := rpc.ContextWithAuthorization(ctx, usageAuthorizer{action: "list"}, "resourceusage", "listsandboxes")
	listed, err = usage.ListSandboxes(owner, &selector, nil, nil)
	require.NoError(t, err)
	require.Empty(t, listed.Sandboxes(), "display labels must not confer list access")
	owner = rpc.ContextWithAuthorization(ctx, usageAuthorizer{action: "read"}, "resourceusage", "getsandbox")
	_, err = usage.GetSandbox(owner, id.String(), nil, nil)
	require.Error(t, err, "display labels must not confer read access")
	sandboxes := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(sandboxserver.NewServer(slog.Default(), inmem.Client))))
	inventory, err := sandboxes.List(operator)
	require.NoError(t, err)
	require.Len(t, inventory.Sandboxes(), 1)
	require.Equal(t, "visible", inventory.Sandboxes()[0].App())
	owner = rpc.ContextWithAuthorization(ctx, usageAuthorizer{action: "list"}, "sandboxes", "list")
	inventory, err = sandboxes.List(owner)
	require.NoError(t, err)
	require.Empty(t, inventory.Sandboxes())
}
