package query

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestParseMonitorQuery(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  MonitorRequest
	}{
		{"packets", MonitorRequest{Source: "packets"}},
		{"packets where protocol == tcp", MonitorRequest{Source: "packets", Packet: &PacketFilter{Protocol: "tcp"}}},
		{"process", MonitorRequest{Source: "process"}},
		{"cpu", MonitorRequest{Source: "cpu", Mode: "snapshot"}},
		{"memory", MonitorRequest{Source: "memory", Mode: "snapshot"}},
		{"network where name = eth*", MonitorRequest{Source: "network", Mode: "snapshot", Name: "eth*"}},
		{"kernel", MonitorRequest{Source: "kernel", Mode: "snapshot"}},
		{"sensors where name = '*temp1'", MonitorRequest{Source: "sensors", Mode: "snapshot", Name: "*temp1"}},
		{"containers where name = web*", MonitorRequest{Source: "containers", Mode: "snapshot", Name: "web*"}},
		{"gpu where name = '*A100'", MonitorRequest{Source: "gpu", Mode: "snapshot", Name: "*A100"}},
		{"disk where operation = WRITE and device = 0x1234", MonitorRequest{Source: "disk", Disk: &DiskFilter{Operation: "write", Device: 0x1234}}},
		{"tracepoint where event = sched:sched_wakeup and fields in (pid, target_cpu) and field.target_cpu = 0x2", MonitorRequest{Source: "tracepoint", Tracepoint: &TracepointFilter{Event: "sched:sched_wakeup", Fields: []string{"pid", "target_cpu"}, Equals: map[string]string{"target_cpu": "0x2"}}}},
		{"tracepoint where event = custom:sample and fields in (CamelCase) and FIELD.CamelCase = 08", MonitorRequest{Source: "tracepoint", Tracepoint: &TracepointFilter{Event: "custom:sample", Fields: []string{"CamelCase"}, Equals: map[string]string{"CamelCase": "08"}}}},
		{"process where name = worker*", MonitorRequest{Source: "process", Process: &ProcessFilter{Name: "worker*"}}},
		{"process where name = '*Helper'", MonitorRequest{Source: "process", Process: &ProcessFilter{Name: "*Helper"}}},
		{"process where pid = 123 and name = worker and action = START", MonitorRequest{Source: "process", PID: 123, Process: &ProcessFilter{Name: "worker", Action: "start"}}},
		{`process where name = "Google Chrome Helper" and action = exit`, MonitorRequest{Source: "process", Process: &ProcessFilter{Name: "Google Chrome Helper", Action: "exit"}}},
		{`process where name = 'worker\'s agent'`, MonitorRequest{Source: "process", Process: &ProcessFilter{Name: "worker's agent"}}},
		{"syscalls where pid=42 and syscall in(0, 1,9)", MonitorRequest{Source: "syscalls", PID: 42, Syscalls: []int{0, 1, 9}}},
		{"syscalls where syscall = 0", MonitorRequest{Source: "syscalls", Syscalls: []int{0}}},
		{"syscalls where syscall = 2 count over 30s by pid", MonitorRequest{Source: "syscalls", Mode: "aggregate", Syscalls: []int{2}, Aggregation: &AggregationRequest{Window: 30 * time.Second, GroupBy: []string{"pid"}}}},
		{"PROCESS COUNT OVER 1m BY NAME, ACTION  ", MonitorRequest{Source: "process", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Minute, GroupBy: []string{"name", "action"}}}},
		{"disk count over 1h", MonitorRequest{Source: "disk", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Hour}}},
		{"packets where dst.port = 80 and protocol = tcp SUM ( LENGTH ) over 30s by dst.port", MonitorRequest{Source: "packets", Mode: "aggregate", Packet: &PacketFilter{Protocol: "tcp", DestinationPort: 80}, Aggregation: &AggregationRequest{Window: 30 * time.Second, Function: "sum", Field: "length", GroupBy: []string{"dst.port"}}}},
		{"disk avg(sectors) over 1m", MonitorRequest{Source: "disk", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Minute, Function: "avg", Field: "sectors"}}},
		{"syscalls min(pid) over 1s", MonitorRequest{Source: "syscalls", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Second, Function: "min", Field: "pid"}}},
		{"syscalls max(tid) over 1s", MonitorRequest{Source: "syscalls", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Second, Function: "max", Field: "tid"}}},
		{"process count_distinct(name) over 1s", MonitorRequest{Source: "process", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Second, Function: "count_distinct", Field: "name"}}},
		{"disk percentile(sectors, 99.9) over 1s by device", MonitorRequest{Source: "disk", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Second, Function: "percentile", Field: "sectors", Percentile: 99.9, GroupBy: []string{"device"}}}},
		{"tracepoint where event = custom:sample and fields in (CamelCase) min(FIELD.CamelCase) over 1s", MonitorRequest{Source: "tracepoint", Mode: "aggregate", Tracepoint: &TracepointFilter{Event: "custom:sample", Fields: []string{"CamelCase"}}, Aggregation: &AggregationRequest{Window: time.Second, Function: "min", Field: "field.CamelCase"}}},
		{"tracepoint where event = custom:sample and fields in (CamelCase) count over 2s by FIELD.CamelCase", MonitorRequest{Source: "tracepoint", Mode: "aggregate", Tracepoint: &TracepointFilter{Event: "custom:sample", Fields: []string{"CamelCase"}}, Aggregation: &AggregationRequest{Window: 2 * time.Second, GroupBy: []string{"field.CamelCase"}}}},
		{"packets where protocol = tcp and direction = outgoing and dst.port = 80", MonitorRequest{Source: "packets", Packet: &PacketFilter{Protocol: "tcp", Direction: "outgoing", DestinationPort: 80}}},
		{"packets where src.ip = 2001:0db8::1 and dst.ip = 192.0.2.5 and protocol = UDP and src.port = 5353 and dst.port = 53", MonitorRequest{Source: "packets", Packet: &PacketFilter{Protocol: "udp", SourceIP: "2001:0db8::1", DestinationIP: "192.0.2.5", SourcePort: 5353, DestinationPort: 53}}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, err := ParseMonitorQuery(tc.query)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseMonitorQuery(%q) = %+v, %v; want %+v", tc.query, got, err, tc.want)
			}
		})
	}
	request, err := ParseMonitorQuery("packets where protocol = tcp and direction = outgoing and dst.port = 80")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		event Event
		want  bool
	}{
		{Event{Packet: &PacketEvent{Protocol: "tcp", Direction: "outgoing", SourcePort: 443, DestinationPort: 80}}, true},
		{Event{Packet: &PacketEvent{Protocol: "tcp", Direction: "outgoing", SourcePort: 80, DestinationPort: 443}}, false},
		{Event{Packet: &PacketEvent{Protocol: "udp", Direction: "outgoing", DestinationPort: 80}}, false},
		{Event{Packet: &PacketEvent{Protocol: "tcp", Direction: "incoming", DestinationPort: 80}}, false},
	} {
		if got := request.Matches(tc.event); got != tc.want {
			t.Fatalf("query matched %+v = %v, want %v", tc.event, got, tc.want)
		}
	}
}

