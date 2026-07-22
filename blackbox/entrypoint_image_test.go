//go:build blackbox

package blackbox

import (
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

// TestDeployImageEntrypointNoCommand verifies that when a service sets no
// command, Miren runs the image's own ENTRYPOINT+CMD in exec form (argv, no
// /bin/sh -c). The fixture image declares no service command, so its ENTRYPOINT
// must run directly. Its CMD carries a $-literal arg; exec form delivers it
// verbatim, whereas a shell-wrapped command would let /bin/sh expand it to
// empty (the var is unset). See MIR-1444.
func TestDeployImageEntrypointNoCommand(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "entrypoint-image",
	})

	// The app only becomes healthy if the image's ENTRYPOINT ran and bound the
	// port, so reaching this point already proves the no-command exec path
	// launches. Now assert argv fidelity from the entrypoint's own logs.
	harness.Poll(t, "entrypoint argv logged", 30*time.Second, 2*time.Second,
		func() (bool, string) {
			r := m.Run("logs", "-a", name)
			// Verbatim, unexpanded: proves exec form, not /bin/sh -c.
			if r.OutputContains("arg[1]=<--note=$ENTRYPOINT_IMAGE_SHOULD_NOT_EXPAND>") {
				return true, ""
			}
			return false, "entrypoint argv not yet in logs"
		},
	)
}
