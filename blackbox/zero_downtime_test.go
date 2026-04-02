//go:build blackbox

package blackbox

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"miren.dev/runtime/blackbox/harness"
)

// TestZeroDowntimeDeploy verifies that HTTP requests continue to succeed during
// a redeploy. This exercises the proactive lease invalidation path: when old
// sandboxes go STOPPED, httpingress should evict their cached leases before
// any request hits a dead IP.
func TestZeroDowntimeDeploy(t *testing.T) {
	t.Parallel()
	c := harness.NewCluster(t)
	m := harness.NewMiren(t, c)

	// Deploy initial version
	name := harness.DeployApp(t, m, harness.AppOptions{
		Testdata: "go-server",
		Env:      []string{"DEPLOY_VERSION=v1"},
	})

	// Set a route so we can reach the app via HTTP ingress
	host := name + ".test.local"
	m.MustRun("route", "set", host, name)
	t.Cleanup(func() {
		m.Run("route", "remove", host)
	})

	// Verify HTTP works before we start
	harness.Poll(t, "initial HTTP reachable", 30*time.Second, 2*time.Second, func() (bool, string) {
		code, _, err := harness.HTTPGet(m, host, "/")
		if err != nil {
			return false, fmt.Sprintf("HTTP error: %v", err)
		}
		if code != 200 {
			return false, fmt.Sprintf("HTTP status %d", code)
		}
		return true, ""
	})

	// Start continuous HTTP requests in background
	var (
		totalRequests atomic.Int64
		failedResults []string
		failMu        sync.Mutex
		stop          = make(chan struct{})
		done          = make(chan struct{})
	)

	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}

			code, _, err := harness.HTTPGet(m, host, "/")
			totalRequests.Add(1)

			if err != nil || (code != 200 && code != 502 && code != 503) {
				// 502/503 during brief transition are noted but may be acceptable
				// in some edge cases; we track them all
			}
			if err != nil {
				failMu.Lock()
				failedResults = append(failedResults, fmt.Sprintf("request #%d: error: %v", totalRequests.Load(), err))
				failMu.Unlock()
			} else if code != 200 {
				failMu.Lock()
				failedResults = append(failedResults, fmt.Sprintf("request #%d: HTTP %d", totalRequests.Load(), code))
				failMu.Unlock()
			}

			time.Sleep(200 * time.Millisecond)
		}
	}()

	// Trigger a redeploy by changing an env var (forces new version + new sandbox)
	t.Log("triggering redeploy...")
	m.MustRun("deploy", "-a", name, "-d", m.ContainerPath(c.TestdataDir+"/go-server"), "-f", "-e", "DEPLOY_VERSION=v2")
	harness.WaitForAppReady(t, m, name, 3*time.Minute)

	// Let requests continue briefly after deploy settles to catch any stragglers
	time.Sleep(3 * time.Second)

	// Stop the request loop
	close(stop)
	<-done

	total := totalRequests.Load()
	t.Logf("total requests during deploy: %d", total)

	failMu.Lock()
	failures := failedResults
	failMu.Unlock()

	if len(failures) > 0 {
		t.Errorf("had %d failed requests out of %d during deploy:", len(failures), total)
		for _, f := range failures {
			t.Logf("  %s", f)
		}
	}
}
