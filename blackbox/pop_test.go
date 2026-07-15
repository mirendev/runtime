//go:build blackbox

package blackbox

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

// TestPOP exercises Miren Anywhere end-to-end. The expensive cloud+POP
// setup (binary builds, migrations, server restart) happens once in the
// parent test; each subtest only deploys its own app, binds a hostname, and
// sends traffic.
func TestPOP(t *testing.T) {
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)
	env := harness.NewCloudEnv(t, m)

	// TestHTTPForwarding: full HTTP path through POP to go-server app.
	//
	//	client → POP TLS → cloud coordination → QUIC H3 → cluster → app → response
	appName := harness.DeployApp(t, m, harness.AppOptions{Testdata: "go-server"})
	hostname := appName + ".test.pop"
	m.MustRun("route", "set", hostname, appName)
	env.BindAppHostname(t, hostname)

	// Assert on the response body to confirm the request actually
	// traversed the full tunnel (not just a 200 from a proxy error page).
	harness.Poll(t, "HTTP via POP", 90*time.Second, 3*time.Second, func() (bool, string) {
		code, body, err := harness.HTTPGetViaPOP(m, env.PopListenPort, hostname, "/")
		if err != nil {
			return false, fmt.Sprintf("curl error: %v", err)
		}
		if code != 200 {
			return false, fmt.Sprintf("status %d, body: %s", code, body)
		}
		if !strings.Contains(body, "Hello from Go!") {
			return false, fmt.Sprintf("unexpected body (not from go-server): %q", body)
		}
		return true, ""
	})
}
