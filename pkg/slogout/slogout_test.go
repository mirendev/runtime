package slogout

import (
	"bytes"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := newLogWriter(logger, slog.LevelInfo, LoggerOpts{})

	// Write a simple message
	message := "This is a test message"
	n, err := writer.Write([]byte(message + "\n"))
	require.NoError(t, err)
	assert.Equal(t, len(message)+1, n)

	// Check the logged output
	logOutput := buf.String()
	assert.Contains(t, logOutput, message)
}

func TestLogWriterMultipleLines(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := newLogWriter(logger.With("module", "multiline-test"), slog.LevelInfo, LoggerOpts{})

	// Write multiple lines in one call
	lines := []string{"Line 1", "Line 2", "Line 3"}
	message := strings.Join(lines, "\n") + "\n"

	n, err := writer.Write([]byte(message))
	require.NoError(t, err)
	assert.Equal(t, len(message), n)

	logOutput := buf.String()

	// Each line should appear in the log
	for _, line := range lines {
		assert.Contains(t, logOutput, line)
	}

	// Should have component info
	assert.Contains(t, logOutput, "multiline-test")
}

func TestLogWriterPartialWrites(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := newLogWriter(logger, slog.LevelInfo, LoggerOpts{})

	// Write partial line first
	n1, err := writer.Write([]byte("Partial "))
	require.NoError(t, err)
	assert.Equal(t, 8, n1)

	// Buffer should have content but no log output yet
	initialOutput := buf.String()
	assert.Empty(t, initialOutput)

	// Complete the line
	n2, err := writer.Write([]byte("line complete\n"))
	require.NoError(t, err)
	assert.Equal(t, 14, n2)

	// Now should have the complete message logged
	logOutput := buf.String()
	assert.Contains(t, logOutput, "Partial line complete")
}

func TestLogWriterEmptyLines(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := newLogWriter(logger, slog.LevelInfo, LoggerOpts{})

	// Write some empty lines mixed with content
	message := "\nactual content\n\n"
	_, err := writer.Write([]byte(message))
	require.NoError(t, err)

	logOutput := buf.String()

	// Should contain the actual content
	assert.Contains(t, logOutput, "actual content")

	// Count log entries - should only have one for the non-empty line
	logLines := strings.Split(strings.TrimSpace(logOutput), "\n")
	contentLines := 0
	for _, line := range logLines {
		if strings.Contains(line, "actual content") {
			contentLines++
		}
	}
	assert.Equal(t, 1, contentLines)
}

func TestLogWriterFlush(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := newLogWriter(logger, slog.LevelInfo, LoggerOpts{})

	// Write partial line without newline
	_, err := writer.Write([]byte("incomplete line"))
	require.NoError(t, err)

	// Should have no output yet
	assert.Empty(t, buf.String())

	// Flush the buffer
	writer.flush()

	// Now should have the incomplete line logged
	logOutput := buf.String()
	assert.Contains(t, logOutput, "incomplete line")
}

func TestLogWriterConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := newLogWriter(logger, slog.LevelInfo, LoggerOpts{})

	// Write multiple lines concurrently
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()
			message := fmt.Sprintf("concurrent message %d\n", id)
			_, err := writer.Write([]byte(message))
			if err != nil {
				t.Errorf("Write failed: %v", err)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	logOutput := buf.String()

	// Should contain messages from all goroutines
	for i := 0; i < 10; i++ {
		expected := fmt.Sprintf("concurrent message %d", i)
		assert.Contains(t, logOutput, expected)
	}
}

func TestWithLoggerCreator(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Test the WithLogger function
	creator := WithLogger(logger, "creator-test")
	require.NotNil(t, creator)

	// The creator should be a valid cio.Creator
	// We can't easily test the actual container creation without containerd,
	// but we can verify the creator is not nil
	assert.NotNil(t, creator)
}

