package workloadidentity

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// PeekSandboxClaims reads the app and role from a token WITHOUT verifying its
// signature. It is only for recovering values from a token this cluster already
// minted and wrote to local disk — e.g. re-registering a sandbox for token
// refresh after a controller restart, so it keeps the role it was built with
// rather than picking up a role the app was reconfigured to since. Never use it
// to authenticate an inbound token.
func PeekSandboxClaims(tokenString string) (app, role string, ok bool) {
	var claims WorkloadClaims
	if _, _, err := jwt.NewParser().ParseUnverified(strings.TrimSpace(tokenString), &claims); err != nil {
		return "", "", false
	}
	return claims.App, claims.Role, true
}

// clockSkewLeeway absorbs small clock differences on exp/nbf/iat. Tokens are
// minted and verified by the same coordinator process today, so this is
// insurance rather than a live requirement.
const clockSkewLeeway = 60 * time.Second

// APIAudience is the audience carried by the identity token mounted into every
// sandbox, and the only audience accepted for inbound calls to the cluster API.
// Tokens minted for external relying parties (AWS STS and friends) request an
// explicit audience, so they never satisfy this check and cannot be replayed
// against us.
const APIAudience = "miren"

// ErrNoIssuer is returned when a Validator has no usable issuer to verify
// against. It fails closed rather than accepting anything.
var ErrNoIssuer = errors.New("workloadidentity: no issuer configured")

// Validator verifies workload identity tokens minted by this cluster.
//
// It deliberately does not reuse the other JWT paths in the tree: pkg/auth is
// pinned to Ed25519 against Miren Cloud's JWKS, and pkg/oidcauth resolves trust
// through oidc_binding entities and validates the audience against the request
// host. Both model a third-party issuer. This one verifies our own issuer, whose
// keys we already hold in process — no HTTP round-trip to ourselves, and no
// dependency on the ingress being up to authenticate a call.
type Validator struct {
	issuer *Issuer
}

func NewValidator(iss *Issuer) *Validator {
	return &Validator{issuer: iss}
}

// Validate parses and verifies a token, returning its claims. The returned
// claims are safe to derive an identity from: issuer, audience, signature, and
// expiry have all been checked, and the app and sandbox bindings are non-empty.
func (v *Validator) Validate(tokenString string) (*WorkloadClaims, error) {
	if v == nil || v.issuer == nil || v.issuer.IssuerURL() == "" {
		return nil, ErrNoIssuer
	}

	var claims WorkloadClaims

	// WithValidMethods is what rejects "none" and any HMAC algorithm, where the
	// public key we look up by kid would otherwise be usable as a shared secret.
	_, err := jwt.ParseWithClaims(tokenString, &claims, v.keyFunc,
		jwt.WithValidMethods([]string{"RS256", "EdDSA"}),
		jwt.WithIssuer(v.issuer.IssuerURL()),
		jwt.WithAudience(APIAudience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clockSkewLeeway),
	)
	if err != nil {
		return nil, fmt.Errorf("workloadidentity: verifying token: %w", err)
	}

	// jwt.WithAudience only checks that APIAudience is *among* the audiences, so
	// a token minted with aud=["sts.amazonaws.com","miren"] would pass. The token
	// server lets a sandbox request arbitrary audiences, so require the audience
	// to be exactly ["miren"] — otherwise a token handed to an external relying
	// party could be replayed against our API by whoever receives it.
	if len(claims.Audience) != 1 || claims.Audience[0] != APIAudience {
		return nil, fmt.Errorf("workloadidentity: token audience %v is not exactly [%q]", claims.Audience, APIAudience)
	}

	if claims.SandboxID == "" {
		return nil, errors.New("workloadidentity: token has no sandbox_id")
	}

	// An app-less token cannot be scoped to anything: rpc.BoundApp would report
	// no binding, and rpc.AllowApp treats an unbound caller as unscoped and lets
	// it through. Refusing to authenticate here is what keeps that default from
	// turning a token with a missing app claim into a cluster-wide one.
	if claims.App == "" {
		return nil, errors.New("workloadidentity: token has no app")
	}

	return &claims, nil
}

// keyFunc resolves the signing key by kid. Our issuer always sets kid, so a
// token without one is not ours; and matching on anything looser than an exact
// kid would mean guessing which key signed a token across a rotation overlap.
func (v *Validator) keyFunc(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("token has no kid")
	}

	for _, jwk := range v.issuer.VerificationKeys() {
		if jwk.KeyID != kid {
			continue
		}
		// Pin the key to the algorithm it was published for. Without this a
		// mixed RS256/EdDSA key set would let a token nominate the wrong key
		// type for its alg.
		if jwk.Algorithm != token.Method.Alg() {
			return nil, fmt.Errorf("key %q is %s, token is signed %s", kid, jwk.Algorithm, token.Method.Alg())
		}
		return jwk.Key, nil
	}

	return nil, fmt.Errorf("unknown key %q", kid)
}
