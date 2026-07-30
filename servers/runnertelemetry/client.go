package runnertelemetry

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/quic-go/quic-go/http3"
	"miren.dev/runtime/pkg/workloadidentity"
)

const (
	// tokenTTL is requested explicitly rather than taking the issuer's default,
	// so the runner knows when its own token dies without having to decode it.
	tokenTTL = time.Hour

	// tokenRefreshLeeway renews this far ahead of expiry, leaving room for a
	// slow mint and for clock skew against the coordinator.
	tokenRefreshLeeway = 5 * time.Minute
)

// ErrIssuerUnavailable means telemetry cannot be shipped because no workload
// issuer has been wired up yet, or the coordinator has none.
var ErrIssuerUnavailable = errors.New("workload identity issuer unavailable")

// TokenSource supplies the system workload token a telemetry request carries.
type TokenSource interface {
	Token() (string, error)
}

// MetricsURL and LogsURL are what a runner points its writers at. Each writer
// appends its own backend-native suffix.
func MetricsURL(coordinatorAddress string) string {
	return "https://" + coordinatorAddress + MetricsBasePath
}

func LogsURL(coordinatorAddress string) string {
	return "https://" + coordinatorAddress + LogsBasePath
}

// IssuerTokenSource mints telemetry tokens through a workload issuer, holding
// each one until shortly before it expires.
//
// Its issuer arrives late on purpose. A runner's telemetry writers are built
// before the runner connects to the coordinator, but the issuer is a remote one
// that only exists once that connection is up, so the writers are handed this
// and the issuer is set behind them. Until that happens Token fails rather than
// returning something unusable, which surfaces as a telemetry send failure
// instead of a silent gap.
type IssuerTokenSource struct {
	mu      sync.Mutex
	issuer  workloadidentity.TokenIssuer
	token   string
	renewAt time.Time

	// now is swappable for tests.
	now func() time.Time
}

func NewIssuerTokenSource() *IssuerTokenSource {
	return &IssuerTokenSource{now: time.Now}
}

// SetIssuer supplies the issuer once the runner has one. Passing nil leaves the
// source unarmed, which is the honest state when the coordinator reports it has
// no issuer configured.
func (s *IssuerTokenSource) SetIssuer(issuer workloadidentity.TokenIssuer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issuer = issuer
	s.token = ""
	s.renewAt = time.Time{}
}

func (s *IssuerTokenSource) Token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.issuer == nil {
		return "", ErrIssuerUnavailable
	}

	now := s.now()
	if s.token != "" && now.Before(s.renewAt) {
		return s.token, nil
	}

	token, err := s.issuer.IssueSystemWorkloadToken(
		workloadidentity.SystemWorkloadTelemetryWriter,
		workloadidentity.TokenOptions{
			Audience: []string{Audience},
			TTL:      tokenTTL,
		})
	if err != nil {
		return "", fmt.Errorf("minting telemetry token: %w", err)
	}

	s.token = token
	s.renewAt = now.Add(tokenTTL - tokenRefreshLeeway)

	return token, nil
}

// tokenRoundTripper attaches the workload token to every telemetry request.
//
// Living in the transport rather than at each send is what keeps the writers
// from having to know they are authenticated at all: they build the same
// request they always did and the credential is applied underneath.
type tokenRoundTripper struct {
	base   http.RoundTripper
	source TokenSource
}

func (t *tokenRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	token, err := t.source.Token()
	if err != nil {
		return nil, fmt.Errorf("telemetry request has no workload token: %w", err)
	}

	// A RoundTripper must not modify the request it is given.
	clone := r.Clone(r.Context())
	clone.Header.Set(TokenHeader, token)

	return t.base.RoundTrip(clone)
}

// ClientConfig describes how a runner reaches its coordinator's ingest
// endpoints.
type ClientConfig struct {
	// ClientCertPEM and ClientKeyPEM are the runner's certificate from Join.
	// The listener requires one, so telemetry rides the same mutual TLS as the
	// rest of the runner's traffic and the token narrows what that identity may
	// do rather than replacing it.
	ClientCertPEM []byte
	ClientKeyPEM  []byte

	// CACertPEM verifies the coordinator.
	CACertPEM []byte

	// TokenSource supplies the system workload token.
	TokenSource TokenSource

	// Timeout bounds a single telemetry request.
	Timeout time.Duration
}

// Client is the HTTP client a runner's telemetry writers send through, paired
// with the QUIC transport underneath it.
//
// The transport is held rather than left anonymous so it can be closed. The
// writers only ever need HTTP, but something has to own the QUIC connections,
// and without a handle on them they stay open until the process exits.
type Client struct {
	// HTTP is what the telemetry writers are constructed with.
	HTTP *http.Client

	transport *http3.Transport
}

// Close tears down the underlying QUIC connections.
func (c *Client) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// NewClient builds the client a runner's telemetry writers send through:
// HTTP/3 to the coordinator, authenticated by the runner's certificate, with a
// scoped workload token on every request.
//
// HTTP/3 is not a preference. The coordinator's authenticated listener is QUIC
// only, so a plain net/http client cannot reach it at all.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.TokenSource == nil {
		return nil, errors.New("telemetry client requires a token source")
	}

	cert, err := tls.X509KeyPair(cfg.ClientCertPEM, cfg.ClientKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("loading runner certificate: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cfg.CACertPEM) {
		return nil, errors.New("parsing cluster CA certificate")
	}

	transport := &http3.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
			NextProtos:   []string{http3.NextProtoH3},
		},
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		HTTP: &http.Client{
			Transport: &tokenRoundTripper{base: transport, source: cfg.TokenSource},
			Timeout:   timeout,
		},
		transport: transport,
	}, nil
}
