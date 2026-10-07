package workloadid

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// signingAlgs are the asymmetric algorithms a token may be signed with. Listing
// them is what stops a token from nominating "none", or nominating HMAC and
// having the issuer's public key used as the shared secret.
//
// Removing this leaves the tests green, because fetchJWKS already refuses to
// let a symmetric key reach the parser. Keep it anyway: the two barriers are
// independent, and this one holds if that one is ever relaxed.
var signingAlgs = []string{
	"RS256", "RS384", "RS512",
	"ES256", "ES384", "ES512",
	"PS256", "PS384", "PS512",
	"EdDSA",
}

const (
	discoveryTTL = time.Hour
	jwksTTL      = time.Hour
	// minRefreshInterval is the floor between forced key-set refetches. A
	// signature we cannot verify is the shape of a key rotation, but it is also
	// the shape of a forged token, and refetching per request would turn an
	// attacker's garbage into our outbound traffic.
	minRefreshInterval = time.Minute
	// clockSkewLeeway absorbs clock differences on exp, nbf and iat. A
	// verifier runs on a different host from the issuer, and Miren's issuer
	// sets nbf to the moment it mints, so a verifier a second behind would
	// otherwise refuse a token it was just handed.
	clockSkewLeeway = 60 * time.Second
	fetchTimeout    = 10 * time.Second
	maxBodyBytes    = 1 << 20
)

type discovery struct {
	Issuer  string `json:"issuer"`
	JwksURI string `json:"jwks_uri"`
}

type issuerState struct {
	discovery     *discovery
	discoveryTime time.Time
	jwks          *jose.JSONWebKeySet
	jwksTime      time.Time
	lastRefresh   time.Time
}

// Validator verifies OIDC JWTs from any issuer, fetching and caching each
// issuer's discovery document and key set.
//
// It knows nothing about which issuers to trust; callers pass the issuer they
// expect on every call. For Miren workload identity tokens, use [Verifier],
// which adds a trusted-issuer list and Miren's claims on top.
type Validator struct {
	client     *http.Client
	requireKID bool

	mu    sync.RWMutex
	state map[string]*issuerState
}

// ValidatorOption configures a [Validator].
type ValidatorOption func(*Validator)

// WithHTTPClient sets the client used to fetch discovery documents and key
// sets. The default has a 10 second timeout.
func WithHTTPClient(c *http.Client) ValidatorOption {
	return func(v *Validator) { v.client = c }
}

// RequireKeyID refuses tokens without a kid header. Without it, a token with no
// kid is checked against the issuer's only key, or a key whose algorithm
// matches, which some third-party issuers rely on.
func RequireKeyID() ValidatorOption {
	return func(v *Validator) { v.requireKID = true }
}

