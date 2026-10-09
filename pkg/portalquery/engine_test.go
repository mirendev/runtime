package query_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	query "miren.dev/runtime/pkg/portalquery"
)

func TestEngineMonitor(t *testing.T) {
	stop := errors.New("callback stopped")
	engine := query.Engine{Events: func(ctx context.Context, r query.MonitorRequest, emit func(query.Event) error) error {
		for _, pid := range []uint32{9, 42, 42} {
			if err := emit(query.Event{PID: pid, Syscall: 3}); err != nil {
				return err
			}
		}
		return nil
	}}
	var events []query.Event
	err := engine.Monitor(context.Background(), query.MonitorRequest{Source: "syscalls", PID: 42}, func(e query.Event) error {
		events = append(events, e)
		if len(events) == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || len(events) != 2 || events[0].PID != 42 || events[0].TAI64N >= events[1].TAI64N {
		t.Fatalf("filtered stream/callback/cursors: %+v, %v", events, err)
	}
	if err := query.ValidateTAI64N(events[0].TAI64N); err != nil {
		t.Fatal(err)
	}
}

func TestEngineDispatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eventCalls, snapshotCalls := 0, 0
		engine := query.Engine{
			Events: func(ctx context.Context, r query.MonitorRequest, emit func(query.Event) error) error {
				eventCalls++
				if err := emit(query.Event{Process: &query.ProcessEvent{Action: "start", Name: "worker"}}); err != nil {
					return err
				}
				<-ctx.Done()
				return ctx.Err()
			},
			Snapshots: func(ctx context.Context, r query.MonitorRequest) (query.Snapshot, error) {
				snapshotCalls++
				if r.Mode != "snapshot" || r.Aggregation != nil {
					t.Fatalf("snapshot selection: %+v", r)
				}
				return query.Snapshot{Source: r.Source, Time: time.Now(), Memory: &query.MemoryInfo{Used: 17}}, nil
			},
		}
		for _, tc := range []struct {
			text              string
			events, snapshots int
		}{
			{"memory", 0, 1},
			{"process count over 300ms", 1, 0},
			{"memory avg(used) over 300ms every 100ms", 0, 3},
			{`process { @starts[] = count() } memory { @ram[] = avg(used) } after 3s { emit @starts; emit @ram }`, 1, 3},
		} {
			eventCalls, snapshotCalls = 0, 0
			r, err := query.ParseMonitorQuery(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			result, err := engine.Query(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if eventCalls != tc.events || snapshotCalls != tc.snapshots {
				t.Fatalf("%s dispatch: events=%d snapshots=%d", tc.text, eventCalls, snapshotCalls)
			}
			switch result.Source {
			case "memory":
				if result.Aggregation == nil {
					if result.Memory.Used != 17 {
						t.Fatal(result)
					}
				} else if string(result.Aggregation.Values[0].Value) != "17" {
					t.Fatal(result)
				}
			case "process":
				if result.Aggregation.Counts[0].Count != 1 {
					t.Fatal(result)
				}
			case "script":
				if len(result.Tables) != 2 || string(result.Tables[0].Aggregation.Rows[0].Values[0]) != "1" || string(result.Tables[1].Aggregation.Rows[0].Values[0]) != "17" {
					t.Fatal(result)
				}
			}
		}
	})
}

func TestEngineBuiltInSnapshotAndValidation(t *testing.T) {
	result, err := (query.Engine{}).Query(context.Background(), query.MonitorRequest{Source: "memory"})
	if err != nil || result.Memory == nil || result.Memory.Total == 0 {
		t.Fatalf("local snapshot: %+v, %v", result, err)
	}
	engine := query.Engine{Events: func(context.Context, query.MonitorRequest, func(query.Event) error) error {
		t.Fatal("invalid request reached collector")
		return nil
	}}
	if _, err := engine.Query(context.Background(), query.MonitorRequest{Source: "unknown"}); err == nil {
		t.Fatal("invalid query accepted")
	}
	if err := engine.Monitor(context.Background(), query.MonitorRequest{Source: "process"}, nil); err == nil {
		t.Fatal("nil callback accepted")
	}
}