func TestParseMonitorQueryRejectsAmbiguity(t *testing.T) {
	for _, query := range []string{
		"", "anything", "packets where", "packets protocol = tcp", "packets where protocol =",
		"packets where protocol = tcp and", "packets where protocol = tcp protocol = udp", "packets where protocol = tcp and protocol = udp",
		"packets where dst.port = 80", "packets where protocol = icmp", "packets where protocol = tcp and dst.port = 65536",
		"packets where protocol = tcp and dst.port = 0", "packets where pid = 42", "packets where protocol in (tcp,udp)",
		"syscalls where src.port = 80", "syscalls where pid = 0", "syscalls where pid = -1", "syscalls where syscall = 65536",
		"syscalls where syscall in ()", "syscalls where syscall in (0,)", "syscalls where syscall in (0 1)",
		"syscalls where syscall in (0,1", "syscalls where syscall in (0) and syscall = 2", "syscalls where syscall = -1",
		"process where action = restart", "process where pid = 0", "process where name = worker and syscall = 1",
		"process where name = *", "process where name = wor*ker", "process where name = *work*",
		"disk where pid = 2", "disk where device = 0", "disk where operation = trim", "disk where operation in (read,write)",
		"tracepoint", "tracepoint where event = sched:sched_wakeup", "tracepoint where fields in (pid)",
		"tracepoint where event = ../sched:sched_wakeup and fields in (pid)",
		"tracepoint where event = sched:sched_wakeup and fields in (pid, pid)",
		"tracepoint where event = sched:sched_wakeup and fields in (pid) and field.prio = 2",
		"tracepoint where event = sched:sched_wakeup and fields in (pid) and field.pid = nope",
		"tracepoint where event = sched:sched_wakeup and fields in (pid) and field.pid = 18446744073709551616",
		"tracepoint where event = sched:sched_wakeup and fields in (pid) and mode = snapshot",
		"cpu where pid = 1", "memory where name = ram", "kernel where load = 1", "network where name = e*th", "sensors where name = *",
		"containers where name = a*b", "gpu where index = 0",
		"syscalls count over 0s", "syscalls count over -1s", "syscalls count over 2h", "syscalls count over nope",
		"syscalls count over 999999999999999999999s", "syscalls count over 30s by", "syscalls count over 30s by pid,",
		"syscalls count over 30s by pid,pid", "syscalls count over 30s by nope", "syscalls count over 30s by pid tid",
		"syscalls count over 30s where syscall = 2", "syscalls sum over 30s", "capabilities count over 30s",
		"packets count over 30s by pid", "disk count over 30s by nope", "process count over 30s by syscall",
		"tracepoint where event = custom:sample and fields in (pid) count over 30s by field.unselected",
		"packets count over 30s by protocol,direction,src.ip,dst.ip,src.port",
		"process sum(name) over 1s", "packets avg(src.ip) over 1s", "disk min(operation) over 1s",
		"syscalls sum() over 1s", "syscalls count(pid) over 1s", "syscalls count_distinct() over 1s",
		"syscalls count_distinct(nope) over 1s", "syscalls max(nope) over 1s", "syscalls sum(pid,tid) over 1s",
		"syscalls percentile(pid) over 1s", "syscalls percentile(pid,NaN) over 1s", "syscalls percentile(pid,Inf) over 1s",
		"syscalls percentile(pid,-0.1) over 1s", "syscalls percentile(pid,100.1) over 1s", "syscalls percentile(pid,bad) over 1s",
		"syscalls percentile(pid,95,99) over 1s", "syscalls sum(pid) max(pid) over 1s",
		"tracepoint where event = custom:sample and fields in (pid) sum(field.other) over 1s",
		`process where name = ""`, `process where name = "broken`, `process where name = "okay"suffix`, `process where name = "bad\q"`,
		"packets where protocol = tcp or protocol = udp", strings.Repeat("a", 4097),
	} {
		t.Run(query, func(t *testing.T) {
			if got, err := ParseMonitorQuery(query); err == nil {
				t.Fatalf("invalid query accepted: %+v", got)
			}
		})
	}
}

