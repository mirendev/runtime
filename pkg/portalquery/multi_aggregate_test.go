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

func TestMultipleAggregateQueriesAndProof(t *testing.T) {
	r, err := ParseMonitorQuery("disk count, sum(sectors), percentile(sectors,95) over 30s by pid,device")
	want := []AggregateMetric{{Function: "count"}, {Function: "sum", Field: "sectors"}, {Function: "percentile", Field: "sectors", Percentile: 95}}
	if err != nil || r.Aggregation == nil || !reflect.DeepEqual(r.Aggregation.Metrics, want) || r.Aggregation.Function != "" || r.Aggregation.Window != 30*time.Second || !reflect.DeepEqual(r.Aggregation.GroupBy, []string{"pid", "device"}) {
		t.Fatalf("multi aggregate parse: %+v, %v", r, err)
	}
	for _, text := range []string{
		"disk count, count over 1s", "disk sum(sectors), sum(sectors) over 1s",
		"disk count, avg(nope) over 1s", "disk count, percentile(sectors,NaN) over 1s",
		"disk count, over 1s", "disk count,, sum(sectors) over 1s",
		"memory count, avg(used) over 1s every 99ms",
		"cpu count, avg(utilization_percent) over 1s every 1s",
		"cgroups count, sum(io.write_bytes) over 3s",
		"cgroups count, avg(io.devices[].counters.wbytes) over 3s",
	} {
		if _, err := ParseMonitorQuery(text); err == nil {
			t.Fatalf("invalid multi query accepted: %s", text)
		}
	}
	for _, a := range []AggregationRequest{
		{Window: time.Second, Function: "sum", Field: "sectors", Metrics: want},
		{Window: time.Second, Metrics: append(want, make([]AggregateMetric, 6)...)},
		{Window: time.Second, Metrics: []AggregateMetric{{}, {Function: "count"}}},
	} {
		if err := a.validate(MonitorRequest{Source: "disk"}); err == nil {
			t.Fatal("invalid API metrics accepted")
		}
	}

}

func TestMultipleEventAggregatesShareWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery("tracepoint where event = block:block_rq_issue and fields in (nr_sector) count, sum(field.nr_sector), percentile(field.nr_sector,95) over 3s by pid")
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		got, err := aggregateEvents(context.Background(), r, func(ctx context.Context, selection MonitorRequest, emit func(Event) error) error {
			calls++
			if selection.Aggregation != nil || selection.Mode != "" {
				t.Fatal("aggregate spec leaked to source")
			}
			for i, sectors := range []json.Number{"2", "11", "7"} {
				pid := uint32(42)
				if i == 2 {
					pid = 7
				}
				if err := emit(Event{PID: pid, Tracepoint: &TracepointEvent{Event: "block:block_rq_issue", Fields: map[string]json.Number{"nr_sector": sectors}}}); err != nil {
					return err
				}
			}
			<-ctx.Done()
			// At the boundary, neither metric should include this record.
			if err := emit(Event{PID: 42, Tracepoint: &TracepointEvent{Event: "block:block_rq_issue", Fields: map[string]json.Number{"nr_sector": "1000"}}}); err != nil {
				return err
			}
			return emit(Event{Kind: "collection_stats", Collection: &CollectionStats{RingBufferDropped: 9}})
		})
		if err != nil || calls != 1 || len(got.Aggregation.Metrics) != 3 {
			t.Fatalf("shared collector: %+v, %v (%d calls)", got.Aggregation, err, calls)
		}
		a := got.Aggregation
		if a.Collection == nil || a.Collection.RingBufferDropped != 9 {
			t.Fatal("final loss stats missing")
		}
		for _, metric := range a.Metrics {
			if !metric.Start.Equal(a.Start) || !metric.End.Equal(a.End) || !reflect.DeepEqual(metric.GroupBy, a.GroupBy) {
				t.Fatal("different metric window/grouping")
			}
		}
		counts := make(map[string]uint64)
		for _, count := range a.Metrics[0].Counts {
			counts[string(count.Group["pid"])] = count.Count
		}
		if !reflect.DeepEqual(counts, map[string]uint64{"42": 2, "7": 1}) {
			t.Fatalf("wrong counts: %v", counts)
		}
		for i, expected := range []map[string]string{{"42": "13", "7": "7"}, {"42": "11", "7": "7"}} {
			values := make(map[string]string)
			for _, value := range a.Metrics[i+1].Values {
				values[string(value.Group["pid"])] = string(value.Value)
			}
			if !reflect.DeepEqual(values, expected) {
				t.Fatalf("metric %d: %v", i+1, values)
			}
		}
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip Snapshot
		if err := json.Unmarshal(data, &roundTrip); err != nil || !reflect.DeepEqual(roundTrip.Aggregation, a) {
			t.Fatalf("multi JSON roundtrip: %s, %v", data, err)
		}
	})
}

func TestMultipleMetricsShareRetainedValueBudget(t *testing.T) {
	r := newAggregateReduction(AggregationRequest{Metrics: []AggregateMetric{{Function: "percentile", Field: "a", Percentile: 50}, {Function: "count_distinct", Field: "b"}}})
	*r.retained = maxAggregateValues - 2
	if err := r.add(map[string]any{"a": 3, "b": "first"}); err != nil {
		t.Fatal(err)
	}
	if *r.retained != maxAggregateValues {
		t.Fatal("metrics did not share storage budget")
	}
	if err := r.add(map[string]any{"a": 7, "b": "second"}); err == nil {
		t.Fatal("query-wide budget exceeded silently")
	}
}

