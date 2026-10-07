package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func inventoryEngine(data any) Engine {
	return Engine{Sources: map[string]CustomSource{"workloads": {
		Fields: []string{"app", "cgroup", "container_id"},
		Snapshots: func(ctx context.Context, r MonitorRequest) (Snapshot, error) {
			// A custom snapshot collector owns its source predicates.
			if rows, ok := data.([]map[string]any); ok {
				selected := make([]map[string]any, 0)
				for _, row := range rows {
					match := true
					for _, filter := range r.Filters {
						match = match && slices.Contains(filter.Values, fmt.Sprint(row[filter.Field]))
					}
					if match {
						selected = append(selected, row)
					}
				}
				return Snapshot{Source: r.Source, Data: selected}, nil
			}
			return Snapshot{Source: r.Source, Data: data}, nil
		},
	}}}
}

func attributedGroup(path, id string, bytes, ios uint64) CgroupInfo {
	return CgroupInfo{Path: path, ID: id, IO: &CgroupIO{WriteBytes: &bytes, WriteIOs: &ios,
		Devices: []CgroupIODevice{{Device: "8:0", Counters: map[string]uint64{"wbytes": bytes, "wios": ios}}}}}
}

func TestInventoryCorrelationAttribution(t *testing.T) {
	for _, text := range []string{
		`cgroups using (workloads where app = "a") on path = cgroup where result.format = rows rate(io.write_bytes), rate(io.write_ios) over 4s every 1s by inventory.app`,
		`cgroups using (workloads where app = "a") on path = cgroup { @io[app: inventory.app] = {bytes_per_second: rate(io.write_bytes), ios_per_second: rate(io.write_ios)} } after 4s { emit @io }`,
	} {
		t.Run(text, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				row := map[string]any{"app": "a", "cgroup": "/a/main", "container_id": "main"}
				engine := inventoryEngine([]map[string]any{row, row,
					{"app": "a", "cgroup": "/a/pause", "container_id": "pause"},
					{"app": "b", "cgroup": "/b", "container_id": "other"},
					{"app": "a", "cgroup": "/missing", "container_id": "gone"},
					{"app": "a", "cgroup": "", "container_id": "starting"},
				})
				inventoryCalls := 0
				original := engine.Sources["workloads"]
				source := original
				source.Snapshots = func(ctx context.Context, r MonitorRequest) (Snapshot, error) {
					inventoryCalls++
					return original.Snapshots(ctx, r)
				}
				engine.Sources["workloads"] = source
				start := time.Now()
				calls := make(map[string]int)
				engine.Snapshots = func(ctx context.Context, r MonitorRequest) (Snapshot, error) {
					if r.Source != "cgroups" || r.Mode != "snapshot" || r.Using != nil || r.Aggregation != nil {
						t.Fatalf("wrong lowered selection: %+v", r)
					}
					calls[r.Path]++
					i := uint64(time.Since(start) / time.Second)
					var groups []CgroupInfo
					switch r.Path {
					case "/a/main":
						groups = []CgroupInfo{attributedGroup(r.Path, "main", 1000+30*i, 100+2*i)}
					case "/a/pause":
						groups = []CgroupInfo{attributedGroup(r.Path, "pause", 500+7*i, 10+3*i)}
					case "/missing":
					default:
						t.Fatalf("unselected app/parent collected: %s", r.Path)
					}
					return Snapshot{Source: r.Source, Cgroups: groups}, nil
				}
				r, err := engine.ParseMonitorQuery(text)
				if err != nil {
					t.Fatal(err)
				}
				// A remote/server engine must rebind the serialized inventory source.
				encoded, _ := json.Marshal(r)
				var decoded MonitorRequest
				if err := json.Unmarshal(encoded, &decoded); err != nil {
					t.Fatal(err)
				}
				if err := (Engine{}).Validate(decoded); err == nil {
					t.Fatal("unregistered inventory source accepted")
				}
				got, err := engine.Query(context.Background(), decoded)
				if err != nil {
					t.Fatal(err)
				}
				if inventoryCalls != 1 || !reflect.DeepEqual(calls, map[string]int{"/a/main": 4, "/a/pause": 4, "/missing": 4}) {
					t.Fatalf("inventory not frozen or duplicate paths sampled: %d, %v", inventoryCalls, calls)
				}
				a := got.Aggregation
				if len(a.Rows) != 1 || string(a.Rows[0].Values[0]) != "37" || string(a.Rows[0].Values[1]) != "5" {
					t.Fatalf("expected app total, not average across containers or lifetime counters: %+v", a)
				}
				group := "inventory.app"
				if strings.Contains(text, "@io") {
					group = "app"
				}
				if string(a.Rows[0].Group[group]) != `"a"` {
					t.Fatalf("wrong attribution: %+v", a.Rows)
				}
			})
		})
	}
}

