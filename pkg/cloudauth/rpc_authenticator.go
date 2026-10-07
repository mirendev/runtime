package cloudauth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/rbac"
	"miren.dev/runtime/pkg/rpc"
)

// DefaultCloudURL is the default URL for miren.cloud
const DefaultCloudURL = "https://api.miren.cloud"

// RPCAuthenticator adapts cloud authentication for RPC usage
type RPCAuthenticator struct {
	jwtValidator  *auth.JWTValidator
	tokenCache    *auth.TokenCache
	authorization *AuthorizationState
	logger        *slog.Logger

	// Tags to use for RBAC evaluation
	tags map[string]any
}

// Config for RPCAuthenticator
type Config struct {
	CloudURL string
	Logger   *slog.Logger
	Tags     map[string]any // Tags for this runtime/cluster
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Logger == nil {
		return fmt.Errorf("logger is required")
	}

	// Validate tags if provided
	if c.Tags != nil {
		for key, value := range c.Tags {
			// Ensure tag keys are strings
			if key == "" {
				return fmt.Errorf("tag key cannot be empty")
			}
			// Ensure tag values are simple types (string, number, bool)
			switch v := value.(type) {
			case string, int, int32, int64, float32, float64, bool:
				// Valid types
			case nil:
				// Null is ok
			default:
				return fmt.Errorf("tag value for key %q must be a simple type (string, number, or bool), got %T", key, v)
			}
		}
	}

	return nil
}

// NewRPCAuthenticator creates a new RPC authenticator
func NewRPCAuthenticator(ctx context.Context, config Config) (*RPCAuthenticator, error) {
	// Set default CloudURL if not provided
	if config.CloudURL == "" {
		config.CloudURL = DefaultCloudURL
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	a := &RPCAuthenticator{
		logger: config.Logger.With("module", "cloud-auth"),
		tags:   config.Tags,
	}

	// Set default tags if not provided
	if a.tags == nil {
		a.tags = make(map[string]any)
	}

	// Initialize JWT validation and RBAC (CloudURL always has a value now)
	a.jwtValidator = auth.NewJWTValidator(config.CloudURL, config.Logger)
	a.tokenCache = auth.NewTokenCache(ctx)
	a.authorization = NewAuthorizationState(ctx, config.Logger)

	return a, nil
}

// Authenticate implements rpc.Authenticator.
// It tries JWT authentication first, then falls back to TLS client certificate.
// Authorization (RBAC) is handled separately via the Authorize method.
func (a *RPCAuthenticator) Authenticate(ctx context.Context, creds *rpc.Credentials) (*rpc.Identity, error) {
	// Try JWT authentication first if Authorization header is present
	if creds.Authorization != "" {
		identity, err := a.authenticateJWT(ctx, creds.Authorization)
		if err != nil {
			return nil, err
		}
		if identity != nil {
			return identity, nil
		}
		// Invalid JWT format, fall through to cert check
	}

	// Fall back to a TLS client certificate the TLS layer verified against the
	// cluster CA.
	if cert := creds.VerifiedPeerCertificate(); cert != nil {
		return &rpc.Identity{
			Subject: cert.Subject.CommonName,
			Method:  rpc.AuthMethodCert,
		}, nil
	}

	// No valid credentials
	return nil, nil
}

// authenticateJWT validates a JWT token and returns the caller's identity.
// Authorization (RBAC) is handled separately in the Authorize method.
func (a *RPCAuthenticator) authenticateJWT(ctx context.Context, authHeader string) (*rpc.Identity, error) {
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, nil // Invalid format, not a JWT
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	token = strings.TrimSpace(token)

	// Check cache first
	var claims *auth.Claims
	if cached, ok := a.tokenCache.Get(token); ok {
		claims = cached
	} else {
		validated, err := a.jwtValidator.ValidateToken(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("token validation failed: %w", err)
		}
		claims = validated

		// Cache the validated token
		a.tokenCache.Set(token, claims)
	}

	a.logger.Debug("JWT authentication successful",
		"subject", claims.Subject,
		"organization_id", claims.OrganizationID,
	)

	return &rpc.Identity{
		Subject: claims.Subject,
		// Preserve token claims for authentication diagnostics only. Authorize
		// resolves effective groups from the latest pushed snapshot instead.
		Groups: claims.GroupIDs,
		Method: rpc.AuthMethodJWT,
		Metadata: map[string]any{
			"organization_id": claims.OrganizationID,
			"email":           claims.Email,
			"name":            claims.Name,
		},
	}, nil
}

// Authorize implements rpc.Authorizer.
// It performs RBAC evaluation to determine if the identity can perform the action.
func (a *RPCAuthenticator) Authorize(ctx context.Context, identity *rpc.Identity, resource, action string) error {
	// Cert-authenticated callers (local/internal) bypass RBAC
	if identity.Method == rpc.AuthMethodCert {
		return nil
	}

	// Build RBAC request using the provided resource and action
	req := &rbac.Request{
		Subject:  identity.Subject,
		Resource: resource,
		Action:   action,
		Tags:     a.tags,
		Context:  map[string]any{},
	}

	if identity.Metadata != nil {
		req.Context["organization_id"] = identity.Metadata["organization_id"]
	}

	decision, reason := a.authorization.Evaluate(req)
	if decision == rbac.DecisionDeny {
		a.logger.Warn("authorization denied",
			"reason", reason,
			"subject", identity.Subject,
			"groups", req.Groups,
			"resource", resource,
			"action", action,
			"tags", a.tags,
		)

		if reason == "not_synced" {
			return fmt.Errorf("access denied: cloud authorization is not synchronized; check the cluster's cloud connection and initial snapshot")
		}
		return fmt.Errorf("access denied by RBAC policy")
	}

	return nil
}

// Stop stops background tasks
func (a *RPCAuthenticator) Stop() {
	a.authorization.evaluator.Stop()
}

// RegisterAuthorization binds cloud authorization to the shared uplink.
func (a *RPCAuthenticator) RegisterAuthorization(link AuthorizationLink) {
	a.authorization.Register(link)
}
