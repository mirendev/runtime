package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadroles"
	"miren.dev/runtime/servers/entityserver"
)

func managementFixture(t *testing.T) (*ingress_v1alpha.RoutesClient, *testutils.InMemEntityServer) {
	t.Helper()
	im, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	for _, name := range []string{"alpha", "beta"} {
		cv, err := im.Client.Create(t.Context(), name, &core_v1alpha.ConfigVersion{Spec: core_v1alpha.ConfigSpec{
			Services: []core_v1alpha.ConfigSpecServices{
				{Name: "web"}, {Name: "api", Port: 8080, PortType: "http"}, {Name: "worker", Port: 9000, PortType: "tcp"},
			},
			Variables: []core_v1alpha.ConfigSpecVariables{{Key: "SECRET", Value: "resolved-env-secret"}},
		}})
		require.NoError(t, err)
		ver, err := im.Client.Create(t.Context(), name, &core_v1alpha.AppVersion{ConfigVersion: cv})
		require.NoError(t, err)
		_, err = im.Client.Create(t.Context(), name, &core_v1alpha.App{ActiveVersion: ver})
		require.NoError(t, err)
	}
	_, err := im.Client.Create(t.Context(), "oidc", &ingress_v1alpha.OidcProvider{Name: "oidc", ClientSecret: "oauth-secret", ConfigJson: "connector-secret"})
	require.NoError(t, err)
	_, err = im.Client.Create(t.Context(), "github", &ingress_v1alpha.OidcProvider{Name: "github", ConnectorType: "github", ClientSecret: "github-secret"})
	require.NoError(t, err)
	_, err = im.Client.Create(t.Context(), "password", &ingress_v1alpha.PasswordProvider{Name: "password", PasswordHash: "bcrypt-secret"})
	require.NoError(t, err)
	ic := ingress.NewClient(slog.Default(), im.EAC.Client)
	server := NewServer(slog.Default(), ic)
	return ingress_v1alpha.NewRoutesClient(rpc.LocalClient(ingress_v1alpha.AdaptRoutes(server))), im
}

func appContext(method rpc.AuthMethod, app string) context.Context {
	return rpc.ContextWithIdentity(context.Background(), &rpc.Identity{
		Method: method, Subject: "test-caller", Metadata: map[string]any{"app": app, "bound_app": app, "role": workloadroles.RoleAppAdmin},
	})
}

