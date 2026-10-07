package metricspush

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"miren.dev/runtime/pkg/workloadidentity"
)

const (
	prometheusImportPath = "/api/v1/import/prometheus"
	otlpImportPath       = "/opentelemetry/v1/metrics"

	forwardTimeout    = 30 * time.Second
	maxErrorBodyBytes = 512
)

// Verifier checks a sandbox's workload token. It is satisfied by
// *workloadidentity.Issuer, which verifies in-process with the keys it signs
// with.
type Verifier interface {
	VerifyToken(token, audience string) (*workloadidentity.WorkloadClaims, error)
}

// Ingest is the coordinator's half of the push path. It is where a push is
// trusted or refused, so it assumes nothing about how the push arrived: the
// relay in this process and a relay on a distributed runner reach the same
// checks.
type Ingest struct {
	log      *slog.Logger
	verifier Verifier
	client   *http.Client

	// enabled is whether the cluster has a remote-write destination at all.
	// It comes from configuration rather than from vmagent being up, so it
	// holds from the moment the listener starts: a sandbox created during boot,
	// before managed metrics arms the ingest, is still told push exists. Fail
	// takes it back if vmagent turns out never to be coming.
	enabled atomic.Bool

	mu      sync.RWMutex
	backend Backend
}

// Backend is everything a push needs that only exists once managed metrics is
// running.
type Backend struct {
	// ImportURL is the managed-metrics vmagent, normally on loopback.
	ImportURL string

	// ClusterID is stamped as miren_cluster, the same value the scrape path
	// uses.
	ClusterID string

	// Resolver looks up the sandbox a token names.
	Resolver SandboxResolver
}

func NewIngest(log *slog.Logger, verifier Verifier, enabled bool) *Ingest {
	i := &Ingest{
		log:      log.With("module", "metricspush"),
		verifier: verifier,
		client:   &http.Client{Timeout: forwardTimeout},
	}
	i.enabled.Store(enabled)
	return i
}

// Available reports whether this cluster accepts pushes: it has a remote-write
// destination configured, and managed metrics has not failed to start.
func (i *Ingest) Available(context.Context) bool {
	return i.enabled.Load()
}

// Fail stops advertising push, for when managed metrics could not start and
// will not be arming this ingest. Without it, every sandbox started afterward
// would be told push works and have each push refused.
func (i *Ingest) Fail() {
	i.enabled.Store(false)
}

// Arm points the ingest at a running vmagent. It arrives after the ingest is
// mounted, because the API listener has to be up before the entity store and
// managed metrics can start behind it. Until then a push is refused as
// unavailable rather than dropped.
func (i *Ingest) Arm(backend Backend) {
	backend.ImportURL = strings.TrimRight(backend.ImportURL, "/")
	i.mu.Lock()
	defer i.mu.Unlock()
	i.backend = backend
}

// Disarm stops forwarding, for when managed metrics shuts down.
func (i *Ingest) Disarm() {
	i.Arm(Backend{})
}

func (i *Ingest) armed() (Backend, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.backend, i.backend.ImportURL != "" && i.backend.Resolver != nil
}

// Push checks p and forwards it to vmagent labeled for the sandbox its token
// names.
func (i *Ingest) Push(ctx context.Context, p Push) error {
	if !p.Scope.valid() {
		return errorf(http.StatusBadRequest, "unknown push scope %q", p.Scope)
	}
	if !p.Format.valid() {
		return errorf(http.StatusBadRequest, "unknown push format %q", p.Format)
	}

	if !i.enabled.Load() {
		return errorf(http.StatusServiceUnavailable,
			"managed metrics is not enabled on this cluster (no telemetry.metrics destination)")
	}
	backend, ok := i.armed()
	if !ok {
		return errorf(http.StatusServiceUnavailable, "managed metrics is starting")
	}

	claims, err := i.verifier.VerifyToken(p.Token, Audience)
	if err != nil {
		i.log.Warn("metrics push rejected", "reason", "invalid workload token", "error", err)
		return errorf(http.StatusUnauthorized, "invalid workload token")
	}
	if claims.IdentityType != workloadidentity.IdentityTypeSandbox || claims.SandboxID == "" || claims.App == "" {
		i.log.Warn("metrics push rejected", "reason", "not a sandbox identity",
			"identity_type", claims.IdentityType, "subject", claims.Subject)
		return errorf(http.StatusForbidden, "metrics push requires a sandbox identity")
	}

	sandbox, err := backend.Resolver.Resolve(ctx, claims.SandboxID)
	if err != nil {
		i.log.Warn("metrics push rejected", "reason", "sandbox lookup failed", "sandbox", claims.SandboxID, "error", err)
		return errorf(http.StatusForbidden, "sandbox %s cannot push metrics", claims.SandboxID)
	}
	// The coordinator minted the token from the same entity, so these can only
	// disagree if something has gone badly wrong. Labels come from the entity
	// either way; refusing here keeps a disagreement from passing quietly.
	if sandbox.App != claims.App {
		i.log.Error("metrics push token names a different app than its sandbox",
			"sandbox", claims.SandboxID, "token_app", claims.App, "sandbox_app", sandbox.App)
		return errorf(http.StatusForbidden, "sandbox %s cannot push metrics", claims.SandboxID)
	}

	body, err := decodeBody(p.Body, p.ContentEncoding)
	if err != nil {
		return err
	}

	labels := sandbox.labels(p.Scope, backend.ClusterID)

	var (
		path        string
		contentType string
	)
	switch p.Format {
	case FormatPrometheus:
		for name := range p.Grouping {
			if err := checkLabelName(name); err != nil {
				return err
			}
		}
		if body, err = preparePrometheus(body, p.ContentType, p.Scope); err != nil {
			return err
		}
		path, contentType = prometheusImportPath, "text/plain"
		// The grouping key names the push, so it wins over a label of the same
		// name inside it, as it does on a Pushgateway.
		for name, value := range p.Grouping {
			labels = append(labels, name+"="+value)
		}
	case FormatOTLP:
		if err := validateOTLP(body, p.Scope); err != nil {
			return err
		}
		path, contentType = otlpImportPath, "application/x-protobuf"
	}

	query := url.Values{"extra_label": labels}
	return i.forward(ctx, backend.ImportURL+path+"?"+query.Encode(), contentType, body, claims.SandboxID)
}

