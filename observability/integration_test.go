//go:build linux

package observability_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/moby/buildkit/identity"
	"github.com/stretchr/testify/require"
	"miren.dev/runtime/components/victorialogs"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/logfilter"
	"miren.dev/runtime/pkg/testutils"
)

// uniqueNamespace generates a unique containerd namespace for test isolation.
// Containerd namespaces have a 76 character limit.
func uniqueNamespace() string {
	return fmt.Sprintf("vl-test-%d", time.Now().UnixNano())
}

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
		namespace := uniqueNamespace()
		httpPort := testutils.GetFreePort(t)

		vlComponent := victorialogs.NewVictoriaLogsComponent(logger, cc, namespace, tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        httpPort,
			RetentionPeriod: "1d",
		}

		err = vlComponent.Start(ctx, config)
		r.NoError(err)
		defer vlComponent.Stop(context.Background())

		// Give VictoriaLogs time to fully start
		time.Sleep(3 * time.Second)

		// Create writer and reader
		address := vlComponent.HTTPEndpoint()

		writer := observability.NewPersistentLogWriter(address, 30*time.Second)

		reader := observability.NewLogReader(address, 30*time.Second)

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
					"miren.sandbox": sandboxID,
					"phase":         "startup",
				},
			},
			{
				body:   "Warning: deprecated API used",
				stream: observability.Stderr,
				attributes: map[string]string{
					"miren.sandbox": sandboxID,
					"phase":         "runtime",
				},
			},
			{
				body:   "User action: button clicked",
				stream: observability.UserOOB,
				attributes: map[string]string{
					"miren.sandbox": sandboxID,
					"user_id":       "test-user",
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
			r.Equal(sandboxID, entry.Attributes["miren.sandbox"], "should have correct sandbox ID")
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
		namespace := uniqueNamespace()
		httpPort := testutils.GetFreePort(t)

		vlComponent := victorialogs.NewVictoriaLogsComponent(logger, cc, namespace, tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        httpPort,
			RetentionPeriod: "1d",
		}

		err = vlComponent.Start(ctx, config)
		r.NoError(err)
		defer vlComponent.Stop(context.Background())

		time.Sleep(3 * time.Second)

		address := vlComponent.HTTPEndpoint()

		writer := observability.NewPersistentLogWriter(address, 30*time.Second)

		reader := observability.NewLogReader(address, 30*time.Second)

		entityID := identity.NewID()

		// Write logs with specific timestamps
		baseTime := time.Now()
		for i := range 5 {
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
		namespace := uniqueNamespace()
		httpPort := testutils.GetFreePort(t)

		vlComponent := victorialogs.NewVictoriaLogsComponent(logger, cc, namespace, tmpDir)

		config := victorialogs.VictoriaLogsConfig{
			HTTPPort:        httpPort,
			RetentionPeriod: "1d",
		}

		err = vlComponent.Start(ctx, config)
		r.NoError(err)
		defer vlComponent.Stop(context.Background())

		time.Sleep(3 * time.Second)

		address := vlComponent.HTTPEndpoint()

		writer := observability.NewPersistentLogWriter(address, 30*time.Second)

		reader := observability.NewLogReader(address, 30*time.Second)

		entityID := identity.NewID()

		// Write 100 logs
		logCount := 100
		for i := range logCount {
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

func TestVictoriaLogsLatestQueryCompatibility(t *testing.T) {
	if os.Getenv("SKIP_INTEGRATION_TEST") != "" {
		t.Skip("Skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cc, err := containerd.New(containerdx.DefaultSocket)
	require.NoError(t, err)
	defer cc.Close()

	dataRoot := t.TempDir()
	namespace := uniqueNamespace()
	port := testutils.GetFreePort(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	component := victorialogs.NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	require.NoError(t, component.Start(ctx, victorialogs.VictoriaLogsConfig{
		HTTPPort: port, RetentionPeriod: "1d",
	}))
	defer func() { _ = component.Stop(context.Background()) }()

	writer := observability.NewPersistentLogWriter(component.HTTPEndpoint(), 30*time.Second)
	reader := observability.NewLogReader(component.HTTPEndpoint(), 30*time.Second)
	base := time.Now().Add(-time.Minute).Truncate(time.Second)
	logs := []observability.LogEntry{
		{Timestamp: base, Stream: observability.Stdout, Body: "ordinary message", Attributes: map[string]string{
			"miren.sandbox": "sandbox/query-test", "service": "web", "run": "run/query-test",
			"search_word": "needlestack", "phrase_field": "red green blue", "regex_field": "ticket-4821",
			"term_a": "alpha", "term_b": "beta", "punctuation": "status=500",
			"quoted": `say"hello`, "backslash": `C:\temp`, "reserved": "in",
			"contains_any_value": "contains_any", "contains_all_value": "contains_all",
		}},
		{Timestamp: base.Add(time.Second), Stream: observability.Stdout, Body: "build output", Attributes: map[string]string{
			"source": "build", "version": "v3",
		}},
		{Timestamp: base.Add(2 * time.Second), Stream: observability.Stdout, Body: "later message", Attributes: map[string]string{
			"miren.sandbox": "sandbox/query-test", "service": "worker", "debug_marker": "debug",
			"contains_any_value": "contains_any",
		}},
		{Timestamp: base.Add(3 * time.Second), Stream: observability.Stdout, Body: "latest message"},
	}
	for _, entry := range logs {
		require.NoError(t, writer.WriteEntry("app/query-test", entry))
	}
	for i, attributes := range []map[string]string{
		{"term_a": "alpha", "term_b": "beta"},
		{"term_a": "alpha"},
		{"term_b": "beta"},
		{"term_a": "alpha", "term_b": "beta", "debug_marker": "debug"},
	} {
		require.NoError(t, writer.WriteEntry("app/filter-test", observability.LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second), Stream: observability.Stdout,
			Body: fmt.Sprintf("filter candidate %d", i), Attributes: attributes,
		}))
	}
	require.NoError(t, writer.WriteEntry(observability.SystemLogEntityID, observability.LogEntry{
		Timestamp: base.Add(time.Second), Stream: observability.Stdout, Body: "system event",
		Attributes: map[string]string{"source": "system", "module": "scheduler"},
	}))
	// An expired row exercises retention rejection without waiting for a sweep.
	require.NoError(t, writer.WriteEntry("app/query-test", observability.LogEntry{
		Timestamp: base.Add(-48 * time.Hour), Stream: observability.Stdout, Body: "expired message",
	}))

	query := func(target observability.LogTarget, want []string, opts ...observability.LogReaderOption) []observability.LogEntry {
		t.Helper()
		var got []observability.LogEntry
		require.Eventually(t, func() bool {
			ch := make(chan observability.LogEntry, 32)
			err := reader.ReadStream(ctx, target, ch, opts...)
			close(ch)
			got = got[:0]
			for entry := range ch {
				got = append(got, entry)
			}
			if err != nil || len(got) != len(want) {
				return false
			}
			bodies := make([]string, len(got))
			for i, entry := range got {
				bodies[i] = entry.Body
			}
			slices.Sort(bodies)
			expected := slices.Clone(want)
			slices.Sort(expected)
			return slices.Equal(bodies, expected)
		}, 30*time.Second, 200*time.Millisecond)
		return slices.Clone(got)
	}
	assertBodies := func(target observability.LogTarget, want ...string) {
		t.Helper()
		entries := query(target, want, observability.WithFromTime(base.Add(-time.Second)))
		bodies := make([]string, 0, len(entries))
		for _, entry := range entries {
			bodies = append(bodies, entry.Body)
		}
		require.ElementsMatch(t, want, bodies)
	}

	app := observability.LogTarget{EntityID: "app/query-test"}
	assertCompiledFilter := func(input string, want ...string) {
		t.Helper()
		filter, parseErr := logfilter.Parse(input)
		require.NoError(t, parseErr)
		assertBodies(observability.LogTarget{EntityID: app.EntityID, Filter: filter.ToLogsQL()}, want...)
	}
	assertBodies(observability.LogTarget{EntityID: app.EntityID, Filter: `*:needlestack`}, "ordinary message")
	assertBodies(observability.LogTarget{EntityID: app.EntityID, Filter: `*:"red green"`}, "ordinary message")
	assertBodies(observability.LogTarget{EntityID: app.EntityID, Filter: `*:~"ticket-[0-9]+"`}, "ordinary message")
	assertBodies(observability.LogTarget{EntityID: "app/filter-test", Filter: `*:alpha *:beta -(*:debug)`}, "filter candidate 0")
	assertBodies(observability.LogTarget{SandboxID: "sandbox/query-test"}, "ordinary message", "later message")
	assertBodies(observability.LogTarget{EntityID: app.EntityID, Filter: `(service:"web" OR miren.service:"web")`}, "ordinary message")
	assertBodies(observability.LogTarget{EntityID: app.EntityID, Filter: `source:build version:"v3"`}, "build output")
	assertBodies(observability.LogTarget{EntityID: app.EntityID, Filter: `(run:"run/query-test" OR miren.run:"run/query-test")`}, "ordinary message")
	assertBodies(observability.LogTarget{EntityID: observability.SystemLogEntityID, Filter: `source:"system" module:"scheduler"`}, "system event")
	assertCompiledFilter(`status=500`, "ordinary message")
	assertCompiledFilter(`say"hello`, "ordinary message")
	assertCompiledFilter(`C:\temp`, "ordinary message")
	assertCompiledFilter(`in`, "ordinary message")
	assertCompiledFilter(`contains_any`, "ordinary message", "later message")
	assertCompiledFilter(`contains_all`, "ordinary message")
	assertCompiledFilter(`contains_any -contains_all`, "later message")

	bounded := query(app, []string{"ordinary message", "build output"}, observability.WithFromTime(base), observability.WithUntilTime(base.Add(2*time.Second)))
	require.Len(t, bounded, 2, "end is exclusive, so the row exactly at --until is omitted")
	limited := query(app, []string{"later message", "latest message"}, observability.WithFromTime(base.Add(-time.Second)), observability.WithLimit(2))
	require.Equal(t, []string{"later message", "latest message"}, []string{limited[0].Body, limited[1].Body})
	require.True(t, limited[0].Timestamp.Before(limited[1].Timestamp), "bounded results are normalized to chronological order")

	tailCtx, stopTail := context.WithTimeout(ctx, 20*time.Second)
	tailCh := make(chan observability.LogEntry, 4)
	tailErr := make(chan error, 1)
	go func() { tailErr <- reader.TailStream(tailCtx, app, tailCh) }()
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, writer.WriteEntry(app.EntityID, observability.LogEntry{
		Timestamp: time.Now(), Stream: observability.Stdout, Body: "live follow message",
	}))
	select {
	case entry := <-tailCh:
		require.Equal(t, "live follow message", entry.Body)
	case <-tailCtx.Done():
		t.Fatal("timed out waiting for live tail entry")
	}
	stopTail()
	<-tailErr

	invalidCh := make(chan observability.LogEntry, 1)
	err = reader.ReadStream(ctx, observability.LogTarget{EntityID: app.EntityID, Filter: `*:(`}, invalidCh)
	require.ErrorContains(t, err, "victorialogs returned status")

	require.NoError(t, component.Stop(ctx))
	component = victorialogs.NewVictoriaLogsComponent(logger, cc, namespace, dataRoot)
	require.NoError(t, component.Start(ctx, victorialogs.VictoriaLogsConfig{HTTPPort: port, RetentionPeriod: "1d"}))
	reader = observability.NewLogReader(component.HTTPEndpoint(), 30*time.Second)
	entries := query(app, []string{"ordinary message", "build output", "later message", "latest message", "live follow message"}, observability.WithFromTime(base.Add(-72*time.Hour)))
	for _, entry := range entries {
		require.NotEqual(t, "expired message", entry.Body)
	}
}

func BenchmarkVictoriaLogsAllFieldSearch(b *testing.B) {
	if os.Getenv("SKIP_INTEGRATION_TEST") != "" {
		b.Skip("Skipping integration benchmark")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cc, err := containerd.New(containerdx.DefaultSocket)
	require.NoError(b, err)
	defer cc.Close()

	port := testutils.GetFreePort(b)
	component := victorialogs.NewVictoriaLogsComponent(
		slog.New(slog.NewTextHandler(io.Discard, nil)), cc, uniqueNamespace(), b.TempDir(),
	)
	require.NoError(b, component.Start(ctx, victorialogs.VictoriaLogsConfig{
		HTTPPort: port, RetentionPeriod: "1d",
	}))
	defer func() { _ = component.Stop(context.Background()) }()

	const (
		rowCount   = 20_000
		fieldCount = 20
		entityID   = "app/wildcard-benchmark"
	)
	writeErrors := make(chan error, 1)
	batch := observability.NewBatchLogWriter(
		observability.NewPersistentLogWriter(component.HTTPEndpoint(), 30*time.Second),
		observability.WithBatchErrorHandler(func(err error, _ int) {
			select {
			case writeErrors <- err:
			default:
			}
		}),
	)
	base := time.Now().Add(-time.Hour)
	for i := range rowCount {
		attributes := make(map[string]string, fieldCount)
		for field := range fieldCount {
			attributes[fmt.Sprintf("field_%02d", field)] = fmt.Sprintf("value-%02d-%05d", field, i)
		}
		if i%200 == 0 {
			attributes["word_marker"] = "benchmarkneedle"
			attributes["phrase_marker"] = "scarlet orange violet"
			attributes["regex_marker"] = fmt.Sprintf("request-%06d", i)
		}
		require.NoError(b, batch.WriteEntry(entityID, observability.LogEntry{
			Timestamp:  base.Add(time.Duration(i) * time.Millisecond),
			Stream:     observability.Stdout,
			Body:       "production-like structured log record",
			Attributes: attributes,
		}))
	}
	batch.Close()
	select {
	case err := <-writeErrors:
		b.Fatalf("ingesting benchmark data: %v", err)
	default:
	}

	reader := observability.NewLogReader(component.HTTPEndpoint(), 30*time.Second)
	queries := map[string]string{
		"word":   `*:benchmarkneedle`,
		"phrase": `*:"scarlet orange"`,
		"regex":  `*:~"request-[0-9]{6}"`,
	}
	for name, filter := range queries {
		filter := filter
		b.Run(name, func(b *testing.B) {
			target := observability.LogTarget{EntityID: entityID, Filter: filter}
			read := func() ([]observability.LogEntry, error) {
				ch := make(chan observability.LogEntry, 100)
				err := reader.ReadStream(ctx, target, ch,
					observability.WithFromTime(base.Add(-time.Second)), observability.WithLimit(100))
				close(ch)
				entries := make([]observability.LogEntry, 0, len(ch))
				for entry := range ch {
					entries = append(entries, entry)
				}
				return entries, err
			}

			var warm []observability.LogEntry
			require.Eventually(b, func() bool {
				var readErr error
				warm, readErr = read()
				return readErr == nil && len(warm) == 100
			}, 30*time.Second, 200*time.Millisecond)

			b.ResetTimer()
			for range b.N {
				entries, err := read()
				if err != nil {
					b.Fatal(err)
				}
				if len(entries) != 100 {
					b.Fatalf("got %d matches, want 100", len(entries))
				}
			}
			b.ReportMetric(rowCount, "source_rows")
			b.ReportMetric(fieldCount, "fields/row")
		})
	}
}
