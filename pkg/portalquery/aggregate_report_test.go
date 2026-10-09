package query

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestRollupAndComputedScript(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		query := `disk { @d[who: pid, op: operation] = {ops: count(), sectors: sum(sectors), mean: avg(sectors), p50: percentile(sectors, 50), kinds: count_distinct(sectors), sizes: hist(sectors)} }
syscalls { @s[who: pid, call: syscall] = {ops: count()} }
after 2s {
  emit @d rollup by who full join @s rollup by who on who select ops_per_s = d.ops / window.seconds, kb = d.sectors * 512 / 1024, ratio = d.ops / s.ops order by kb desc limit 1;
  emit @d rollup by who;
  emit @d
}`
		r, err := ParseMonitorQuery(query)
		if err != nil {
			t.Fatal(err)
		}
		var subscriptions atomic.Int32
		got, err := aggregateScript(context.Background(), r, func(ctx context.Context, selection MonitorRequest, emit func(Event) error) error {
			subscriptions.Add(1)
			if selection.Source == "disk" {
				for _, row := range []struct {
					pid, sectors uint32
					op           string
				}{
					{1, 2, "read"}, {1, 2, "read"}, {1, 8, "write"}, {1, 100, "write"}, {1, 8, "write"}, {2, 500, "read"},
				} {
					if err := emit(Event{PID: row.pid, Disk: &DiskEvent{Operation: row.op, Sectors: row.sectors}}); err != nil {
						return err
					}
				}
			} else {
				for _, event := range []Event{{PID: 1, Syscall: 2}, {PID: 1, Syscall: 3}, {PID: 3, Syscall: 4}} {
					if err := emit(event); err != nil {
						return err
					}
				}
			}
			<-ctx.Done()
			return ctx.Err()
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if subscriptions.Load() != 2 || len(got.Tables) != 3 {
			t.Fatalf("rollup duplicated subscriptions: %+v", got)
		}
		joined := got.Tables[0].Aggregation
		if joined.TotalGroups != 3 || len(joined.Rows) != 1 || string(joined.Rows[0].Group["who"]) != "2" {
			t.Fatalf("projection sorting/limit did not run after rollup/join: %+v", joined)
		}
		if string(joined.Rows[0].Values[7]) != "0.5" || string(joined.Rows[0].Values[8]) != "250" || string(joined.Rows[0].Values[9]) != "null" {
			t.Fatalf("rates, unit conversion, missing-join arithmetic: %+v", joined.Rows[0])
		}
		coarse := got.Tables[1].Aggregation
		if len(coarse.Rows) != 2 || !reflect.DeepEqual(coarse.GroupBy, []string{"who"}) || len(got.Tables[2].Aggregation.Rows) != 3 {
			t.Fatal("rollup must preserve original detailed table and deduplicate repeated projections")
		}
		for _, row := range coarse.Rows {
			if string(row.Group["who"]) != "1" {
				continue
			}
			// Mean of subgroup means is 61/3, not 24; averaging subgroup p50s is wrong too.
			for i, want := range []string{"5", "120", "24", "8", "3"} {
				if string(row.Values[i]) != want {
					t.Fatalf("metric %d = %s, want %s", i, row.Values[i], want)
				}
			}
			var histogram Histogram
			if err := json.Unmarshal(row.Values[5], &histogram); err != nil || histogram.Count != 5 || len(histogram.Buckets) != 3 {
				t.Fatalf("histogram rollup: %+v, %v", histogram, err)
			}
		}
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"kb":250`) || strings.Contains(string(data), `"rollups":`) {
			t.Fatalf("computed wire shape/private state: %s", data)
		}
		var decoded Snapshot
		if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded.Tables[0].Aggregation.Rows, joined.Rows) {
			t.Fatalf("computed join wire round trip: %v", err)
		}
	})
}

func TestComputedPrecedenceNullsAndPrecision(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`tracepoint:custom:sample { @a[who: pid] = {total: sum(field.value), ops: count()} }
after 1s { emit @a select precise = total + 1, precedence = 10 - 6 / 2 * 3 + 1, parens = -(10 - 6) * .5, zero = total / (ops - 1), chained = precise - total, associate = 20 / 2 / 5 }`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := aggregateScript(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
			if err := emit(Event{PID: 7, Tracepoint: &TracepointEvent{Event: "custom:sample", Fields: map[string]json.Number{"value": "9007199254740993"}}}); err != nil {
				return err
			}
			<-ctx.Done()
			return nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		row := got.Tables[0].Aggregation.Rows[0]
		for i, want := range []string{"9007199254740993", "1", "9007199254740994", "2", "-2", "null", "1", "2"} {
			if string(row.Values[i]) != want {
				t.Fatalf("expression %d = %s, want %s", i, row.Values[i], want)
			}
		}
	})
}

func TestPeriodicRollupUsesBucketDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`syscalls { @a[who: pid, call: syscall] = {ops: count()} }
every 1s { emit @a rollup by who select per_s = ops / window.seconds; clear @a } after 1500ms { stop }`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := aggregateScript(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
			for _, call := range []int{2, 3} {
				if err := emit(Event{PID: 1, Syscall: call}); err != nil {
					return err
				}
			}
			time.Sleep(time.Second)
			if err := emit(Event{PID: 2, Syscall: 3}); err != nil {
				return err
			}
			<-ctx.Done()
			return nil
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		windows := got.Tables[0].Windows
		if len(windows) != 2 || string(windows[0].Rows[0].Values[0]) != "2" || string(windows[1].Rows[0].Group["who"]) != "2" || string(windows[1].Rows[0].Values[1]) != "2" {
			t.Fatalf("independent buckets and half-second final bucket: %+v", windows)
		}
	})
}

func TestReportSyntaxValidationAndSigning(t *testing.T) {
	base := `syscalls { @a[who: pid, call: syscall] = {ops: count(), total: sum(pid)} } `
	for _, suffix := range []string{
		`after 1s { emit @a rollup by who }`,
		`after 1s { emit @a rollup by call, who select n = ops / window.seconds order by n asc limit 2 }`,
	} {
		if _, err := ParseMonitorQuery(base + suffix); err != nil {
			t.Fatalf("valid report: %s: %v", suffix, err)
		}
	}
	for _, tc := range []struct{ suffix, want string }{
		{`after 1s { emit @a rollup by nope }`, "rollup key"},
		{`after 1s { emit @a rollup by who, who }`, "unique grouping"},
		{`after 1s { emit @a select x = nope / 2 }`, "unknown or ambiguous"},
		{`after 1s { emit @a select ops = total / 2 }`, "conflicts"},
		{`after 1s { emit @a select x = x / 2 }`, "unknown or ambiguous"},
		{`after 1s { emit @a select window = 1 }`, "excluding window"},
	} {
		if _, err := ParseMonitorQuery(base + tc.suffix); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("want %q: %v", tc.want, err)
		}
	}
	if _, err := ParseMonitorQuery(`syscalls { @a[who: pid] = hist(pid) } after 1s { emit @a select x = value / 2 }`); err == nil || !strings.Contains(err.Error(), "histogram") {
		t.Fatalf("histogram arithmetic: %v", err)
	}
	_, err := ParseMonitorQuery(base + `after 1s { emit @a rollup by who select x = ops / 2 }`)
	if err != nil {
		t.Fatal(err)
	}

}

func TestSampledRollupPreservesObservedRateTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`network { @n[who: name, iface: index] = {throughput: rate(bytes_recv), samples: count()} }
after 4s { emit @n rollup by who; emit @n }`)
		if err != nil {
			t.Fatal(err)
		}
		var calls int
		got, err := aggregateScript(context.Background(), r, nil, func(context.Context, MonitorRequest) (Snapshot, error) {
			rows := [][]InterfaceInfo{
				{{Name: "net", Index: 1, BytesRecv: 100}},
				{{Name: "net", Index: 1, BytesRecv: 110}, {Name: "net", Index: 2, BytesRecv: 1000}},
				{{Name: "net", Index: 2, BytesRecv: 1030}},
				{{Name: "net", Index: 2, BytesRecv: 1060}},
			}
			row := rows[calls]
			calls++
			return Snapshot{Source: "network", Network: row}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		coarse := got.Tables[0].Aggregation
		// 10 + 30 + 30 bytes / 3 observed seconds, not 10 + 30 B/s.
		if calls != 4 || len(coarse.Rows) != 1 || string(coarse.Rows[0].Values[0]) != "23.333333333333333333" || string(coarse.Rows[0].Values[1]) != "5" || len(got.Tables[1].Aggregation.Rows) != 2 {
			t.Fatalf("coarse rates lost observation timing or baselines: calls=%d result=%+v", calls, coarse)
		}
	})
}

func TestRollupBudgetsAndComputedServerValidation(t *testing.T) {
	r, err := ParseMonitorQuery(`syscalls { @a[who: pid, call: syscall] = percentile(pid, 99) } after 1s { emit @a rollup by who }`)
	if err != nil {
		t.Fatal(err)
	}
	reduction := newAggregateReduction(*r.Probes[0].Aggregation)
	reduction.configureRollups([][]string{{"who"}, {"who"}})
	*reduction.retained = maxAggregateValues - 2
	if err := reduction.add(map[string]any{"pid": 1, "syscall": 2}); err != nil || *reduction.retained != maxAggregateValues || len(reduction.rollups) != 1 {
		t.Fatalf("dedup/shared rollup budget: %v", err)
	}
	if err := reduction.add(map[string]any{"pid": 1, "syscall": 3}); err == nil {
		t.Fatal("rollup escaped retention cap")
	}
	r, err = ParseMonitorQuery(`syscalls { @a[who: pid] = count() } after 1s { emit @a select n = value / 2 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []AggregateExpression{
		{Op: "number", Value: "1/2"}, {Op: "number", Value: strings.Repeat("9", 129)},
		{Op: "/", Args: []AggregateExpression{{Op: "number", Value: "2"}}},
		{Op: "field", Value: "missing"}, {Op: "exec", Value: "anything"},
		{Op: "field", Value: "value", Args: []AggregateExpression{{Op: "number", Value: "2"}}},
	} {
		r.Reports[0].Select[0].Expression = e
		if err := r.Validate(); err == nil {
			t.Fatalf("malformed API expression accepted: %+v", e)
		}
	}
	deep := AggregateExpression{Op: "number", Value: "1"}
	for range 16 {
		deep = AggregateExpression{Op: "neg", Args: []AggregateExpression{deep}}
	}
	r.Reports[0].Select[0].Expression = deep
	if err := r.Validate(); err == nil || !strings.Contains(err.Error(), "depth 16") {
		t.Fatalf("unbounded expression depth: %v", err)
	}
	var terms []string
	for range 10 {
		terms = append(terms, strings.Repeat("9", 128))
	}
	r, err = ParseMonitorQuery(`syscalls { @a[] = count() } after 1s { emit @a select n = ` + strings.Join(terms, " * ") + ` }`)
	if err != nil {
		t.Fatal(err)
	}
	input := &AggregationResult{Columns: []AggregateMetric{{Name: "value", Function: "count"}}, Rows: []AggregateRow{{Values: []json.RawMessage{json.RawMessage("1")}}}}
	if err := applyComputedColumns(r.Reports[0].Select, input); err == nil || !strings.Contains(err.Error(), "4096-bit") {
		t.Fatalf("unbounded computed result: %v", err)
	}
}

func TestReportDocumentedExample(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "#### Rollups and computed columns")
	if !ok {
		t.Fatal("missing report documentation")
	}
	_, query, ok := strings.Cut(section, "miren runner query node-a '")
	if !ok {
		t.Fatal("missing report example")
	}
	query, _, ok = strings.Cut(query, "'\n```")
	if !ok {
		t.Fatal("unterminated report example")
	}
	if _, err := ParseMonitorQuery(query); err != nil {
		t.Fatalf("documented query does not parse: %v", err)
	}
}