func TestRouteManagementContract(t *testing.T) {
	c, im := managementFixture(t)
	ctx := appContext(rpc.AuthMethodWorkload, "alpha")
	set, err := c.Set(operatorContext("operator"), "APP.EXAMPLE.COM", "alpha", "")
	require.NoError(t, err)
	assert.Equal(t, "web", set.Route().Service())
	assert.Equal(t, "app.example.com", set.Route().Host())

	m := &ingress_v1alpha.RouteClaimMapping{}
	m.SetClaim("email")
	m.SetHeader("X-User-Email")
	protected, err := c.Protect(ctx, testHost, false, "oidc", []*ingress_v1alpha.RouteClaimMapping{m}, false)
	require.NoError(t, err)
	assert.Equal(t, "oidc", protected.Route().Provider())
	assert.Len(t, protected.Route().ClaimMappings(), 1)
	_, err = c.Waf(ctx, testHost, false, 3)
	require.NoError(t, err)
	_, err = c.Timeout(ctx, testHost, false, "2m30s")
	require.NoError(t, err)
	_, err = c.TlsCheck(ctx, testHost, "/authorize-tls")
	require.NoError(t, err)
	_, err = c.SetMaintenance(operatorContext("operator"), testHost, false, "upgrade", "")
	require.NoError(t, err)
	_, err = c.Set(ctx, testHost, "alpha", "api")
	require.NoError(t, err)

	shown, err := c.Show(ctx, testHost, false)
	require.NoError(t, err)
	r := shown.Route()
	assert.Equal(t, "api", r.Service())
	assert.Equal(t, int64(3), r.WafLevel())
	assert.True(t, r.WafEnabled())
	assert.True(t, r.Maintenance())
	assert.Equal(t, "2m30s", r.RequestTimeout())
	assert.Equal(t, "/authorize-tls", r.TlsCheck())
	assert.Equal(t, "upgrade", r.MaintenanceReason())
	assert.Equal(t, "operator", r.MaintenanceStartedBy())
	require.Len(t, r.ClaimMappings(), 1, "mutations must not accumulate repeated mappings")
	assert.Equal(t, "X-User-Email", r.ClaimMappings()[0].Header())
	assert.Positive(t, r.CreatedAt())

	listed, err := c.List(ctx, "")
	require.NoError(t, err)
	require.Len(t, listed.Routes(), 1)
	data, err := json.Marshal(listed.Routes())
	require.NoError(t, err)
	for _, secret := range []string{"oauth-secret", "connector-secret", "bcrypt-secret", "resolved-env-secret", "password_hash", "client_secret", "config_json", "attrs", "auth_provider", "waf_profile"} {
		assert.NotContains(t, string(data), secret)
	}

	_, err = c.Protect(ctx, testHost, false, "github", nil, false)
	require.NoError(t, err)
	shown, err = c.Show(ctx, testHost, false)
	require.NoError(t, err)
	assert.Equal(t, "connector", shown.Route().ProviderType())
	assert.Equal(t, "github", shown.Route().ConnectorType())
	assert.Empty(t, shown.Route().ClaimMappings())
	_, err = c.Protect(ctx, testHost, false, "password", nil, false)
	require.NoError(t, err)
	shown, err = c.Show(ctx, testHost, false)
	require.NoError(t, err)
	assert.Equal(t, "password", shown.Route().ProviderType())
	_, err = c.Unprotect(ctx, testHost, false)
	require.NoError(t, err)
	cleared, err := c.Timeout(ctx, testHost, false, "")
	require.NoError(t, err)
	assert.Empty(t, cleared.Route().RequestTimeout())
	assert.Equal(t, int64(3), cleared.Route().WafLevel(), "clearing timeout preserves WAF")
	assert.Equal(t, "alpha", cleared.Route().App())
	assert.Equal(t, testHost, cleared.Route().Host())
	_, err = c.Waf(ctx, testHost, false, 0)
	require.NoError(t, err)
	_, err = c.TlsCheck(ctx, testHost, "")
	require.NoError(t, err)
	shown, err = c.Show(ctx, testHost, false)
	require.NoError(t, err)
	assert.Empty(t, shown.Route().Provider())
	assert.Empty(t, shown.Route().ClaimMappings())
	assert.Zero(t, shown.Route().WafLevel())
	assert.False(t, shown.Route().WafEnabled())
	assert.Empty(t, shown.Route().RequestTimeout())
	assert.Empty(t, shown.Route().TlsCheck())
	_, err = c.Remove(ctx, testHost)
	require.NoError(t, err)
	_, err = c.Show(ctx, testHost, false)
	require.Error(t, err)
	// Provider entities remain on their separately owned management plane.
	var pw ingress_v1alpha.PasswordProvider
	require.NoError(t, im.Client.Get(t.Context(), "password", &pw))
}

