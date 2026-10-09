package query

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func TestSampledQueryValidationAndProof(t *testing.T) {
	for _, query := range []string{
		"memory avg(used) over 30s", "memory count over 30s",
		"CPU AVG(utilization_percent) OVER 30s EVERY 500ms BY name",
		"network where name = eth* avg(bytes_recv_per_second) over 1m every 2s by name",
		"gpu percentile(temperature_celsius,95) over 5s every 100ms by uuid",
		"kernel max(counters.processes_running) over 10s every 1s",
		"containers count_distinct(id) over 10s every 1s",
		"process where name = worker* count over 10s every 1s by name",
		"process avg(cpu_percent) over 5s every 1s by pid,name",
		"process max(rss_bytes) over 5s every 1s by pid,name",
		"process avg(threads) over 5s every 1s by user,state",
	} {
		r, err := ParseMonitorQuery(query)
		if err != nil || !sampledAggregation(r) {
			t.Fatalf("sample query %q: %+v, %v", query, r, err)
		}
	}
	for _, query := range []string{
		"memory avg(used) over 1s every 0s", "memory avg(used) over 1s every -1s",
		"memory avg(used) over 1s every nope", "memory avg(used) over 1s every 99ms",
		"memory avg(used) over 1s every 2s", "memory avg(used) over 500ms",
		"cpu avg(user) over 30s", "network sum(bytes_sent) over 30s",
		"cpu avg(utilization_percent) over 1s every 1s", "process where action = start count over 1s every 100ms",
		"cpu count over 1s every 1s by utilization_percent",
		"process avg(cpu_seconds) over 5s every 1s", "process avg(cpu_percent) over 5s",
		"process avg(cpu_percent) over 1s every 1s", "process sum(pid) over 5s every 1s",
		"process count_distinct(action) over 1s every 100ms", "packets count over 1s every 100ms",
		"syscalls count over 1s every 100ms", "capabilities count over 1s every 100ms",
	} {
		if _, err := ParseMonitorQuery(query); err == nil {
			t.Fatalf("invalid sampled query accepted: %q", query)
		}
	}
	event, err := ParseMonitorQuery("process count over 1s by action")
	if err != nil || sampledAggregation(event) {
		t.Fatal("legacy process event aggregate changed")
	}
	r, err := ParseMonitorQuery("memory avg(used) over 3s every 500ms")
	if err != nil || r.Aggregation.Every != 500*time.Millisecond {
		t.Fatal("every not parsed")
	}

}

func sampledFixture(t *testing.T, query string, snapshots []Snapshot) Snapshot {
	t.Helper()
	r, err := ParseMonitorQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	got, err := aggregateSnapshots(context.Background(), r, func(_ context.Context, selection MonitorRequest) (Snapshot, error) {
		if selection.Mode != "snapshot" || selection.Aggregation != nil {
			t.Fatal("sampler did not request snapshots")
		}
		if index >= len(snapshots) {
			t.Fatalf("unexpected sample %d", index)
		}
		snapshot := snapshots[index]
		snapshot.Source = r.Source
		index++
		return snapshot, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if index != len(snapshots) {
		t.Fatalf("collected %d snapshots, want %d", index, len(snapshots))
	}
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

func TestSampledGaugeFunctions(t *testing.T) {
	for _, tc := range []struct{ metric, want string }{
		{"count", "3"}, {"avg(temperature_celsius)", "4.5"},
		{"sum(temperature_celsius)", "13.5"}, {"min(temperature_celsius)", "1.25"},
		{"max(temperature_celsius)", "9.5"}, {"percentile(temperature_celsius,50)", "2.75"},
		{"count_distinct(temperature_celsius)", "3"},
	} {
		synctest.Test(t, func(t *testing.T) {
			got := sampledFixture(t, "sensors "+tc.metric+" over 3s every 1s by name", []Snapshot{
				{Sensors: []SensorInfo{{Name: "chip", Temperature: 1.25}}},
				{Sensors: []SensorInfo{{Name: "chip", Temperature: 9.5}}},
				{Sensors: []SensorInfo{{Name: "chip", Temperature: 2.75}}},
			})
			if tc.metric == "count" {
				if len(got.Aggregation.Counts) != 1 || got.Aggregation.Counts[0].Count != 3 {
					t.Fatal("wrong sampled record count")
				}
			} else if len(got.Aggregation.Values) != 1 || string(got.Aggregation.Values[0].Value) != tc.want {
				t.Fatalf("%s: %+v, want %s", tc.metric, got.Aggregation.Values, tc.want)
			}
			if got.Aggregation.Every != time.Second || got.Aggregation.End.Sub(got.Aggregation.Start) != 3*time.Second {
				t.Fatal("incorrect sampled window")
			}
		})
	}
	synctest.Test(t, func(t *testing.T) {
		got := sampledFixture(t, "memory avg(used) over 2s", []Snapshot{{Memory: &MemoryInfo{Used: ^uint64(0) - 2}}, {Memory: &MemoryInfo{Used: ^uint64(0)}}})
		if string(got.Aggregation.Values[0].Value) != "18446744073709551614" {
			t.Fatal("lost full-width integer precision")
		}
	})
}

func TestSampledCPUUtilization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		got := sampledFixture(t, "cpu avg(utilization_percent) over 3s every 1s by name", []Snapshot{
			{CPU: []CPUInfo{{Name: "cpu0", Total: 1000, Idle: 800, IOWait: 50}, {Name: "cpu1", Total: 2000, Idle: 1500, IOWait: 100}}},
			{CPU: []CPUInfo{{Name: "cpu1", Total: 2010, Idle: 1507, IOWait: 102}, {Name: "cpu0", Total: 1010, Idle: 804, IOWait: 51}}},
			{CPU: []CPUInfo{{Name: "cpu0", Total: 1030, Idle: 808, IOWait: 52}, {Name: "cpu1", Total: 2030, Idle: 1517, IOWait: 104}}},
		})
		values := got.Aggregation.Values
		if len(values) != 2 || string(values[0].Group["name"]) != `"cpu0"` || string(values[0].Value) != "62.5" || string(values[1].Value) != "25" {
			t.Fatalf("incorrect utilization or CPU identity: %+v", values)
		}
	})
}

func TestSampledProcessMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cpu := []float64{10, 100, 11.5, 100.25, 50, 100.75, 52, 101.5}
		rss := []uint64{100, 1000, 300, 500, 200, 2000, 400, 1000}
		var snapshots []Snapshot
		for i := 0; i < 4; i++ {
			started := time.Unix(1, 0)
			if i >= 2 {
				started = time.Unix(2, 0) // Reused PID with a larger counter must still start a new baseline.
			}
			snapshots = append(snapshots, Snapshot{Processes: []ProcessInfo{
				{PID: 71, Name: "worker", Started: started, CPUSeconds: &cpu[2*i], RSSBytes: &rss[2*i]},
				{PID: 72, Name: "worker", Started: time.Unix(1, 0), CPUSeconds: &cpu[2*i+1], RSSBytes: &rss[2*i+1]},
			}})
		}
		for _, tc := range []struct{ metric, first, second string }{
			{"avg(cpu_percent)", "175", "50"},
			{"avg(rss_bytes)", "250", "1125"},
		} {
			got := sampledFixture(t, "process "+tc.metric+" over 4s every 1s by pid,name", snapshots)
			values := got.Aggregation.Values
			if len(values) != 2 || string(values[0].Group["pid"]) != "71" || string(values[0].Value) != tc.first || string(values[1].Value) != tc.second {
				t.Fatalf("%s lost scale, PID identity, or precision: %+v", tc.metric, values)
			}
		}
		missing := sampledFixture(t, "process avg(cpu_percent) over 3s every 1s", []Snapshot{
			{Processes: []ProcessInfo{{PID: 71, Started: time.Unix(1, 0), CPUSeconds: &cpu[0]}}},
			{Processes: []ProcessInfo{{PID: 71, Started: time.Unix(1, 0)}}},
			{Processes: []ProcessInfo{{PID: 71, Started: time.Unix(1, 0), CPUSeconds: &cpu[2]}}},
		})
		if string(missing.Aggregation.Values[0].Value) != "null" {
			t.Fatal("missing CPU counter must restart the baseline, not fabricate a measurement")
		}
	})
	at := time.Now()
	fields := map[string]any{"cpu_seconds": json.Number("13")}
	deriveSample(fields, sampleObservation{map[string]any{"cpu_seconds": json.Number("10")}, at}, at.Add(2*time.Second), snapshotSampleFields("process"))
	if fields["cpu_percent"] != json.Number("150.000000000000000000") {
		t.Fatal("process CPU did not use actual elapsed time or was clamped to 100%")
	}
}