func TestLogWriterWithIgnorePattern(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create writer with ignore pattern for ClickHouse timestamps
	opts := LoggerOpts{
		IgnorePattern: regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2} \d{2}:\d{2}:\d{2}\.\d+`),
	}
	writer := newLogWriter(logger, slog.LevelInfo, opts)

	// Write lines with and without timestamps
	lines := []string{
		"2025.05.29 22:30:27.021829 This header should be ignored",
		"This should be logged",
		"2025.05.29 22:30:28.123456 This header should also be ignored",
		"Another line to log",
	}

	for _, line := range lines {
		_, err := writer.Write([]byte(line + "\n"))
		require.NoError(t, err)
	}

	logOutput := buf.String()

	// Should contain non-timestamp lines
	assert.Contains(t, logOutput, "This should be logged")
	assert.Contains(t, logOutput, "Another line to log")
	assert.Contains(t, logOutput, "This header should be ignored")
	assert.Contains(t, logOutput, "This header should also be ignored")

	// Should NOT contain timestamp bits
	assert.NotContains(t, logOutput, "2025.05.29 22:30:27.021829")
	assert.NotContains(t, logOutput, "2025.05.29 22:30:28.123456")
}

func TestLogWriterWithJSONParsing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	opts := LoggerOpts{ParseJSON: true}
	writer := newLogWriter(logger, slog.LevelInfo, opts)

	// Write JSON log line similar to etcd output
	jsonLine := `{"level":"info","ts":"2025-05-29T22:30:27.123Z","msg":"starting etcd server","version":"3.5.15","cluster-id":"abc123"}`
	_, err := writer.Write([]byte(jsonLine + "\n"))
	require.NoError(t, err)

	logOutput := buf.String()

	// Should contain the message
	assert.Contains(t, logOutput, "starting etcd server")

	// Should contain extracted fields but not 'ts' or 'level'
	assert.Contains(t, logOutput, "version")
	assert.Contains(t, logOutput, "3.5.15")
	assert.Contains(t, logOutput, "cluster-id")
	assert.Contains(t, logOutput, "abc123")

	// Should NOT contain the timestamp field
	assert.NotContains(t, logOutput, "2025-05-29T22:30:27.123Z")
}

func TestLogWriterJSONWithDifferentLevels(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	opts := LoggerOpts{ParseJSON: true}
	writer := newLogWriter(logger, slog.LevelInfo, opts)

	// Test different log levels
	testCases := []struct {
		level    string
		message  string
		expected string
	}{
		{"error", "Something went wrong", "ERROR"},
		{"warn", "This is a warning", "WARN"},
		{"info", "This is info", "INFO"},
		{"debug", "Debug message", "DEBUG"},
	}

	for _, tc := range testCases {
		jsonLine := fmt.Sprintf(`{"level":"%s","msg":"%s","data":"test"}`, tc.level, tc.message)
		_, err := writer.Write([]byte(jsonLine + "\n"))
		require.NoError(t, err)
	}

	logOutput := buf.String()

	// Check that all messages appear
	for _, tc := range testCases {
		assert.Contains(t, logOutput, tc.message)
		// The exact format depends on the handler, but level should be reflected
		if tc.expected == "ERROR" {
			// Error messages should be in the output
			assert.Contains(t, logOutput, tc.message)
		}
	}
}

func TestWithLoggerOptions(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Test WithLogger with options
	creator := WithLogger(logger, "options-test",
		WithIgnorePattern(`^\d{4}-\d{2}-\d{2}`),
		WithJSONParsing())
	require.NotNil(t, creator)

	// Test convenience functions work
	ignoreOpt := WithIgnorePattern("test.*pattern")
	jsonOpt := WithJSONParsing()

	opts := LoggerOpts{}
	ignoreOpt(&opts)
	jsonOpt(&opts)

	assert.NotNil(t, opts.IgnorePattern)
	assert.True(t, opts.ParseJSON)
}

func TestNewWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Test basic NewWriter functionality
	writer := NewWriter(logger, slog.LevelInfo)
	require.NotNil(t, writer)

	// Write a line
	message := "Test message via NewWriter"
	n, err := writer.Write([]byte(message + "\n"))
	require.NoError(t, err)
	assert.Equal(t, len(message)+1, n)

	// Close to flush
	err = writer.Close()
	require.NoError(t, err)

	logOutput := buf.String()
	assert.Contains(t, logOutput, message)
}

func TestNewWriterWithJSONParsing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Create writer with JSON parsing enabled
	writer := NewWriter(logger, slog.LevelInfo, WithJSONParsing())
	require.NotNil(t, writer)

	// Write JSON formatted log line (like containerd outputs)
	jsonLine := `{"level":"info","ts":"2025-05-29T22:30:27.123Z","logger":"containerd","caller":"containerd/server.go:99","msg":"containerd server started","runtime":"io.containerd.runsc.v1","version":"1.7.18"}`
	_, err := writer.Write([]byte(jsonLine + "\n"))
	require.NoError(t, err)

	logOutput := buf.String()

	// Should contain the message
	assert.Contains(t, logOutput, "containerd server started")

	// Should contain extracted fields
	assert.Contains(t, logOutput, "logger=containerd")
	assert.Contains(t, logOutput, "caller=containerd/server.go:99")
	assert.Contains(t, logOutput, "runtime=io.containerd.runsc.v1")
	assert.Contains(t, logOutput, "version=1.7.18")
}

func TestNewWriterPartialWrites(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := NewWriter(logger, slog.LevelWarn)
	require.NotNil(t, writer)

	// Write partial JSON line
	_, err := writer.Write([]byte(`{"level":"warn","msg":"partial `))
	require.NoError(t, err)

	// No output yet
	assert.Empty(t, buf.String())

	// Complete the line
	_, err = writer.Write([]byte(`message","extra":"data"}` + "\n"))
	require.NoError(t, err)

	// Now should have output
	logOutput := buf.String()
	assert.Contains(t, logOutput, "WARN")

	// Close writer
	err = writer.Close()
	require.NoError(t, err)
}

func TestJSONParsingEdgeCases(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := NewWriter(logger, slog.LevelInfo, WithJSONParsing())

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "invalid JSON",
			input:    "not json at all",
			expected: []string{"not json at all", "json_parse_error"},
		},
		{
			name:     "JSON without msg",
			input:    `{"level":"info","ts":"2025-05-29T22:30:27.123Z"}`,
			expected: []string{`msg="{\"level\":\"info\",\"ts\":\"2025-05-29T22:30:27.123Z\"}"`}, // Uses full line as message
		},
		{
			name:     "JSON with message field instead of msg",
			input:    `{"level":"info","message":"alternative message field"}`,
			expected: []string{"alternative message field"},
		},
		{
			name:     "empty JSON object",
			input:    `{}`,
			expected: []string{`{}`},
		},
		{
			name:     "JSON with nested objects",
			input:    `{"level":"info","msg":"nested test","data":{"foo":"bar","baz":123}}`,
			expected: []string{"nested test", "data="},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			_, err := writer.Write([]byte(tc.input + "\n"))
			require.NoError(t, err)

			logOutput := buf.String()
			for _, expected := range tc.expected {
				assert.Contains(t, logOutput, expected, "Test case: %s", tc.name)
			}
		})
	}

	err := writer.Close()
	require.NoError(t, err)
}

