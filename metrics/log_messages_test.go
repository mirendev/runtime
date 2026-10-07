package metrics

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miren.dev/runtime/pkg/logcount"
)

func TestLogMessages_Emit(t *testing.T) {
	counts := &logcount.Counts{}
	log := slog.New(logcount.NewHandler(slog.NewTextHandler(io.Discard, nil), counts))
	log.Error("one")
	log.Error("two")
	log.Log(logcount.Relayed(context.Background(), logcount.SourceVMAgent, slog.LevelInfo),
		slog.LevelInfo, "starting vmagent")

	sink := &recordingSink{}
	lm := NewLogMessages(testLogger(), sink)
	lm.Counts = counts
	lm.Entity = EntityRunner

	require.NoError(t, lm.Emit(context.Background(), time.Now()))
	require.Len(t, sink.batches, 1)

	got := map[[2]string]float64{}
	for _, p := range sink.batches[0] {
		assert.Equal(t, "miren_log_messages_total", p.Name)
		assert.Equal(t, EntityRunner, p.Labels["entity"])
		got[[2]string{p.Labels["source"], p.Labels["level"]}] = p.Value
	}

	assert.Equal(t, map[[2]string]float64{
		{"miren", "debug"}:   0,
		{"miren", "info"}:    0,
		{"miren", "warn"}:    0,
		{"miren", "error"}:   2,
		{"vmagent", "debug"}: 0,
		{"vmagent", "info"}:  1,
		{"vmagent", "warn"}:  0,
		{"vmagent", "error"}: 0,
	}, got, "miren always; a relay once it has printed anything, with zeros for its quiet levels")
}

func TestLogMessages_DefaultsDescribeThisProcess(t *testing.T) {
	lm := NewLogMessages(testLogger(), nil)

	assert.Equal(t, EntityControl, lm.Entity)
	assert.Same(t, logcount.Default, lm.Counts)
}
