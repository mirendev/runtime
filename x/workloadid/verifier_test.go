package workloadid

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testKID      = "test-key-1"
	testAudience = "https://service.example"
)

// fakeIssuer is a cluster: a discovery document, a JWKS, and a signing key.
type fakeIssuer struct {
	// The JWKS handler runs on the server's goroutine while the rotation test
	// swaps the key from the test's, so the mutable fields need a lock.
	mu     sync.Mutex
	srv    *httptest.Server
	key    *rsa.PrivateKey
	kid    string
	issuer string
	// issuerOverride makes the discovery document claim a different issuer,
	// for the "points at someone else's keys" case.
	issuerOverride string
	// extraKeys are published alongside the signing key.
	extraKeys []jose.JSONWebKey
	jwksCalls int
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	f := &fakeIssuer{key: generateKey(t), kid: testKID}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		iss := f.issuer
		if f.issuerOverride != "" {
			iss = f.issuerOverride
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(discovery{Issuer: iss, JwksURI: f.issuer + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.jwksCalls++
		keys := append([]jose.JSONWebKey{{Key: f.key.Public(), KeyID: f.kid, Algorithm: "RS256", Use: "sig"}}, f.extraKeys...)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: keys})
	})

	f.srv = httptest.NewServer(mux)
	f.issuer = f.srv.URL
	t.Cleanup(f.srv.Close)
	return f
}

func generateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func (f *fakeIssuer) rotate(t *testing.T, kid string) {
	t.Helper()
	key := generateKey(t)
	f.mu.Lock()
	f.key, f.kid = key, kid
	f.mu.Unlock()
}

func (f *fakeIssuer) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jwksCalls
}

func (f *fakeIssuer) token(t *testing.T, mutate func(jwt.MapClaims)) string {
	t.Helper()
	f.mu.Lock()
	key, kid, issuer := f.key, f.kid, f.issuer
	f.mu.Unlock()
	claims := jwt.MapClaims{
		"iss":             issuer,
		"sub":             "org:miren:app:mirendev:sandbox:sbx_1",
		"aud":             testAudience,
		"app":             "mirendev",
		"cluster_id":      "prod",
		"organization_id": "miren",
		"sandbox_id":      "sbx_1",
		"identity_type":   "sandbox",
		"role":            "writer",
		"iat":             time.Now().Unix(),
		"exp":             time.Now().Add(time.Hour).Unix(),
	}
	if mutate != nil {
		mutate(claims)
	}
	return signToken(t, claims, key, kid)
}

