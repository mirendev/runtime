package slogfmt

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openStream(t *testing.T, name string) (*os.File, string) {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), name))
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })

	fi, err := f.Stat()
	require.NoError(t, err)
	st := fi.Sys().(*syscall.Stat_t)
	return f, fmt.Sprintf("%d:%d", st.Dev, st.Ino)
}

func TestOnJournalStream(t *testing.T) {
	stream, id := openStream(t, "stream")
	other, _ := openStream(t, "other")

	t.Run("matching stream", func(t *testing.T) {
		t.Setenv("JOURNAL_STREAM", id)
		assert.True(t, OnJournalStream(stream))
	})

	t.Run("inherited by a redirected stream", func(t *testing.T) {
		t.Setenv("JOURNAL_STREAM", id)
		assert.False(t, OnJournalStream(other))
	})

	t.Run("unset", func(t *testing.T) {
		t.Setenv("JOURNAL_STREAM", "")
		assert.False(t, OnJournalStream(stream))
	})
}