func TestNumericEventComparisons(t *testing.T) {
	for _, op := range []string{">", ">=", "<", "<="} {
		for _, text := range []string{
			"syscalls where phase = completion and duration_ns" + op + "5000000 count over 1s",
			"syscalls:completion where duration_ns " + op + " 5000000 { @slow[] = count() } after 1s { emit @slow }",
		} {
			r, err := ParseMonitorQuery(text)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []uint64{4999999, 5000000, 5000001} {
				want := op == ">" && n > 5000000 || op == ">=" && n >= 5000000 || op == "<" && n < 5000000 || op == "<=" && n <= 5000000
				if r.Matches(Event{DurationNS: &n}) != want {
					t.Fatalf("%s at %d", text, n)
				}
			}
			if r.Matches(Event{}) || !r.Matches(Event{Kind: "collection_stats"}) {
				t.Fatal("missing values or diagnostics mishandled")
			}
		}
	}
	for _, tc := range []struct {
		query, value string
		want         bool
	}{
		{`tracepoint where event = custom:sample and fields in (Value) and field.Value > 18446744073709551614`, "18446744073709551615", true},
		{`tracepoint where event = custom:sample and fields in (Value) and field.Value > 18446744073709551614`, "18446744073709551614", false},
		{`tracepoint where event = custom:sample and fields in (Value) and field.Value < -2`, "-3", true},
		{`tracepoint where event = custom:sample and fields in (Value) and field.Value < -2`, "-1", false},
		{`tracepoint where event = custom:sample and fields in (Value) and field.Value >= 010`, "9", false},
		{`tracepoint where event = custom:sample and fields in (Value) and field.Value >= 0x10`, "16", true},
		{`tracepoint where event = custom:sample and fields in (Value) and field.Value < 2.5`, "2", true},
	} {
		r, err := ParseMonitorQuery(tc.query)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Matches(Event{Tracepoint: &TracepointEvent{Event: "custom:sample", Fields: map[string]json.Number{"Value": json.Number(tc.value)}}}); got != tc.want {
			t.Fatalf("%s with %s: %v", tc.query, tc.value, got)
		}
	}
	for _, text := range []string{
		`syscalls where duration_ns > 1`, `disk where operation > 2`, `memory where used > 1`,
		`process where pid > 1 avg(cpu_percent) over 2s every 1s`,
		`disk:completion where duration_ns > NaN { @x[] = count() } after 1s { emit @x }`,
		`disk:completion where duration_ns > [1,2] { @x[] = count() } after 1s { emit @x }`,
		`tracepoint where event = custom:sample and fields in (a) and field.b > 1`,
	} {
		if _, err := ParseMonitorQuery(text); err == nil {
			t.Fatalf("accepted %s", text)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`disk:completion where duration_ns > 5000000 and duration_ns <= 9000000 { @slow[] = {calls: count(), elapsed: sum(duration_ns)} } every 1s { emit @slow; clear @slow } after 2s { stop }`)
		if err != nil {
			t.Fatal(err)
		}
		result, err := aggregateEvents(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
			for _, n := range []uint64{5000000, 5000001, 9000000, 9000001} {
				if err := emit(Event{Disk: &DiskEvent{DurationNS: &n}}); err != nil {
					return err
				}
			}
			<-ctx.Done()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := result.Windows[0].Rows[0].Values; string(got[0]) != "2" || string(got[1]) != "14000001" {
			t.Fatalf("unfiltered reductions: %s", got)
		}
	})
}

