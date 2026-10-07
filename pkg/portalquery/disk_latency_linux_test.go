//go:build linux

package query

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"golang.org/x/sys/unix"
)

func TestDiskRequestRWBS(t *testing.T) {
	// Deliberately asymmetric bit positions: never assume a kernel's layout.
	b := requestFlagBits{valid: true, sync: 12, meta: 15, fua: 18, preflush: 20, rahead: 10}
	for _, tc := range []struct {
		op    byte
		flags uint32
		want  string
	}{
		{1, 1<<20 | 1<<18 | 1<<12 | 1<<15 | 1<<30, "FWFSM"},
		{0, 1<<10 | 1<<12, "RAS"},
		{2, 0, "F"},
		{3, 1 << 12, "DS"},
		{5, 1 << 15, "DEM"},
		{255, 0, "N"},
	} {
		if got := requestRWBS(tc.op, tc.flags, b); got != tc.want {
			t.Fatalf("operation=%d flags=%x: %s, want %s", tc.op, tc.flags, got, tc.want)
		}
	}
}

func TestDiskCompletionDecoderAndInstructions(t *testing.T) {
	raw := make([]byte, 80)
	binary.NativeEndian.PutUint64(raw[0:8], uint64(12)<<32|34)
	copy(raw[8:24], "issuer")
	binary.NativeEndian.PutUint32(raw[32:36], 0x800001)
	binary.NativeEndian.PutUint64(raw[40:48], 1234)
	binary.NativeEndian.PutUint32(raw[48:52], 16)
	raw[52] = 1
	bits := requestFlagBits{valid: true, sync: 10, meta: 13, fua: 21, preflush: 25, rahead: 17}
	flags := uint32(1<<bits.sync | 1<<bits.fua | 1<<bits.preflush)
	binary.NativeEndian.PutUint32(raw[60:64], flags)
	binary.NativeEndian.PutUint64(raw[64:72], 98765)
	binary.NativeEndian.PutUint64(raw[72:80], 999)
	e, err := decodeDiskCompletionRecordWithFlags(raw, bits)
	if err != nil || e.PID != 12 || e.TID != 34 || e.Name != "issuer" || e.Phase != "completion" || e.Disk.Device != 0x800001 || e.Disk.Sector != 1234 || e.Disk.Sectors != 16 || e.Disk.Operation != "write" {
		t.Fatalf("decode: %+v, %v", e, err)
	}
	if e.Disk.DurationNS == nil || *e.Disk.DurationNS != 999 || e.Disk.IOCgroupID == nil || *e.Disk.IOCgroupID != 98765 {
		t.Fatalf("duration: %+v", e.Disk)
	}
	if e.Disk.RWBS != "FWFS" || e.Disk.RequestFlags == nil || *e.Disk.RequestFlags != flags {
		t.Fatalf("flags: %+v", e.Disk)
	}
	if _, err := decodeDiskCompletionRecord(raw[:79]); err == nil {
		t.Fatal("accepted truncated completion")
	}
	l := diskBTFLayout{issueArg: 0, completeArg: 0, rqQ: 8, rqSector: 16, rqBytes: 24, rqFlags: 28, qDisk: 32, diskMajor: 40, diskFirstMinor: 44}
	for _, insns := range []asm.Instructions{diskIssueCompletionInstructions(l, 10, 0, nil), diskIssueCompletionInstructions(l, 0, 11, nil), diskCompleteInstructions(l, 10, 11, nil)} {
		if err := insns.Marshal(&bytes.Buffer{}, binary.LittleEndian); err != nil {
			t.Fatalf("invalid instructions: %v", err)
		}
	}
}

func TestDiskCompletionKernelBTF(t *testing.T) {
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); err != nil {
		t.Skip("kernel BTF unavailable")
	}
	l, err := loadDiskBTFLayout()
	if err != nil {
		t.Fatalf("running kernel unsupported: %v", err)
	}
	if l.issueArg < 0 || l.completeArg < 0 {
		t.Fatalf("invalid layout: %+v", l)
	}
}

func TestDiskCompletionProgramsLoad(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to load eBPF programs")
	}
	l, err := loadDiskBTFLayout()
	if err != nil {
		t.Fatal(err)
	}
	pending, err := ebpf.NewMap(&ebpf.MapSpec{Name: "disk_test_pending", Type: ebpf.Hash, KeySize: 8, ValueSize: 72, MaxEntries: 16})
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	events, err := ebpf.NewMap(&ebpf.MapSpec{Name: "disk_test_events", Type: ebpf.RingBuf, MaxEntries: 4096})
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	c, err := newCollectionState()
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	for name, insns := range map[string]asm.Instructions{
		"issue":    diskIssueCompletionInstructions(l, pending.FD(), 0, c),
		"entry":    diskIssueCompletionInstructions(l, 0, events.FD(), c),
		"complete": diskCompleteInstructions(l, pending.FD(), events.FD(), c),
	} {
		program, err := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "disk_test_" + name, Type: ebpf.RawTracepoint, License: "GPL", Instructions: insns})
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		program.Close()
	}
}

