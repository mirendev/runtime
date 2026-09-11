//go:build linux

package commands

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/exitrecord"
)

type earlyCapture struct{ records []slog.Record }

func (h *earlyCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *earlyCapture) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *earlyCapture) WithGroup(string) slog.Handler            { return h }
func (h *earlyCapture) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func newEarlyLogger() (*slog.Logger, *earlyCapture) {
	h := &earlyCapture{}
	return slog.New(h), h
}

func TestReportPreviousExitEarly(t *testing.T) {
	dataPath := t.TempDir()
	dir := filepath.Join(dataPath, "server")
	require.NoError(t, exitrecord.Write(dir, exitrecord.Record{
		Unit:       "miren.service",
		Result:     exitrecord.ResultOOMKill,
		MemoryPeak: 4294967296,
		MemoryMax:  4294967296,
		Restarts:   7,
	}))

	log, h := newEarlyLogger()
	reportPreviousExitEarly(dataPath, log)

	require.Len(t, h.records, 1)
	assert.Equal(t, slog.LevelWarn, h.records[0].Level)
	assert.Contains(t, h.records[0].Message, "memory limit")
}

func TestReportPreviousExitEarlyKeepsTheRecordUnreported(t *testing.T) {
	dataPath := t.TempDir()
	dir := filepath.Join(dataPath, "server")
	require.NoError(t, exitrecord.Write(dir, exitrecord.Record{
		Unit:   "miren.service",
		Result: exitrecord.ResultOOMKill,
	}))

	log, _ := newEarlyLogger()
	reportPreviousExitEarly(dataPath, log)

	// The whole point: this path runs on a boot that may never finish, so it
	// must not retire the record. The boot component does that once the warning
	// has actually reached durable storage.
	_, found, err := exitrecord.Read(dir)
	require.NoError(t, err)
	assert.True(t, found, "the early report must leave the record for the durable one")

	_, err = os.Stat(filepath.Join(dir, exitrecord.ReportedFileName))
	assert.True(t, os.IsNotExist(err))
}

func TestReportPreviousExitEarlyWarnsOnEveryCrashloopBoot(t *testing.T) {
	dataPath := t.TempDir()
	dir := filepath.Join(dataPath, "server")

	log, h := newEarlyLogger()

	// Three boots that each die before observability comes up. Every one has to
	// warn: this is the only output a crashlooping server produces, and the
	// record is overwritten each time so there is nothing to catch up on later.
	for i := 0; i < 3; i++ {
		require.NoError(t, exitrecord.Write(dir, exitrecord.Record{
			Unit:     "miren.service",
			Result:   exitrecord.ResultOOMKill,
			Restarts: i,
		}))
		reportPreviousExitEarly(dataPath, log)
	}

	assert.Len(t, h.records, 3)
}

func TestReportPreviousExitEarlyStaysQuietWhenNothingWentWrong(t *testing.T) {
	log, h := newEarlyLogger()

	reportPreviousExitEarly(t.TempDir(), log)

	assert.Empty(t, h.records, "a clean previous run must not produce a warning")
}

func TestReportPreviousExitEarlySurvivesCorruptRecord(t *testing.T) {
	dataPath := t.TempDir()
	dir := filepath.Join(dataPath, "server")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, exitrecord.FileName), []byte("{not json"), 0o644))

	log, h := newEarlyLogger()

	// This runs before the server has started anything. It must never be a
	// reason the server fails to boot.
	assert.NotPanics(t, func() { reportPreviousExitEarly(dataPath, log) })
	require.Len(t, h.records, 1)
	assert.Equal(t, slog.LevelWarn, h.records[0].Level)
}
