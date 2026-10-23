package buildkit

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClientWaitsForStartInProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewExternalComponent(slog.New(slog.NewTextHandler(io.Discard, nil)), "/nonexistent/buildkitd.sock")

		// The socket never appears, so Start polls until its deadline.
		startCtx, cancelStart := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancelStart()
		startErr := make(chan error, 1)
		go func() { startErr <- c.Start(startCtx, Config{}) }()
		synctest.Wait()

		begun := time.Now()
		_, err := c.Client(t.Context())
		// A Start that fails still releases waiting clients, which then see
		// the daemon is not running rather than hanging until their deadline.
		require.ErrorContains(t, err, "not running")
		require.Error(t, <-startErr)
		require.GreaterOrEqual(t, time.Since(begun), 10*time.Second, "Client returned before Start finished")
	})
}

func TestClientGivesUpWhenItsContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewExternalComponent(slog.New(slog.NewTextHandler(io.Discard, nil)), "/nonexistent/buildkitd.sock")

		startCtx, cancelStart := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancelStart()
		go func() { _ = c.Start(startCtx, Config{}) }()
		synctest.Wait()

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		_, err := c.Client(ctx)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})
}
