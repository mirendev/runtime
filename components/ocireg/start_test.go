package ocireg

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegistryStartReturnsBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	registry := NewRegistry(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	err = registry.Start(t.Context(), listener.Addr().String())
	require.ErrorContains(t, err, "listen")
}

func TestRegistryRequestContextSurvivesLifetimeCancellation(t *testing.T) {
	type contextKey struct{}
	lifetime, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "request-value"))
	registry := NewRegistry(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	require.NoError(t, registry.Start(lifetime, "127.0.0.1:0"))
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		require.NoError(t, registry.Shutdown(shutdownCtx))
	})

	requestContext := registry.server.BaseContext(nil)
	cancel()
	require.NoError(t, requestContext.Err())
	require.Equal(t, "request-value", requestContext.Value(contextKey{}))
}