func TestMultipleAggregatesEmptyResults(t *testing.T) {
	for _, group := range [][]string{nil, {"pid"}} {
		r := newAggregateReduction(AggregationRequest{GroupBy: group, Metrics: []AggregateMetric{{Function: "count"}, {Function: "avg", Field: "pid"}}})
		a := r.result("syscalls", time.Now(), time.Now()).Aggregation
		data, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		if len(group) != 0 && (!strings.Contains(string(data), `"counts":[]`) || !strings.Contains(string(data), `"values":[]`)) {
			t.Fatalf("empty metrics omitted: %s", data)
		}
		if len(group) == 0 && (a.Metrics[0].Counts[0].Count != 0 || string(a.Metrics[1].Values[0].Value) != "null") {
			t.Fatalf("empty metric semantics: %s", data)
		}
	}
}

func TestCgroupIOMultipleSampledAggregates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var snapshots []Snapshot
		for i, read := range []uint64{100, 130, 110, 150} {
			write := []uint64{1000, 1040, 1090, 1160}[i]
			memory := []uint64{10, 0, 90, 30}[i]
			g := CgroupInfo{Path: "/writer", ID: "1:2", MemoryBytes: &memory, IO: &CgroupIO{ReadBytes: &read, WriteBytes: &write, Devices: []CgroupIODevice{{Device: "8:0", Counters: map[string]uint64{"rbytes": read, "wbytes": write}}}}}
			if i == 1 {
				g.MemoryBytes = nil
			}
			snapshots = append(snapshots, Snapshot{Cgroups: []CgroupInfo{g}})
		}
		got := sampledFixture(t, "cgroups count, avg(memory_bytes), avg(io.read_bytes_per_second), avg(io.write_bytes_per_second) over 4s every 1s by path", snapshots)
		metrics := got.Aggregation.Metrics
		if len(metrics) != 4 || metrics[0].Counts[0].Count != 4 {
			t.Fatalf("count lost initial/optional samples: %+v", metrics)
		}
		for i, want := range []string{"43.333333333333333333", "35", "53.333333333333333333"} {
			if len(metrics[i+1].Values) != 1 || string(metrics[i+1].Values[0].Value) != want {
				t.Fatalf("metric %d: %+v, want %s", i+1, metrics[i+1].Values, want)
			}
		}
		for _, metric := range metrics {
			if metric.Every != time.Second || !metric.Start.Equal(got.Aggregation.Start) || !metric.End.Equal(got.Aggregation.End) {
				t.Fatal("sample metrics disagree on interval/window")
			}
		}
	})
}

func TestCgroupIORatesGuardDeviceChangesAndHiddenResets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var snapshots []Snapshot
		// Total rises during a per-device reset: do not invent 980 B/s.
		for _, devices := range [][]CgroupIODevice{
			{{Device: "8:0", Counters: map[string]uint64{"rbytes": 100}}, {Device: "8:1", Counters: map[string]uint64{"rbytes": 1000}}},
			{{Device: "8:1", Counters: map[string]uint64{"rbytes": 2000}}, {Device: "8:0", Counters: map[string]uint64{"rbytes": 80}}},
			{{Device: "8:0", Counters: map[string]uint64{"rbytes": 90}}, {Device: "8:1", Counters: map[string]uint64{"rbytes": 2030}}},
			{{Device: "8:0", Counters: map[string]uint64{"rbytes": 110}}},
			{{Device: "8:0", Counters: map[string]uint64{"rbytes": 160}}},
		} {
			total := uint64(0)
			for _, device := range devices {
				total += device.Counters["rbytes"]
			}
			snapshots = append(snapshots, Snapshot{Cgroups: []CgroupInfo{{Path: "/writer", ID: "1:2", IO: &CgroupIO{ReadBytes: &total, Devices: devices}}}})
		}
		got := sampledFixture(t, "cgroups avg(io.read_bytes_per_second) over 5s every 1s", snapshots)
		// Only stable monotonic intervals count: 40 and 50 B/s.
		if string(got.Aggregation.Values[0].Value) != "45" {
			t.Fatalf("invalid I/O interval included: %+v", got.Aggregation.Values)
		}
	})
}

func TestCgroupIORatesDistinguishMissingEmptyAndNewDevice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		zero, hundred, later := uint64(0), uint64(100), uint64(125)
		var snapshots []Snapshot
		for _, io := range []*CgroupIO{
			nil,
			{ReadBytes: &zero, Devices: []CgroupIODevice{}},
			{ReadBytes: &zero, Devices: []CgroupIODevice{}},
			{ReadBytes: &hundred, Devices: []CgroupIODevice{{Device: "8:0", Counters: map[string]uint64{"rbytes": hundred}}}},
			{ReadBytes: &later, Devices: []CgroupIODevice{{Device: "8:0", Counters: map[string]uint64{"rbytes": later}}}},
		} {
			snapshots = append(snapshots, Snapshot{Cgroups: []CgroupInfo{{Path: "/writer", ID: "1:2", IO: io}}})
		}
		got := sampledFixture(t, "cgroups avg(io.read_bytes_per_second) over 5s every 1s", snapshots)
		// Empty available I/O contributes zero; a new device starts a baseline.
		// Only 0 and 25 B/s are usable, not the new device's historical 100 bytes.
		if string(got.Aggregation.Values[0].Value) != "12.5" {
			t.Fatalf("empty/missing/new device rates conflated: %+v", got.Aggregation.Values)
		}
	})
}
