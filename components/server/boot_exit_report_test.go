//go:build linux

package server

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/pkg/exitrecord"
)

// capturingHandler collects records so a test can assert on level and fields
// rather than on formatted text.
type capturingHandler struct {
	records []slog.Record
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler            { return h }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *capturingHandler) attrs(i int) map[string]slog.Value {
	out := map[string]slog.Value{}
	h.records[i].Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value
		return true
	})
	return out
}

func newCapturingLogger() (*slog.Logger, *capturingHandler) {
	h := &capturingHandler{}
	return slog.New(h), h
}

func TestReportPreviousExitOOMKill(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 9, 11, 17, 4, 11, 0, time.UTC)
	require.NoError(t, exitrecord.Write(dir, exitrecord.Record{
		At:         at,
		Unit:       "miren.service",
		Result:     exitrecord.ResultOOMKill,
		ExitCode:   "killed",
		ExitStatus: "9",
		MemoryPeak: 4294967296,
		MemoryMax:  4294967296,
		Restarts:   3,
	}))

	log, h := newCapturingLogger()
	reportPreviousExit(dir, log)

	require.Len(t, h.records, 1)

	// Warn, not Error: the platform contained the runaway and restarted. An
	// Error here would page someone about a system that worked.
	assert.Equal(t, slog.LevelWarn, h.records[0].Level)
	assert.Contains(t, h.records[0].Message, "memory limit")

	attrs := h.attrs(0)
	assert.Equal(t, int64(4294967296), attrs["memory_peak"].Int64())
	assert.Equal(t, int64(4294967296), attrs["memory_max"].Int64())
	assert.Equal(t, int64(3), attrs["restarts"].Int64())
	assert.Contains(t, attrs["hint"].String(), "systemctl edit miren")
}

func TestReportPreviousExitAbnormal(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, exitrecord.Write(dir, exitrecord.Record{
		At:         time.Now().UTC(),
		Unit:       "miren.service",
		Result:     "exit-code",
		ExitCode:   "exited",
		ExitStatus: "1",
	}))

	log, h := newCapturingLogger()
	reportPreviousExit(dir, log)

	require.Len(t, h.records, 1)
	assert.Equal(t, slog.LevelWarn, h.records[0].Level)
	assert.Contains(t, h.records[0].Message, "exited abnormally")
	assert.NotContains(t, h.records[0].Message, "memory limit")

	attrs := h.attrs(0)
	assert.Equal(t, "exit-code", attrs["result"].String())
	assert.Equal(t, "1", attrs["exit_status"].String())
}

func TestReportPreviousExitStaysQuietWhenNothingWentWrong(t *testing.T) {
	log, h := newCapturingLogger()

	reportPreviousExit(t.TempDir(), log)

	assert.Empty(t, h.records, "a clean previous run must not produce a warning")
}

func TestReportPreviousExitFiresOncePerOccurrence(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, exitrecord.Write(dir, exitrecord.Record{
		Unit:   "miren.service",
		Result: exitrecord.ResultOOMKill,
	}))

	log, h := newCapturingLogger()
	reportPreviousExit(dir, log)
	reportPreviousExit(dir, log)

	assert.Len(t, h.records, 1, "a restart loop must not re-warn about the same exit on every boot")

	// The detail is kept for later diagnosis, just not re-reported.
	_, err := os.Stat(filepath.Join(dir, exitrecord.ReportedFileName))
	assert.NoError(t, err)
}

func TestReportPreviousExitSurvivesCorruptRecord(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, exitrecord.FileName), []byte("{not json"), 0o644))

	log, h := newCapturingLogger()

	// A server that can't read its own crash record has a reporting gap, not a
	// reason to stay down.
	assert.NotPanics(t, func() { reportPreviousExit(dir, log) })
	require.Len(t, h.records, 1)
	assert.Equal(t, slog.LevelWarn, h.records[0].Level)
}
