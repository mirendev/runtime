package identityproviders

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/oidcauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/servers/routes"
)

func newTestServer(t *testing.T) (*ingress_v1alpha.IdentityProvidersClient, *ingress.Client) {
	t.Helper()
	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)
	ic := ingress.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server)))
	return ingress_v1alpha.NewIdentityProvidersClient(rpc.LocalClient(ingress_v1alpha.AdaptIdentityProviders(NewServer(ic)))), ic
}

func operatorContext() context.Context {
	return rpc.ContextWithIdentity(context.Background(), &rpc.Identity{Method: rpc.AuthMethodCert, Subject: "operator"})
}

func TestProviderLifecycle(t *testing.T) {
	c, ic := newTestServer(t)
	ctx := operatorContext()
	empty, err := c.List(ctx)
	require.NoError(t, err)
	require.Empty(t, empty.Providers())

	_, err = c.AddOIDC(ctx, "login", "https://issuer.example", "client", "oauth-secret-one", []string{"email"}, false)
	require.NoError(t, err)
	p, err := ic.GetOIDCProvider(ctx, "login")
	require.NoError(t, err)
	id := p.ID
	require.Equal(t, "oauth-secret-one", p.ClientSecret)
	shown, err := c.Show(ctx, "login")
	require.NoError(t, err)
	require.Equal(t, "oidc", shown.Provider().Kind())
	require.Equal(t, "https://issuer.example", shown.Provider().ProviderUrl())
	require.Equal(t, []string{"openid", "email"}, shown.Provider().Scopes())

	_, err = c.AddOIDC(ctx, "login", "https://issuer.example", "new-client", "oauth-secret-two", nil, false)
	require.ErrorContains(t, err, "update is required")
	_, err = c.AddOIDC(ctx, "login", "https://issuer.example", "new-client", "oauth-secret-two", []string{"profile", "openid"}, true)
	require.NoError(t, err)
	p, err = ic.GetOIDCProvider(ctx, "login")
	require.NoError(t, err)
	require.Equal(t, id, p.ID)
	require.Equal(t, "oauth-secret-two", p.ClientSecret)
	require.Equal(t, "profile openid", p.Scopes)

	org := &ingress_v1alpha.GitHubOrganization{}
	org.SetName("mirendev")
	org.SetTeams([]string{"platform", "engineering"})
	_, err = c.AddGitHub(ctx, "github", "github-client", "github-secret", []*ingress_v1alpha.GitHubOrganization{org}, false)
	require.NoError(t, err)
	gh, err := c.Show(ctx, "github")
	require.NoError(t, err)
	require.Equal(t, "github", gh.Provider().Kind())
	require.Equal(t, "github-client", gh.Provider().ClientId())
	require.Len(t, gh.Provider().Orgs(), 1)
	require.Equal(t, "mirendev", gh.Provider().Orgs()[0].Name())
	require.Equal(t, []string{"platform", "engineering"}, gh.Provider().Orgs()[0].Teams())

	_, err = c.AddPassword(ctx, "shared", "first-password", false)
	require.NoError(t, err)
	pw, err := ic.GetPasswordProvider(ctx, "shared")
	require.NoError(t, err)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(pw.PasswordHash), []byte("first-password")))
	_, err = c.AddPassword(ctx, "shared", "next-password", false)
	require.ErrorContains(t, err, "update is required")
	_, err = c.AddPassword(ctx, "shared", "next-password", true)
	require.NoError(t, err)
	updated, err := ic.GetPasswordProvider(ctx, "shared")
	require.NoError(t, err)
	require.Equal(t, pw.ID, updated.ID)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("next-password")))
	require.Error(t, bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("first-password")))

	// Legacy arbitrary config may contain credentials; only typed metadata
	// may cross the new boundary, not config_json from the entity.
	_, err = ic.CreateOrUpdateOIDCProvider(ctx, &ingress_v1alpha.OidcProvider{
		Name: "legacy", ConnectorType: "github", ClientSecret: "legacy-oauth-secret",
		ConfigJson: `{"orgs":[{"name":"legacy-org","teams":["ops"]}],"client_secret":"embedded-secret"}`,
	})
	require.NoError(t, err)
	listed, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed.Providers(), 4)
	for _, info := range listed.Providers() {
		read, err := c.Show(ctx, info.Name())
		require.NoError(t, err)
		for _, dto := range []*ingress_v1alpha.ProviderInfo{info, read.Provider()} {
			data, err := json.Marshal(dto)
			require.NoError(t, err)
			for _, forbidden := range []string{"secret", "password_hash", "config_json", "first-password", "next-password", updated.PasswordHash} {
				require.NotContains(t, string(data), forbidden)
			}
		}
	}
	_, err = c.Remove(ctx, "login", false)
	require.NoError(t, err)
	_, err = c.Show(ctx, "login")
	require.ErrorContains(t, err, "not found")
	_, err = c.Remove(ctx, "missing", false)
	require.ErrorContains(t, err, "not found")
}

