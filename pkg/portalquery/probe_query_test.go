package query

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestProbeQueries(t *testing.T) {
	text := `syscalls:completion where syscall in (:fsync, :fdatasync) {
  let caller = stack.user(from: ["os.(*File).Sync", glob("*Fdatasync")], offsets: false)
  let directory = path.prefix(file.path, 3)
  @syncs[process_name, directory, caller] = {calls: count(), elapsed: sum(duration_ns), p95: percentile(duration_ns, 95)}
} after 30s { emit @syncs order by elapsed desc limit 25 }`
	r, err := ParseMonitorQuery(text)
	if err != nil {
		t.Fatal(err)
	}
	if r.Phase != "completion" || !r.Paths || r.FileDepth != 3 || r.Stacks == nil || !r.Stacks.User || !r.Stacks.UserShape.DropOffsets || r.Aggregation.SortMetric != 1 || r.Aggregation.Limit != 25 || r.Aggregation.Table != "syncs" || r.Aggregation.Metrics[2].Name != "p95" || r.Aggregation.GroupAliases["user.stack"] != "caller" {
		t.Fatalf("incorrect lowering: %+v", r)
	}
	for _, query := range []string{
		`disk:completion where operation == write { @writes[device_name, io.cgroup.path] = {requests: count(), sectors: sum(sectors)} } every 30s { emit @writes; clear @writes } after 5m { stop }`,
		`packets where protocol = tcp and dst.port = 80 { @traffic[src.ip] = sum(length) } after 30s { emit @traffic }`,
		`process:start { @starts[name] = count() } after 1s { emit @starts }`,
		`memory { @usage[] = avg(used) } after 2s { emit @usage }`,
		`tracepoint:sched:sched_switch where fields in (prev_pid) { @switches[field.prev_pid] = count() } after 1s { emit @switches }`,
	} {
		if _, err := ParseMonitorQuery(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for _, query := range []string{
		`disk { @x[] = count() } after 0s { emit @x }`,
		`disk { @x[] = count() } every 30s { emit @x } after 5m { stop }`,
		`disk { @x[] = count() } every 30s { emit @x; clear @wrong } after 5m { stop }`,
		`disk { @x[] = count() } every 1ms { emit @x; clear @x } after 5m { stop }`,
		`disk { @x[] = count() } after 1s { emit @wrong }`,
		`disk { @x[] = count() } after 1s { emit @x order by missing desc }`,
		`disk { @x[] = {a: count(), a: sum(sectors)} } after 1s { emit @x }`,
		`disk { @x[] = {a: count(), b: count()} } after 1s { emit @x }`,
		`disk { @x[] = system("rm") } after 1s { emit @x }`,
		`disk { @x[] = count(); @y[] = count() } after 1s { emit @x }`,
		`syscalls where syscall in () { @x[] = count() } after 1s { emit @x }`,
		`syscalls { let x = stack.user(from: []); @x[x] = count() } after 1s { emit @x }`,
		`syscalls { let x = path.prefix("file.path", 3); @x[x] = count() } after 1s { emit @x }`,
		"process where name = \"raw\nnewline\" { @x[] = count() } after 1s { emit @x }",
	} {
		if _, err := ParseMonitorQuery(query); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
}

func TestProbeBuckets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`syscalls:completion { let process = pid; @cost[process] = {calls: count(), cost: sum(duration_ns)} } every 1s { emit @cost; clear @cost } after 2500ms { stop }`)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		result, err := aggregateEvents(context.Background(), r, func(ctx context.Context, selection MonitorRequest, emit func(Event) error) error {
			calls++
			if selection.Mode != "" || selection.Aggregation != nil {
				t.Fatal("query controls reached collector")
			}
			for _, sample := range []struct {
				wait  time.Duration
				pid   uint32
				value uint64
			}{{999 * time.Millisecond, 7, 3}, {time.Millisecond, 7, 11}, {1200 * time.Millisecond, 9, 17}} {
				time.Sleep(sample.wait)
				value := sample.value
				if err := emit(Event{PID: sample.pid, DurationNS: &value}); err != nil {
					return err
				}
			}
			<-ctx.Done()
			// A boundary event must not leak into the shortened last bucket.
			value := uint64(100)
			if err := emit(Event{PID: 9, DurationNS: &value}); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if calls != 1 || len(result.Windows) != 3 || result.Aggregation != nil {
			t.Fatalf("subscriptions=%d result=%+v", calls, result)
		}
		for i, want := range []string{"3", "11", "17"} {
			a := result.Windows[i]
			if a.Table != "cost" || len(a.Rows) != 1 || string(a.Rows[0].Values[0]) != "1" || string(a.Rows[0].Values[1]) != want || a.Columns[1].Name != "cost" || !reflect.DeepEqual(a.GroupBy, []string{"process"}) {
				t.Fatalf("bucket %d: %+v", i, a)
			}
			if i > 0 && !a.Start.Equal(result.Windows[i-1].End) {
				t.Fatal("bucket gap")
			}
		}
		if result.Windows[2].End.Sub(result.Windows[2].Start) != 500*time.Millisecond {
			t.Fatal("partial final bucket not retained")
		}
		encoded, err := json.Marshal(result)
		if err != nil || !strings.Contains(string(encoded), `"windows"`) || !strings.Contains(string(encoded), `"process":7`) {
			t.Fatalf("wire format: %s %v", encoded, err)
		}
	})
}

func TestProbeScriptParsing(t *testing.T) {
	r, err := ParseMonitorQuery(`syscalls:completion where syscall = :fsync {
 let proc = process_name;
 @flushes[proc, caller: stack.user(offsets: false)] = {ops: count(), cost: sum(duration_ns)}
}
disk:completion where device_name = "nvme0n1" {
 let proc = name_group;
 @io[proc, device_name] = {ops: count(), p99: percentile(duration_ns, 99)}
} after 30s { emit @io order by p99 asc limit 5; emit @flushes order by cost desc limit 10 }`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "script" || r.Mode != "aggregate" || len(r.Probes) != 2 || r.Aggregation.Window != 30*time.Second {
		t.Fatalf("script not lowered: %+v", r)
	}
	a, b := r.Probes[0], r.Probes[1]
	if a.Source != "syscalls" || !reflect.DeepEqual(a.SyscallNames, []string{"fsync"}) || a.Stacks == nil || !a.Stacks.User || a.Aggregation.Table != "flushes" || a.Aggregation.SortMetric != 1 || a.Aggregation.Limit != 10 || a.Aggregation.GroupAliases["process_name"] != "proc" {
		t.Fatalf("first selector lost capture, names or report: %+v", a)
	}
	if b.Source != "disk" || b.Stacks != nil || b.EventFilters["device_name"] != "nvme0n1" || b.Aggregation.Table != "io" || !b.Aggregation.Ascending || b.Aggregation.Limit != 5 || b.Aggregation.GroupAliases["name_group"] != "proc" {
		t.Fatalf("second selector leaked locals or controls: %+v", b)
	}
	resolved, err := ResolveSyscallNames(r, "arm64")
	if err != nil || !reflect.DeepEqual(resolved.Probes[0].Syscalls, []int{82}) || len(resolved.Probes[0].SyscallNames) != 0 || len(r.Probes[0].SyscallNames) != 1 || len(r.Probes[0].Syscalls) != 0 {
		t.Fatalf("server resolution mutated signed script or used client ABI: %+v, %v", resolved, err)
	}
	for _, script := range []string{
		`process { @a[] = count() } syscalls { @b[] = count() } after 1s { emit @a }`,
		`process { @a[] = count() } syscalls { @a[] = count() } after 1s { emit @a }`,
		`process { @a[] = count() } syscalls { @b[] = count() } after 1s { emit @a; emit @b; emit @other }`,
		`process { @a[] = count() } syscalls { @b[] = count() } after 1s { emit @a; emit @a; emit @b }`,
		`process { @a[] = count() } syscalls { @b[] = count() } after 1s { emit @a } after 1s { emit @b }`,
		`process { @a[] = count() } syscalls { @b[] = count() } after 1s { emit @a } after 2s { emit @b }`,
		`process { @a[] = count() } syscalls { @b[] = count() } every 1s { emit @a; clear @b; emit @b; clear @b } after 2s { stop }`,
		`process { @a[] = count() } syscalls { @b[] = count() } every 1s { emit @a; clear @a; emit @b } after 2s { stop }`,
		`process { @a[] = count() } syscalls { @b[] = count() } after 1s { stop; emit @a; emit @b }`,
		`memory { @a[] = avg(used) } syscalls { @b[] = count() } every 1s { emit @a; clear @a; emit @b; clear @b } after 2s { stop }`,
	} {
		if _, err := ParseMonitorQuery(script); err == nil {
			t.Fatalf("accepted invalid script: %s", script)
		}
	}
	for _, n := range []int{8, 9} {
		var probes, reports strings.Builder
		for i := range n {
			name := "t" + strconv.Itoa(i)
			probes.WriteString("process { @" + name + "[] = count() }")
			reports.WriteString("emit @" + name + ";")
		}
		_, err := ParseMonitorQuery(probes.String() + "after 1s {" + reports.String() + "}")
		if (err == nil) != (n == 8) {
			t.Fatalf("%d selectors: %v", n, err)
		}
	}
}

func TestProbeUnemittedTableErrorsNameTable(t *testing.T) {
	for _, tc := range []struct {
		query, table string
	}{
		{`process { @a[] = count() } after 1s { stop }`, "@a"},
		{`process { @a[] = count() } syscalls { @b[] = count() } after 1s { emit @a }`, "@b"},
	} {
		_, err := ParseMonitorQuery(tc.query)
		if err == nil || !strings.Contains(err.Error(), tc.table) {
			t.Fatalf("missing %s in error for %s: %v", tc.table, tc.query, err)
		}
	}
}

func TestProbeScriptSharedBuckets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`process:start where name = worker { @starts[] = count() }
syscalls:completion where syscall = 3 and duration_ns > 5 { @calls[] = sum(duration_ns) }
every 1s { emit @calls; clear @calls; emit @starts; clear @starts } after 2500ms { stop }`)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		began := make(chan string, 2)
		result, err := aggregateScript(context.Background(), r, func(ctx context.Context, selection MonitorRequest, emit func(Event) error) error {
			if end, _ := ctx.Deadline(); !end.Equal(start.Add(2500*time.Millisecond)) || selection.Mode != "" || selection.Aggregation != nil || len(selection.Probes) != 0 {
				t.Errorf("wrong collector timing/selection: %v, %+v", end, selection)
			}
			began <- selection.Source
			// Simulate independently delayed attachment and source-specific data.
			if selection.Source == "process" {
				time.Sleep(650 * time.Millisecond)
				if err := emit(Event{Process: &ProcessEvent{Name: "other", Action: "start"}}); err != nil {
					return err
				}
				for _, wait := range []time.Duration{250 * time.Millisecond, 100 * time.Millisecond} {
					time.Sleep(wait)
					if err := emit(Event{Process: &ProcessEvent{Name: "worker", Action: "start"}}); err != nil {
						return err
					}
				}
			} else {
				for i, wait := range []time.Duration{900 * time.Millisecond, 100 * time.Millisecond, 1300 * time.Millisecond} {
					time.Sleep(wait)
					value := []uint64{19, 37, 41}[i]
					if err := emit(Event{Syscall: 3, DurationNS: &value}); err != nil {
						return err
					}
				}
			}
			<-ctx.Done()
			// Half-open end: a valid event delivered at the deadline is ignored.
			value := uint64(71)
			return emit(Event{Syscall: 3, DurationNS: &value, Process: &ProcessEvent{Name: "worker", Action: "start"}})
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(began) != 2 || time.Since(start) != 2500*time.Millisecond || len(result.Tables) != 2 {
			t.Fatalf("sources not concurrent: %+v, elapsed %s", result, time.Since(start))
		}
		for table, values := range [][]string{{"1", "1", "0"}, {"19", "37", "41"}} {
			windows := result.Tables[table].Windows
			if len(windows) != 3 {
				t.Fatalf("missing windows: %+v", windows)
			}
			for i, want := range values {
				window := windows[i]
				if !window.Start.Equal(start.Add(time.Duration(i)*time.Second)) || !window.End.Equal(start.Add(min(time.Duration(i+1)*time.Second, 2500*time.Millisecond))) || len(window.Rows) != 1 || string(window.Rows[0].Values[0]) != want {
					t.Fatalf("table %d bucket %d: %+v; want %s", table, i, window, want)
				}
			}
		}
	})
}

