package sandbox

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/saga"
)

// healthStub replaces the health check's HTTP GET with answers taken in order;
// the last answer repeats. It records every URL asked for.
type healthStub struct {
	mu      sync.Mutex
	answers []healthAnswer
	urls    []string
}

type healthAnswer struct {
	status int
	err    error
}

func stubHealthCheck(t *testing.T, answers ...healthAnswer) *healthStub {
	t.Helper()
	s := &healthStub{answers: answers}
	prevGet, prevInterval := healthCheckGet, healthCheckInterval
	healthCheckGet = func(ctx context.Context, url string) (int, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.urls = append(s.urls, url)
		a := s.answers[0]
		if len(s.answers) > 1 {
			s.answers = s.answers[1:]
		}
		return a.status, a.err
	}
	healthCheckInterval = time.Millisecond
	t.Cleanup(func() { healthCheckGet, healthCheckInterval = prevGet, prevInterval })
	return s
}

// withHealthCheck gives the harness's sandbox an HTTP port and a health check.
func withHealthCheck(h *testHarness, path, timeout string) {
	h.entities.sandbox.Spec.HealthCheckPath = path
	h.entities.sandbox.Spec.HealthCheckTimeout = timeout
	h.entities.sandbox.Spec.Container = []compute.SandboxSpecContainer{{
		Name: "app",
		Port: []compute.SandboxSpecContainerPort{{Port: 8080, Name: "http", Type: "http"}},
	}}
}

func TestCreateSandboxSaga_NoHealthCheckSkipsProbe(t *testing.T) {
	stub := stubHealthCheck(t, healthAnswer{status: 500})
	h := newTestHarness(t)

	require.NoError(t, h.execute(t))
	assert.Empty(t, stub.urls, "a sandbox without a health check must not be probed")
	assert.Contains(t, h.execution(t).ExecutionOrder, actionSetRunning)
}

func TestCreateSandboxSaga_HealthCheckPassesAfterRetries(t *testing.T) {
	stub := stubHealthCheck(t,
		healthAnswer{err: fmt.Errorf("connection refused")},
		healthAnswer{status: 503},
		healthAnswer{status: 200},
	)
	h := newTestHarness(t)
	withHealthCheck(h, "/up", "5s")

	require.NoError(t, h.execute(t))

	exec := h.execution(t)
	assert.Equal(t, saga.StatusCompleted, exec.Status)
	require.Len(t, stub.urls, 3)
	assert.Equal(t, "http://10.0.0.5:8080/up", stub.urls[0])

	order := exec.ExecutionOrder
	assert.Less(t, indexOf(order, actionWaitHealthy), indexOf(order, actionSetRunning),
		"the sandbox must not be RUNNING before its health check passes")
	assert.Less(t, indexOf(order, actionWaitHealthy), indexOf(order, actionUpdateSvcs),
		"the sandbox must not join its services before its health check passes")
}

func TestCreateSandboxSaga_HealthCheckRedirectPasses(t *testing.T) {
	stubHealthCheck(t, healthAnswer{status: 302})
	h := newTestHarness(t)
	withHealthCheck(h, "/", "5s")

	require.NoError(t, h.execute(t))
	assert.Contains(t, h.execution(t).ExecutionOrder, actionSetRunning)
}

func TestCreateSandboxSaga_HealthCheckFailsAsWorkload(t *testing.T) {
	stubHealthCheck(t, healthAnswer{status: 500})
	h := newTestHarness(t)
	withHealthCheck(h, "/up", "30ms")

	err := h.execute(t)
	require.Error(t, err)
	assert.True(t, cond.IsWorkload(err), "a failing health check is the app's problem, not Miren's")

	exec := h.execution(t)
	assert.Equal(t, saga.StatusFailed, exec.Status)
	assert.NotContains(t, exec.ExecutionOrder, actionSetRunning)
	assert.Equal(t, 0, h.obs.updateSvcsCalls, "a sandbox that failed its check must not join its services")

	require.Len(t, h.obs.loggedEvents, 1)
	assert.Contains(t, h.obs.loggedEvents[0], "GET /up")
	assert.Contains(t, h.obs.loggedEvents[0], "answered 500")

	// The booted sandbox is torn down as for any other failed start.
	assert.Equal(t, 1, h.runtime.destroySubCtrsCalls)
	assert.Equal(t, 1, h.networking.releaseCalls)
}

