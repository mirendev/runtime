package commands

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/routes"
)

// Only Routes is exposed: an accidental EntityAccess or IdentityProviders
// dependency fails on the wire rather than passing through a permissive mock.
func TestRouteCLIWithoutEntityAccess(t *testing.T) {
	testRouteCLI(t, "current")
}

func TestRouteCLILegacyFallback(t *testing.T) {
	for _, mode := range []string{"old-service", "missing-service", "missing-timeout-method"} {
		t.Run(mode, func(t *testing.T) { testRouteCLI(t, mode) })
	}
}

func testRouteCLI(t *testing.T, mode string) {
	t.Helper()
	t.Setenv("MIREN_CLUSTER", "")
	t.Setenv("MIREN_CONFIG", filepath.Join(t.TempDir(), "no-config"))
	im, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	// SchemaKind entries installed by schema.Apply use string values, whereas
	// LookupKind queries keywords. Real etcd indexes encode them identically;
	// the mock's typed comparisons need a matching keyword value explicitly.
	ingressSchema := im.GetEntity("dev.miren.ingress/schema.v1alpha")
	require.NotNil(t, ingressSchema)
	ingressSchema.Set(entity.Keyword(entity.SchemaKind, "http_route"))
	im.AddEntity(ingressSchema)
	ver, err := im.Client.Create(t.Context(), "shop-v1", &core_v1alpha.AppVersion{Config: core_v1alpha.Config{
		Services: []core_v1alpha.Services{{Name: "web"}, {Name: "api", Port: 8081, PortType: "http"}},
	}})
	require.NoError(t, err)
	_, err = im.Client.Create(t.Context(), "shop", &core_v1alpha.App{ActiveVersion: ver})
	require.NoError(t, err)
	_, err = im.Client.Create(t.Context(), "login", &ingress_v1alpha.OidcProvider{
		Name: "login", ProviderUrl: "https://login.example", ClientSecret: "never-output-this",
	})
	require.NoError(t, err)
	_, err = im.Client.Create(t.Context(), "password", &ingress_v1alpha.PasswordProvider{
		Name: "password", PasswordHash: "never-output-this-hash",
	})
	require.NoError(t, err)
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"), rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	iface := ingress_v1alpha.AdaptRoutes(routes.NewServer(slog.Default(), ingress.NewClient(slog.Default(), im.EAC.Client)))
	if mode == "old-service" {
		var methods []rpc.Method
		for _, method := range iface.Methods() {
			if method.Index < 2 {
				methods = append(methods, method)
			}
		}
		iface = rpc.NewInterface(methods, nil)
	}
	if mode == "missing-timeout-method" {
		var methods []rpc.Method
		for _, method := range iface.Methods() {
			if method.Name != "timeout" {
				methods = append(methods, method)
			}
		}
		iface = rpc.NewInterface(methods, nil)
	}
	if mode != "missing-service" {
		server.Server().ExposeValue("dev.miren.runtime/routes", iface)
	}
	if mode != "current" {
		server.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(im.Server))
	}

	run := func(command any, args ...string) (string, error) {
		t.Helper()
		// PrintJSON uses process stdout, whereas text uses Context.Stdout.
		capture, err := os.CreateTemp(t.TempDir(), "stdout")
		require.NoError(t, err)
		previous := os.Stdout
		os.Stdout = capture
		defer func() { os.Stdout = previous; require.NoError(t, capture.Close()) }()
		out, commandErr := RunCommand(command, append([]string{"--server-address", server.ListenAddr()}, args...)...)
		_, err = capture.Seek(0, io.SeekStart)
		require.NoError(t, err)
		data, err := io.ReadAll(capture)
		require.NoError(t, err)
		text := out.Stdout.String() + string(data)
		require.NotContains(t, text, "never-output-this")
		return text, commandErr
	}
	ok := func(command any, args ...string) string {
		t.Helper()
		out, err := run(command, args...)
		require.NoError(t, err)
		return out
	}

	require.Contains(t, ok(RouteSet, "shop.example", "shop", "--service", "api"), "Route set: shop.example → shop (api)")
	require.Contains(t, ok(RouteProtect, "shop.example", "--provider", "login", "--claim-header", " email : X-Email ", "--json"), `"provider_type": "oidc"`)
	require.JSONEq(t, `{"route":"shop.example","waf_level":3}`, ok(RouteWaf, "shop.example", "--level", "3", "--json"))
	require.JSONEq(t, `{"route":"shop.example","request_timeout":"2m30s"}`, ok(RouteTimeout, "shop.example", "2m30s", "--json"))
	require.JSONEq(t, `{"route":"shop.example","request_timeout":"2m30s"}`, ok(RouteTimeout, "shop.example", "--json"))
	require.JSONEq(t, `{"route":"shop.example","tls_check":"/allow-tls"}`, ok(RouteTLSCheck, "shop.example", "/allow-tls", "--json"))
	require.JSONEq(t, `{"route":"shop.example","tls_check":"/allow-tls"}`, ok(RouteTLSCheck, "shop.example", "--json"))
	shown := ok(RouteShow, "shop.example", "--json")
	require.JSONEq(t, `{
		"host":"shop.example","app":"shop","service":"api","default":false,
		"protected":true,"protection_type":"oidc","provider_name":"login","provider_url":"https://login.example",
		"claim_mappings":[{"claim":"email","header":"X-Email"}],"waf_level":3,
		"request_timeout":"2m30s","tls_check":"/allow-tls","maintenance":false
	}`, shown)
	require.Contains(t, ok(RouteList, "--json"), `"app": "app/shop"`)
	require.Contains(t, ok(RouteList), "shop.example")
	require.Contains(t, ok(RouteShow, "shop.example"), "https://login.example")

	// A JSON rejection must not replace the existing OIDC protection. Human
	// mode must warn and ignore mappings, including malformed ones, for passwords.
	_, err = run(RouteProtect, "shop.example", "--provider", "password", "--claim-header", "malformed", "--json")
	require.ErrorContains(t, err, "--claim-header is not supported")
	require.Contains(t, ok(RouteShow, "shop.example", "--json"), `"provider_name": "login"`)
	require.Contains(t, ok(RouteProtect, "shop.example", "--provider", "password", "--claim-header", "malformed"), "Warning: --claim-header is ignored")
	_, err = run(RouteProtect, "shop.example", "--provider", "login", "--claim-header", "malformed")
	require.Error(t, err)
	require.Contains(t, ok(RouteShow, "shop.example", "--json"), `"provider_name": "password"`)
	require.Contains(t, ok(RouteUnprotect, "shop.example"), "Protection removed")
	require.Contains(t, ok(RouteUnprotect, "shop.example"), "Route is not protected")
	require.JSONEq(t, `{"route":"shop.example","waf_level":0}`, ok(RouteWaf, "shop.example", "--disable", "--json"))
	require.Contains(t, ok(RouteWaf, "shop.example", "--disable"), "WAF is not enabled")
	require.JSONEq(t, `{"route":"shop.example"}`, ok(RouteTimeout, "shop.example", "--clear", "--json"))
	require.Contains(t, ok(RouteTimeout, "shop.example", "--clear"), "No request timeout override")
	require.JSONEq(t, `{"route":"shop.example"}`, ok(RouteTLSCheck, "shop.example", "--clear", "--json"))
	require.Contains(t, ok(RouteTLSCheck, "shop.example", "--clear"), "No TLS check")

	require.Contains(t, ok(RouteSetDefault, "shop"), "Set default route to: shop")
	require.JSONEq(t, `{"route":"default","request_timeout":"7m"}`, ok(RouteTimeout, "--default", "7m", "--json"))
	require.Contains(t, ok(RouteProtect, "--default", "--provider", "password", "--json"), `"route": "default"`)
	require.JSONEq(t, `{"route":"default","waf_level":4}`, ok(RouteWaf, "--default", "--level", "4", "--json"))
	require.Contains(t, ok(RouteShow, "--default", "--json"), `"default": true`)
	require.Contains(t, ok(RouteUnprotect, "--default"), "Protection removed")
	require.Contains(t, ok(RouteUnsetDefault), "Removed default route from app: shop")
	require.Contains(t, ok(RouteUnsetDefault), "No default route was set")
	require.Contains(t, ok(RouteRemove, "shop.example"), "Route removed: shop.example")
	require.Contains(t, ok(RouteList), "No routes found")
}

