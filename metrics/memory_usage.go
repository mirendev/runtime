package metrics

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/units"
)

type MemoryUsage struct {
	Log *slog.Logger

	DB *sql.DB `asm:"clickhouse"`
}

var _ = autoreg.Register[MemoryUsage]()

func (m *MemoryUsage) Populated() error {
	return m.Setup()
}

func (m *MemoryUsage) Setup() error {
	stmts := []string{`
-- Table to store cgroup memory measurements
CREATE TABLE IF NOT EXISTS memory_usage (
    timestamp DateTime64(3), -- Millisecond precision timestamp
		entity LowCardinality(String), -- Entity identifier
    memory_usage UInt64,     -- Memory usage in bytes
    
    -- Order by timestamp and container for efficient time-series queries
    -- Partition by day to allow efficient data retention and queries
    -- Using ReplacingMergeTree to handle potential duplicate measurements
) ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (timestamp, entity);`,
		`
-- Create a materialized view with minute-level aggregation
-- This will improve query performance for minute-level analysis
CREATE MATERIALIZED VIEW IF NOT EXISTS memory_usage_minute
ENGINE = AggregatingMergeTree()
PARTITION BY toYYYYMMDD(timestamp_minute)
ORDER BY (timestamp_minute, entity)
AS SELECT
    toStartOfMinute(timestamp) as timestamp_minute,
    entity,
    max(memory_usage) as max_memory_usage
FROM memory_usage
GROUP BY
    timestamp_minute,
    entity;

`}

	for _, stmt := range stmts {
		_, err := m.DB.Exec(stmt)
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *MemoryUsage) RecordUsage(ctx context.Context, entity string, ts time.Time, memory units.Bytes) error {
	_, err := m.DB.ExecContext(ctx, `
    INSERT INTO memory_usage 
      (timestamp, entity, memory_usage)
    VALUES (toDateTime64('?', 6), ?, ?)
`,
		ts.UnixMicro(), entity, memory.Int64())
	return err
}

type MemoryUsageAtTime struct {
	Timestamp time.Time
	Memory    units.Bytes
}

func (m *MemoryUsage) UsageLastHour(entity string) ([]MemoryUsageAtTime, error) {
	query := `
-- Query to get memory usage over the last hour in 1-minute increments
-- Using the materialized view for better performance
SELECT
    timestamp_minute,
    max_memory_usage,
FROM memory_usage_minute
WHERE
    entity = @entity AND
    timestamp_minute >= now() - INTERVAL 1 HOUR
ORDER BY timestamp_minute ASC
WITH FILL 
	   FROM toStartOfMinute(now() - INTERVAL 1 HOUR) 
	   TO toStartOfMinute(now())
	   STEP INTERVAL 1 MINUTE;
`

	rows, err := m.DB.Query(query, sql.Named("entity", entity))
	if err != nil {
		return nil, err
	}

	var results []MemoryUsageAtTime

	for rows.Next() {
		var result MemoryUsageAtTime
		err = rows.Scan(&result.Timestamp, &result.Memory)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, err
}
