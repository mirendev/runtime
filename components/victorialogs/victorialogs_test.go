//go:build linux

package victorialogs_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/stretchr/testify/require"
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
		r.Equal("localhost:9428", component.HTTPEndpoint())

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

		r.Equal("localhost:9430", component.HTTPEndpoint())
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
