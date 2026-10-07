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

	"miren.dev/runtime/metrics"
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

	// StreamHeader marks a metrics batch as the runner's own operational
	// series (process memory and identity, host usage, controller queues)
	// rather than per-sandbox usage. Operational series join the
	// coordinator's, so they reach the embedded store and also leave the
	// cluster through the managed-metrics shipping path; sandbox series stay
	// in the embedded store, the same as the coordinator's own.
	//
	// A header on the existing route rather than a route of its own is what
	// makes version skew free. A coordinator that predates it ignores the
	// header and forwards the batch as it always has, so a runner upgraded
	// first loses nothing, where a new route would answer 404 and leave the
	// runner's writer retrying the batch until the coordinator caught up.
	StreamHeader = "Miren-Telemetry-Stream"

	// StreamOperational is the StreamHeader value for operational series.
	StreamOperational = "operational"

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

	// operational receives batches marked with StreamOperational. Nil means
	// every batch is forwarded to the backend unread.
	operational metrics.PointWriter
}

// NewMetricsHandler forwards accepted batches to VictoriaMetrics' Prometheus
// import endpoint. address is the backend's host:port, normally loopback.
//
// Batches marked as operational go to operational instead, which is the
// coordinator's own operational fanout: it already writes to the same
// embedded store, and it is where the shipping sink attaches. A nil
// operational forwards those batches to the backend like any other.
func NewMetricsHandler(log *slog.Logger, verifier Verifier, address string, operational metrics.PointWriter) http.Handler {
	h := newHandler(log, verifier, address, metricsImportPath, "text/plain", "metrics")
	h.operational = operational
	return h
}

// NewLogsHandler forwards accepted batches to VictoriaLogs' JSON-lines insert
// endpoint. address is the backend's host:port, normally loopback.
func NewLogsHandler(log *slog.Logger, verifier Verifier, address string) http.Handler {
	return newHandler(log, verifier, address, logsInsertPath, "application/x-ndjson", "logs")
}