func TestProbeScriptMixedSampling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`process:start { @starts[] = count() } memory { @mem[] = avg(used) } after 2500ms { emit @mem; emit @starts }`)
		if err != nil {
			t.Fatal(err)
		}
		samples := 0
		result, err := aggregateScript(context.Background(), r, func(ctx context.Context, selection MonitorRequest, emit func(Event) error) error {
			if selection.Source != "process" {
				t.Error("sampled source reached event collector")
			}
			if err := emit(Event{Process: &ProcessEvent{Action: "start"}}); err != nil {
				return err
			}
			<-ctx.Done()
			return nil
		}, func(ctx context.Context, selection MonitorRequest) (Snapshot, error) {
			if selection.Source != "memory" || selection.Mode != "snapshot" {
				t.Errorf("event source reached sampler: %+v", selection)
			}
			used := []uint64{10, 20, 90}[samples]
			samples++
			return Snapshot{Source: "memory", Memory: &MemoryInfo{Used: used}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		a, b := result.Tables[0].Aggregation, result.Tables[1].Aggregation
		average, err := strconv.ParseFloat(string(b.Rows[0].Values[0]), 64)
		if err != nil || samples != 3 || string(a.Rows[0].Values[0]) != "1" || average != 40 || !a.Start.Equal(b.Start) || !a.End.Equal(b.End) {
			t.Fatalf("mixed sampling: %+v, %+v, %d samples", a, b, samples)
		}
	})
}

