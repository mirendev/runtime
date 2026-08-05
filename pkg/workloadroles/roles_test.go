package workloadroles

import "testing"

func has(r Role, resource, action string) bool {
	return r.Perms[resource][action]
}

func TestDefaultIsAppScoped(t *testing.T) {
	if _, ok := Lookup(Default); !ok {
		t.Fatalf("default role %q is not in the catalog", Default)
	}
	if !IsAppScoped(Default) {
		t.Errorf("default role %q must be app-scoped", Default)
	}
}

func TestLookupUnknownFailsClosed(t *testing.T) {
	if _, ok := Lookup("no-such-role"); ok {
		t.Error("unknown role must return ok=false")
	}
	if _, ok := Lookup(""); ok {
		t.Error("empty role must return ok=false")
	}
	if IsAppScoped("no-such-role") {
		t.Error("unknown role must not report as app-scoped")
	}
}

func TestNoneGrantsNothing(t *testing.T) {
	r, ok := Lookup(RoleNone)
	if !ok {
		t.Fatal("none role missing")
	}
	if len(r.Perms) != 0 {
		t.Errorf("none must grant nothing, got %v", r.Perms)
	}
	if r.ClusterScoped {
		t.Error("none must not be cluster-scoped")
	}
}

func TestScoping(t *testing.T) {
	appScoped := []string{RoleNone, RoleAppReadonly, RoleAppDeployer, RoleAppDebugger, RoleAppAdmin}
	clusterScoped := []string{RoleClusterReadonly, RoleClusterDeployer, RoleClusterDebugger, RoleClusterAdmin}

	for _, name := range appScoped {
		if !IsAppScoped(name) {
			t.Errorf("%s should be app-scoped", name)
		}
	}
	for _, name := range clusterScoped {
		if IsAppScoped(name) {
			t.Errorf("%s should be cluster-scoped", name)
		}
		if r, _ := Lookup(name); !r.ClusterScoped {
			t.Errorf("%s must set ClusterScoped", name)
		}
	}
}

// The invariant that keeps app-scoped roles from reaching another app: every
// method they grant must be one whose handler calls rpc.AllowApp. We can't
// import pkg/rpc's handlers here, so the guarded set is defined as the union of
// the app-scoped permission blocks — the same blocks the handler guards are
// added for. If someone adds a method to an app-scoped role that isn't in a
// guarded block (e.g. crud.list, logs.sandboxlogs), this fails.
func TestAppScopedRolesOnlyGrantGuardedMethods(t *testing.T) {
	guarded := merge(appRead, appDeploy, appConfig, execBlock)

	for name, role := range Roles {
		if role.ClusterScoped {
			continue
		}
		for resource, actions := range role.Perms {
			for action := range actions {
				if !guarded[resource][action] {
					t.Errorf("app-scoped role %q grants %s.%s, which is not in a guarded block "+
						"— it would reach every app, not just the workload's own", name, resource, action)
				}
			}
		}
	}
}

// The cert-only carve-outs must never appear in any role, not even cluster-admin.
func TestCarveOutsAbsentFromAllRoles(t *testing.T) {
	forbidden := map[string][]string{
		"entityaccess":       {"get", "put", "create", "replace", "patch", "delete", "list", "reindex"},
		"stream":             {"recv"},
		"runnerregistration": {"issueworkloadtoken", "createinvite", "join", "revokeinvite", "removerunner", "cordonrunner", "uncordonrunner", "drainrunner", "refreshcertificate"},
		"netdb":              {"releaseip", "releasesubnet", "releaseall", "gc"},
		"outboardcontrol":    {"checkversion"},
		"oidcbindings":       {"add", "remove", "list"},
		// setworkloadrole assigns a role (including cluster-scoped ones) to an
		// app. It has no rpc.AllowApp guard and is the operator-only path, so it
		// must never be granted to a token role — otherwise a workload could
		// escalate its own app to a cluster role.
		"crud": {"setworkloadrole"},
	}

	for name, role := range Roles {
		for resource, actions := range forbidden {
			for _, action := range actions {
				if has(role, resource, action) {
					t.Errorf("role %q grants carved-out %s.%s — must stay on the cert-only plane", name, resource, action)
				}
			}
		}
	}
}

func TestClusterAdminIsBroad(t *testing.T) {
	admin, _ := Lookup(RoleClusterAdmin)

	// Spot-check that cluster-admin actually spans read, app-write, exec, and
	// infra-adjacent surfaces — otherwise a composition bug could silently
	// narrow it.
	want := [][2]string{
		{"crud", "destroy"},
		{"deployment", "deployversion"},
		{"crud", "setenvvar"},
		{"sandboxexec", "exec"},
		{"admin", "invoke"},
		{"disks", "delete"},
		{"logs", "sandboxlogs"},
	}
	for _, wa := range want {
		if !has(admin, wa[0], wa[1]) {
			t.Errorf("cluster-admin missing %s.%s", wa[0], wa[1])
		}
	}
}

func TestRoleMembershipSpotChecks(t *testing.T) {
	cases := []struct {
		role            string
		granted, denied [2]string
	}{
		{RoleAppReadonly, [2]string{"logs", "applogs"}, [2]string{"crud", "setenvvar"}},
		{RoleAppReadonly, [2]string{"appstatus", "appinfo"}, [2]string{"sandboxexec", "exec"}},
		{RoleAppDeployer, [2]string{"deployment", "deployversion"}, [2]string{"sandboxexec", "exec"}},
		{RoleAppDeployer, [2]string{"builder", "buildfromtar"}, [2]string{"crud", "setenvvar"}},
		{RoleAppDebugger, [2]string{"sandboxexec", "exec"}, [2]string{"deployment", "deployversion"}},
		{RoleAppAdmin, [2]string{"crud", "setenvvar"}, [2]string{"crud", "list"}},
		{RoleAppAdmin, [2]string{"sandboxexec", "exec"}, [2]string{"crud", "destroy"}},
		{RoleClusterReadonly, [2]string{"crud", "list"}, [2]string{"crud", "setenvvar"}},
		{RoleClusterReadonly, [2]string{"logs", "sandboxlogs"}, [2]string{"sandboxexec", "exec"}},
		// cluster-deployer deploys/configures any app but does not do app
		// lifecycle: crud.new/destroy are cluster-admin's.
		{RoleClusterDeployer, [2]string{"deployment", "deployversion"}, [2]string{"crud", "destroy"}},
		{RoleClusterDeployer, [2]string{"crud", "setenvvar"}, [2]string{"crud", "new"}},
		{RoleClusterDebugger, [2]string{"internalhttp", "dorequest"}, [2]string{"crud", "destroy"}},
	}

	for _, c := range cases {
		role, ok := Lookup(c.role)
		if !ok {
			t.Fatalf("role %q not found", c.role)
		}
		if !has(role, c.granted[0], c.granted[1]) {
			t.Errorf("%s should grant %s.%s", c.role, c.granted[0], c.granted[1])
		}
		if has(role, c.denied[0], c.denied[1]) {
			t.Errorf("%s should NOT grant %s.%s", c.role, c.denied[0], c.denied[1])
		}
	}
}

// merge must not mutate its inputs — the blocks are shared package state.
func TestMergeDoesNotMutateInputs(t *testing.T) {
	before := len(appRead["logs"])
	_ = merge(appRead, appConfig)
	if len(appRead["logs"]) != before {
		t.Error("merge mutated the appRead block")
	}
	if appRead["crud"] != nil {
		t.Error("merge leaked appConfig's crud resource into appRead")
	}
}
