package ocireg

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
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

func TestRegistryOnlyListensOnChosenInterface(t *testing.T) {
	reservation, err := net.Listen("tcp", "127.0.0.2:0")
	require.NoError(t, err)
	addr := reservation.Addr().(*net.TCPAddr)
	require.NoError(t, reservation.Close())

	issuer := registryTestIssuer(t)
	registry := NewRegistry(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, issuer)
	require.NoError(t, registry.Start(t.Context(), addr.String()))
	t.Cleanup(func() { require.NoError(t, registry.Shutdown(context.Background())) })

	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Get("http://" + addr.String() + "/v2/")
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	require.NoError(t, response.Body.Close())

	other := net.JoinHostPort("127.0.0.1", fmt.Sprint(addr.Port))
	conn, err := net.DialTimeout("tcp", other, time.Second)
	if err == nil {
		conn.Close()
	}
	require.Error(t, err, "registry unexpectedly reachable on a different interface")
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