// NewValidator returns a Validator with an empty cache.
func NewValidator(opts ...ValidatorOption) *Validator {
	v := &Validator{
		client: &http.Client{Timeout: fetchTimeout},
		state:  make(map[string]*issuerState),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Validate checks that token was signed by issuer, has not expired, and names
// audience, then decodes its claims into claims (a jwt.MapClaims or a struct
// embedding jwt.RegisteredClaims).
//
// An unknown key ID or bad signature triggers one refetch of the issuer's key
// set, since that is what a key rotation looks like, but no more than once a
// minute per issuer.
func (v *Validator) Validate(ctx context.Context, token, issuer, audience string, claims jwt.Claims) error {
	if audience == "" {
		return errors.New("an expected audience is required")
	}

	err := v.parse(ctx, token, issuer, claims, false)
	if err != nil && (errors.Is(err, jwt.ErrTokenUnverifiable) || errors.Is(err, jwt.ErrTokenSignatureInvalid)) {
		if v.mayRefresh(issuer) {
			err = v.parse(ctx, token, issuer, claims, true)
		}
	}
	if err != nil {
		return err
	}

	// Checked here rather than with jwt.WithAudience so the error names the
	// audience presented, which is most of what a misconfigured caller needs.
	aud, err := claims.GetAudience()
	if err != nil {
		return fmt.Errorf("reading token audience: %w", err)
	}
	if !slices.Contains(aud, audience) {
		return fmt.Errorf("token audience %v does not include %q: %w", []string(aud), audience, jwt.ErrTokenInvalidAudience)
	}
	return nil
}

func (v *Validator) parse(ctx context.Context, token, issuer string, claims jwt.Claims, refresh bool) error {
	keyFunc, err := v.keyFunc(ctx, issuer, refresh)
	if err != nil {
		return fmt.Errorf("resolving keys for %s: %w", issuer, err)
	}

	parser := jwt.NewParser(
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods(signingAlgs),
		jwt.WithLeeway(clockSkewLeeway),
	)
	if _, err := parser.ParseWithClaims(token, claims, keyFunc); err != nil {
		return fmt.Errorf("token rejected: %w", err)
	}
	return nil
}

// mayRefresh reports whether enough time has passed to force another key-set
// fetch for this issuer, and records the attempt when it allows one.
func (v *Validator) mayRefresh(issuer string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	st := v.state[issuer]
	if st == nil {
		return true
	}
	if time.Since(st.lastRefresh) < minRefreshInterval {
		return false
	}
	st.lastRefresh = time.Now()
	return true
}

func (v *Validator) keyFunc(ctx context.Context, issuer string, refresh bool) (jwt.Keyfunc, error) {
	v.mu.RLock()
	st := v.state[issuer]
	if st != nil && st.jwks != nil && !refresh && time.Since(st.jwksTime) < jwksTTL {
		jwks := st.jwks
		v.mu.RUnlock()
		return v.keyFuncFor(jwks), nil
	}
	v.mu.RUnlock()

	doc, err := v.getDiscovery(ctx, issuer)
	if err != nil {
		return nil, err
	}
	jwks, err := v.fetchJWKS(ctx, doc.JwksURI)
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	st = v.stateFor(issuer)
	st.jwks = jwks
	st.jwksTime = time.Now()
	return v.keyFuncFor(jwks), nil
}

// stateFor returns the cache entry for issuer, creating it. Callers hold v.mu.
func (v *Validator) stateFor(issuer string) *issuerState {
	st := v.state[issuer]
	if st == nil {
		st = &issuerState{}
		v.state[issuer] = st
	}
	return st
}

func (v *Validator) getDiscovery(ctx context.Context, issuer string) (*discovery, error) {
	v.mu.RLock()
	if st := v.state[issuer]; st != nil && st.discovery != nil && time.Since(st.discoveryTime) < discoveryTTL {
		doc := st.discovery
		v.mu.RUnlock()
		return doc, nil
	}
	v.mu.RUnlock()

	url := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	var doc discovery
	if err := v.getJSON(ctx, url, &doc); err != nil {
		return nil, fmt.Errorf("fetching discovery document: %w", err)
	}

	// The document has to agree about whose it is, or an issuer URL could be
	// pointed at somebody else's keys.
	if strings.TrimRight(doc.Issuer, "/") != strings.TrimRight(issuer, "/") {
		return nil, fmt.Errorf("discovery document claims issuer %q, expected %q", doc.Issuer, issuer)
	}
	if doc.JwksURI == "" {
		return nil, errors.New("discovery document has no jwks_uri")
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	st := v.stateFor(issuer)
	st.discovery = &doc
	st.discoveryTime = time.Now()
	return &doc, nil
}

// fetchJWKS fetches a key set and keeps only its asymmetric public keys.
//
// A key published in a JWKS is public by definition, so a symmetric one would
// let anyone produce a valid HMAC signature. signingAlgs already refuses HMAC
// at the parser; dropping such keys here means they never reach it either.
// They are dropped rather than failing the whole set so one malformed entry
// from a third-party issuer doesn't take its good keys down with it.
func (v *Validator) fetchJWKS(ctx context.Context, uri string) (*jose.JSONWebKeySet, error) {
	var raw jose.JSONWebKeySet
	if err := v.getJSON(ctx, uri, &raw); err != nil {
		return nil, fmt.Errorf("fetching JWKS: %w", err)
	}

	jwks := &jose.JSONWebKeySet{}
	for _, k := range raw.Keys {
		switch k.Key.(type) {
		case *rsa.PublicKey, *ecdsa.PublicKey, ed25519.PublicKey:
			jwks.Keys = append(jwks.Keys, k)
		}
	}
	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("JWKS has no asymmetric public keys (%d keys published)", len(raw.Keys))
	}
	return jwks, nil
}

func (v *Validator) getJSON(ctx context.Context, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned status %d", url, resp.StatusCode)
	}
	// Bounded so a hostile or broken endpoint cannot make us read forever.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, into)
}

func (v *Validator) keyFuncFor(jwks *jose.JSONWebKeySet) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid != "" {
			keys := jwks.Key(kid)
			if len(keys) == 0 {
				return nil, fmt.Errorf("no key %q in JWKS: %w", kid, jwt.ErrTokenUnverifiable)
			}
			return keys[0].Key, nil
		}
		if v.requireKID {
			return nil, errors.New("token has no kid header")
		}

		if len(jwks.Keys) == 1 {
			return jwks.Keys[0].Key, nil
		}
		alg, _ := token.Header["alg"].(string)
		for _, k := range jwks.Keys {
			if k.Algorithm == alg {
				return k.Key, nil
			}
		}
		return nil, fmt.Errorf("no kid header and no key matches alg %q among %d keys: %w", alg, len(jwks.Keys), jwt.ErrTokenUnverifiable)
	}
}

// PeekIssuer reads a token's iss claim without verifying anything. The value
// is only good for choosing which issuer to verify against; it means nothing
// until that issuer's keys have verified the token.
func PeekIssuer(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decoding claims: %w", err)
	}
	var c struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return "", fmt.Errorf("parsing claims: %w", err)
	}
	if c.Issuer == "" {
		return "", errors.New("token has no issuer claim")
	}
	return c.Issuer, nil
}
