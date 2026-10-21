package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
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

func TestSandboxLifecycle(t *testing.T) {
	for _, owner := range []string{"pool", "pool-other-version", "version", "unowned", "missing-pool", "missing-version", "missing-app-pool", "missing-app-version"} {
		for _, status := range []compute_v1alpha.SandboxStatus{
			compute_v1alpha.PENDING, compute_v1alpha.RUNNING, compute_v1alpha.STOPPED, compute_v1alpha.DEAD,
		} {
			t.Run(owner+"/"+string(status), func(t *testing.T) {
				ctx := context.Background()
				inmem, cleanup := testutils.NewInMemEntityServer(t)
				t.Cleanup(cleanup)
				appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{})
				require.NoError(t, err)
				versionID, err := inmem.Client.Create(ctx, "shop-v1", &core_v1alpha.AppVersion{App: appID})
				require.NoError(t, err)
				poolID, err := inmem.Client.Create(ctx, "shop-web", &compute_v1alpha.SandboxPool{App: appID})
				require.NoError(t, err)
				sb := &compute_v1alpha.Sandbox{Status: status, Spec: compute_v1alpha.SandboxSpec{
					Container: []compute_v1alpha.SandboxSpecContainer{{Env: []string{"SECRET=retained"}}},
				}}
				var opts []entityserver.CreateOptions
				switch owner {
				case "pool":
					// No version: pool ownership alone must suffice.
					opts = append(opts, entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
				case "pool-other-version":
					otherApp, err := inmem.Client.Create(ctx, "other", &core_v1alpha.App{})
					require.NoError(t, err)
					sb.Spec.Version, err = inmem.Client.Create(ctx, "other-v1", &core_v1alpha.AppVersion{App: otherApp})
					require.NoError(t, err)
					opts = append(opts, entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
				case "version":
					sb.Spec.Version = versionID
				case "missing-pool":
					sb.Spec.Version = versionID
					opts = append(opts, entityserver.WithLabels(types.LabelSet("pool", "sandbox_pool/missing")))
				case "missing-version":
					sb.Spec.Version = versionID
					require.NoError(t, inmem.Client.Delete(ctx, versionID))
				case "missing-app-pool":
					opts = append(opts, entityserver.WithLabels(types.LabelSet("pool", poolID.String())))
					require.NoError(t, inmem.Client.Delete(ctx, appID))
				case "missing-app-version":
					sb.Spec.Version = versionID
					require.NoError(t, inmem.Client.Delete(ctx, appID))
				}
				sbID, err := inmem.Client.Create(ctx, "target", sb, opts...)
				require.NoError(t, err)
				client := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(
					NewServer(testutils.TestLogger(t), inmem.Client))))
				for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
					other := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: method,
						Metadata: map[string]any{"app": "other", "bound_app": "other"}})
					_, err = client.Stop(other, sbID.String())
					require.EqualError(t, err, "unauthorized")
					_, err = client.Delete(other, sbID.String())
					require.EqualError(t, err, "unauthorized")
				}
				require.Equal(t, status, func() compute_v1alpha.SandboxStatus {
					var got compute_v1alpha.Sandbox
					require.NoError(t, inmem.Client.GetById(ctx, sbID, &got))
					return got.Status
				}())
				own := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": "shop"}})
				switch owner {
				case "unowned", "missing-pool", "missing-version", "missing-app-pool", "missing-app-version":
					for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
						bound := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: method,
							Metadata: map[string]any{"app": "shop", "bound_app": "shop"}})
						_, err = client.Stop(bound, "target")
						require.EqualError(t, err, "unauthorized")
						_, err = client.Delete(bound, sbID.String())
						require.EqualError(t, err, "unauthorized")
					}
					// Operator/cluster callers may recover unowned or orphaned sandboxes.
					own = rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "operator"})
				}
				if status == compute_v1alpha.DEAD {
					_, err = client.Stop(own, sbID.String())
					require.NoError(t, err)
					var got compute_v1alpha.Sandbox
					require.NoError(t, inmem.Client.GetById(ctx, sbID, &got))
					require.Equal(t, compute_v1alpha.DEAD, got.Status)
				}
				_, err = client.Delete(own, "target")
				if status == compute_v1alpha.DEAD {
					require.NoError(t, err)
					require.Error(t, inmem.Client.GetById(ctx, sbID, &compute_v1alpha.Sandbox{}))
					return
				}
				require.ErrorContains(t, err, "must be dead")
				_, err = client.Stop(own, "target")
				require.NoError(t, err)
				_, err = client.Stop(own, sbID.String())
				require.NoError(t, err)
				var got compute_v1alpha.Sandbox
				require.NoError(t, inmem.Client.GetById(ctx, sbID, &got))
				require.Equal(t, compute_v1alpha.STOPPED, got.Status)
				require.Equal(t, sb.Spec, got.Spec)
				_, err = client.Delete(own, sbID.String())
				require.ErrorContains(t, err, "must be dead")
			})
		}
	}
}

