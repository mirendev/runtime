package etcd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// etcd memory tuning derives etcd's runtime configuration from the amount of RAM
// on the node it runs on. etcd is a small co-tenant here: it should hold to roughly
// 10% of system RAM rather than grow to fill the node. We compute a per-node budget
//
//	B = max(0.10 * system_RAM, 100 MiB)
//
// and scale etcd's soft config (Go GC pressure via GOMEMLIMIT/GOGC, compaction/snapshot
// cadence, concurrent streams) off it. These are advisory only: no cgroup limit is
// imposed, so etcd is never OOM-killed by us; GOMEMLIMIT simply drives the Go runtime to
// GC harder as it approaches the budget.
//
// The one exception is --quota-backend-bytes, a hard knob whose breach takes etcd
// read-only cluster-wide. It is NOT scaled down by RAM: it is floored well above realistic
// keyspaces (see quotaFloorBytes) so the tuning only ever adds backend headroom on large
// nodes, never removes it on small ones. Operators can override it explicitly.
const (
	kib = 1024
	mib = 1024 * 1024
	gib = 1024 * 1024 * 1024

	// memoryFloorBytes is etcd's practical resident floor: below ~100 MiB it cannot
	// hold a working keyspace, so the budget never drops under this even on tiny nodes.
	memoryFloorBytes = 100 * mib

	// quotaFloorBytes is the minimum backend quota we ever set. --quota-backend-bytes is the
	// one hard knob whose breach raises a cluster-wide NOSPACE alarm (etcd goes read-only
	// until a manual compact/defrag/disarm), and keyspace tracks workload rather than the RAM
	// of the node etcd co-tenants on. etcd's built-in default (2 GiB) proved too small in
	// production — a ~711 MB live keyspace bloated to 2.1 GB and hit the quota — so we floor
	// at 4 GiB (~5.6x that live size, ~2x the bloated file). Flooring keeps the tuning purely
	// additive headroom on large nodes and never a write-outage regression on small ones.
	quotaFloorBytes = 4 * gib

	// quotaCapBytes caps the backend quota regardless of how large the node is. This is
	// etcd's own maximum supported backend size.
	quotaCapBytes = 8 * gib

	// Tier boundaries, expressed against the budget B. They line up with the standard
	// node sizes: nodes up to ~2 GB, ~8 GB, and ~16 GB of RAM respectively (B = RAM/10
	// above the floor), so a 2 GB node lands in the smallest tier, an 8 GB node in the
	// next, and so on.
	tierSmallMaxBytes  = 2 * gib / 10  // ~204.8 MiB
	tierMediumMaxBytes = 8 * gib / 10  // ~819.2 MiB
	tierLargeMaxBytes  = 16 * gib / 10 // ~1638.4 MiB
)

// etcdTuning is the set of etcd config values computed for a given node size.
type etcdTuning struct {
	SystemRAMBytes int64
	BudgetBytes    int64

	QuotaBackendBytes      int64
	GoMemLimitBytes        int64
	GOGC                   int
	AutoCompactionMode     string
	AutoCompactionReten    string
	SnapshotCount          int
	SnapshotCatchupEntries int
	MaxConcurrentStreams   int
	CompactionBatchLimit   int
}

// detectSystemRAMBytes reads total system memory from /proc/meminfo. It returns 0 when
// the value cannot be determined so callers fall back to the memory floor.
func detectSystemRAMBytes() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * kib // /proc/meminfo reports kB
	}
	return 0
}

// computeTuning derives etcd config from the node's RAM. A systemRAMBytes of 0 (unknown)
// yields the smallest tier via the budget floor. quotaOverride, when > 0, sets
// --quota-backend-bytes explicitly (operator-configured); otherwise the quota is the
// RAM-scaled value clamped to [quotaFloorBytes, quotaCapBytes].
func computeTuning(systemRAMBytes int64, quotaOverride int64) etcdTuning {
	budget := max(systemRAMBytes/10, memoryFloorBytes)

	quota := quotaOverride
	if quota <= 0 {
		quota = min(max(budget*30/100, quotaFloorBytes), quotaCapBytes)
	}

	streams := budget / (2 * mib)
	switch {
	case streams < 50:
		streams = 50
	case streams > 8000:
		streams = 8000
	}

	t := etcdTuning{
		SystemRAMBytes:       systemRAMBytes,
		BudgetBytes:          budget,
		QuotaBackendBytes:    quota,
		GoMemLimitBytes:      budget * 65 / 100,
		AutoCompactionMode:   "periodic",
		MaxConcurrentStreams: int(streams),
	}

	switch {
	case budget <= tierSmallMaxBytes:
		t.GOGC = 25
		t.AutoCompactionReten = "30m"
		t.SnapshotCount = 1000
		t.SnapshotCatchupEntries = 500
		t.CompactionBatchLimit = 100
	case budget <= tierMediumMaxBytes:
		t.GOGC = 50
		t.AutoCompactionReten = "1h"
		t.SnapshotCount = 5000
		t.SnapshotCatchupEntries = 2500
		t.CompactionBatchLimit = 500
	case budget <= tierLargeMaxBytes:
		t.GOGC = 75
		t.AutoCompactionReten = "2h"
		t.SnapshotCount = 10000
		t.SnapshotCatchupEntries = 5000
		t.CompactionBatchLimit = 1000
	default:
		t.GOGC = 100
		t.AutoCompactionReten = "3h"
		t.SnapshotCount = 10000
		t.SnapshotCatchupEntries = 5000
		t.CompactionBatchLimit = 1000
	}

	return t
}

// envVars renders the Go-runtime and etcd env for the container. GOMEMLIMIT/GOGC steer the
// Go runtime; the ETCD_* vars configure compaction. The bbolt freelist type is kept from the
// prior fixed config.
func (t etcdTuning) envVars() []string {
	return []string{
		fmt.Sprintf("GOMEMLIMIT=%dB", t.GoMemLimitBytes),
		fmt.Sprintf("GOGC=%d", t.GOGC),
		fmt.Sprintf("ETCD_AUTO_COMPACTION_MODE=%s", t.AutoCompactionMode),
		fmt.Sprintf("ETCD_AUTO_COMPACTION_RETENTION=%s", t.AutoCompactionReten),
		"ETCD_EXPERIMENTAL_BACKEND_BBOLT_FREELIST_TYPE=map",
	}
}

// signature is a stable fingerprint of the tuning-derived spec (env + args). It is
// persisted in etcdState so a restart on a node whose RAM has changed regenerates the
// container instead of reusing a spec built for the old budget.
func (t etcdTuning) signature() string {
	return strings.Join(append(t.envVars(), t.args()...), " ")
}

// args renders the etcd command-line flags for the tuning. Flag names target etcd v3.5,
// where snapshot-catchup-entries and compaction-batch-limit are experimental-prefixed
// (both graduated to stable names in 3.6).
func (t etcdTuning) args() []string {
	return []string{
		"--quota-backend-bytes", strconv.FormatInt(t.QuotaBackendBytes, 10),
		"--snapshot-count", strconv.Itoa(t.SnapshotCount),
		"--experimental-snapshot-catchup-entries", strconv.Itoa(t.SnapshotCatchupEntries),
		"--max-concurrent-streams", strconv.Itoa(t.MaxConcurrentStreams),
		"--experimental-compaction-batch-limit", strconv.Itoa(t.CompactionBatchLimit),
	}
}