func TestProbeScriptFailureCancelsPeers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`process { @a[] = count() } syscalls { @b[] = count() } after 1m { emit @a; emit @b }`)
		if err != nil {
			t.Fatal(err)
		}
		ready, stopped := make(chan struct{}), make(chan struct{})
		failure := errors.New("capture failed")
		start := time.Now()
		result, err := aggregateScript(context.Background(), r, func(ctx context.Context, selection MonitorRequest, emit func(Event) error) error {
			if selection.Source == "process" {
				close(ready)
				<-ctx.Done()
				close(stopped)
				return ctx.Err()
			}
			<-ready
			return failure
		}, nil)
		if !errors.Is(err, failure) || !strings.Contains(err.Error(), "@b (syscalls)") || len(result.Tables) != 0 || time.Since(start) != 0 {
			t.Fatalf("failure did not cancel peers/discard partial results: %+v, %v", result, err)
		}
		select {
		case <-stopped:
		default:
			t.Fatal("returned while peer still running")
		}
	})
}

func TestProbeSourceFailure(t *testing.T) {
	r, err := ParseMonitorQuery(`disk { @x[] = count() } every 1s { emit @x; clear @x } after 2s { stop }`)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("failed capture")
	if _, err := aggregateEvents(context.Background(), r, func(context.Context, MonitorRequest, func(Event) error) error { return want }); !errors.Is(err, want) {
		t.Fatalf("source failure hidden: %v", err)
	}
}

func TestProbeSeriesGroupBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`syscalls { @x[pid] = count() } every 1s { emit @x; clear @x } after 2s { stop }`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = aggregateEvents(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
			for bucket := 0; bucket < 2; bucket++ {
				if bucket == 1 {
					time.Sleep(time.Second)
				}
				for i := 1; i <= 2049; i++ {
					if err := emit(Event{PID: uint32(i)}); err != nil {
						return err
					}
				}
			}
			<-ctx.Done()
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "4096 retained") {
			t.Fatalf("series bypassed group budget: %v", err)
		}
	})
}

func FuzzProbeQueries(f *testing.F) {
	f.Add(`tracepoint:sched:sched_switch { @x[previous: field.prev_pid] = count() } after 1s { emit @x order by value asc }`)
	f.Add(`process { @a[] = count() } syscalls { @b[] = count() } every 1s { emit @b; clear @b; emit @a; clear @a } after 3s { stop }`)
	f.Add(`memory { @mem[] = avg(used) } tracepoint:sched:sched_switch { @switches[field.prev_pid] = count() } after 2s { emit @mem; emit @switches }`)
	for _, seed := range []string{`disk { @x[] = count() } after 1s { emit @x }`, `disk where operation = [write,read] { @x[] = count() } after 1s { emit @x }`, `syscalls { let x = stack.user(from: []); @x[x] = count() } after 1s { emit @x }`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		r, err := ParseMonitorQuery(text)
		if err == nil && r.Source != "capabilities" {
			if err := r.Validate(); err != nil {
				t.Fatalf("parser accepted invalid request: %v", err)
			}
		}
	})
}