func TestInventoryCorrelationSnapshotAndGenericTarget(t *testing.T) {
	engine := inventoryEngine([]map[string]any{{"app": "a", "cgroup": "/a"}, {"app": "b", "cgroup": "/b"}})
	engine.Snapshots = func(ctx context.Context, r MonitorRequest) (Snapshot, error) {
		return Snapshot{Source: r.Source, Cgroups: []CgroupInfo{attributedGroup(r.Path, "id", 123, 7)}}, nil
	}
	r, err := engine.ParseMonitorQuery(`cgroups using (workloads where app = a) on path = cgroup`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.Query(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	rows := got.Data.([]map[string]any)
	if len(rows) != 1 || rows[0]["inventory.app"] != "a" || rows[0]["io.write_bytes"] != json.Number("123") {
		t.Fatalf("counter snapshot lost attribution/precision: %+v", rows)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), `"cgroups"`) && strings.Contains(string(encoded), `"cgroups":`) {
		t.Fatalf("misleading empty cgroups alongside correlated data: %s", encoded)
	}
	// No inventory source/field names are hard-coded; network uses the same operation.
	engine = inventoryEngine([]map[string]any{{"app": "a", "cgroup": "eth7"}})
	engine.Snapshots = func(ctx context.Context, r MonitorRequest) (Snapshot, error) {
		return Snapshot{Source: r.Source, Network: []InterfaceInfo{{Name: "eth0", BytesSent: 9999}, {Name: "eth7", BytesSent: 19}}}, nil
	}
	r, err = engine.ParseMonitorQuery(`network using (workloads) on name = cgroup`)
	if err != nil {
		t.Fatal(err)
	}
	got, err = engine.Query(context.Background(), r)
	if err != nil || len(got.Data.([]map[string]any)) != 1 || got.Data.([]map[string]any)[0]["bytes_sent"] != json.Number("19") {
		t.Fatalf("generic exact correlation: %+v, %v", got, err)
	}
}

func TestInventoryCorrelationRejectsMisleadingSelections(t *testing.T) {
	for _, tc := range []struct {
		data any
		want string
	}{
		{[]map[string]any{{"app": "a", "cgroup": "/a"}, {"app": "b", "cgroup": "/a"}}, "ambiguous"},
		{[]map[string]any{{"cgroup": "/a"}, {"cgroup": "/a/child"}}, "double-count"},
		{[]map[string]any{{"cgroup": "/"}, {"cgroup": "/a"}}, "double-count"},
		{[]map[string]any{{"cgroup": "/a/*"}}, "exact canonical"},
		{[]map[string]any{{"cgroup": "/a/../b"}}, "exact canonical"},
		{[]map[string]any{{"cgroup": 42}}, "must be a string"},
		{[]map[string]any{{"cgroup": "/a", "app": []string{"a"}}}, "must be scalar"},
		{[]map[string]any{nil}, "must be objects"},
		{map[string]any{"cgroup": "/a"}, "must be an array"},
		{make([]map[string]any, 4097), "4096 rows"},
		{[]map[string]any{{"cgroup": "/a", "app": strings.Repeat("x", 7<<20)}}, "7 MiB"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			engine := inventoryEngine(tc.data)
			engine.Snapshots = func(context.Context, MonitorRequest) (Snapshot, error) {
				t.Fatal("invalid inventory reached target collection")
				return Snapshot{}, nil
			}
			r, err := engine.ParseMonitorQuery(`cgroups using (workloads) on path = cgroup`)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Query(context.Background(), r); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}
	engine := inventoryEngine([]map[string]any{})
	for _, text := range []string{
		`cgroups using (workloads) on path = absent`,
		`cgroups using (workloads) on id = cgroup`,
		`cgroups using (workloads) on path = cgroup where path = /a`,
		`disk using (workloads) on path = cgroup count over 4s`,
		`workloads { @inventory[app, cgroup] = count() } cgroups { @io[path] = rate(io.write_bytes) } after 4s { emit @inventory inner join @io on app }`,
	} {
		if _, err := engine.ParseMonitorQuery(text); err == nil {
			t.Fatalf("invalid correlation/completed join accepted: %s", text)
		}
	}
}

func TestInventoryCorrelationLifetimesAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine := inventoryEngine([]map[string]any{{"app": "a", "cgroup": "/a"}})
		start := time.Now()
		engine.Snapshots = func(ctx context.Context, r MonitorRequest) (Snapshot, error) {
			i := int(time.Since(start) / time.Second)
			groups := []CgroupInfo{}
			if i != 1 {
				id := "old"
				if i >= 3 {
					id = "new"
				}
				groups = append(groups, attributedGroup("/a", id, []uint64{10, 0, 100, 1000, 1017}[i], 1))
			}
			return Snapshot{Source: r.Source, Cgroups: groups}, nil
		}
		r, err := engine.ParseMonitorQuery(`cgroups using (workloads) on path = cgroup rate(io.write_bytes) over 5s every 1s by inventory.app`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := engine.Query(context.Background(), r)
		if err != nil || len(got.Aggregation.Values) != 1 || string(got.Aggregation.Values[0].Value) != "17" {
			t.Fatalf("missing/recreated cgroups bridged lifetimes: %+v, %v", got, err)
		}
		for _, cancelInventory := range []bool{true, false} {
			ctx, cancel := context.WithCancel(context.Background())
			go func() { time.Sleep(1500 * time.Millisecond); cancel() }()
			block := func(ctx context.Context, _ MonitorRequest) (Snapshot, error) {
				<-ctx.Done()
				return Snapshot{}, ctx.Err()
			}
			engine := inventoryEngine([]map[string]any{{"cgroup": "/a"}})
			if cancelInventory {
				source := engine.Sources["workloads"]
				source.Snapshots = block
				engine.Sources["workloads"] = source
			} else {
				engine.Snapshots = block
			}
			if _, err := engine.Query(ctx, r); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation returned partial success: %v", err)
			}
		}
		// Inventory time is part of the finite window, not an unbounded prelude.
		source := engine.Sources["workloads"]
		source.Snapshots = func(ctx context.Context, _ MonitorRequest) (Snapshot, error) {
			<-ctx.Done()
			return Snapshot{}, ctx.Err()
		}
		engine.Sources["workloads"] = source
		start = time.Now()
		got, err = engine.Query(context.Background(), r)
		if err != nil || time.Since(start) != 5*time.Second || len(got.Aggregation.Values) != 0 {
			t.Fatalf("window overrun: %+v, %v", got, err)
		}
	})
}

