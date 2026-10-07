package workloadidentity

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	// systemTokenTTL is requested explicitly rather than taking the issuer's
	// default, so the source knows when its own token dies without having to
	// decode it.
	systemTokenTTL = time.Hour

	// systemTokenRefreshLeeway renews this far ahead of expiry, leaving room for
	// a slow mint and for clock skew against whoever verifies the token.
	systemTokenRefreshLeeway = 5 * time.Minute
)

// ErrIssuerUnavailable means no token can be minted because no issuer has been
// wired up yet, or there is none to wire.
var ErrIssuerUnavailable = errors.New("workload identity issuer unavailable")

// TokenSource supplies the token a request carries.
type TokenSource interface {
	Token() (string, error)
}

// SystemTokenSource mints tokens for one system workload and one audience
// through an issuer, holding each until shortly before it expires.
//
// Minting lazily on the request path is deliberate: the caller needs no
// goroutine of its own, and an idle exporter mints nothing.
//
// Its issuer may arrive late. A distributed runner builds its telemetry
// writers before it connects to the coordinator, but its issuer is a remote one
// that only exists once that connection is up, so the writers are handed this
// and the issuer is set behind them. Until that happens Token fails rather than
// returning something unusable, which surfaces as a send failure instead of a
// silent gap.
type SystemTokenSource struct {
	workload SystemWorkload
	audience string

	mu      sync.Mutex
	issuer  TokenIssuer
	token   string
	renewAt time.Time

	// now is swappable for tests.
	now func() time.Time
}

func NewSystemTokenSource(workload SystemWorkload, audience string) *SystemTokenSource {
	return &SystemTokenSource{workload: workload, audience: audience, now: time.Now}
}

// SetIssuer supplies the issuer once there is one. Passing nil leaves the
// source unarmed, which is the honest state when no issuer is configured.
//
// Re-arming drops any cached token: a new issuer means a new connection, and a
// token minted through the old one would keep a stale credential alive past
// the event that replaced it.
func (s *SystemTokenSource) SetIssuer(issuer TokenIssuer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issuer = issuer
	s.token = ""
	s.renewAt = time.Time{}
}

func (s *SystemTokenSource) Token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.issuer == nil {
		return "", ErrIssuerUnavailable
	}

	now := s.now()
	if s.token != "" && now.Before(s.renewAt) {
		return s.token, nil
	}

	token, err := s.issuer.IssueSystemWorkloadToken(s.workload, TokenOptions{
		Audience: []string{s.audience},
		TTL:      systemTokenTTL,
	})
	if err != nil {
		return "", fmt.Errorf("minting %s token for %s: %w", s.workload, s.audience, err)
	}

	s.token = token
	s.renewAt = now.Add(systemTokenTTL - systemTokenRefreshLeeway)

	return token, nil
}

// BearerTransport attaches a token from Source to every request as an
// Authorization bearer, replacing any Authorization the request already
// carries. That replacement is the precedence rule: a caller that configured
// workload identity gets it, even when a static credential is also around.
type BearerTransport struct {
	Base   http.RoundTripper
	Source TokenSource
}

func (t *BearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	token, err := t.Source.Token()
	if err != nil {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, fmt.Errorf("request has no workload token: %w", err)
	}

	// A RoundTripper must not modify the request it is given.
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+token)

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
