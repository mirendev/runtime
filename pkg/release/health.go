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
		// TODO: Implement proper health check endpoint in miren server
		// Once implemented, this should be set to the actual health endpoint URL
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

	// First, check systemd service status
	if err := v.checkSystemdStatus(ctx); err != nil {
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

// checkSystemdStatus checks if the systemd service is active
func (v *systemdHealthVerifier) checkSystemdStatus(ctx context.Context) error {
	// Use --user flag for user services when not running as root
	args := []string{"is-active", v.opts.ServiceName}
	if os.Geteuid() != 0 {
		args = append([]string{"--user"}, args...)
	}

	cmd := exec.CommandContext(ctx, "systemctl", args...)
	output, err := cmd.Output()
	if err != nil {
		// Check if service exists
		statusArgs := []string{"status", v.opts.ServiceName}
		if os.Geteuid() != 0 {
			statusArgs = append([]string{"--user"}, statusArgs...)
		}
		checkCmd := exec.CommandContext(ctx, "systemctl", statusArgs...)
		if checkErr := checkCmd.Run(); checkErr != nil {
			return fmt.Errorf("service %s not found", v.opts.ServiceName)
		}
		return fmt.Errorf("service %s is not active", v.opts.ServiceName)
	}

	status := strings.TrimSpace(string(output))
	if status != "active" {
		return fmt.Errorf("service %s is %s, expected active", v.opts.ServiceName, status)
	}

	return nil
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
	cmd := exec.Command("systemctl", "is-active", "miren")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	status := strings.TrimSpace(string(output))
	return status == "active"
}
