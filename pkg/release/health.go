package release

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// HealthVerifier checks service health after upgrade
type HealthVerifier interface {
	VerifyHealth(ctx context.Context, timeout time.Duration) error
}

// HealthCheckOptions contains options for health verification
type HealthCheckOptions struct {
	// ServiceName is the systemd service name
	ServiceName string
	// HealthEndpoint is the HTTP health check endpoint
	HealthEndpoint string
	// MaxRetries is the maximum number of health check retries
	MaxRetries int
	// RetryDelay is the delay between retries
	RetryDelay time.Duration
}

// DefaultHealthCheckOptions returns default health check options
func DefaultHealthCheckOptions() HealthCheckOptions {
	return HealthCheckOptions{
		ServiceName: "miren",
		// Health endpoint available at :80/.well-known/miren/health (returns JSON with component checks)
		// Set this to check server health during upgrades
		HealthEndpoint: "",
		MaxRetries:     30,
		RetryDelay:     2 * time.Second,
	}
}

// systemdHealthVerifier implements HealthVerifier for systemd services
type systemdHealthVerifier struct {
	opts       HealthCheckOptions
	httpClient *http.Client
}

// NewHealthVerifier creates a new health verifier
func NewHealthVerifier(opts HealthCheckOptions) HealthVerifier {
	return &systemdHealthVerifier{
		opts: opts,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// VerifyHealth verifies the service is healthy after upgrade
func (v *systemdHealthVerifier) VerifyHealth(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Check systemd service status with retries (service may take a moment to become active after restart)
	if err := v.checkSystemdStatus(ctx, deadline); err != nil {
		return fmt.Errorf("systemd service check failed: %w", err)
	}

	// If health endpoint is configured, check it
	if v.opts.HealthEndpoint != "" {
		if err := v.checkHealthEndpoint(ctx, deadline); err != nil {
			return fmt.Errorf("health endpoint check failed: %w", err)
		}
	}

	return nil
}

// checkSystemdStatus checks if the systemd service is active, with retries
func (v *systemdHealthVerifier) checkSystemdStatus(ctx context.Context, deadline time.Time) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("systemd service management requires root privileges (running as uid %d)", os.Geteuid())
	}

	// Check if service exists first (only once, not on every retry)
	// Note: systemctl status returns non-zero when service is stopped, so use list-unit-files
	checkCmd := exec.CommandContext(ctx, "systemctl", "list-unit-files", v.opts.ServiceName+".service")
	checkOutput, err := checkCmd.Output()
	if err != nil || !strings.Contains(string(checkOutput), v.opts.ServiceName) {
		return fmt.Errorf("service %s not found", v.opts.ServiceName)
	}

	// Retry until service is active
	var lastErr error
	retries := 0

	for time.Now().Before(deadline) && retries < v.opts.MaxRetries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cmd := exec.CommandContext(ctx, "systemctl", "is-active", v.opts.ServiceName)
		output, err := cmd.Output()
		status := strings.TrimSpace(string(output))
		if err == nil && status == "active" {
			return nil
		}
		lastErr = fmt.Errorf("service %s is %s, expected active", v.opts.ServiceName, status)

		retries++
		if retries < v.opts.MaxRetries && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(v.opts.RetryDelay):
			}
		}
	}

	if lastErr != nil {
		return fmt.Errorf("%w (after %d retries)", lastErr, retries)
	}
	return fmt.Errorf("service did not become active after %d retries", retries)
}

// checkHealthEndpoint checks if the health endpoint is responding
func (v *systemdHealthVerifier) checkHealthEndpoint(ctx context.Context, deadline time.Time) error {
	retries := 0

	for time.Now().Before(deadline) && retries < v.opts.MaxRetries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, err := http.NewRequestWithContext(ctx, "GET", v.opts.HealthEndpoint, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := v.httpClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		retries++
		if retries < v.opts.MaxRetries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(v.opts.RetryDelay):
				// Continue to next retry
			}
		}
	}

	return fmt.Errorf("health endpoint did not respond after %d retries", retries)
}

// NoOpHealthVerifier is a health verifier that always succeeds (for testing)
type NoOpHealthVerifier struct{}

// VerifyHealth always returns success
func (n *NoOpHealthVerifier) VerifyHealth(ctx context.Context, timeout time.Duration) error {
	return nil
}

// IsServerRunning checks if the miren server is currently running as a systemd service
func IsServerRunning() bool {
	// Require root for systemd service checks
	if os.Geteuid() != 0 {
		return false
	}

	cmd := exec.Command("systemctl", "is-active", "miren")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	status := strings.TrimSpace(string(output))
	return status == "active"
}
