//go:build linux

package buildkit_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/pkg/containerdx"
)

const testNamespace = "miren-test"

func TestBuildkitComponent(t *testing.T) {
	if os.Getenv("SKIP_COMPONENT_TEST") != "" {
		t.Skip("Skipping component test")
	}

	t.Run("can start and stop BuildKit", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))

		tmpDir := t.TempDir()

		component := buildkit.NewComponent(logger, cc, "miren-test", tmpDir)

		config := buildkit.Config{
			SocketDir:      t.TempDir(),
			GCKeepStorage:  1024 * 1024 * 1024, // 1GB
			GCKeepDuration: 86400,              // 1 day
			RegistryHost:   "cluster.local:5000",
		}

		err = component.Start(ctx, config)
		r.NoError(err)

		r.True(component.IsRunning())
		r.NotEmpty(component.SocketPath())

		// Verify we can get a client
		client, err := component.Client(ctx)
		r.NoError(err)
		r.NotNil(client)

		// Get daemon info to confirm it's working
		info, err := client.Info(ctx)
		r.NoError(err)
		r.NotEmpty(info.BuildkitVersion.Version)

		client.Close()

		// Stop the component
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()

		err = component.Stop(stopCtx)
		r.NoError(err)

		r.False(component.IsRunning())
	})

	t.Run("cannot start twice", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		tmpDir := t.TempDir()

		component := buildkit.NewComponent(logger, cc, "miren-test", tmpDir)

		config := buildkit.Config{
			SocketDir:    t.TempDir(),
			RegistryHost: "cluster.local:5000",
		}

		err = component.Start(ctx, config)
		r.NoError(err)
		defer component.Stop(context.Background())

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

		component := buildkit.NewComponent(logger, cc, "miren-test", tmpDir)

		// Stop without starting should be no-op
		err = component.Stop(ctx)
		r.NoError(err)
	})

	t.Run("client fails when not running", func(t *testing.T) {
		r := require.New(t)

		ctx := context.Background()

		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		tmpDir := t.TempDir()

		component := buildkit.NewComponent(logger, cc, "miren-test", tmpDir)

		// Client should fail when not running
		_, err = component.Client(ctx)
		r.Error(err)
		r.Contains(err.Error(), "not running")
	})

	t.Run("uses default GC settings", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		tmpDir := t.TempDir()

		component := buildkit.NewComponent(logger, cc, "miren-test", tmpDir)

		// Use zero values to trigger defaults
		config := buildkit.Config{
			SocketDir:      t.TempDir(),
			GCKeepStorage:  0, // Should use default
			GCKeepDuration: 0, // Should use default
		}

		err = component.Start(ctx, config)
		r.NoError(err)
		defer component.Stop(context.Background())

		r.True(component.IsRunning())
	})
}

// buildkitTaskPID loads the miren-buildkit container's task PID directly via
// containerd, so tests can prove a restart replaced the underlying process.
func buildkitTaskPID(ctx context.Context, cc *containerd.Client) (uint32, error) {
	ctx = namespaces.WithNamespace(ctx, testNamespace)
	container, err := cc.LoadContainer(ctx, "miren-buildkit")
	if err != nil {
		return 0, err
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		return 0, err
	}
	return task.Pid(), nil
}

func buildkitContainerInfo(ctx context.Context, t *testing.T, cc *containerd.Client) (time.Time, []string) {
	t.Helper()
	ctx = namespaces.WithNamespace(ctx, testNamespace)
	container, err := cc.LoadContainer(ctx, "miren-buildkit")
	require.NoError(t, err)
	info, err := container.Info(ctx)
	require.NoError(t, err)
	spec, err := container.Spec(ctx)
	require.NoError(t, err)
	return info.CreatedAt, spec.Process.Env
}

