package metricspush

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/workloadidentity"
)

type recordingPusher struct {
	pushes []Push
	err    error
}

func (p *recordingPusher) Push(_ context.Context, push Push) error {
	p.pushes = append(p.pushes, push)
	return p.err
}

func (p *recordingPusher) Available(context.Context) bool { return true }

type countingIssuer struct {
	minted  int
	gotApp  string
	gotOpts workloadidentity.TokenOptions
}

func (i *countingIssuer) IssueTokenWithOptions(app, _ string, opts workloadidentity.TokenOptions) (string, error) {
	i.minted++
	i.gotApp, i.gotOpts = app, opts
	return "minted-" + string(rune('0'+i.minted)), nil
}

// httptest requests come from 192.0.2.1; only that address with the right
// secret is the sandbox.
func testAuth(remoteHost, secret string) (string, string, bool) {
	if remoteHost == "192.0.2.1" && secret == "s3cret" {
		return "sandbox/bg-1", "cloud", true
	}
	return "", "", false
}

type relayFixture struct {
	mux    *http.ServeMux
	relay  *Relay
	pusher *recordingPusher
	issuer *countingIssuer
}

func newRelayFixture() *relayFixture {
	f := &relayFixture{mux: http.NewServeMux(), pusher: &recordingPusher{}, issuer: &countingIssuer{}}
	f.relay = NewRelay(testLogger(), testAuth, f.issuer, f.pusher)
	f.relay.Register(f.mux)
	return f
}

