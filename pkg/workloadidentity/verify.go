package workloadidentity

import (
	"crypto"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// VerifyToken checks a token minted by this issuer and returns its claims.
//
// This is the in-process verification path, for callers that live in the same
// process as the signing key (the coordinator). It reads the public keys
// directly and performs no network I/O, so it neither depends on nor races the
// JWKS endpoint. Verifiers in other processes should fetch JWKS over the
// issuer's discovery document instead — see pkg/oidcauth's Validator.
//
// expectedAudience is required. A token is only accepted if it names that
// audience, which is what stops a token minted for one service being replayed
// against another.
func (iss *Issuer) VerifyToken(tokenString, expectedAudience string) (*WorkloadClaims, error) {
	if expectedAudience == "" {
		return nil, fmt.Errorf("an expected audience is required to verify a token")
	}

	parser := jwt.NewParser(
		// Pin the accepted algorithms to the ones this issuer actually signs
		// with. Without this a token could nominate its own algorithm, which is
		// the classic JWT confusion attack.
		jwt.WithValidMethods(iss.supportedAlgs()),
		jwt.WithIssuer(iss.issuerURL),
		jwt.WithAudience(expectedAudience),
		jwt.WithExpirationRequired(),
	)

	claims := &WorkloadClaims{}
	if _, err := parser.ParseWithClaims(tokenString, claims, iss.keyForToken); err != nil {
		return nil, fmt.Errorf("verifying token: %w", err)
	}

	return claims, nil
}

// VerifySystemWorkloadToken verifies a token and additionally requires that it
// identifies the expected Miren-owned system workload rather than a customer
// workload or a different system workload.
//
// Services that only system workloads may reach should call this rather than
// VerifyToken, so neither the identity type nor workload authorization can be
// forgotten at a call site. Sandbox tokens are signed by the same key and will
// verify cleanly; these claims are the only thing separating them.
func (iss *Issuer) VerifySystemWorkloadToken(tokenString, expectedAudience string, expectedWorkload SystemWorkload) (*WorkloadClaims, error) {
	if !expectedWorkload.valid() {
		return nil, fmt.Errorf("unknown expected system workload %q", expectedWorkload)
	}

	claims, err := iss.VerifyToken(tokenString, expectedAudience)
	if err != nil {
		return nil, err
	}

	if claims.IdentityType != IdentityTypeSystem {
		return nil, fmt.Errorf("token is not a system workload identity (identity_type %q)", claims.IdentityType)
	}
	if !claims.SystemWorkload.valid() {
		return nil, fmt.Errorf("system workload token carries unknown workload %q", claims.SystemWorkload)
	}
	if claims.SystemWorkload != expectedWorkload {
		return nil, fmt.Errorf("token identifies system workload %q, expected %q", claims.SystemWorkload, expectedWorkload)
	}

	return claims, nil
}

// keyForToken resolves the public key a token was signed with by matching its
// kid header against the issuer's live and advertised keys. A token without a
// kid is refused: this issuer stamps one on everything it signs, so a missing
// kid means the token did not come from here.
func (iss *Issuer) keyForToken(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, fmt.Errorf("token has no kid header")
	}

	if pub := iss.publicKeyByID(kid); pub != nil {
		return pub, nil
	}

	return nil, fmt.Errorf("no known signing key for kid %q", kid)
}

// publicKeyByID returns the public key with the given key ID, or nil. Live
// signing keys are searched before advertised (verify-only) ones, though key
// IDs are content-derived so the two sets cannot disagree about a key.
func (iss *Issuer) publicKeyByID(kid string) crypto.PublicKey {
	for _, k := range iss.keys {
		if k.kid == kid {
			return k.public
		}
	}
	for _, jwk := range iss.advertised {
		if jwk.KeyID == kid {
			return jwk.Key
		}
	}
	return nil
}
