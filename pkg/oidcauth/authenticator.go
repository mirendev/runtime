package oidcauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

// OIDCAuthenticator validates external OIDC bearer tokens against configured
// oidc_binding entities in the entity store.
type OIDCAuthenticator struct {
	eac       atomic.Pointer[entityserver_v1alpha.EntityAccessClient]
	validator *Validator
	logger    *slog.Logger
}

// NewOIDCAuthenticator creates a new OIDC authenticator. The entity access client
// must be set via SetEAC before authentication will work.
func NewOIDCAuthenticator(logger *slog.Logger) *OIDCAuthenticator {
	return &OIDCAuthenticator{
		validator: NewValidator(),
		logger:    logger.With("module", "oidc-auth"),
	}
}

// SetEAC sets the entity access client for querying OIDC bindings. This is called
// after the entity store is initialized, since auth is wired before the store.
func (a *OIDCAuthenticator) SetEAC(eac *entityserver_v1alpha.EntityAccessClient) {
	a.eac.Store(eac)
}

// Authenticate checks if the request carries a valid OIDC bearer token that
// matches a configured oidc_binding entity.
func (a *OIDCAuthenticator) Authenticate(ctx context.Context, r *http.Request) (*rpc.Identity, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, nil
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	tokenString = strings.TrimSpace(tokenString)

	eac := a.eac.Load()
	if eac == nil {
		return nil, nil // Entity access client not yet initialized
	}

	// Peek at the token's issuer claim without verifying signature
	issuer, err := peekIssuer(tokenString)
	if err != nil {
		return nil, nil // Not a valid JWT, let other authenticators try
	}

	// Query entity store for oidc_binding entities matching this issuer
	bindings, err := a.listBindingsByIssuer(ctx, issuer)
	if err != nil {
		a.logger.Error("failed to query OIDC bindings", "issuer", issuer, "error", err)
		return nil, nil // Don't block auth on entity store errors
	}

	if len(bindings) == 0 {
		return nil, nil // No bindings for this issuer
	}

	// Determine the audience to validate against.
	// Use the hostname from the request.
	audience := r.Host
	if audience == "" && r.TLS != nil {
		audience = r.TLS.ServerName
	}
	if audience == "" {
		return nil, nil
	}

	// Validate the token
	claims, err := a.validator.ValidateToken(ctx, tokenString, issuer, audience)
	if err != nil {
		a.logger.Debug("OIDC token validation failed", "issuer", issuer, "error", err)
		return nil, nil
	}

	// Check each binding for a match
	for _, binding := range bindings {
		if !claims.MatchesSubjectPattern(binding.SubjectPattern) {
			continue
		}

		conditions := make([]ClaimCondition, len(binding.ClaimConditions))
		for i, cc := range binding.ClaimConditions {
			conditions[i] = ClaimCondition{Key: cc.Key, Pattern: cc.Pattern}
		}

		if !claims.MatchesClaimConditions(conditions) {
			continue
		}

		// Resolve app name from the binding's app ref
		appName := resolveAppName(string(binding.App))

		a.logger.Info("OIDC authentication successful",
			"issuer", issuer,
			"subject", claims.Subject,
			"provider", binding.Provider,
			"bound_app", appName,
		)

		return &rpc.Identity{
			Subject: claims.Subject,
			Method:  rpc.AuthMethodOIDC,
			Metadata: map[string]any{
				"bound_app": appName,
				"provider":  binding.Provider,
			},
		}, nil
	}

	repository, _ := claims.getClaimString("repository")

	a.logger.Info("OIDC token did not match any binding",
		"issuer", issuer,
		"subject", claims.Subject,
		"repository", repository,
	)
	return nil, &BindingMismatchError{
		Issuer:     issuer,
		Subject:    claims.Subject,
		Repository: repository,
	}
}

// BindingMismatchError reports a token that verified against a configured issuer
// but matched none of that issuer's bindings. It carries claims from the token
// the caller presented, so echoing it back tells them nothing they didn't
// already hold. It deliberately says nothing about the bindings themselves.
// Those are cluster configuration, and this error is rendered for a caller who
// is by definition unauthenticated.
type BindingMismatchError struct {
	Issuer     string
	Subject    string
	Repository string
}

var _ rpc.DisclosableAuthError = (*BindingMismatchError)(nil)

// AuthErrorCode implements rpc.DisclosableAuthError, opting this error into
// being returned to the caller rather than collapsing into a bare 401.
func (e *BindingMismatchError) AuthErrorCode() string {
	return rpc.AuthErrorOIDCBindingMismatch
}

func (e *BindingMismatchError) Error() string {
	msg := fmt.Sprintf("OIDC token did not match any CI binding (issuer=%s subject=%s", e.Issuer, e.Subject)
	if e.Repository != "" {
		msg += " repository=" + e.Repository
	}
	return msg + ")"
}

// peekIssuer extracts the issuer claim from a JWT without verifying the signature.
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

func (a *OIDCAuthenticator) listBindingsByIssuer(ctx context.Context, issuer string) ([]core_v1alpha.OidcBinding, error) {
	eac := a.eac.Load()
	if eac == nil {
		return nil, fmt.Errorf("entity access client not initialized")
	}
	listResp, err := eac.List(ctx, entity.String(core_v1alpha.OidcBindingIssuerId, issuer))
	if err != nil {
		return nil, err
	}

	entities := listResp.Values()
	bindings := make([]core_v1alpha.OidcBinding, 0, len(entities))

	for _, e := range entities {
		var b core_v1alpha.OidcBinding
		b.Decode(&rpcEntityAdapter{entity: e})
		bindings = append(bindings, b)
	}

	return bindings, nil
}

// resolveAppName extracts the app name from an entity reference like "app/my-app".
func resolveAppName(appRef string) string {
	if idx := strings.LastIndex(appRef, "/"); idx >= 0 {
		return appRef[idx+1:]
	}
	return appRef
}

// rpcEntityAdapter wraps an RPC entity to implement entity.AttrGetter.
type rpcEntityAdapter struct {
	entity *entityserver_v1alpha.Entity
}

func (w *rpcEntityAdapter) Get(id entity.Id) (entity.Attr, bool) {
	if id == entity.DBId {
		return entity.Ref(entity.DBId, entity.Id(w.entity.Id())), true
	}
	attrs := w.entity.Attrs()
	for _, attr := range attrs {
		if entity.Id(attr.ID) == id {
			return attr, true
		}
	}
	return entity.Attr{}, false
}

func (w *rpcEntityAdapter) GetAll(name entity.Id) []entity.Attr {
	var result []entity.Attr
	attrs := w.entity.Attrs()
	for _, attr := range attrs {
		if entity.Id(attr.ID) == name {
			result = append(result, attr)
		}
	}
	return result
}

func (w *rpcEntityAdapter) Attrs() []entity.Attr {
	return w.entity.Attrs()
}