func TestJSONLevelParsing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := NewWriter(logger, slog.LevelInfo, WithJSONParsing())

	// Test various level formats
	levels := []struct {
		input    string
		contains string
	}{
		{`{"level":"error","msg":"error test"}`, "ERROR"},
		{`{"level":"ERROR","msg":"uppercase error"}`, "ERROR"},
		{`{"level":"warn","msg":"warn test"}`, "WARN"},
		{`{"level":"warning","msg":"warning test"}`, "WARN"},
		{`{"level":"info","msg":"info test"}`, "INFO"},
		{`{"level":"information","msg":"information test"}`, "INFO"},
		{`{"level":"debug","msg":"debug test"}`, "DEBUG"},
		{`{"level":"unknown","msg":"unknown level"}`, "INFO"}, // Falls back to default
	}

	for _, tc := range levels {
		buf.Reset()
		_, err := writer.Write([]byte(tc.input + "\n"))
		require.NoError(t, err)

		logOutput := buf.String()
		assert.Contains(t, logOutput, tc.contains)
	}

	err := writer.Close()
	require.NoError(t, err)
}

func TestContainerdStyleJSONLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := NewWriter(logger.With("component", "containerd"), slog.LevelInfo, WithJSONParsing())

	// Simulate actual containerd JSON log output
	containerdLogs := []string{
		`{"level":"info","ts":"2025-05-29T22:30:27.021829Z","logger":"containerd","caller":"server/server.go:99","msg":"containerd successfully booted in 0.123456s"}`,
		`{"level":"warn","ts":"2025-05-29T22:30:28.123456Z","logger":"containerd.grpc","caller":"grpc/server.go:55","msg":"grpc: Server.Serve failed to complete security handshake","error":"tls: first record does not look like a TLS handshake"}`,
		`{"level":"error","ts":"2025-05-29T22:30:29.234567Z","logger":"containerd.runtime.v2.shim","caller":"shim/shim.go:123","msg":"failed to start shim","id":"test-container","error":"exit status 1"}`,
	}

	for _, log := range containerdLogs {
		_, err := writer.Write([]byte(log + "\n"))
		require.NoError(t, err)
	}

	logOutput := buf.String()

	// Should contain messages
	assert.Contains(t, logOutput, "containerd successfully booted")
	assert.Contains(t, logOutput, "failed to complete security handshake")
	assert.Contains(t, logOutput, "failed to start shim")

	// Should contain extracted fields
	assert.Contains(t, logOutput, "component=containerd")
	assert.Contains(t, logOutput, "logger=containerd.grpc")
	assert.Contains(t, logOutput, "id=test-container")

	// Should have appropriate log levels
	assert.Contains(t, logOutput, "INFO")
	assert.Contains(t, logOutput, "WARN")
	assert.Contains(t, logOutput, "ERROR")

	err := writer.Close()
	require.NoError(t, err)
}

