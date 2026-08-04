package workloadidentity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadroles"
)

// Authenticator authenticates callers presenting a workload identity token
// minted by this cluster — code running inside a sandbox, using the token
// mounted at MIREN_IDENTITY_TOKEN_PATH.
//
// It takes a concrete *Issuer rather than the TokenIssuer interface on purpose:
// only the coordinator holds the signing key, so only the coordinator can
// verify. Distributed runners hold a proxy issuer that mints over RPC and
// cannot satisfy this, which is correct — sandboxes on a runner still dial the
// coordinator's API, where this authenticator lives.
type Authenticator struct {
	validator *Validator
	issuerURL string
	logger    *slog.Logger
}

var _ rpc.Authenticator = (*Authenticator)(nil)

func NewAuthenticator(iss *Issuer, logger *slog.Logger) *Authenticator {
	return &Authenticator{
		validator: NewValidator(iss),
		issuerURL: iss.IssuerURL(),
		logger:    logger,
	}
}

// Authenticate returns an identity for a valid workload identity token, or
// (nil, nil) for anything else so the rest of the authenticator chain can try.
//
// It never returns an error: the RPC server treats an authenticator error as
// terminal and rejects the request, which would break every other credential
// type the moment a malformed bearer token arrived.
func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request) (*rpc.Identity, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, nil
	}

	tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

	// Claim the token only if it names us as its issuer. No cloud or external
	// OIDC token can carry our issuer URL, so this settles which authenticator
	// owns the token before any signature work happens.
	issuer, err := peekIssuer(tokenString)
	if err != nil || issuer != a.issuerURL {
		return nil, nil
	}

	claims, err := a.validator.Validate(tokenString)
	if err != nil {
		// Ours by issuer but not valid: expired, wrong audience (a token minted
		// for an external relying party), or forged. Nothing else in the chain
		// will accept it either, but returning nil keeps that decision theirs.
		a.logger.Debug("rejected workload identity token", "error", err)
		return nil, nil
	}

	// "app" is what rpc.BoundApp reads to confine the caller. Only a KNOWN
	// cluster-scoped role unconfines it (empty app → reaches every app); the
	// origin app is still recorded under "workload_app" for audit. An unknown
	// role stays confined to its origin app — the authorizer denies it every
	// method anyway, and keeping it confined is the safe default on any path
	// that consults AllowApp without the authorizer (e.g. a public method).
	boundApp := claims.App
	if role, ok := workloadroles.Lookup(claims.Role); ok && role.ClusterScoped {
		boundApp = ""
	}

	return &rpc.Identity{
		Subject: claims.Subject,
		Method:  rpc.AuthMethodWorkload,
		Metadata: map[string]any{
			"app":             boundApp,
			"role":            claims.Role,
			"workload_app":    claims.App,
			"sandbox_id":      claims.SandboxID,
			"organization_id": claims.OrganizationID,
			"cluster_id":      claims.ClusterID,
		},
	}, nil
}

// peekIssuer extracts the issuer claim from a JWT without verifying the
// signature. Only ever used to route a token to the right authenticator; the
// value is untrusted until Validate has checked the signature against it.
func peekIssuer(tokenString string) (string, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("not a valid JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	if claims.Issuer == "" {
		return "", fmt.Errorf("JWT missing issuer claim")
	}

	return claims.Issuer, nil
}
