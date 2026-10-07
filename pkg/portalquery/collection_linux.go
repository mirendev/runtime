//go:build linux

package query

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
)

const taskIdentitySize = 24 // pid/tid plus 16-byte current task comm
const (
	collectionRingDropped = iota
	collectionStackFailed
	collectionStackCollision
	collectionPairingFailed
	collectionUnmatchedExit
	// Block counters are callback-scoped diagnostics for the completion
	// collector, rather than counts of unique requests.
	collectionBlockIssues
	collectionBlockCompletions
	collectionBlockReissues
	collectionBlockPartialCompletions
	collectionBlockFinalCompletions
	collectionCounterCount
)

type collectionState struct{ counters *ebpf.Map }

func newCollectionState() (*collectionState, error) {
	m, err := ebpf.NewMap(&ebpf.MapSpec{Name: "portal_loss", Type: ebpf.Array, KeySize: 4, ValueSize: 8, MaxEntries: collectionCounterCount})
	if err != nil {
		return nil, fmt.Errorf("create collection counters: %w", err)
	}
	return &collectionState{counters: m}, nil
}

func (s *collectionState) close() {
	if s != nil {
		s.counters.Close()
	}
}

// Uses a reserved scratch slot below all event records; helpers preserve R6–R9.
func appendCollectionCounter(insns asm.Instructions, s *collectionState, counter int, label string) asm.Instructions {
	if s == nil {
		return insns
	}
	return append(insns,
		asm.StoreImm(asm.RFP, -480, int64(counter), asm.Word),
		asm.LoadMapPtr(asm.R1, s.counters.FD()),
		asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, -480),
		asm.FnMapLookupElem.Call(), asm.JEq.Imm(asm.R0, 0, label),
		asm.Mov.Imm(asm.R1, 1), asm.StoreXAdd(asm.R0, asm.R1, asm.DWord),
		asm.Mov.Imm(asm.R0, 0).WithSymbol(label),
	)
}

func appendRingLoss(insns asm.Instructions, s *collectionState) asm.Instructions {
	if s == nil {
		return insns
	}
	insns = append(insns, asm.JSGE.Imm(asm.R0, 0, "ring_loss_done"))
	return appendCollectionCounter(insns, s, collectionRingDropped, "ring_loss_done")
}

func appendTaskIdentity(insns asm.Instructions, offset int16) asm.Instructions {
	return append(insns,
		asm.FnGetCurrentPidTgid.Call(), asm.StoreMem(asm.RFP, offset, asm.R0, asm.DWord),
		asm.Mov.Imm(asm.R0, 0), asm.StoreMem(asm.RFP, offset+8, asm.R0, asm.DWord), asm.StoreMem(asm.RFP, offset+16, asm.R0, asm.DWord),
		asm.Mov.Reg(asm.R1, asm.RFP), asm.Add.Imm(asm.R1, int32(offset+8)),
		asm.Mov.Imm(asm.R2, 16), asm.FnGetCurrentComm.Call(),
	)
}

func decodeTaskIdentity(raw []byte, event *Event) {
	pidTID := binary.NativeEndian.Uint64(raw[:8])
	event.PID, event.TID = uint32(pidTID>>32), uint32(pidTID)
	event.Name = string(bytes.TrimRight(raw[8:24], "\x00"))
}

func (s *collectionState) snapshot() (*CollectionStats, error) {
	values := make([]uint64, collectionCounterCount)
	for i := range values {
		key := uint32(i)
		if err := s.counters.Lookup(&key, &values[i]); err != nil {
			return nil, err
		}
	}
	return &CollectionStats{
		RingBufferDropped: values[collectionRingDropped], StackCaptureFailures: values[collectionStackFailed],
		StackCollisions: values[collectionStackCollision], PairingFailures: values[collectionPairingFailed],
		UnmatchedExits: values[collectionUnmatchedExit], BlockIssues: values[collectionBlockIssues],
		BlockCompletions: values[collectionBlockCompletions], BlockReissues: values[collectionBlockReissues],
		BlockPartialCompletions: values[collectionBlockPartialCompletions], BlockFinalCompletions: values[collectionBlockFinalCompletions],
	}, nil
}

func (s *collectionState) report(event *Event) error {
	stats, err := s.snapshot()
	if err == nil {
		event.Collection = stats
	}
	return err
}

// Enrichment is deliberately separate from kernel-time task comm. The bounded
// one-second cache avoids procfs I/O per event; names are best-effort, not an
// atomic identity assertion, and may lag exec/exit/PID reuse by one second.
func enrichEventNames(emit func(Event) error) func(Event) error {
	type entry struct {
		name    string
		cgroup  string
		kernel  bool
		expires time.Time
	}
	cache := make(map[uint32]entry)
	cgroups := make(map[uint64]entry)
	return func(event Event) error {
		if event.Kind == "collection_stats" {
			return emit(event)
		}
		event.NameGroup = event.Name
		if event.PID != 0 {
			now := time.Now()
			e, ok := cache[event.PID]
			if !ok || !now.Before(e.expires) {
				e = entry{expires: now.Add(time.Second)}
				if verifyHostPID(event.PID) == nil {
					if path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", event.PID)); err == nil {
						e.name = filepath.Base(strings.TrimSuffix(path, " (deleted)"))
					}
					if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", event.PID)); err == nil {
						if end := strings.LastIndex(string(data), ")"); end >= 0 {
							fields := strings.Fields(string(data)[end+1:])
							if len(fields) > 6 {
								flags, _ := strconv.ParseUint(fields[6], 10, 64)
								e.kernel = flags&0x00200000 != 0
							}
						}
					}
				}
				if len(cache) >= 1024 {
					clear(cache)
				}
				cache[event.PID] = e
			}
			event.ProcessName = e.name
			if e.kernel && strings.HasPrefix(event.Name, "kworker/") {
				event.NameGroup = "kworker"
			}
			tid := event.TID
			if tid == 0 {
				tid = event.PID
			}
			key := uint64(event.PID)<<32 | uint64(tid)
			cg, ok := cgroups[key]
			if !ok || !now.Before(cg.expires) {
				cg = entry{expires: now.Add(time.Second)}
				if verifyHostPID(event.PID) == nil {
					if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/cgroup", event.PID, tid)); err == nil {
						cg.cgroup = unifiedCgroupPath(data)
					}
				}
				if len(cgroups) >= 1024 {
					clear(cgroups)
				}
				cgroups[key] = cg
			}
			event.CgroupPath = cg.cgroup
		}
		return emit(event)
	}
}

// Procfs membership is per thread, and relative to the server's cgroup
// namespace. Never guess a v2 path from one of the v1 controller hierarchies.
func unifiedCgroupPath(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "0::/") {
			return strings.TrimPrefix(line, "0::")
		}
	}
	return ""
}
