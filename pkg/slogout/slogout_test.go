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

	writer := newLogWriter(logger, slog.LevelInfo, "stdout", "test-component", LoggerOpts{})

	// Write a simple message
	message := "This is a test message"
	n, err := writer.Write([]byte(message + "\n"))
	require.NoError(t, err)
	assert.Equal(t, len(message)+1, n)

	// Check the logged output
	logOutput := buf.String()
	assert.Contains(t, logOutput, message)
	assert.Contains(t, logOutput, "test-component")
	assert.Contains(t, logOutput, "stdout")
}

func TestLogWriterMultipleLines(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := newLogWriter(logger, slog.LevelInfo, "stdout", "multiline-test", LoggerOpts{})

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

	writer := newLogWriter(logger, slog.LevelInfo, "stdout", "partial-test", LoggerOpts{})

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
	assert.Contains(t, logOutput, "partial-test")
}

func TestLogWriterEmptyLines(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := newLogWriter(logger, slog.LevelInfo, "stdout", "empty-test", LoggerOpts{})

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

	writer := newLogWriter(logger, slog.LevelInfo, "stdout", "flush-test", LoggerOpts{})

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
	assert.Contains(t, logOutput, "flush-test")
}

func TestLogWriterConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	writer := newLogWriter(logger, slog.LevelInfo, "stdout", "concurrent-test", LoggerOpts{})

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
	writer := newLogWriter(logger, slog.LevelInfo, "stdout", "clickhouse-test", opts)

	// Write lines with and without timestamps
	lines := []string{
		"2025.05.29 22:30:27.021829 This should be ignored",
		"This should be logged",
		"2025.05.29 22:30:28.123456 This should also be ignored",
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
	
	// Should NOT contain timestamp lines
	assert.NotContains(t, logOutput, "This should be ignored")
	assert.NotContains(t, logOutput, "This should also be ignored")
}

func TestLogWriterWithJSONParsing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	opts := LoggerOpts{ParseJSON: true}
	writer := newLogWriter(logger, slog.LevelInfo, "stdout", "etcd-test", opts)

	// Write JSON log line similar to etcd output
	jsonLine := `{"level":"info","ts":"2025-05-29T22:30:27.123Z","msg":"starting etcd server","version":"3.5.15","cluster-id":"abc123"}`
	_, err := writer.Write([]byte(jsonLine + "\n"))
	require.NoError(t, err)

	logOutput := buf.String()
	
	// Should contain the message
	assert.Contains(t, logOutput, "starting etcd server")
	
	// Should contain component info
	assert.Contains(t, logOutput, "etcd-test")
	
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
	writer := newLogWriter(logger, slog.LevelInfo, "stdout", "level-test", opts)

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