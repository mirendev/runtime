package query

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProcessTransitionsAndFilters(t *testing.T) {
	before := map[uint32]processState{
		17: {started: 1000, name: "old"},
		23: {started: 2000, name: "steady"},
		31: {started: 3000, name: "gone"},
	}
	after := map[uint32]processState{
		17: {started: 4000, name: "reused"},
		23: {started: 2000, name: "steady"},
		42: {started: 5000, name: "new"},
	}
	events := processTransitions(before, after)
	want := map[string]bool{"17/old/exit": true, "17/reused/start": true, "31/gone/exit": true, "42/new/start": true}
	if len(events) != len(want) {
		t.Fatalf("transitions: %+v", events)
	}
	for _, event := range events {
		key := strings.Join([]string{strconv.FormatUint(uint64(event.PID), 10), event.Process.Name, event.Process.Action}, "/")
		if !want[key] || event.Time.IsZero() {
			t.Fatalf("unexpected transition: %+v", event)
		}
		delete(want, key)
	}
	request, err := ParseMonitorQuery("process where pid = 17 and name = reused and action = start")
	if err != nil {
		t.Fatal(err)
	}
	if !request.Matches(Event{PID: 17, Process: &ProcessEvent{Name: "reused", Action: "start"}}) ||
		request.Matches(Event{PID: 17, Process: &ProcessEvent{Name: "old", Action: "exit"}}) ||
		request.Matches(Event{PID: 42, Process: &ProcessEvent{Name: "reused", Action: "start"}}) ||
		request.Matches(Event{PID: 17, Syscall: 1}) {
		t.Fatal("process filters did not select only the requested lifecycle event")
	}
	encoded, err := json.Marshal(Event{PID: 17, Process: &ProcessEvent{Name: "reused", Action: "start"}})
	if err != nil || !strings.Contains(string(encoded), `"process":`) || strings.Contains(string(encoded), `"syscall":`) {
		t.Fatalf("process JSON: %s, %v", encoded, err)
	}
	for _, bad := range []MonitorRequest{
		{Source: "process", Syscalls: []int{1}}, {Source: "process", Packet: &PacketFilter{}},
		{Source: "process", Process: &ProcessFilter{Action: "restart"}},
		{Source: "syscalls", Process: &ProcessFilter{Name: "worker"}},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("invalid process request accepted: %+v", bad)
		}
	}
}

func TestProcessNameGlobs(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"worker*", "worker-agent", true}, {"worker*", "agent-worker", false},
		{"*worker", "agent-worker", true}, {"*worker", "worker-agent", false},
		{"worker", "worker", true}, {"worker", "workers", false},
	} {
		r := MonitorRequest{Source: "process", Process: &ProcessFilter{Name: tc.pattern}}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		if got := r.Matches(Event{Process: &ProcessEvent{Name: tc.name, Action: "start"}}); got != tc.want {
			t.Errorf("%q matches %q = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestProcessSnapshotLifecycle(t *testing.T) {
	if os.Getenv("PORTAL_PROCESS_TEST_CHILD") == "1" {
		time.Sleep(10 * time.Second)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	before, err := snapshotProcesses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before[uint32(os.Getpid())].started == 0 {
		t.Fatal("snapshot did not include the current process")
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcessSnapshotLifecycle$")
	cmd.Env = append(os.Environ(), "PORTAL_PROCESS_TEST_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	var after map[uint32]processState
	pid := uint32(cmd.Process.Pid)
	for ctx.Err() == nil {
		after, err = snapshotProcesses(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if after[pid].started != 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if after[pid].started == 0 {
		t.Fatal("spawned process was not visible")
	}
	if !hasProcessTransition(processTransitions(before, after), pid, "start") {
		t.Fatal("spawned process did not produce a start transition")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	ended, err := snapshotProcesses(ctx)
	if err != nil || !hasProcessTransition(processTransitions(after, ended), pid, "exit") {
		t.Fatalf("exited process transition missing: %v, %v", err, ended[pid])
	}
}

func hasProcessTransition(events []Event, pid uint32, action string) bool {
	for _, event := range events {
		if event.PID == pid && event.Process.Action == action {
			return true
		}
	}
	return false
}

func TestProcessSnapshotMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := MonitorRequest{Source: "process", Mode: "snapshot", PID: uint32(os.Getpid())}
	snapshot, err := processSnapshot(ctx, r)
	if err != nil || len(snapshot.Processes) != 1 || snapshot.Processes[0].PID != r.PID {
		t.Fatalf("current process snapshot: %+v, %v", snapshot.Processes, err)
	}
	info := snapshot.Processes[0]
	if runtime.GOOS == "linux" {
		if info.CPUSeconds == nil || *info.CPUSeconds < 0 || info.RSSBytes == nil || *info.RSSBytes == 0 || info.VMSBytes == nil || *info.VMSBytes < *info.RSSBytes ||
			info.Threads == nil || *info.Threads <= 0 || info.User == "" || info.State == "" || !strings.Contains(info.CommandLine, os.Args[0]) {
			t.Fatalf("current process is missing supported metrics: %+v", info)
		}
	}
	r.Process = &ProcessFilter{Name: "no-such-portal-process"}
	filtered, err := processSnapshot(ctx, r)
	if err != nil || len(filtered.Processes) != 0 {
		t.Fatalf("metric collection lost name filter: %+v, %v", filtered.Processes, err)
	}
	cancel()
	if _, err := processSnapshot(ctx, r); err != context.Canceled {
		t.Fatalf("canceled process snapshot: %v", err)
	}
}

func TestProcessMetricsJSON(t *testing.T) {
	var seconds float64
	var rss uint64
	var threads int32
	data, err := json.Marshal(ProcessInfo{CPUSeconds: &seconds, RSSBytes: &rss, Threads: &threads})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cpu_seconds", "rss_bytes", "threads"} {
		if string(fields[name]) != "0" {
			t.Fatalf("available zero %s was omitted: %s", name, data)
		}
	}
	for _, name := range []string{"vms_bytes", "user", "state", "command_line"} {
		if _, ok := fields[name]; ok {
			t.Fatalf("unavailable %s was fabricated: %s", name, data)
		}
	}
}
