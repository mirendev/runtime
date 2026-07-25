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
	rbacEval      *rbac.Evaluator
	policyFetcher *PolicyFetcher
	logger        *slog.Logger

	// Tags to use for RBAC evaluation
	tags map[string]any
}

// Config for RPCAuthenticator
type Config struct {
	CloudURL   string
	AuthClient *AuthClient
	Logger     *slog.Logger
	Tags       map[string]any // Tags for this runtime/cluster
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

	// Always initialize RBAC when using cloud authentication
	// Create policy fetcher with the logger option
	a.policyFetcher = NewPolicyFetcher(config.CloudURL, config.AuthClient, WithLogger(config.Logger))

	// Start fetching policies
	if err := a.policyFetcher.Start(context.Background()); err != nil {
		a.logger.Warn("failed to start policy fetcher", "error", err)
	}

	// Create evaluator with the policy fetcher as provider
	a.rbacEval = rbac.NewEvaluator(ctx, a.policyFetcher, config.Logger)

	// Set the evaluator in the policy fetcher so it can clear the cache on refresh
	a.policyFetcher.SetEvaluator(a.rbacEval)

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
		Groups:  claims.GroupIDs,
		Method:  rpc.AuthMethodJWT,
		Metadata: map[string]any{
			"organization_id": claims.OrganizationID,
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
		Groups:   identity.Groups,
		Resource: resource,
		Action:   action,
		Tags:     a.tags,
		Context:  map[string]any{},
	}

	// Add organization_id from metadata if present
	if identity.Metadata != nil {
		if orgID, ok := identity.Metadata["organization_id"]; ok {
			req.Context["organization_id"] = orgID
		}
	}

	decision := a.rbacEval.Evaluate(req)
	if decision == rbac.DecisionDeny {
		a.logger.Warn("authorization denied",
			"subject", identity.Subject,
			"groups", identity.Groups,
			"resource", resource,
			"action", action,
			"tags", a.tags,
		)

		// Trigger a refresh of RBAC rules (with 30-second cooldown)
		a.policyFetcher.RefreshIfNeeded(ctx)

		return fmt.Errorf("access denied by RBAC policy")
	}

	return nil
}

// Stop stops background tasks
func (a *RPCAuthenticator) Stop() {
	a.policyFetcher.Stop()
	a.rbacEval.Stop()
}

// GetEvaluator returns the RBAC evaluator
func (a *RPCAuthenticator) GetEvaluator() *rbac.Evaluator {
	return a.rbacEval
}

// GetPolicyFetcher returns the policy fetcher
func (a *RPCAuthenticator) GetPolicyFetcher() *PolicyFetcher {
	return a.policyFetcher
}
