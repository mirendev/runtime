//go:build linux

package buildkit_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/buildkit"
)

func TestExternalComponentRejectsSocketWithoutBuildkitAPI(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "buildkitd.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	component := buildkit.NewExternalComponent(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		socketPath,
	)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.Error(t, component.Start(ctx, buildkit.Config{}))
}