func TestRouteCLIDoesNotFallbackOnRPCFailure(t *testing.T) {
	t.Setenv("MIREN_CLUSTER", "")
	t.Setenv("MIREN_CONFIG", filepath.Join(t.TempDir(), "no-config"))
	im, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	_, err := im.Client.Create(t.Context(), "shop", &core_v1alpha.App{})
	require.NoError(t, err)
	ic := ingress.NewClient(slog.Default(), im.EAC.Client)
	_, err = ic.SetRoute(t.Context(), "shop.example", "app/shop")
	require.NoError(t, err)
	for _, failure := range []error{rpc.ErrUnauthorized, cond.NotFound("route", "shop.example")} {
		t.Run(failure.Error(), func(t *testing.T) {
			server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"), rpc.WithAuthenticator(&rpc.NoOpAuthenticator{}))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, server.Close()) })
			iface := ingress_v1alpha.AdaptRoutes(routes.NewServer(slog.Default(), ic))
			methods := iface.Methods()
			for i := range methods {
				methods[i].Handler = func(context.Context, rpc.Call) error { return failure }
			}
			server.Server().ExposeValue("dev.miren.runtime/routes", rpc.NewInterface(methods, nil))
			var entityCalls atomic.Int64
			entityMethods := entityserver_v1alpha.AdaptEntityAccess(im.Server).Methods()
			for i := range entityMethods {
				original := entityMethods[i].Handler
				entityMethods[i].Handler = func(ctx context.Context, call rpc.Call) error {
					entityCalls.Add(1)
					return original(ctx, call)
				}
			}
			server.Server().ExposeValue("entities", rpc.NewInterface(entityMethods, nil))
			for _, command := range []struct {
				fn   any
				args []string
			}{
				{RouteSet, []string{"shop.example", "shop"}},
				{RouteShow, []string{"shop.example"}},
				{RouteList, nil},
				{RouteRemove, []string{"shop.example"}},
				{RouteSetDefault, []string{"shop"}},
				{RouteUnsetDefault, nil},
				{RouteProtect, []string{"shop.example", "--provider", "login"}},
				{RouteUnprotect, []string{"shop.example"}},
				{RouteWaf, []string{"shop.example"}},
				{RouteTimeout, []string{"shop.example", "5m"}},
				{RouteTLSCheck, []string{"shop.example", "/tls"}},
			} {
				_, err := RunCommand(command.fn, append([]string{"--server-address", server.ListenAddr()}, command.args...)...)
				require.Error(t, err)
				require.ErrorContains(t, err, failure.Error())
			}
			require.Zero(t, entityCalls.Load(), "RPC failures must not reach EntityAccess")
		})
	}
}
