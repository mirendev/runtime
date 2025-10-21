package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/moby/buildkit/identity"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/testutils"
)

func TestLogWriter(t *testing.T) {
	t.Run("can write and read single log", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var (
			lm observability.LogsMaintainer
			pw observability.PersistentLogWriter
			lr observability.LogReader
		)

		r.NoError(reg.Populate(&lm))
		r.NoError(reg.Populate(&pw))
		r.NoError(reg.Populate(&lr))
		r.NoError(lm.Setup(ctx))

		id := identity.NewID()

		err := pw.WriteEntry(id, observability.LogEntry{
			Timestamp: time.Now(),
			Stream:    observability.Stdout,
			Body:      "this is a log line",
		})
		r.NoError(err)

		// Give VictoriaLogs time to index the log
		time.Sleep(2 * time.Second)

		entries, err := lr.Read(ctx, id)
		if err != nil {
			t.Logf("Read error: %v", err)
		}
		r.NoError(err)

		r.Len(entries, 1)
		r.Equal("this is a log line", entries[0].Body)
		r.Equal(observability.Stdout, entries[0].Stream)
	})

	t.Run("can write logs with different streams", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var pw observability.PersistentLogWriter
		var lr observability.LogReader

		r.NoError(reg.Populate(&pw))
		r.NoError(reg.Populate(&lr))

		id := identity.NewID()

		streams := []observability.LogStream{
			observability.Stdout,
			observability.Stderr,
			observability.Error,
			observability.UserOOB,
		}

		for i, stream := range streams {
			err := pw.WriteEntry(id, observability.LogEntry{
				Timestamp: time.Now(),
				Stream:    stream,
				Body:      "log from stream " + string(stream),
			})
			r.NoError(err, "failed to write log %d", i)
		}

		time.Sleep(2 * time.Second)

		entries, err := lr.Read(ctx, id, observability.WithLimit(10))
		r.NoError(err)

		r.Len(entries, 4, "should have 4 log entries")

		// Verify all streams are present
		foundStreams := make(map[observability.LogStream]bool)
		for _, entry := range entries {
			foundStreams[entry.Stream] = true
		}

		for _, stream := range streams {
			r.True(foundStreams[stream], "stream %s should be present", stream)
		}
	})

	t.Run("can write logs with attributes", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var pw observability.PersistentLogWriter
		var lr observability.LogReader

		r.NoError(reg.Populate(&pw))
		r.NoError(reg.Populate(&lr))

		id := identity.NewID()

		err := pw.WriteEntry(id, observability.LogEntry{
			Timestamp: time.Now(),
			Stream:    observability.Stdout,
			Body:      "log with attributes",
			Attributes: map[string]string{
				"sandbox":   "test-sandbox-123",
				"container": "test-container",
				"version":   "v1.0.0",
			},
		})
		r.NoError(err)

		time.Sleep(2 * time.Second)

		entries, err := lr.Read(ctx, id)
		r.NoError(err)

		r.Len(entries, 1)
		r.Equal("log with attributes", entries[0].Body)
		r.NotNil(entries[0].Attributes)
		r.Equal("test-sandbox-123", entries[0].Attributes["sandbox"])
		r.Equal("test-container", entries[0].Attributes["container"])
		r.Equal("v1.0.0", entries[0].Attributes["version"])
	})

	t.Run("can write logs with trace ID", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var pw observability.PersistentLogWriter
		var lr observability.LogReader

		r.NoError(reg.Populate(&pw))
		r.NoError(reg.Populate(&lr))

		id := identity.NewID()
		traceID := identity.NewID()

		err := pw.WriteEntry(id, observability.LogEntry{
			Timestamp: time.Now(),
			Stream:    observability.Stdout,
			Body:      "log with trace",
			TraceID:   traceID,
		})
		r.NoError(err)

		time.Sleep(2 * time.Second)

		entries, err := lr.Read(ctx, id)
		r.NoError(err)

		r.Len(entries, 1)
		r.Equal(traceID, entries[0].TraceID)
	})

	t.Run("can write multiple logs and read them in order", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var pw observability.PersistentLogWriter
		var lr observability.LogReader

		r.NoError(reg.Populate(&pw))
		r.NoError(reg.Populate(&lr))

		id := identity.NewID()

		// Write logs with timestamps in order
		baseTime := time.Now()
		for i := 0; i < 5; i++ {
			err := pw.WriteEntry(id, observability.LogEntry{
				Timestamp: baseTime.Add(time.Duration(i) * time.Millisecond),
				Stream:    observability.Stdout,
				Body:      "log line " + string('A'+rune(i)),
			})
			r.NoError(err)
		}

		time.Sleep(2 * time.Second)

		entries, err := lr.Read(ctx, id, observability.WithLimit(10))
		r.NoError(err)

		r.Len(entries, 5)

		// Verify order
		for i := 0; i < 5; i++ {
			expectedBody := "log line " + string('A'+rune(i))
			r.Equal(expectedBody, entries[i].Body, "log %d should be in order", i)
		}
	})
}

