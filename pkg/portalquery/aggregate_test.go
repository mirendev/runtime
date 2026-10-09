package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestAggregateWindowAndFilters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		request, err := ParseMonitorQuery("syscalls where syscall = 2 count over 30s by pid")
		if err != nil {
			t.Fatal(err)
		}
		source := func(ctx context.Context, r MonitorRequest, emit func(Event) error) error {
			if r.Mode != "" || r.Aggregation != nil {
				t.Fatal("query spec leaked into event source")
			}
			for _, e := range []Event{{PID: 7, Syscall: 2}, {PID: 42, Syscall: 2}, {PID: 99, Syscall: 3}} {
				if err := emit(e); err != nil {
					return err
				}
			}
			time.Sleep(30*time.Second - time.Nanosecond)
			if err := emit(Event{PID: 7, Syscall: 2, Time: time.Now().Add(-time.Hour)}); err != nil {
				return err
			}
			time.Sleep(time.Nanosecond)
			if err := emit(Event{PID: 42, Syscall: 2}); err != nil {
				return err
			}
			<-ctx.Done()
			return nil
		}
		start := time.Now()
		got, err := aggregateEvents(context.Background(), request, source)
		if err != nil {
			t.Fatal(err)
		}
		want := []AggregateCount{{Group: map[string]json.RawMessage{"pid": json.RawMessage(`7`)}, Count: 2}, {Group: map[string]json.RawMessage{"pid": json.RawMessage(`42`)}, Count: 1}}
		if !reflect.DeepEqual(got.Aggregation.Counts, want) || !got.Aggregation.Start.Equal(start) || !got.Aggregation.End.Equal(start.Add(30*time.Second)) || !got.Time.Equal(got.Aggregation.End) {
			t.Fatalf("wrong window or counts: %+v", got.Aggregation)
		}
	})
}

func TestAggregateOtherSources(t *testing.T) {
	for _, tc := range []struct {
		query string
		event Event
		group string
	}{
		{"process count over 1s by name,action", Event{Process: &ProcessEvent{Name: "worker", Action: "start"}}, `{"action":"start","name":"worker"}`},
		{"packets count over 1s by dst.port,src.ip", Event{Packet: &PacketEvent{DestinationPort: 80, SourceIP: "192.0.2.7"}}, `{"dst.port":80,"src.ip":"192.0.2.7"}`},
		{"disk count over 1s by device,operation", Event{Disk: &DiskEvent{Device: 12, Operation: "read"}}, `{"device":12,"operation":"read"}`},
		{"tracepoint where event = custom:sample and fields in (value) count over 1s by field.value", Event{Tracepoint: &TracepointEvent{Event: "custom:sample", Fields: map[string]json.Number{"value": "18446744073709551615"}}}, `{"field.value":18446744073709551615}`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				request, err := ParseMonitorQuery(tc.query)
				if err != nil {
					t.Fatal(err)
				}
				got, err := aggregateEvents(context.Background(), request, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
					if err := emit(tc.event); err != nil {
						return err
					}
					<-ctx.Done()
					return ctx.Err()
				})
				if err != nil || len(got.Aggregation.Counts) != 1 || got.Aggregation.Counts[0].Count != 1 {
					t.Fatalf("count: %+v, %v", got, err)
				}
				group, _ := json.Marshal(got.Aggregation.Counts[0].Group)
				if string(group) != tc.group {
					t.Fatalf("group = %s, want %s", group, tc.group)
				}
				data, _ := json.Marshal(got)
				if strings.Contains(string(data), `"processes"`) {
					t.Fatal("aggregation mislabeled as a process snapshot")
				}
			})
		})
	}
}

func TestAggregateEmptyAndFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, group := range [][]string{nil, {"pid"}} {
			r := MonitorRequest{Source: "syscalls", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Second, GroupBy: group}}
			got, err := aggregateEvents(context.Background(), r, func(ctx context.Context, _ MonitorRequest, _ func(Event) error) error { <-ctx.Done(); return nil })
			if err != nil || got.Aggregation.Counts == nil {
				t.Fatalf("empty window: %+v, %v", got, err)
			}
			if len(group) == 0 && (len(got.Aggregation.Counts) != 1 || got.Aggregation.Counts[0].Count != 0) || len(group) != 0 && len(got.Aggregation.Counts) != 0 {
				t.Fatalf("empty count semantics: %+v", got.Aggregation)
			}
		}
		r := MonitorRequest{Source: "syscalls", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Second}}
		failure := errors.New("source failed")
		if _, err := aggregateEvents(context.Background(), r, func(context.Context, MonitorRequest, func(Event) error) error { return failure }); !errors.Is(err, failure) {
			t.Fatalf("source failure hidden: %v", err)
		}
		if _, err := aggregateEvents(context.Background(), r, func(context.Context, MonitorRequest, func(Event) error) error { return nil }); err == nil {
			t.Fatal("partial window reported as complete")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if _, err := aggregateEvents(ctx, r, func(ctx context.Context, _ MonitorRequest, _ func(Event) error) error { <-ctx.Done(); return nil }); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("caller cancellation became a result: %v", err)
		}
	})
}

func TestAggregateGroupLimit(t *testing.T) {
	for _, size := range []int{4096, 4097} {
		synctest.Test(t, func(t *testing.T) {
			r := MonitorRequest{Source: "syscalls", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Second, GroupBy: []string{"pid"}}}
			got, err := aggregateEvents(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
				for i := 1; i <= size; i++ {
					if err := emit(Event{PID: uint32(i)}); err != nil {
						return err
					}
				}
				<-ctx.Done()
				return nil
			})
			if size == 4096 && (err != nil || len(got.Aggregation.Counts) != 4096) || size == 4097 && (err == nil || !strings.Contains(err.Error(), "4096 groups")) {
				t.Fatalf("group limit %d: %+v, %v", size, got, err)
			}
		})
	}
}

func TestAggregateCompoundGroups(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery("process count over 1s by name,action")
		if err != nil {
			t.Fatal(err)
		}
		got, err := aggregateEvents(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
			for _, process := range []ProcessEvent{{Name: "worker", Action: "start"}, {Name: "worker", Action: "exit"}, {Name: "worker", Action: "start"}, {Name: "other", Action: "start"}} {
				if err := emit(Event{Process: &process}); err != nil {
					return err
				}
			}
			<-ctx.Done()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		counts := make(map[string]uint64)
		for _, count := range got.Aggregation.Counts {
			group, err := json.Marshal(count.Group)
			if err != nil {
				t.Fatal(err)
			}
			counts[string(group)] = count.Count
		}
		want := map[string]uint64{`{"action":"start","name":"worker"}`: 2, `{"action":"exit","name":"worker"}`: 1, `{"action":"start","name":"other"}`: 1}
		if !reflect.DeepEqual(counts, want) {
			t.Fatalf("compound grouping ignored a field: %v", counts)
		}
	})
}

func aggregateFixture(t *testing.T, query string, events []Event) Snapshot {
	t.Helper()
	r, err := ParseMonitorQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	got, err := aggregateEvents(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
		for _, event := range events {
			if err := emit(event); err != nil {
				return err
			}
		}
		<-ctx.Done()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Verify the wire representation, including full-width integers.
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestAggregateFunctions(t *testing.T) {
	for _, tc := range []struct{ metric, first, second string }{
		{"sum(field.value)", "17", "12"},
		{"avg(field.value)", "3.4", "6"},
		{"min(field.value)", "-5", "3"},
		{"max(field.value)", "18", "9"},
		{"count_distinct(field.value)", "4", "2"},
		{"percentile(field.value, 50)", "2", "3"},
		{"percentile(field.value, 0)", "-5", "3"},
		{"percentile(field.value, 100)", "18", "9"},
		{"percentile(field.value, 99.9)", "18", "9"},
	} {
		t.Run(tc.metric, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var events []Event
				for i, values := range [][]string{{"-5", "0", "2", "18", "2"}, {"9", "3"}} {
					for _, value := range values {
						events = append(events, Event{Tracepoint: &TracepointEvent{Event: "custom:sample", Fields: map[string]json.Number{"value": json.Number(value), "group": json.Number(fmt.Sprint(i + 1))}}})
					}
				}
				got := aggregateFixture(t, "tracepoint where event = custom:sample and fields in (value,group) "+tc.metric+" over 1s by field.group", events)
				values := got.Aggregation.Values
				if len(values) != 2 || string(values[0].Group["field.group"]) != "1" || string(values[1].Group["field.group"]) != "2" || string(values[0].Value) != tc.first || string(values[1].Value) != tc.second || got.Aggregation.Counts != nil {
					t.Fatalf("grouped values: %+v; want %s, %s", values, tc.first, tc.second)
				}
			})
		})
	}
}

