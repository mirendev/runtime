// Package otlpexport describes where the runtime ships its own traces and how
// it authenticates there, for every path that sends them: the server's own
// SDK exporter, the relay that forwards CLI spans, and the loopback relay that
// buildkitd exports through.
package otlpexport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"miren.dev/runtime/pkg/workloadidentity"
)

// DefaultTimeout bounds one export. It matches the OTel SDK's own default,
// which stops applying once the exporter is handed a custom client.
const DefaultTimeout = 10 * time.Second

// Destination is an OTLP/HTTP traces collector and the credentials for it.
type Destination struct {
	// Endpoint is the OTLP/HTTP base URL; TracesURL appends the signal path.
	Endpoint string

	// Headers are static headers from OTEL_EXPORTER_OTLP_HEADERS and
	// OTEL_EXPORTER_OTLP_TRACES_HEADERS, attached to every export.
	Headers map[string]string

	// Token, when set, authenticates every export with a workload identity
	// bearer that replaces any static Authorization.
	Token workloadidentity.TokenSource
}

// Resolve builds the destination for endpoint from the standard OTel header
// env vars and an optional token source.
//
// When a token source is set, a static Authorization header is dropped here
// rather than silently overwritten per request, so the operator hears about it
// once at startup. That is also the migration path: set an audience while the
// old header is still in the unit, confirm the collector sees tokens, then
// remove the header.
func Resolve(log *slog.Logger, endpoint string, token workloadidentity.TokenSource) Destination {
	headers := ParseHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"))
	maps.Copy(headers, ParseHeaders(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS")))

	if token != nil {
		for k := range headers {
			if http.CanonicalHeaderKey(k) == "Authorization" {
				log.Warn("ignoring the static Authorization header in OTEL_EXPORTER_OTLP_HEADERS; " +
					"traces authenticate with workload identity because telemetry.traces.workload_identity_audience is set")
				delete(headers, k)
			}
		}
	}

	return Destination{Endpoint: endpoint, Headers: headers, Token: token}
}

// TracesURL is where OTLP/HTTP traces are posted for this destination.
func (d Destination) TracesURL() string {
	return strings.TrimRight(d.Endpoint, "/") + "/v1/traces"
}

// Transport wraps base so every request carries the destination's static
// headers and, when configured, its workload identity bearer.
func (d Destination) Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	var rt http.RoundTripper = &headerTransport{base: base, headers: d.Headers}
	if d.Token != nil {
		rt = &workloadidentity.BearerTransport{Base: rt, Source: d.Token}
	}
	return rt
}

// Client is an HTTP client that sends to this destination with its
// credentials applied.
//
// The credentials live in the transport, so they would ride along on every
// hop of a redirect. Go's default client strips Authorization when a redirect
// changes host; this keeps that promise by not following such a redirect at
// all. The export ends with the redirect status rather than carrying the
// token or a static API key somewhere it wasn't configured to go. Redirects
// within the configured host are still followed.
func (d Destination) Client() *http.Client {
	return &http.Client{
		Transport:     d.Transport(nil),
		Timeout:       DefaultTimeout,
		CheckRedirect: sameHostRedirectsOnly,
	}
}

func sameHostRedirectsOnly(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if req.URL.Host != via[0].URL.Host {
		return http.ErrUseLastResponse
	}
	return nil
}

// Forward posts an already encoded OTLP traces request, carrying its content
// type and encoding through untouched. It returns the collector's status so a
// relay can hand it back to whoever sent the spans.
func (d Destination) Forward(ctx context.Context, client *http.Client, body []byte, contentType, contentEncoding string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TracesURL(), bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("building OTLP request: %w", err)
	}
	if contentType == "" {
		contentType = "application/x-protobuf"
	}
	req.Header.Set("Content-Type", contentType)
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if len(t.headers) == 0 {
		return t.base.RoundTrip(r)
	}
	clone := r.Clone(r.Context())
	for k, v := range t.headers {
		clone.Header.Set(k, v)
	}
	return t.base.RoundTrip(clone)
}

// ParseHeaders parses the OTEL_EXPORTER_OTLP_HEADERS format, key1=value1,
// key2=value2, with values URL-decoded as the OTel spec requires (so a basic
// credential is written Authorization=Basic%20...). A value that fails to
// decode is kept as written.
func ParseHeaders(raw string) map[string]string {
	headers := make(map[string]string)
	for pair := range strings.SplitSeq(raw, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if decoded, err := url.PathUnescape(v); err == nil {
			v = decoded
		}
		headers[k] = v
	}
	return headers
}

// RelayAddr is where the server accepts OTLP/HTTP traces from processes it
// runs outside any sandbox, today only buildkitd. It is fixed rather than
// ephemeral because buildkitd's container spec, env included, outlives a
// server restart; a port that moved on every restart would force the
// container to be recreated each time. The 1xxxx range follows etcd's lead in
// staying clear of the standard 4318 an operator's own collector may hold.
const RelayAddr = "127.0.0.1:14318"

// maxRelayBody bounds one relayed export. buildkitd batches spans well under
// this; anything larger is a misbehaving client rather than a real batch.
const maxRelayBody = 16 << 20

// NewRelay returns a handler that forwards OTLP/HTTP trace exports to d with
// its credentials applied, so a process that cannot refresh a token on its own
// can still export as the cluster's telemetry writer. The collector's status
// and response body pass straight back, which keeps the sender's own retry
// logic working.
//
// The relay does not authenticate its callers. Anything that can reach the
// host's loopback can post spans that arrive as the cluster's telemetry
// writer and read the collector's reply to that post, but never the token.
// Only the body and its content headers are forwarded, so a caller can't
// steer the export with headers the collector routes on, such as a tenant.
// Build steps can't reach the relay: no solve grants the network.host
// entitlement. That is the same boundary vmagent's loopback import already
// has for metrics, and the two should be hardened together (MIR-1985).
func NewRelay(log *slog.Logger, d Destination) http.Handler {
	target, err := url.Parse(d.TracesURL())
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "traces destination is misconfigured", http.StatusBadGateway)
		})
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			u := *target
			pr.Out.URL = &u
			pr.Out.Host = u.Host
			// Only what describes the body goes through. The destination's
			// own headers and credentials are applied in the transport.
			pr.Out.Header = relayedHeaders(pr.In.Header)
		},
		Transport: d.Transport(nil),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Warn("failed to relay spans to collector", "error", err)
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/traces" {
			http.NotFound(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRelayBody)
		proxy.ServeHTTP(w, r)
	})
}

// relayedHeaders keeps the headers an OTLP/HTTP export needs to be decoded.
func relayedHeaders(in http.Header) http.Header {
	out := http.Header{}
	for _, k := range []string{"Content-Type", "Content-Encoding"} {
		if v := in.Values(k); len(v) > 0 {
			out[k] = v
		}
	}
	return out
}