type lifecycleConcurrentUpdate struct {
	rpc.Client
	update func()
}

func (c *lifecycleConcurrentUpdate) Call(ctx context.Context, method string, args, result any) error {
	switch method {
	case "put", "patch", "delete", "deleteAtRevision":
		if c.update != nil {
			update := c.update
			c.update = nil
			update()
		}
	}
	return c.Client.Call(ctx, method, args, result)
}

func TestSandboxLifecycleRejectsConcurrentStatusChange(t *testing.T) {
	for _, operation := range []string{"stop", "delete"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			inmem, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			initial, concurrent := compute_v1alpha.RUNNING, compute_v1alpha.DEAD
			if operation == "delete" {
				initial, concurrent = compute_v1alpha.DEAD, compute_v1alpha.RUNNING
			}
			id, err := inmem.Client.Create(ctx, "target", &compute_v1alpha.Sandbox{Status: initial})
			require.NoError(t, err)
			transport := &lifecycleConcurrentUpdate{Client: inmem.EAC.Client, update: func() {
				require.NoError(t, inmem.Client.UpdateAttrs(ctx, id, (&compute_v1alpha.Sandbox{Status: concurrent}).Encode()))
			}}
			ec := entityserver.NewClient(testutils.TestLogger(t), entityserver_v1alpha.NewEntityAccessClient(transport))
			client := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(NewServer(testutils.TestLogger(t), ec))))
			if operation == "stop" {
				_, err = client.Stop(ctx, id.String())
			} else {
				_, err = client.Delete(ctx, id.String())
			}
			require.ErrorContains(t, err, "conflict")
			var current compute_v1alpha.Sandbox
			require.NoError(t, inmem.Client.GetById(ctx, id, &current))
			require.Equal(t, concurrent, current.Status)
		})
	}
}

func TestSandboxLifecycleRejectsMissingOwnerName(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	appID := entity.Id("app/nameless")
	inmem.AddEntity(entity.New(entity.DBId, appID, (&core_v1alpha.App{}).Encode))
	versionID, err := inmem.Client.Create(ctx, "nameless-v1", &core_v1alpha.AppVersion{App: appID})
	require.NoError(t, err)
	id, err := inmem.Client.Create(ctx, "shop", &compute_v1alpha.Sandbox{
		Status: compute_v1alpha.RUNNING, Spec: compute_v1alpha.SandboxSpec{Version: versionID},
	})
	require.NoError(t, err)
	client := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(NewServer(testutils.TestLogger(t), inmem.Client))))
	own := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": "shop"}})
	_, err = client.Stop(own, id.String())
	require.ErrorIs(t, err, rpc.ErrUnauthorized)
	var current compute_v1alpha.Sandbox
	require.NoError(t, inmem.Client.GetById(ctx, id, &current))
	require.Equal(t, compute_v1alpha.RUNNING, current.Status)
}

