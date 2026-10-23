package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/ui"
)

func TestSandboxFilterDescribe(t *testing.T) {
	require.Empty(t, sandboxFilter{}.describe())
	require.Equal(t, ` for app "myapp"`, sandboxFilter{app: "myapp"}.describe())
	require.Equal(t, ` for service "web"`, sandboxFilter{service: "web"}.describe())
	require.Equal(t,
		` for app "myapp", service "web"`,
		sandboxFilter{app: "myapp", service: "web"}.describe())
}

func TestSandboxFilterApply(t *testing.T) {
	entries := []sandboxListEntry{
		{ID: "running", App: "shop", Service: "web", Status: "running"},
		{ID: "stopped", App: "shop", Service: "worker", Status: "stopped"},
		{ID: "dead", App: "other", Service: "web", Status: "dead"},
	}

	tests := []struct {
		name       string
		filter     sandboxFilter
		wantIDs    []string
		wantHidden int64
	}{
		{name: "default hides terminal sandboxes", wantIDs: []string{"running"}, wantHidden: 2},
		{name: "all includes terminal sandboxes", filter: sandboxFilter{includeDead: true}, wantIDs: []string{"running", "stopped", "dead"}},
		{name: "explicit status overrides default", filter: sandboxFilter{status: "dead"}, wantIDs: []string{"dead"}},
		{name: "app filtering precedes hidden count", filter: sandboxFilter{app: "shop"}, wantIDs: []string{"running"}, wantHidden: 1},
		{name: "service filter", filter: sandboxFilter{includeDead: true, service: "worker"}, wantIDs: []string{"stopped"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, hidden := test.filter.apply(entries)
			ids := make([]string, 0, len(got))
			for _, entry := range got {
				ids = append(ids, entry.ID)
			}
			require.Equal(t, test.wantIDs, ids)
			require.Equal(t, test.wantHidden, hidden)
		})
	}
}

func TestSandboxListEntryJSONIsInventoryOnly(t *testing.T) {
	entry := newSandboxListEntry(
		"sandbox/shop-web-abc", "sb-abc", "shop", "app_version/shop-v1", "v1",
		"web", "pool/shop-web", "sp-web", "10.0.0.4", "runner-1", "status.running",
		1_725_000_000_000, 1_725_000_060_000,
	)

	var out bytes.Buffer
	require.NoError(t, PrintJSONTo(&out, []sandboxListEntry{entry}))
	assert.JSONEq(t, `[
		{
			"id": "sandbox/shop-web-abc",
			"app": "shop",
			"version": "app_version/shop-v1",
			"service": "web",
			"pool": "pool/shop-web",
			"address": "10.0.0.4",
			"runner": "runner-1",
			"status": "running",
			"created_at": "2024-08-30T06:40:00Z",
			"updated_at": "2024-08-30T06:41:00Z"
		}
	]`, out.String())
	assert.NotContains(t, out.String(), "spec")
	assert.NotContains(t, out.String(), "container")
	assert.NotContains(t, out.String(), "env")
	assert.NotContains(t, out.String(), "short_id")
}

func TestSandboxListEmptyJSONIsArray(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, PrintJSONTo(&out, make([]sandboxListEntry, 0)))
	assert.JSONEq(t, `[]`, out.String())
}

func TestLegacySandboxListDropsExecutionSpec(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	r := require.New(t)
	// The mock schema loader stores these aliases as untyped strings, while the
	// production store validates them into keywords. Seed the validated form so
	// this test drives LookupKind exactly as an old server does.
	_, err := inmem.Store.UpdateEntity(ctx, compute_v1alpha.Schema, entity.New(
		entity.Keyword(entity.SchemaKind, "sandbox"),
		entity.Keyword(entity.SchemaKind, "sandbox_pool"),
		entity.Keyword(entity.SchemaKind, "node"),
	))
	r.NoError(err)
	_, err = inmem.Store.UpdateEntity(ctx, core_v1alpha.Schema, entity.New(
		entity.Keyword(entity.SchemaKind, "app_version"),
	))
	r.NoError(err)

	versionID, err := inmem.Client.Create(ctx, "shop-v1", &core_v1alpha.AppVersion{
		App: entity.Id("app/shop"),
	})
	r.NoError(err)
	poolID, err := inmem.Client.Create(ctx, "shop-web", &compute_v1alpha.SandboxPool{Service: "web"})
	r.NoError(err)

	const canary = "mir1765-legacy-fallback-must-not-return-this"
	_, err = inmem.Client.Create(ctx, "shop-web-running", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING,
		Spec: compute_v1alpha.SandboxSpec{
			Version: versionID,
			Container: []compute_v1alpha.SandboxSpecContainer{{
				Name: "app",
				Env:  []string{"DATABASE_URL=" + canary},
			}},
		},
	}, entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
	r.NoError(err)
	_, err = inmem.Client.Create(ctx, "shop-web-dead", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.DEAD,
		Spec: compute_v1alpha.SandboxSpec{
			Version: versionID,
			Container: []compute_v1alpha.SandboxSpecContainer{{
				Name: "app",
				Env:  []string{"DATABASE_URL=" + canary},
			}},
		},
	}, entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
	r.NoError(err)

	entries, err := listSandboxesFromEntities(ctx, inmem.EAC)
	r.NoError(err)
	r.Len(entries, 2)
	visible, hidden := (sandboxFilter{}).apply(entries)
	r.Len(visible, 1)
	r.Equal(int64(1), hidden)
	r.Equal("running", visible[0].Status)
	r.Equal("shop", visible[0].App)
	r.Equal("web", visible[0].Service)

	wire, err := json.Marshal(entries)
	r.NoError(err)
	assert.NotContains(t, string(wire), canary)
	assert.NotContains(t, string(wire), `"spec"`)
	assert.NotContains(t, string(wire), `"env"`)
}

func TestServerPredatesSandboxInventoryOnlyForLookupFailures(t *testing.T) {
	const capability = "dev.miren.runtime/sandboxes"
	const remote = "prod:8443"

	lookup := rpc.NewResolveLookupError(capability, remote, "unknown object: "+capability)
	assert.True(t, serverPredatesSandboxInventory(lookup))
	assert.True(t, serverPredatesSandboxInventory(&ui.Diagnostic{Summary: "old cluster", Cause: lookup}))

	notOldServer := []error{
		rpc.NewResolveUnreachableError(capability, remote, 5*time.Second, errors.New("dial")),
		rpc.NewResolveWentSilentError(capability, remote, 30*time.Second, errors.New("reset")),
		rpc.NewResolveNoAnswerError(capability, remote, 8*time.Second, errors.New("timeout")),
		rpc.NewResolveStatusError(capability, remote, 401),
		errors.New("boom"),
	}
	for _, err := range notOldServer {
		assert.False(t, serverPredatesSandboxInventory(err), "%v must not activate the raw fallback", err)
	}
}
