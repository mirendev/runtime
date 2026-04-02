//go:build blackbox

package blackbox

import (
	"testing"

	"miren.dev/runtime/blackbox/harness"
)

func TestEnvSetGetList(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
	})

	// Set an env var
	m.MustRun("env", "set", "-a", name, "-e", "MY_TEST_VAR=hello123")

	// Get the env var
	r := m.MustRun("env", "get", "MY_TEST_VAR", "-a", name)
	r.RequireContains(t, "hello123")

	// List env vars
	r = m.MustRun("env", "list", "-a", name)
	r.RequireContains(t, "MY_TEST_VAR")

	// Update the env var
	m.MustRun("env", "set", "-a", name, "-e", "MY_TEST_VAR=updated456")
	r = m.MustRun("env", "get", "MY_TEST_VAR", "-a", name)
	r.RequireContains(t, "updated456")
}
