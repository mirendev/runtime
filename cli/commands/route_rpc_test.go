package commands

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/routes"
)

// Only Routes is exposed: an accidental EntityAccess or IdentityProviders
// dependency fails on the wire rather than passing through a permissive mock.
func TestRouteCLIWithoutEntityAccess(t *testing.T) {
	t.Setenv("MIREN_CLUSTER", "")
	t.Setenv("MIREN_CONFIG", filepath.Join(t.TempDir(), "no-config"))
	im, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
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
	server.Server().ExposeValue("dev.miren.runtime/routes", ingress_v1alpha.AdaptRoutes(
		routes.NewServer(slog.Default(), ingress.NewClient(slog.Default(), im.EAC.Client))))

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
