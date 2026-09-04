// Package workloadroles is the single source of truth for the canned roles a
// sandbox's workload identity token can be minted with.
//
// A role is a coarse allow-list of (resource, action) pairs — resource is a
// lowercased RPC interface name, action a lowercased method name, matching what
// rpc.Authorizer.Authorize receives. It answers "may this workload ever call
// this method", never "against which app": per-app confinement is enforced
// separately by rpc.AllowApp inside the handlers.
//
// That split is why ClusterScoped matters. An app-scoped role only ever grants
// methods that call rpc.AllowApp, so a token minted for app X can reach the
// method but only for X. A cluster-scoped role is not confined to one app, so
// it may also grant cluster-wide methods — and the authenticator signals that
// by leaving the identity's bound app empty, which makes rpc.AllowApp permit
// every app.
//
// The package has no internal dependencies so both the authorizer (pkg/oidcauth)
// and the authenticator (pkg/workloadidentity) can import it without a cycle.
//
// This map is not the permanent model. RFD-67 (MIR-891) moves authorization to
// {Resource, Action, Instance} with a Scope on each permission, at which point
// this catalog becomes a set of policy presets rather than a Go map and the
// per-handler rpc.AllowApp calls fall away. The token carries the role *name*,
// not a resolved permission set, so that backend swap needs no token-format
// change and no reissue — read the map as the current implementation, not the
// enduring shape.
package workloadroles

// Role names. These are the values stored on an app and carried in the token's
// role claim.
const (
	RoleNone            = "none"
	RoleAppReadonly     = "app-readonly"
	RoleAppDeployer     = "app-deployer"
	RoleAppDebugger     = "app-debugger"
	RoleAppAdmin        = "app-admin"
	RoleClusterReadonly = "cluster-readonly"
	RoleClusterDeployer = "cluster-deployer"
	RoleClusterDebugger = "cluster-debugger"
	RoleClusterAdmin    = "cluster-admin"
)

// Default is the role an app runs under when it hasn't chosen one. It preserves
// roughly the pre-role behavior (own-app reads) so nothing regresses; apps opt
// up from here.
const Default = RoleAppReadonly

// Role is a permission set. Perms[resource][action] == true means allowed.
type Role struct {
	// Perms is the allow-list. Treat as read-only.
	Perms perms
	// ClusterScoped reports whether the role reaches beyond its own app. When
	// false the identity is confined to its origin app by rpc.AllowApp; when
	// true it is not.
	ClusterScoped bool
}

type perms map[string]map[string]bool

// permission blocks. app-scoped blocks contain ONLY methods guarded by
// rpc.AllowApp in their handler — the tripwire test enforces this, and it is
// the invariant that keeps an app-scoped role from reaching another app.
var (
	// appRead: read one app's own state.
	appRead = perms{
		"appstatus": set("appinfo"),
		"logs":      set("applogs", "streamlogs", "streamlogchunks"),
		// getconfiguration is deliberately excluded: it returns resolved env
		// including sibling-service secrets. Revisit if a read role should see it.
		"deployment": set("listdeployments", "getdeploymentbyid", "getactivedeployment"),
	}

	// appDeploy: build and (re)deploy one app.
	appDeploy = perms{
		"deployment": set(
			"deployversion", "createdeployment", "updatedeploymentstatus",
			"updatedeploymentphase", "updatefaileddeployment",
			"updatedeploymentappversion", "canceldeployment",
		),
		"builder": set("buildfromtar", "prepareupload", "buildfromprepared"),
	}

	// appConfig: mutate one app's configuration. These handlers gain their
	// rpc.AllowApp guard in this change; before that they were cluster-wide.
	appConfig = perms{
		"crud": set(
			"setconfiguration", "setenvvar", "setenvvars",
			"setinitialenvvars", "deleteenvvar", "restart", "sethost",
		),
		"deployment": set("setenvvars", "deleteenvvars"),
	}

	// exec: open a shell / run a command in a sandbox. Guarded in this change.
	execBlock = perms{
		"sandboxexec": set("exec"),
	}

	// clusterRead: read across the cluster and its infrastructure.
	clusterRead = perms{
		"crud":               set("list"),
		"logs":               set("sandboxlogs"),
		"disks":              set("list", "getbyid", "getbyname"),
		"addons":             set("listinstances"),
		"runnerregistration": set("listinvites", "listrunners", "workloadissuerinfo"),
		"netdb":              set("listleases", "status"),
		"sandboxmetrics":     set("snapshot"),
		// Usage reads span every app on the cluster, so they can only sit in a
		// cluster-scoped block; there is no way to confine them to the calling
		// app the way rpc.AllowApp confines the blocks above.
		"resourceusage": set(
			// RPC surface.
			"listsandboxes", "getsandbox", "listnodes", "listapps",
			// The same six questions over plain HTTP GET.
			"httplistsandboxes", "httpgetsandbox",
			"httplistnodes", "httpgetnode",
			"httplistapps", "httpgetapp",
		),
		"outboardcontrol": set("health"),
		"userquery":       set("whoami"),
		"builder":         set("analyzeapp"),
	}
)

