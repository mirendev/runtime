//go:build linux

package buildkit_test

import (
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/buildkit"
)

func TestExternalComponentVerifiesSocketOnStart(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "buildkitd.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	component := buildkit.NewExternalComponent(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		socketPath,
	)
	require.NoError(t, component.Start(t.Context(), buildkit.Config{}))
	require.ErrorContains(t, component.Start(t.Context(), buildkit.Config{}), "already running")
}