func TestRouteOwnershipAndDefaults(t *testing.T) {
	c, _ := managementFixture(t)
	op := operatorContext("operator")
	_, err := c.Set(op, testHost, "alpha", "api")
	require.NoError(t, err)
	_, err = c.Set(op, "beta.example.com", "beta", "web")
	require.NoError(t, err)
	_, err = c.SetDefault(op, "alpha")
	require.NoError(t, err)

	for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
		ctx := appContext(method, "beta")
		calls := map[string]func() error{
			"claim hostname":      func() error { _, e := c.Set(ctx, "new.example.com", "beta", "web"); return e },
			"claim wildcard":      func() error { _, e := c.Set(ctx, "*.example.net", "beta", "web"); return e },
			"show":                func() error { _, e := c.Show(ctx, testHost, false); return e },
			"remove":              func() error { _, e := c.Remove(ctx, testHost); return e },
			"take ownership":      func() error { _, e := c.Set(ctx, testHost, "beta", "api"); return e },
			"give away ownership": func() error { _, e := c.Set(ctx, "beta.example.com", "alpha", "web"); return e },
			"protect":             func() error { _, e := c.Protect(ctx, testHost, false, "oidc", nil, false); return e },
			"unprotect":           func() error { _, e := c.Unprotect(ctx, testHost, false); return e },
			"waf":                 func() error { _, e := c.Waf(ctx, testHost, false, 2); return e },
			"timeout":             func() error { _, e := c.Timeout(ctx, testHost, false, "1m"); return e },
			"tls":                 func() error { _, e := c.TlsCheck(ctx, testHost, "/tls"); return e },
			"list other app":      func() error { _, e := c.List(ctx, "alpha"); return e },
			"set default":         func() error { _, e := c.SetDefault(ctx, "beta"); return e },
			"unset default":       func() error { _, e := c.UnsetDefault(ctx); return e },
			"show default":        func() error { _, e := c.Show(ctx, "", true); return e },
			"protect default":     func() error { _, e := c.Protect(ctx, "", true, "password", nil, false); return e },
			"unprotect default":   func() error { _, e := c.Unprotect(ctx, "", true); return e },
			"waf default":         func() error { _, e := c.Waf(ctx, "", true, 1); return e },
			"timeout default":     func() error { _, e := c.Timeout(ctx, "", true, "1m"); return e },
		}
		for name, call := range calls {
			t.Run(string(method)+"/"+name, func(t *testing.T) { require.ErrorContains(t, call(), "unauthorized") })
		}
		_, err := c.Set(ctx, "beta.example.com", "beta", "api")
		require.NoError(t, err, "app-scoped callers can still update an existing route they own")
		list, err := c.List(ctx, "")
		require.NoError(t, err)
		require.Len(t, list.Routes(), 1)
		assert.Equal(t, "beta.example.com", list.Routes()[0].Host())
		// Even an app that owns the default target cannot see default policy.
		_, err = c.Show(appContext(method, "alpha"), "", true)
		require.ErrorContains(t, err, "unauthorized")
	}

	_, err = c.Protect(op, "", true, "password", nil, false)
	require.NoError(t, err)
	_, err = c.Waf(op, "", true, 4)
	require.NoError(t, err)
	_, err = c.Timeout(op, "", true, "4m")
	require.NoError(t, err)
	_, err = c.SetDefault(op, "beta")
	require.NoError(t, err)
	shown, err := c.Show(op, "", true)
	require.NoError(t, err)
	assert.Equal(t, "beta", shown.Route().App())
	assert.Equal(t, "password", shown.Route().Provider())
	assert.Equal(t, int64(4), shown.Route().WafLevel())
	assert.Equal(t, "4m", shown.Route().RequestTimeout())
	listed, err := c.List(op, "")
	require.NoError(t, err)
	require.Len(t, listed.Routes(), 3, "changing the default must not leave a second default")
	unset, err := c.UnsetDefault(op)
	require.NoError(t, err)
	assert.True(t, unset.Changed())
	assert.Equal(t, "beta", unset.App())
	unset, err = c.UnsetDefault(op)
	require.NoError(t, err)
	assert.False(t, unset.Changed())
	_, err = c.Show(op, testHost, false)
	require.NoError(t, err, "unsetting defaults must leave named routes intact")
	_, err = c.Set(op, testHost, "beta", "web")
	require.NoError(t, err, "cluster operators can reassign routes")
	_, err = c.Set(op, "*.example.net", "alpha", "web")
	require.NoError(t, err, "cluster operators can claim wildcards")
	for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
		_, err = c.Set(appContext(method, "beta"), "feat.example.net", "beta", "web")
		require.ErrorIs(t, err, rpc.ErrUnauthorized, "an exact claim must not shadow another app's wildcard")
	}
	_, err = c.Show(op, "feat.example.net", false)
	require.Error(t, err, "denied claims must not create an exact route")
}

