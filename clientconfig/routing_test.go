package clientconfig

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/slogrus"
)

type routingCredentialRecorder struct {
	bearers atomic.Int64
}

func (r *routingCredentialRecorder) Authenticate(_ context.Context, creds *rpc.Credentials) (*rpc.Identity, error) {
	if creds.BearerToken() != "" {
		r.bearers.Add(1)
	}
	return &rpc.Identity{Subject: "routing-test"}, nil
}

func TestAutomaticClusterRouting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ca := testAuthority(t)
	cert, err := ca.IssueCertificate(caauth.Options{
		CommonName: "api.miren", DNSNames: []string{"api.miren"}, ValidFor: time.Hour,
	})
	require.NoError(t, err)
	recorder := &routingCredentialRecorder{}
	server, err := rpc.NewState(t.Context(), rpc.WithBindAddr("127.0.0.1:0"), rpc.WithAuthenticator(recorder),
		rpc.WithCertPEMs(cert.CertPEM, cert.KeyPEM))
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/clusters/cluster-abc/rpc", r.URL.Path)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(cloud.Close)
	cluster := &ClusterConfig{
		XID: "cluster-abc", Identity: "cloud", CloudURL: cloud.URL, Insecure: true,
		AllAddresses: []string{"invalid::::", server.ListenAddr()}, CACertFingerprint: ca.Fingerprint(),
	}
	config := viaCloudConfig(t, cluster)
	var diagnostics bytes.Buffer
	ctx := slogrus.WithLogger(t.Context(), slog.New(slog.NewJSONHandler(&diagnostics, &slog.HandlerOptions{Level: slog.LevelDebug})))
	connect := func() error {
		state, err := cluster.State(ctx, config)
		require.NoError(t, err)
		defer state.Close()
		_, err = state.Client("missing")
		return err
	}
	// Resolving a missing capability on the real QUIC server yields a lookup error.
	// The fake cloud instead yields a 503, distinguishing the chosen route.
	assertDirect := func() {
		err := connect()
		var resolve *rpc.ResolveError
		require.True(t, errors.As(err, &resolve), "%v", err)
		require.Equal(t, rpc.ResolveLookupError, resolve.Kind, "%v", err)
		require.ErrorContains(t, err, "unknown object: missing")
	}
	assertDirect()
	require.Positive(t, recorder.bearers.Load(), "the direct peer must observe credentials on an accepted route")
	require.Empty(t, cluster.CACert, "discovered trust is per connection, not a config mutation")

	cluster.CACertFingerprint = "wrong-pin"
	diagnostics.Reset()
	require.ErrorContains(t, connect(), "503", "a mismatched CA must fall back to cloud")
	require.Contains(t, diagnostics.String(), server.ListenAddr())
	require.Contains(t, diagnostics.String(), "cluster CA fingerprint mismatch")
	diagnostics.Reset()
	require.ErrorContains(t, connect(), "503")
	require.Contains(t, diagnostics.String(), "Direct cluster route failed")
	require.Contains(t, diagnostics.String(), server.ListenAddr())
	require.Contains(t, diagnostics.String(), "cluster CA fingerprint mismatch")
	cluster.CACertFingerprint = ca.Fingerprint()
	assertDirect()

	cluster.TLSServerName = "other-api.miren"
	require.ErrorContains(t, connect(), "503", "pinned-root recovery must respect an explicit server name")
	cluster.TLSServerName = ""
	assertDirect()

	// Existing direct entries use their saved CA and explicit TLS server name.
	cluster.CACert = string(ca.GetCACertificate())
	cluster.CACertFingerprint = ""
	cluster.TLSServerName = APIServerName
	assertDirect()
	cluster.CACert = ""
	cluster.CACertFingerprint = ca.Fingerprint()
	cluster.TLSServerName = ""

	runnerCert, err := ca.IssueCertificate(caauth.Options{
		CommonName: "runner", DNSNames: []string{"runner.miren"}, ValidFor: time.Hour,
	})
	require.NoError(t, err)
	runnerRecorder := &routingCredentialRecorder{}
	runner, err := rpc.NewState(t.Context(), rpc.WithBindAddr("127.0.0.1:0"),
		rpc.WithCertPEMs(runnerCert.CertPEM, runnerCert.KeyPEM), rpc.WithAuthenticator(runnerRecorder))
	require.NoError(t, err)
	t.Cleanup(func() { _ = runner.Close() })
	cluster.AllAddresses = []string{runner.ListenAddr()}
	require.ErrorContains(t, connect(), "503", "a trusted CA alone does not make an unrelated SAN an API identity")
	require.Zero(t, runnerRecorder.bearers.Load(), "an unrelated same-CA peer must not receive the bearer token")
	cluster.CACertFingerprint = "wrong-pin"
	require.ErrorContains(t, connect(), "503")
	require.Zero(t, runnerRecorder.bearers.Load(), "a mismatched CA must not receive the bearer token")
	cluster.CACertFingerprint = ca.Fingerprint()

	expiredCert, err := ca.IssueCertificate(caauth.Options{
		CommonName: APIServerName, DNSNames: []string{APIServerName}, ValidFor: -time.Hour,
	})
	require.NoError(t, err)
	expiredRecorder := &routingCredentialRecorder{}
	expiredServer, err := rpc.NewState(t.Context(), rpc.WithBindAddr("127.0.0.1:0"),
		rpc.WithCertPEMs(expiredCert.CertPEM, expiredCert.KeyPEM), rpc.WithAuthenticator(expiredRecorder))
	require.NoError(t, err)
	t.Cleanup(func() { _ = expiredServer.Close() })
	cluster.AllAddresses = []string{expiredServer.ListenAddr()}
	diagnostics.Reset()
	require.ErrorContains(t, connect(), "503", "a matching pin and hostname must not bypass certificate expiry")
	require.Contains(t, diagnostics.String(), "expired")
	require.Zero(t, expiredRecorder.bearers.Load(), "an expired API certificate must not receive the bearer token")

	// Simulate adding while remote, then returning to the direct network.
	cluster.AllAddresses = []string{"invalid::::"}
	require.ErrorContains(t, connect(), "503")
	cluster.AllAddresses = []string{server.ListenAddr()}
	assertDirect()
	// Returning to the previous config reuses only its short-lived cloud hint.
	cluster.AllAddresses = []string{"invalid::::"}
	require.ErrorContains(t, connect(), "503")
	cluster.AllAddresses = []string{server.ListenAddr()}
	cluster.ViaCloud = true
	require.ErrorContains(t, connect(), "503", "explicit cloud routing overrides a working direct route")
	cluster.ViaCloud = false

	t.Run("legacy cloud entry remains forced", func(t *testing.T) {
		original := cluster
		defer func() { cluster = original }()
		// Old automatic fallbacks had no addresses, CA, fingerprint, or flag
		// distinguishing them from an explicit cloud-routing request.
		cluster = &ClusterConfig{}
		require.NoError(t, yaml.Unmarshal([]byte(fmt.Sprintf(`hostname: ""
via_cloud: true
xid: cluster-abc
identity: cloud
cloud_url: %q
insecure: true
`, cloud.URL)), cluster))
		require.True(t, cluster.ViaCloud)
		require.Empty(t, cluster.CACertFingerprint)
		require.ErrorContains(t, connect(), "503")
	})

	// A failed relay is never cached: recovery is noticed immediately.
	address := server.ListenAddr()
	require.NoError(t, server.Close())
	require.ErrorContains(t, connect(), "503")
	server, err = rpc.NewState(t.Context(), rpc.WithBindAddr(address), rpc.WithCertPEMs(cert.CertPEM, cert.KeyPEM))
	require.NoError(t, err)
	assertDirect()

	canceledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = cluster.State(canceledCtx, config)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAutomaticClusterRoutingCancelsDNS(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	previous := net.DefaultResolver
	defer func() { net.DefaultResolver = previous }()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	var active atomic.Int64
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		active.Add(1)
		defer active.Add(-1)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return nil, fmt.Errorf("DNS test shut down")
		}
	}}
	cluster := &ClusterConfig{Hostname: "unresolvable.invalid:8443", XID: "cluster-abc", Identity: "cloud"}
	config := viaCloudConfig(t, cluster)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		state, err := cluster.State(ctx, config)
		if state != nil {
			defer state.Close()
		}
		if err == nil {
			_, err = state.Client("missing")
		}
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("direct routing did not start DNS resolution")
	}
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled route selection did not return")
	}
	require.Eventually(t, func() bool { return active.Load() == 0 }, time.Second, time.Millisecond,
		"cancellation must stop the outstanding DNS work, not just abandon its result")
}

