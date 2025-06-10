// Package slogout provides adapters to route container output through slog.Logger
// instead of directly to stdout/stderr. This is useful for modules that launch
// containers (like etcd and clickhouse) where we want structured logging instead
// of raw container output mixed with our application logs.
package slogout

import (
	"encoding/json"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/containerd/containerd/v2/pkg/cio"
)

// LoggerOpts provides options for configuring log processing behavior
type LoggerOpts struct {
	// IgnorePattern is a regexp that, if matched, will cause the line to be ignored
	IgnorePattern *regexp.Regexp
	// ParseJSON indicates whether to parse each line as JSON and extract key/value pairs
	ParseJSON bool
	// ParseKeyValue indicates whether to parse each line as key=value pairs
	ParseKeyValue bool

	ClampLevel bool       // If true, will clamp log levels to MaxLevel
	MaxLevel   slog.Level // Maximum log level to process (default: Info)
}

// LoggerOption is a function that configures LoggerOpts
type LoggerOption func(*LoggerOpts)

// WithIgnorePattern sets a regexp pattern to ignore matching lines
func WithIgnorePattern(pattern string) LoggerOption {
	return func(opts *LoggerOpts) {
		if pattern != "" {
			opts.IgnorePattern = regexp.MustCompile(pattern)
		}
	}
}

// WithJSONParsing enables JSON parsing of log lines
func WithJSONParsing() LoggerOption {
	return func(opts *LoggerOpts) {
		opts.ParseJSON = true
	}
}

// WithKeyValueParsing enables key=value parsing of log lines
func WithKeyValueParsing() LoggerOption {
	return func(opts *LoggerOpts) {
		opts.ParseKeyValue = true
	}
}

// WithMaxLevel sets the maximum log level to process
func WithMaxLevel(level slog.Level) LoggerOption {
	return func(opts *LoggerOpts) {
		opts.ClampLevel = true
		opts.MaxLevel = level
	}
}

// logWriter wraps an slog.Logger to implement io.Writer interface.
// Each write operation splits the input by lines and logs each line separately.
type logWriter struct {
	logger *slog.Logger
	level  slog.Level
	opts   LoggerOpts
	buf    []byte
	mu     sync.Mutex
}

// newLogWriter creates a new logWriter that routes output to slog.Logger
func newLogWriter(logger *slog.Logger, level slog.Level, opts LoggerOpts) *logWriter {
	return &logWriter{
		logger: logger,
		level:  level,
		opts:   opts,
	}
}

// Write implements io.Writer interface
func (w *logWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Append new data to buffer
	w.buf = append(w.buf, p...)

	// Process complete lines
	for {
		lineEnd := -1
		for i, b := range w.buf {
			if b == '\n' {
				lineEnd = i
				break
			}
		}

		if lineEnd == -1 {
			// No complete line yet
			break
		}

		// Extract the line (excluding \n)
		line := string(w.buf[:lineEnd])

		// Remove the processed line from buffer
		w.buf = w.buf[lineEnd+1:]

		// Log non-empty lines
		if line != "" {
			w.processLine(line)
		}
	}

	return len(p), nil
}

// flush logs any remaining data in the buffer
func (w *logWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) > 0 {
		line := string(w.buf)
		if line != "" {
			w.processLine(line)
		}
		w.buf = nil
	}
}

// processLine handles a single log line according to the configured options
func (w *logWriter) processLine(line string) {
	// Check if we should ignore this line
	if w.opts.IgnorePattern != nil {
		loc := w.opts.IgnorePattern.FindStringIndex(line)
		if loc != nil {
			line = strings.TrimSpace(line[loc[1]:])
		}
	}

	// Parse based on configuration
	switch {
	case w.opts.ParseJSON:
		w.processJSONLine(line)
	case w.opts.ParseKeyValue:
		w.processKeyValueLine(line)
	default:
		// Log as plain text
		w.logger.Log(nil, w.level, line)
	}
}

var jsonIgnoreKeys = map[string]struct{}{
	"ts":      {},
	"time":    {},
	"level":   {},
	"msg":     {},
	"message": {},
}

