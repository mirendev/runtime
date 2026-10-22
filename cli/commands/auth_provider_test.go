package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/identityproviders"
)

type providerCLIIdentity struct{}

func (providerCLIIdentity) Authenticate(context.Context, *rpc.Credentials) (*rpc.Identity, error) {
	return &rpc.Identity{Method: rpc.AuthMethodJWT, Subject: "operator"}, nil
}

type providerCLIPolicy struct{ deny bool }

func (p providerCLIPolicy) Authorize(_ context.Context, _ *rpc.Identity, resource, _ string) error {
	if resource == "identityproviders" && p.deny {
		return fmt.Errorf("provider management denied")
	}
	return nil
}

func providerCLIContext(t *testing.T, deny bool, legacy ...bool) (*Context, *ingress.Client) {
	t.Helper()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ic := ingress.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server)))
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("localhost:0"),
		rpc.WithAuthenticator(providerCLIIdentity{}), rpc.WithAuthorizer(providerCLIPolicy{deny: deny}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	// Current servers deliberately offer no EntityAccess unless testing denial.
	if len(legacy) > 0 && legacy[0] {
		server.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
		if len(legacy) > 1 && legacy[1] {
			server.Server().ExposeValue("dev.miren.runtime/identity-providers", rpc.NewInterface([]rpc.Method{
				{Name: "oldMethod", InterfaceName: "IdentityProviders"},
			}, nil))
		}
	} else {
		server.Server().ExposeValue("dev.miren.runtime/identity-providers", ingress_v1alpha.AdaptIdentityProviders(identityproviders.NewServer(ic)))
		if deny {
			// An available legacy capability must never bypass RPC denial.
			server.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
		}
	}
	ctx := &Context{Context: t.Context(), Log: slog.Default(), Stderr: io.Discard}
	ctx.Config.ServerAddress = server.ListenAddr()
	return ctx, ic
}

// Exercise command flag parsing and its public handler with the same output
// destination for text and PrintJSON, without loading machine-local config.
func runProviderCLI(t *testing.T, ctx *Context, command any, args ...string) (string, error) {
	t.Helper()
	cmd := Infer("auth provider", "test", command)
	if err := cmd.fs.Parse(args); err != nil {
		return "", err
	}
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)
	defer out.Close()
	previous, previousCtx := os.Stdout, ctx.Stdout
	os.Stdout, ctx.Stdout = out, out
	defer func() { os.Stdout, ctx.Stdout = previous, previousCtx }()
	result := reflect.ValueOf(command).Call([]reflect.Value{reflect.ValueOf(ctx), cmd.opts.Elem()})
	data, err := os.ReadFile(out.Name())
	require.NoError(t, err)
	if !result[0].IsNil() {
		return string(data), result[0].Interface().(error)
	}
	return string(data), nil
}

func TestAuthProviderCLIWithoutEntityAccess(t *testing.T) {
	t.Run("current", func(t *testing.T) { testAuthProviderCLI(t, false) })
	t.Run("legacy", func(t *testing.T) { testAuthProviderCLI(t, true) })
	t.Run("missing-methods", func(t *testing.T) { testAuthProviderCLI(t, true, true) })
}

