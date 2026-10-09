package otlpexport

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/workloadidentity"
)

func TestParseHeaders(t *testing.T) {
	got := ParseHeaders(" Authorization=Basic%20abc%3D%3D , X-Tenant=acme,malformed,=novalue, X-Raw=100%zz ")
	require.Equal(t, map[string]string{
		"Authorization": "Basic abc==",
		"X-Tenant":      "acme",
		"X-Raw":         "100%zz",
	}, got)
	require.Empty(t, ParseHeaders(""))
}

func TestResolveMergesSignalHeaders(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-Tenant=generic,X-Only-Generic=1")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "X-Tenant=traces")

	d := Resolve(slog.Default(), "https://collector.example", nil)
	require.Equal(t, map[string]string{"X-Tenant": "traces", "X-Only-Generic": "1"}, d.Headers)
}

// With identity configured, a static Authorization left in the env is dropped
// up front and everything else is kept. Without identity it is the credential.
func TestResolveDropsStaticAuthorizationOnlyWithIdentity(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Basic%20abc,X-Tenant=acme")

	static := Resolve(slog.Default(), "https://collector.example", nil)
	require.Equal(t, "Basic abc", static.Headers["authorization"])

	src := workloadidentity.NewSystemTokenSource(workloadidentity.SystemWorkloadTelemetryWriter, "aud")
	withIdentity := Resolve(slog.Default(), "https://collector.example", src)
	require.Equal(t, map[string]string{"X-Tenant": "acme"}, withIdentity.Headers)
}

func TestTracesURL(t *testing.T) {
	require.Equal(t, "https://c.example/v1/traces", Destination{Endpoint: "https://c.example"}.TracesURL())
	require.Equal(t, "https://c.example/otlp/v1/traces", Destination{Endpoint: "https://c.example/otlp/"}.TracesURL())
}

type collector struct {
	server   *httptest.Server
	requests []*http.Request
	bodies   [][]byte
	status   int
}

func newCollector(t *testing.T) *collector {
	c := &collector{status: http.StatusOK}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.requests = append(c.requests, r)
		c.bodies = append(c.bodies, body)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(c.status)
		_, _ = w.Write([]byte("collector-response"))
	}))
	t.Cleanup(c.server.Close)
	return c
}

func testIssuer(t *testing.T) *workloadidentity.Issuer {
	t.Helper()
	iss, err := workloadidentity.NewIssuer(workloadidentity.IssuerConfig{
		DataPath:  t.TempDir(),
		IssuerURL: "https://cluster.example",
	})
	require.NoError(t, err)
	return iss
}

// The whole point, end to end: what reaches the collector is a telemetry
// writer token for the collector's audience, which a verifier holding only the
// cluster's public keys accepts, and never the static credential.
func TestForwardAuthenticatesWithWorkloadIdentity(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Basic%20c3RhdGlj,X-Tenant=acme")
	c := newCollector(t)
	iss := testIssuer(t)

	src := workloadidentity.NewSystemTokenSource(workloadidentity.SystemWorkloadTelemetryWriter, "traces.example")
	src.SetIssuer(iss)
	d := Resolve(slog.Default(), c.server.URL, src)

	status, err := d.Forward(t.Context(), d.Client(), []byte("spans"), "application/x-protobuf", "gzip")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	require.Len(t, c.requests, 1)
	req := c.requests[0]
	require.Equal(t, "/v1/traces", req.URL.Path)
	require.Equal(t, "acme", req.Header.Get("X-Tenant"))
	require.Equal(t, "gzip", req.Header.Get("Content-Encoding"))
	require.Equal(t, []byte("spans"), c.bodies[0])

	bearer, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	require.True(t, ok, "expected a bearer, got %q", req.Header.Get("Authorization"))
	claims, err := iss.VerifySystemWorkloadToken(bearer, "traces.example", workloadidentity.SystemWorkloadTelemetryWriter)
	require.NoError(t, err)
	require.NotNil(t, claims)
}

func TestForwardWithStaticHeaders(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Basic%20c3RhdGlj")
	c := newCollector(t)
	d := Resolve(slog.Default(), c.server.URL, nil)

	_, err := d.Forward(t.Context(), d.Client(), []byte("spans"), "", "")
	require.NoError(t, err)
	require.Equal(t, "Basic c3RhdGlj", c.requests[0].Header.Get("Authorization"))
	require.Equal(t, "application/x-protobuf", c.requests[0].Header.Get("Content-Type"))
}

// buildkitd posts to the relay with no credential and gets the collector's
// answer back verbatim, so its exporter's own retry logic still sees real
// statuses.
func TestRelayForwardsWithTokenAndPassesStatusBack(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
	c := newCollector(t)
	c.status = http.StatusTooManyRequests
	iss := testIssuer(t)

	src := workloadidentity.NewSystemTokenSource(workloadidentity.SystemWorkloadTelemetryWriter, "traces.example")
	src.SetIssuer(iss)
	relay := httptest.NewServer(NewRelay(slog.Default(), Resolve(slog.Default(), c.server.URL, src), testRelaySecret))
	t.Cleanup(relay.Close)

	req, err := http.NewRequest(http.MethodPost, relay.URL+"/v1/traces", bytes.NewReader([]byte("build spans")))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer "+testRelaySecret)
	req.Header.Set("X-Scope-OrgID", "someone-else")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.Equal(t, "collector-response", string(body))

	require.Len(t, c.requests, 1)
	require.Equal(t, "/v1/traces", c.requests[0].URL.Path)
	require.Equal(t, []byte("build spans"), c.bodies[0])
	require.Equal(t, "application/x-protobuf", c.requests[0].Header.Get("Content-Type"))
	require.Equal(t, "gzip", c.requests[0].Header.Get("Content-Encoding"))
	require.Empty(t, c.requests[0].Header.Get("X-Scope-OrgID"), "a local caller must not choose where spans land")
	bearer, ok := strings.CutPrefix(c.requests[0].Header.Get("Authorization"), "Bearer ")
	require.True(t, ok)
	_, err = iss.VerifySystemWorkloadToken(bearer, "traces.example", workloadidentity.SystemWorkloadTelemetryWriter)
	require.NoError(t, err)
}

