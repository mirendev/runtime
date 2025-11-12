//go:build linux

package observability_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/moby/buildkit/identity"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/victorialogs"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/containerdx"
)

func TestVictoriaLogsIntegration(t *testing.T) {
	if os.Getenv("SKIP_INTEGRATION_TEST") != "" {
		t.Skip("Skipping integration test")
	}

	t.Run("end-to-end write and read", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		// Start Victoria Logs component
		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))

		tmpDir := t.TempDir()

		vlComponent := victorialogs.NewVictoriaLogsComponent(logger, cc, "miren-test", tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        9432,
			RetentionPeriod: "1d",
		}

		err = vlComponent.Start(ctx, config)
		r.NoError(err)
		defer vlComponent.Stop(context.Background())

		// Give VictoriaLogs time to fully start
		time.Sleep(3 * time.Second)

		// Create writer and reader
		address := vlComponent.HTTPEndpoint()

		writer := &observability.PersistentLogWriter{
			Address: address,
			Timeout: 30 * time.Second,
		}
		writer.Populated()

		reader := &observability.LogReader{
			Address: address,
			Timeout: 30 * time.Second,
		}
		reader.Populated()

		// Write some logs
		entityID := identity.NewID()
		sandboxID := identity.NewID()

		testLogs := []struct {
			body       string
			stream     observability.LogStream
			attributes map[string]string
		}{
			{
				body:   "Application started",
				stream: observability.Stdout,
				attributes: map[string]string{
					"sandbox": sandboxID,
					"phase":   "startup",
				},
			},
			{
				body:   "Warning: deprecated API used",
				stream: observability.Stderr,
				attributes: map[string]string{
					"sandbox": sandboxID,
					"phase":   "runtime",
				},
			},
			{
				body:   "User action: button clicked",
				stream: observability.UserOOB,
				attributes: map[string]string{
					"sandbox": sandboxID,
					"user_id": "test-user",
				},
			},
		}

		for i, tl := range testLogs {
			err := writer.WriteEntry(entityID, observability.LogEntry{
				Timestamp:  time.Now(),
				Stream:     tl.stream,
				Body:       tl.body,
				Attributes: tl.attributes,
			})
			r.NoError(err, "failed to write log %d", i)
		}

		// Give VictoriaLogs time to index
		time.Sleep(3 * time.Second)

		// Read logs by entity
		entries, err := reader.Read(ctx, entityID, observability.WithLimit(10))
		r.NoError(err)

		r.Len(entries, 3, "should have 3 log entries")

		// Verify logs
		foundBodies := make(map[string]bool)
		for _, entry := range entries {
			foundBodies[entry.Body] = true
		}

		for _, tl := range testLogs {
			r.True(foundBodies[tl.body], "should find log: %s", tl.body)
		}

		// Read logs by sandbox
		sandboxEntries, err := reader.ReadBySandbox(ctx, sandboxID, observability.WithLimit(10))
		r.NoError(err)

		r.Len(sandboxEntries, 3, "should have 3 logs for sandbox")

		for _, entry := range sandboxEntries {
			r.Equal(sandboxID, entry.Attributes["sandbox"], "should have correct sandbox ID")
		}
	})

	t.Run("can filter logs by time", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		// Start Victoria Logs component
		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		tmpDir := t.TempDir()

		vlComponent := victorialogs.NewVictoriaLogsComponent(logger, cc, "miren-test", tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        9433,
			RetentionPeriod: "1d",
		}

		err = vlComponent.Start(ctx, config)
		r.NoError(err)
		defer vlComponent.Stop(context.Background())

		time.Sleep(3 * time.Second)

		address := vlComponent.HTTPEndpoint()

		writer := &observability.PersistentLogWriter{
			Address: address,
			Timeout: 30 * time.Second,
		}
		writer.Populated()

		reader := &observability.LogReader{
			Address: address,
			Timeout: 30 * time.Second,
		}
		reader.Populated()

		entityID := identity.NewID()

		// Write logs with specific timestamps
		baseTime := time.Now()
		for i := 0; i < 5; i++ {
			err := writer.WriteEntry(entityID, observability.LogEntry{
				Timestamp: baseTime.Add(time.Duration(i) * time.Second),
				Stream:    observability.Stdout,
				Body:      "log " + string('0'+rune(i)),
			})
			r.NoError(err)
		}

		time.Sleep(3 * time.Second)

		// Read logs from middle timestamp (slightly before second 2 to account for precision)
		cutoffTime := baseTime.Add(2*time.Second - 100*time.Millisecond)
		entries, err := reader.Read(ctx, entityID,
			observability.WithFromTime(cutoffTime),
			observability.WithLimit(10))
		r.NoError(err)

		r.GreaterOrEqual(len(entries), 2, "should have at least 2 entries after cutoff")

		for _, entry := range entries {
			r.True(entry.Timestamp.After(cutoffTime) || entry.Timestamp.Equal(cutoffTime),
				"all entries should be after cutoff time")
		}
	})

	t.Run("can handle high volume writes", func(t *testing.T) {
		r := require.New(t)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		// Start Victoria Logs component
		cc, err := containerd.New(containerdx.DefaultSocket)
		r.NoError(err)
		defer cc.Close()

		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		tmpDir := t.TempDir()

		vlComponent := victorialogs.NewVictoriaLogsComponent(logger, cc, "miren-test", tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        9434,
			RetentionPeriod: "1d",
		}

		err = vlComponent.Start(ctx, config)
		r.NoError(err)
		defer vlComponent.Stop(context.Background())

		time.Sleep(3 * time.Second)

		address := vlComponent.HTTPEndpoint()

		writer := &observability.PersistentLogWriter{
			Address: address,
			Timeout: 30 * time.Second,
		}
		writer.Populated()

		reader := &observability.LogReader{
			Address: address,
			Timeout: 30 * time.Second,
		}
		reader.Populated()

		entityID := identity.NewID()

		// Write 100 logs
		logCount := 100
		for i := 0; i < logCount; i++ {
			err := writer.WriteEntry(entityID, observability.LogEntry{
				Timestamp: time.Now(),
				Stream:    observability.Stdout,
				Body:      "high volume log " + string('0'+rune(i%10)),
			})
			r.NoError(err)
		}

		time.Sleep(3 * time.Second)

		// Read with high limit
		entries, err := reader.Read(ctx, entityID, observability.WithLimit(150))
		r.NoError(err)

		r.Equal(logCount, len(entries), "should have all logs")
	})
}