func signToken(t *testing.T, claims jwt.MapClaims, key *rsa.PrivateKey, kid string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func newTestVerifier(t *testing.T, f *fakeIssuer, org string) *Verifier {
	t.Helper()
	v, err := NewVerifier(VerifierConfig{
		TrustedIssuers:      []string{f.issuer},
		Audience:            testAudience,
		RequireOrganization: org,
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func TestVerifyAcceptsAGoodToken(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	c, err := v.Verify(context.Background(), f.token(t, nil))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.App != "mirendev" || c.OrganizationID != "miren" || c.ClusterID != "prod" ||
		c.SandboxID != "sbx_1" || c.IdentityType != IdentityTypeSandbox || c.Role != "writer" {
		t.Errorf("claims = %+v", c)
	}
	if c.Issuer != f.issuer || c.ExpiresAt == nil {
		t.Errorf("registered claims = %+v", c.RegisteredClaims)
	}
}

// The classic confusion attack: nominate HMAC and hand over a signature made
// with the issuer's public key, which the attacker also has.
func TestVerifyRejectsAlgorithmConfusion(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	pub, err := jose.JSONWebKey{Key: f.key.Public()}.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": f.issuer, "aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = f.kid
	forged, err := tok.SignedString(pub)
	if err != nil {
		t.Fatalf("sign forged: %v", err)
	}

	if _, err := v.Verify(context.Background(), forged); err == nil {
		t.Fatal("an HMAC-signed token was accepted")
	}
}

// "alg": "none" must never verify.
func TestVerifyRejectsUnsignedToken(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"iss": f.issuer, "aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = f.kid
	unsigned, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}

	if _, err := v.Verify(context.Background(), unsigned); err == nil {
		t.Fatal("an unsigned token was accepted")
	}
}

// A token from a cluster we never named must be refused without a network call.
func TestVerifyRejectsUntrustedIssuer(t *testing.T) {
	trusted := newFakeIssuer(t)
	stranger := newFakeIssuer(t)
	v := newTestVerifier(t, trusted, "")

	if _, err := v.Verify(context.Background(), stranger.token(t, nil)); err == nil {
		t.Fatal("a token from an untrusted issuer was accepted")
	}
	if stranger.calls() != 0 {
		t.Error("an untrusted issuer's key set was fetched")
	}
}

// A token minted for somebody else must not work on us, even though it is
// genuinely signed by a cluster we trust.
func TestVerifyRejectsWrongAudience(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	tok := f.token(t, func(c jwt.MapClaims) { c["aud"] = "https://someone-else.example" })
	_, err := v.Verify(context.Background(), tok)
	if err == nil {
		t.Fatal("a token minted for another audience was accepted")
	}
	if !strings.Contains(err.Error(), "someone-else.example") {
		t.Errorf("error %q should name the audience presented", err)
	}
	if !errors.Is(err, jwt.ErrTokenInvalidAudience) {
		t.Errorf("error %q should wrap jwt.ErrTokenInvalidAudience", err)
	}
}

func TestVerifyAcceptsAudienceInAList(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	tok := f.token(t, func(c jwt.MapClaims) {
		c["aud"] = []any{"https://other.example", testAudience}
	})
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	tok := f.token(t, func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() })
	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("an expired token was accepted")
	}
}

// Expiry is mandatory: a token that never expires is a bearer credential.
func TestVerifyRejectsTokenWithoutExpiry(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	tok := f.token(t, func(c jwt.MapClaims) { delete(c, "exp") })
	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("a token with no expiry was accepted")
	}
}

func TestVerifyRejectsTokenWithoutKID(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	signed := signToken(t, jwt.MapClaims{
		"iss": f.issuer, "aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
	}, f.key, "")
	if _, err := v.Verify(context.Background(), signed); err == nil {
		t.Fatal("a token with no kid was accepted")
	}
}

// A trusted issuer URL must not be usable to serve somebody else's keys.
func TestVerifyRejectsDiscoveryIssuerMismatch(t *testing.T) {
	f := newFakeIssuer(t)
	f.issuerOverride = "https://evil.example"
	v := newTestVerifier(t, f, "")

	if _, err := v.Verify(context.Background(), f.token(t, nil)); err == nil {
		t.Fatal("a discovery document claiming another issuer was accepted")
	}
}

// Defence in depth: enforced only when configured.
func TestRequireOrganization(t *testing.T) {
	f := newFakeIssuer(t)

	if _, err := newTestVerifier(t, f, "miren").Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Errorf("matching organization was rejected: %v", err)
	}

	if _, err := newTestVerifier(t, f, "someone-else").Verify(context.Background(), f.token(t, nil)); err == nil {
		t.Error("a token from another organization was accepted")
	}

	// Unset means unenforced, which is the default.
	if _, err := newTestVerifier(t, f, "").Verify(context.Background(),
		f.token(t, func(c jwt.MapClaims) { delete(c, "organization_id") })); err != nil {
		t.Errorf("organization check ran while unconfigured: %v", err)
	}
}

// A rotated signing key must recover without a restart.
func TestVerifyRefetchesKeysOnRotation(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("warm: %v", err)
	}
	callsAfterWarm := f.calls()

	f.rotate(t, "test-key-2")

	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("Verify after rotation: %v", err)
	}
	if f.calls() <= callsAfterWarm {
		t.Error("the key set was not refetched after rotation")
	}
}

// A pattern in the trusted set breaks the invariant that issuer implies
// organization, so it is refused at construction rather than at request time.
func TestNewVerifierRejectsPatternIssuers(t *testing.T) {
	if _, err := NewVerifier(VerifierConfig{
		TrustedIssuers: []string{"https://*.miren.systems"},
		Audience:       testAudience,
	}); err == nil {
		t.Error("a wildcard issuer was accepted")
	}
}

