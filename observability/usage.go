package observability

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"miren.dev/runtime/pkg/asm/autoreg"
)

type ResourcesMonitor struct {
	DB  *sql.DB `asm:"clickhouse"`
	Log *slog.Logger
}

func (m *ResourcesMonitor) Setup(ctx context.Context) error {
	_, err := m.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS resource_usage
(
    timestamp DateTime64(9) CODEC(Delta(8), ZSTD(1)),
    container_id LowCardinality(String) CODEC(ZSTD(1)),
		cpu Float32 CODEC(ZSTD(1)),
		memory UInt64 CODEC(ZSTD(1))
)
ENGINE = MergeTree
PARTITION BY toDate(timestamp)
ORDER BY (container_id, toUnixTimestamp(timestamp))
`)

	return err
}

var _ = autoreg.Register[ResourcesMonitor]()

func CGroupPathForPid(pid uint32) (string, error) {
	// read cgroup
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(int(pid)), "cgroup"))
	if err != nil {
		return "", err
	}

	_, path, ok := bytes.Cut(data, []byte("::"))
	if !ok {
		return "", errors.New("failed to parse cgroup")
	}

	return filepath.Join("/sys/fs/cgroup", string(bytes.TrimSpace(path))), nil
}

func (m *ResourcesMonitor) Monitor(ctx context.Context, id, cgroupPath string) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var (
		prev     uint64
		lastTick time.Time
		buf      = make([]byte, 1024)
	)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ts := <-ticker.C:
			// read cgroup stats
			cur, err := m.readCgroupStats(cgroupPath, buf)
			if err != nil {
				m.Log.Error("failed to read cgroup stats", "err", err)
				continue
			}

			if prev != 0 {
				cpuDelta := cur - prev

				perc := (float64(cpuDelta) / float64(ts.Sub(lastTick).Microseconds())) * 100

				mem, err := m.readMemoryStats(cgroupPath, buf)
				if err != nil {
					m.Log.Error("failed to read memory stats", "err", err)
				}

				m.DB.ExecContext(ctx,
					`INSERT INTO resource_usage (timestamp, container_id, cpu, memory) VALUES (NOW(), ?, ?, ?)`,
					id, perc, mem,
				)
			}

			lastTick = ts
			prev = cur
		}
	}
}

func (m *ResourcesMonitor) LastestUsage(id string) (float64, uint64, error) {
	var (
		perc float64
		mem  uint64
	)

	err := m.DB.QueryRow(
		"SELECT cpu, memory FROM resource_usage WHERE container_id = ? ORDER BY timestamp DESC LIMIT 1", id,
	).Scan(&perc, &mem)
	if err != nil {
		return 0, 0, err
	}

	return perc, mem, nil
}

func (m *ResourcesMonitor) readFile(cgroupPath string, sb []byte) ([]byte, error) {
	f, err := os.Open(cgroupPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	n, err := f.Read(sb[:100])
	if err != nil {
		return nil, err
	}

	return sb[:n], nil

}

func (m *ResourcesMonitor) readCgroupStats(cgroupPath string, sb []byte) (uint64, error) {
	sb, err := m.readFile(filepath.Join(cgroupPath, "cpu.stat"), sb)
	if err != nil {
		return 0, err
	}

	data := sb[11:]

	i := bytes.IndexByte(data, '\n')

	return strconv.ParseUint(string(data[:i]), 10, 64)
}

func (m *ResourcesMonitor) readMemoryStats(cgroupPath string, sb []byte) (uint64, error) {
	sb, err := m.readFile(filepath.Join(cgroupPath, "memory.current"), sb)
	if err != nil {
		return 0, err
	}

	sb = sb[:len(sb)-1] // skip the newline
	return strconv.ParseUint(string(sb), 10, 64)
}