func (i *Ingest) forward(ctx context.Context, target, contentType string, body []byte, sandboxID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building forward request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := i.client.Do(req)
	if err != nil {
		i.log.Warn("forwarding pushed metrics to vmagent", "sandbox", sandboxID, "error", err)
		return errorf(http.StatusBadGateway, "managed metrics is unavailable")
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		_, _ = io.Copy(io.Discard, resp.Body)
		i.log.Warn("vmagent rejected pushed metrics",
			"sandbox", sandboxID, "status", resp.StatusCode, "detail", strings.TrimSpace(string(detail)))
		// vmagent's 4xx is about the payload, which the workload can fix, so
		// it is passed along as one. Anything else is ours.
		if resp.StatusCode/100 == 4 {
			return errorf(http.StatusBadRequest, "metrics rejected: %s", strings.TrimSpace(string(detail)))
		}
		return errorf(http.StatusBadGateway, "managed metrics is unavailable")
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// decodeBody undoes the Content-Encoding the workload sent, bounded so a small
// compressed push cannot expand into something enormous. OTLP exporters gzip by
// default in several SDKs.
func decodeBody(body []byte, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		return body, nil
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, errorf(http.StatusBadRequest, "decoding gzip body: %v", err)
		}
		defer zr.Close()
		out, err := io.ReadAll(io.LimitReader(zr, maxPushBytes+1))
		if err != nil {
			return nil, errorf(http.StatusBadRequest, "decoding gzip body: %v", err)
		}
		if len(out) > maxPushBytes {
			return nil, errorf(http.StatusRequestEntityTooLarge, "push exceeds %d bytes", maxPushBytes)
		}
		return out, nil
	default:
		return nil, errorf(http.StatusUnsupportedMediaType, "unsupported Content-Encoding %q", encoding)
	}
}

// Handler serves IngestPath for relays on the coordinator's API listener.
//
// Reaching it takes an authenticated caller, which in practice is a runner's
// certificate. That is not what authorizes the push: a certificate identity
// bypasses authorization entirely, so the sandbox token in TokenHeader is what
// Push checks, and it is the token alone that decides whose labels the samples
// get.
func (i *Ingest) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPushBytes))
		if err != nil {
			http.Error(w, fmt.Sprintf("push exceeds %d bytes", maxPushBytes), http.StatusRequestEntityTooLarge)
			return
		}

		grouping, err := url.ParseQuery(r.Header.Get(GroupingHeader))
		if err != nil {
			http.Error(w, "malformed grouping key", http.StatusBadRequest)
			return
		}

		err = i.Push(r.Context(), Push{
			Token:           r.Header.Get(TokenHeader),
			Scope:           Scope(r.Header.Get(ScopeHeader)),
			Format:          Format(r.Header.Get(FormatHeader)),
			Grouping:        flattenGrouping(grouping),
			ContentType:     r.Header.Get("Content-Type"),
			ContentEncoding: r.Header.Get("Content-Encoding"),
			Body:            body,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// StatusHandler serves StatusPattern: 204 when the cluster accepts pushes, 503
// when it has no remote-write destination.
func (i *Ingest) StatusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !i.Available(r.Context()) {
			http.Error(w, "managed metrics is not enabled on this cluster", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func flattenGrouping(values url.Values) map[string]string {
	if len(values) == 0 {
		return nil
	}
	grouping := make(map[string]string, len(values))
	for name, vs := range values {
		if len(vs) > 0 {
			grouping[name] = vs[len(vs)-1]
		}
	}
	return grouping
}