func TestProviderCollisionsAndConnectorReplacement(t *testing.T) {
	c, ic := newTestServer(t)
	ctx := operatorContext()
	_, err := c.AddPassword(ctx, "password", "credential", false)
	require.NoError(t, err)
	_, err = c.AddOIDC(ctx, "oauth", "https://issuer.example", "client", "secret", nil, false)
	require.NoError(t, err)
	for _, update := range []bool{false, true} {
		_, err = c.AddOIDC(ctx, "password", "https://issuer.example", "client", "secret", nil, update)
		require.ErrorContains(t, err, "unique across types")
		_, err = c.AddGitHub(ctx, "password", "client", "secret", nil, update)
		require.ErrorContains(t, err, "unique across types")
		_, err = c.AddPassword(ctx, "oauth", "credential", update)
		require.ErrorContains(t, err, "unique across types")
	}
	before, err := ic.GetOIDCProvider(ctx, "oauth")
	require.NoError(t, err)
	_, err = c.AddGitHub(ctx, "oauth", "github-client", "github-secret", nil, false)
	require.ErrorContains(t, err, "update is required")
	_, err = c.AddGitHub(ctx, "oauth", "github-client", "github-secret", nil, true)
	require.NoError(t, err)
	show, err := c.Show(ctx, "oauth")
	require.NoError(t, err)
	require.Equal(t, "github", show.Provider().Kind())
	after, err := ic.GetOIDCProvider(ctx, "oauth")
	require.NoError(t, err)
	require.Equal(t, before.ID, after.ID)
	_, err = c.AddOIDC(ctx, "oauth", "https://other.example", "new-client", "new-secret", nil, true)
	require.NoError(t, err)
	show, err = c.Show(ctx, "oauth")
	require.NoError(t, err)
	require.Equal(t, "oidc", show.Provider().Kind())
}

func TestProviderRemovalChecksAllRouteReferences(t *testing.T) {
	for _, kind := range []string{"oidc", "github", "password"} {
		for _, defaultRoute := range []bool{false, true} {
			t.Run(kind+"/default="+map[bool]string{false: "false", true: "true"}[defaultRoute], func(t *testing.T) {
				c, ic := newTestServer(t)
				ctx := operatorContext()
				var id entity.Id
				if kind == "password" {
					_, err := c.AddPassword(ctx, "provider", "credential", false)
					require.NoError(t, err)
					p, err := ic.GetPasswordProvider(ctx, "provider")
					require.NoError(t, err)
					id = p.ID
				} else {
					_, err := ic.CreateOrUpdateOIDCProvider(ctx, &ingress_v1alpha.OidcProvider{Name: "provider", ConnectorType: kind})
					require.NoError(t, err)
					p, err := ic.GetOIDCProvider(ctx, "provider")
					require.NoError(t, err)
					id = p.ID
				}
				// An orphan app must not hide a named or default route's reference.
				route := &ingress_v1alpha.HttpRoute{Host: "example.com", Default: defaultRoute, App: "app/missing", AuthProvider: id}
				routeID, err := ic.GetEntityStore().CreateOrUpdate(ctx, "referencing-route", route)
				require.NoError(t, err)
				_, err = c.Remove(ctx, "provider", false)
				require.ErrorContains(t, err, "attached to 1 route(s)")
				_, err = c.Show(ctx, "provider")
				require.NoError(t, err)
				_, err = c.Remove(ctx, "provider", true)
				require.NoError(t, err)
				_, err = c.Show(ctx, "provider")
				require.ErrorContains(t, err, "not found")
				var stored ingress_v1alpha.HttpRoute
				require.NoError(t, ic.GetEntityStore().GetById(ctx, routeID, &stored))
				require.Equal(t, id, stored.AuthProvider, "force removal must not mutate routes")
			})
		}
	}
}