// processJSONLine parses a JSON line and extracts key/value pairs
func (w *logWriter) processJSONLine(line string) {
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(line), &jsonData); err != nil {
		// If JSON parsing fails, log as plain text
		w.logger.Log(nil, w.level, line,
			"json_parse_error", err.Error())
		return
	}

	// Extract level if present, otherwise use default
	level := w.level
	if levelValue, exists := jsonData["level"]; exists {
		if levelStr, ok := levelValue.(string); ok {
			level = parseLogLevel(levelStr)
		}
	}

	// Build attributes from JSON data, excluding 'ts' and 'level'
	var attrs []slog.Attr

	if w.opts.ClampLevel && level > w.opts.MaxLevel {
		level = w.opts.MaxLevel // Respect maximum log level
		attrs = append(attrs, slog.String("orig-level", level.String()))
	}

	for key, value := range jsonData {
		if _, ignore := jsonIgnoreKeys[key]; ignore {
			continue // Skip ignored keys
		}

		attrs = append(attrs, slog.Any(key, value))
	}

	// Get the message - try 'msg' first, then 'message', then use full JSON
	var message string
	if msg, exists := jsonData["msg"]; exists {
		if msgStr, ok := msg.(string); ok {
			message = msgStr
		}
	}
	if message == "" {
		if msg, exists := jsonData["message"]; exists {
			if msgStr, ok := msg.(string); ok {
				message = msgStr
			}
		}
	}
	if message == "" {
		message = line // Use full JSON line as message
	}

	w.logger.LogAttrs(nil, level, message, attrs...)
}

// keyValuePattern matches key=value pairs, handling quoted values
var keyValuePattern = regexp.MustCompile(`(\w+)=("(?:[^"\\]|\\.)*"|[^\s]+)`)

var kvIgnoreKeys = map[string]struct{}{
	"time":  {},
	"level": {},
	"msg":   {},
}

// processKeyValueLine parses a line containing key=value pairs
func (w *logWriter) processKeyValueLine(line string) {
	matches := keyValuePattern.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		// No key=value pairs found, log as plain text
		w.logger.Log(nil, w.level, line)
		return
	}

	level := w.level
	message := ""

	var attrs []slog.Attr

	for _, match := range matches {
		if len(match) >= 3 {
			key := match[1]
			value := match[2]
			// Remove quotes if present
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				// Unescape the quoted string
				value = value[1 : len(value)-1]
				value = strings.ReplaceAll(value, `\"`, `"`)
				value = strings.ReplaceAll(value, `\\`, `\`)
			}

			switch key {
			case "ts", "time":
				// Ignore timestamp keys
				continue
			case "level":
				level = parseLogLevel(value)
			case "msg", "message":
				message = value
			default:
				attrs = append(attrs, slog.String(key, value))
			}
		}
	}

	if message == "" {
		message = line // Use the full line as message if no msg key found
	}

	if level > w.opts.MaxLevel && w.opts.ClampLevel {
		level = w.opts.MaxLevel // Respect maximum log level
		attrs = append(attrs, slog.String("orig-level", level.String()))
	}

	w.logger.LogAttrs(nil, level, message, attrs...)
}

// parseLogLevel converts a string level to slog.Level
func parseLogLevel(levelStr string) slog.Level {
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "info", "information":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo // Default fallback
	}
}

// WithLogger creates a cio.Creator that routes container output through slog.Logger
// instead of the default stdio. The module parameter is used to tag log entries
// with the source module (e.g., "etcd", "clickhouse").
func WithLogger(logger *slog.Logger, module string, options ...LoggerOption) cio.Creator {
	opts := LoggerOpts{}
	for _, option := range options {
		option(&opts)
	}

	return cio.NewCreator(cio.WithStreams(
		nil, // stdin - not used
		newLogWriter(logger.With("module", module), slog.LevelInfo, opts),
		newLogWriter(logger.With("module", module), slog.LevelInfo, opts),
	))
}

// NewWriter creates an io.WriteCloser that can be used as cmd.Stdout/cmd.Stderr
// to parse and route output through slog.Logger. The returned writer should be
// closed when done to flush any remaining buffered data.
func NewWriter(logger *slog.Logger, level slog.Level, options ...LoggerOption) io.WriteCloser {
	opts := LoggerOpts{}
	for _, option := range options {
		option(&opts)
	}

	return newLogWriter(logger, level, opts)
}

// Close implements io.Closer to flush remaining data
func (w *logWriter) Close() error {
	w.flush()
	return nil
}
