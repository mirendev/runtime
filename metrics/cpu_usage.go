package metrics

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/davecgh/go-spew/spew"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/units"
)

type CPUUsage struct {
	Log *slog.Logger

	DB *sql.DB `asm:"clickhouse"`
}

var _ = autoreg.Register[CPUUsage]()

func (m *CPUUsage) Populated() error {
	return m.Setup()
}

func (m *CPUUsage) Setup() error {
	_, err := m.DB.Exec(
		`
CREATE TABLE IF NOT EXISTS cpu_usage (
    timestamp DateTime64(6) CODEC(Delta(8), ZSTD(1)),
    window_end DateTime64(6) CODEC(Delta(8), ZSTD(1)),
    entity LowCardinality(String) CODEC(ZSTD(1)),
		cpu_usec UInt64,
		attributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),
    INDEX idx_attr_key mapKeys(attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_attr_value mapValues(attributes) TYPE bloom_filter(0.01) GRANULARITY 1
) 
		ENGINE = MergeTree
		ORDER BY (entity, toUnixTimestamp(timestamp))
		TTL toDateTime(timestamp) + INTERVAL 30 DAY
		SETTINGS ttl_only_drop_parts = 1;
`)
	return err
}

func (m *CPUUsage) RecordUsage(ctx context.Context, entity string, windowStart, windowEnd time.Time, cpuUsec units.Microseconds, attrs map[string]string) error {
	spew.Dump(windowStart, windowStart.UnixMicro(), windowEnd, windowEnd.UnixMicro())

	_, err := m.DB.ExecContext(ctx, `
    INSERT INTO cpu_usage 
      (timestamp, window_end, entity, cpu_usec, attributes)
    VALUES (toDateTime64('?', 6), toDateTime64('?', 6), ?, ?, ?)
`,
		windowStart.UnixMicro(), windowEnd.UnixMicro(), entity, cpuUsec, attrs)
	return err
}

type UsageAtTime struct {
	Timestamp time.Time
	Cores     float64
}

func (m *CPUUsage) CPUUsageLastHour(entity string) ([]UsageAtTime, error) {
	query := `
WITH 
minute_boundaries AS (
    SELECT 
        timestamp,
        window_end,
        cpu_usec,
        dateDiff('microsecond', timestamp, window_end) as window_duration_usec,
        bucket_minute
    FROM cpu_usage
    ARRAY JOIN 
        arrayDistinct(
            arrayMap(
                x -> toStartOfMinute(timestamp) + toIntervalMinute(x),
                range(0, dateDiff('minute', timestamp, window_end) + 1)
            )
        ) as bucket_minute
    WHERE entity = @entity
      AND bucket_minute >= now() - INTERVAL 1 HOUR
),
intersections AS (
    SELECT 
        bucket_minute,
        (cpu_usec * (
            dateDiff(
                'microsecond',
                greatest(bucket_minute, timestamp),
                least(bucket_minute + toIntervalMinute(1), window_end)
            ) / window_duration_usec
        )) / (60 * 1000000) as distributed_cpu_cores
    FROM minute_boundaries
    WHERE dateDiff('second', bucket_minute, window_end) > 0
        AND dateDiff('second', timestamp, bucket_minute + toIntervalMinute(1)) > 0
)
SELECT 
    bucket_minute,
    sum(distributed_cpu_cores) as total_cpu_cores
FROM intersections
GROUP BY 
    bucket_minute
ORDER BY 
    bucket_minute
WITH FILL 
     FROM toStartOfMinute(now()- INTERVAL 1 HOUR) 
	   TO toStartOfMinute(now())
	   STEP INTERVAL 1 MINUTE;
`

	rows, err := m.DB.Query(query, sql.Named("entity", entity))
	if err != nil {
		return nil, err
	}

	var results []UsageAtTime

	for rows.Next() {
		var result UsageAtTime
		err = rows.Scan(&result.Timestamp, &result.Cores)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	return results, err
}

// Returns the cpu usage in cores for the given entity in the last minute
func (m *CPUUsage) CPUUsageOver(entity string, interval string) (float64, error) {
	// TODO: this will overcount/undercount when a window doesn't fit exactly in the interval.
	// For now, we're going to not worry about that, but we should!
	query := `
SELECT
    -- Sum total CPU microseconds in the window
    round(
      sum(cpu_usec) / 
     (dateDiff('microsecond', min(timestamp), max(window_end))
    ), 3) AS avg_cores
FROM cpu_usage
WHERE entity = ? AND timestamp >= now() - INTERVAL ?
`
	var cores float64
	err := m.DB.QueryRow(query, entity, interval).Scan(&cores)
	if err != nil {
		return 0, err
	}

	return cores, err
}

// Returns the cpu usage in cores for the given entity in the last minute
func (m *CPUUsage) CurrentCPUUsage(entity string) (float64, error) {
	return m.CPUUsageOver(entity, "1 MINUTE")
}

func (m *CPUUsage) CPUUsageOverLastHour(entity string) (float64, error) {
	return m.CPUUsageOver(entity, "1 HOUR")
}

func (m *CPUUsage) CPUUsageOverDay(entity string) (float64, error) {
	return m.CPUUsageOver(entity, "24 HOUR")
}

// Returns the cpu usage in cores for the given entity in the last minute
func (m *CPUUsage) CPUUsageDayAgo(entity string, day units.Days) (float64, error) {
	query := `-- Calculate 1-minute rolling average
SELECT
    -- Sum total CPU microseconds in the window
    round(sum(cpu_usec) / 
    -- Divide by total microseconds in the window to get core fraction
    (dateDiff('microsecond', 
        min(timestamp), 
        max(window_end))
    ), 3) AS avg_cores
FROM cpu_usage
WHERE entity = ? 
  AND timestamp >= (today() - INTERVAL ? DAYS)
  AND timestamp < (today() - INTERVAL ? DAYS)
`
	var cores float64
	err := m.DB.QueryRow(query, entity, day+1, day).Scan(&cores)
	if err != nil {
		return 0, err
	}

	return cores, err
}
