package slogfmt

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func journalLines(t *testing.T, buf *bytes.Buffer) []string {
	t.Helper()
	out := buf.String()
	require.True(t, strings.HasSuffix(out, "\n"), "output should end in a newline: %q", out)
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

func TestJournalPriorityPrefixesByLevel(t *testing.T) {
	var buf bytes.Buffer
	h := NewTextHandler(&buf, &slog.HandlerOptions{Level: Trace}).WithJournalPriority(nil)
	log := slog.New(h)

	log.Log(context.Background(), Trace, "t")
	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")
	log.Log(context.Background(), slog.LevelError+4, "past error")

	lines := journalLines(t, &buf)
	require.Len(t, lines, 6)
	for i, want := range []string{"<7>", "<7>", "<6>", "<4>", "<3>", "<3>"} {
		assert.True(t, strings.HasPrefix(lines[i], want), "line %d should start with %s: %q", i, want, lines[i])
	}
}

func TestJournalPriorityPrefixesContinuationLines(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewTextHandler(&buf, nil).WithJournalPriority(nil))

	log.Error("failed", "trace", "line one\nline two\nline three")

	lines := journalLines(t, &buf)
	require.Greater(t, len(lines), 1, "a multi-line attr should span several lines")
	for i, line := range lines {
		assert.True(t, strings.HasPrefix(line, "<3>"), "line %d should start with <3>: %q", i, line)
		assert.NotEqual(t, "<3>", line, "line %d would be an empty err entry", i)
	}
}

func TestAppendJournalLinesDropsEmptyLines(t *testing.T) {
	got := appendJournalLines(nil, []byte("a\n\n  b\n\n"), '4')
	assert.Equal(t, "<4>a\n<4>  b\n", string(got))

	got = appendJournalLines(nil, []byte("no newline"), '6')
	assert.Equal(t, "<6>no newline", string(got))
}

func TestJournalPriorityUsesLevelFunc(t *testing.T) {
	var buf bytes.Buffer
	asInfo := func(context.Context, slog.Level) slog.Level { return slog.LevelInfo }
	log := slog.New(NewTextHandler(&buf, nil).WithJournalPriority(asInfo))

	log.Error("loud but routine")

	out := buf.String()
	assert.True(t, strings.HasPrefix(out, "<6>"), "priority should come from the level func: %q", out)
	assert.Contains(t, out, "[ERROR]", "the printed level should not change")
}

func TestJournalPrioritySurvivesWithAttrsAndGroups(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewTextHandler(&buf, nil).WithJournalPriority(nil))

	log.With("module", "coordinator").WithGroup("g").With("k", "v").Warn("w")

	assert.True(t, strings.HasPrefix(buf.String(), "<4>"), "got %q", buf.String())
}

func TestNoJournalPriorityByDefault(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewTextHandler(&buf, nil))

	log.Error("e", "trace", "a\nb")

	assert.NotContains(t, buf.String(), "<3>")
}