func TestInventoryCorrelationScriptsAndCollectionLimits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine := inventoryEngine([]map[string]any{{"app": "a", "cgroup": "/a"}, {"app": "b", "cgroup": "/b"}})
		start := time.Now()
		engine.Snapshots = func(ctx context.Context, r MonitorRequest) (Snapshot, error) {
			rate := uint64(13)
			if r.Path == "/b" {
				rate = 31
			}
			return Snapshot{Source: r.Source, Cgroups: []CgroupInfo{attributedGroup(r.Path, r.Path, 1000+uint64(time.Since(start)/time.Second)*rate, 1)}}, nil
		}
		r, err := engine.ParseMonitorQuery(`
cgroups using (workloads where app in (a,b)) on path = cgroup { @all[app: inventory.app] = rate(io.write_bytes) }
cgroups using (workloads where app = a) on path = cgroup { @one[app: inventory.app] = rate(io.write_bytes) }
after 3s { emit @all left join @one on app }`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := engine.Query(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		rows := got.Tables[0].Aggregation.Rows
		if len(rows) != 2 {
			t.Fatalf("wrong joined attribution: %+v", rows)
		}
		for _, row := range rows {
			want := []json.RawMessage{json.RawMessage("13"), json.RawMessage("13")}
			if string(row.Group["app"]) == `"b"` {
				want = []json.RawMessage{json.RawMessage("31"), json.RawMessage("null")}
			}
			if !reflect.DeepEqual(row.Values, want) {
				t.Fatalf("misattributed join: %+v", row)
			}
		}
		metadata, err := engine.Metadata(r.Probes[0])
		if err != nil || !slices.Contains(metadata.GroupByFields, "inventory.app") || !slices.Contains(metadata.SampleGroupByFields, "inventory.app") {
			t.Fatalf("missing inventory metadata: %+v, %v", metadata, err)
		}
	})
	// The exact inventory limit is accepted; absent keys cause no target reads.
	empty := make([]map[string]any, 4096)
	for i := range empty {
		empty[i] = map[string]any{}
	}
	engine := inventoryEngine(empty)
	engine.Snapshots = func(context.Context, MonitorRequest) (Snapshot, error) {
		t.Fatal("empty selection collected targets")
		return Snapshot{}, nil
	}
	r, err := engine.ParseMonitorQuery(`CGROUPS USING (workloads) ON path = cgroup`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := engine.Query(context.Background(), r)
	if err != nil || len(got.Data.([]map[string]any)) != 0 {
		t.Fatalf("empty selection: %+v, %v", got, err)
	}
	for _, tc := range []struct {
		count       int
		wrongSource bool
		want        string
	}{
		{2, false, "duplicate target identity"}, {4097, false, "4096 records"}, {1, true, "snapshot source mismatch"},
	} {
		engine := inventoryEngine([]map[string]any{{"cgroup": "/a"}})
		engine.Snapshots = func(context.Context, MonitorRequest) (Snapshot, error) {
			groups := make([]CgroupInfo, tc.count)
			for i := range groups {
				groups[i] = attributedGroup("/a", "id", 1, 1)
			}
			source := "cgroups"
			if tc.wrongSource {
				source = "network"
			}
			return Snapshot{Source: source, Cgroups: groups}, nil
		}
		if _, err := engine.Query(context.Background(), r); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("expected %s, got %v", tc.want, err)
		}
	}
}