func TestRouteValidation(t *testing.T) {
	c, im := managementFixture(t)
	ctx := appContext(rpc.AuthMethodWorkload, "alpha")
	_, err := c.Set(operatorContext("operator"), testHost, "alpha", "web")
	require.NoError(t, err)
	_, err = c.Timeout(ctx, testHost, false, "45s")
	require.NoError(t, err)
	mapping := &ingress_v1alpha.RouteClaimMapping{}
	mapping.SetClaim("email")
	mapping.SetHeader("bad\r\nheader")
	bad := map[string]func() error{
		"invalid mapping": func() error {
			_, e := c.Protect(ctx, testHost, false, "oidc", []*ingress_v1alpha.RouteClaimMapping{mapping}, false)
			return e
		},
		"password mapping": func() error {
			_, e := c.Protect(ctx, testHost, false, "password", []*ingress_v1alpha.RouteClaimMapping{mapping}, false)
			return e
		},
		"missing service":      func() error { _, e := c.Set(ctx, testHost, "alpha", "missing"); return e },
		"tcp service":          func() error { _, e := c.Set(ctx, testHost, "alpha", "worker"); return e },
		"bad wildcard":         func() error { _, e := c.Set(ctx, "*.com", "alpha", "web"); return e },
		"missing provider":     func() error { _, e := c.Protect(ctx, testHost, false, "absent", nil, false); return e },
		"waf high":             func() error { _, e := c.Waf(ctx, testHost, false, 5); return e },
		"waf negative":         func() error { _, e := c.Waf(ctx, testHost, false, -1); return e },
		"timeout zero":         func() error { _, e := c.Timeout(ctx, testHost, false, "0s"); return e },
		"timeout negative":     func() error { _, e := c.Timeout(ctx, testHost, false, "-2s"); return e },
		"timeout malformed":    func() error { _, e := c.Timeout(ctx, testHost, false, "forever"); return e },
		"tls query":            func() error { _, e := c.TlsCheck(ctx, testHost, "/tls?query=1"); return e },
		"tls host":             func() error { _, e := c.TlsCheck(ctx, testHost, "//evil.example/tls"); return e },
		"conflicting selector": func() error { _, e := c.Show(operatorContext("operator"), testHost, true); return e },
	}
	for name, call := range bad {
		t.Run(name, func(t *testing.T) { require.Error(t, call()) })
	}
	shown, err := c.Show(ctx, testHost, false)
	require.NoError(t, err)
	assert.Equal(t, "45s", shown.Route().RequestTimeout(), "rejected writes leave the stored timeout intact")
	// Default web is a real service selection, not a validation escape hatch.
	var app core_v1alpha.App
	require.NoError(t, im.Client.Get(t.Context(), "alpha", &app))
	ver, err := im.Client.Create(t.Context(), "no-web", &core_v1alpha.AppVersion{Config: core_v1alpha.Config{
		Services: []core_v1alpha.Services{{Name: "worker", Port: 9000, PortType: "tcp"}},
	}})
	require.NoError(t, err)
	app.ActiveVersion = ver
	require.NoError(t, im.Client.Update(t.Context(), &app))
	_, err = c.Set(ctx, "new.example.com", "alpha", "")
	require.ErrorContains(t, err, "web")
	// Default-route assignment historically only requires an existing app.
	_, err = c.SetDefault(operatorContext("operator"), "alpha")
	require.NoError(t, err)
}

func TestRouteDanglingProviderReference(t *testing.T) {
	c, im := managementFixture(t)
	ctx := appContext(rpc.AuthMethodWorkload, "alpha")
	_, err := c.Set(operatorContext("operator"), testHost, "alpha", "web")
	require.NoError(t, err)
	_, err = c.Protect(ctx, testHost, false, "oidc", nil, false)
	require.NoError(t, err)
	// Forced provider removal is owned by the provider API, not route mutation.
	require.NoError(t, im.Client.Delete(t.Context(), "oidc_provider/oidc"))
	shown, err := c.Show(ctx, testHost, false)
	require.NoError(t, err)
	assert.True(t, shown.Route().Protected())
	assert.Empty(t, shown.Route().Provider())
	_, err = c.Timeout(ctx, testHost, false, "1m")
	require.NoError(t, err)
	var route ingress_v1alpha.HttpRoute
	require.NoError(t, im.Client.Get(t.Context(), testHost, &route))
	assert.Equal(t, entity.Id("oidc_provider/oidc"), route.AuthProvider)
	_, err = c.Unprotect(ctx, testHost, false)
	require.NoError(t, err)
}