func TestCounterRatesAndLifecycles(t *testing.T) {
	at := time.Now()
	metadata := snapshotSampleFields("network")
	previous := sampleObservation{map[string]any{"bytes_recv": json.Number("18446744073709551500")}, at}
	current := map[string]any{"bytes_recv": json.Number("18446744073709551510")}
	deriveSample(current, previous, at.Add(2*time.Second), metadata)
	if current["bytes_recv_per_second"] != json.Number("5.000000000000000000") {
		t.Fatal("rate used nominal interval or lost counter precision")
	}
	reset := map[string]any{"bytes_recv": json.Number("2")}
	deriveSample(reset, previous, at.Add(time.Second), metadata)
	if reset["bytes_recv_per_second"] != nil {
		t.Fatal("counter reset became a negative/huge rate")
	}
	synctest.Test(t, func(t *testing.T) {
		got := sampledFixture(t, "network avg(bytes_recv_per_second) over 6s every 1s by name", []Snapshot{
			{Network: []InterfaceInfo{{Name: "eth0", Index: 7, BytesRecv: 100}}},
			{Network: []InterfaceInfo{{Name: "eth0", Index: 7, BytesRecv: 110}}},
			{Network: nil}, // Disappearance must discard the old baseline.
			{Network: []InterfaceInfo{{Name: "eth0", Index: 7, BytesRecv: 1000}}},
			{Network: []InterfaceInfo{{Name: "eth0", Index: 7, BytesRecv: 2}}}, // Reset is a new baseline.
			{Network: []InterfaceInfo{{Name: "eth0", Index: 7, BytesRecv: 22}}},
		})
		if len(got.Aggregation.Values) != 1 || string(got.Aggregation.Values[0].Value) != "15" {
			t.Fatalf("bad reset/missing semantics: %+v", got.Aggregation.Values)
		}
	})
}

func TestSampledMissingFailuresAndSlowReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		value := 40.0
		got := sampledFixture(t, "gpu avg(utilization_percent) over 3s every 1s by uuid", []Snapshot{{GPUs: []GPUInfo{{UUID: "gpu-a"}}}, {GPUs: []GPUInfo{{UUID: "gpu-a", Utilization: &value}}}, {GPUs: []GPUInfo{{UUID: "gpu-a"}}}})
		if string(got.Aggregation.Values[0].Value) != "40" {
			t.Fatal("missing GPU metric counted as zero")
		}
		empty := sampledFixture(t, "cpu avg(utilization_percent) over 2s by name", []Snapshot{{}, {}})
		if empty.Aggregation.Values == nil || len(empty.Aggregation.Values) != 0 {
			t.Fatal("empty grouped sample result must be []")
		}
		null := sampledFixture(t, "gpu avg(power_watts) over 1s", []Snapshot{{GPUs: []GPUInfo{{UUID: "gpu-a"}}}})
		if string(null.Aggregation.Values[0].Value) != "null" {
			t.Fatal("missing ungrouped metric must be null")
		}
		r, _ := ParseMonitorQuery("memory avg(used) over 5s every 1s")
		failure := errors.New("collector failed")
		if _, err := aggregateSnapshots(context.Background(), r, func(context.Context, MonitorRequest) (Snapshot, error) { return Snapshot{}, failure }); !errors.Is(err, failure) {
			t.Fatal("collector failure hidden")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if _, err := aggregateSnapshots(ctx, r, func(context.Context, MonitorRequest) (Snapshot, error) {
			return Snapshot{Source: "memory", Memory: &MemoryInfo{Used: 7}}, nil
		}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation returned a partial success: %v", err)
		}
		start := time.Now()
		var calls []time.Duration
		got, err := aggregateSnapshots(context.Background(), r, func(ctx context.Context, _ MonitorRequest) (Snapshot, error) {
			calls = append(calls, time.Since(start))
			select {
			case <-time.After(1500 * time.Millisecond):
			case <-ctx.Done():
				return Snapshot{}, ctx.Err()
			}
			return Snapshot{Source: "memory", Memory: &MemoryInfo{Used: 7}}, nil
		})
		if err != nil || !reflect.DeepEqual(calls, []time.Duration{0, 2 * time.Second, 4 * time.Second}) || string(got.Aggregation.Values[0].Value) != "7" {
			t.Fatalf("slow collection caused catch-up: %v, %v", calls, err)
		}
	})
}

