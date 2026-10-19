package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	compute "miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/netutil"
	"miren.dev/runtime/pkg/saga"
)

// --- Wait healthy ---
//
// A service that declares a health check isn't RUNNING, and so takes no
// traffic, until a GET of its path answers 2xx or 3xx. The launcher keeps the
// previous version's pool serving until the new one has a RUNNING sandbox, so
// a version that boots but doesn't work never replaces one that does.

const defaultHealthCheckTimeout = 60 * time.Second

// healthCheckInterval is the pause between attempts, and healthCheckGet makes
// one attempt. Both are variables so tests can run without a network.
var (
	healthCheckInterval = time.Second
	healthCheckGet      = httpHealthCheckGet
)

// healthCheckClient never follows redirects (a 3xx passes as is) and never
// goes through a proxy: the sandbox address is only reachable from this node.
var healthCheckClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func httpHealthCheckGet(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "miren-health-check")
	resp, err := healthCheckClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, nil
}

// resolveHealthCheckTimeout parses SandboxSpec.HealthCheckTimeout, falling back
// to the default on empty, invalid, or non-positive values, as port waits do.
func resolveHealthCheckTimeout(spec string) time.Duration {
	if spec == "" {
		return defaultHealthCheckTimeout
	}
	d, err := time.ParseDuration(spec)
	if err != nil || d <= 0 {
		return defaultHealthCheckTimeout
	}
	return d
}

type waitHealthyIn struct {
	SandboxID     string         `json:"sandbox_id" saga:"sandbox_id"`
	Addresses     []string       `json:"addresses" saga:"addresses"`
	WaitPortIDs   []string       `json:"wait_port_ids" saga:"wait_port_ids"`
	WaitPortPorts []int          `json:"wait_port_ports" saga:"wait_port_ports"`
	ObservedPorts []observedPort `json:"observed_ports" saga:"observed_ports"`
	PortsReady    saga.Edge      `saga:"ports_ready"`
}

type waitHealthyOut struct {
	Healthy saga.Edge `saga:"healthy"`
}

func waitHealthy(ctx context.Context, in waitHealthyIn) (waitHealthyOut, error) {
	deps := saga.Get[*createSandboxDeps](ctx)
	log := saga.Get[*slog.Logger](ctx)

	sb, _, err := deps.entities.GetSandbox(ctx, in.SandboxID)
	if err != nil {
		return waitHealthyOut{}, fmt.Errorf("fetching sandbox: %w", err)
	}
	path := sb.Spec.HealthCheckPath
	if path == "" {
		return waitHealthyOut{}, nil
	}

	url, err := healthCheckURL(sb, in.Addresses, in.ObservedPorts, path)
	if err != nil {
		return waitHealthyOut{}, fmt.Errorf("health check: %w", err)
	}
	timeout := resolveHealthCheckTimeout(sb.Spec.HealthCheckTimeout)

	log.Info("saga: waiting for health check", "sandbox", in.SandboxID, "url", url, "timeout", timeout)

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var last string
	for {
		if exitErr := containerExited(checkCtx, deps, in.WaitPortIDs, in.WaitPortPorts); exitErr != nil {
			msg := fmt.Sprintf("app exited during its health check (GET %s)", path)
			deps.logSandboxEvent(ctx, in.SandboxID, msg)
			return waitHealthyOut{}, cond.Workload(fmt.Errorf("%s: %w", msg, exitErr))
		}

		status, err := healthCheckGet(checkCtx, url)
		switch {
		case err == nil && status >= 200 && status < 400:
			log.Info("saga: health check passed", "sandbox", in.SandboxID, "status", status)
			return waitHealthyOut{}, nil
		case err == nil:
			last = "answered " + strconv.Itoa(status)
		case checkCtx.Err() == nil:
			last = err.Error()
		}

		select {
		case <-checkCtx.Done():
		case <-time.After(healthCheckInterval):
			continue
		}
		break
	}

	if ctx.Err() != nil {
		// Shutting down, not a failed check.
		return waitHealthyOut{}, ctx.Err()
	}
	if last == "" {
		last = "no answer"
	}
	msg := fmt.Sprintf("health check GET %s did not pass within %s (last attempt: %s)", path, timeout, last)
	deps.logSandboxEvent(ctx, in.SandboxID, msg)
	return waitHealthyOut{}, cond.Workload(errors.New(msg))
}

// containerExited returns errProcessExited's error if a container wait-ports
// waited on has since exited, so a crash fails the check now rather than at
// its timeout. WaitForPort reports exit before it looks at ports, and with a
// port that already bound it returns at once.
func containerExited(ctx context.Context, deps *createSandboxDeps, ids []string, ports []int) error {
	for i := range ids {
		if i >= len(ports) {
			break
		}
		if err := deps.runtime.WaitForPort(ctx, ids[i], ports[i], 10*time.Millisecond); errors.Is(err, errProcessExited) {
			return err
		}
	}
	return nil
}

// healthCheckURL returns the URL to check: the address and port the activator
// will route the sandbox's traffic to, so the check sees what requests will.
func healthCheckURL(sb *compute.Sandbox, addresses []string, observed []observedPort, path string) (string, error) {
	if len(addresses) == 0 {
		return "", fmt.Errorf("sandbox has no address")
	}
	// The activator's port: the first HTTP port (3000 if none is declared), or
	// the port the app bound instead, which set-running records as bound_port.
	port := int64(3000)
	for _, c := range sb.Spec.Container {
		if p := slices.IndexFunc(c.Port, func(p compute.SandboxSpecContainerPort) bool { return p.Type == "http" }); p >= 0 {
			port = c.Port[p].Port
			break
		}
	}
	if len(observed) > 0 && observed[0].Port > 0 {
		port = int64(observed[0].Port)
	}
	// patch-network records addresses in this order; the activator uses the first.
	base, err := netutil.BuildHTTPURL(addresses[0], port)
	if err != nil {
		return "", err
	}
	return base + path, nil
}

func undoWaitHealthy(_ context.Context, _ waitHealthyIn, _ waitHealthyOut) error {
	return nil
}