func TestSuccessfulClusterRoutes(t *testing.T) {
	for _, savedCA := range []bool{false, true} {
		t.Run(fmt.Sprintf("saved_CA_%v", savedCA), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			var handshakes, calls, cloudCalls atomic.Int64
			tracer := rpc.DefaultQUICConfig.Tracer
			rpc.DefaultQUICConfig.Tracer = func(_ context.Context, isClient bool, _ quic.ConnectionID) qlogwriter.Trace {
				if !isClient {
					handshakes.Add(1)
				}
				return nil
			}
			defer func() { rpc.DefaultQUICConfig.Tracer = tracer }()
			ca := testAuthority(t)
			cert, err := ca.IssueCertificate(caauth.Options{CommonName: APIServerName, DNSNames: []string{APIServerName}, ValidFor: time.Hour})
			require.NoError(t, err)
			iface := rpc.NewInterface([]rpc.Method{{Name: "Fail", InterfaceName: "routing", Public: true,
				Handler: func(context.Context, rpc.Call) error {
					calls.Add(1)
					return errors.New("application failed after writing")
				},
			}}, nil)
			start := func(address string) *rpc.State {
				server, err := rpc.NewState(t.Context(), rpc.WithBindAddr(address), rpc.WithCertPEMs(cert.CertPEM, cert.KeyPEM))
				require.NoError(t, err)
				t.Cleanup(func() { _ = server.Close() })
				server.Server().ExposeValue("routing", iface)
				return server
			}
			server := start("127.0.0.1:0")
			relay, err := rpc.NewState(t.Context(), rpc.WithBindAddr("127.0.0.1:0"), rpc.WithWSBindAddr("127.0.0.1:0"), rpc.WithCertPEMs(cert.CertPEM, cert.KeyPEM))
			require.NoError(t, err)
			t.Cleanup(func() { _ = relay.Close() })
			relay.Server().ExposeValue("routing", iface)
			target, err := url.Parse("https://" + relay.WSListenAddr())
			require.NoError(t, err)
			roots := x509.NewCertPool()
			require.True(t, roots.AppendCertsFromPEM(ca.GetCACertificate()))
			proxy := httputil.NewSingleHostReverseProxy(target)
			proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: APIServerName}}
			var cloudDown atomic.Bool
			cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cloudCalls.Add(1)
				if cloudDown.Load() {
					http.Error(w, "no link to the cluster", http.StatusServiceUnavailable)
					return
				}
				r.URL.Path = "/_rpc/message"
				proxy.ServeHTTP(w, r)
			}))
			t.Cleanup(cloud.Close)
			cluster := &ClusterConfig{XID: "cluster-cache", Identity: "cloud", CloudURL: cloud.URL,
				AllAddresses: []string{server.ListenAddr()}, CACertFingerprint: ca.Fingerprint(), Insecure: true}
			if savedCA {
				cluster.Hostname, cluster.CACert, cluster.TLSServerName = server.ListenAddr(), string(ca.GetCACertificate()), APIServerName
			}
			connect := func() *rpc.NetworkClient {
				// Recreate Config too: the successful route must survive separate CLI invocations.
				state, err := cluster.State(t.Context(), viaCloudConfig(t, cluster))
				require.NoError(t, err)
				t.Cleanup(func() { _ = state.Close() })
				client, err := state.Client("routing")
				require.NoError(t, err)
				return client
			}
			first := connect()
			initial := int64(3)
			if savedCA {
				initial = 1
			}
			require.Equal(t, initial, handshakes.Load(), "saved trust needs no probe; fingerprint recovery is paid once")
			connect()
			connect()
			require.Equal(t, initial+2, handshakes.Load(), "each steady-state RPC client needs only its own handshake")
			require.ErrorContains(t, first.Call(t.Context(), "Fail", nil, nil), "application failed after writing")
			require.EqualValues(t, 1, calls.Load(), "routing must never replay an application call")
			require.Zero(t, cloudCalls.Load())
			missing, err := cluster.State(t.Context(), viaCloudConfig(t, cluster))
			require.NoError(t, err)
			t.Cleanup(func() { _ = missing.Close() })
			_, err = missing.Client("missing")
			var lookup *rpc.ResolveError
			require.ErrorAs(t, err, &lookup)
			require.Equal(t, rpc.ResolveLookupError, lookup.Kind)
			require.Equal(t, initial+3, handshakes.Load(), "a lookup rejection must not rediscover a working route")
			require.Zero(t, cloudCalls.Load())
			unauthenticated := viaCloudConfig(t, cluster)
			identity, err := unauthenticated.GetIdentity("cloud")
			require.NoError(t, err)
			identity.Token = ""
			state, err := cluster.State(t.Context(), unauthenticated)
			if state != nil {
				t.Cleanup(func() { _ = state.Close() })
			}
			require.ErrorContains(t, err, "failed to authenticate with cloud")
			connect()
			require.Equal(t, initial+4, handshakes.Load(), "credential failures must preserve the cached direct route and recovered CA")
			require.Zero(t, cloudCalls.Load())
			if savedCA {
				return
			}

			address := server.ListenAddr()
			require.NoError(t, server.Close())
			connect() // The cached direct route fails; rediscovery falls back to the working relay.
			require.EqualValues(t, 1, cloudCalls.Load())
			cacheDir, err := getCacheDir()
			require.NoError(t, err)
			paths, err := filepath.Glob(filepath.Join(cacheDir, "cluster_route_*.json"))
			require.NoError(t, err)
			require.Len(t, paths, 1)
			firstCloud, err := os.Stat(paths[0])
			require.NoError(t, err)
			server = start(address)
			before := handshakes.Load()
			connect()
			require.EqualValues(t, 2, cloudCalls.Load(), "a working cloud route is reused until expiry")
			require.Equal(t, before, handshakes.Load(), "a cached cloud route must not probe")
			info, err := os.Stat(paths[0])
			require.NoError(t, err)
			require.Equal(t, firstCloud.ModTime(), info.ModTime(), "using a cloud route must not postpone rediscovery")
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
			old := time.Now().Add(-time.Minute)
			require.NoError(t, os.Chtimes(paths[0], old, old))
			connect()
			require.EqualValues(t, 2, cloudCalls.Load(), "cloud expiry notices the direct network again")
			require.NoError(t, server.Close())
			connect()
			require.EqualValues(t, 3, cloudCalls.Load())
			start(address)
			cloudDown.Store(true)
			connect()
			require.EqualValues(t, 4, cloudCalls.Load(), "a failed cached cloud route triggers direct rediscovery without waiting for expiry")
		})
	}
}
