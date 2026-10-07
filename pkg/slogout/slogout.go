// Package slogout provides adapters to route container output through slog.Logger
// instead of directly to stdout/stderr. This is useful for modules that launch
// containers (like etcd) where we want structured logging instead
// of raw container output mixed with our application logs.
package slogout

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/containerd/containerd/v2/pkg/cio"

	"miren.dev/runtime/pkg/logcount"
)

// LoggerOpts provides options for configuring log processing behavior
type LoggerOpts struct {
	// IgnorePattern is a regexp that, if matched, will cause the line to be ignored
	IgnorePattern *regexp.Regexp
	// ParseJSON indicates whether to parse each line as JSON and extract key/value pairs
	ParseJSON bool
	// ParseKeyValue indicates whether to parse each line as key=value pairs
	ParseKeyValue bool
	// ParseVictoria indicates whether to parse each line in the tab-separated
	// format VictoriaMetrics components log in by default
	ParseVictoria bool

	// Source is the child process this output comes from. Its lines are
	// counted under that name and at their own level, even when ClampLevel
	// prints them lower. SourceMiren, the zero value, counts them as miren's.
	Source logcount.Source

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

// WithVictoriaParsing enables parsing of the VictoriaMetrics log format
// (timestamp, level, caller and message separated by tabs), used by
// vmagent, victoria-metrics and victoria-logs.
func WithVictoriaParsing() LoggerOption {
	return func(opts *LoggerOpts) {
		opts.ParseVictoria = true
	}
}

// WithSource names the child process whose output this is, for log line
// counting. WithLogger and AttachLogger set it from their module argument.
func WithSource(name string) LoggerOption {
	return func(opts *LoggerOpts) {
		opts.Source = logcount.SourceFor(name)
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

	// relayCtx caches one counting context per child level, so a relayed
	// line costs no allocation. Guarded by mu.
	relayCtx map[slog.Level]context.Context
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
	case w.opts.ParseVictoria:
		w.processVictoriaLine(line)
	default:
		w.emit(w.level, false, line)
	}
}

// emit logs one line that the child wrote at level. With clamp set and
// ClampLevel configured, a level above MaxLevel is printed at MaxLevel and
// the child's own level is kept in an orig-level attribute. Either way the
// line is counted at the child's level.
func (w *logWriter) emit(level slog.Level, clamp bool, msg string, attrs ...slog.Attr) {
	ctx := w.countContext(level)
	if clamp && w.opts.ClampLevel && level > w.opts.MaxLevel {
		attrs = append(attrs, slog.String("orig-level", level.String()))
		level = w.opts.MaxLevel
	}
	w.logger.LogAttrs(ctx, level, msg, attrs...)
}

func (w *logWriter) countContext(level slog.Level) context.Context {
	if w.opts.Source == logcount.SourceMiren {
		return context.TODO()
	}
	if ctx, ok := w.relayCtx[level]; ok {
		return ctx
	}
	if w.relayCtx == nil {
		w.relayCtx = make(map[slog.Level]context.Context)
	}
	ctx := logcount.Relayed(context.TODO(), w.opts.Source, level)
	w.relayCtx[level] = ctx
	return ctx
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
	var jsonData map[string]any
	if err := json.Unmarshal([]byte(line), &jsonData); err != nil {
		// If JSON parsing fails, log as plain text
		w.emit(w.level, false, line, slog.String("json_parse_error", err.Error()))
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

	w.emit(level, true, message, attrs...)
}

// keyValuePattern matches key=value pairs, handling quoted values
var keyValuePattern = regexp.MustCompile(`([\w.-]+)=("(?:[^"\\]|\\.)*"|[^\s]+)`)

// processKeyValueLine parses a line containing key=value pairs
func (w *logWriter) processKeyValueLine(line string) {
	matches := keyValuePattern.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		// No key=value pairs found, log as plain text
		w.emit(w.level, false, line)
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

	w.emit(level, true, message, attrs...)
}

// processVictoriaLine parses a line in the VictoriaMetrics default format:
// timestamp, level, caller and message, separated by tabs. The timestamp is
// dropped because the logger stamps its own.
func (w *logWriter) processVictoriaLine(line string) {
	parts := strings.SplitN(line, "\t", 4)
	if len(parts) != 4 {
		w.emit(w.level, false, line)
		return
	}
	w.emit(parseLogLevel(parts[1]), true, parts[3], slog.String("caller", parts[2]))
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
	case "error", "dpanic", "panic", "fatal":
		return slog.LevelError
	default:
		return slog.LevelInfo // Default fallback
	}
}

func loggerStreams(logger *slog.Logger, module string, options ...LoggerOption) cio.Opt {
	opts := LoggerOpts{Source: logcount.SourceFor(module)}
	for _, option := range options {
		option(&opts)
	}

	return cio.WithStreams(
		nil, // stdin - not used
		newLogWriter(logger.With("module", module), slog.LevelInfo, opts),
		newLogWriter(logger.With("module", module), slog.LevelInfo, opts),
	)
}

// WithLogger creates a cio.Creator that routes container output through slog.Logger
// instead of the default stdio. The module parameter is used to tag log entries
// with the source module (e.g., "etcd").
func WithLogger(logger *slog.Logger, module string, options ...LoggerOption) cio.Creator {
	return cio.NewCreator(loggerStreams(logger, module, options...))
}

// AttachLogger creates a cio.Attach that reconnects an existing task's output
// FIFOs to slog.Logger.
func AttachLogger(logger *slog.Logger, module string, options ...LoggerOption) cio.Attach {
	return cio.NewAttach(loggerStreams(logger, module, options...))
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
