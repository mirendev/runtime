package query

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

const joinSelectors = `syscalls { @a[proc: pid] = {ops: count(), total: sum(pid)} }
disk { @b[proc: pid] = {ops: count(), sectors: sum(sectors)} } `

func TestJoinSyntaxAndProof(t *testing.T) {
	r, err := ParseMonitorQuery(joinSelectors + `after 1s { emit @a left join @b on proc order by sectors desc limit 2 }`)
	if err != nil {
		t.Fatal(err)
	}
	want := AggregateReport{Left: "a", Right: "b", Kind: "left", On: []string{"proc"}, Sort: "sectors", Limit: 2}
	if r.Source != "script" || len(r.Reports) != 1 || !reflect.DeepEqual(r.Reports[0], want) || len(r.Probes) != 2 || r.Probes[0].Aggregation.Limit != 0 || r.Probes[1].Aggregation.Limit != 0 {
		t.Fatalf("join must lower to unlimited collectors and post-aggregation report: %+v", r)
	}
	for _, suffix := range []string{
		`after 1s { emit @a full join @b on proc; emit @a }`,
		`every 100ms { emit @a inner join @b on proc order by a.ops asc; clear @a; clear @b } after 1s { stop }`,
	} {
		if _, err := ParseMonitorQuery(joinSelectors + suffix); err != nil {
			t.Fatalf("valid join rejected: %s: %v", suffix, err)
		}
	}
	for _, tc := range []struct{ suffix, want string }{
		{`after 1s { emit @a left join @b on proc order by ops desc }`, "ambiguous sort metric"},
		{`after 1s { emit @a left join @b on nope }`, "grouping column"},
		{`after 1s { emit @a left join @b on proc, proc }`, "unique grouping column"},
		{`after 1s { emit @a left join @a on proc }`, "different known tables"},
		{`after 1s { emit @a left join @missing on proc }`, "known tables"},
		{`every 100ms { emit @a left join @b on proc; clear @a } after 1s { stop }`, "clear @b"},
		{`after 1s { emit @a left join @b on proc; clear @a }`, "unexpected"},
		{`after 1s { emit @a left join @b on proc order by missing desc }`, "unknown sort metric"},
		{`after 1s { emit @a left join @b on proc limit 4097 }`, "0–4096"},
	} {
		if _, err := ParseMonitorQuery(joinSelectors + tc.suffix); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("expected %q: %s: %v", tc.want, tc.suffix, err)
		}
	}
}

func joinFixtureTable(name string, records [][2]string) *AggregationResult {
	a := &AggregationResult{Table: name, GroupBy: []string{"proc"}, Columns: []AggregateMetric{{Name: "ops", Function: "count"}}, Rows: []AggregateRow{}}
	for _, record := range records {
		a.Rows = append(a.Rows, AggregateRow{Group: map[string]json.RawMessage{"proc": json.RawMessage(record[0])}, Values: []json.RawMessage{json.RawMessage(record[1])}})
	}
	return a
}

