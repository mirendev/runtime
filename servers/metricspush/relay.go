package metricspush

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"miren.dev/runtime/pkg/workloadidentity"
)

const (
	// RelayBasePath is where a workload finds the relay on its bridge router.
	// Each scope is a complete base for the client that uses it: a Pushgateway
	// client is pointed at <base>/<scope>, an OTLP exporter at
	// <base>/<scope>/otlp.
	RelayBasePath = "/v1/metrics"

	// relayTokenTTL is requested explicitly so the relay knows when a cached
	// token dies without decoding it.
	relayTokenTTL = time.Hour

	// relayTokenLeeway renews this far ahead of expiry, leaving room for a slow
	// mint on a runner and for clock skew against the coordinator.
	relayTokenLeeway = 5 * time.Minute

	// relayPushRate and relayPushBurst bound how often one sandbox may push.
	// Scraping bounds this by its interval; a push has no interval, so a
	// worker pushing in a loop could otherwise fill the vmagent queue every
	// app shares. A push a second with a burst of ten is far above any sane
	// cadence (OTel exporters default to a minute) and still stops a loop.
	relayPushRate  = rate.Limit(1)
	relayPushBurst = 10

	// relayLimiterIdle is how long a sandbox's limiter outlives its last push.
	relayLimiterIdle = 10 * time.Minute
)

// Pusher delivers a push to the coordinator. The coordinator's own relay hands
// pushes straight to its *Ingest; a runner's relay sends them over the network
// with a *Client.
type Pusher interface {
	Push(ctx context.Context, p Push) error

	// Available reports whether the cluster accepts pushes at all. Push is
	// only advertised to a sandbox when it does, so an app that finds the
	// push URLs in its environment can rely on them.
	Available(ctx context.Context) bool
}

// SandboxAuthenticator identifies the sandbox behind a request from the
// caller's source address and the secret it presented. It is the token
// server's check, shared so both endpoints stand on the same ground.
type SandboxAuthenticator func(remoteHost, secret string) (sandboxID, app string, ok bool)

// TokenIssuer mints the token the relay attaches for a sandbox. On a runner it
// is the remote issuer, which has the coordinator mint the token and refuses a
// sandbox that is not scheduled to this runner.
type TokenIssuer interface {
	IssueTokenWithOptions(app, sandboxID string, opts workloadidentity.TokenOptions) (string, error)
}

// Relay is the workload-facing half of the push path, served on a node's
// bridge router beside the token server.
//
// It asks the workload for nothing that changes: the per-sandbox secret it
// authenticates with is set once in the sandbox's environment, so a
// Pushgateway client or OTel exporter can carry it as a static header. The
// token the coordinator needs rotates hourly, and minting it is the relay's
// job rather than the workload's.
type Relay struct {
	log    *slog.Logger
	auth   SandboxAuthenticator
	issuer TokenIssuer
	pusher Pusher
	now    func() time.Time

	mu       sync.Mutex
	tokens   map[string]relayToken
	limiters map[string]*relayLimiter
}

type relayLimiter struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

type relayToken struct {
	token   string
	renewAt time.Time
}

func NewRelay(log *slog.Logger, auth SandboxAuthenticator, issuer TokenIssuer, pusher Pusher) *Relay {
	return &Relay{
		log:      log.With("module", "metricspush-relay"),
		auth:     auth,
		issuer:   issuer,
		pusher:   pusher,
		now:      time.Now,
		tokens:   make(map[string]relayToken),
		limiters: make(map[string]*relayLimiter),
	}
}

// Register mounts the relay's routes on mux.
//
// The Pushgateway routes follow its API exactly, so an unmodified client works
// against a scope's base URL. PUT and POST differ on a Pushgateway only in how
// they replace what it has stored; nothing is stored here, so both forward.
// DELETE has nothing to remove and is accepted so a client's cleanup call does
// not fail.
func (r *Relay) Register(mux *http.ServeMux) {
	pushgateway := RelayBasePath + "/{scope}/metrics/{grouping...}"
	mux.HandleFunc(http.MethodPost+" "+pushgateway, r.handlePushgateway)
	mux.HandleFunc(http.MethodPut+" "+pushgateway, r.handlePushgateway)
	mux.HandleFunc(http.MethodDelete+" "+pushgateway, func(w http.ResponseWriter, req *http.Request) {
		if _, _, ok := r.authenticate(w, req); ok {
			w.WriteHeader(http.StatusAccepted)
		}
	})
	mux.HandleFunc(http.MethodPost+" "+RelayBasePath+"/{scope}/otlp/v1/metrics", r.handleOTLP)
}

