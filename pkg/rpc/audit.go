package rpc

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// certFingerprint returns the hex-encoded SHA-256 of a certificate's raw DER,
// the standard stable identifier for a cert in an audit trail.
func certFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// minLevelHandler wraps a slog.Handler and enforces its own minimum level,
// independent of the wrapped handler's configured level. Records below min are
// dropped even if the wrapped handler would emit them; records at or above min
// are passed straight to the wrapped handler's Handle, which does the
// formatting and writing without re-checking the level.
//
// The Info floor is therefore only as reliable as that last assumption: it
// relies on the wrapped sink not re-gating in Handle. That is the slog.Handler
// contract (the standard library handlers gate only in Enabled, which the
// Logger checks before calling Handle), so any well-behaved sink is fine, but a
// sink that re-checks the level in Handle would silently break the floor.
//
// The audit trail uses this to hold a fixed Info floor regardless of the
// process-wide verbosity, which it must do in both directions. Run the process
// verbosely and a plain shared logger would never suppress the audit stream's
// own Debug records (loopback internal traffic); run it quietly — a CLI
// invocation sits at Warn — and a shared logger would drop audit Info entirely.
// Pinning the floor at Info decouples the audit trail from the operational -v
// knob either way.
//
// The corollary is worth stating because it is easy to forget: the audit stream
// is immune to -v, so no choice of default verbosity can reduce its volume.
// Anything about audit volume has to be fixed in the audit code itself, which
// is what certAuthDeduper does.
type minLevelHandler struct {
	slog.Handler
	min slog.Level
}

func (h minLevelHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.min
}

func (h minLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return minLevelHandler{Handler: h.Handler.WithAttrs(attrs), min: h.min}
}

func (h minLevelHandler) WithGroup(name string) slog.Handler {
	return minLevelHandler{Handler: h.Handler.WithGroup(name), min: h.min}
}

// newAuditLogger derives the security audit logger from the server's base
// logger. It shares the base's sink (so audit records flow to the same log
// collection) but pins the level floor at Info via minLevelHandler and tags the
// stream module=audit so it can be filtered and routed downstream.
func newAuditLogger(base *slog.Logger) *slog.Logger {
	return slog.New(minLevelHandler{Handler: base.Handler(), min: slog.LevelInfo}).
		With("module", "audit")
}

// isLoopbackAddr reports whether addr (an *http.Request RemoteAddr in "host:port"
// form) is a loopback address — i.e. a same-host internal component rather than
// a network peer. Loopback traffic is the coordinator's own services talking to
// each other (e.g. the API server polling its own entity store) and dominates
// the log; audit records for it are demoted to Debug so the Info stream stays
// the network attack surface. An unparseable address is treated as non-loopback
// so we err toward recording at the higher level.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// remoteHost strips the port from an *http.Request RemoteAddr. Source ports are
// ephemeral and carry no attribution value, so the audit trail keys on the host.
func remoteHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// defaultCertAuthInterval is how often a still-active (cert, peer) pair re-emits
// its certificate detail. Short enough that the trail keeps useful temporal
// resolution, long enough that a busy peer costs a dozen records an hour rather
// than several thousand.
const defaultCertAuthInterval = 5 * time.Minute

// maxCertAuthTracked bounds the dedup table. Real clusters track a handful of
// pairs (one per runner, plus operators); the cap exists so that a peer churning
// through source addresses cannot grow the table without limit.
const maxCertAuthTracked = 1024

type certAuthKey struct {
	fingerprint string
	host        string
}

type certAuthState struct {
	last       time.Time
	suppressed int64
}

// certAuthDeduper collapses repeated cert-auth records for the same certificate
// and peer host into one record per interval.
//
// The per-request cert-auth record is almost entirely redundant. Every field
// that distinguishes it (subject, issuer, serial, fingerprint) describes the
// certificate rather than the request, so a long-lived peer re-emits the same
// ~330 bytes on every single RPC. Measured on a coordinator with two
// distributed runners, this was 5,958 records/hour and 47% of everything the
// process wrote, to convey that the same two certs were still the same two
// certs.
//
// Per-request attribution is not lost, because it never lived here: logAccess
// records the subject and auth method for every non-public call, and that is
// the trail the audit design leans on (see logCertAuth). What this preserves is
// the certificate detail, emitted on first sight of a (cert, peer host) pair
// and again once per interval, carrying the number of authentications absorbed
// since the last record. One line saying a cert authenticated 5,958 times is
// strictly more informative than 5,958 identical lines.
type certAuthDeduper struct {
	mu       sync.Mutex
	interval time.Duration
	max      int
	seen     map[certAuthKey]*certAuthState

	// now is overridable so tests can drive the interval without sleeping.
	now func() time.Time
}

func newCertAuthDeduper() *certAuthDeduper {
	return &certAuthDeduper{
		interval: defaultCertAuthInterval,
		max:      maxCertAuthTracked,
		seen:     make(map[certAuthKey]*certAuthState),
	}
}

func (d *certAuthDeduper) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

