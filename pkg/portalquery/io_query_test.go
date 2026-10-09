package query

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func TestIOQueryFields(t *testing.T) {
	for _, q := range []string{
		"syscalls where paths = true and phase = completion and syscall in (:fsync,:fdatasync) count, sum(duration_ns) over 30s by pid, file.path",
		"disk where phase = completion count, avg(duration_ns), percentile(duration_ns,95) over 30s by device, pid, name",
	} {
		if _, err := ParseMonitorQuery(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, q := range []string{
		"syscalls count over 1s by file.path", "disk avg(duration_ns) over 1s", "syscalls where paths = maybe", "process where phase = completion", "disk where phase = unknown",
	} {
		if _, err := ParseMonitorQuery(q); err == nil {
			t.Fatalf("accepted %s", q)
		}
	}
	if err := (MonitorRequest{Source: "disk", Paths: true}).Validate(); err == nil {
		t.Fatal("disk paths accepted")
	}
	d := uint64(321)
	fields := eventGroupFields(Event{Disk: &DiskEvent{Device: 11, DurationNS: &d}, PID: 12}, nil)
	if fields["duration_ns"] != d || fields["pid"] != uint32(12) {
		t.Fatalf("disk fields: %+v", fields)
	}
	fields = eventGroupFields(Event{File: &SyscallFile{FD: 7, Path: "/data/test"}}, nil)
	if fields["file.path"] != "/data/test" || fields["file.fd"] != int32(7) {
		t.Fatalf("file fields: %+v", fields)
	}
}

func TestEventStringFilters(t *testing.T) {
	query := "disk where device_name = nvme0n1 and name = worker* and name_group = worker and process_name = writer and rwbs = *SM and cgroup.path = /apps/pg* and io.cgroup.path = /apps/logs*"
	r, err := ParseMonitorQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	e := Event{Name: "worker-1", NameGroup: "worker", ProcessName: "writer", CgroupPath: "/apps/pg-17", Disk: &DiskEvent{DeviceName: "nvme0n1", RWBS: "WSM", IOCgroupPath: "/apps/logs-3"}}
	if !r.Matches(e) {
		t.Fatal("all matching filters rejected")
	}
	for field := range r.EventFilters {
		wrong := r
		wrong.EventFilters = map[string]string{field: "absent"}
		if wrong.Matches(e) {
			t.Fatalf("ignored filter %s", field)
		}
	}
	if r.Matches(Event{Disk: &DiskEvent{}}) || !r.Matches(Event{Kind: "collection_stats"}) {
		t.Fatal("missing metadata matched or diagnostics discarded")
	}
	for _, query := range []string{
		"syscalls where process_name = writer*", "syscalls where name = worker", "tracepoint where event = sched:sched_switch and fields in (prev_pid) and name_group = kworker",
		"syscalls where paths = true and file.depth = 3 count over 1s by file.dir, cgroup.path",
		"disk count, sum(sectors) over 30s by device_name, io.cgroup.id, io.cgroup.path, io.cgroup.error",
	} {
		if _, err := ParseMonitorQuery(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for _, query := range []string{
		"syscalls where device_name = disk", "tracepoint where event = sched:sched_switch and fields in (prev_pid) and rwbs = W",
		"syscalls where io.cgroup.path = /", "tracepoint where event = sched:sched_switch and fields in (prev_pid) count over 1s by io.cgroup.path",
		"disk where process_name = ''", "disk where name = '*worker*'", "disk where file.depth = 0 count over 1s",
		"syscalls where file.depth = 3 count over 1s", "syscalls where file.depth = 0 count over 1s", "syscalls where paths = true and file.depth = 33 count over 1s",
	} {
		if _, err := ParseMonitorQuery(query); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}

}

func TestDirectoryAggregationDepth(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		events := []Event{
			{File: &SyscallFile{Path: "/var/lib/datadb/part-1/bloom"}},
			{File: &SyscallFile{Path: "/var/lib/datadb/part-2/index"}},
			{File: &SyscallFile{Path: "/var/lib/other/part-3/index"}},
			{File: &SyscallFile{Error: "closed"}},
		}
		for _, depth := range []int{0, 3, 32} {
			r := MonitorRequest{Source: "syscalls", Mode: "aggregate", Paths: true, FileDepth: depth, Aggregation: &AggregationRequest{Window: time.Second, GroupBy: []string{"file.dir"}}}
			got, err := aggregateEvents(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
				for _, e := range events {
					if err := emit(e); err != nil {
						return err
					}
				}
				<-ctx.Done()
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			counts := map[string]uint64{}
			for _, row := range got.Aggregation.Counts {
				counts[string(row.Group["file.dir"])] = row.Count
			}
			want := map[string]uint64{`"/var/lib/datadb/part-1"`: 1, `"/var/lib/datadb/part-2"`: 1, `"/var/lib/other/part-3"`: 1, "null": 1}
			if depth == 3 {
				want = map[string]uint64{`"/var/lib/datadb"`: 2, `"/var/lib/other"`: 1, "null": 1}
			}
			if !reflect.DeepEqual(counts, want) {
				t.Fatalf("depth=%d: %v, want %v", depth, counts, want)
			}
		}
		data, err := json.Marshal(events)
		if err != nil || events[0].File.Path != "/var/lib/datadb/part-1/bloom" {
			t.Fatalf("projection altered events: %s, %v", data, err)
		}
	})
}

func TestLiteralAndMultipleStackAnchors(t *testing.T) {
	r, err := ParseMonitorQuery(`syscalls where stacks = user and user.stack.offsets = false and user.stack.from in ("os.(*File).Sync", '*Fdatasync') count over 1s by user.stack`)
	if err != nil {
		t.Fatal(err)
	}
	frames := []SymbolFrame{{Module: "m", Name: "runtime.wrapper"}, {Module: "m", Name: "unix.Fdatasync"}, {Module: "m", Name: "os.(*File).Sync"}, {Module: "m", Name: "caller"}}
	if got := (CapturedStack{Frames: frames}).key(r.Stacks.UserShape); got != "m:unix.Fdatasync;m:os.(*File).Sync;m:caller" {
		t.Fatalf("pattern order won over first frame: %s", got)
	}
	shape := &StackShape{From: "os.(*File).Sync", DropOffsets: true}
	if got := (CapturedStack{Frames: frames}).key(shape); got != "m:os.(*File).Sync;m:caller" {
		t.Fatalf("literal pointer receiver not matched: %s", got)
	}
	frames[2].Name = "os.(xFile).Sync"
	if got := (CapturedStack{Frames: frames}).key(shape); got != "m:runtime.wrapper;m:unix.Fdatasync;m:os.(xFile).Sync;m:caller" {
		t.Fatalf("interior star treated as wildcard: %s", got)
	}
}