func TestLifecycleTargetResolution(t *testing.T) {
	ctx := context.Background()
	inmem, cleanup := testutils.NewEtcdEntityServer(t)
	t.Cleanup(cleanup)
	appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	client := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(NewServer(testutils.TestLogger(t), inmem.Client))))
	for _, target := range []string{"missing", appID.String()} {
		_, err := client.Stop(ctx, target)
		if target == "missing" {
			require.ErrorContains(t, err, "not found")
		} else {
			require.ErrorContains(t, err, "not a sandbox")
		}
		_, err = client.Delete(ctx, target)
		require.Error(t, err)
		for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
			bound := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: method,
				Metadata: map[string]any{"app": "shop", "bound_app": "shop"}})
			_, err := client.Stop(bound, target)
			require.EqualError(t, err, "unauthorized")
			_, err = client.Delete(bound, target)
			require.EqualError(t, err, "unauthorized")
		}
	}
	var app core_v1alpha.App
	require.NoError(t, inmem.Client.GetById(ctx, appID, &app))
	// Short IDs are resolved by the production etcd-backed entity server, not
	// by the in-memory test store used for the lifecycle matrix above.
	sbID, err := inmem.Client.Create(ctx, "operator-target", &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING})
	require.NoError(t, err)
	var sb compute_v1alpha.Sandbox
	ent, err := inmem.Client.GetByIdWithEntity(ctx, sbID, &sb)
	require.NoError(t, err)
	shortID := ent.Entity().ShortId()
	require.NotEmpty(t, shortID)
	_, err = client.Stop(ctx, shortID)
	require.NoError(t, err)
	require.NoError(t, inmem.Client.GetById(ctx, sbID, &sb))
	require.Equal(t, compute_v1alpha.STOPPED, sb.Status)
	require.NoError(t, inmem.Client.UpdateAttrs(ctx, sbID, entity.Ref(compute_v1alpha.SandboxStatusId, compute_v1alpha.SandboxStatusDeadId)))
	_, err = client.Delete(ctx, "sandbox/"+shortID)
	require.NoError(t, err)
	require.Error(t, inmem.Client.GetById(ctx, sbID, &sb))
}