func TestProviderManagementRejectsNonOperators(t *testing.T) {
	c, _ := newTestServer(t)
	_, err := c.AddPassword(operatorContext(), "existing", "credential", false)
	require.NoError(t, err)
	for _, method := range []rpc.AuthMethod{rpc.AuthMethodOIDC, rpc.AuthMethodWorkload, rpc.AuthMethodSystem, rpc.AuthMethodSigned, rpc.AuthMethodAnonymous, rpc.AuthMethodToken, "unknown"} {
		for _, app := range []string{"", "own-app", "other-app"} {
			ctx := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{Method: method, Metadata: map[string]any{
				"app": app, "bound_app": app, "role": "cluster-admin",
			}})
			_, err := c.AddOIDC(ctx, "new", "https://issuer.example", "client", "secret", nil, false)
			require.ErrorContains(t, err, "requires a cluster operator")
			_, err = c.AddGitHub(ctx, "new", "client", "secret", nil, false)
			require.ErrorContains(t, err, "requires a cluster operator")
			_, err = c.AddPassword(ctx, "new", "credential", false)
			require.ErrorContains(t, err, "requires a cluster operator")
			_, err = c.List(ctx)
			require.ErrorContains(t, err, "requires a cluster operator")
			_, err = c.Show(ctx, "existing")
			require.ErrorContains(t, err, "requires a cluster operator")
			_, err = c.Remove(ctx, "existing", true)
			require.ErrorContains(t, err, "requires a cluster operator")
		}
	}
	_, err = c.List(context.Background())
	require.ErrorContains(t, err, "requires a cluster operator")
	jwt := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{Method: rpc.AuthMethodJWT})
	listed, err := c.List(jwt)
	require.NoError(t, err)
	require.Len(t, listed.Providers(), 1)
}

func TestProviderManagementRejectsRunnerCertificates(t *testing.T) {
	c, ic := newTestServer(t)
	operator := operatorContext()
	_, err := c.AddOIDC(operator, "oauth", "https://issuer.example", "client", "secret", nil, false)
	require.NoError(t, err)
	_, err = c.AddPassword(operator, "password", "credential", false)
	require.NoError(t, err)
	oauth, err := ic.GetOIDCProvider(operator, "oauth")
	require.NoError(t, err)
	password, err := ic.GetPasswordProvider(operator, "password")
	require.NoError(t, err)
	for _, subject := range []string{"runner-node1", "miren-runner"} {
		t.Run(subject, func(t *testing.T) {
			runner := rpc.ContextWithIdentity(context.Background(), &rpc.Identity{Method: rpc.AuthMethodCert, Subject: subject})
			for _, update := range []bool{false, true} {
				name, passwordName := "new", "new-password"
				if update {
					name, passwordName = "oauth", "password"
				}
				_, err := c.AddOIDC(runner, name, "https://other.example", "other", "replacement", nil, update)
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
				_, err = c.AddGitHub(runner, name, "other", "replacement", nil, update)
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
				_, err = c.AddPassword(runner, passwordName, "replacement", update)
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
			}
			_, err := c.List(runner)
			require.ErrorIs(t, err, rpc.ErrUnauthorized)
			for _, name := range []string{"oauth", "password"} {
				_, err = c.Show(runner, name)
				require.ErrorIs(t, err, rpc.ErrUnauthorized)
				for _, force := range []bool{false, true} {
					_, err = c.Remove(runner, name, force)
					require.ErrorIs(t, err, rpc.ErrUnauthorized)
				}
			}
		})
	}
	listed, err := c.List(operator)
	require.NoError(t, err)
	require.Len(t, listed.Providers(), 2)
	storedOAuth, err := ic.GetOIDCProvider(operator, "oauth")
	require.NoError(t, err)
	require.Equal(t, oauth, storedOAuth)
	storedPassword, err := ic.GetPasswordProvider(operator, "password")
	require.NoError(t, err)
	require.Equal(t, password, storedPassword)
}