// record reports whether this authentication should be logged and, if so, how
// many authentications for the same pair went unlogged since the previous
// record. A nil deduper logs everything, which keeps a hand-constructed
// StateCommon (as in tests) on the pre-dedup behaviour.
func (d *certAuthDeduper) record(key certAuthKey) (bool, int64) {
	if d == nil {
		return true, 0
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.clock()

	if st, ok := d.seen[key]; ok {
		if now.Sub(st.last) < d.interval {
			st.suppressed++
			return false, 0
		}
		suppressed := st.suppressed
		st.last = now
		st.suppressed = 0
		return true, suppressed
	}

	d.evictLocked(now)
	d.seen[key] = &certAuthState{last: now}
	return true, 0
}

// evictLocked keeps the table under its cap, dropping entries that have gone
// quiet for longer than an interval first and falling back to the
// least-recently-recorded entry. An evicted entry loses its pending suppressed
// count, so the peer simply reads as newly seen next time.
func (d *certAuthDeduper) evictLocked(now time.Time) {
	if len(d.seen) < d.max {
		return
	}

	for k, st := range d.seen {
		if now.Sub(st.last) >= d.interval {
			delete(d.seen, k)
		}
	}
	if len(d.seen) < d.max {
		return
	}

	var (
		oldestKey certAuthKey
		oldest    time.Time
		found     bool
	)
	for k, st := range d.seen {
		if !found || st.last.Before(oldest) {
			oldestKey, oldest, found = k, st.last, true
		}
	}
	if found {
		delete(d.seen, oldestKey)
	}
}

// logCertAuth emits a single durable audit record for a successful cert-method
// authentication.
//
// Note on scope: the listener uses tls.VerifyClientCertIfGiven, so any client
// cert reaching this point has already chained to the cluster CA and a forged
// cert is rejected during the TLS handshake, before any authenticator runs.
// This record therefore only ever captures legitimate cert auth (in practice,
// internal component mTLS) — it can never see an attacker. It exists so that
// legitimate cert use is attributable after the fact, not as a tripwire; the
// per-request access log (logAccess) is the auth-method-agnostic audit trail
// that survives a bypass on any path. Kept deliberately to one line.
//
// Logged at Info for network peers and Debug for loopback (same-host internal
// component mTLS), so the Info stream isn't drowned by the coordinator's own
// services authenticating to each other. See isLoopbackAddr.
//
// Records are deduplicated per (certificate, peer host) so that a long-lived
// peer costs one record per interval rather than one per RPC; see
// certAuthDeduper for why that loses no attribution. A record that stands in
// for suppressed ones carries a "suppressed" count.
func logCertAuth(ctx context.Context, log *slog.Logger, dedup *certAuthDeduper, r *http.Request) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return
	}
	cert := r.TLS.PeerCertificates[0]

	fingerprint := certFingerprint(cert)
	shouldLog, suppressed := dedup.record(certAuthKey{
		fingerprint: fingerprint,
		host:        remoteHost(r.RemoteAddr),
	})
	if !shouldLog {
		return
	}

	level := slog.LevelInfo
	if isLoopbackAddr(r.RemoteAddr) {
		level = slog.LevelDebug
	}

	attrs := []any{
		"remote", r.RemoteAddr,
		"subject", cert.Subject.String(),
		"issuer", cert.Issuer.String(),
		"serial", cert.SerialNumber.String(),
		"fingerprint", fingerprint,
		"verified", len(r.TLS.VerifiedChains) > 0,
		"chains", len(r.TLS.VerifiedChains),
	}
	if suppressed > 0 {
		attrs = append(attrs, "suppressed", suppressed)
	}

	log.Log(ctx, level, "cert auth", attrs...)
}

// logAccess emits a per-request RPC access record for a non-public method.
// It is intentionally auth-method-agnostic: every line carries the source IP,
// the authenticated subject, and the auth method, so the trail stays useful no
// matter which auth path a caller (or a future bypass) came in on. outcome is
// "ok" for an authorized dispatch, or "unauthorized"/"forbidden" for a rejected
// one; non-ok outcomes log at Warn so denials surface without a level filter.
//
// remote is a free-form label for where the call came from, because not every
// transport has an address: an HTTP peer passes RemoteAddr, and a message
// transport passes whatever identifies its far end. It is only read to decide
// whether the peer is loopback, which is trusted same-host chatter.
//
// Callers invoke this only for non-public methods. Public methods (e.g. health
// checks and runner Join, which are public precisely because they carry no
// caller identity) are intentionally left out of the audit trail rather than
// logging identity-less lines for them.
func logAccess(ctx context.Context, log *slog.Logger, remote string, mm Method, outcome string, extra ...any) {
	var subject, method string
	if id := IdentityFromContext(ctx); id != nil {
		subject = id.Subject
		method = string(id.Method)
	}

	// Denials are security-relevant wherever they come from, so they always
	// surface at Warn. A successful call from a network peer is the audit
	// signal we care about (the MIR-1323 exfil was a remote read) and logs at
	// Info; the same call over loopback is trusted same-host component chatter
	// and drops to Debug. See isLoopbackAddr.
	level := slog.LevelInfo
	switch {
	case outcome != "ok":
		level = slog.LevelWarn
	case isLoopbackAddr(remote):
		level = slog.LevelDebug
	}

	attrs := []any{
		"remote", remote,
		"rpc", mm.InterfaceName + "." + mm.Name,
		"subject", subject,
		"auth_method", method,
		"outcome", outcome,
	}
	attrs = append(attrs, extra...)

	log.Log(ctx, level, "rpc access", attrs...)
}

// logAuthReject records a rejected capability-signature request (a bad, expired,
// or forged rpc-signature on the ed25519 capability path in authRequest). These
// rejections happen before any Identity is established, so they don't flow
// through logAccess, but a forged or replayed capability is exactly the kind of
// event the audit trail exists to capture, so it must not be confined to the
// general log. Always Warn, since every one of these is a failed auth attempt.
func logAuthReject(log *slog.Logger, remote string, oid OID, reason string) {
	log.Warn("auth rejected",
		"remote", remote,
		"oid", string(oid),
		"reason", reason,
	)
}