// Execute the actual completion reducer in the kernel with controlled request
// keys/byte counts. This distinguishes pointer pairing from dev/sector pairing
// and verifies partial completions, final deletion, errors, and loss counters.
func TestDiskCompletionPairingLive(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root raw tracepoints")
	}
	pending, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.Hash, KeySize: 8, ValueSize: 72, MaxEntries: 16})
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	events, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.RingBuf, MaxEntries: 4096})
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	c, err := newCollectionState()
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	reader, err := ringbuf.NewReader(events)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	seed := func(key uint64, pid uint32, age time.Duration) {
		t.Helper()
		var now unix.Timespec
		if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &now); err != nil {
			t.Fatal(err)
		}
		value := make([]byte, 72)
		binary.NativeEndian.PutUint64(value[0:8], uint64(pid)<<32|uint64(pid+1))
		copy(value[8:24], "issuer")
		binary.NativeEndian.PutUint64(value[24:32], uint64(now.Nano()-int64(age)))
		binary.NativeEndian.PutUint32(value[32:36], 0x10300123)
		binary.NativeEndian.PutUint32(value[36:40], 4096)
		binary.NativeEndian.PutUint64(value[40:48], 987)
		binary.NativeEndian.PutUint32(value[48:52], 8)
		value[52] = 1
		binary.NativeEndian.PutUint64(value[64:72], uint64(pid)*100)
		if err := pending.Update(key, value, ebpf.UpdateAny); err != nil {
			t.Fatal(err)
		}
	}
	trigger := func(key, completed, status int32) {
		t.Helper()
		i := diskCompleteInstructions(diskBTFLayout{}, pending.FD(), events.FD(), c)
		// Replace only raw argument loads; retain the production pairing logic.
		i[0], i[1], i[2] = asm.Mov.Imm(asm.R6, key), asm.Mov.Imm(asm.R7, completed), asm.Mov.Imm(asm.R8, status)
		prefix := asm.Instructions{asm.LoadMem(asm.R0, asm.R1, 8, asm.DWord), asm.JNE.Imm(asm.R0, unix.SYS_GETPID, "exit"), asm.FnGetCurrentPidTgid.Call(), asm.JNE.Imm32(asm.R0, int32(unix.Gettid()), "exit")}
		p, err := ebpf.NewProgram(&ebpf.ProgramSpec{Type: ebpf.RawTracepoint, License: "GPL", Instructions: append(prefix, i...)})
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		l, err := link.AttachRawTracepoint(link.RawTracepointOptions{Name: "sys_enter", Program: p})
		if err != nil {
			t.Fatal(err)
		}
		unix.Getpid()
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
	read := func() Event {
		t.Helper()
		reader.SetDeadline(time.Now().Add(time.Second))
		r, err := reader.Read()
		if err != nil {
			t.Fatal(err)
		}
		e, err := decodeDiskCompletionRecord(r.RawSample)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	seed(101, 11, 100*time.Millisecond)
	seed(202, 22, 400*time.Millisecond) // same sector/device, different request
	trigger(101, 1024, 0)
	var value [72]byte
	if err := pending.Lookup(uint64(101), &value); err != nil || binary.NativeEndian.Uint32(value[36:40]) != 3072 {
		t.Fatalf("partial: %v %+v", err, value)
	}
	reader.SetDeadline(time.Now().Add(10 * time.Millisecond))
	if _, err := reader.Read(); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("partial emitted event: %v", err)
	}
	trigger(202, 4096, 0)
	e := read()
	if e.PID != 22 || e.TID != 23 || *e.Disk.DurationNS < uint64(400*time.Millisecond) || e.Disk.Device != 0x10300123 || *e.Disk.Status != 0 || e.Disk.IOCgroupID == nil || *e.Disk.IOCgroupID != 2200 {
		t.Fatalf("wrong second request: %+v %+v", e, e.Disk)
	}
	trigger(101, 3072, 0)
	e = read()
	if e.PID != 11 || e.Disk.Sectors != 8 || *e.Disk.DurationNS < uint64(100*time.Millisecond) {
		t.Fatalf("wrong first request: %+v %+v", e, e.Disk)
	}
	if err := pending.Lookup(uint64(101), &value); !errors.Is(err, ebpf.ErrKeyNotExist) {
		t.Fatalf("completed request retained: %v", err)
	}
	trigger(101, 1, 0)
	stats, err := c.snapshot()
	if err != nil || stats.UnmatchedExits != 1 || stats.BlockCompletions != 4 || stats.BlockIssues != 0 || stats.BlockReissues != 0 || stats.BlockPartialCompletions != 1 || stats.BlockFinalCompletions != 2 {
		t.Fatalf("unmatched: %+v %v", stats, err)
	}
	seed(303, 33, 20*time.Millisecond)
	trigger(303, 1024, 5)
	if err := pending.Lookup(uint64(303), &value); err != nil || binary.NativeEndian.Uint32(value[36:40]) != 3072 {
		t.Fatalf("partial error removed request: %v", err)
	}
	trigger(303, 3072, 0)
	e = read()
	if e.PID != 33 || *e.Disk.Status != 5 {
		t.Fatalf("error completion: %+v %+v", e, e.Disk)
	}
	if err := pending.Lookup(uint64(303), &value); !errors.Is(err, ebpf.ErrKeyNotExist) {
		t.Fatalf("failed request retained: %v", err)
	}
	seed(404, 44, 10*time.Millisecond)
	if err := pending.Lookup(uint64(404), &value); err != nil {
		t.Fatal(err)
	}
	binary.NativeEndian.PutUint32(value[36:40], 0)
	binary.NativeEndian.PutUint32(value[48:52], 0)
	value[52] = 2
	if err := pending.Update(uint64(404), value, ebpf.UpdateExist); err != nil {
		t.Fatal(err)
	}
	trigger(404, 0, 0)
	e = read()
	if e.PID != 44 || e.Disk.Operation != "flush" || e.Disk.Sectors != 0 || *e.Disk.Status != 0 {
		t.Fatalf("zero-byte flush: %+v %+v", e, e.Disk)
	}
}