func TestAggregatePrecision(t *testing.T) {
	for _, tc := range []struct {
		metric, want string
		input        []string
	}{
		{"sum(field.x)", "36893488147419103230", []string{"18446744073709551615", "18446744073709551615"}},
		{"avg(field.x)", "18446744073709551614.5", []string{"18446744073709551615", "18446744073709551614"}},
		{"min(field.x)", "9007199254740992", []string{"9007199254740993", "9007199254740992"}},
		{"max(field.x)", "9007199254740993", []string{"9007199254740992", "9007199254740993"}},
		{"avg(field.x)", "0.333333333333333333", []string{"0", "0", "1"}},
		{"percentile(field.x, 50)", "9007199254740992", []string{"9007199254740993", "9007199254740992"}},
	} {
		t.Run(tc.metric+tc.want, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var events []Event
				for _, value := range tc.input {
					events = append(events, Event{Tracepoint: &TracepointEvent{Event: "custom:sample", Fields: map[string]json.Number{"x": json.Number(value)}}})
				}
				got := aggregateFixture(t, "tracepoint where event = custom:sample and fields in (x) "+tc.metric+" over 1s", events)
				if len(got.Aggregation.Values) != 1 || string(got.Aggregation.Values[0].Value) != tc.want {
					t.Fatalf("precision lost: %+v, want %s", got.Aggregation.Values, tc.want)
				}
			})
		})
	}
}

func TestAggregateNumericAndEmptyFields(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, tc := range []struct {
			query, want string
			events      []Event
		}{
			{"packets where protocol = tcp sum(length) over 1s", "134", []Event{{Packet: &PacketEvent{Protocol: "tcp", Length: 120}}, {Packet: &PacketEvent{Protocol: "udp", Length: 1000}}, {Packet: &PacketEvent{Protocol: "tcp", Length: 14}}}},
			{"disk sum(sectors) over 1s", "19", []Event{{Disk: &DiskEvent{Sectors: 8}}, {Disk: &DiskEvent{Sectors: 11}}}},
			{"process count_distinct(name) over 1s", "2", []Event{{Process: &ProcessEvent{Name: "worker"}}, {Process: &ProcessEvent{Name: "other"}}, {Process: &ProcessEvent{Name: "worker"}}}},
			{"packets sum(length) over 1s", "0", nil},
			{"process count_distinct(name) over 1s", "0", nil},
			{"packets avg(length) over 1s", "null", nil},
			{"packets min(length) over 1s", "null", nil},
			{"packets max(length) over 1s", "null", nil},
			{"packets percentile(length,95) over 1s", "null", nil},
		} {
			got := aggregateFixture(t, tc.query, tc.events)
			if len(got.Aggregation.Values) != 1 || string(got.Aggregation.Values[0].Value) != tc.want {
				t.Fatalf("%s: %+v, want %s", tc.query, got.Aggregation.Values, tc.want)
			}
		}
		got := aggregateFixture(t, "packets sum(length) over 1s by dst.port", nil)
		if got.Aggregation.Values == nil || len(got.Aggregation.Values) != 0 {
			t.Fatalf("empty grouped values must be []: %+v", got.Aggregation)
		}
	})
	for _, p := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		r := MonitorRequest{Source: "syscalls", Mode: "aggregate", Aggregation: &AggregationRequest{Window: time.Second, Function: "percentile", Field: "pid", Percentile: p}}
		if err := r.Validate(); err == nil {
			t.Fatalf("invalid percentile accepted: %v", p)
		}
	}
}

func TestAggregateRetainedValueLimit(t *testing.T) {
	for _, function := range []string{"count_distinct", "percentile"} {
		t.Run(function, func(t *testing.T) {
			a := AggregationRequest{Function: function, Field: "pid"}
			if function == "percentile" {
				a.Percentile = 95
			}
			var groups [2]aggregateAccumulator
			retained := 0
			for i := 0; i < 65536; i++ {
				if err := groups[i%2].add(a, i/2, &retained); err != nil {
					t.Fatalf("failed below shared limit: %v", err)
				}
			}
			if function == "count_distinct" {
				if err := groups[0].add(a, 0, &retained); err != nil || retained != 65536 {
					t.Fatalf("duplicate consumed distinct capacity: %d, %v", retained, err)
				}
			}
			if err := groups[0].add(a, 32768, &retained); err == nil || !strings.Contains(err.Error(), "65536 retained values") {
				t.Fatalf("query-wide capacity not enforced: %v", err)
			}
		})
	}
}