func TestLogReader(t *testing.T) {
	t.Run("can read logs with time filter", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var pw observability.PersistentLogWriter
		var lr observability.LogReader

		r.NoError(reg.Populate(&pw))
		r.NoError(reg.Populate(&lr))

		id := identity.NewID()

		baseTime := time.Now()
		// Write 5 logs with 100ms intervals
		for i := 0; i < 5; i++ {
			err := pw.WriteEntry(id, observability.LogEntry{
				Timestamp: baseTime.Add(time.Duration(i) * 100 * time.Millisecond),
				Stream:    observability.Stdout,
				Body:      "log " + string('0'+rune(i)),
			})
			r.NoError(err)
		}

		// Cutoff at 250ms (should get logs 3 and 4)
		cutoffTime := baseTime.Add(250 * time.Millisecond)

		time.Sleep(2 * time.Second)

		// Read logs from cutoff time (should get logs 3 and 4)
		entries, err := lr.Read(ctx, id, observability.WithFromTime(cutoffTime))
		r.NoError(err)

		r.GreaterOrEqual(len(entries), 2, "should have at least 2 entries after cutoff")

		// Check that all returned entries are after cutoff
		for _, entry := range entries {
			r.True(entry.Timestamp.After(cutoffTime) || entry.Timestamp.Equal(cutoffTime),
				"entry timestamp should be >= cutoff time")
		}
	})

	t.Run("can read logs with limit", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var pw observability.PersistentLogWriter
		var lr observability.LogReader

		r.NoError(reg.Populate(&pw))
		r.NoError(reg.Populate(&lr))

		id := identity.NewID()

		// Write 10 logs
		for i := 0; i < 10; i++ {
			err := pw.WriteEntry(id, observability.LogEntry{
				Timestamp: time.Now(),
				Stream:    observability.Stdout,
				Body:      "log " + string('0'+rune(i)),
			})
			r.NoError(err)
		}

		time.Sleep(2 * time.Second)

		// Read with limit of 3
		entries, err := lr.Read(ctx, id, observability.WithLimit(3))
		r.NoError(err)

		r.LessOrEqual(len(entries), 3, "should return at most 3 entries")
	})

	t.Run("can read logs by sandbox", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var pw observability.PersistentLogWriter
		var lr observability.LogReader

		r.NoError(reg.Populate(&pw))
		r.NoError(reg.Populate(&lr))

		entityID := identity.NewID()
		sandboxID := identity.NewID()

		// Write logs with sandbox attribute
		for i := 0; i < 3; i++ {
			err := pw.WriteEntry(entityID, observability.LogEntry{
				Timestamp: time.Now(),
				Stream:    observability.Stdout,
				Body:      "sandbox log " + string('0'+rune(i)),
				Attributes: map[string]string{
					"sandbox": sandboxID,
				},
			})
			r.NoError(err)
		}

		time.Sleep(2 * time.Second)

		// Read by sandbox
		entries, err := lr.ReadBySandbox(ctx, sandboxID)
		r.NoError(err)

		r.Len(entries, 3)
		for i, entry := range entries {
			r.Equal("sandbox log "+string('0'+rune(i)), entry.Body)
			r.Equal(sandboxID, entry.Attributes["sandbox"])
		}
	})

	t.Run("returns empty list for non-existent entity", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var lr observability.LogReader
		r.NoError(reg.Populate(&lr))

		nonExistentID := identity.NewID()

		entries, err := lr.Read(ctx, nonExistentID)
		r.NoError(err)
		r.Empty(entries)
	})
}

func TestDebugLogWriter(t *testing.T) {
	t.Run("writes to logger without error", func(t *testing.T) {
		r := require.New(t)

		reg, cleanup := testutils.Registry(observability.TestInject)
		defer cleanup()

		var debugWriter observability.DebugLogWriter
		r.NoError(reg.Populate(&debugWriter))

		err := debugWriter.WriteEntry("test-entity", observability.LogEntry{
			Timestamp: time.Now(),
			Stream:    observability.Stdout,
			Body:      "debug log",
		})
		r.NoError(err)
	})
}