func TestCreateSandboxSaga_HealthCheckFollowsReroutedPort(t *testing.T) {
	stub := stubHealthCheck(t, healthAnswer{status: 200})
	h := newTestHarness(t)
	withHealthCheck(h, "/up", "5s")
	// The configured port (8080) never binds; the app is on 3000 instead.
	h.runtime.waitForPortErr = fmt.Errorf("timeout waiting for port 8080")
	h.runtime.diagnoseOK = true
	h.runtime.diagnoseRoutable = []int{3000}

	require.NoError(t, h.execute(t))
	require.NotEmpty(t, stub.urls)
	assert.Equal(t, "http://10.0.0.5:3000/up", stub.urls[0])
}

func TestHealthCheckURL(t *testing.T) {
	sb := &compute.Sandbox{Spec: compute.SandboxSpec{Container: []compute.SandboxSpecContainer{{
		Port: []compute.SandboxSpecContainerPort{
			{Port: 9000, Type: "tcp"},
			{Port: 8080, Type: "http"},
		},
	}}}}

	got, err := healthCheckURL(sb, []string{"10.8.0.5/24", "fd00::5/64"}, nil, "/up")
	require.NoError(t, err)
	assert.Equal(t, "http://10.8.0.5:8080/up", got, "the HTTP port on the first address, as the activator routes")

	got, err = healthCheckURL(sb, []string{"fd00::5/64"}, nil, "/up?deep=1")
	require.NoError(t, err)
	assert.Equal(t, "http://[fd00::5]:8080/up?deep=1", got)

	got, err = healthCheckURL(sb, []string{"10.8.0.5/24"}, []observedPort{{Port: 4000}}, "/up")
	require.NoError(t, err)
	assert.Equal(t, "http://10.8.0.5:4000/up", got, "a rerouted port wins, as it does for routing")

	got, err = healthCheckURL(&compute.Sandbox{}, []string{"10.8.0.5/24"}, nil, "/up")
	require.NoError(t, err)
	assert.Equal(t, "http://10.8.0.5:3000/up", got, "the activator's default port")

	_, err = healthCheckURL(sb, nil, nil, "/up")
	assert.Error(t, err, "no address")
}

func TestResolveHealthCheckTimeout(t *testing.T) {
	assert.Equal(t, defaultHealthCheckTimeout, resolveHealthCheckTimeout(""))
	assert.Equal(t, defaultHealthCheckTimeout, resolveHealthCheckTimeout("soon"))
	assert.Equal(t, defaultHealthCheckTimeout, resolveHealthCheckTimeout("0s"))
	assert.Equal(t, 2*time.Minute, resolveHealthCheckTimeout("2m"))
}

func TestHTTPHealthCheckGetDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "miren-health-check", r.UserAgent())
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	status, err := httpHealthCheckGet(context.Background(), srv.URL+"/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, status)
}

func indexOf(order []string, action string) int {
	for i, a := range order {
		if a == action {
			return i
		}
	}
	return -1
}

func TestCreateSandboxSaga_HealthCheckFailsFastWhenAppExits(t *testing.T) {
	stubHealthCheck(t, healthAnswer{err: fmt.Errorf("connection refused")})
	h := newTestHarness(t)
	withHealthCheck(h, "/up", "30s")
	// wait-ports sees the port bind (call 1); the app then exits.
	h.runtime.waitForPortFn = func(call int) error {
		if call == 1 {
			return nil
		}
		return errProcessExited
	}

	start := time.Now()
	err := h.execute(t)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "an exit must not wait out the 30s timeout")
	assert.True(t, cond.IsWorkload(err))
	require.Len(t, h.obs.loggedEvents, 1)
	assert.Contains(t, h.obs.loggedEvents[0], "app exited during its health check")
}
