package oidcauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadroles"
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

// CompositeAuthenticator tries a chain of authenticators in order and returns
// the first identity produced.
type CompositeAuthenticator struct {
	chain []rpc.Authenticator
}

// NewCompositeAuthenticator creates a composite authenticator that chains primary and OIDC auth.
func NewCompositeAuthenticator(primary rpc.Authenticator, oidc *OIDCAuthenticator) *CompositeAuthenticator {
	return NewCompositeAuthenticatorChain(primary, oidc)
}

// NewCompositeAuthenticatorChain builds a composite from an ordered chain,
// skipping any nil authenticator so callers can pass one that is conditionally
// unavailable (e.g. workload identity on a cluster with no issuer).
//
// Order matters. Put the workload identity authenticator ahead of OIDC: it
// claims tokens by our own issuer URL, which spares OIDC an entity-store lookup
// per token, and it forecloses an oidc_binding registered against our own
// issuer from reinterpreting our workload tokens under an attacker-chosen app.
func NewCompositeAuthenticatorChain(auths ...rpc.Authenticator) *CompositeAuthenticator {
	c := &CompositeAuthenticator{}
	for _, a := range auths {
		if isNilAuthenticator(a) {
			continue
		}
		c.chain = append(c.chain, a)
	}
	return c
}

// isNilAuthenticator reports whether a is unusable, catching a nil pointer
// stored in a non-nil interface as well as a plain nil interface. Callers build
// the chain from optional components, and a typed nil would otherwise reach
// Authenticate with a nil receiver. Every implementation is a pointer, so that
// is the only typed nil worth handling.
func isNilAuthenticator(a rpc.Authenticator) bool {
	if a == nil {
		return true
	}
	v := reflect.ValueOf(a)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

func (c *CompositeAuthenticator) Authenticate(ctx context.Context, r *http.Request) (*rpc.Identity, error) {
	// Keep the first error rather than the last. An authenticator that fails on
	// a token meant for a later link in the chain (the cloud JWT validator
	// handed a GitHub Actions token, say) reports an error that only matters if
	// nobody else claims the token.
	//
	// A binding mismatch is the exception: it means an authenticator got far
	// enough to verify the token's signature and audience and knows exactly why
	// the caller was rejected, so it wins over a mere "not my token" parse
	// failure from another link.
	var firstErr, mismatchErr error

	for _, auth := range c.chain {
		identity, err := auth.Authenticate(ctx, r)
		if identity != nil {
			return identity, nil
		}
		if err == nil {
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
		if _, ok := errors.AsType[*BindingMismatchError](err); mismatchErr == nil && ok {
			mismatchErr = err
		}
	}

	// A binding mismatch wins over another link's error: it means an
	// authenticator got far enough to verify the token's signature and audience
	// and knows exactly why the caller was rejected, so it beats a mere "not my
	// token" parse failure from another link.
	if mismatchErr != nil {
		return nil, mismatchErr
	}
	return nil, firstErr
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
		return authorizeRole("OIDC", oidcDeployRole, resource, action)

	case rpc.AuthMethodWorkload:
		// In-sandbox workloads are restricted to the role their token names, and
		// app-scoped roles are further confined to their own app by rpc.AllowApp
		// in the handlers. An unknown role name resolves to nothing and is
		// denied everything (fail closed).
		return authorizeWorkload(identity, resource, action)

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

	case rpc.AuthMethodJWT, rpc.AuthMethodAnonymous, rpc.AuthMethodToken:
		// Delegate to primary (cloud RBAC).
		if c.primary != nil {
			return c.primary.Authorize(ctx, identity, resource, action)
		}
		return nil

	default:
		// Fail closed on an auth method we don't know about. This used to share
		// the delegate-to-primary branch above, which meant a newly added
		// AuthMethod was granted everything wherever primary was nil — i.e. on
		// the local-only path, the common configuration.
		return fmt.Errorf("access denied: unknown authentication method %q", identity.Method)
	}
}

// authorizeRole checks a resource/action pair against a fixed role map.
func authorizeRole(roleName string, role map[string]map[string]bool, resource, action string) error {
	actions, ok := role[resource]
	if !ok {
		return fmt.Errorf("%s access denied: resource %q not permitted", roleName, resource)
	}
	if !actions[action] {
		return fmt.Errorf("%s access denied: action %q on resource %q not permitted", roleName, action, resource)
	}
	return nil
}

// authorizeWorkload checks a call against the role named in the workload
// identity's token. The role name is resolved from the catalog; an unknown or
// missing name grants nothing.
func authorizeWorkload(identity *rpc.Identity, resource, action string) error {
	name, _ := identity.Metadata["role"].(string)
	role, ok := workloadroles.Lookup(name)
	if !ok {
		return fmt.Errorf("workload access denied: unknown role %q", name)
	}
	return authorizeRole("workload role "+name, role.Perms, resource, action)
}
