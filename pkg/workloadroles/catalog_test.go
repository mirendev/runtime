package workloadroles_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/api/admin/admin_v1alpha"
	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/build/build_v1alpha"
	"miren.dev/runtime/api/debug/debug_v1alpha"
	"miren.dev/runtime/api/deployment/deployment_v1alpha"
	"miren.dev/runtime/api/exec/exec_v1alpha"
	"miren.dev/runtime/api/metric/metric_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/api/usage/usage_v1alpha"
	"miren.dev/runtime/pkg/rpc"
	"miren.dev/runtime/pkg/workloadroles"
)

// realMethods maps a catalog resource to the methods its generated interface
// actually defines.
//
// The adapters are handed a nil implementation deliberately: nothing here
// dispatches a call, it only reads the method table the generator emitted.
//
// This is not every interface in the catalog -- adding one is a line here plus
// an import -- but an entry that IS listed is checked exactly.
func realMethods(t *testing.T) map[string]map[string]bool {
	t.Helper()

	ifaces := []*rpc.Interface{
		usage_v1alpha.AdaptResourceUsage(nil),
		metric_v1alpha.AdaptSandboxMetrics(nil),
		runner_v1alpha.AdaptRunnerRegistration(nil),
		exec_v1alpha.AdaptSandboxExec(nil),
		app_v1alpha.AdaptCrud(nil),
		app_v1alpha.AdaptAppStatus(nil),
		app_v1alpha.AdaptLogs(nil),
		app_v1alpha.AdaptAddons(nil),
		deployment_v1alpha.AdaptDeployment(nil),
		build_v1alpha.AdaptBuilder(nil),
		admin_v1alpha.AdaptAdmin(nil),
		debug_v1alpha.AdaptNetDB(nil),
	}

	out := map[string]map[string]bool{}
	for _, iface := range ifaces {
		for _, m := range iface.Methods() {
			resource := strings.ToLower(m.InterfaceName)
			if out[resource] == nil {
				out[resource] = map[string]bool{}
			}
			out[resource][strings.ToLower(m.Name)] = true
		}
	}

	require.NotEmpty(t, out, "no interfaces resolved; the adapters may have changed shape")

	return out
}

// A role granting a method that does not exist grants nothing, and nothing else
// reports it: the catalog compiles, the spot-check tests pass, and the failure
// only shows up as an authorization denial against a live cluster.
//
// This is exactly how the usage API shipped briefly unreachable -- listSandboxes
// and getSandbox were renamed and the catalog kept naming the old ones, so every
// workload identity token was denied the very API the rename was meant to
// improve. The CLI never noticed because it authenticates with a certificate
// rather than a role.
func TestCatalogNamesOnlyRealMethods(t *testing.T) {
	known := realMethods(t)

	for roleName, role := range workloadroles.Roles {
		for resource, actions := range role.Perms {
			methods, checked := known[resource]
			if !checked {
				// An interface this test does not import yet. Skipping is the
				// honest thing to do; the alternative is a false failure that
				// teaches people to delete the assertion.
				continue
			}

			for action := range actions {
				assert.Truef(t, methods[action],
					"role %q grants %s.%s, which the generated interface does not define",
					roleName, resource, action)
			}
		}
	}
}

// The reverse direction is a warning, not a rule: a method nobody grants is
// often correct (cert-only internals are deliberately ungranted). This asserts
// only the case we care about -- that the usage API, whose whole point is being
// callable by something other than the CLI, is fully reachable by a cluster
// reader.
func TestClusterReadonlyCanReachEveryUsageMethod(t *testing.T) {
	role, ok := workloadroles.Lookup(workloadroles.RoleClusterReadonly)
	require.True(t, ok)

	granted := role.Perms["resourceusage"]
	require.NotEmpty(t, granted, "cluster-readonly must be able to read usage")

	for _, m := range usage_v1alpha.AdaptResourceUsage(nil).Methods() {
		action := strings.ToLower(m.Name)
		assert.Truef(t, granted[action],
			"cluster-readonly cannot call resourceusage.%s; a caller using workload identity would be denied", action)
	}
}