func TestJoinRowsKindsNullsPrecisionAndWire(t *testing.T) {
	left := joinFixtureTable("a", [][2]string{{`"only-left"`, "10"}, {`"both"`, "20"}, {"null", "30"}, {"9007199254740993", "40"}, {`"7"`, "50"}})
	right := joinFixtureTable("b", [][2]string{{`"both"`, "200"}, {`"only-right"`, "300"}, {"null", "400"}, {"9007199254740992", "500"}, {"9007199254740993.0", "600"}, {"7", "700"}})
	left.Collection = &CollectionStats{RingBufferDropped: 3}
	right.Collection = &CollectionStats{RingBufferDropped: 7, PairingFailures: 2}
	left.StackCoverage = &StackCoverageReport{User: &StackCoverage{Events: 5, Captured: 4, CaptureFailures: 1, HelperErrors: map[string]uint64{"-14": 1}}}
	right.StackCoverage = &StackCoverageReport{Kernel: &StackCoverage{Events: 6, Captured: 6}}
	for _, tc := range []struct {
		kind  string
		count int
	}{{"inner", 2}, {"left", 5}, {"full", 9}} {
		t.Run(tc.kind, func(t *testing.T) {
			report := AggregateReport{Left: "a", Right: "b", Kind: tc.kind, On: []string{"proc"}}
			got, err := joinAggregateRows(report, left, right)
			if err != nil || len(got.Rows) != tc.count {
				t.Fatalf("%s rows: %+v, %v", tc.kind, got, err)
			}
			if got.Collection != nil || got.Collections["a"].RingBufferDropped != 3 || got.Collections["b"].RingBufferDropped != 7 || got.Collections["b"].PairingFailures != 2 {
				t.Fatal("join must retain separate source diagnostics")
			}
			if got.StackCoverage != nil || !reflect.DeepEqual(got.StackCoverageByTable["a"], left.StackCoverage) || !reflect.DeepEqual(got.StackCoverageByTable["b"], right.StackCoverage) {
				t.Fatal("join must not sum or lose source stack coverage")
			}
			for _, row := range got.Rows {
				if string(row.Group["proc"]) == `"both"` && (string(row.Values[0]) != "20" || string(row.Values[1]) != "200") {
					t.Fatal("matched values not aligned")
				}
				if string(row.Group["proc"]) == "null" && string(row.Values[0]) != "null" && string(row.Values[1]) != "null" {
					t.Fatal("null keys must not match")
				}
				if string(row.Values[0]) == "40" && string(row.Values[1]) != "600" {
					t.Fatal("numeric join lost full-width precision or decimal equivalence")
				}
			}
			data, err := json.Marshal(got)
			if err != nil || !strings.Contains(string(data), `"a":{"ops":20},"b":{"ops":200}`) {
				t.Fatalf("namespaced metric wire format: %s, %v", data, err)
			}
			var decoded AggregationResult
			if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded.Rows, got.Rows) || !reflect.DeepEqual(decoded.Join, &report) || !reflect.DeepEqual(decoded.StackCoverageByTable, got.StackCoverageByTable) {
				t.Fatalf("joined row round trip: %+v, %v", decoded, err)
			}
		})
	}
	if len(left.Rows) != 5 || left.Columns[0].Name != "ops" || right.Columns[0].Name != "ops" {
		t.Fatal("joining mutated source tables")
	}
}

func TestJoinCompositeKeysAndAscendingNulls(t *testing.T) {
	left := joinFixtureTable("a", [][2]string{{"1", "100"}, {"1", "10"}, {"2", "0"}})
	right := joinFixtureTable("b", [][2]string{{"1", "50"}, {"1", "5"}})
	for _, table := range []*AggregationResult{left, right} {
		table.GroupBy = []string{"proc", "device", "extra"}
		for i := range table.Rows {
			table.Rows[i].Group["device"] = json.RawMessage([]string{`"nvme"`, `"sda"`, `"nvme"`}[i])
			table.Rows[i].Group["extra"] = json.RawMessage(`"` + table.Table + `"`)
		}
	}
	report := AggregateReport{Left: "a", Right: "b", Kind: "left", On: []string{"proc", "device"}, Sort: "b.ops", Ascending: true, Limit: 2}
	got, err := joinAggregateRows(report, left, right)
	if err != nil || got.TotalGroups != 3 || len(got.Rows) != 2 {
		t.Fatalf("composite join: %+v, %v", got, err)
	}
	if string(got.Rows[0].Values[0]) != "10" || string(got.Rows[0].Values[1]) != "5" || string(got.Rows[1].Values[1]) != "50" || string(got.Rows[0].Group["a.extra"]) != `"a"` || string(got.Rows[0].Group["b.extra"]) != `"b"` {
		t.Fatalf("composite pairing, null ordering or extra group preservation: %+v", got.Rows)
	}
	report.Limit = 0
	got, err = joinAggregateRows(report, left, right)
	if err != nil || string(got.Rows[2].Values[0]) != "0" || string(got.Rows[2].Values[1]) != "null" {
		t.Fatal("zero measurement must differ from an absent match; null sorts last")
	}
	for _, pair := range [][2]*AggregationResult{
		{joinFixtureTable("a", nil), right}, {left, joinFixtureTable("b", nil)},
	} {
		report.Kind = "inner"
		got, err := joinAggregateRows(report, pair[0], pair[1])
		if err != nil || len(got.Rows) != 0 {
			t.Fatalf("empty inner join: %+v, %v", got, err)
		}
	}
}

func TestJoinRejectsDuplicatesBeforeLimits(t *testing.T) {
	left := joinFixtureTable("a", [][2]string{{"1", "10"}, {"1.0", "20"}})
	right := joinFixtureTable("b", [][2]string{{"2", "100"}})
	for _, kind := range []string{"inner", "left", "full"} {
		for _, pair := range [][2]*AggregationResult{{left, right}, {right, left}} {
			report := AggregateReport{Left: pair[0].Table, Right: pair[1].Table, Kind: kind, On: []string{"proc"}, Limit: 1}
			if _, err := joinAggregateRows(report, pair[0], pair[1]); err == nil || !strings.Contains(err.Error(), "duplicate join keys") {
				t.Fatalf("duplicate unmatched keys must fail even before inner join/limit: %v", err)
			}
		}
	}
}