func TestSelectForApp(t *testing.T) {
	for _, tc := range []struct {
		name, service, web, worker string
		noWeb, noActive            bool
		wantService, wantVersion   string
		wantCount                  int32
		wantFailure                string
	}{
		{name: "old web beats active worker", web: "old", worker: "active", wantService: "web", wantVersion: "old", wantCount: 2},
		{name: "active preferred", web: "both", worker: "active", wantService: "web", wantVersion: "active", wantCount: 1},
		{name: "explicit worker", service: "worker", web: "active", worker: "old", wantService: "worker", wantVersion: "old", wantCount: 1},
		{name: "missing web never substitutes worker", worker: "active", wantFailure: "missing_default"},
		{name: "explicit web never substitutes worker", service: "web", worker: "active", wantFailure: "no_running"},
		{name: "unknown service", service: "typo", web: "active", worker: "active", wantFailure: "unknown_service"},
		{name: "no web service", noWeb: true, worker: "old", wantService: "worker", wantVersion: "old", wantCount: 1},
		{name: "no active version", noActive: true, web: "old", wantService: "web", wantVersion: "old", wantCount: 2},
		{name: "no eligible sandboxes", noWeb: true, wantFailure: "no_running"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			inmem, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			appID, err := inmem.Client.Create(ctx, "shop", &core_v1alpha.App{})
			require.NoError(t, err)
			versions := map[string]entity.Id{}
			for _, name := range []string{"old", "active"} {
				versions[name], err = inmem.Client.Create(ctx, name, &core_v1alpha.AppVersion{App: appID})
				require.NoError(t, err)
			}
			if !tc.noActive {
				require.NoError(t, inmem.Client.UpdateAttrs(ctx, appID, entity.Ref(core_v1alpha.AppActiveVersionId, versions["active"])))
			}
			otherApp, err := inmem.Client.Create(ctx, "other", &core_v1alpha.App{})
			require.NoError(t, err)
			otherPool, err := inmem.Client.Create(ctx, "other-web", &compute_v1alpha.SandboxPool{App: otherApp, Service: "web"})
			require.NoError(t, err)
			create := func(name string, pool, version entity.Id, status compute_v1alpha.SandboxStatus, scheduled bool) entity.Id {
				id, err := inmem.Client.Create(ctx, name, &compute_v1alpha.Sandbox{Status: status,
					Spec: compute_v1alpha.SandboxSpec{Version: version, Container: []compute_v1alpha.SandboxSpecContainer{{Env: []string{"SECRET=not-on-wire"}}}},
				}, entityserver.WithLabels(types.LabelSet("pool", pool.String())))
				require.NoError(t, err)
				if scheduled {
					require.NoError(t, inmem.Client.UpdateAttrs(ctx, id, (&compute_v1alpha.Schedule{Key: compute_v1alpha.Key{Node: "node/one"}}).Encode()))
				}
				return id
			}
			// Neither an ephemeral sandbox with our version nor another app's
			// pool with our version is eligible: pool ownership is authoritative.
			create("ephemeral", "", versions["active"], compute_v1alpha.RUNNING, true)
			create("other", otherPool, versions["active"], compute_v1alpha.RUNNING, true)
			eligible := map[string]bool{}
			for service, mode := range map[string]string{"web": tc.web, "worker": tc.worker} {
				if service == "web" && tc.noWeb {
					continue
				}
				pool, err := inmem.Client.Create(ctx, service, &compute_v1alpha.SandboxPool{App: appID, Service: service})
				require.NoError(t, err)
				create(service+"-unscheduled", pool, versions["active"], compute_v1alpha.RUNNING, false)
				create(service+"-pending", pool, versions["active"], compute_v1alpha.PENDING, true)
				create(service+"-stopped", pool, versions["active"], compute_v1alpha.STOPPED, true)
				for _, version := range []string{"old", "active"} {
					if mode != version && mode != "both" {
						continue
					}
					count := 1
					if version == "old" && service == "web" {
						count = 2
					}
					for i := range count {
						id := create(service+"-"+version+string(rune('a'+i)), pool, versions[version], compute_v1alpha.RUNNING, true)
						if service == tc.wantService && version == tc.wantVersion {
							eligible[id.String()] = true
						}
					}
				}
			}
			client := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(NewServer(testutils.TestLogger(t), inmem.Client))))
			for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
				other := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: method, Metadata: map[string]any{"app": "other", "bound_app": "other"}})
				_, err := client.SelectForApp(other, "shop", tc.service)
				require.ErrorContains(t, err, "unauthorized")
			}
			own := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"app": "shop"}})
			result, err := client.SelectForApp(own, "shop", tc.service)
			require.NoError(t, err)
			require.Equal(t, tc.wantFailure, result.Failure())
			if tc.wantFailure != "" {
				require.Nil(t, result.Selection())
				switch tc.wantFailure {
				case "unknown_service":
					require.Equal(t, []string{"web", "worker"}, result.Services())
				case "missing_default":
					require.Equal(t, []string{"worker"}, result.Services())
				}
				return
			}
			got := result.Selection()
			require.True(t, eligible[got.Id()], "selected an ineligible sandbox: %s", got.Id())
			require.NotEmpty(t, got.ShortId())
			require.Equal(t, tc.wantService, got.Service())
			require.Equal(t, versions[tc.wantVersion].String(), got.Version())
			require.Equal(t, tc.wantCount, got.CandidateCount())
			wire, err := json.Marshal(got)
			require.NoError(t, err)
			require.NotContains(t, string(wire), "SECRET")
			require.NotContains(t, string(wire), "spec")
		})
	}
}

