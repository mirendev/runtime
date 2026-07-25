package oidcauth

import (
	"context"
	"errors"
	"fmt"

	"miren.dev/runtime/pkg/rpc"
)

// oidcDeployRole defines the RPC interfaces and actions allowed for OIDC-authenticated callers.
var oidcDeployRole = map[string]map[string]bool{
	"deployment": {
		"deployversion":              true,
		"createdeployment":           true,
		"updatedeploymentstatus":     true,
		"updatedeploymentphase":      true,
		"listdeployments":            true,
		"getdeploymentbyid":          true,
		"updatefaileddeployment":     true,
		"getactivedeployment":        true,
		"canceldeployment":           true,
		"updatedeploymentappversion": true,
	},
	"logs": {
		"applogs":         true,
		"streamlogs":      true,
		"streamlogchunks": true,
	},
	"crud": {
		"list":             true,
		"getconfiguration": true,
	},
	"builder": {
		"buildfromtar": true,
		"analyzeapp":   true,
	},
	"telemetry": {
		"reportspans": true,
	},
	"appstatus": {
		"appinfo": true,
	},
}

// CompositeAuthenticator chains a primary authenticator with the OIDC authenticator.
// It tries the primary first and falls back to OIDC.
type CompositeAuthenticator struct {
	primary rpc.Authenticator
	oidc    rpc.Authenticator
}

// NewCompositeAuthenticator creates a composite authenticator that chains primary and OIDC auth.
func NewCompositeAuthenticator(primary rpc.Authenticator, oidc *OIDCAuthenticator) *CompositeAuthenticator {
	return &CompositeAuthenticator{primary: primary, oidc: oidc}
}

func (c *CompositeAuthenticator) Authenticate(ctx context.Context, creds *rpc.Credentials) (*rpc.Identity, error) {
	// Try primary authenticator first
	identity, err := c.primary.Authenticate(ctx, creds)
	if identity != nil {
		return identity, nil
	}

	// Fall back to OIDC authenticator. The primary may have returned an error
	// because it couldn't validate a token that's actually meant for OIDC
	// (e.g., cloud JWT validator failing on a GitHub Actions OIDC token).
	var oidcIdentity *rpc.Identity
	var oidcErr error
	if c.oidc != nil {
		oidcIdentity, oidcErr = c.oidc.Authenticate(ctx, creds)
		if oidcIdentity != nil {
			return oidcIdentity, nil
		}
	}

	// Neither succeeded. A binding mismatch wins over the primary's error: it
	// means OIDC got far enough to verify the token's signature and audience, so
	// it knows exactly why the caller was rejected. The primary, by contrast,
	// only failed to parse a token that was never meant for it.
	var mismatch *BindingMismatchError
	if errors.As(oidcErr, &mismatch) {
		return nil, oidcErr
	}

	// Otherwise prefer the primary error if there was one.
	if err != nil {
		return nil, err
	}
	return nil, oidcErr
}

// CompositeAuthorizer handles authorization for both primary and OIDC auth methods.
type CompositeAuthorizer struct {
	primary rpc.Authorizer
}

// NewCompositeAuthorizer creates a composite authorizer that handles both primary and OIDC authorization.
func NewCompositeAuthorizer(primary rpc.Authorizer) *CompositeAuthorizer {
	return &CompositeAuthorizer{primary: primary}
}

func (c *CompositeAuthorizer) Authorize(ctx context.Context, identity *rpc.Identity, resource, action string) error {
	switch identity.Method {
	case rpc.AuthMethodCert:
		// Local/internal callers bypass all checks
		return nil

	case rpc.AuthMethodOIDC:
		// OIDC callers are restricted to the oidc-deploy role
		return authorizeOIDC(resource, action)

	case rpc.AuthMethodSystem:
		// System workload identities exist to reach specific cluster-internal
		// services (the registry, telemetry), each of which verifies the token
		// itself against its own audience. They carry no RPC privileges at all:
		// falling through to the default would hand blanket access to any
		// caller holding one, which is the opposite of why the identity is
		// scoped.
		//
		// Deny-by-default is a starting point, not the intended end state.
		// System workload tokens carry a real subject
		// (org:X:cluster:Y:system:name), so once authorization is driven by
		// policy rather than by the identity method, a workload that needs a
		// particular method should be granted it by rule like any other
		// subject, and this arm should go away rather than grow a list.
		return fmt.Errorf("access denied: system workload identities may not call RPC methods")

	case rpc.AuthMethodJWT, rpc.AuthMethodAnonymous, rpc.AuthMethodToken, rpc.AuthMethodSigned:
		// Delegate to primary (cloud RBAC). A signed identity names the key a
		// capability was issued to, which carries no privilege of its own.
		fallthrough
	default:
		if c.primary != nil {
			return c.primary.Authorize(ctx, identity, resource, action)
		}
		return nil
	}
}

func authorizeOIDC(resource, action string) error {
	actions, ok := oidcDeployRole[resource]
	if !ok {
		return fmt.Errorf("OIDC access denied: resource %q not permitted", resource)
	}
	if !actions[action] {
		return fmt.Errorf("OIDC access denied: action %q on resource %q not permitted", action, resource)
	}
	return nil
}
