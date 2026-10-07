package workloadid

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Third-party issuers may sign without a kid. By default the Validator falls
// back to the issuer's only key rather than refusing.
func TestValidateWithoutKIDUsesTheOnlyKey(t *testing.T) {
	f := newFakeIssuer(t)

	signed := signToken(t, jwt.MapClaims{
		"iss": f.issuer, "aud": testAudience, "sub": "repo:acme/web",
		"exp": time.Now().Add(time.Hour).Unix(),
	}, f.key, "")

	claims := jwt.MapClaims{}
	if err := NewValidator().Validate(context.Background(), signed, f.issuer, testAudience, claims); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims["sub"] != "repo:acme/web" {
		t.Errorf("claims = %v", claims)
	}

	if err := NewValidator(RequireKeyID()).Validate(context.Background(), signed, f.issuer, testAudience, jwt.MapClaims{}); err == nil {
		t.Error("RequireKeyID accepted a token with no kid")
	}
}

// The caller names the issuer; a token from a different one must not verify
// even if the caller's issuer is reachable.
func TestValidateRejectsIssuerMismatch(t *testing.T) {
	a := newFakeIssuer(t)
	b := newFakeIssuer(t)

	if err := NewValidator().Validate(context.Background(), a.token(t, nil), b.issuer, testAudience, jwt.MapClaims{}); err == nil {
		t.Fatal("a token was accepted against an issuer that did not sign it")
	}
}

func TestValidateRequiresAnAudience(t *testing.T) {
	f := newFakeIssuer(t)
	if err := NewValidator().Validate(context.Background(), f.token(t, nil), f.issuer, "", jwt.MapClaims{}); err == nil {
		t.Fatal("Validate accepted an empty expected audience")
	}
}

func TestPeekIssuer(t *testing.T) {
	f := newFakeIssuer(t)
	iss, err := PeekIssuer(f.token(t, nil))
	if err != nil || iss != f.issuer {
		t.Errorf("PeekIssuer = %q, %v; want %q", iss, err, f.issuer)
	}

	for _, tok := range []string{"", "a.b", "a.!!!.c", f.token(t, func(c jwt.MapClaims) { delete(c, "iss") })} {
		if _, err := PeekIssuer(tok); err == nil {
			t.Errorf("PeekIssuer(%q) succeeded", tok)
		}
	}
}