func TestRouteRolePolicy(t *testing.T) {
	auth := oidcauth.NewCompositeAuthorizer(nil)
	for _, role := range []string{workloadroles.RoleAppReadonly, workloadroles.RoleAppAdmin, workloadroles.RoleClusterReadonly, workloadroles.RoleClusterAdmin} {
		identity := &rpc.Identity{Method: rpc.AuthMethodWorkload, Metadata: map[string]any{"role": role}}
		for _, action := range []string{"list", "show", "set", "remove", "protect", "unprotect", "waf", "timeout", "tlscheck", "setdefault", "unsetdefault"} {
			err := auth.Authorize(t.Context(), identity, "routes", action)
			allowed := action == "list" || action == "show" || role == workloadroles.RoleClusterAdmin || (role == workloadroles.RoleAppAdmin && action != "setdefault" && action != "unsetdefault")
			assert.Equal(t, allowed, err == nil, "%s/%s", role, action)
		}
		require.Error(t, auth.Authorize(t.Context(), identity, "entityaccess", "get"))
		require.Error(t, auth.Authorize(t.Context(), identity, "entityaccess", "deleteatrevision"))
	}
}

// Inject ownership changes at the entity boundary after the handler's read.
// Both updates and deletes must fail rather than touch the new owner's route.
type racingEntities struct {
	*entityserver.EntityServer
	race func()
}

func (e *racingEntities) Replace(ctx context.Context, state *entityserver_v1alpha.EntityAccessReplace) error {
	e.race()
	return e.EntityServer.Replace(ctx, state)
}

func (e *racingEntities) DeleteAtRevision(ctx context.Context, state *entityserver_v1alpha.EntityAccessDeleteAtRevision) error {
	e.race()
	return e.EntityServer.DeleteAtRevision(ctx, state)
}

func TestRouteOwnershipRace(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			im, cleanup := testutils.NewEtcdEntityServer(t)
			t.Cleanup(cleanup)
			alpha, err := im.Client.Create(t.Context(), "alpha", &core_v1alpha.App{})
			require.NoError(t, err)
			beta, err := im.Client.Create(t.Context(), "beta", &core_v1alpha.App{})
			require.NoError(t, err)
			routeID, err := im.Client.Create(t.Context(), testHost, &ingress_v1alpha.HttpRoute{
				Host: testHost, App: alpha, Service: "api",
			})
			require.NoError(t, err)
			ctx := appContext(rpc.AuthMethodWorkload, "alpha")
			proxy := &racingEntities{EntityServer: im.Server}
			proxy.race = func() {
				require.NoError(t, im.Client.Patch(t.Context(), routeID, 0, entity.Ref(ingress_v1alpha.HttpRouteAppId, beta)))
			}
			ic := ingress.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(proxy)))
			c := ingress_v1alpha.NewRoutesClient(rpc.LocalClient(ingress_v1alpha.AdaptRoutes(NewServer(slog.Default(), ic))))
			if remove {
				_, err = c.Remove(ctx, testHost)
			} else {
				_, err = c.Timeout(ctx, testHost, false, "8m")
			}
			require.Error(t, err)
			var route ingress_v1alpha.HttpRoute
			require.NoError(t, im.Client.Get(t.Context(), testHost, &route))
			assert.Equal(t, beta, route.App)
			assert.Empty(t, route.RequestTimeout)
		})
	}
}