func TestDurationComparisonLiterals(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  string
	}{
		{`syscalls where phase = completion and duration_ns > 5ms count over 1s`, "5000000"},
		{`syscalls where phase = completion and DURATION_NS >= 1.5ms count over 1s`, "1500000"},
		{`syscalls:completion where duration_ns < 2.5µs { @x[] = count() } after 1s { emit @x }`, "2500"},
		{`disk:completion where duration_ns <= 2.5μs { @x[] = count() } after 1s { emit @x }`, "2500"},
		{`disk:completion where duration_ns > 0x10 { @x[] = count() } after 1s { emit @x }`, "0x10"},
	} {
		r, err := ParseMonitorQuery(tc.query)
		if err != nil {
			t.Fatalf("%s: %v", tc.query, err)
		}
		if got := r.Comparisons[0].Value; got != tc.want {
			t.Fatalf("%s threshold = %q, want %q", tc.query, got, tc.want)
		}
	}

	r, err := ParseMonitorQuery(`syscalls:completion where duration_ns > 5ms { @x[] = count() } after 1s { emit @x }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		n    uint64
		want bool
	}{{4999999, false}, {5000000, false}, {5000001, true}} {
		if got := r.Matches(Event{DurationNS: &tc.n}); got != tc.want {
			t.Fatalf("duration %d matched = %v, want %v", tc.n, got, tc.want)
		}
	}

	for _, query := range []string{
		`syscalls where phase = completion and duration_ns > 5watts count over 1s`,
		`syscalls where phase = completion and duration_ns > 1e100s count over 1s`,
		`syscalls where syscall > 5ms count over 1s`,
		`tracepoint where event = custom:sample and fields in (Value) and field.Value > 5ms`,
	} {
		if _, err := ParseMonitorQuery(query); err == nil {
			t.Fatalf("accepted invalid duration threshold: %s", query)
		}
	}
}

func TestReadableQuerySyntaxErrors(t *testing.T) {
	for _, text := range []string{
		"disk:completion where duration_ns ! 1 { @io[] = count() } after 1s { emit @io }",
		"disk where sectors ! 1 count over 1s",
		"disk {\n  @io[] = count(\n} after 1s { emit @io }",
		"disk { @io[] = count() } after 1s { emit @io order by value sideways }",
		"process where name = \"é\" and pid ! 1",
	} {
		_, err := ParseMonitorQuery(text)
		if err == nil {
			t.Fatalf("accepted %s", text)
		}
		message := err.Error()
		if !strings.Contains(message, "line ") || !strings.Contains(message, "column ") || !strings.Contains(message, "^") || strings.Contains(message, "check rune") || strings.Contains(message, "!/") || strings.Contains(message, "&{") {
			t.Fatalf("unreadable error: %s", message)
		}
		if strings.Contains(text, " ! ") && !strings.Contains(message, "after a field name") {
			t.Fatalf("missing operator hint: %s", message)
		}
	}
	_, err := ParseMonitorQuery("disk where sectors ! 1 count over 1s")
	want := "line 1, column 20: expected =, ==, in, >, >=, < or <= after a field name\n  disk where sectors ! 1 count over 1s\n  " + strings.Repeat(" ", 19) + "^"
	if err.Error() != want {
		t.Fatalf("incorrect caret: %s", err)
	}
}

func TestNumericComparisonValidation(t *testing.T) {
	for _, c := range []NumericComparison{
		{Field: "duration_ns", Op: "!=", Value: "1"},
		{Field: "duration_ns", Op: ">", Value: "1/2"},
		{Field: "duration_ns", Op: ">", Value: "NaN"},
		{Field: "duration_ns", Op: ">", Value: strings.Repeat("1", 129)},
		{Field: "operation", Op: ">", Value: "1"},
	} {
		r := MonitorRequest{Source: "disk", Phase: "completion", Comparisons: []NumericComparison{c}}
		if err := r.Validate(); err == nil {
			t.Fatalf("server accepted invalid predicate: %+v", c)
		}
	}
	r := MonitorRequest{Source: "disk", Phase: "completion", Comparisons: make([]NumericComparison, 17)}
	if err := r.Validate(); err == nil {
		t.Fatal("unbounded comparisons")
	}
}
