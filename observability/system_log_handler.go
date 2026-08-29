package observability

import (
	"context"
	"log/slog"
)

// SystemLogEntityID is the well-known entity ID for system/server logs.
const SystemLogEntityID = "system/miren-server"

// safeAttrKey remaps slog attribute keys that would collide with reserved
// VictoriaLogs fields. For example, the controller framework logs
// "entity" to identify which entity triggered a reconcile — without
// remapping, this overwrites the VictoriaLogs "entity" routing field.
func safeAttrKey(key string) string {
	if isReservedLogField(key) {
		return "log_" + key
	}
	return key
}

// SystemLogHandler is an slog.Handler that tees log records to both an
// underlying handler (typically stderr) and a VictoriaLogs log writer.
// This enables querying server logs through the same `miren logs system`
// interface used for application and sandbox logs.
type SystemLogHandler struct {
	inner  slog.Handler
	writer LogWriter
	attrs  []slog.Attr
	groups []string
}

// NewSystemLogHandler wraps an existing handler, adding a tee to the given
// log writer. All log records are written to VictoriaLogs under the
// SystemLogEntityID entity with source:"system".
func NewSystemLogHandler(inner slog.Handler, writer LogWriter) *SystemLogHandler {
	return &SystemLogHandler{
		inner:  inner,
		writer: writer,
	}
}

func (h *SystemLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *SystemLogHandler) Handle(ctx context.Context, record slog.Record) error {
	// Always forward to the underlying handler first
	if err := h.inner.Handle(ctx, record); err != nil {
		return err
	}

	// Build attributes map from handler-level and record-level attrs
	attrs := make(map[string]string)
	attrs["source"] = "system"

	// Add handler-level attrs (from WithAttrs), remapping any that would
	// collide with reserved log fields used for routing in VictoriaLogs.
	for _, a := range h.attrs {
		attrs[safeAttrKey(a.Key)] = a.Value.String()
	}

	// Add record-level attrs
	record.Attrs(func(a slog.Attr) bool {
		attrs[safeAttrKey(a.Key)] = a.Value.String()
		return true
	})

	// Map slog level to stream (stdout for info/debug, stderr for warn/error)
	stream := Stdout
	if record.Level >= slog.LevelWarn {
		stream = Stderr
	}

	entry := LogEntry{
		Timestamp:  record.Time.UTC(),
		Stream:     stream,
		Attributes: attrs,
		Body:       record.Message,
	}

	// Best-effort write — don't block the logging pipeline
	_ = h.writer.WriteEntry(SystemLogEntityID, entry)

	return nil
}

func (h *SystemLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SystemLogHandler{
		inner:  h.inner.WithAttrs(attrs),
		writer: h.writer,
		attrs:  append(h.attrs[:len(h.attrs):len(h.attrs)], attrs...),
		groups: h.groups,
	}
}

func (h *SystemLogHandler) WithGroup(name string) slog.Handler {
	return &SystemLogHandler{
		inner:  h.inner.WithGroup(name),
		writer: h.writer,
		attrs:  h.attrs,
		groups: append(h.groups[:len(h.groups):len(h.groups)], name),
	}
}