func TestRouteTargetResolvedAppAuthorizationEtcd(t *testing.T) {
	if testing.Short() {
		t.Skip("requires etcd short-ID resolution")
	}
	store, cleanup := testutils.NewEtcdEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	configID, err := store.Client.Create(ctx, "config", &core_v1alpha.ConfigVersion{Spec: core_v1alpha.ConfigSpec{
		Services: []core_v1alpha.ConfigSpecServices{{Name: "web"}},
	}})
	require.NoError(t, err)
	versionID, err := store.Client.Create(ctx, "version", &core_v1alpha.AppVersion{ConfigVersion: configID})
	require.NoError(t, err)
	appID, err := store.Client.Create(ctx, "other", &core_v1alpha.App{ActiveVersion: versionID})
	require.NoError(t, err)
	var app core_v1alpha.App
	record, err := store.Client.GetByIdWithEntity(ctx, appID, &app)
	require.NoError(t, err)
	alias := record.Entity().ShortId()
	require.NotEmpty(t, alias)
	ic := ingress.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(store.Server)))
	client := ingress_v1alpha.NewRoutesClient(rpc.LocalClient(ingress_v1alpha.AdaptRoutes(NewServer(slog.Default(), ic))))
	for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
		_, err := client.Set(appContext(method, alias), "blocked.example.com", alias, "web")
		require.ErrorIs(t, err, rpc.ErrUnauthorized)
		_, err = client.SetDefault(appContext(method, alias), alias)
		require.ErrorIs(t, err, rpc.ErrUnauthorized)
	}
	route, err := ic.Lookup(ctx, "blocked.example.com")
	require.NoError(t, err)
	require.Nil(t, route)
	op := operatorContext("operator")
	result, err := client.Set(op, "allowed.example.com", alias, "web")
	require.NoError(t, err)
	require.Equal(t, "other", result.Route().App())
	def, err := client.SetDefault(op, alias)
	require.NoError(t, err)
	require.Equal(t, "other", def.Route().App())
	var version core_v1alpha.AppVersion
	record, err = store.Client.GetByIdWithEntity(ctx, versionID, &version)
	require.NoError(t, err)
	_, err = client.Set(op, "invalid.example.com", record.Entity().ShortId(), "web")
	require.ErrorContains(t, err, "not an app")
	_, err = client.SetDefault(op, record.Entity().ShortId())
	require.ErrorContains(t, err, "not an app")
}

func TestRouteOwnerRequiresExactAppReferenceEtcd(t *testing.T) {
	if testing.Short() {
		t.Skip("requires etcd short-ID resolution")
	}
	store, cleanup := testutils.NewEtcdEntityServer(t)
	t.Cleanup(cleanup)
	ctx := t.Context()
	appID, err := store.Client.Create(ctx, "other", &core_v1alpha.App{})
	require.NoError(t, err)
	versionID, err := store.Client.Create(ctx, "other", &core_v1alpha.AppVersion{App: appID})
	require.NoError(t, err)
	var app core_v1alpha.App
	appRecord, err := store.Client.GetByIdWithEntity(ctx, appID, &app)
	require.NoError(t, err)
	var version core_v1alpha.AppVersion
	versionRecord, err := store.Client.GetByIdWithEntity(ctx, versionID, &version)
	require.NoError(t, err)
	ic := ingress.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(store.Server)))
	client := ingress_v1alpha.NewRoutesClient(rpc.LocalClient(ingress_v1alpha.AdaptRoutes(NewServer(slog.Default(), ic))))
	for _, tc := range []struct {
		name  string
		owner entity.Id
	}{
		{"app-alias", entity.Id("app/" + appRecord.Entity().ShortId())},
		{"version-alias", entity.Id("app/" + versionRecord.Entity().ShortId())},
		{"wrong-kind", versionID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := tc.name + ".example.com"
			if tc.owner != versionID {
				_, err := store.Client.Create(ctx, tc.owner.String()[len("app/"):], &core_v1alpha.App{})
				require.NoError(t, err)
			}
			id, err := store.Client.Create(ctx, host, &ingress_v1alpha.HttpRoute{Host: host, App: tc.owner, Service: "web"})
			require.NoError(t, err)
			if tc.owner != versionID {
				require.NoError(t, store.Client.Delete(ctx, tc.owner))
			}
			for _, method := range []rpc.AuthMethod{rpc.AuthMethodWorkload, rpc.AuthMethodOIDC} {
				scoped := appContext(method, "other")
				_, err := client.Show(scoped, host, false)
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
				_, err = client.Timeout(scoped, host, false, "5m")
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
				_, err = client.Remove(scoped, host)
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
				listed, err := client.List(scoped, "")
				require.NoError(t, err)
				for _, route := range listed.Routes() {
					require.NotEqual(t, host, route.Host())
				}
			}
			var unchanged ingress_v1alpha.HttpRoute
			require.NoError(t, store.Client.GetById(ctx, id, &unchanged))
			require.Equal(t, tc.owner, unchanged.App)
			require.Empty(t, unchanged.RequestTimeout)
			op := operatorContext("operator")
			result, err := client.Show(op, host, false)
			require.NoError(t, err)
			require.Empty(t, result.Route().App())
			_, err = client.Remove(op, host)
			require.NoError(t, err)
		})
	}
	_, err = store.Client.Create(ctx, "owned.example.com", &ingress_v1alpha.HttpRoute{Host: "owned.example.com", App: appID, Service: "web"})
	require.NoError(t, err)
	result, err := client.Show(appContext(rpc.AuthMethodWorkload, "other"), "owned.example.com", false)
	require.NoError(t, err)
	require.Equal(t, "other", result.Route().App())
}