func newHandler(log *slog.Logger, verifier Verifier, address, path, contentType, kind string) *handler {
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

	claims, err := h.verifier.VerifySystemWorkloadToken(token, Audience,
		workloadidentity.SystemWorkloadTelemetryWriter)
	if err != nil {
		h.log.Warn("telemetry ingest rejected", "reason", "invalid workload token", "error", err)
		http.Error(w, "invalid workload token", http.StatusUnauthorized)
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxIngestBytes)

	if h.operational != nil && r.Header.Get(StreamHeader) == StreamOperational {
		h.writeOperational(w, r, body, claims.RunnerID)
		return
	}

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

// clusterLabel is the label the shipping path uses to tell clusters apart in
// the shared store. The shipping sink only adds it when a point lacks it, so a
// runner that supplied its own could file its series under another cluster;
// stripping it here leaves the coordinator's stamp as the only source.
const clusterLabel = "miren_cluster"

// runnerLabel names the runner a series belongs to. When the runner's token
// says which runner it is, that is written over whatever the point carried.
const runnerLabel = "miren_runner"

const (
	// maxOperationalPoints bounds what one operational batch may make the
	// coordinator hold in memory. A runner emits a few dozen operational
	// points per flush, and the coordinator's own writers hold at most ten
	// thousand, so anything beyond this could not be kept anyway.
	maxOperationalPoints = 10000

	// operationalChunk is how much of a batch is handed to the sinks at once.
	// A sink refuses a write that would overflow its buffer outright, so
	// feeding a large batch in pieces lets it take as much as it has room for
	// rather than all or nothing.
	operationalChunk = 1000
)

func (h *handler) writeOperational(w http.ResponseWriter, r *http.Request, body io.Reader, runnerID string) {
	data, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, "reading telemetry failed", http.StatusBadRequest)
		return
	}

	parsed, err := metrics.ParsePoints(data, maxOperationalPoints)
	if err != nil {
		http.Error(w, "reading telemetry failed", http.StatusBadRequest)
		return
	}

	// Nothing here is refused back to the runner. Its writer keeps a refused
	// batch and prepends it to the next one, so a refusal would be retried
	// forever: one bad line would block every sample queued behind it, and
	// after an outage a backlog could outgrow what the sinks here will ever
	// take at once. Operational series are gauges re-sampled every few
	// seconds, so shedding what cannot be used costs little and fresh samples
	// flow again at once.
	if parsed.Invalid > 0 {
		h.log.Warn("operational metrics skipped", "reason", "unparseable line",
			"lines", parsed.Invalid, "first_error", parsed.FirstInvalid)
	}

	// A token minted before the coordinator stamped runner IDs, or on a
	// cluster with authentication disabled, does not say which runner sent it.
	// Such a batch falls back to trusting the runner's own label rather than
	// being dropped. The first case only lasts as long as a token minted by
	// the previous coordinator, and dropping instead would open a gap in every
	// runner's series right after a coordinator upgrade, which is exactly when
	// the restart and build-skew rules are looking.
	//
	// That window is an hour for a runner's own telemetry client, which asks
	// for one, but the coordinator honors any requested lifetime up to
	// workloadidentity.MaxTTL. A runner set on choosing its own label could
	// have asked the previous coordinator for a token that long, so MaxTTL is
	// the real bound, not the hour.
	if runnerID == "" {
		h.log.Debug("operational metrics attributed by label", "reason", "token names no runner")
	}

	points := make([]metrics.MetricPoint, 0, len(parsed.Points))
	impersonating, relabeled := 0, 0
	for _, point := range parsed.Points {
		// A point naming the control process would ship as the coordinator's
		// own series and feed the coordinator's restart and build-skew rules,
		// whichever runner it claims to be from.
		//
		// Without a verified runner ID, a point with no runner label is refused
		// too: the shipping sink fills in the coordinator's runner ID on a
		// point that has none, so it would pass as the coordinator's as well.
		if point.Labels["entity"] == metrics.EntityControl || (runnerID == "" && point.Labels[runnerLabel] == "") {
			impersonating++
			continue
		}

		_, hasCluster := point.Labels[clusterLabel]
		stampRunner := runnerID != "" && point.Labels[runnerLabel] != runnerID
		if hasCluster || stampRunner {
			if stampRunner && point.Labels[runnerLabel] != "" {
				relabeled++
			}
			labels := make(map[string]string, len(point.Labels)+1)
			for name, value := range point.Labels {
				if name != clusterLabel {
					labels[name] = value
				}
			}
			if runnerID != "" {
				labels[runnerLabel] = runnerID
			}
			point.Labels = labels
		}
		points = append(points, point)
	}
	if impersonating > 0 {
		h.log.Warn("operational metrics skipped", "reason", "not labeled as a runner",
			"points", impersonating)
	}
	if relabeled > 0 {
		h.log.Warn("operational metrics relabeled", "reason", "labeled as a different runner",
			"points", relabeled, "runner", runnerID)
	}
	if parsed.OverLimit > 0 {
		h.log.Warn("operational metrics skipped", "reason", "batch over limit",
			"points", parsed.OverLimit, "limit", maxOperationalPoints)
	}

	// Every chunk goes to the fanout even after one is refused. The fanout
	// reports any sink's refusal without saying which, and stopping would
	// starve the sinks that still have room: a full shipping buffer would
	// otherwise cost the embedded store its copy too.
	refused := 0
	var firstRefusal error
	for start := 0; start < len(points); start += operationalChunk {
		chunk := points[start:min(start+operationalChunk, len(points))]
		if err := h.operational.WritePoints(r.Context(), chunk); err != nil {
			if firstRefusal == nil {
				firstRefusal = err
			}
			refused += len(chunk)
		}
	}
	if refused > 0 {
		h.log.Warn("operational metrics refused by a sink", "points", refused, "error", firstRefusal)
	}

	w.WriteHeader(http.StatusNoContent)
}
