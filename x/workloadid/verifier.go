package workloadid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// APIAudience is the audience of the identity token mounted into every sandbox,
// and the only one a cluster's own API accepts. Tokens for anything else should
// be minted with that service's own audience.
const APIAudience = "miren"

// IdentityType names the kind of principal a token represents.
type IdentityType string

const (
	// IdentityTypeSandbox is a customer workload running in a sandbox.
	IdentityTypeSandbox IdentityType = "sandbox"
	// IdentityTypeSystem is a Miren-owned workload, named by SystemWorkload.
	IdentityTypeSystem IdentityType = "system"
)

// Claims is the payload of a Miren workload identity token.
type Claims struct {
	jwt.RegisteredClaims
	OrganizationID string       `json:"organization_id,omitempty"`
	ClusterID      string       `json:"cluster_id,omitempty"`
	App            string       `json:"app,omitempty"`
	SandboxID      string       `json:"sandbox_id"`
	IdentityType   IdentityType `json:"identity_type,omitempty"`
	// SystemWorkload names the Miren-owned workload a system token identifies,
	// such as "telemetrywriter". Empty on sandbox tokens.
	SystemWorkload string `json:"system_workload,omitempty"`
	// Role is the authorization role the cluster resolved for the sandbox's
	// app. Only sandbox tokens carry it.
	Role string `json:"role,omitempty"`
}

// VerifierConfig describes who a [Verifier] is and whom it trusts.
type VerifierConfig struct {
	// TrustedIssuers are the issuer URLs of the clusters whose tokens are
	// accepted. They must be exact URLs, never patterns; see
	// RequireOrganization for why.
	TrustedIssuers []string

	// Audience is the value callers must mint their tokens for: this service.
	Audience string

	// RequireOrganization, when set, also requires the token's
	// organization_id to match.
	//
	// While TrustedIssuers is a list of exact URLs this is defence in depth.
	// Each issuer belongs to one cluster, which belongs to one organization,
	// so pinning the issuer already pins the organization. That stops being
	// true the moment the trusted set holds a pattern or a shared multi-tenant
	// anchor, and then this check is the one doing the work.
	RequireOrganization string

	// Validator does the underlying verification. Leave it nil for a fresh
	// one that requires a kid header; pass one to share its key cache or
	// HTTP client.
	Validator *Validator
}

// Verifier checks Miren workload identity tokens from a fixed set of trusted
// clusters.
type Verifier struct {
	validator  *Validator
	trusted    map[string]bool
	audience   string
	requireOrg string
}

// NewVerifier validates cfg and returns a Verifier.
func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	if cfg.Audience == "" {
		return nil, errors.New("workloadid: an audience is required")
	}
	trusted := make(map[string]bool, len(cfg.TrustedIssuers))
	for _, iss := range cfg.TrustedIssuers {
		iss = strings.TrimRight(strings.TrimSpace(iss), "/")
		if iss == "" {
			continue
		}
		if strings.ContainsAny(iss, "*?") {
			return nil, fmt.Errorf("workloadid: issuer %q looks like a pattern; trust exact URLs only", iss)
		}
		trusted[iss] = true
	}
	if len(trusted) == 0 {
		return nil, errors.New("workloadid: at least one trusted issuer is required")
	}

	v := cfg.Validator
	if v == nil {
		// Miren's issuer stamps a kid on everything it signs, so a token
		// without one did not come from a cluster we trust.
		v = NewValidator(RequireKeyID())
	}
	return &Verifier{
		validator:  v,
		trusted:    trusted,
		audience:   cfg.Audience,
		requireOrg: cfg.RequireOrganization,
	}, nil
}

// Verify checks a token and returns its claims.
//
// The issuer is read from the token before anything is verified, because it
// decides which key set the signature is checked against. That value is
// untrusted: it only selects one of the issuers already in the trusted set, and
// a token naming any other issuer is refused without a network call.
func (v *Verifier) Verify(ctx context.Context, token string) (*Claims, error) {
	issuer, err := PeekIssuer(token)
	if err != nil {
		return nil, fmt.Errorf("reading token issuer: %w", err)
	}
	if !v.trusted[strings.TrimRight(issuer, "/")] {
		return nil, fmt.Errorf("issuer %q is not trusted", issuer)
	}

	claims := &Claims{}
	if err := v.validator.Validate(ctx, token, issuer, v.audience, claims); err != nil {
		return nil, err
	}

	if v.requireOrg != "" && claims.OrganizationID != v.requireOrg {
		return nil, fmt.Errorf("token organization %q is not %q", claims.OrganizationID, v.requireOrg)
	}
	return claims, nil
}

// VerifyRequest verifies the bearer token in r's Authorization header.
func (v *Verifier) VerifyRequest(r *http.Request) (*Claims, error) {
	token, err := BearerToken(r)
	if err != nil {
		return nil, err
	}
	return v.Verify(r.Context(), token)
}

// BearerToken returns the token from r's "Authorization: Bearer" header.
func BearerToken(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", errors.New("no Authorization header")
	}
	scheme, token, ok := strings.Cut(h, " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", errors.New("no bearer token in the Authorization header")
	}
	return token, nil
}
