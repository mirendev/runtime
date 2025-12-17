//go:build linux

package buildkit_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/pkg/containerdx"
)

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
