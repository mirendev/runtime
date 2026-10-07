package query_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	query "miren.dev/runtime/pkg/portalquery"
)

type jobRecord struct {
	Queue string `json:"queue"`
	Bytes uint64 `json:"bytes"`
}

func jobEngine() query.Engine {
	return query.Engine{Sources: map[string]query.CustomSource{
		"jobs": {
			Fields: []string{"queue", "bytes"}, NumericFields: []string{"bytes"},
			Events: func(ctx context.Context, r query.MonitorRequest, emit func(query.Event) error) error {
				for _, job := range []jobRecord{{"fast", 3}, {"slow", 11}, {"fast", 7}, {"fast", 19}} {
					if err := emit(query.Event{Time: time.Now(), Data: job, Fields: map[string]any{"queue": job.Queue, "bytes": job.Bytes}}); err != nil {
						return err
					}
				}
				<-ctx.Done()
				return ctx.Err()
			},
		},
	}}
}

func TestCustomSourceMonitor(t *testing.T) {
	engine := jobEngine()
	r, err := engine.ParseMonitorQuery(`jobs where queue in (fast, other) and bytes >= 7 and bytes < 19`)
	if err != nil {
		t.Fatal(err)
	}
	// Binding is local state, not part of the serializable request contract.
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded query.MonitorRequest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop")
	err = engine.Monitor(context.Background(), decoded, func(event query.Event) error {
		if event.Data != (jobRecord{"fast", 7}) || event.Time.IsZero() || event.TAI64N == "" {
			t.Fatalf("wrong typed event or timestamp: %+v", event)
		}
		data, err := json.Marshal(event)
		if err != nil || strings.Contains(string(data), `"syscall"`) || !strings.Contains(string(data), `"data":{"queue":"fast","bytes":7}`) {
			t.Fatalf("custom JSON: %s, %v", data, err)
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	if err := (query.Engine{}).Validate(r); err == nil {
		t.Fatal("request leaked its source into another engine")
	}
}

func TestCustomSourceAggregatesAndScripts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine := jobEngine()
		for _, text := range []string{
			`jobs where queue = fast and bytes > 3 and result.format = rows sum(bytes) over 1s by queue`,
			`jobs where queue = fast and bytes > 3 { @total[queue] = sum(bytes) } after 1s { emit @total }`,
		} {
			r, err := engine.ParseMonitorQuery(text)
			if err != nil {
				t.Fatal(err)
			}
			result, err := engine.Query(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Aggregation.Rows) != 1 || string(result.Aggregation.Rows[0].Values[0]) != "26" || string(result.Aggregation.Rows[0].Group["queue"]) != `"fast"` {
				t.Fatalf("%s: %+v", text, result.Aggregation)
			}
		}
		for _, report := range []string{`emit @a; emit @b`, `emit @a left join @b on queue`} {
			r, err := engine.ParseMonitorQuery(`jobs where queue = fast { @a[queue] = sum(bytes) }
jobs where queue = fast and bytes > 3 { @b[queue] = count() }
after 1s { ` + report + ` }`)
			if err != nil {
				t.Fatal(err)
			}
			result, err := engine.Query(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if report == `emit @a; emit @b` {
				if len(result.Tables) != 2 {
					t.Fatal(result)
				}
				if string(result.Tables[0].Aggregation.Rows[0].Values[0]) != "29" || string(result.Tables[1].Aggregation.Rows[0].Values[0]) != "2" {
					t.Fatal(result)
				}
			} else if len(result.Tables) != 1 || len(result.Tables[0].Aggregation.Rows[0].Values) != 2 || string(result.Tables[0].Aggregation.Rows[0].Values[0]) != "29" || string(result.Tables[0].Aggregation.Rows[0].Values[1]) != "2" {
				t.Fatalf("join result: %+v", result)
			}
		}
		engine.Snapshots = func(ctx context.Context, r query.MonitorRequest) (query.Snapshot, error) {
			return query.Snapshot{Source: "memory", Memory: &query.MemoryInfo{Used: 41}}, nil
		}
		r, err := engine.ParseMonitorQuery(`jobs { @jobs[] = sum(bytes) } memory { @ram[] = avg(used) } after 1s { emit @jobs; emit @ram }`)
		if err != nil {
			t.Fatal(err)
		}
		result, err := engine.Query(context.Background(), r)
		if err != nil || len(result.Tables) != 2 || string(result.Tables[0].Aggregation.Rows[0].Values[0]) != "40" || string(result.Tables[1].Aggregation.Rows[0].Values[0]) != "41" {
			t.Fatalf("mixed built-in/custom script: %+v, %v", result, err)
		}
		r, err = engine.ParseMonitorQuery(`jobs { @jobs[] = sum(bytes) } every 500ms { emit @jobs; clear @jobs } after 1s { stop }`)
		if err != nil {
			t.Fatal(err)
		}
		result, err = engine.Query(context.Background(), r)
		if err != nil || len(result.Windows) != 2 || string(result.Windows[0].Rows[0].Values[0]) != "40" || len(result.Windows[1].Rows) != 1 || string(result.Windows[1].Rows[0].Values[0]) != "0" {
			t.Fatalf("periodic custom reports: %+v, %v", result, err)
		}
	})
}

func TestCustomSourceSnapshotAndValidation(t *testing.T) {
	engine := jobEngine()
	engine.Sources["inventory"] = query.CustomSource{
		Fields: []string{"queue"},
		Snapshots: func(ctx context.Context, r query.MonitorRequest) (query.Snapshot, error) {
			if r.Mode != "snapshot" || !reflect.DeepEqual(r.Filters, []query.SourceFilter{{Field: "queue", Values: []string{"fast"}}}) {
				t.Fatalf("snapshot selection: %+v", r)
			}
			return query.Snapshot{Source: r.Source, Time: time.Now(), Data: []jobRecord{{"fast", 13}}}, nil
		},
	}
	r, err := engine.ParseMonitorQuery(`inventory where queue = fast`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Query(context.Background(), r)
	if err != nil || !reflect.DeepEqual(snapshot.Data, []jobRecord{{"fast", 13}}) {
		t.Fatalf("snapshot: %+v, %v", snapshot, err)
	}
	metadata, err := engine.Metadata(query.MonitorRequest{Source: "jobs"})
	if err != nil || !reflect.DeepEqual(metadata.GroupByFields, []string{"queue", "bytes"}) || !reflect.DeepEqual(metadata.NumericFields, []string{"bytes"}) {
		t.Fatalf("metadata: %+v, %v", metadata, err)
	}
	for _, text := range []string{
		`jobs where missing = fast`, `jobs where queue = fast and queue = slow`,
		`jobs sum(queue) over 1s`, `jobs count over 1s by missing`,
		`jobs where queue > 7`, `jobs count over 1s every 100ms`,
		`inventory count over 1s`, `inventory where queue = fast and queue > 1`,
	} {
		if _, err := engine.ParseMonitorQuery(text); err == nil {
			t.Fatalf("invalid query accepted: %s", text)
		}
	}
	if _, err := query.ParseMonitorQuery("jobs"); err == nil {
		t.Fatal("custom source leaked into standalone parser")
	}
	if _, err := engine.Query(context.Background(), query.MonitorRequest{Source: "jobs"}); err == nil {
		t.Fatal("event-only source accepted as snapshot")
	}
	if err := engine.Validate(query.MonitorRequest{Source: "jobs", PID: 42}); err == nil {
		t.Fatal("built-in filters accepted for custom source")
	}
	engine.Sources["memory"] = engine.Sources["jobs"]
	r, err = engine.ParseMonitorQuery("memory")
	if err != nil || r.Mode != "snapshot" {
		t.Fatalf("built-in source overridden: %+v, %v", r, err)
	}
}