func TestAggregatePercentileRanks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var events []Event
		for i := 100; i >= 1; i-- {
			events = append(events, Event{PID: uint32(i)})
		}
		for _, tc := range []struct{ percent, want string }{{"0", "1"}, {"7", "7"}, {"7.01", "8"}, {"50", "50"}, {"100", "100"}} {
			got := aggregateFixture(t, "syscalls percentile(pid,"+tc.percent+") over 1s", events)
			if string(got.Aggregation.Values[0].Value) != tc.want {
				t.Fatalf("percentile %s: %s, want %s", tc.percent, got.Aggregation.Values[0].Value, tc.want)
			}
		}
	})
}

func TestCompletionAggregatesAndFinalCollectionStats(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery("syscalls where phase = completion and syscall = 74 sum(duration_ns) over 1s by pid, name")
		if err != nil {
			t.Fatal(err)
		}
		source := func(ctx context.Context, selection MonitorRequest, emit func(Event) error) error {
			if selection.Phase != "completion" {
				t.Fatal("lost syscall phase")
			}
			for _, duration := range []uint64{7, 19} {
				if err := emit(Event{PID: 42, TID: 43, Name: "writer", Syscall: 74, Phase: "completion", DurationNS: &duration, Collection: &CollectionStats{StackCollisions: 2, StackCaptureFailures: 2}}); err != nil {
					return err
				}
			}
			<-ctx.Done()
			return emit(Event{Kind: "collection_stats", Collection: &CollectionStats{RingBufferDropped: 5, StackCollisions: 3, StackCaptureFailures: 4}})
		}
		got, err := aggregateEvents(context.Background(), r, source)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Aggregation.Values) != 1 || string(got.Aggregation.Values[0].Value) != "26" || string(got.Aggregation.Values[0].Group["name"]) != `"writer"` {
			t.Fatalf("wrong duration/identity aggregate: %+v", got.Aggregation)
		}
		stats := got.Aggregation.Collection
		if stats == nil || stats.RingBufferDropped != 5 || stats.StackCollisions != 3 || stats.StackCaptureFailures != 4 {
			t.Fatalf("lost final counters or added cumulative counters: %+v", stats)
		}
	})
	for _, query := range []string{"syscalls sum(duration_ns) over 1s", "syscalls where phase = entry avg(return_value) over 1s", "syscalls where phase = exit", "disk where phase = exit"} {
		if _, err := ParseMonitorQuery(query); err == nil {
			t.Fatalf("invalid phase query accepted: %s", query)
		}
	}
	for _, query := range []string{"syscalls count over 1s by name", "disk count over 1s by pid,tid,name", "tracepoint where event = sched:sched_switch and fields in (prev_pid) count over 1s by pid,tid,name"} {
		if _, err := ParseMonitorQuery(query); err != nil {
			t.Fatalf("identity query rejected: %s: %v", query, err)
		}
	}
}

func TestHistogramBoundsAndBudget(t *testing.T) {
	// Explicit edges distinguish half-open boundaries, signed values and
	// full-width integers from a floating-point or rounded implementation.
	for _, tc := range []struct{ input, lower, upper string }{
		{"1", "1", "1.25"}, {"1.249", "1", "1.25"}, {"1.25", "1.25", "1.5"},
		{"2", "2", "2.5"}, {"0.125", "0.125", "0.15625"},
		{"-1", "-1", "-0.875"}, {"-1.001", "-1.25", "-1"},
		{"-1.25", "-1.25", "-1"}, {"-2", "-2", "-1.75"},
		{"9600000", "8388608", "10485760"}, {"14000000", "12582912", "14680064"},
		{"18446744073709551615", "16140901064495857664", "18446744073709551616"},
	} {
		n, _ := new(big.Rat).SetString(tc.input)
		bucket := histogramBucket(n)
		lower, _ := new(big.Rat).SetString(bucket.Lower.String())
		upper, _ := new(big.Rat).SetString(bucket.Upper.String())
		wantLower, _ := new(big.Rat).SetString(tc.lower)
		wantUpper, _ := new(big.Rat).SetString(tc.upper)
		if lower.Cmp(wantLower) != 0 || upper.Cmp(wantUpper) != 0 || lower.Cmp(n) > 0 || upper.Cmp(n) <= 0 {
			t.Fatalf("histogram(%s) = %+v, want [%s, %s)", tc.input, bucket, tc.lower, tc.upper)
		}
	}
	var accumulator aggregateAccumulator
	retained := maxAggregateValues - 1
	a := AggregationRequest{Function: "hist", Field: "pid"}
	for _, value := range []int{0, 1, 1} {
		if err := accumulator.add(a, value, &retained); err != nil {
			t.Fatal(err)
		}
	}
	if retained != maxAggregateValues {
		t.Fatal("zero/repeated buckets must not retain extra slots")
	}
	if err := accumulator.add(a, 2, &retained); err == nil {
		t.Fatal("histogram escaped the shared retention budget")
	}
	if got := string(accumulator.value(a)); got != `{"count":3,"zero_count":1,"buckets":[{"lower":1,"upper":1.25,"count":2}]}` {
		t.Fatalf("histogram result: %s", got)
	}
}

