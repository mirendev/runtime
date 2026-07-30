package rpc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// contextKey is a private type for context keys to avoid collisions
type contextKey string

const (
	// identityContextKey is the context key for storing the authenticated Identity
	identityContextKey contextKey = "rpc-identity"
)

// IdentityFromContext retrieves the Identity from the context, if present
func IdentityFromContext(ctx context.Context) *Identity {
	if id, ok := ctx.Value(identityContextKey).(*Identity); ok {
		return id
	}
	return nil
}

// ContextWithIdentity returns a new context with the Identity stored
func ContextWithIdentity(ctx context.Context, identity *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, identity)
}

// AuthMethod indicates how a caller was authenticated
type AuthMethod string

const (
	AuthMethodCert      AuthMethod = "cert"      // TLS client certificate
	AuthMethodJWT       AuthMethod = "jwt"       // JWT token (e.g., from Miren Cloud)
	AuthMethodAnonymous AuthMethod = "anonymous" // No authentication (public methods)
	AuthMethodToken     AuthMethod = "token"     // Bearer token (e.g., outboard)
	AuthMethodOIDC      AuthMethod = "oidc"      // External OIDC token (e.g., GitHub Actions)
	AuthMethodSystem    AuthMethod = "system"    // Cluster-issued system workload identity
)

// Identity represents an authenticated caller
type Identity struct {
	// Subject is the primary identifier (cert CN, JWT subject, etc.)
	Subject string

	// Groups contains group memberships (from JWT claims, etc.)
	Groups []string

	// Method indicates how the caller was authenticated
	Method AuthMethod

	// Metadata holds auth-method-specific data (e.g., OrganizationID for cloud auth)
	Metadata map[string]any
}

// Authenticator validates credentials and returns caller identity
type Authenticator interface {
	// Authenticate validates the request's credentials and returns the caller's identity.
	// Returns:
	//   - (*Identity, nil) if credentials are valid
	//   - (nil, nil) if no credentials present or credentials are invalid
	//   - (nil, error) if an error occurred during authentication
	Authenticate(ctx context.Context, r *http.Request) (*Identity, error)
}

// Authorizer checks if an identity is allowed to perform an action on a resource
type Authorizer interface {
	// Authorize checks if the identity is allowed to perform the action on the resource.
	// For RPC methods, resource is typically the interface name (lowercase) and
	// action is the method name (lowercase).
	// Returns nil if allowed, or an error describing why access was denied.
	Authorize(ctx context.Context, identity *Identity, resource, action string) error
}

// ErrUnauthorized is returned when an app-scoped caller attempts to operate
// on an app they are not bound to.
var ErrUnauthorized = errors.New("unauthorized")

// AllowApp checks whether the current caller is permitted to operate on the
// named app. Callers that are not app-scoped (cert, JWT, anonymous) are always
// allowed. OIDC callers are restricted to the app their binding is for.
func AllowApp(ctx context.Context, appName string) bool {
	identity := IdentityFromContext(ctx)
	if identity == nil || identity.Method != AuthMethodOIDC {
		return true
	}
	boundApp, _ := identity.Metadata["bound_app"].(string)
	return boundApp != "" && boundApp == appName
}

// BoundApp returns the app name that the current OIDC caller is bound to,
// or empty string if the caller is not app-scoped.
func BoundApp(ctx context.Context) string {
	identity := IdentityFromContext(ctx)
	if identity == nil || identity.Method != AuthMethodOIDC {
		return ""
	}
	boundApp, _ := identity.Metadata["bound_app"].(string)
	return boundApp
}

// AppAccessError returns a descriptive error for an app-scoping denial.
func AppAccessError(ctx context.Context, appName string) error {
	boundApp := BoundApp(ctx)
	if boundApp == "" {
		return fmt.Errorf("%w: OIDC identity missing bound app", ErrUnauthorized)
	}
	return fmt.Errorf("%w: bound to app %q, cannot operate on %q", ErrUnauthorized, boundApp, appName)
}

// NoOpAuthenticator allows all requests without checking credentials.
// Used for testing only.
type NoOpAuthenticator struct{}

func (n *NoOpAuthenticator) Authenticate(ctx context.Context, r *http.Request) (*Identity, error) {
	return &Identity{
		Subject: "anonymous",
		Method:  AuthMethodAnonymous,
	}, nil
}

// LocalOnlyAuthenticator requires a valid TLS client certificate.
// Used when cloud authentication is not enabled.
type LocalOnlyAuthenticator struct{}

func (l *LocalOnlyAuthenticator) Authenticate(ctx context.Context, r *http.Request) (*Identity, error) {
	// Check for TLS client certificate. Only trust a cert that the TLS layer
	// verified against the cluster CA (VerifiedChains non-empty); a
	// presented-but-unverified cert must never yield a cert identity, which
	// grants RBAC-bypassing privileges in Authorize.
	if r.TLS != nil && len(r.TLS.VerifiedChains) > 0 && len(r.TLS.PeerCertificates) > 0 {
		cert := r.TLS.PeerCertificates[0]
		return &Identity{
			Subject: cert.Subject.CommonName,
			Method:  AuthMethodCert,
		}, nil
	}

	// No valid credentials
	return nil, nil
}