func (r *Relay) handlePushgateway(w http.ResponseWriter, req *http.Request) {
	grouping, err := parseGroupingKey(req.PathValue("grouping"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.relay(w, req, FormatPrometheus, grouping)
}

func (r *Relay) handleOTLP(w http.ResponseWriter, req *http.Request) {
	r.relay(w, req, FormatOTLP, nil)
}

func (r *Relay) relay(w http.ResponseWriter, req *http.Request, format Format, grouping map[string]string) {
	scope := Scope(req.PathValue("scope"))
	if !scope.valid() {
		http.Error(w, fmt.Sprintf("unknown scope %q: push to %s/%s or %s/%s",
			scope, RelayBasePath, ScopeSandbox, RelayBasePath, ScopeApp), http.StatusNotFound)
		return
	}

	sandboxID, app, ok := r.authenticate(w, req)
	if !ok {
		return
	}
	if !r.allow(sandboxID) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "pushing too often: at most one push a second per sandbox", http.StatusTooManyRequests)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxPushBytes))
	if err != nil {
		http.Error(w, fmt.Sprintf("push exceeds %d bytes", maxPushBytes), http.StatusRequestEntityTooLarge)
		return
	}

	token, err := r.token(sandboxID, app)
	if err != nil {
		r.log.Warn("minting metrics push token", "sandbox", sandboxID, "error", err)
		http.Error(w, "metrics push is unavailable", http.StatusServiceUnavailable)
		return
	}

	err = r.pusher.Push(req.Context(), Push{
		Token:           token,
		Scope:           scope,
		Format:          format,
		Grouping:        grouping,
		ContentType:     req.Header.Get("Content-Type"),
		ContentEncoding: req.Header.Get("Content-Encoding"),
		Body:            body,
	})
	if err != nil {
		// A 401 from the coordinator means the token went bad under us, most
		// likely a key rotation. Dropping it makes the workload's next push
		// mint a fresh one.
		if pe, ok := errors.AsType[*Error](err); ok && pe.Status == http.StatusUnauthorized {
			r.forget(sandboxID)
		}
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// authenticate resolves the calling sandbox, writing the refusal itself when it
// cannot.
func (r *Relay) authenticate(w http.ResponseWriter, req *http.Request) (sandboxID, app string, ok bool) {
	remoteHost, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		http.Error(w, "invalid remote address", http.StatusBadRequest)
		return "", "", false
	}
	secret, found := requestSecret(req)
	if !found {
		w.Header().Set("WWW-Authenticate", `Bearer realm="miren-metrics"`)
		http.Error(w, "missing credential: send MIREN_IDENTITY_TOKEN_SECRET as a Bearer token", http.StatusUnauthorized)
		return "", "", false
	}
	sandboxID, app, ok = r.auth(remoteHost, secret)
	if !ok {
		http.Error(w, "credential does not match the calling sandbox", http.StatusForbidden)
		return "", "", false
	}
	return sandboxID, app, true
}

// requestSecret takes the sandbox secret as a Bearer token, which is what an
// OTel exporter's headers setting sends, or as the password of Basic auth,
// which is what most Pushgateway clients offer. The Basic username is ignored.
func requestSecret(req *http.Request) (string, bool) {
	if _, password, ok := req.BasicAuth(); ok && password != "" {
		return password, true
	}
	auth := req.Header.Get("Authorization")
	if secret, ok := strings.CutPrefix(auth, "Bearer "); ok && secret != "" {
		return secret, true
	}
	return "", false
}

func (r *Relay) token(sandboxID, app string) (string, error) {
	now := r.now()

	r.mu.Lock()
	cached, ok := r.tokens[sandboxID]
	r.mu.Unlock()
	if ok && now.Before(cached.renewAt) {
		return cached.token, nil
	}

	token, err := r.issuer.IssueTokenWithOptions(app, sandboxID, workloadidentity.TokenOptions{
		Audience: []string{Audience},
		TTL:      relayTokenTTL,
	})
	if err != nil {
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Sweep on write so sandboxes that are gone do not keep their tokens.
	for id, t := range r.tokens {
		if !now.Before(t.renewAt) {
			delete(r.tokens, id)
		}
	}
	r.tokens[sandboxID] = relayToken{token: token, renewAt: now.Add(relayTokenTTL - relayTokenLeeway)}
	return token, nil
}

// allow takes a token from the sandbox's bucket, creating it on first use and
// sweeping buckets that have sat idle.
func (r *Relay) allow(sandboxID string) bool {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	l, ok := r.limiters[sandboxID]
	if !ok {
		for id, idle := range r.limiters {
			if now.Sub(idle.lastUsed) > relayLimiterIdle {
				delete(r.limiters, id)
			}
		}
		l = &relayLimiter{limiter: rate.NewLimiter(relayPushRate, relayPushBurst)}
		r.limiters[sandboxID] = l
	}
	l.lastUsed = now
	return l.limiter.AllowN(now, 1)
}

func (r *Relay) forget(sandboxID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tokens, sandboxID)
}

// parseGroupingKey reads a Pushgateway grouping path, job/<name>{/<label>/<value>},
// including the @base64 form clients use for values a path cannot carry.
func parseGroupingKey(path string) (map[string]string, error) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 || len(segments)%2 != 0 {
		return nil, errors.New("grouping key must be job/<name> followed by label/value pairs")
	}

	grouping := make(map[string]string, len(segments)/2)
	for i := 0; i < len(segments); i += 2 {
		name, value := segments[i], segments[i+1]
		if base, ok := strings.CutSuffix(name, "@base64"); ok {
			decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
			if err != nil {
				return nil, fmt.Errorf("grouping label %q: invalid base64 value", base)
			}
			name, value = base, string(decoded)
		}
		if i == 0 && name != "job" {
			return nil, errors.New("grouping key must start with job/<name>")
		}
		if _, dup := grouping[name]; dup {
			return nil, fmt.Errorf("grouping label %q given twice", name)
		}
		grouping[name] = value
	}
	if grouping["job"] == "" {
		return nil, errors.New("job name must not be empty")
	}
	return grouping, nil
}