func (f *relayFixture) do(method, path, body string, setup func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer s3cret")
	if setup != nil {
		setup(req)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}

func TestRelayPushgateway(t *testing.T) {
	f := newRelayFixture()

	rec := f.do(http.MethodPut, "/v1/metrics/app/metrics/job/bgtask/queue/mail", "depth 3\n", func(r *http.Request) {
		r.Header.Set("Content-Type", "text/plain; version=0.0.4")
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, f.pusher.pushes, 1)
	p := f.pusher.pushes[0]
	require.Equal(t, ScopeApp, p.Scope)
	require.Equal(t, FormatPrometheus, p.Format)
	require.Equal(t, map[string]string{"job": "bgtask", "queue": "mail"}, p.Grouping)
	require.Equal(t, "depth 3\n", string(p.Body))
	require.Equal(t, "minted-1", p.Token)

	// The token is minted for this endpoint's audience, not the default one.
	require.Equal(t, []string{Audience}, f.issuer.gotOpts.Audience)
	require.Equal(t, "cloud", f.issuer.gotApp)
}

func TestRelayOTLP(t *testing.T) {
	f := newRelayFixture()
	rec := f.do(http.MethodPost, "/v1/metrics/sandbox/otlp/v1/metrics", "proto-bytes", func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-protobuf")
		r.Header.Set("Content-Encoding", "gzip")
	})
	require.Equal(t, http.StatusOK, rec.Code)
	p := f.pusher.pushes[0]
	require.Equal(t, FormatOTLP, p.Format)
	require.Equal(t, ScopeSandbox, p.Scope)
	require.Equal(t, "gzip", p.ContentEncoding)
}

func TestRelayAuthentication(t *testing.T) {
	path := "/v1/metrics/sandbox/metrics/job/w"

	t.Run("no credential", func(t *testing.T) {
		f := newRelayFixture()
		rec := f.do(http.MethodPost, path, "up 1\n", func(r *http.Request) { r.Header.Del("Authorization") })
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Empty(t, f.pusher.pushes)
	})
	t.Run("wrong secret", func(t *testing.T) {
		f := newRelayFixture()
		rec := f.do(http.MethodPost, path, "up 1\n", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") })
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Empty(t, f.pusher.pushes)
		require.Zero(t, f.issuer.minted)
	})
	t.Run("right secret from another address", func(t *testing.T) {
		f := newRelayFixture()
		rec := f.do(http.MethodPost, path, "up 1\n", func(r *http.Request) { r.RemoteAddr = "192.0.2.9:4000" })
		require.Equal(t, http.StatusForbidden, rec.Code)
	})
	t.Run("basic auth password", func(t *testing.T) {
		f := newRelayFixture()
		rec := f.do(http.MethodPost, path, "up 1\n", func(r *http.Request) {
			r.Header.Del("Authorization")
			r.SetBasicAuth("anything", "s3cret")
		})
		require.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestRelayCachesTokens(t *testing.T) {
	f := newRelayFixture()
	now := time.Unix(1_700_000_000, 0)
	f.relay.now = func() time.Time { return now }

	push := func() { f.do(http.MethodPost, "/v1/metrics/sandbox/metrics/job/w", "up 1\n", nil) }

	push()
	push()
	require.Equal(t, 1, f.issuer.minted, "a live token is reused")

	now = now.Add(relayTokenTTL - relayTokenLeeway)
	push()
	require.Equal(t, 2, f.issuer.minted, "a token near expiry is renewed")

	// A 401 from the coordinator means the token is no good any more.
	f.pusher.err = &Error{Status: http.StatusUnauthorized, Message: "invalid workload token"}
	push()
	f.pusher.err = nil
	push()
	require.Equal(t, 3, f.issuer.minted, "a rejected token is dropped")
}

func TestRelayRateLimitsPerSandbox(t *testing.T) {
	f := newRelayFixture()
	now := time.Unix(1_700_000_000, 0)
	f.relay.now = func() time.Time { return now }
	push := func() int {
		return f.do(http.MethodPost, "/v1/metrics/sandbox/metrics/job/w", "up 1\n", nil).Code
	}

	for range relayPushBurst {
		require.Equal(t, http.StatusOK, push())
	}
	rec := f.do(http.MethodPost, "/v1/metrics/sandbox/metrics/job/w", "up 1\n", nil)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "1", rec.Header().Get("Retry-After"))
	require.Len(t, f.pusher.pushes, relayPushBurst, "a limited push must not reach the coordinator")

	now = now.Add(time.Second)
	require.Equal(t, http.StatusOK, push(), "the bucket refills")
}

func TestRelayPassesCoordinatorVerdict(t *testing.T) {
	f := newRelayFixture()
	f.pusher.err = &Error{Status: http.StatusBadRequest, Message: `label "miren_app" is reserved`}
	rec := f.do(http.MethodPost, "/v1/metrics/sandbox/metrics/job/w", "up 1\n", nil)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `label "miren_app" is reserved`)
}

func TestRelayUnknownScope(t *testing.T) {
	f := newRelayFixture()
	rec := f.do(http.MethodPost, "/v1/metrics/cluster/metrics/job/w", "up 1\n", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Empty(t, f.pusher.pushes)
}

func TestRelayDeleteIsAccepted(t *testing.T) {
	f := newRelayFixture()
	rec := f.do(http.MethodDelete, "/v1/metrics/sandbox/metrics/job/w", "", nil)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Empty(t, f.pusher.pushes)
}

func TestParseGroupingKey(t *testing.T) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte("a/b"))

	cases := []struct {
		path string
		want map[string]string
		err  bool
	}{
		{path: "job/w", want: map[string]string{"job": "w"}},
		{path: "job/w/instance/i1", want: map[string]string{"job": "w", "instance": "i1"}},
		{path: "job@base64/" + encoded, want: map[string]string{"job": "a/b"}},
		{path: "job/w/path@base64/" + encoded + "==", want: map[string]string{"job": "w", "path": "a/b"}},
		{path: "job", err: true},
		{path: "instance/i1", err: true},
		{path: "job/w/instance", err: true},
		{path: "job/w/job/x", err: true},
		{path: "job@base64/!!", err: true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, err := parseGroupingKey(tc.path)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