type rereadingEntities struct {
	*entityserver.EntityServer
	id   entity.Id
	race func()
}

func (e *rereadingEntities) Get(ctx context.Context, state *entityserver_v1alpha.EntityAccessGet) error {
	if state.Args().Id() == e.id.String() && e.race != nil {
		race := e.race
		e.race = nil
		race()
	}
	return e.EntityServer.Get(ctx, state)
}

func TestRouteSnapshotRequiresExactRouteEtcd(t *testing.T) {
	for _, replacement := range []string{"short-id-alias", "wrong-kind"} {
		for _, operation := range []string{"show", "timeout"} {
			t.Run(replacement+"/"+operation, func(t *testing.T) {
				im, cleanup := testutils.NewEtcdEntityServer(t)
				t.Cleanup(cleanup)
				ctx := operatorContext("operator")
				appID, err := im.Client.Create(ctx, "owner", &core_v1alpha.App{})
				require.NoError(t, err)
				victimID, err := im.Client.Create(ctx, "victim.example", &ingress_v1alpha.HttpRoute{
					Host: "victim.example", App: appID, RequestTimeout: "17s",
				})
				require.NoError(t, err)
				victim, err := im.Server.Store.GetEntity(ctx, victimID)
				require.NoError(t, err)
				// A default route's ID ends in the app name, which can match
				// the short ID of an unrelated route after default deletion.
				alias := victim.ShortId()
				require.NotEmpty(t, alias)
				targetID, err := im.Client.Create(ctx, alias, &core_v1alpha.App{})
				require.NoError(t, err)
				defaultID, err := im.Client.Create(ctx, "default-"+targetID.String(), &ingress_v1alpha.HttpRoute{
					Default: true, App: targetID,
				})
				require.NoError(t, err)
				proxy := &rereadingEntities{EntityServer: im.Server, id: defaultID}
				proxy.race = func() {
					if replacement == "short-id-alias" {
						require.NoError(t, im.Server.Store.DeleteEntity(ctx, defaultID))
					} else {
						victim, err = im.Server.Store.ReplaceEntity(ctx, entity.New(
							entity.DBId, defaultID, (&core_v1alpha.App{}).Encode,
						))
						require.NoError(t, err)
						victimID = defaultID
					}
					victim, err = im.Server.Store.GetEntity(ctx, victimID)
					require.NoError(t, err)
				}
				ic := ingress.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(proxy)))
				client := ingress_v1alpha.NewRoutesClient(rpc.LocalClient(ingress_v1alpha.AdaptRoutes(NewServer(slog.Default(), ic))))
				if operation == "show" {
					_, err = client.Show(ctx, "", true)
				} else {
					_, err = client.Timeout(ctx, "", true, "9m")
				}
				require.ErrorIs(t, err, cond.ErrNotFound{})
				retained, err := im.Server.Store.GetEntity(ctx, victimID)
				require.NoError(t, err)
				require.Equal(t, victim.Attrs(), retained.Attrs(), "a rejected read or mutation must leave the replacement entity unchanged")
			})
		}
	}
}