// Roles is the catalog. Composed from the blocks above so a method appears in
// exactly one place.
var Roles = map[string]Role{
	RoleNone: {Perms: perms{}},

	RoleAppReadonly: {Perms: appRead},
	RoleAppDeployer: {Perms: merge(appRead, appDeploy)},
	RoleAppDebugger: {Perms: merge(appRead, execBlock)},
	RoleAppAdmin:    {Perms: merge(appRead, appDeploy, appConfig, execBlock)},

	RoleClusterReadonly: {Perms: merge(appRead, clusterRead), ClusterScoped: true},
	// cluster-deployer deploys and configures any app, but does NOT create or
	// destroy apps — app lifecycle (crud.new/destroy) belongs to cluster-admin.
	// Otherwise a "deployer" could delete every app while app-admin can't even
	// delete its own, which nobody predicts from the names.
	RoleClusterDeployer: {
		Perms:         merge(appRead, clusterRead, appDeploy, appConfig),
		ClusterScoped: true,
	},
	RoleClusterDebugger: {
		Perms: merge(appRead, clusterRead, execBlock, perms{
			"admin":        set("invoke", "listmethods", "describemethods"),
			"internalhttp": set("dorequest"),
		}),
		ClusterScoped: true,
	},
	RoleClusterAdmin: {Perms: clusterAdminPerms(), ClusterScoped: true},
}

// clusterAdminPerms is every grantable method: the union of all blocks plus the
// remaining app/debug verbs. It deliberately does NOT include the cert-only
// carve-outs — entityaccess.*, stream.*, the mutating runnerregistration.*
// (esp. issueworkloadtoken), netdb writes, outboardcontrol.checkversion, and
// oidcbindings.* — which stay on the internal cert plane. A role is a bearer
// token mounted in a sandbox; even cluster-admin must not be able to mint
// identities, corrupt the entity store, or reconfigure the cluster fabric.
func clusterAdminPerms() perms {
	return merge(
		appRead, appDeploy, appConfig, execBlock, clusterRead,
		perms{
			"crud":         set("new", "destroy"),
			"admin":        set("invoke", "listmethods", "describemethods"),
			"internalhttp": set("dorequest"),
			"disks":        set("new", "delete"),
			"addons":       set("createinstance", "deleteinstance"),
		},
	)
}

// Lookup returns the role by name. Unknown names return ok=false so callers fail
// closed rather than granting anything.
func Lookup(name string) (Role, bool) {
	r, ok := Roles[name]
	return r, ok
}

// IsAppScoped reports whether name is a known role confined to its own app.
// Used to reject cluster-scoped roles from app-owner-controlled sources
// (.miren/app.toml). An unknown name is not app-scoped.
func IsAppScoped(name string) bool {
	r, ok := Roles[name]
	return ok && !r.ClusterScoped
}

func set(actions ...string) map[string]bool {
	m := make(map[string]bool, len(actions))
	for _, a := range actions {
		m[a] = true
	}
	return m
}

// merge unions permission blocks into a fresh map, leaving the inputs untouched.
func merge(blocks ...perms) perms {
	out := perms{}
	for _, b := range blocks {
		for resource, actions := range b {
			if out[resource] == nil {
				out[resource] = map[string]bool{}
			}
			for action := range actions {
				out[resource][action] = true
			}
		}
	}
	return out
}
