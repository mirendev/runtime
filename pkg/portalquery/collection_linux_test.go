//go:build linux

package query

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

func TestEventProcessNameEnrichment(t *testing.T) {
	exe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	var got Event
	emit := enrichEventNames(func(e Event) error { got = e; return nil })
	for _, comm := range []string{"truncated-name", "kworker/16:1H"} {
		if err := emit(Event{PID: uint32(os.Getpid()), Name: comm}); err != nil {
			t.Fatal(err)
		}
		if got.Name != comm || got.NameGroup != comm || got.ProcessName != filepath.Base(exe) {
			t.Fatalf("lost full name or normalized a userspace worker: %+v", got)
		}
	}
	if err := emit(Event{PID: ^uint32(0), Name: "gone"}); err != nil || got.ProcessName != "" || got.NameGroup != "gone" {
		t.Fatalf("unavailable process name: %+v, %v", got, err)
	}
	fields := eventGroupFields(Event{Name: "original"}, nil)
	if fields["name_group"] != "original" {
		t.Fatal("unenriched event lost its task-name grouping")
	}
}

func TestUnifiedCgroupMembership(t *testing.T) {
	for _, tc := range []struct{ data, want string }{
		{"3:cpu:/wrong\n0::/apps/pg:writer\n", "/apps/pg:writer"},
		{"0::/\n", "/"}, {"3:cpu:/only-v1\n", ""}, {"0::relative\n", ""},
	} {
		if got := unifiedCgroupPath([]byte(tc.data)); got != tc.want {
			t.Fatalf("%q: %q", tc.data, got)
		}
	}
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) == 3 && parts[0] == "0" && parts[1] == "" {
			want = parts[2]
		}
	}
	if want == "" {
		t.Skip("requires unified cgroup membership")
	}
	var got Event
	emit := enrichEventNames(func(e Event) error { got = e; return nil })
	if err := emit(Event{PID: uint32(os.Getpid()), TID: uint32(unix.Gettid())}); err != nil || got.CgroupPath != want {
		t.Fatalf("membership: %+v, %v; want %s", got, err, want)
	}
	if err := emit(Event{PID: uint32(os.Getpid()), TID: ^uint32(0)}); err != nil || got.CgroupPath != "" {
		t.Fatalf("missing thread inherited process membership: %+v, %v", got, err)
	}
}

func TestEnrichedEventFiltersLive(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root eBPF")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, done := make(chan Event, 1), make(chan error, 1)
	go func() {
		done <- CollectEvents(ctx, MonitorRequest{Source: "syscalls", PID: uint32(os.Getpid()), Syscalls: []int{unix.SYS_GETPID}, EventFilters: map[string]string{"process_name": filepath.Base(exe)}}, func(e Event) error {
			if e.Kind != "collection_stats" {
				select {
				case got <- e:
				default:
				}
			}
			return nil
		})

	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			unix.Getpid()
		case e := <-got:
			if e.ProcessName != filepath.Base(exe) || e.CgroupPath == "" || e.PID != uint32(os.Getpid()) {
				t.Fatalf("missing enriched identity: %+v", e)
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			return
		case err := <-done:
			t.Fatalf("collector stopped: %v", err)
		case <-ctx.Done():
			t.Fatal("enriched filter discarded every event")
		}
	}
}

func TestCollectionKernelLossCounters(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root for forced BPF capture loss")
	}
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "ring_full", true: "stack_collision"}[collision], func(t *testing.T) {
			// Keep unrelated runtime threads' getpid calls out of the exact loss count.
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			tid := unix.Gettid()
			collection, err := newCollectionState()
			if err != nil {
				t.Fatal(err)
			}
			defer collection.close()
			insns := asm.Instructions{asm.Mov.Reg(asm.R6, asm.R1), asm.FnGetCurrentPidTgid.Call(), asm.Mov.Reg32(asm.R0, asm.R0), asm.JNE.Imm32(asm.R0, int32(tid), "exit"), asm.LoadMem(asm.R0, asm.R6, 8, asm.DWord), asm.JNE.Imm(asm.R0, int32(unix.SYS_GETPID), "exit")}
			if collision {
				m, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.StackTrace, KeySize: 4, ValueSize: 64, MaxEntries: 1})
				if err != nil {
					t.Fatal(err)
				}
				defer m.Close()
				s := &stackCaptureState{spec: StackCapture{User: true, Kernel: true, Depth: 8}, stackMap: m, collection: collection}
				insns = appendStackCapture(insns, asm.R6, -24, s)
			} else {
				m, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.RingBuf, MaxEntries: 4096})
				if err != nil {
					t.Fatal(err)
				}
				defer m.Close()
				insns = append(insns, asm.Mov.Imm(asm.R0, 0), asm.StoreMem(asm.RFP, -8, asm.R0, asm.DWord), asm.LoadMapPtr(asm.R1, m.FD()), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, -8), asm.Mov.Imm(asm.R3, 8), asm.Mov.Imm(asm.R4, 0), asm.FnRingbufOutput.Call())
				insns = appendRingLoss(insns, collection)
			}
			insns = append(insns, asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"), asm.Return())
			program, err := ebpf.NewProgram(&ebpf.ProgramSpec{Type: ebpf.RawTracepoint, License: "GPL", Instructions: insns})
			if err != nil {
				t.Fatal(err)
			}
			defer program.Close()
			attached, err := link.AttachRawTracepoint(link.RawTracepointOptions{Name: "sys_enter", Program: program})
			if err != nil {
				t.Fatal(err)
			}
			defer attached.Close()
			for i := 0; i < 600; i++ {
				unix.Getpid()
			}
			if err := attached.Close(); err != nil {
				t.Fatal(err)
			}
			stats, err := collection.snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if collision {
				if stats.StackCollisions == 0 || stats.StackCaptureFailures < stats.StackCollisions || stats.RingBufferDropped != 0 {
					t.Fatalf("collision counters: %+v", stats)
				}
				// 4096-byte ring, 8-byte header + 8-byte record, one slot
				// reserved by the producer/consumer mask: 255 successes.
			} else if stats.RingBufferDropped != 600-255 || stats.StackCaptureFailures != 0 {
				t.Fatalf("ring counters: %+v", stats)
			}
		})
	}
}