func TestCounterRateObservedTimeAndGroupedTotals(t *testing.T) {
	start := time.Unix(100, 0)
	metadata := snapshotSampleFields("network")
	a := AggregationRequest{Function: "rate", Field: "bytes_recv"}
	reduction := newAggregateReduction(a)
	previous := make(map[string]sampleObservation)
	// Two counters share a group. Unequal intervals must be time-weighted,
	// overlapping intervals must count time once, and gaps must not count.
	for _, sample := range []struct {
		second int
		values []string
	}{
		{0, []string{"100", "1000"}},
		{1, []string{"110", "1030"}},
		{4, []string{"170", "1060"}},
		{5, nil},
		{7, []string{"500", "2000"}},
		{9, []string{"520", "2030"}},
	} {
		current := make(map[string]sampleObservation)
		at := start.Add(time.Duration(sample.second) * time.Second)
		for i, value := range sample.values {
			id := []string{"a", "b"}[i]
			fields := map[string]any{"bytes_recv": json.Number(value)}
			deriveSample(fields, previous[id], at, metadata)
			if err := reduction.addSample(fields); err != nil {
				t.Fatal(err)
			}
			current[id] = sampleObservation{fields, at}
		}
		previous = current
	}
	got := reduction.result("network", start, start.Add(10*time.Second)).Aggregation
	// 40 + 90 + 50 bytes over 1 + 3 + 2 observed seconds = 30 B/s.
	if string(got.Values[0].Value) != "30" {
		t.Fatalf("rate must sum concurrent counters over actual valid time: %+v", got)
	}
	// Full-width integer deltas must not be rounded before subtraction.
	fields := map[string]any{"bytes_recv": json.Number("18446744073709551615")}
	deriveSample(fields, sampleObservation{map[string]any{"bytes_recv": json.Number("18446744073709551614")}, start}, start.Add(time.Second), metadata)
	interval := fields["__counter_delta.bytes_recv"].(counterInterval)
	if interval.delta.Cmp(big.NewRat(1, 1)) != 0 {
		t.Fatal("counter delta lost integer precision")
	}
}

func TestCounterRateResetsAndIdentity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		got := sampledFixture(t, "network rate(bytes_recv), count over 7s every 1s by name", []Snapshot{
			{Network: []InterfaceInfo{{Name: "eth0", Index: 1, BytesRecv: 100}}},
			{Network: []InterfaceInfo{{Name: "eth0", Index: 1, BytesRecv: 110}}},
			{Network: []InterfaceInfo{{Name: "eth0", Index: 2, BytesRecv: 1000}}}, // Recreated interface.
			{Network: []InterfaceInfo{{Name: "eth0", Index: 2, BytesRecv: 2}}},    // Reset.
			{},
			{Network: []InterfaceInfo{{Name: "eth0", Index: 2, BytesRecv: 500}}}, // Reappearing baseline.
			{Network: []InterfaceInfo{{Name: "eth0", Index: 2, BytesRecv: 520}}},
		})
		if string(got.Aggregation.Metrics[0].Values[0].Value) != "15" || got.Aggregation.Metrics[1].Counts[0].Count != 6 {
			t.Fatalf("rate counted resets/identity changes/gaps or dropped count baselines: %+v", got.Aggregation)
		}
		empty := sampledFixture(t, "network rate(bytes_recv) over 2s every 1s", []Snapshot{{}, {}})
		if string(empty.Aggregation.Values[0].Value) != "null" {
			t.Fatal("unobserved rate must be null")
		}
	})
	for _, query := range []string{
		"syscalls rate(pid) over 2s", "memory rate(used) over 2s", "network rate(name) over 2s",
		"network rate(bytes_recv_per_second) over 2s", "network rate(bytes_recv) over 1s every 1s",
	} {
		if _, err := ParseMonitorQuery(query); err == nil {
			t.Fatalf("invalid counter rate accepted: %s", query)
		}
	}
}

func TestCgroupCounterRateGuardsDeviceResets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var snapshots []Snapshot
		for _, counters := range [][2]uint64{{100, 200}, {120, 220}, {5, 400}, {15, 440}, {1000, 2000}, {1010, 2020}} {
			total := counters[0] + counters[1]
			id := "1:2"
			if counters[0] >= 1000 {
				id = "1:3" // Recreated group with larger historical counters.
			}
			snapshots = append(snapshots, Snapshot{Cgroups: []CgroupInfo{{Path: "/writer", ID: id, IO: &CgroupIO{WriteIOs: &total, Devices: []CgroupIODevice{
				{Device: "8:0", Counters: map[string]uint64{"wios": counters[0]}},
				{Device: "8:1", Counters: map[string]uint64{"wios": counters[1]}},
			}}}}})
		}
		got := sampledFixture(t, "cgroups rate(io.write_ios) over 6s every 1s by path", snapshots)
		// Only 40, 50 and 30 operations over three valid seconds.
		if string(got.Aggregation.Values[0].Value) != "40" {
			t.Fatalf("hidden device reset or recreated cgroup counted: %+v", got.Aggregation)
		}
	})
}