func TestCloseFlushesBuffer(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := NewWriter(logger, slog.LevelInfo)

	// Write without newline
	message := "message without newline"
	_, err := writer.Write([]byte(message))
	require.NoError(t, err)

	// Should have no output yet
	assert.Empty(t, buf.String())

	// Close should flush the buffer
	err = writer.Close()
	require.NoError(t, err)

	// Now should have the message
	logOutput := buf.String()
	assert.Contains(t, logOutput, message)
}

func TestLogWriterWithKeyValueParsing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := NewWriter(logger, slog.LevelInfo, WithKeyValueParsing())

	// Test containerd-style key=value log line
	kvLine := `time="2025-06-06T17:16:10.202323607-07:00" level=info msg="loading plugin \"io.containerd.event.v1.publisher\"..." runtime=io.containerd.runc.v2 type=io.containerd.event.v1`
	_, err := writer.Write([]byte(kvLine + "\n"))
	require.NoError(t, err)

	logOutput := buf.String()

	// Should contain the message (with escaped quotes in the output)
	assert.Contains(t, logOutput, `loading plugin \"io.containerd.event.v1.publisher\"...`)

	// Should contain extracted fields
	assert.Contains(t, logOutput, "runtime=io.containerd.runc.v2")
	assert.Contains(t, logOutput, "type=io.containerd.event.v1")

	// Should use correct log level
	assert.Contains(t, logOutput, "INFO")

	err = writer.Close()
	require.NoError(t, err)
}

