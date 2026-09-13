package exitrecord

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()

	want := Record{
		At:         time.Date(2026, 9, 11, 17, 4, 11, 0, time.UTC),
		Unit:       "miren.service",
		Result:     ResultOOMKill,
		ExitCode:   "killed",
		ExitStatus: "9",
		MemoryPeak: 4294967296,
		MemoryMax:  4294967296,
		Restarts:   3,
	}

	require.NoError(t, Write(dir, want))

	got, found, err := Read(dir)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, want, got)
	assert.True(t, got.OOMKilled())
}

func TestWriteCreatesMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "server")

	require.NoError(t, Write(dir, Record{Unit: "miren.service", Result: ResultOOMKill}))

	_, found, err := Read(dir)
	require.NoError(t, err)
	assert.True(t, found)
}

func TestWriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, Write(dir, Record{Unit: "miren.service", Result: ResultOOMKill}))
	require.NoError(t, Write(dir, Record{Unit: "miren.service", Result: "exit-code"}))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, FileName, entries[0].Name())

	got, _, err := Read(dir)
	require.NoError(t, err)
	assert.Equal(t, "exit-code", got.Result, "the second write should replace the first")
}

func TestReadWithNoRecord(t *testing.T) {
	// The ordinary case: nothing went wrong last time.
	_, found, err := Read(t.TempDir())
	assert.NoError(t, err)
	assert.False(t, found)
}

func TestReadRejectsCorruptRecord(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o644))

	_, found, err := Read(dir)
	assert.Error(t, err)
	assert.False(t, found)
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Write(dir, Record{Unit: "miren.service", Result: ResultOOMKill}))

	require.NoError(t, Clear(dir))

	_, found, err := Read(dir)
	require.NoError(t, err)
	assert.False(t, found)

	assert.NoError(t, Clear(dir), "clearing an already-clean directory is not an error")
}

func TestMarkReported(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Write(dir, Record{Unit: "miren.service", Result: ResultOOMKill}))

	require.NoError(t, MarkReported(dir))

	// Gone as far as Read is concerned, so the warning fires once per
	// occurrence rather than on every boot.
	_, found, err := Read(dir)
	require.NoError(t, err)
	assert.False(t, found)

	// But kept on disk for later diagnosis.
	_, err = os.Stat(filepath.Join(dir, ReportedFileName))
	assert.NoError(t, err)

	assert.NoError(t, MarkReported(dir), "marking an absent record is not an error")
}
