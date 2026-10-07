package workloadidentity

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// countingIssuer counts mints so caching can be observed, and records the
// options it was asked for.
type countingIssuer struct {
	mints    int
	err      error
	gotOpts  TokenOptions
	gotLoad  SystemWorkload
	tokenSeq []string
}

func (s *countingIssuer) IssueToken(app, sandboxID string) (string, error) { return "", nil }

func (s *countingIssuer) IssueTokenWithOptions(app, sandboxID string, opts TokenOptions) (string, error) {
	return "", nil
}

func (s *countingIssuer) IssuerURL() string { return "https://issuer.invalid" }

func (s *countingIssuer) IssueSystemWorkloadToken(workload SystemWorkload, opts TokenOptions) (string, error) {
	s.gotLoad, s.gotOpts = workload, opts
	if s.err != nil {
		return "", s.err
	}
	s.mints++
	if len(s.tokenSeq) >= s.mints {
		return s.tokenSeq[s.mints-1], nil
	}
	return "token", nil
}

// Before an issuer arrives the honest answer is a failure. An empty token
// would get the request rejected downstream, which reads as a credential
// problem rather than a startup ordering one.
func TestSystemTokenSourceFailsBeforeIssuerArrives(t *testing.T) {
	src := NewSystemTokenSource(SystemWorkloadTelemetryWriter, "aud")

	_, err := src.Token()
	require.ErrorIs(t, err, ErrIssuerUnavailable)
}

func TestSystemTokenSourceMintsAndCaches(t *testing.T) {
	iss := &countingIssuer{}
	src := NewSystemTokenSource(SystemWorkloadTelemetryWriter, "traces.example.com")
	src.SetIssuer(iss)

	first, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, "token", first)

	second, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, iss.mints, "a live token should be reused rather than reminted per request")

	// The audience is what keeps this token from being spendable at another
	// service, and the explicit TTL is what lets the source know when to renew
	// without decoding the token.
	require.Equal(t, SystemWorkloadTelemetryWriter, iss.gotLoad)
	require.Equal(t, []string{"traces.example.com"}, iss.gotOpts.Audience)
	require.Equal(t, systemTokenTTL, iss.gotOpts.TTL)
}

func TestSystemTokenSourceRenewsBeforeExpiry(t *testing.T) {
	iss := &countingIssuer{tokenSeq: []string{"first", "second"}}
	src := NewSystemTokenSource(SystemWorkloadTelemetryWriter, "aud")
	src.SetIssuer(iss)

	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	src.now = func() time.Time { return now }

	tok, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, "first", tok)

	now = now.Add(systemTokenTTL - systemTokenRefreshLeeway - time.Second)
	tok, err = src.Token()
	require.NoError(t, err)
	require.Equal(t, "first", tok, "still inside the renewal window")

	now = now.Add(2 * time.Second)
	tok, err = src.Token()
	require.NoError(t, err)
	require.Equal(t, "second", tok, "past the renewal point, while the old token is still valid")
}

func TestSystemTokenSourcePropagatesMintFailure(t *testing.T) {
	iss := &countingIssuer{err: errors.New("coordinator refused")}
	src := NewSystemTokenSource(SystemWorkloadTelemetryWriter, "aud")
	src.SetIssuer(iss)

	_, err := src.Token()
	require.Error(t, err)
	require.Contains(t, err.Error(), "coordinator refused")
}

// Re-arming drops the cached token. A new issuer means a new connection, and
// holding a token minted through the old one would keep a stale credential
// alive past the event that replaced it.
func TestSystemTokenSourceResetsOnNewIssuer(t *testing.T) {
	iss := &countingIssuer{tokenSeq: []string{"first", "second"}}
	src := NewSystemTokenSource(SystemWorkloadTelemetryWriter, "aud")

	src.SetIssuer(iss)
	first, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, "first", first)

	src.SetIssuer(iss)
	second, err := src.Token()
	require.NoError(t, err)
	require.Equal(t, "second", second)
	require.Equal(t, 2, iss.mints)
}

type headerRecorder struct {
	got http.Header
}

func (h *headerRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	h.got = r.Header.Clone()
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
}

// A static Authorization already on the request loses to the token, and every
// other header survives. That is the precedence operators get when they set a
// workload identity audience while OTEL_EXPORTER_OTLP_HEADERS still carries a
// basic credential.
func TestBearerTransportReplacesAuthorizationOnly(t *testing.T) {
	src := NewSystemTokenSource(SystemWorkloadTelemetryWriter, "aud")
	src.SetIssuer(&countingIssuer{})
	rec := &headerRecorder{}
	client := &http.Client{Transport: &BearerTransport{Base: rec, Source: src}}

	req, err := http.NewRequest(http.MethodPost, "https://collector.invalid/v1/traces", strings.NewReader("x"))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Basic c3RhdGljOmNyZWQ=")
	req.Header.Set("X-Scope-OrgID", "tenant")

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	require.Equal(t, "Bearer token", rec.got.Get("Authorization"))
	require.Equal(t, "tenant", rec.got.Get("X-Scope-OrgID"))
	require.Equal(t, "Basic c3RhdGljOmNyZWQ=", req.Header.Get("Authorization"), "the caller's request must not be modified")
}

// Without a token the request must not go out at all. Falling back to whatever
// static credential happened to be present would quietly undo the precedence.
func TestBearerTransportRefusesWithoutToken(t *testing.T) {
	rec := &headerRecorder{}
	client := &http.Client{Transport: &BearerTransport{
		Base:   rec,
		Source: NewSystemTokenSource(SystemWorkloadTelemetryWriter, "aud"),
	}}

	req, err := http.NewRequest(http.MethodPost, "https://collector.invalid/v1/traces", strings.NewReader("x"))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Basic c3RhdGljOmNyZWQ=")

	_, err = client.Do(req)
	require.ErrorIs(t, err, ErrIssuerUnavailable)
	require.Nil(t, rec.got, "nothing should reach the wire")
}