func TestSandboxLifecycleRejectsAliasedOwnerReferences(t *testing.T) {
	if testing.Short() {
		t.Skip("requires etcd short-ID resolution")
	}
	for _, ownerKind := range []string{"pool", "version", "app", "wrong-kind-app"} {
		t.Run(ownerKind, func(t *testing.T) {
			store, cleanup := testutils.NewEtcdEntityServer(t)
			t.Cleanup(cleanup)
			ctx := t.Context()
			appID, err := store.Client.Create(ctx, "shop", &core_v1alpha.App{})
			require.NoError(t, err)
			versionID, err := store.Client.Create(ctx, "shop", &core_v1alpha.AppVersion{App: appID})
			require.NoError(t, err)
			poolID, err := store.Client.Create(ctx, "shop", &compute_v1alpha.SandboxPool{App: appID})
			require.NoError(t, err)
			var dangling entity.Id
			sb := &compute_v1alpha.Sandbox{Status: compute_v1alpha.RUNNING, Spec: compute_v1alpha.SandboxSpec{Version: versionID}}
			var labels types.Labels
			if ownerKind == "wrong-kind-app" {
				require.NoError(t, store.Client.UpdateAttrs(ctx, versionID, entity.Ref(core_v1alpha.AppVersionAppId, versionID)))
			} else {
				target := map[string]entity.Id{"pool": poolID, "version": versionID, "app": appID}[ownerKind]
				record, err := store.Store.GetEntity(ctx, target)
				require.NoError(t, err)
				switch ownerKind {
				case "pool":
					dangling, err = store.Client.Create(ctx, record.ShortId(), &compute_v1alpha.SandboxPool{App: appID})
					labels = types.LabelSet("pool", dangling.String())
				case "version":
					dangling, err = store.Client.Create(ctx, record.ShortId(), &core_v1alpha.AppVersion{App: appID})
					sb.Spec.Version = dangling
				case "app":
					dangling, err = store.Client.Create(ctx, record.ShortId(), &core_v1alpha.App{})
					require.NoError(t, err)
					require.NoError(t, store.Client.UpdateAttrs(ctx, versionID, entity.Ref(core_v1alpha.AppVersionAppId, dangling)))
				}
				require.NoError(t, err)
			}
			id, err := store.Client.Create(ctx, "target", sb, entityserver.WithLabels(labels))
			require.NoError(t, err)
			if dangling != "" {
				require.NoError(t, store.Client.Delete(ctx, dangling))
			}
			client := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(NewServer(testutils.TestLogger(t), store.Client))))
			for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
				bound := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: method, Metadata: map[string]any{"app": "shop", "bound_app": "shop"}})
				_, err := client.Stop(bound, id.String())
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
				require.EqualError(t, err, "unauthorized")
				_, err = client.Delete(bound, id.String())
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
				require.EqualError(t, err, "unauthorized")
			}
			var current compute_v1alpha.Sandbox
			require.NoError(t, store.Client.GetById(ctx, id, &current))
			require.Equal(t, compute_v1alpha.RUNNING, current.Status)
			_, err = client.Stop(ctx, id.String())
			require.NoError(t, err)
		})
	}
}

func TestSelectForAppRejectsNonApp(t *testing.T) {
	store, cleanup := testutils.NewEtcdEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	versionID, err := store.Client.Create(ctx, "shop", &core_v1alpha.AppVersion{})
	require.NoError(t, err)
	record, err := store.Store.GetEntity(ctx, versionID)
	require.NoError(t, err)
	client := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(NewServer(testutils.TestLogger(t), store.Client))))
	_, err = client.SelectForApp(ctx, record.ShortId(), "")
	require.ErrorContains(t, err, "not an app")
}

type sandboxVisibilityAuthorizer struct {
	action string
	admin  bool
}

func (sandboxVisibilityAuthorizer) Authorize(context.Context, *rpc.Identity, string, string) error {
	return nil
}
func (a sandboxVisibilityAuthorizer) AuthorizeResource(_ context.Context, _ *rpc.Identity, req rpc.ResourceRequest) error {
	if a.admin || (req.Resource == "app" && req.Action == a.action && req.Instance == "mine" && req.Owner == "alice") {
		return nil
	}
	return rpc.ErrUnauthorized
}

