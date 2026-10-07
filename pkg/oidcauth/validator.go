package oidcauth

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"miren.dev/runtime/x/workloadid"
)

// Validator validates OIDC tokens by performing discovery and JWKS-based
// verification. It adapts workloadid.Validator to the Claims this package
// matches bindings against.
type Validator struct {
	v *workloadid.Validator
}

// NewValidator creates a new OIDC token validator.
func NewValidator() *Validator {
	// No RequireKeyID: bindings can name any third-party issuer, and some sign
	// without a kid.
	return &Validator{v: workloadid.NewValidator()}
}

// ValidateToken validates an OIDC JWT token against the expected issuer and audience.
// It performs OIDC discovery and JWKS verification automatically.
func (v *Validator) ValidateToken(ctx context.Context, tokenString, expectedIssuer, expectedAudience string) (*Claims, error) {
	mapClaims := jwt.MapClaims{}
	if err := v.v.Validate(ctx, tokenString, expectedIssuer, expectedAudience, mapClaims); err != nil {
		return nil, err
	}
	return mapClaimsToClaims(mapClaims), nil
}

func mapClaimsToClaims(mc jwt.MapClaims) *Claims {
	c := &Claims{
		Extra: make(map[string]any),
	}

	if iss, ok := mc["iss"].(string); ok {
		c.Issuer = iss
	}
	if sub, ok := mc["sub"].(string); ok {
		c.Subject = sub
	}

	// Parse audience
	switch v := mc["aud"].(type) {
	case string:
		c.Audience = []string{v}
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok {
				c.Audience = append(c.Audience, s)
			}
		}
	}

	if exp, ok := mc["exp"].(float64); ok {
		c.Expiry = time.Unix(int64(exp), 0)
	}

	// Copy all extra claims
	for k, val := range mc {
		switch k {
		case "iss", "sub", "aud", "exp", "nbf", "iat", "jti":
			continue
		}
		c.Extra[k] = val
	}

	return c
}