func TestJoinScriptLimitsAndBuckets(t *testing.T) {
	for _, periodic := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			report := `after 2s { emit @a inner join @b on proc order by sectors desc limit 1; emit @a }`
			if periodic {
				report = `every 1s { emit @a inner join @b on proc order by sectors desc limit 1; clear @a; clear @b; emit @a } after 2s { stop }`
			}
			r, err := ParseMonitorQuery(joinSelectors + report)
			if err != nil {
				t.Fatal(err)
			}
			got, err := aggregateScript(context.Background(), r, func(ctx context.Context, selection MonitorRequest, emit func(Event) error) error {
				for bucket := 0; bucket < 2; bucket++ {
					if bucket == 1 {
						time.Sleep(time.Second)
					}
					pids := []uint32{1, 2, 3}
					if bucket == 1 && periodic {
						pids = []uint32{4, 5, 6} // Never join keys across buckets.
					}
					if selection.Source == "syscalls" {
						for _, pid := range pids {
							if err := emit(Event{PID: pid}); err != nil {
								return err
							}
						}
					} else {
						// The best matching row is not the left table's top row.
						for i, pid := range pids[:2] {
							if err := emit(Event{PID: pid, Disk: &DiskEvent{Sectors: uint32(10 + i*90)}}); err != nil {
								return err
							}
						}
					}
				}
				<-ctx.Done()
				return nil
			}, nil)
			if err != nil || len(got.Tables) != 2 {
				t.Fatalf("join script: %+v, %v", got, err)
			}
			results := []*AggregationResult{got.Tables[0].Aggregation}
			if periodic {
				results = got.Tables[0].Windows
			}
			for i, result := range results {
				want := "2"
				if periodic && i == 1 {
					want = "5"
				}
				if len(result.Rows) != 1 || string(result.Rows[0].Group["proc"]) != want || result.TotalGroups != 2 {
					t.Fatalf("join/limit/bucket ordering: %+v", result)
				}
			}
			// The second emit reuses original rows, not the limited join result.
			if !periodic && len(got.Tables[1].Aggregation.Rows) != 3 {
				t.Fatal("joined emit mutated the original table")
			}
		})
	}
}

func TestJoinSampledTables(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery(`memory { @used[capacity: total] = max(used) }
memory { @free[capacity: total] = max(available) }
after 2s { emit @used full join @free on capacity order by used.value desc }`)
		if err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int32
		got, err := aggregateScript(context.Background(), r, nil, func(context.Context, MonitorRequest) (Snapshot, error) {
			calls.Add(1)
			return Snapshot{Source: "memory", Memory: &MemoryInfo{Total: 100, Used: 25, Available: 75}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		rows := got.Tables[0].Aggregation.Rows
		if calls.Load() != 4 || len(rows) != 1 || string(rows[0].Values[0]) != "25" || string(rows[0].Values[1]) != "75" {
			t.Fatalf("sampled join: calls=%d rows=%+v", calls.Load(), rows)
		}
	})
}

func TestJoinServerValidation(t *testing.T) {
	for _, mutate := range []func(*MonitorRequest){
		func(r *MonitorRequest) { r.Reports[0].Kind = "cross" },
		func(r *MonitorRequest) { r.Reports[0].On = nil },
		func(r *MonitorRequest) { r.Reports[0].Right = "missing" },
		func(r *MonitorRequest) { r.Reports[0].Limit = -1 },
		func(r *MonitorRequest) { r.Probes[0].Aggregation.Limit = 1 },
		func(r *MonitorRequest) { r.Probes[0].Aggregation.Nonzero = true },
		func(r *MonitorRequest) { r.Reports = append(r.Reports, make([]AggregateReport, 8)...) },
	} {
		r, err := ParseMonitorQuery(joinSelectors + `after 1s { emit @a full join @b on proc }`)
		if err != nil {
			t.Fatal(err)
		}
		mutate(&r)
		if err := r.Validate(); err == nil {
			t.Fatalf("invalid API report accepted: %+v", r)
		}
	}
	if err := (MonitorRequest{Source: "memory", Mode: "snapshot", Reports: []AggregateReport{{Left: "a"}}}).Validate(); err == nil {
		t.Fatal("reports accepted outside a script")
	}
}
