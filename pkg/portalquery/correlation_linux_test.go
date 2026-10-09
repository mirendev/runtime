//go:build linux

package query

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"
)

func TestInventoryCorrelationCgroupFilesystem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mount := t.TempDir()
		for name, io := range map[string]string{
			"app/worker": "8:0 wbytes=100 wios=2\n", "app/pause": "8:0 wbytes=30 wios=7\n",
			"app/worker/child": "8:0 wbytes=90 wios=1\n", "other": "8:0 wbytes=90000 wios=999\n",
		} {
			dir := filepath.Join(mount, name)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "io.stat"), []byte(io), 0600); err != nil {
				t.Fatal(err)
			}
		}
		engine := inventoryEngine([]map[string]any{{"app": "a", "cgroup": "/app/worker"}, {"app": "a", "cgroup": "/app/pause"}, {"app": "b", "cgroup": "/other"}})
		start := time.Now()
		engine.Snapshots = func(ctx context.Context, r MonitorRequest) (Snapshot, error) {
			if time.Since(start) == time.Second {
				value := "8:0 wbytes=117 wios=5\n"
				if r.Path == "/app/pause" {
					value = "8:0 wbytes=41 wios=8\n"
				}
				if err := os.WriteFile(filepath.Join(mount, r.Path, "io.stat"), []byte(value), 0600); err != nil {
					return Snapshot{}, err
				}
			}
			groups, err := cgroupSnapshot(ctx, mount, r.Path)
			return Snapshot{Source: "cgroups", Cgroups: groups}, err
		}
		r, err := engine.ParseMonitorQuery(`cgroups using (workloads where app = a) on path = cgroup rate(io.write_bytes), rate(io.write_ios) over 2s every 1s by inventory.app`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := engine.Query(context.Background(), r)
		if err != nil || len(got.Aggregation.Metrics[0].Values) != 1 || string(got.Aggregation.Metrics[0].Values[0].Value) != "28" || string(got.Aggregation.Metrics[1].Values[0].Value) != "4" {
			t.Fatalf("filesystem accounting includes child/other or loses containers: %+v, %v", got, err)
		}
		// A path absent on disk is an empty snapshot, not an error or a zero.
		groups, err := cgroupSnapshot(context.Background(), mount, "/gone")
		if err != nil || len(groups) != 0 {
			t.Fatalf("missing target: %+v, %v", groups, err)
		}
		// Exact reads never read a corrupt sibling or the selected group's child.
		for _, name := range []string{"other", "app/worker/child"} {
			if err := os.WriteFile(filepath.Join(mount, name, "io.stat"), []byte("invalid"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		r, err = engine.ParseMonitorQuery(`cgroups using (workloads where app = a) on path = cgroup`)
		if err != nil {
			t.Fatal(err)
		}
		got, err = engine.Query(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		rows := got.Data.([]map[string]any)
		if len(rows) != 2 || rows[0]["io.write_bytes"] != json.Number("41") || rows[1]["io.write_bytes"] != json.Number("117") {
			t.Fatalf("exact lifetime counters: %+v", rows)
		}
	})
}
