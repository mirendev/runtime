//go:build linux

package query

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestProbeSyscallCompletionLive(t *testing.T) {
	requireHostPIDNamespace(t)
	r, err := ParseMonitorQuery(fmt.Sprintf(`syscalls:completion where pid = %d and syscall = :getpid and duration_ns > 0 { @calls[pid] = {calls: count(), elapsed: sum(duration_ns)} } every 1s { emit @calls; clear @calls } after 3s { stop }`, os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	r, err = ResolveSyscallNames(r, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				unix.Getpid()
			}
		}
	}()
	result, err := aggregateEvents(ctx, r, syscallEvents)
	cancel()
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Windows) != 3 {
		t.Fatalf("missing reports: %+v", result)
	}
	for _, window := range result.Windows[1:] {
		if window.Table != "calls" || len(window.Rows) != 1 || len(window.Rows[0].Values) != 2 || string(window.Rows[0].Values[0]) == "0" || string(window.Rows[0].Values[1]) == "0" {
			t.Fatalf("missing live completion reductions: %+v", window)
		}
		t.Logf("live bucket calls=%s elapsed_ns=%s", window.Rows[0].Values[0], window.Rows[0].Values[1])
	}
}

func TestSyscallCompletionLive(t *testing.T) {
	requireHostPIDNamespace(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var events []Event
	done := make(chan error, 1)
	go func() {
		done <- syscallEvents(ctx, MonitorRequest{
			Source: "syscalls", PID: uint32(os.Getpid()), Phase: "completion",
			Syscalls: []int{unix.SYS_GETPID, unix.SYS_CLOSE, unix.SYS_READ},
			Stacks:   &StackCapture{User: true, Depth: 32},
		}, func(event Event) error {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
			return nil
		})
	}()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	probeTID := uint32(unix.Gettid())
	// Wait for an actual completion, not a guessed attachment delay.
	ready := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		unix.Getpid()
		mu.Lock()
		for _, event := range events {
			ready = ready || (event.TID == probeTID && event.Syscall == unix.SYS_GETPID)
		}
		mu.Unlock()
		if ready {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("collector startup: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("completion collector did not become ready")
	}
	_, _, _ = unix.RawSyscall(unix.SYS_GETPID, 0, 0, 0)
	_, _, _ = unix.RawSyscall(unix.SYS_CLOSE, ^uintptr(0), 0, 0)
	var pipe [2]int
	if err := unix.Pipe(pipe[:]); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pipe[0])
	defer unix.Close(pipe[1])
	go func() { time.Sleep(40 * time.Millisecond); unix.Write(pipe[1], []byte{1}) }()
	if n, err := unix.Read(pipe[0], make([]byte, 1)); err != nil || n != 1 {
		t.Fatalf("blocking read: %d, %v", n, err)
	}
	type concurrentProbe struct {
		tid     uint32
		length  int
		minimum time.Duration
	}
	results := make(chan concurrentProbe, 2)
	start := make(chan struct{})
	prepared := make(chan struct{}, 2)
	for _, length := range []int{3, 7} {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			var fds [2]int
			if err := unix.Pipe(fds[:]); err != nil {
				t.Errorf("concurrent pipe: %v", err)
				prepared <- struct{}{}
				results <- concurrentProbe{}
				return
			}
			defer unix.Close(fds[0])
			defer unix.Close(fds[1])
			tid := uint32(unix.Gettid())
			delay := time.Duration(length) * 20 * time.Millisecond
			go func() { <-start; time.Sleep(delay); unix.Write(fds[1], make([]byte, length)) }()
			prepared <- struct{}{}
			<-start
			if n, err := unix.Read(fds[0], make([]byte, length)); err != nil || n != length {
				t.Errorf("concurrent read: %d, %v", n, err)
			}
			results <- concurrentProbe{tid, length, delay - 20*time.Millisecond}
		}()
	}
	<-prepared
	<-prepared
	close(start)
	wanted := []concurrentProbe{<-results, <-results}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		blockedSeen := false
		for _, event := range events {
			blockedSeen = blockedSeen || (event.TID == probeTID && event.Syscall == unix.SYS_READ && event.DurationNS != nil && *event.DurationNS >= uint64(20*time.Millisecond))
		}
		for _, probe := range wanted {
			seen := false
			for _, event := range events {
				seen = seen || (event.TID == probe.tid && event.Syscall == unix.SYS_READ && event.ReturnValue != nil && *event.ReturnValue == int64(probe.length) && event.DurationNS != nil && *event.DurationNS >= uint64(probe.minimum))
			}
			blockedSeen = blockedSeen && seen
		}
		mu.Unlock()
		if blockedSeen {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	var success, failure, blocked bool
	var diagnostic bool
	for _, probe := range wanted {
		seen := false
		for _, event := range events {
			seen = seen || (event.TID == probe.tid && event.Syscall == unix.SYS_READ && event.ReturnValue != nil && *event.ReturnValue == int64(probe.length) && event.DurationNS != nil && *event.DurationNS >= uint64(probe.minimum))
		}
		if !seen || probe.tid == 0 {
			t.Fatalf("concurrent syscall was not paired by thread: %+v", probe)
		}
	}
	for _, event := range events {
		if event.Kind == "collection_stats" {
			diagnostic = event.Collection != nil
			continue
		}
		if event.TID != probeTID {
			continue
		}
		if event.Phase != "completion" || event.DurationNS == nil || event.ReturnValue == nil || event.Collection == nil {
			t.Fatalf("incomplete completion event: %+v", event)
		}
		switch event.Syscall {
		case unix.SYS_GETPID:
			success = success || (*event.ReturnValue == int64(os.Getpid()) && event.UserStack != nil && len(event.UserStack.Frames) != 0)
		case unix.SYS_CLOSE:
			failure = failure || *event.ReturnValue == -int64(unix.EBADF)
		case unix.SYS_READ:
			blocked = blocked || (*event.ReturnValue == 1 && *event.DurationNS >= uint64(20*time.Millisecond) && *event.DurationNS < uint64(2*time.Second))
		}
	}
	if !success || !failure || !blocked || !diagnostic {
		t.Fatalf("missing probes: success=%v failure=%v blocked=%v diagnostic=%v events=%d", success, failure, blocked, diagnostic, len(events))
	}
}