func testAuthProviderCLI(t *testing.T, legacy ...bool) {
	ctx, ic := providerCLIContext(t, false, legacy...)
	out, err := runProviderCLI(t, ctx, AuthProviderList)
	require.NoError(t, err)
	require.Equal(t, "No identity providers found\n", out)
	out, err = runProviderCLI(t, ctx, AuthProviderList, "--format", "json")
	require.NoError(t, err)
	require.JSONEq(t, "[]", out)

	out, err = runProviderCLI(t, ctx, AuthProviderAddOIDC, "login", "--provider-url", "https://issuer.example", "--client-id", "oauth-client", "--client-secret", "oauth-secret", "--scope", "email")
	require.NoError(t, err)
	require.Contains(t, out, "openid email")
	require.NotContains(t, out, "oauth-secret")
	out, err = runProviderCLI(t, ctx, AuthProviderShow, "login", "--json")
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"login","type":"oidc","provider_url":"https://issuer.example","client_id":"oauth-client","scopes":["openid","email"]}`, out)
	_, err = runProviderCLI(t, ctx, AuthProviderAddOIDC, "login", "--provider-url", "https://issuer.example", "--client-id", "oauth-client", "--client-secret", "rotated-secret")
	require.ErrorContains(t, err, "--update")
	_, err = runProviderCLI(t, ctx, AuthProviderAddOIDC, "login", "--provider-url", "https://issuer.example", "--client-id", "oauth-client", "--client-secret", "rotated-secret", "--update")
	require.NoError(t, err)
	oidc, err := ic.GetOIDCProvider(ctx, "login")
	require.NoError(t, err)
	require.Equal(t, "rotated-secret", oidc.ClientSecret)

	out, err = runProviderCLI(t, ctx, AuthProviderAddGitHub, "github", "--client-id", "github-client", "--client-secret", "github-secret", "--org", "mirendev:platform,eng", "--org", "other")
	require.NoError(t, err)
	require.Contains(t, out, "mirendev:platform,eng, other")
	require.NotContains(t, out, "github-secret")
	out, err = runProviderCLI(t, ctx, AuthProviderShow, "github", "--format", "json")
	require.NoError(t, err)
	var shown struct {
		Name, Type string
		ClientID   string `json:"client_id"`
		ConfigJSON string `json:"config_json"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &shown))
	require.Equal(t, "github", shown.Name)
	require.Equal(t, "github", shown.Type)
	require.Equal(t, "github-client", shown.ClientID)
	require.JSONEq(t, `{"orgs":[{"Name":"mirendev","Teams":["platform","eng"]},{"Name":"other","Teams":null}]}`, shown.ConfigJSON)
	out, err = runProviderCLI(t, ctx, AuthProviderShow, "github")
	require.NoError(t, err)
	require.Contains(t, out, "orgs: mirendev[platform,eng], other")

	passwordFile := filepath.Join(t.TempDir(), "password")
	require.NoError(t, os.WriteFile(passwordFile, []byte("file-credential\r\n"), 0600))
	out, err = runProviderCLI(t, ctx, AuthProviderAddPassword, "shared", "--password", "@"+passwordFile)
	require.NoError(t, err)
	require.Contains(t, out, "password")
	require.NotContains(t, out, "file-credential")
	pw, err := ic.GetPasswordProvider(ctx, "shared")
	require.NoError(t, err)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(pw.PasswordHash), []byte("file-credential")))
	_, err = runProviderCLI(t, ctx, AuthProviderAddPassword, "shared", "--password", "new-credential", "--update")
	require.NoError(t, err)
	_, err = runProviderCLI(t, ctx, AuthProviderAddGitHub, "shared", "--client-id", "id", "--client-secret", "secret", "--update")
	require.ErrorContains(t, err, "unique across types")
	out, err = runProviderCLI(t, ctx, AuthProviderShow, "shared", "--json")
	require.NoError(t, err)
	require.JSONEq(t, `{"name":"shared","type":"password"}`, out)
	out, err = runProviderCLI(t, ctx, AuthProviderList, "--json")
	require.NoError(t, err)
	var listed []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	require.ElementsMatch(t, []map[string]any{
		{"name": "login", "type": "oidc", "provider_url": "https://issuer.example", "client_id": "oauth-client", "scopes": []any{"openid"}},
		{"name": "github", "type": "github", "client_id": "github-client", "config_json": `{"orgs":[{"Name":"mirendev","Teams":["platform","eng"]},{"Name":"other","Teams":null}]}`},
		{"name": "shared", "type": "password"},
	}, listed)
	for _, secret := range []string{"oauth-secret", "rotated-secret", "github-secret", "file-credential", "new-credential", "password_hash"} {
		require.NotContains(t, out, secret)
	}

	_, err = ic.GetEntityStore().CreateOrUpdate(ctx, "protected", &ingress_v1alpha.HttpRoute{Default: true, AuthProvider: oidc.ID})
	require.NoError(t, err)
	_, err = runProviderCLI(t, ctx, AuthProviderRemove, "login")
	require.ErrorContains(t, err, "(default)")
	require.ErrorContains(t, err, "miren route unprotect")
	require.ErrorContains(t, err, "--force")
	out, err = runProviderCLI(t, ctx, AuthProviderRemove, "login", "--force")
	require.NoError(t, err)
	require.Equal(t, "Removed oidc provider: login\n", out)
	out, err = runProviderCLI(t, ctx, AuthProviderRemove, "shared")
	require.NoError(t, err)
	require.Equal(t, "Removed password provider: shared\n", out)
	_, err = runProviderCLI(t, ctx, AuthProviderShow, "shared")
	require.ErrorContains(t, err, "not found")
}

func TestAuthProviderCLIDenialAndValidation(t *testing.T) {
	ctx, _ := providerCLIContext(t, true)
	for _, tc := range []struct {
		command any
		args    []string
	}{
		{AuthProviderAddOIDC, []string{"oidc", "--provider-url", "https://issuer.example", "--client-id", "id", "--client-secret", "secret"}},
		{AuthProviderAddGitHub, []string{"github", "--client-id", "id", "--client-secret", "secret"}},
		{AuthProviderAddPassword, []string{"shared", "--password", "credential"}},
		{AuthProviderList, nil},
		{AuthProviderShow, []string{"shared"}},
		{AuthProviderRemove, []string{"shared", "--force"}},
	} {
		out, err := runProviderCLI(t, ctx, tc.command, tc.args...)
		require.ErrorContains(t, err, "403")
		require.Empty(t, out)
	}
	for _, org := range []string{" ", "mirendev:", "mirendev: , "} {
		_, err := runProviderCLI(t, ctx, AuthProviderAddGitHub, "github", "--client-id", "id", "--client-secret", "secret", "--org", org)
		require.ErrorContains(t, err, "invalid --org")
	}
	empty := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(empty, []byte("\r\n"), 0600))
	_, err := runProviderCLI(t, ctx, AuthProviderAddPassword, "shared", "--password", "@"+empty)
	require.ErrorContains(t, err, "password file is empty")
}