func TestDiskCompletionEventsLive(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root block tracepoints")
	}
	for _, phase := range []string{"entry", "completion"} {
		t.Run(phase, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "block-latency")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got := make(chan Event, 256)
			done := make(chan error, 1)
			go func() {
				done <- diskEvents(ctx, MonitorRequest{Source: "disk", Phase: phase}, func(e Event) error {
					select {
					case got <- e:
					default:
					}
					return nil
				})
			}()
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			var found Event
			for found.Disk == nil {
				select {
				case <-ticker.C:
					if _, err := f.WriteAt(bytes.Repeat([]byte{0x5a}, 65536), 0); err != nil {
						t.Fatal(err)
					}
					if err := f.Sync(); err != nil {
						t.Fatal(err)
					}
				case e := <-got:
					if e.Disk != nil && e.Disk.Operation == "write" {
						found = e
					}
				case err := <-done:
					t.Fatalf("block collector stopped: %v", err)
				case <-deadline.C:
					t.Fatal("no completed block request after repeated writes/fsync")
				}
			}
			if (phase == "completion" && (found.Phase != "completion" || found.Disk.DurationNS == nil || *found.Disk.DurationNS == 0 || found.Disk.Status == nil)) || found.Disk.RequestFlags == nil || found.Disk.RWBS == "" || found.Disk.DeviceName == "" || found.Name == "" {
				t.Fatalf("incomplete event: %+v %+v", found, found.Disk)
			}
			if phase == "entry" && (found.Disk.DurationNS != nil || found.Disk.Status != nil) {
				t.Fatalf("completion-only metadata on issue: %+v", found.Disk)
			}
			l, err := loadDiskBTFLayout()
			if err != nil || len(l.ioCgroupOffsets) != 6 {
				t.Fatalf("test kernel lacks expected block cgroup layout: %+v, %v", l, err)
			}
			if found.Disk.IOCgroupID == nil || *found.Disk.IOCgroupID == 0 {
				t.Fatalf("write bio ownership not captured: %+v", found.Disk)
			}
			if found.Disk.IOCgroupPath != "" {
				st, err := os.Stat("/sys/fs/cgroup" + found.Disk.IOCgroupPath)
				if err != nil || st.Sys().(*syscall.Stat_t).Ino != *found.Disk.IOCgroupID {
					t.Fatalf("charged ID/path mismatch: %+v, %v", found.Disk, err)
				}
			}
			major, minor := found.Disk.Device>>20, found.Disk.Device&0xfffff
			if _, err := os.Stat(fmt.Sprintf("/sys/dev/block/%d:%d", major, minor)); err != nil {
				t.Fatalf("invalid kernel device encoding: %+v: %v", found.Disk, err)
			}
			t.Logf("observed device=%d:%d issuer=%s io_cgroup_id=%d io_cgroup_path=%q error=%q", major, minor, found.Name, *found.Disk.IOCgroupID, found.Disk.IOCgroupPath, found.Disk.IOCgroupError)
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
