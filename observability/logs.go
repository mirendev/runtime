package observability

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/asm/autoreg"
)

type LogStream string

const (
	Stdout  LogStream = "stdout"
	Stderr  LogStream = "stderr"
	Error   LogStream = "error"
	UserOOB LogStream = "user-oob"
)

type LogEntry struct {
	Timestamp time.Time
	Stream    LogStream
	Body      string
}

type PersistentLogWriter struct {
	DB *sql.DB `asm:"clickhouse"`
}

func (l *PersistentLogWriter) WriteEntry(entity string, le LogEntry) error {
	_, err := l.DB.Exec("INSERT INTO logs (timestamp, entity, stream, body) VALUES (?, ?, ?, ?)",
		le.Timestamp.UnixMicro(), entity, le.Stream, le.Body)
	return err
}

type PersistentLogReader struct {
	DB *sql.DB `asm:"clickhouse"`
}

func (l *PersistentLogReader) Read(ctx context.Context, id string) ([]LogEntry, error) {
	rows, err := l.DB.QueryContext(ctx, "SELECT timestamp, body FROM logs WHERE entity = ?", id)
	if err != nil {
		return nil, err
	}

	var entries []LogEntry

	for rows.Next() {
		var e LogEntry
		err := rows.Scan(&e.Timestamp, &e.Body)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}

	return entries, nil
}

type LogsMaintainer struct {
	DB *sql.DB `asm:"clickhouse"`
}

var _ = autoreg.Register[LogsMaintainer]()

func (m *LogsMaintainer) Setup(ctx context.Context) error {
	_, err := m.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS logs
(
    timestamp DateTime64(6) CODEC(Delta(8), ZSTD(1)),
    entity LowCardinality(String) CODEC(ZSTD(1)),
		stream Enum8('stdout' = 1, 'stderr' = 2, 'error' = 3, 'user-oob' = 4) CODEC(ZSTD(1)),
    body String CODEC(ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toDate(timestamp)
ORDER BY (timestamp, entity, stream)
`)

	return err
}

type LogWriter interface {
	WriteEntry(entity string, le LogEntry) error
}

type DebugLogWriter struct {
	Log *slog.Logger
}

func (d *DebugLogWriter) WriteEntry(entity string, le LogEntry) error {
	d.Log.Debug(le.Body, "stream", le.Stream, "entity", entity)
	return nil
}

type LogReader struct {
	DB *sql.DB `asm:"clickhouse"`
}

var _ = autoreg.Register[LogReader]()

type logReadOpts struct {
	From  time.Time
	Limit int
}

type LogReaderOption func(*logReadOpts)

func WithFromTime(t time.Time) LogReaderOption {
	return func(o *logReadOpts) {
		o.From = t
	}
}

func WithLimit(l int) LogReaderOption {
	return func(o *logReadOpts) {
		o.Limit = l
	}
}

func (l *LogReader) Read(ctx context.Context, id string, opts ...LogReaderOption) ([]LogEntry, error) {
	var o logReadOpts

	for _, opt := range opts {
		opt(&o)
	}

	var (
		rows *sql.Rows
		err  error

		limit = o.Limit
	)

	if limit == 0 {
		limit = 100
	}

	if !o.From.IsZero() {
		rows, err = l.DB.QueryContext(ctx,
			`SELECT timestamp, stream, body
			   FROM logs
			  WHERE entity = ? AND timestamp >= '?'
			  ORDER BY timestamp ASC
			  LIMIT ?`, id, o.From.UnixMicro(), limit)
		if err != nil {
			return nil, err
		}
	} else {
		rows, err = l.DB.QueryContext(ctx,
			`SELECT timestamp, stream, body
			   FROM logs
			  WHERE entity = ?
			  ORDER BY timestamp ASC
			  LIMIT ?`, id, limit)
		if err != nil {
			return nil, err
		}
	}

	var entries []LogEntry

	for rows.Next() {
		var e LogEntry
		err := rows.Scan(&e.Timestamp, &e.Stream, &e.Body)
		if err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}

	if err := rows.Close(); err != nil {
		return nil, err
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil

	return entries, nil
}
