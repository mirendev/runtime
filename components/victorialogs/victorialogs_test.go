//go:build linux

package victorialogs_test

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
	"miren.dev/runtime/components/victorialogs"
	"miren.dev/runtime/pkg/containerdx"
)

func TestVictoriaLogsComponent(t *testing.T) {
	if os.Getenv("SKIP_COMPONENT_TEST") != "" {
		t.Skip("Skipping component test")
	}

	t.Run("can start and stop VictoriaLogs", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))

		tmpDir := t.TempDir()

		component := victorialogs.NewVictoriaLogsComponent(logger, cc, "miren-test", tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        9428,
			RetentionPeriod: "7d",
		}

		err = component.Start(ctx, config)
		r.NoError(err)

		r.True(component.IsRunning())
		r.Equal("127.0.0.1:9428", component.HTTPEndpoint())

		// Give it a moment to fully start
		time.Sleep(2 * time.Second)

		// Stop the component
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()

		err = component.Stop(stopCtx)
		r.NoError(err)

		r.False(component.IsRunning())
	})

	t.Run("cannot start twice", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		tmpDir := t.TempDir()

		component := victorialogs.NewVictoriaLogsComponent(logger, cc, "miren-test", tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        9429,
			RetentionPeriod: "7d",
		}

		err = component.Start(ctx, config)
		r.NoError(err)
		defer component.Stop(ctx)

		// Try to start again
		err = component.Start(ctx, config)
		r.Error(err)
		r.Contains(err.Error(), "already running")
	})

	t.Run("can stop when not running", func(t *testing.T) {
		r := require.New(t)

		ctx := context.Background()

		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		tmpDir := t.TempDir()

		component := victorialogs.NewVictoriaLogsComponent(logger, cc, "miren-test", tmpDir)

		// Stop without starting should be no-op
		err = component.Stop(ctx)
		r.NoError(err)
	})

	t.Run("uses custom port", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		tmpDir := t.TempDir()

		component := victorialogs.NewVictoriaLogsComponent(logger, cc, "miren-test", tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        9430,
			RetentionPeriod: "7d",
		}

		err = component.Start(ctx, config)
		r.NoError(err)
		defer component.Stop(ctx)

		r.Equal("127.0.0.1:9430", component.HTTPEndpoint())
	})

	t.Run("uses custom retention period", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		tmpDir := t.TempDir()

		component := victorialogs.NewVictoriaLogsComponent(logger, cc, "miren-test", tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        9431,
			RetentionPeriod: "14d",
		}

		err = component.Start(ctx, config)
		r.NoError(err)
		defer component.Stop(ctx)

		r.True(component.IsRunning())
	})
}

func TestVictoriaLogsComponentAutoRestart(t *testing.T) {
	if os.Getenv("SKIP_COMPONENT_TEST") != "" {
		t.Skip("Skipping component test")
	}

	r := require.New(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cc, err := containerd.New(containerdx.DefaultSocket)
	r.NoError(err)
	defer cc.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Use a unique namespace to avoid conflicts
	testNamespace := fmt.Sprintf("miren-vl-restart-test-%d", time.Now().UnixNano())
	tmpDir := t.TempDir()

	component := victorialogs.NewVictoriaLogsComponent(logger, cc, testNamespace, tmpDir)

	// Use a unique port to avoid conflicts
	httpPort := 9450

	config := victorialogs.VictoriaLogsConfig{
		HTTPPort:        httpPort,
		RetentionPeriod: "7d",
	}

	// Ensure cleanup
	defer func() {
		if component.IsRunning() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer stopCancel()
			if err := component.Stop(stopCtx); err != nil {
				t.Logf("failed to stop component: %v", err)
			}
		}
		cleanupContainers(t, cc, testNamespace)
	}()

	t.Log("Starting victorialogs component...")
	err = component.Start(ctx, config)
	r.NoError(err)
	r.True(component.IsRunning())

	endpoint := fmt.Sprintf("http://127.0.0.1:%d", httpPort)

	// Wait for victorialogs to be fully ready
	r.Eventually(func() bool {
		resp, err := http.Get(endpoint + "/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 500*time.Millisecond, "victorialogs should be healthy")

	t.Log("victorialogs is ready, now killing the task to simulate crash...")

	// Kill the victorialogs task directly using containerd to simulate a crash
	nsCtx := namespaces.WithNamespace(context.Background(), testNamespace)
	containers, err := cc.Containers(nsCtx)
	r.NoError(err, "failed to list containers")

	var vlContainer containerd.Container
	for _, c := range containers {
		if strings.Contains(c.ID(), "victorialogs") {
			vlContainer = c
			break
		}
	}
	r.NotNil(vlContainer, "should find victorialogs container")

	task, err := vlContainer.Task(nsCtx, nil)
	r.NoError(err, "should get task")

	// Kill the task with SIGKILL to simulate crash
	err = task.Kill(nsCtx, unix.SIGKILL)
	r.NoError(err, "should be able to kill task")

	t.Log("Task killed, waiting for auto-restart...")

	// Wait for the component to auto-restart and become ready again
	// The restart has a 2 second backoff for the first restart
	r.Eventually(func() bool {
		resp, err := http.Get(endpoint + "/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 45*time.Second, 1*time.Second, "victorialogs should auto-restart and become healthy")

	t.Log("victorialogs auto-restarted successfully!")

	// Verify the component still reports as running
	assert.True(t, component.IsRunning(), "component should still report as running after auto-restart")

	t.Log("Auto-restart test completed successfully!")
}

func cleanupContainers(t *testing.T, cc *containerd.Client, namespace string) {
	ctx := namespaces.WithNamespace(context.Background(), namespace)

	containers, err := cc.Containers(ctx)
	if err != nil {
		t.Logf("failed to list containers for cleanup: %v", err)
		return
	}

	for _, container := range containers {
		task, err := container.Task(ctx, nil)
		if err == nil {
			task.Kill(ctx, unix.SIGTERM)
			task.Wait(ctx)
			task.Delete(ctx)
		}

		err = container.Delete(ctx, containerd.WithSnapshotCleanup)
		if err != nil {
			t.Logf("failed to delete container %s: %v", container.ID(), err)
		} else {
			t.Logf("cleaned up container %s", container.ID())
		}
	}
}
