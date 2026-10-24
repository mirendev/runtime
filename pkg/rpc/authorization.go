package rpc

import "context"

// ResourceRequest names a logical operation after the handler has resolved its
// target. Instance and Owner must come from authoritative server-side state.
type ResourceRequest struct {
	Resource string
	Action   string
	Instance string
	Owner    string
}

// ResourceAuthorizer supplements the transport's method gate with a check on
// the resolved resource. Authorizers without it retain their method-only gate.
type ResourceAuthorizer interface {
	AuthorizeResource(context.Context, *Identity, ResourceRequest) error
}

// LogicalAuthorizer distinguishes scoped policy from legacy method gates.
// Standalone role checks are not Cloud logical policy, even when composed with it.
type LogicalAuthorizer interface {
	UsesLogicalAuthorization(context.Context, *Identity) bool
}

// UsesLogicalAuthorization reports whether new resolved-resource checks apply.
// A false result from Cloud must include authorization of the original method,
// atomically with selecting legacy policy, not just a snapshot version lookup.
// Resource authorizers without a mode selector always enforce their policy.
func UsesLogicalAuthorization(ctx context.Context) bool {
	auth, _ := ctx.Value(authorizationKey{}).(authorizationContext)
	if mode, ok := auth.authorizer.(LogicalAuthorizer); ok {
		return mode.UsesLogicalAuthorization(ctx, IdentityFromContext(ctx))
	}
	return HasResourceAuthorizer(ctx)
}

type authorizationKey struct{}

type authorizationContext struct {
	authorizer Authorizer
	resource   string
	action     string
}

// ContextWithAuthorization carries the transport's authorizer to domain
// handlers, including the original method for legacy-policy compatibility.
func ContextWithAuthorization(ctx context.Context, authorizer Authorizer, resource, action string) context.Context {
	return context.WithValue(ctx, authorizationKey{}, authorizationContext{authorizer, resource, action})
}

// RPCPermissionFromContext returns the original transport permission.
func RPCPermissionFromContext(ctx context.Context) (resource, action string) {
	auth, _ := ctx.Value(authorizationKey{}).(authorizationContext)
	return auth.resource, auth.action
}

func HasResourceAuthorizer(ctx context.Context) bool {
	auth, _ := ctx.Value(authorizationKey{}).(authorizationContext)
	_, ok := auth.authorizer.(ResourceAuthorizer)
	return ok
}

// AuthorizeResource checks a resolved target using the configured authorizer.
// An in-process call with no configured authorizer retains its existing trust.
func AuthorizeResource(ctx context.Context, request ResourceRequest) error {
	auth, _ := ctx.Value(authorizationKey{}).(authorizationContext)
	if authorizer, ok := auth.authorizer.(ResourceAuthorizer); ok {
		return authorizer.AuthorizeResource(ctx, IdentityFromContext(ctx), request)
	}
	return nil
}

// AppAction lists only RPC methods whose handlers perform a resolved app
// authorization check. Such methods may defer the Cloud method gate to that
// check; extending this list without migrating the handler would bypass policy.
func AppAction(resource, action string) string {
	switch resource {
	case "crud":
		switch action {
		case "new":
			return "create"
		case "destroy":
			return "destroy"
		case "getconfiguration", "listversions":
			return "read"
		case "setconfiguration", "setenvvar", "setenvvars", "setinitialenvvars", "deleteenvvar":
			return "configure"
		case "restart":
			return "deploy"
		}
	case "appstatus":
		if action == "appinfo" {
			return "read"
		}
	case "runs":
		switch action {
		case "getrun", "listruns":
			return "read"
		case "createrun", "submitrun", "cancelrun":
			return "exec"
		}
	case "sandboxexec":
		if action == "exec" || action == "attach" {
			return "exec"
		}
	case "logs":
		switch action {
		case "applogs", "sandboxlogs", "streamlogs", "streamlogchunks", "streamlogchunksv2":
			return "logs"
		}
	case "builder":
		switch action {
		case "buildfromtar", "prepareupload", "buildfromprepared":
			return "deploy"
		}
	case "deployment":
		switch action {
		case "updatedeploymentstatus", "updatedeploymentphase", "updatefaileddeployment", "updatedeploymentappversion", "canceldeployment", "deployversion":
			return "deploy"
		case "listdeployments", "getdeploymentbyid", "getactivedeployment", "getdeploylock":
			return "read"
		case "setenvvars", "deleteenvvars":
			return "configure"
		}
	}
	return ""
}