func TestNewVerifierRequiresAudienceAndIssuers(t *testing.T) {
	if _, err := NewVerifier(VerifierConfig{TrustedIssuers: []string{"https://a.example"}}); err == nil {
		t.Error("a verifier with no audience was accepted")
	}
	if _, err := NewVerifier(VerifierConfig{Audience: testAudience}); err == nil {
		t.Error("a verifier with no trusted issuers was accepted")
	}
}

func TestVerifyRequestReadsBearerToken(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+f.token(t, nil))
	if _, err := v.VerifyRequest(req); err != nil {
		t.Errorf("VerifyRequest: %v", err)
	}

	for _, tc := range []struct{ name, header string }{
		{"missing", ""},
		{"not bearer", "Basic abc"},
		{"empty bearer", "Bearer "},
		{"garbage", "Bearer not-a-jwt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			if _, err := v.VerifyRequest(r); err == nil {
				t.Error("accepted a request without a valid bearer token")
			}
		})
	}
}

// A symmetric key published in a JWKS is public by definition, so anyone can
// mint a valid HS256 signature with it. fetchJWKS drops the key on arrival and
// the algorithm allowlist refuses HMAC at the parser; either alone stops this.
func TestVerifyRejectsSymmetricKeyForgery(t *testing.T) {
	secret := []byte("a symmetric key that should never be in a JWKS")
	const symKID = "sym-1"

	f := newFakeIssuer(t)
	f.extraKeys = []jose.JSONWebKey{{Key: secret, KeyID: symKID, Algorithm: "HS256", Use: "sig"}}
	v := newTestVerifier(t, f, "")

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": f.issuer, "aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = symKID
	forged, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := v.Verify(context.Background(), forged); err == nil {
		t.Fatal("a token signed with a published symmetric key was accepted")
	}
	// The issuer's real key still works alongside the dropped one.
	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Errorf("a good token was rejected after a symmetric key was dropped: %v", err)
	}
}

// Pins the fetch-time barrier on its own, since the allowlist would otherwise
// mask its removal.
func TestFetchJWKSDropsSymmetricKeys(t *testing.T) {
	f := newFakeIssuer(t)
	f.extraKeys = []jose.JSONWebKey{{Key: []byte("secret"), KeyID: "sym-1", Algorithm: "HS256", Use: "sig"}}

	jwks, err := NewValidator().fetchJWKS(context.Background(), f.issuer+"/jwks")
	if err != nil {
		t.Fatalf("fetchJWKS: %v", err)
	}
	if len(jwks.Keys) != 1 || jwks.Keys[0].KeyID != testKID {
		t.Errorf("kept keys = %+v, want only %q", jwks.Keys, testKID)
	}
}

// A stream of unverifiable tokens must not turn into a stream of outbound JWKS
// fetches. The first failure is allowed one refetch; the rest ride the floor.
func TestVerifyBoundsRefetchOnBadSignatures(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	if _, err := v.Verify(context.Background(), f.token(t, nil)); err != nil {
		t.Fatalf("warm: %v", err)
	}
	forged := signToken(t, jwt.MapClaims{
		"iss": f.issuer, "aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
	}, generateKey(t), testKID)

	before := f.calls()
	for range 10 {
		if _, err := v.Verify(context.Background(), forged); err == nil {
			t.Fatal("a forged signature verified")
		}
	}
	if got := f.calls() - before; got > 1 {
		t.Errorf("%d key-set fetches for 10 bad signatures, want at most 1", got)
	}
}

// The verifier runs on a different host from the issuer, which stamps nbf at
// the moment it mints, so a few seconds of clock skew must not refuse a fresh
// token. Skew well past the leeway still does.
func TestVerifyToleratesClockSkew(t *testing.T) {
	f := newFakeIssuer(t)
	v := newTestVerifier(t, f, "")

	ahead := f.token(t, func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(5 * time.Second).Unix() })
	if _, err := v.Verify(context.Background(), ahead); err != nil {
		t.Errorf("a token minted by a clock 5s ahead was refused: %v", err)
	}

	future := f.token(t, func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(10 * time.Minute).Unix() })
	if _, err := v.Verify(context.Background(), future); err == nil {
		t.Error("a token not valid for another 10 minutes was accepted")
	}
}
