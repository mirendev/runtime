package ocireg

import (
	"io"
	"log/slog"
	"net"
	"testing"

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
