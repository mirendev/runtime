package runnertelemetry

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/quic-go/quic-go/http3"
	"miren.dev/runtime/pkg/workloadidentity"
)

// MetricsURL and LogsURL are what a runner points its writers at. Each writer
// appends its own backend-native suffix.
func MetricsURL(coordinatorAddress string) string {
	return "https://" + coordinatorAddress + MetricsBasePath
}

func LogsURL(coordinatorAddress string) string {
	return "https://" + coordinatorAddress + LogsBasePath
}

// NewTokenSource returns the source a runner's telemetry client mints through:
// telemetry writer tokens scoped to the coordinator's ingest. Its issuer is set
// once the runner has connected.
func NewTokenSource() *workloadidentity.SystemTokenSource {
	return workloadidentity.NewSystemTokenSource(workloadidentity.SystemWorkloadTelemetryWriter, Audience)
}

// tokenRoundTripper attaches the workload token to every telemetry request.
//
// Living in the transport rather than at each send is what keeps the writers
// from having to know they are authenticated at all: they build the same
// request they always did and the credential is applied underneath.
type tokenRoundTripper struct {
	base   http.RoundTripper
	source workloadidentity.TokenSource
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
	TokenSource workloadidentity.TokenSource

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

// Operational returns a client for the runner's operational metrics writer.
// It shares this client's transport and credential and marks every request
// with StreamOperational, so the coordinator can tell those batches from the
// per-sandbox series sent through HTTP.
func (c *Client) Operational() *http.Client {
	return &http.Client{
		Transport: &streamRoundTripper{base: c.HTTP.Transport, stream: StreamOperational},
		Timeout:   c.HTTP.Timeout,
	}
}

type streamRoundTripper struct {
	base   http.RoundTripper
	stream string
}

func (t *streamRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set(StreamHeader, t.stream)
	return t.base.RoundTrip(clone)
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
