package run

import (
	"context"
	"database/sql"
	"time"
)

type LogEntry struct {
	Timestamp time.Time
	Body      string
}

type LogWriter struct {
	DB *sql.DB `asm:"clickhouse"`
}

func (l *LogWriter) WriteEntry(id string, body string) error {
	_, err := l.DB.Exec("INSERT INTO logs (timestamp, container_id, body) VALUES (NOW(), ?, ?)", id, body)
	return err
}

type LogReader struct {
	DB *sql.DB `asm:"clickhouse"`
}

func (l *LogReader) Read(ctx context.Context, id string) ([]LogEntry, error) {
	rows, err := l.DB.QueryContext(ctx, "SELECT timestamp, body FROM logs WHERE container_id = ?", id)
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

func (m *LogsMaintainer) Setup(ctx context.Context) error {
	_, err := m.DB.ExecContext(ctx, `
CREATE TABLE logs
(
    timestamp DateTime64(9) CODEC(Delta(8), ZSTD(1)),
    container_id LowCardinality(String) CODEC(ZSTD(1)),
    body String CODEC(ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toDate(timestamp)
ORDER BY (container_id, toUnixTimestamp(timestamp))
`)

	return err
}
