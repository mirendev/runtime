package httpingress

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/components/activator"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/testutils"
	"miren.dev/runtime/pkg/rpc"
)

// backendActivator hands out leases pointing at a fixed backend URL.
type backendActivator struct {
	stubActivator
	url string
}

func (a backendActivator) AcquireLease(context.Context, *core_v1alpha.AppVersion, string) (*activator.Lease, error) {
	return &activator.Lease{Size: 10, URL: a.url}, nil
}

// tlsCheckBackend stands in for an app's tls_check endpoint: 200 for the
// names it serves, 404 otherwise. It records every ask.
type tlsCheckBackend struct {
	serves       map[string]bool
	broken       bool
	earlyHints   bool
	requireHTTPS bool

	mu   sync.Mutex
	asks []*http.Request
}

func (b *tlsCheckBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.asks = append(b.asks, r.Clone(context.Background()))
	b.mu.Unlock()
	if b.earlyHints {
		w.WriteHeader(http.StatusEarlyHints)
	}
	if b.requireHTTPS && r.Header.Get("X-Forwarded-Proto") != "https" {
		http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusMovedPermanently)
		return
	}
	if b.broken {
		http.Error(w, "booting", http.StatusBadGateway)
		return
	}
	if r.URL.Path == "/tls-check" && b.serves[r.URL.Query().Get("domain")] {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.NotFound(w, r)
}

func (b *tlsCheckBackend) askCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.asks)
}

type tlsCheckFixture struct {
	srv     *Server
	inmem   *testutils.InMemEntityServer
	backend *tlsCheckBackend
	appID   entity.Id
	cvID    entity.Id
}

func newTLSCheckFixture(t *testing.T, serves ...string) *tlsCheckFixture {
	t.Helper()
	ctx := context.Background()

	inmem, cleanup := testutils.NewInMemEntityServer(t)
	t.Cleanup(cleanup)

	appID, err := inmem.Client.Create(ctx, "app", &core_v1alpha.App{})
	require.NoError(t, err)
	cvID, err := inmem.Client.Create(ctx, "cfg", &core_v1alpha.ConfigVersion{
		App: appID,
		Spec: core_v1alpha.ConfigSpec{Services: []core_v1alpha.ConfigSpecServices{
			{Name: "web"},
		}},
	})
	require.NoError(t, err)
	verID, err := inmem.Client.Create(ctx, "ver", &core_v1alpha.AppVersion{App: appID, ConfigVersion: cvID})
	require.NoError(t, err)
	require.NoError(t, inmem.Client.Update(ctx, &core_v1alpha.App{ID: appID, ActiveVersion: verID}))

	backend := &tlsCheckBackend{serves: map[string]bool{}}
	for _, s := range serves {
		backend.serves[s] = true
	}
	hs := httptest.NewServer(backend)
	t.Cleanup(hs.Close)

	rpcClient := rpc.LocalClient(entityserver_v1alpha.AdaptEntityAccess(inmem.Server))
	srv := &Server{
		Log:        slog.Default(),
		eac:        inmem.EAC,
		aa:         backendActivator{url: hs.URL},
		apps:       make(map[string]*appUsage),
		transports: make(map[time.Duration]http.RoundTripper),
	}
	srv.ingressClient = ingress.NewClient(srv.Log, rpcClient)

	return &tlsCheckFixture{srv: srv, inmem: inmem, backend: backend, appID: appID, cvID: cvID}
}

func (f *tlsCheckFixture) route(t *testing.T, host, tlsCheck, authProvider string) {
	t.Helper()
	_, err := f.inmem.Client.Create(context.Background(), host, &ingress_v1alpha.HttpRoute{
		Host:         host,
		App:          f.appID,
		TlsCheck:     tlsCheck,
		AuthProvider: entity.Id(authProvider),
	})
	require.NoError(t, err)
}

func (f *tlsCheckFixture) ephemeral(t *testing.T, label string, expiresAt time.Time) {
	t.Helper()
	_, err := f.inmem.Client.Create(context.Background(), "ver-"+label, &core_v1alpha.AppVersion{
		App:                f.appID,
		ConfigVersion:      f.cvID,
		EphemeralLabel:     label,
		EphemeralExpiresAt: expiresAt,
	})
	require.NoError(t, err)
}

func (f *tlsCheckFixture) allow(t *testing.T, host string) bool {
	t.Helper()
	ok, err := f.srv.AllowCertificate(context.Background(), host)
	require.NoError(t, err)
	return ok
}