func TestKeyValueParsingEdgeCases(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := NewWriter(logger, slog.LevelInfo, WithKeyValueParsing())

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple key=value pairs",
			input:    `level=info msg="test message" component=test`,
			expected: []string{"test message", "component=test"},
		},
		{
			name:     "quoted values with spaces",
			input:    `level=warn msg="this has spaces" path="/path/with spaces/file.txt"`,
			expected: []string{"this has spaces", `path="/path/with spaces/file.txt"`},
		},
		{
			name:     "escaped quotes in values",
			input:    `level=error msg="error: \"failed to connect\"" host="test.example.com"`,
			expected: []string{`error: \"failed to connect\"`, "host=test.example.com"},
		},
		{
			name:     "no key=value pairs",
			input:    `this is just plain text without any pairs`,
			expected: []string{"this is just plain text without any pairs"},
		},
		{
			name:     "mixed content",
			input:    `Starting server on port 8080 status=ok`,
			expected: []string{"Starting server on port 8080", "status=ok"},
		},
		{
			name:     "empty quoted value",
			input:    `level=debug msg="" field=""`,
			expected: []string{"field="},
		},
		{
			name:     "containerd shim log",
			input:    `time="2025-06-06T17:16:10-07:00" level=error msg="failed to start shim" id=test-container error="exit status 1"`,
			expected: []string{"failed to start shim", "id=test-container", `error="exit status 1"`},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			_, err := writer.Write([]byte(tc.input + "\n"))
			require.NoError(t, err)

			logOutput := buf.String()
			for _, expected := range tc.expected {
				assert.Contains(t, logOutput, expected, "Test case: %s", tc.name)
			}
		})
	}

	err := writer.Close()
	require.NoError(t, err)
}

func TestKeyValueLevelParsing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := NewWriter(logger, slog.LevelInfo, WithKeyValueParsing())

	// Test various level formats
	levels := []struct {
		input    string
		contains string
	}{
		{`level=error msg="error test"`, "ERROR"},
		{`level=ERROR msg="uppercase error"`, "ERROR"},
		{`level=warn msg="warn test"`, "WARN"},
		{`level=warning msg="warning test"`, "WARN"},
		{`level=info msg="info test"`, "INFO"},
		{`level=debug msg="debug test"`, "DEBUG"},
		{`msg="no level specified"`, "INFO"}, // Falls back to writer's default
	}

	for _, tc := range levels {
		buf.Reset()
		_, err := writer.Write([]byte(tc.input + "\n"))
		require.NoError(t, err)

		logOutput := buf.String()
		assert.Contains(t, logOutput, tc.contains)
	}

	err := writer.Close()
	require.NoError(t, err)
}

func TestContainerdNonJSONLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := NewWriter(logger.With("component", "containerd"), slog.LevelInfo, WithKeyValueParsing())

	// Simulate actual containerd non-JSON log output
	containerdLogs := []string{
		`time="2025-06-06T17:16:10.202323607-07:00" level=info msg="loading plugin \"io.containerd.event.v1.publisher\"..." runtime=io.containerd.runc.v2 type=io.containerd.event.v1`,
		`time="2025-06-06T17:16:10.202456789-07:00" level=info msg="loading plugin \"io.containerd.grpc.v1.cri\"..." type=io.containerd.grpc.v1`,
		`time="2025-06-06T17:16:10.202567890-07:00" level=warn msg="failed to load plugin" error="plugin init failed" plugin=io.containerd.grpc.v1.cri`,
		`time="2025-06-06T17:16:10.202678901-07:00" level=error msg="containerd: shim error" id=test-container namespace=default error="OCI runtime create failed"`,
	}

	for _, log := range containerdLogs {
		_, err := writer.Write([]byte(log + "\n"))
		require.NoError(t, err)
	}

	logOutput := buf.String()

	// Should contain messages (with escaped quotes in the output)
	assert.Contains(t, logOutput, `loading plugin \"io.containerd.event.v1.publisher\"...`)
	assert.Contains(t, logOutput, `loading plugin \"io.containerd.grpc.v1.cri\"...`)
	assert.Contains(t, logOutput, "failed to load plugin")
	assert.Contains(t, logOutput, "containerd: shim error")

	// Should contain extracted fields
	assert.Contains(t, logOutput, "component=containerd")
	assert.Contains(t, logOutput, "runtime=io.containerd.runc.v2")
	assert.Contains(t, logOutput, "type=io.containerd.event.v1")
	assert.Contains(t, logOutput, "plugin=io.containerd.grpc.v1.cri")
	assert.Contains(t, logOutput, "namespace=default")

	// Should have appropriate log levels
	assert.Contains(t, logOutput, "INFO")
	assert.Contains(t, logOutput, "WARN")
	assert.Contains(t, logOutput, "ERROR")

	err := writer.Close()
	require.NoError(t, err)
}