func TestHistogramQueryAndSorting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var events []Event
		for _, value := range []string{"-1", "0", "1", "1", "2"} {
			events = append(events, Event{Tracepoint: &TracepointEvent{Event: "custom:sample", Fields: map[string]json.Number{"value": json.Number(value)}}})
		}
		got := aggregateFixture(t, "tracepoint where event = custom:sample and fields in (value) hist(field.value), count over 1s", events)
		var h Histogram
		if err := json.Unmarshal(got.Aggregation.Metrics[0].Values[0].Value, &h); err != nil {
			t.Fatal(err)
		}
		if h.Count != 5 || h.ZeroCount != 1 || len(h.Buckets) != 3 || h.Buckets[0].Count != 1 || h.Buckets[1].Count != 2 || h.Buckets[2].Count != 1 || got.Aggregation.Metrics[1].Counts[0].Count != 5 {
			t.Fatalf("wrong sorted histogram or sibling count: %+v", got.Aggregation)
		}
		empty := aggregateFixture(t, "syscalls hist(pid) over 1s", nil)
		if string(empty.Aggregation.Values[0].Value) != `{"count":0,"zero_count":0,"buckets":[]}` {
			t.Fatal("empty histogram must not be null")
		}
	})
	for _, query := range []string{
		`disk:completion { @h[] = {latency: hist(duration_ns), ops: count()} } after 1s { emit @h order by ops desc limit 10 }`,
		`disk:completion { @h[] = hist(duration_ns) } every 100ms { emit @h; clear @h } after 1s { stop }`,
	} {
		if _, err := ParseMonitorQuery(query); err != nil {
			t.Fatalf("valid histogram query: %v", err)
		}
	}
	for _, query := range []string{
		`disk:completion { @h[] = hist(duration_ns) } after 1s { emit @h order by value desc limit 10 }`,
		`syscalls where result.limit = 1 hist(pid), count over 1s`,
	} {
		if _, err := ParseMonitorQuery(query); err == nil || !strings.Contains(err.Error(), "sort metric") {
			t.Fatalf("histogram sort should fail explicitly: %v", err)
		}
	}
}

func TestPeriodicValidationNamesFailedConstraint(t *testing.T) {
	for _, tc := range []struct {
		source           string
		window, interval time.Duration
		want             string
	}{
		{"cgroups", time.Second, time.Second, "cgroups is a sampled snapshot source"},
		{"syscalls", time.Second, 99 * time.Millisecond, "at least 100ms"},
		{"syscalls", time.Second, 2 * time.Second, "cannot exceed"},
		{"syscalls", 6500 * time.Millisecond, 100 * time.Millisecond, "maximum of 64 buckets"},
	} {
		a := AggregationRequest{Window: tc.window, ReportEvery: tc.interval}
		err := a.validate(MonitorRequest{Source: tc.source})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected %q, got %v", tc.source, tc.want, err)
		}
	}
	if err := (AggregationRequest{Window: 6400 * time.Millisecond, ReportEvery: 100 * time.Millisecond}).validate(MonitorRequest{Source: "syscalls"}); err != nil {
		t.Fatalf("exactly 64 buckets rejected: %v", err)
	}
}

