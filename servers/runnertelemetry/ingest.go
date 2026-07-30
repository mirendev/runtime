// Package runnertelemetry accepts the metrics and logs a distributed runner
// ships, and forwards them to the cluster's VictoriaMetrics and VictoriaLogs.
//
// It exists so that neither of those ever has to listen anywhere a runner can
// reach. Open-source VictoriaMetrics and VictoriaLogs have no authentication of
// their own, so historically the only thing standing between a runner's network
// and unauthenticated write access to both was a firewall rule. Runners already
// hold a certificate from Join and already talk to the coordinator over an
// authenticated listener, so routing telemetry through that listener lets both
// stay bound to loopback permanently.
package runnertelemetry

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"miren.dev/runtime/pkg/workloadidentity"
)

const (
	// Audience scopes a telemetry token to this service. A system workload may
	// legitimately call several services, so the token it presents here must
	// name this one; sharing an audience with, say, the registry would make a
	// token minted for one replayable against the other.
	Audience = "miren-telemetry"

	// TokenHeader carries the runner's system workload token.
	//
	// Deliberately not Authorization. On a cloud-registered cluster the RPC
	// listener's authenticator tries the cloud JWT validator first whenever an
	// Authorization header is present, and a validation failure there returns a
	// hard error rather than falling through to the certificate check below it
	// (see pkg/cloudauth/rpc_authenticator.go). A cluster-issued workload token
	// is not a cloud JWT and would fail that validator, so putting it in
	// Authorization would turn an otherwise-valid mTLS request into a 401. A
	// header the authenticator ignores keeps the two credentials independent.
	TokenHeader = "Miren-Workload-Token"

	// MetricsBasePath and LogsBasePath are what a runner points its writers at.
	// Each writer appends its own backend-native suffix, so the bytes on the
	// wire are exactly what VictoriaMetrics and VictoriaLogs already accept and
	// this package never has to understand the payloads.
	MetricsBasePath = "/_telemetry/metrics"
	LogsBasePath    = "/_telemetry/logs"

	metricsImportPath = "/api/v1/import/prometheus"
	logsInsertPath    = "/insert/jsonline"

	// maxIngestBytes bounds a single batch. Comfortably above a full metrics
	// buffer or log batch, low enough that the coordinator cannot be made to
	// buffer something enormous on a runner's say-so.
	maxIngestBytes = 32 << 20

	// forwardTimeout bounds the hop to the local backend. It is loopback, so
	// anything slower than this is a backend in trouble rather than a slow link.
	forwardTimeout = 30 * time.Second

	maxErrorBodyBytes = 512
)

// MetricsPattern and LogsPattern are the ServeMux patterns these handlers mount
// on.
//
// They pin an exact path and method rather than proxying everything beneath a
// prefix. A prefix would hand a runner the rest of both APIs, including reads
// and VictoriaMetrics' delete-series admin endpoint, which would make scoping
// the token pointless: the credential would say "may write telemetry" while the
// route said "may do anything."
var (
	MetricsPattern = http.MethodPost + " " + MetricsBasePath + metricsImportPath
	LogsPattern    = http.MethodPost + " " + LogsBasePath + logsInsertPath
)

// Verifier checks that a presented token really identifies the telemetry writer
// of a runner in this cluster. It is satisfied by *workloadidentity.Issuer,
// which verifies in-process against the signing keys it already holds.
type Verifier interface {
	VerifySystemWorkloadToken(token, audience string, workload workloadidentity.SystemWorkload) (*workloadidentity.WorkloadClaims, error)
}

type handler struct {
	log         *slog.Logger
	verifier    Verifier
	targetURL   string
	contentType string
	client      *http.Client
	kind        string
}

// NewMetricsHandler forwards accepted batches to VictoriaMetrics' Prometheus
// import endpoint. address is the backend's host:port, normally loopback.
func NewMetricsHandler(log *slog.Logger, verifier Verifier, address string) http.Handler {
	return newHandler(log, verifier, address, metricsImportPath, "text/plain", "metrics")
}

// NewLogsHandler forwards accepted batches to VictoriaLogs' JSON-lines insert
// endpoint. address is the backend's host:port, normally loopback.
func NewLogsHandler(log *slog.Logger, verifier Verifier, address string) http.Handler {
	return newHandler(log, verifier, address, logsInsertPath, "application/x-ndjson", "logs")
}

func newHandler(log *slog.Logger, verifier Verifier, address, path, contentType, kind string) http.Handler {
	return &handler{
		log:         log.With("module", "runnertelemetry", "kind", kind),
		verifier:    verifier,
		targetURL:   backendURL(address, path),
		contentType: contentType,
		client:      &http.Client{Timeout: forwardTimeout},
		kind:        kind,
	}
}

// backendURL renders the forwarding target. A bare host:port means plain HTTP,
// which is what an embedded backend on loopback is.
func backendURL(address, path string) string {
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = "http://" + address
	}
	return strings.TrimRight(address, "/") + path
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The token is verified here rather than trusted from the request's ambient
	// RPC identity, and that is not redundant. Reaching this handler at all
	// requires a cluster certificate, but a certificate identity bypasses
	// authorization entirely (pkg/cloudauth/rpc_authenticator.go), so accepting
	// it would mean any cluster peer could write telemetry. The token is what
	// narrows that to "the telemetry writer of a registered runner", and it is
	// short-lived where the certificate is not.
	token := r.Header.Get(TokenHeader)
	if token == "" {
		h.log.Warn("telemetry ingest rejected", "reason", "no workload token")
		http.Error(w, "missing workload token", http.StatusUnauthorized)
		return
	}

	if _, err := h.verifier.VerifySystemWorkloadToken(token, Audience,
		workloadidentity.SystemWorkloadTelemetryWriter); err != nil {
		h.log.Warn("telemetry ingest rejected", "reason", "invalid workload token", "error", err)
		http.Error(w, "invalid workload token", http.StatusUnauthorized)
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxIngestBytes)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.targetURL, body)
	if err != nil {
		h.log.Error("building forward request", "error", err)
		http.Error(w, "forwarding telemetry failed", http.StatusInternalServerError)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = h.contentType
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := h.client.Do(req)
	if err != nil {
		h.log.Error("forwarding telemetry to backend", "error", err, "target", h.targetURL)
		http.Error(w, "forwarding telemetry failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// The runner decides whether to retry from this status, so a backend
	// rejection has to reach it rather than being flattened into a success.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))

		// Quote a bounded prefix, but still drain the rest. Closing a body with
		// bytes left unread retires the connection, so a backend that rejects
		// steadily would cost a fresh dial per batch at exactly the moment
		// things are already going wrong.
		_, _ = io.Copy(io.Discard, resp.Body)

		h.log.Warn("backend rejected telemetry",
			"status", resp.StatusCode, "detail", strings.TrimSpace(string(detail)))
		http.Error(w, fmt.Sprintf("backend returned %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	// Status before the drain. Draining goes to io.Discard rather than to w, so
	// today the order does not matter, but it would the moment anyone relays
	// the backend's body to the caller: the first write would commit a 200 and
	// silently discard whatever the backend actually said.
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(io.Discard, resp.Body)
}