func TestAllowCertificate(t *testing.T) {
	t.Run("exact route needs no vouching", func(t *testing.T) {
		f := newTLSCheckFixture(t)
		f.route(t, "app.example.com", "", "")

		require.True(t, f.allow(t, "app.example.com"))
		require.Zero(t, f.backend.askCount())
	})

	t.Run("unrelated host", func(t *testing.T) {
		f := newTLSCheckFixture(t)
		f.route(t, "*.example.com", "/tls-check", "")

		require.False(t, f.allow(t, "other.org"))
		require.Zero(t, f.backend.askCount())
	})

	// The MIR-1919 scanner case: a wildcard route with no check refuses every
	// guess instead of issuing for it.
	t.Run("wildcard without tls_check refuses", func(t *testing.T) {
		f := newTLSCheckFixture(t)
		f.route(t, "*.example.com", "", "")

		require.False(t, f.allow(t, "database.example.com"))
		require.Zero(t, f.backend.askCount())
	})

	t.Run("wildcard asks its tls_check", func(t *testing.T) {
		f := newTLSCheckFixture(t, "mirener.example.com")
		f.route(t, "*.example.com", "/tls-check", "")

		require.True(t, f.allow(t, "mirener.example.com"))
		require.False(t, f.allow(t, "database.example.com"))
		require.Equal(t, 2, f.backend.askCount())

		ask := f.backend.asks[0]
		require.Equal(t, http.MethodGet, ask.Method)
		require.Equal(t, "/tls-check", ask.URL.Path)
		require.Equal(t, "mirener.example.com", ask.URL.Query().Get("domain"))
		require.Equal(t, "mirener.example.com", ask.Host)
	})

	t.Run("live ephemeral under a wildcard needs no ask", func(t *testing.T) {
		f := newTLSCheckFixture(t)
		f.route(t, "*.example.com", "/tls-check", "")
		f.ephemeral(t, "pr-7", time.Time{})

		require.True(t, f.allow(t, "pr-7.example.com"))
		require.Zero(t, f.backend.askCount())
	})

	// The second MIR-1919 hole: scanners prepending "www." to real routes.
	t.Run("ephemeral subdomain of a normal route", func(t *testing.T) {
		f := newTLSCheckFixture(t)
		f.route(t, "app.example.com", "", "")
		f.ephemeral(t, "pr-33", time.Time{})
		f.ephemeral(t, "pr-old", time.Now().Add(-time.Hour))

		require.True(t, f.allow(t, "pr-33.app.example.com"))
		require.False(t, f.allow(t, "www.app.example.com"))
		require.False(t, f.allow(t, "pr-old.app.example.com"), "an expired ephemeral must not vouch")
		require.Zero(t, f.backend.askCount())
	})

	t.Run("normal route can vouch for its own subdomains", func(t *testing.T) {
		f := newTLSCheckFixture(t, "tenant.app.example.com")
		f.route(t, "app.example.com", "/tls-check", "")

		require.True(t, f.allow(t, "tenant.app.example.com"))
		require.False(t, f.allow(t, "www.app.example.com"))
	})

	// A 5xx isn't a refusal. The app may still be booting from zero, and a
	// remembered "no" would leave its first visitors on the fallback cert.
	t.Run("an app with no clear answer is undecided", func(t *testing.T) {
		f := newTLSCheckFixture(t, "mirener.example.com")
		f.route(t, "*.example.com", "/tls-check", "")
		f.backend.broken = true

		ok, err := f.srv.AllowCertificate(context.Background(), "mirener.example.com")
		require.Error(t, err)
		require.False(t, ok)
	})

	// Plenty of apps redirect plain HTTP. The ask has to arrive looking like
	// the HTTPS request it is vouching for, or it gets a redirect, not a verdict.
	t.Run("ask presents as https", func(t *testing.T) {
		f := newTLSCheckFixture(t, "mirener.example.com")
		f.route(t, "*.example.com", "/tls-check", "")
		f.backend.requireHTTPS = true

		require.True(t, f.allow(t, "mirener.example.com"))
	})

	t.Run("early hints don't mask the verdict", func(t *testing.T) {
		f := newTLSCheckFixture(t, "mirener.example.com")
		f.route(t, "*.example.com", "/tls-check", "")
		f.backend.earlyHints = true

		require.True(t, f.allow(t, "mirener.example.com"))
	})

	// The ask comes from the platform, not a visitor, so the route's auth must
	// not stand in the way. The provider here doesn't exist; had auth run, it
	// would have answered 503.
	t.Run("ask bypasses route auth", func(t *testing.T) {
		f := newTLSCheckFixture(t, "mirener.example.com")
		f.route(t, "*.example.com", "/tls-check", "oidc_provider/does-not-exist")

		require.True(t, f.allow(t, "mirener.example.com"))
	})
}