// TestBuildkitRestart covers the miren-restart scenario (MIR-1303): a fresh
// Component that finds an already-running buildkit container must evict the
// stale task and bind a new one, and the exit monitor must reflect an
// out-of-band daemon death.
func TestBuildkitRestart(t *testing.T) {
	if os.Getenv("SKIP_COMPONENT_TEST") != "" {
		t.Skip("Skipping component test")
	}

	newClient := func(t *testing.T) *containerd.Client {
		cc, err := containerd.New(containerdx.DefaultSocket)
		require.NoError(t, err)
		t.Cleanup(func() { cc.Close() })
		return cc
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	t.Run("restart evicts the stale task and recreates a working daemon", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		cc := newClient(t)
		dataDir := t.TempDir()
		config := buildkit.Config{SocketDir: t.TempDir(), RegistryHost: "cluster.local:5000"}

		// First "process" launches the daemon.
		first := buildkit.NewComponent(logger, cc, testNamespace, dataDir)
		r.NoError(first.Start(ctx, config))
		r.True(first.IsRunning())

		pidBefore, err := buildkitTaskPID(ctx, cc)
		r.NoError(err)

		// Second "process" (a miren restart) reuses the existing container but
		// must force-stop the stale task and start a fresh one bound to itself.
		second := buildkit.NewComponent(logger, cc, testNamespace, dataDir)
		r.NoError(second.Start(ctx, config))
		r.True(second.IsRunning())

		pidAfter, err := buildkitTaskPID(ctx, cc)
		r.NoError(err)
		r.NotEqual(pidBefore, pidAfter, "restart must replace the buildkitd process, not reuse it")

		// The freshly bound daemon must actually serve requests.
		client, err := second.Client(ctx)
		r.NoError(err)
		info, err := client.Info(ctx)
		r.NoError(err)
		r.NotEmpty(info.BuildkitVersion.Version)
		client.Close()

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer stopCancel()
		r.NoError(second.Stop(stopCtx))
		r.False(second.IsRunning())
	})

	t.Run("exit monitor clears running when the daemon dies", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		cc := newClient(t)
		config := buildkit.Config{SocketDir: t.TempDir(), RegistryHost: "cluster.local:5000"}

		component := buildkit.NewComponent(logger, cc, testNamespace, t.TempDir())
		r.NoError(component.Start(ctx, config))
		r.True(component.IsRunning())

		// Kill buildkitd out from under the component.
		nsctx := namespaces.WithNamespace(ctx, testNamespace)
		container, err := cc.LoadContainer(nsctx, "miren-buildkit")
		r.NoError(err)
		task, err := container.Task(nsctx, nil)
		r.NoError(err)
		r.NoError(task.Kill(nsctx, unix.SIGKILL))

		// The monitor goroutine should observe the exit and flip IsRunning.
		require.Eventually(t, func() bool { return !component.IsRunning() },
			30*time.Second, 200*time.Millisecond,
			"exit monitor should mark the daemon not running after it dies")

		// Even though the daemon already died (IsRunning is false), Stop must
		// still tear down the leftover container and its dead task rather than
		// no-op away and leak them.
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer stopCancel()
		r.NoError(component.Stop(stopCtx))

		_, err = cc.LoadContainer(nsctx, "miren-buildkit")
		r.Error(err, "Stop should delete the container even after an unexpected death")
	})

	// A container reused across restarts keeps the env it was created with, so
	// a changed trace destination (or a credential the operator removed) has to
	// force a new container, while unchanged settings keep reusing it.
	t.Run("restart recreates the container when trace export changes", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		cc := newClient(t)
		dataDir := t.TempDir()
		config := buildkit.Config{
			SocketDir:    t.TempDir(),
			RegistryHost: "cluster.local:5000",
			Traces:       buildkit.TracesExport{Endpoint: "https://collector.example"},
		}

		first := buildkit.NewComponent(logger, cc, testNamespace, dataDir)
		r.NoError(first.Start(ctx, config))
		createdFirst, env := buildkitContainerInfo(ctx, t, cc)
		r.Contains(env, "OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example")

		same := buildkit.NewComponent(logger, cc, testNamespace, dataDir)
		r.NoError(same.Start(ctx, config))
		createdSame, _ := buildkitContainerInfo(ctx, t, cc)
		r.Equal(createdFirst, createdSame, "unchanged trace settings must reuse the container")

		config.Traces = buildkit.TracesExport{Endpoint: "http://127.0.0.1:14318", Relayed: true}
		relayed := buildkit.NewComponent(logger, cc, testNamespace, dataDir)
		r.NoError(relayed.Start(ctx, config))
		r.True(relayed.IsRunning())
		createdRelayed, env := buildkitContainerInfo(ctx, t, cc)
		r.NotEqual(createdFirst, createdRelayed, "changed trace settings must recreate the container")
		r.Contains(env, "OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:14318")
		r.NotContains(env, "OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example")

		client, err := relayed.Client(ctx)
		r.NoError(err)
		_, err = client.Info(ctx)
		r.NoError(err)
		client.Close()

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer stopCancel()
		r.NoError(relayed.Stop(stopCtx))
	})

	t.Run("Stop removes the task and container", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		cc := newClient(t)
		config := buildkit.Config{SocketDir: t.TempDir(), RegistryHost: "cluster.local:5000"}

		component := buildkit.NewComponent(logger, cc, testNamespace, t.TempDir())
		r.NoError(component.Start(ctx, config))
		r.True(component.IsRunning())

		stopCtx, stopCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer stopCancel()
		r.NoError(component.Stop(stopCtx))
		r.False(component.IsRunning())

		// Stop deletes the container outright, so it should no longer load.
		nsctx := namespaces.WithNamespace(ctx, testNamespace)
		_, err := cc.LoadContainer(nsctx, "miren-buildkit")
		r.Error(err)
	})
}