func TestRelayRejectsOtherPaths(t *testing.T) {
	c := newCollector(t)
	relay := httptest.NewServer(NewRelay(slog.Default(), Destination{Endpoint: c.server.URL}, testRelaySecret))
	t.Cleanup(relay.Close)

	resp, err := http.Post(relay.URL+"/v1/metrics", "application/x-protobuf", strings.NewReader("m"))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, err = http.Get(relay.URL + "/v1/traces")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	require.Empty(t, c.requests)
}

const testRelaySecret = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

func postRelay(base, authorization string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, base+"/v1/traces", strings.NewReader("spans"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return http.DefaultClient.Do(req)
}

// Loopback is open to every process on the host, so only a caller holding the
// relay secret gets to export as the cluster's telemetry writer.
func TestRelayRequiresSecret(t *testing.T) {
	c := newCollector(t)
	relay := httptest.NewServer(NewRelay(slog.Default(), Destination{Endpoint: c.server.URL}, testRelaySecret))
	t.Cleanup(relay.Close)

	for _, auth := range []string{"", "Bearer wrong", "Basic " + testRelaySecret, testRelaySecret} {
		resp, err := postRelay(relay.URL, auth)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "Authorization %q", auth)
	}
	require.Empty(t, c.requests)

	resp, err := postRelay(relay.URL, "Bearer "+testRelaySecret)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, c.requests, 1)
	require.Empty(t, c.requests[0].Header.Get("Authorization"), "the relay secret must not reach the collector")
}

// With no secret to check against, the relay takes nothing.
func TestRelayWithoutSecretFailsClosed(t *testing.T) {
	c := newCollector(t)
	relay := httptest.NewServer(NewRelay(slog.Default(), Destination{Endpoint: c.server.URL}, ""))
	t.Cleanup(relay.Close)

	for _, auth := range []string{"", "Bearer "} {
		resp, err := postRelay(relay.URL, auth)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	}
	require.Empty(t, c.requests)
}

func TestLoadRelaySecretIsStableAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "otlp-relay", "secret")

	first, err := LoadRelaySecret(path)
	require.NoError(t, err)
	require.True(t, validRelaySecret(first))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	again, err := LoadRelaySecret(path)
	require.NoError(t, err)
	require.Equal(t, first, again, "buildkitd's env, and so its container, must survive a restart")
}

// A file the relay can't use, such as one cut short by a crash or carrying
// characters that would split the OTel headers list, is replaced.
func TestLoadRelaySecretReplacesUnusableFile(t *testing.T) {
	for _, content := range []string{"", "SHORT", "abcdefghijklmnopqrstuvwxyz0123", "ABCDEFGHIJKLMNOPQRSTUVWXYZ,X=1"} {
		path := filepath.Join(t.TempDir(), "secret")
		require.NoError(t, os.WriteFile(path, []byte(content), 0600))

		secret, err := LoadRelaySecret(path)
		require.NoError(t, err)
		require.True(t, validRelaySecret(secret))
		require.NotEqual(t, content, secret)
	}
}

// A relay that cannot get a token must not reach the collector at all.
func TestRelayWithoutIssuerFailsClosed(t *testing.T) {
	c := newCollector(t)
	src := workloadidentity.NewSystemTokenSource(workloadidentity.SystemWorkloadTelemetryWriter, "traces.example")
	relay := httptest.NewServer(NewRelay(slog.Default(), Destination{Endpoint: c.server.URL, Token: src}, testRelaySecret))
	t.Cleanup(relay.Close)

	resp, err := postRelay(relay.URL, "Bearer "+testRelaySecret)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	require.Empty(t, c.requests)
}

// Credentials live in the transport, so following a redirect to another host
// would hand them to it. The client stops at the redirect instead.
func TestClientDoesNotFollowCrossHostRedirects(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-Api-Key=secret")
	elsewhere := newCollector(t)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.server.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirector.Close)

	d := Resolve(slog.Default(), redirector.URL, nil)
	status, err := d.Forward(t.Context(), d.Client(), []byte("spans"), "", "")
	require.NoError(t, err)
	require.Equal(t, http.StatusTemporaryRedirect, status)
	require.Empty(t, elsewhere.requests, "nothing, and so no credential, should reach the other host")
}

func TestClientFollowsSameHostRedirects(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-Api-Key=secret")
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/traces" {
			http.Redirect(w, r, "/moved/v1/traces", http.StatusPermanentRedirect)
			return
		}
		gotKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := Resolve(slog.Default(), srv.URL, nil)
	status, err := d.Forward(t.Context(), d.Client(), []byte("spans"), "", "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "secret", gotKey)
}