func TestSandboxInventoryVisibilityAndSelection(t *testing.T) {
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	versions := map[string]entity.Id{}
	pools := map[string]entity.Id{}
	for name, owner := range map[string]string{"mine": "alice", "private": "bob"} {
		appID, err := inmem.Client.Create(ctx, name, &core_v1alpha.App{CreatedBy: owner})
		require.NoError(t, err)
		versions[name], err = inmem.Client.Create(ctx, name, &core_v1alpha.AppVersion{App: appID})
		require.NoError(t, err)
		pools[name], err = inmem.Client.Create(ctx, name, &compute_v1alpha.SandboxPool{App: appID})
		require.NoError(t, err)
	}
	want := []string{}
	for _, tc := range []struct {
		name, pool, version string
		visible             bool
	}{
		{"owned-pool", pools["mine"].String(), versions["private"].String(), true},
		{"owned-run", "", versions["mine"].String(), true},
		{"private-pool", pools["private"].String(), versions["mine"].String(), false},
		{"broken-pool", "sandbox_pool/missing", versions["mine"].String(), false},
		{"orphan", "", "", false},
	} {
		id, err := inmem.Client.Create(ctx, tc.name, &compute_v1alpha.Sandbox{
			Spec: compute_v1alpha.SandboxSpec{Version: entity.Id(tc.version)},
		}, entityserver.WithLabels(types.LabelSet("pool", tc.pool)))
		require.NoError(t, err)
		if tc.visible {
			want = append(want, id.String())
		}
	}
	client := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(NewServer(testutils.TestLogger(t), inmem.Client))))
	caller := func(action string, admin bool) context.Context {
		return rpc.ContextWithAuthorization(ctx, sandboxVisibilityAuthorizer{action: action, admin: admin}, "sandboxes", "list")
	}
	listed, err := client.List(caller("list", false))
	require.NoError(t, err)
	var got []string
	for _, sb := range listed.Sandboxes() {
		got = append(got, sb.Id())
		require.Equal(t, "mine", sb.App(), "display ownership must agree with authorization, not the version")
	}
	require.ElementsMatch(t, want, got)
	readOnly, err := client.List(caller("read", false))
	require.NoError(t, err)
	require.Empty(t, readOnly.Sandboxes())
	poolClient := compute_v1alpha.NewSandboxPoolsClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxPools(NewPoolServer(inmem.Client))))
	listedPools, err := poolClient.List(caller("list", false), "")
	require.NoError(t, err)
	require.Len(t, listedPools.Pools(), 1)
	require.Equal(t, pools["mine"].String(), listedPools.Pools()[0].Id())
	for _, app := range []string{"mine", "private", "nonexistent"} {
		filtered, err := poolClient.List(caller("list", false), app)
		require.NoError(t, err, app)
		if app == "mine" {
			require.Len(t, filtered.Pools(), 1)
			require.Equal(t, pools["mine"].String(), filtered.Pools()[0].Id())
		} else {
			require.Empty(t, filtered.Pools(), "hidden and nonexistent apps must be indistinguishable")
		}
	}
	listedPools, err = poolClient.List(caller("read", false), "")
	require.NoError(t, err)
	require.Empty(t, listedPools.Pools())
	admin, err := client.List(caller("", true))
	require.NoError(t, err)
	require.Len(t, admin.Sandboxes(), 5, "administrators retain orphan visibility")
	_, err = client.SelectForApp(caller("list", false), "mine", "")
	require.ErrorIs(t, err, rpc.ErrUnauthorized)
	_, err = client.SelectForApp(caller("exec", false), "mine", "")
	require.NoError(t, err)
	_, err = client.SelectForApp(caller("exec", false), "private", "")
	require.ErrorIs(t, err, rpc.ErrUnauthorized)
	for _, method := range []rpc.AuthMethod{rpc.AuthMethodOIDC, rpc.AuthMethodWorkload} {
		bound := rpc.ContextWithIdentity(ctx, &rpc.Identity{Method: method, Metadata: map[string]any{"app": "mine", "bound_app": "mine"}})
		listed, err := client.List(bound)
		require.NoError(t, err)
		require.Len(t, listed.Sandboxes(), 2)
		// Even a cluster administrator cannot use orphan recovery through an
		// app-bound identity.
		bound = rpc.ContextWithAuthorization(bound, sandboxVisibilityAuthorizer{admin: true}, "sandboxes", "list")
		listed, err = client.List(bound)
		require.NoError(t, err)
		require.Len(t, listed.Sandboxes(), 2)
	}
}

func TestInventoryPropagatesGlobalAuthorizationFailureWhenEmpty(t *testing.T) {
	store, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ctx := rpc.ContextWithAuthorization(t.Context(), inventoryFailedAuthorizer{}, "sandboxes", "list")
	sandboxes := compute_v1alpha.NewSandboxesClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxes(NewServer(testutils.TestLogger(t), store.Client))))
	_, err := sandboxes.List(ctx)
	require.ErrorContains(t, err, "policy service unavailable")
	pools := compute_v1alpha.NewSandboxPoolsClient(rpc.LocalClient(compute_v1alpha.AdaptSandboxPools(NewPoolServer(store.Client))))
	_, err = pools.List(ctx, "")
	require.ErrorContains(t, err, "policy service unavailable")
}

type inventoryFailedAuthorizer struct{}

func (inventoryFailedAuthorizer) Authorize(context.Context, *rpc.Identity, string, string) error {
	return nil
}

func (inventoryFailedAuthorizer) AuthorizeResource(context.Context, *rpc.Identity, rpc.ResourceRequest) error {
	return fmt.Errorf("policy service unavailable")
}