func TestProbeAliasesOrderingAndInferredFields(t *testing.T) {
	r, err := ParseMonitorQuery(`tracepoint:jbd2:jbd2_handle_start where field.dev > 1 {
  let blocks = field.requested_blocks;
  @j[proc: process_name, device: field.dev] = {handles: count(), blocks: sum(blocks)}
} after 30s { emit @j order by blocks asc limit 2 }`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Tracepoint.Fields, []string{"requested_blocks", "dev"}) || !r.Aggregation.Ascending || r.Aggregation.SortMetric != 1 || r.Aggregation.GroupAliases["process_name"] != "proc" || r.Aggregation.GroupAliases["field.dev"] != "device" {
		t.Fatalf("incorrect lowering: %+v", r)
	}
	for _, tc := range []struct {
		query  string
		fields []string
	}{
		{`tracepoint:sched:sched_switch { @x[] = count() } after 1s { emit @x }`, nil},
		{`tracepoint:sched:sched_switch { @x[proc: pid] = count() } after 1s { emit @x }`, nil},
		{`tracepoint:sched:sched_switch { @x[next: field.next_pid] = sum(field.prev_pid) } after 1s { emit @x }`, []string{"next_pid", "prev_pid"}},
		{`tracepoint:sched:sched_switch where field.prev_pid = 1 { @x[field.prev_pid] = count() } after 1s { emit @x }`, []string{"prev_pid"}},
		{`tracepoint:sched:sched_switch where fields in (common_pid) { @x[field.prev_pid] = sum(field.common_pid) } after 1s { emit @x }`, []string{"common_pid", "prev_pid"}},
	} {
		r, err := ParseMonitorQuery(tc.query)
		if err != nil || !reflect.DeepEqual(r.Tracepoint.Fields, tc.fields) {
			t.Fatalf("%s: %+v, %v", tc.query, r, err)
		}
	}
	for _, query := range []string{
		`disk { @x[proc: name, proc: operation] = count() } after 1s { emit @x }`,
		`disk { @x[bad.alias: name] = count() } after 1s { emit @x }`,
		`disk { @x[a: name, b: name] = count() } after 1s { emit @x }`,
		`disk { @x[] = count() } after 1s { emit @x order by missing asc }`,
		`tracepoint:sched:sched_switch { @x[field.bad/name] = count() } after 1s { emit @x }`,
	} {
		if _, err := ParseMonitorQuery(query); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	r, err = ParseMonitorQuery(`syscalls { @x[caller: stack.user(offsets: false)] = count() } after 1s { emit @x }`)
	if err != nil || r.Stacks == nil || !r.Stacks.User || r.Aggregation.GroupAliases["user.stack"] != "caller" {
		t.Fatalf("inline capture: %+v, %v", r, err)
	}
	var oversized strings.Builder
	oversized.WriteString("tracepoint:custom:sample {")
	for i := 0; i < 17; i++ {
		name := strconv.Itoa(i)
		oversized.WriteString("let x" + name + " = field.f" + name + ";")
	}
	oversized.WriteString("@x[] = count() } after 1s { emit @x }")
	if _, err := ParseMonitorQuery(oversized.String()); err == nil || !strings.Contains(err.Error(), "at most 16") {
		t.Fatalf("inference bypassed capture limit: %v", err)
	}

	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`tracepoint:custom:sample { @x[proc: pid] = {calls: count(), cost: sum(field.cost)} } every 1s { emit @x order by cost asc limit 1; clear @x } after 2s { stop }`)
		if err != nil {
			t.Fatal(err)
		}
		result, err := aggregateEvents(context.Background(), r, func(ctx context.Context, selection MonitorRequest, emit func(Event) error) error {
			if !reflect.DeepEqual(selection.Tracepoint.Fields, []string{"cost"}) {
				t.Fatal("inferred fields did not reach collector")
			}
			for _, n := range []int{9, 2} {
				if err := emit(Event{PID: uint32(n), Tracepoint: &TracepointEvent{Event: "custom:sample", Fields: map[string]json.Number{"cost": json.Number(strconv.Itoa(n))}}}); err != nil {
					return err
				}
			}
			<-ctx.Done()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if row := result.Windows[0].Rows[0]; string(row.Group["proc"]) != "2" || string(row.Values[1]) != "2" {
			t.Fatalf("ascending aliased bucket: %+v", row)
		}
	})
}