func TestAggregateStackCoverageAcrossBuckets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery("syscalls where syscall = 2 and stacks = both count, sum(pid) over 3s by pid, user.stack")
		if err != nil {
			t.Fatal(err)
		}
		r.Aggregation.ReportEvery, r.Aggregation.Limit = time.Second, 1
		r.Stacks.UserShape = &StackShape{Top: 1}
		got, err := aggregateEvents(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
			for _, event := range []Event{
				{PID: 1, Syscall: 2, UserStack: &CapturedStack{Error: "failed", CaptureErrorCode: -14}, KernelStack: &CapturedStack{Frames: []SymbolFrame{{Name: "kernel"}}}},
				{PID: 2, Syscall: 2, UserStack: &CapturedStack{Frames: []SymbolFrame{{Name: "caller"}, {Address: "0x2"}}, DepthLimitReached: true}, KernelStack: &CapturedStack{Error: "collision", CaptureErrorCode: -17}},
				{PID: 99, Syscall: 3}, // rejected by the predicate
				{Kind: "collection_stats", Collection: &CollectionStats{StackCaptureFailures: 50}},
			} {
				if err := emit(event); err != nil {
					return err
				}
			}
			time.Sleep(time.Second)
			if err := emit(Event{PID: 3, Syscall: 2, UserStack: &CapturedStack{Frames: []SymbolFrame{{Name: "one"}, {Name: "two"}}}}); err != nil {
				return err
			}
			time.Sleep(2 * time.Second)
			if err := emit(Event{PID: 4, Syscall: 2}); err != nil { // excluded at the end boundary
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []*StackCoverageReport{
			{User: &StackCoverage{Events: 2, Captured: 1, CaptureFailures: 1, HelperErrors: map[string]uint64{"-14": 1}, DepthLimitReached: 1, Frames: 2, NamedFrames: 1, UnresolvedFrames: 1, PartiallySymbolized: 1},
				Kernel: &StackCoverage{Events: 2, Captured: 1, CaptureFailures: 1, HelperErrors: map[string]uint64{"-17": 1}, Frames: 1, NamedFrames: 1, FullySymbolized: 1}},
			{User: &StackCoverage{Events: 1, Captured: 1, Frames: 2, NamedFrames: 2, FullySymbolized: 1}, Kernel: &StackCoverage{Events: 1, Missing: 1}},
			{User: &StackCoverage{}, Kernel: &StackCoverage{}},
		}
		if len(got.Windows) != len(want) || len(got.Windows[0].Rows) != 1 || got.Windows[0].TotalGroups != 2 {
			t.Fatalf("expected limited multi-metric buckets: %+v", got)
		}
		data, err := json.Marshal(got)
		var decoded Snapshot
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		for i, window := range decoded.Windows {
			if !reflect.DeepEqual(window.StackCoverage, want[i]) {
				t.Fatalf("bucket %d coverage duplicated/lost or shaped: %+v", i, window.StackCoverage)
			}
		}
		if decoded.Windows[0].Collection != nil || decoded.Windows[2].Collection.StackCaptureFailures != 50 {
			t.Fatal("subscription-wide capture counters must remain separate from received-event coverage")
		}
	})
}

func TestOneShotStackCoverageWireFormats(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, query := range []string{
			"syscalls where stacks = user count over 1s",
			"syscalls where stacks = user count, sum(pid) over 1s",
			`syscalls { @s[caller: stack.user(top: 1)] = {ops: count()} } after 1s { emit @s }`,
		} {
			got := aggregateFixture(t, query, []Event{{PID: 7, UserStack: &CapturedStack{Frames: []SymbolFrame{{Name: "a"}, {Address: "0x1"}}}}})
			data, err := json.Marshal(got)
			var decoded Snapshot
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			want := &StackCoverageReport{User: &StackCoverage{Events: 1, Captured: 1, Frames: 2, NamedFrames: 1, UnresolvedFrames: 1, PartiallySymbolized: 1}}
			if !reflect.DeepEqual(decoded.Aggregation.StackCoverage, want) {
				t.Fatalf("%s lost user-only coverage: %+v", query, decoded.Aggregation.StackCoverage)
			}
			for _, metric := range decoded.Aggregation.Metrics {
				if metric.StackCoverage != nil {
					t.Fatal("coverage must not be duplicated per metric")
				}
			}
		}
		plain := aggregateFixture(t, "syscalls count over 1s", nil)
		if plain.Aggregation.StackCoverage != nil {
			t.Fatal("stack coverage should be omitted when capture is disabled")
		}
	})
}
