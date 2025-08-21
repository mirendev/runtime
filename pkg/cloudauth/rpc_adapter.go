package cloudauth

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/rbac"
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
func NewRPCAuthenticator(config Config) (*RPCAuthenticator, error) {
	// Set default CloudURL if not provided
	if config.CloudURL == "" {
		config.CloudURL = DefaultCloudURL
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	a := &RPCAuthenticator{
		logger: config.Logger.With("component", "rpc-cloud-auth"),
		tags:   config.Tags,
	}

	// Set default tags if not provided
	if a.tags == nil {
		a.tags = make(map[string]any)
	}

	// Initialize JWT validation and RBAC (CloudURL always has a value now)
	a.jwtValidator = auth.NewJWTValidator(config.CloudURL)
	a.tokenCache = auth.NewTokenCache()

	// Always initialize RBAC when using cloud authentication
	// Create policy fetcher with the logger option
	a.policyFetcher = NewPolicyFetcher(config.CloudURL, config.AuthClient, WithLogger(config.Logger))

	// Start fetching policies
	if err := a.policyFetcher.Start(context.Background()); err != nil {
		a.logger.Warn("failed to start policy fetcher", "error", err)
	}

	// Create evaluator with the policy fetcher as provider
	a.rbacEval = rbac.NewEvaluator(a.policyFetcher, config.Logger)

	return a, nil
}

// AuthenticateRequest implements rpc.Authenticator
// This is called before any RPC method is invoked. It's not currently wired
// into the RPC layer at the method call layer, but it's also ONLY used to
// authenticate HTTP requests that are routed to RPC methods.
func (a *RPCAuthenticator) AuthenticateRequest(ctx context.Context, r *http.Request) (bool, string, error) {
	// Extract token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		// No auth header - could be certificate-based auth
		return false, "", nil
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false, "", fmt.Errorf("invalid authorization header format")
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
			return false, "", fmt.Errorf("token validation failed: %w", err)
		}
		claims = validated

		// Cache the validated token
		a.tokenCache.Set(token, claims)
	}

	// TODO For now we're going to hardcode the resource and action
	// as being related to cluster access. In the future, we'll make changes
	// to this where we request authorization for specific high level actions,
	// like "deploy", "manage", etc.
	// At that point, we'll rewire this method to be called post-RPC invoke
	// inside the handler for the method itself.

	req := &rbac.Request{
		Subject:  claims.Subject,
		Groups:   claims.GroupIDs,
		Resource: "cluster",
		Action:   "access",
		Tags:     a.tags, // Use the configured tags
		Context: map[string]any{
			"organization_id": claims.OrganizationID,
		},
	}

	decision := a.rbacEval.Evaluate(req)
	if decision == rbac.DecisionDeny {
		a.logger.Warn("authorization denied",
			"subject", claims.Subject,
			"groups", claims.GroupIDs,
			"resource", r.URL.Path,
			"action", r.Method,
			"tags", a.tags,
		)
		return false, "", fmt.Errorf("access denied by RBAC policy")
	}

	a.logger.Debug("JWT authentication successful",
		"subject", claims.Subject,
		"organization_id", claims.OrganizationID,
	)

	return true, claims.Subject, nil
}

// NoAuthorization implements rpc.Authenticator
func (a *RPCAuthenticator) NoAuthorization(ctx context.Context, r *http.Request) (bool, string, error) {
	// Check if we have client certificates (already validated by TLS layer)
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		cert := r.TLS.PeerCertificates[0]

		// Extract the identity from the certificate
		a.logger.Debug("request allowed via client certificate",
			"subject", cert.Subject.String(),
			"path", r.URL.Path)
		return true, cert.Subject.CommonName, nil
	}

	// No valid auth provided, deny the request
	a.logger.Debug("request denied - authentication required", "path", r.URL.Path)
	return false, "", fmt.Errorf("authentication required")
}

// Stop stops background tasks
func (a *RPCAuthenticator) Stop() {
	a.policyFetcher.Stop()
	a.rbacEval.Stop()
}
