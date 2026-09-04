package ctrbuild

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/lbdmod"
)

func TestTailWriterLogsLinesAndKeepsTheTail(t *testing.T) {
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	w := newTailWriter(log, "build", 3)

	// Split across writes, as a pipe delivers it.
	_, err := w.Write([]byte("first\nsec"))
	require.NoError(t, err)
	_, err = w.Write([]byte("ond\nthird\nfourth\n"))
	require.NoError(t, err)

	assert.Equal(t, "second\nthird\nfourth", w.Tail(), "only the last few lines are kept")
	assert.Contains(t, logged.String(), "first", "every line still reaches the log")
	assert.Contains(t, logged.String(), "fourth")
}

func TestTailWriterKeepsBuildOutputOffInfo(t *testing.T) {
	// The automatic rebuild after a kernel upgrade runs unattended, so a whole
	// compile at Info would be noise in the daemon log. The tail is kept
	// regardless of level, which is what a failure reports.
	var atInfo bytes.Buffer
	log := slog.New(slog.NewTextHandler(&atInfo, &slog.HandlerOptions{Level: slog.LevelInfo}))
	w := newTailWriter(log, "build", 10)

	_, err := w.Write([]byte("  CC [M]  /src/lbd_main.o\n"))
	require.NoError(t, err)

	assert.Empty(t, atInfo.String(), "compile output must not reach an Info-level log")
	assert.Contains(t, w.Tail(), "lbd_main.o", "the tail is still captured for failure reporting")
}

func TestTailWriterFlushesAnUnterminatedLine(t *testing.T) {
	w := newTailWriter(slog.New(slog.DiscardHandler), "build", 10)
	_, err := w.Write([]byte("no trailing newline"))
	require.NoError(t, err)

	assert.Empty(t, w.Tail(), "an unterminated line is not a line yet")
	w.flush()
	assert.Equal(t, "no trailing newline", w.Tail())
}

func TestTailWriterIgnoresBlankLines(t *testing.T) {
	w := newTailWriter(slog.New(slog.DiscardHandler), "build", 10)
	_, err := w.Write([]byte("\n  \nreal\n\n"))
	require.NoError(t, err)
	assert.Equal(t, "real", w.Tail())
}

func TestOCIMountsCarryReadOnlyThrough(t *testing.T) {
	mounts := ociMounts([]lbdmod.Mount{
		{Source: "/build/src", Destination: "/src"},
		{Source: "/lib/modules", Destination: "/lib/modules", ReadOnly: true},
	})

	require.Len(t, mounts, 2)

	assert.Equal(t, "/src", mounts[0].Destination)
	assert.Equal(t, "/build/src", mounts[0].Source)
	assert.Equal(t, "bind", mounts[0].Type)
	assert.Equal(t, []string{"rbind", "rw"}, mounts[0].Options)

	// The host's module tree must never be writable from the builder.
	assert.Equal(t, []string{"rbind", "ro"}, mounts[1].Options)
}
