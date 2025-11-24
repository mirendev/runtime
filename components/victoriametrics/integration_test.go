package victoriametrics_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	vm "miren.dev/runtime/components/victoriametrics"
	"miren.dev/runtime/pkg/testutils"
)

const testNamespace = "miren-victoriametrics-test"

func TestVictoriaMetricsComponentIntegration(t *testing.T) {
	reg, cleanup := testutils.Registry()
	defer cleanup()

	var cc *containerd.Client
	err := reg.Resolve(&cc)
	require.NoError(t, err, "failed to resolve containerd client")

	// Create temporary directory for test data
	tmpDir, err := os.MkdirTemp("", "victoriametrics-test")
	require.NoError(t, err, "failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	// Create logger
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create VictoriaMetrics component
	component := vm.NewVictoriaMetricsComponent(log, cc, testNamespace, tmpDir)

	// Use test-specific port to avoid conflicts
	httpPort := 28428

	config := vm.VictoriaMetricsConfig{
		HTTPPort:        httpPort,
		DataPath:        tmpDir,
		RetentionPeriod: "1",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Ensure cleanup
	defer func() {
		if component.IsRunning() {
			err := component.Stop(ctx)
			if err != nil {
				t.Logf("failed to stop component: %v", err)
			}
		}

		// Clean up any remaining containers
		cleanupContainer(t, cc, testNamespace)
	}()

	// Start the VictoriaMetrics component
	t.Log("Starting VictoriaMetrics component...")
	err = component.Start(ctx, config)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			t.Skip("permission denied error, skipping test")
		}
		require.NoError(t, err, "failed to start VictoriaMetrics component")
	}

	// Verify component reports as running
	assert.True(t, component.IsRunning(), "component should report as running")

	// Get HTTP endpoint
	httpEndpoint := component.HTTPEndpoint()
	assert.NotEmpty(t, httpEndpoint, "HTTP endpoint should not be empty")

	expectedHTTPEndpoint := fmt.Sprintf("localhost:%d", httpPort)
	assert.Equal(t, expectedHTTPEndpoint, httpEndpoint, "HTTP endpoint should match expected")

	// Wait for VictoriaMetrics to be fully ready by polling health endpoint
	t.Log("Waiting for VictoriaMetrics to be ready...")
	client := &http.Client{Timeout: 5 * time.Second}
	require.Eventually(t, func() bool {
		resp, err := client.Get("http://" + httpEndpoint + "/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 500*time.Millisecond, "VictoriaMetrics failed to become ready")

	// Test HTTP endpoint availability
	t.Log("Testing HTTP endpoint...")
	resp, err := client.Get("http://" + httpEndpoint + "/metrics")
	require.NoError(t, err, "metrics endpoint should be reachable")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "metrics endpoint should return 200")
	t.Log("HTTP endpoint is responding")

	// Test basic VictoriaMetrics functionality
	t.Log("Testing VictoriaMetrics import functionality...")
	importURL := "http://" + httpEndpoint + "/api/v1/import/prometheus"
	metricData := "test_metric{label=\"value\"} 42 " + fmt.Sprintf("%d", time.Now().UnixMilli())

	importReq, err := http.NewRequestWithContext(ctx, "POST", importURL, strings.NewReader(metricData))
	if err == nil {
		importReq.Header.Set("Content-Type", "text/plain")
		importResp, err := client.Do(importReq)
		if err == nil {
			defer importResp.Body.Close()
			if importResp.StatusCode == http.StatusNoContent || importResp.StatusCode == http.StatusOK {
				t.Log("Successfully imported test metric")

				// Poll until metric is queryable (instead of fixed sleep)
				queryURL := "http://" + httpEndpoint + "/api/v1/query?query=test_metric"
				assert.Eventually(t, func() bool {
					queryResp, err := client.Get(queryURL)
					if err != nil {
						return false
					}
					defer queryResp.Body.Close()
					return queryResp.StatusCode == http.StatusOK
				}, 10*time.Second, 200*time.Millisecond, "metric query should eventually succeed")
				t.Log("Successfully queried test metric")
			} else {
				t.Logf("Import returned status %d", importResp.StatusCode)
			}
		} else {
			t.Logf("Failed to import test metric: %v", err)
		}
	}

	// Test restart functionality
	t.Log("Testing restart functionality...")
	err = component.Stop(ctx)
	require.NoError(t, err, "failed to stop component")

	assert.False(t, component.IsRunning(), "component should not report as running after stop")
	assert.Empty(t, component.HTTPEndpoint(), "endpoint should be empty when not running")

	// Start again - this should use the restart logic
	err = component.Start(ctx, config)
	require.NoError(t, err, "failed to restart VictoriaMetrics component")

	assert.True(t, component.IsRunning(), "component should report as running after restart")

	// Wait for VictoriaMetrics to be ready after restart by polling
	require.Eventually(t, func() bool {
		resp, err := client.Get("http://" + component.HTTPEndpoint() + "/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 500*time.Millisecond, "VictoriaMetrics failed to become ready after restart")

	// Test connectivity after restart
	resp2, err := client.Get("http://" + component.HTTPEndpoint() + "/metrics")
	require.NoError(t, err, "metrics endpoint should be reachable after restart")
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	t.Log("HTTP endpoint responding after restart")

	t.Log("Restart test completed successfully!")
}

func TestVictoriaMetricsComponent_DefaultConfig(t *testing.T) {
	reg, cleanup := testutils.Registry()
	defer cleanup()

	var cc *containerd.Client
	err := reg.Resolve(&cc)
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp("", "victoriametrics-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	component := vm.NewVictoriaMetricsComponent(log, cc, testNamespace, tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	defer func() {
		if component.IsRunning() {
			component.Stop(ctx)
		}
		cleanupContainer(t, cc, testNamespace)
	}()

	// Start with minimal config (defaults should be applied)
	config := vm.VictoriaMetricsConfig{}

	err = component.Start(ctx, config)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			t.Skip("permission denied error, skipping test")
		}
		require.NoError(t, err)
	}

	// Should use default port 8428
	expectedEndpoint := "localhost:8428"
	assert.Equal(t, expectedEndpoint, component.HTTPEndpoint())
}

func TestVictoriaMetricsComponent_AlreadyRunning(t *testing.T) {
	reg, cleanup := testutils.Registry()
	defer cleanup()

	var cc *containerd.Client
	err := reg.Resolve(&cc)
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp("", "victoriametrics-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	component := vm.NewVictoriaMetricsComponent(log, cc, testNamespace, tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	defer func() {
		if component.IsRunning() {
			component.Stop(ctx)
		}
		cleanupContainer(t, cc, testNamespace)
	}()

	config := vm.VictoriaMetricsConfig{HTTPPort: 28429}

	// Start once
	err = component.Start(ctx, config)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			t.Skip("permission denied error, skipping test")
		}
		require.NoError(t, err)
	}

	// Try to start again while running
	err = component.Start(ctx, config)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestVictoriaMetricsComponent_StopWhenNotRunning(t *testing.T) {
	reg, cleanup := testutils.Registry()
	defer cleanup()

	var cc *containerd.Client
	err := reg.Resolve(&cc)
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp("", "victoriametrics-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	component := vm.NewVictoriaMetricsComponent(log, cc, testNamespace, tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop without starting should be a no-op (idempotent)
	err = component.Stop(ctx)
	require.NoError(t, err, "Stop should be idempotent and not error when not running")
	assert.False(t, component.IsRunning(), "component should still not be running")
}

func TestVictoriaMetricsComponent_GracefulShutdown(t *testing.T) {
	reg, cleanup := testutils.Registry()
	defer cleanup()

	var cc *containerd.Client
	err := reg.Resolve(&cc)
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp("", "victoriametrics-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	component := vm.NewVictoriaMetricsComponent(log, cc, testNamespace, tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	defer cleanupContainer(t, cc, testNamespace)

	config := vm.VictoriaMetricsConfig{HTTPPort: 28430}

	err = component.Start(ctx, config)
	if err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			t.Skip("permission denied error, skipping test")
		}
		require.NoError(t, err)
	}

	// Wait for VictoriaMetrics to be fully ready by polling
	client := &http.Client{Timeout: 5 * time.Second}
	require.Eventually(t, func() bool {
		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/health", config.HTTPPort))
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 500*time.Millisecond, "VictoriaMetrics failed to become ready")

	// Stop should complete within reasonable time
	stopStart := time.Now()
	err = component.Stop(ctx)
	stopDuration := time.Since(stopStart)

	require.NoError(t, err)
	assert.Less(t, stopDuration, 35*time.Second, "graceful shutdown should complete within 35 seconds")
	assert.False(t, component.IsRunning())

	t.Logf("Graceful shutdown completed in %v", stopDuration)
}

func TestVictoriaMetricsComponent_MultipleStarts(t *testing.T) {
	reg, cleanup := testutils.Registry()
	defer cleanup()

	var cc *containerd.Client
	err := reg.Resolve(&cc)
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp("", "victoriametrics-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	component := vm.NewVictoriaMetricsComponent(log, cc, testNamespace, tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	defer cleanupContainer(t, cc, testNamespace)

	config := vm.VictoriaMetricsConfig{HTTPPort: 28431}

	// Start, stop, start, stop multiple times
	client := &http.Client{Timeout: 5 * time.Second}
	for i := 0; i < 3; i++ {
		t.Logf("Cycle %d: Starting...", i+1)
		err = component.Start(ctx, config)
		if err != nil {
			if strings.Contains(err.Error(), "permission denied") {
				t.Skip("permission denied error, skipping test")
			}
			require.NoError(t, err, "failed to start on cycle %d", i+1)
		}

		assert.True(t, component.IsRunning(), "should be running after start on cycle %d", i+1)

		// Wait for VictoriaMetrics to be ready by polling
		require.Eventually(t, func() bool {
			resp, err := client.Get(fmt.Sprintf("http://localhost:%d/health", config.HTTPPort))
			if err != nil {
				return false
			}
			resp.Body.Close()
			return resp.StatusCode == http.StatusOK
		}, 30*time.Second, 500*time.Millisecond, "VictoriaMetrics failed to become ready on cycle %d", i+1)

		t.Logf("Cycle %d: Stopping...", i+1)
		err = component.Stop(ctx)
		require.NoError(t, err, "failed to stop on cycle %d", i+1)
		assert.False(t, component.IsRunning(), "should not be running after stop on cycle %d", i+1)
	}

	t.Log("Multiple start/stop cycles completed successfully!")
}

func cleanupContainer(t *testing.T, cc *containerd.Client, namespace string) {
	ctx := context.Background()
	ctx = namespaces.WithNamespace(ctx, namespace)

	// Try to find and delete any test containers
	containers, err := cc.Containers(ctx)
	if err != nil {
		t.Logf("failed to list containers for cleanup: %v", err)
		return
	}

	for _, container := range containers {
		// Stop and delete task if it exists
		task, err := container.Task(ctx, nil)
		if err == nil {
			task.Kill(ctx, unix.SIGTERM)
			es, err := task.Wait(ctx)
			require.NoError(t, err)

			select {
			case <-ctx.Done():
				require.NoError(t, ctx.Err())
			case <-es:
				//ok
			}

			task.Delete(ctx)
		}

		// Delete container
		err = container.Delete(ctx, containerd.WithSnapshotCleanup)
		if err != nil {
			t.Logf("failed to delete container %s: %v", container.ID(), err)
		} else {
			t.Logf("cleaned up container %s", container.ID())
		}
	}
}
