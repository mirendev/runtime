package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAggregateOptionalGroupsAndRows(t *testing.T) {
	r, err := ParseMonitorQuery("syscalls where paths = true and result.format = rows count, sum(file.fd), count_distinct(file.path) over 1s by file.error")
	if err != nil {
		t.Fatal(err)
	}
	reduction := newAggregateReduction(*r.Aggregation)
	for _, event := range []Event{
		{File: &SyscallFile{FD: 59, Path: "/data/bloom"}},
		{File: &SyscallFile{FD: 17, Error: "closed"}},
		{}, // unrelated syscall; absent file is not a numeric zero
	} {
		if err := reduction.add(eventGroupFields(event, nil)); err != nil {
			t.Fatal(err)
		}
	}
	a := reduction.result("syscalls", time.Now(), time.Now()).Aggregation
	want := map[string][]json.RawMessage{
		"null":     {json.RawMessage("2"), json.RawMessage("59"), json.RawMessage("1")},
		`"closed"`: {json.RawMessage("1"), json.RawMessage("17"), json.RawMessage("null")},
	}
	for _, row := range a.Rows {
		key := string(row.Group["file.error"])
		if !reflect.DeepEqual(row.Values, want[key]) {
			t.Fatalf("group %s: %s, want %s", key, row.Values, want[key])
		}
		delete(want, key)
	}
	if len(want) != 0 || a.TotalGroups != 2 || len(a.Columns) != 3 {
		t.Fatalf("lost groups or metrics: %+v", a)
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"metrics"`) || strings.Contains(string(data), `"counts"`) || strings.Contains(string(data), `"values":[]`) {
		t.Fatalf("legacy output duplicated compact rows: %s", data)
	}
	var decoded AggregationResult
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded.Rows, a.Rows) || !reflect.DeepEqual(decoded.Columns, a.Columns) {
		t.Fatalf("row roundtrip: %s, %v", data, err)
	}
}

func TestAggregateRowSelectionPrecisionAndMissingMetrics(t *testing.T) {
	r := newAggregateReduction(AggregationRequest{
		Compact: true, Nonzero: true, Limit: 2, SortMetric: 1, GroupBy: []string{"group"},
		Metrics: []AggregateMetric{{Function: "sum", Field: "a"}, {Function: "sum", Field: "b"}},
	})
	for _, fields := range []map[string]any{
		{"group": "smaller", "a": 1, "b": json.Number("9007199254740992")},
		{"group": "larger", "a": 1, "b": json.Number("9007199254740993")},
		{"group": "zero", "a": 0, "b": 0},
		{"group": "missing", "a": 0},
	} {
		if err := r.addSample(fields); err != nil {
			t.Fatal(err)
		}
	}
	a := r.result("test", time.Now(), time.Now()).Aggregation
	if a.TotalGroups != 4 || a.OmittedZeroGroups != 1 || len(a.Rows) != 2 || string(a.Rows[0].Group["group"]) != `"larger"` || string(a.Rows[1].Group["group"]) != `"smaller"` {
		t.Fatalf("selection/precision: %+v", a)
	}
	// Unknown metrics must be null, not zero, and must survive nonzero filtering.
	r.request.Limit = 0
	a = r.result("test", time.Now(), time.Now()).Aggregation
	if len(a.Rows) != 3 || string(a.Rows[2].Group["group"]) != `"missing"` || string(a.Rows[2].Values[1]) != "null" {
		t.Fatalf("missing metric: %+v", a.Rows)
	}
	r.request.Ascending, r.request.Limit = true, 1
	a = r.result("test", time.Now(), time.Now()).Aggregation
	if len(a.Rows) != 1 || string(a.Rows[0].Group["group"]) != `"smaller"` {
		t.Fatalf("ascending limit/precision: %+v", a.Rows)
	}
	r.request.Limit = 0
	a = r.result("test", time.Now(), time.Now()).Aggregation
	if string(a.Rows[0].Group["group"]) != `"smaller"` || string(a.Rows[1].Group["group"]) != `"larger"` || string(a.Rows[2].Group["group"]) != `"missing"` {
		t.Fatalf("ascending null ordering: %+v", a.Rows)
	}
	// Sampled grouping also uses a null bucket, rather than dropping the record.
	if err := r.addSample(map[string]any{"a": 7}); err != nil {
		t.Fatal(err)
	}
	a = r.result("test", time.Now(), time.Now()).Aggregation
	found := false
	for _, row := range a.Rows {
		if string(row.Group["group"]) == "null" {
			found = string(row.Values[0]) == "7" && string(row.Values[1]) == "null"
		}
	}
	if !found {
		t.Fatal("sample without grouping field was dropped")
	}
}

func TestAggregateResultControlQueries(t *testing.T) {
	for _, query := range []string{
		"disk where result.format = rows and result.nonzero = true and result.limit = 10 and result.sort_metric = 1 count, sum(sectors) over 1s by device_name",
		"cgroups where result.limit = 5 avg(memory_bytes) over 3s every 1s by path",
	} {
		if _, err := ParseMonitorQuery(query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	for _, query := range []string{
		"disk where result.limit = 1", "disk where result.limit = -1 count over 1s",
		"disk where result.limit = 4097 count over 1s", "disk where result.sort_metric = 1 count over 1s",
		"disk where result.nonzero = yes count over 1s", "disk where result.format = bad count over 1s",
	} {
		if _, err := ParseMonitorQuery(query); err == nil {
			t.Fatalf("invalid result controls accepted: %s", query)
		}
	}
	r := newAggregateReduction(AggregationRequest{Compact: true, GroupBy: []string{"pid"}})
	data, err := json.Marshal(r.result("syscalls", time.Now(), time.Now()).Aggregation)
	if err != nil || !strings.Contains(string(data), `"rows":[]`) || !strings.Contains(string(data), `"total_groups":0`) {
		t.Fatalf("empty rows: %s, %v", data, err)
	}
}

func TestCompactRowsDoNotRepeatLongStacks(t *testing.T) {
	stack := strings.Repeat("module:long_function;", 128)
	r := newAggregateReduction(AggregationRequest{GroupBy: []string{"stack"}, Metrics: []AggregateMetric{{Function: "count"}, {Function: "sum", Field: "x"}, {Function: "max", Field: "x"}}})
	if err := r.add(map[string]any{"stack": stack, "x": 7}); err != nil {
		t.Fatal(err)
	}
	start, end := time.Now(), time.Now()
	legacy, err := json.Marshal(r.result("test", start, end).Aggregation)
	if err != nil {
		t.Fatal(err)
	}
	r.request.Compact = true
	compact, err := json.Marshal(r.result("test", start, end).Aggregation)
	if err != nil || strings.Count(string(compact), stack) != 1 || strings.Count(string(legacy), stack) != 3 || len(compact)*2 >= len(legacy) {
		t.Fatalf("stack output not compact: %d vs legacy %d bytes, %v", len(compact), len(legacy), err)
	}
	t.Logf("long-stack output: %d bytes compact vs %d legacy", len(compact), len(legacy))
}

func TestNamedTableRowJSON(t *testing.T) {
	r, err := ParseMonitorQuery(`syscalls:completion { @io[proc: pid] = {total: sum(duration_ns), ops: count(), p99: percentile(duration_ns, 99)} } after 1s { emit @io order by total asc }`)
	if err != nil {
		t.Fatal(err)
	}
	reduction := newAggregateReduction(*r.Aggregation)
	for _, fields := range []map[string]any{
		{"pid": 7, "duration_ns": uint64(18446744073709551615)},
		{"pid": 7, "duration_ns": uint64(1)},
		{"pid": 8}, // count observes the group; optional duration metrics remain null
	} {
		if err := reduction.add(fields); err != nil {
			t.Fatal(err)
		}
	}
	a := reduction.result("syscalls", time.Now(), time.Now()).Aggregation
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"values":{"ops":2,"p99":18446744073709551615,"total":18446744073709551616}`) || !strings.Contains(string(data), `"values":{"ops":1,"p99":null,"total":null}`) || strings.Contains(string(data), `"values":[`) {
		t.Fatalf("incorrect named metrics: %s", data)
	}
	var decoded AggregationResult
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded.Rows, a.Rows) || !reflect.DeepEqual(decoded.Columns, a.Columns) {
		t.Fatalf("named roundtrip lost column order: %s %v", data, err)
	}
	// Keep the legacy wire format, including compatibility with older table results.
	a.Table = ""
	legacy, err := json.Marshal(a)
	if err != nil || !strings.Contains(string(legacy), `"values":[18446744073709551616,2,18446744073709551615]`) {
		t.Fatalf("legacy arrays changed: %s %v", legacy, err)
	}
	if err := json.Unmarshal(legacy, &decoded); err != nil || !reflect.DeepEqual(decoded.Rows, a.Rows) {
		t.Fatalf("legacy decoding changed: %v", err)
	}
	for _, value := range []string{`{"ops":1,"p99":2}`, `{"ops":1,"p99":2,"wrong":3}`} {
		bad := `{"table":"io","columns":[{"name":"ops"},{"name":"p99"},{"name":"total"}],"rows":[{"group":{},"values":` + value + `}]}`
		if err := json.Unmarshal([]byte(bad), &decoded); err == nil {
			t.Fatal("silently lost a named metric")
		}
	}
	for _, query := range []string{
		`syscalls { @x[] = count() } after 1s { emit @x }`,
		`syscalls { @x[pid] = count() } after 1s { emit @x }`,
	} {
		r, err := ParseMonitorQuery(query)
		if err != nil {
			t.Fatal(err)
		}
		a := newAggregateReduction(*r.Aggregation).result(r.Source, time.Now(), time.Now()).Aggregation
		data, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Aggregation.GroupBy) == 0 && !strings.Contains(string(data), `"values":{"value":0}`) || len(r.Aggregation.GroupBy) != 0 && !strings.Contains(string(data), `"rows":[]`) {
			t.Fatalf("empty named output: %s", data)
		}
	}
}