func TestProviderValidation(t *testing.T) {
	c, _ := newTestServer(t)
	ctx := operatorContext()
	_, err := c.AddOIDC(ctx, "", "https://issuer.example", "client", "secret", nil, false)
	require.ErrorContains(t, err, "name is required")
	_, err = c.AddOIDC(ctx, "oidc", "", "client", "secret", nil, false)
	require.Error(t, err)
	_, err = c.AddGitHub(ctx, "github", "client", "", nil, false)
	require.Error(t, err)
	org := &ingress_v1alpha.GitHubOrganization{}
	_, err = c.AddGitHub(ctx, "github", "client", "secret", []*ingress_v1alpha.GitHubOrganization{org}, false)
	require.ErrorContains(t, err, "organization name is required")
	org.SetName("mirendev")
	org.SetTeams([]string{" "})
	_, err = c.AddGitHub(ctx, "github", "client", "secret", []*ingress_v1alpha.GitHubOrganization{org}, false)
	require.ErrorContains(t, err, "team name cannot be empty")
	_, err = c.AddPassword(ctx, "password", "", false)
	require.ErrorContains(t, err, "password cannot be empty")
	_, err = c.AddPassword(ctx, "password", strings.Repeat("x", 73), false)
	require.Error(t, err)
	listed, err := c.List(ctx)
	require.NoError(t, err)
	require.Empty(t, listed.Providers())
}

func TestConcurrentCrossKindAdds(t *testing.T) {
	c, _ := newTestServer(t)
	ctx := operatorContext()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Go(func() { _, err := c.AddPassword(ctx, "same", "credential", false); errs <- err })
	wg.Go(func() { _, err := c.AddGitHub(ctx, "same", "client", "secret", nil, false); errs <- err })
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else {
			require.ErrorContains(t, err, "unique across types")
		}
	}
	require.Equal(t, 1, successes)
	listed, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed.Providers(), 1)
}

type operatorAuthenticator struct{}

func (operatorAuthenticator) Authenticate(context.Context, *rpc.Credentials) (*rpc.Identity, error) {
	return &rpc.Identity{Method: rpc.AuthMethodJWT, Subject: "operator"}, nil
}

type providerPolicy struct{ deny bool }

func (p providerPolicy) Authorize(_ context.Context, _ *rpc.Identity, resource, _ string) error {
	if resource != "identityproviders" || p.deny {
		return fmt.Errorf("policy denies access")
	}
	return nil
}

func TestProviderRPCWithoutEntityAccess(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(fmt.Sprintf("deny=%t", deny), func(t *testing.T) {
			ctx := t.Context()
			inmem, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			ic := ingress.NewClient(slog.Default(), rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server)))
			ss, err := rpc.NewState(ctx, rpc.WithSkipVerify,
				rpc.WithAuthenticator(operatorAuthenticator{}),
				rpc.WithAuthorizer(oidcauth.NewCompositeAuthorizer(providerPolicy{deny: deny})))
			require.NoError(t, err)
			ss.Server().ExposeValue("dev.miren.runtime/identity-providers", ingress_v1alpha.AdaptIdentityProviders(NewServer(ic)))
			ss.Server().ExposeValue("entities", entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
			cs, err := rpc.NewState(ctx, rpc.WithSkipVerify)
			require.NoError(t, err)
			conn, err := cs.Connect(ss.ListenAddr(), "dev.miren.runtime/identity-providers")
			require.NoError(t, err)
			c := ingress_v1alpha.NewIdentityProvidersClient(conn)
			_, err = c.AddPassword(ctx, "shared", "network-credential", false)
			if deny {
				var denial cond.ErrRemote
				require.ErrorAs(t, err, &denial)
				require.Equal(t, "permission", denial.Category)
				require.Equal(t, "forbidden", denial.Code)
				return
			}
			require.NoError(t, err)
			_, err = c.AddGitHub(ctx, "invalid", "client", "secret", []*ingress_v1alpha.GitHubOrganization{nil}, false)
			require.ErrorContains(t, err, "organization is required")
			_, err = c.Show(ctx, "invalid")
			require.ErrorContains(t, err, "not found")
			listed, err := c.List(ctx)
			require.NoError(t, err)
			require.Len(t, listed.Providers(), 1)
			shown, err := c.Show(ctx, "shared")
			require.NoError(t, err)
			require.Equal(t, "password", shown.Provider().Kind())
			entities, err := cs.Connect(ss.ListenAddr(), "entities")
			require.NoError(t, err)
			_, err = entityserver_v1alpha.NewEntityAccessClient(entities).Get(ctx, "password_provider/shared")
			var denial cond.ErrRemote
			require.ErrorAs(t, err, &denial)
			require.Equal(t, "permission", denial.Category)
			require.Equal(t, "forbidden", denial.Code)
			_, err = c.Remove(ctx, "shared", false)
			require.NoError(t, err)
		})
	}
}

