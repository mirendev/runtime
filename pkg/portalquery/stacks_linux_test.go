//go:build linux

package query

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDecodeSignedStackCaptureFailure(t *testing.T) {
	s := &stackCaptureState{spec: StackCapture{User: true, Kernel: true}}
	raw := make([]byte, s.recordSize())
	binary.NativeEndian.PutUint64(raw[:8], uint64(123)<<32|456)
	userError, kernelError := int64(-14), int64(-12)
	binary.NativeEndian.PutUint64(raw[8:16], uint64(userError))
	binary.NativeEndian.PutUint64(raw[16:24], uint64(kernelError))
	var event Event
	if err := s.decode(context.Background(), raw, &event); err != nil {
		t.Fatal(err)
	}
	if event.PID != 123 || event.TID != 456 || event.UserStack == nil || event.KernelStack == nil ||
		event.UserStack.CaptureErrorCode != -14 || event.KernelStack.CaptureErrorCode != -12 ||
		!strings.Contains(event.UserStack.Error, "-14") || !strings.Contains(event.KernelStack.Error, "-12") {
		t.Fatalf("signed helper failures were not preserved: %+v", event)
	}
}

func TestSyscallRawTracepointStackCapture(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to verify raw-tracepoint stack helper compatibility")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	complete := errors.New("captured syscall stacks")
	done := make(chan error, 1)
	go func() {
		done <- syscallEvents(ctx, MonitorRequest{Source: "syscalls", PID: uint32(os.Getpid()), Syscalls: []int{unix.SYS_GETPID}, Stacks: &StackCapture{User: true, Kernel: true, Depth: 16, Symbolize: true}}, func(event Event) error {
			if event.PID != uint32(os.Getpid()) || event.TID == 0 {
				return errors.New("incorrect captured pid/tid")
			}
			if event.UserStack == nil || len(event.UserStack.Frames) == 0 || event.UserStack.Frames[0].Address == "" {
				return errors.New("missing actual user stack frames")
			}
			if event.KernelStack == nil || len(event.KernelStack.Frames) == 0 || event.KernelStack.Frames[0].Address == "" {
				return errors.New("missing actual kernel stack frames")
			}
			named := false
			for _, frame := range event.KernelStack.Frames {
				named = named || frame.Name != ""
			}
			if !named {
				return errors.New("kernel stack addresses were not symbolized")
			}
			return complete
		})
	}()
	for {
		unix.Getpid()
		select {
		case err := <-done:
			if !errors.Is(err, complete) {
				t.Fatal(err)
			}
			return
		case <-ctx.Done():
			t.Fatal("timed out waiting for a syscall stack")
		default:
		}
	}
}

func TestGenericTracepointStackCapture(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root for live tracepoint stack capture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	complete := errors.New("captured generic tracepoint stacks")
	done := make(chan error, 1)
	spec := &TracepointFilter{Event: "raw_syscalls:sys_enter", Fields: []string{"common_pid", "id"}, Equals: map[string]string{"id": strconv.Itoa(unix.SYS_GETPID)}}
	go func() {
		done <- tracepointEvents(ctx, MonitorRequest{Source: "tracepoint", Tracepoint: spec, Stacks: &StackCapture{User: true, Kernel: true, Depth: 16}}, func(event Event) error {
			if event.Tracepoint.Fields["common_pid"] != json.Number(strconv.FormatUint(uint64(event.TID), 10)) || event.PID != uint32(os.Getpid()) {
				return errors.New("generic tracepoint pid/tid fields do not identify the triggering task")
			}
			if event.UserStack == nil || len(event.UserStack.Frames) == 0 || event.KernelStack == nil || len(event.KernelStack.Frames) == 0 {
				return errors.New("generic tracepoint did not return actual user and kernel frames")
			}
			return complete
		})
	}()
	for {
		unix.Getpid()
		select {
		case err := <-done:
			if !errors.Is(err, complete) {
				t.Fatal(err)
			}
			return
		case <-ctx.Done():
			t.Fatal("timed out waiting for a generic tracepoint stack")
		default:
		}
	}
}