type providerReferenceBarrier struct {
	rpc.Client
	method           string
	entered, release chan struct{}
	once             sync.Once
}

func (b *providerReferenceBarrier) Call(ctx context.Context, method string, args, result any) error {
	if method == b.method {
		b.once.Do(func() { close(b.entered) })
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return b.Client.Call(ctx, method, args, result)
}

func TestProviderRemovalRacesRouteProtection(t *testing.T) {
	for _, tc := range []struct{ kind, first string }{
		{"oidc", "protect"}, {"oidc", "remove"},
		{"password", "protect"}, {"password", "remove"},
	} {
		t.Run(tc.kind+"/"+tc.first+" first", func(t *testing.T) {
			im, cleanup := testutils.NewInMemEntityServer(t)
			t.Cleanup(cleanup)
			ctx, cancel := context.WithTimeout(operatorContext(), 10*time.Second)
			t.Cleanup(cancel)
			app, err := im.Client.Create(ctx, "site", &core_v1alpha.App{})
			require.NoError(t, err)
			var id entity.Id
			if tc.kind == "password" {
				id, err = im.Client.Create(ctx, "login", &ingress_v1alpha.PasswordProvider{Name: "login"})
			} else {
				id, err = im.Client.Create(ctx, "login", &ingress_v1alpha.OidcProvider{Name: "login"})
			}
			require.NoError(t, err)
			routeID, err := im.Client.Create(ctx, "site.example", &ingress_v1alpha.HttpRoute{Host: "site.example", App: app})
			require.NoError(t, err)
			barrier := &providerReferenceBarrier{
				Client: rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(im.Server)),
				method: "replace", entered: make(chan struct{}), release: make(chan struct{}),
			}
			if tc.first == "remove" {
				barrier.method = "delete"
			}
			release := sync.OnceFunc(func() { close(barrier.release) })
			t.Cleanup(release)
			// Match coordinator wiring: both public APIs share one ingress client.
			ic := ingress.NewClient(slog.Default(), barrier)
			providers := ingress_v1alpha.NewIdentityProvidersClient(rpc.LocalClient(ingress_v1alpha.AdaptIdentityProviders(NewServer(ic))))
			routes := ingress_v1alpha.NewRoutesClient(rpc.LocalClient(ingress_v1alpha.AdaptRoutes(routes.NewServer(slog.Default(), ic))))
			protect := func() error { _, err := routes.Protect(ctx, "site.example", false, "login", nil, false); return err }
			remove := func() error { _, err := providers.Remove(ctx, "login", false); return err }
			firstCall, secondCall := protect, remove
			if tc.first == "remove" {
				firstCall, secondCall = remove, protect
			}
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() { firstDone <- firstCall() }()
			select {
			case <-barrier.entered:
			case <-ctx.Done():
				t.Fatal("first operation did not reach its commit: ", ctx.Err())
			}
			// Let the competing operation attempt its lookup while the first
			// is paused after resolution/reference checks, before its write.
			go func() { secondDone <- secondCall() }()
			select {
			case err := <-secondDone:
				secondDone <- err
			case <-time.After(100 * time.Millisecond):
			}
			release()
			require.NoError(t, <-firstDone)
			err = <-secondDone
			var route ingress_v1alpha.HttpRoute
			require.NoError(t, im.Client.GetById(ctx, routeID, &route))
			if tc.first == "protect" {
				require.ErrorContains(t, err, "attached to 1 route(s)")
				require.Equal(t, id, route.AuthProvider)
				_, err = providers.Show(ctx, "login")
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "provider not found")
				require.Empty(t, route.AuthProvider)
				_, err = providers.Show(ctx, "login")
				require.ErrorContains(t, err, "not found")
			}
		})
	}
}
