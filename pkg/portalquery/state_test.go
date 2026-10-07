package query

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/net"
)

func TestHostSnapshots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, source := range []string{"cpu", "memory", "network", "kernel", "sensors"} {
		t.Run(source, func(t *testing.T) {
			request := MonitorRequest{Source: source, Mode: "snapshot"}
			if err := request.Validate(); err != nil {
				t.Fatal(err)
			}
			got, err := querySnapshot(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if got.Source != source || got.Time.IsZero() {
				t.Fatalf("wrong snapshot identity: %+v", got)
			}
			switch source {
			case "cpu":
				if len(got.CPU) == 0 || got.CPU[0].Name == "" || got.CPU[0].Idle+got.CPU[0].User+got.CPU[0].System <= 0 {
					t.Fatalf("invalid CPU counters: %+v", got.CPU)
				}
			case "memory":
				if got.Memory == nil || got.Memory.Total == 0 || got.Memory.Available > got.Memory.Total {
					t.Fatalf("invalid memory snapshot: %+v", got.Memory)
				}
			case "network":
				if len(got.Network) == 0 || got.Network[0].Name == "" {
					t.Fatalf("invalid interface snapshot: %+v", got.Network)
				}
			case "kernel":
				if got.Kernel == nil || got.Kernel.BootTime.IsZero() || got.Kernel.UptimeSeconds == 0 {
					t.Fatalf("invalid kernel snapshot: %+v", got.Kernel)
				}
			}
		})
	}
	interfaces, err := net.InterfacesWithContext(ctx)
	if err != nil || len(interfaces) == 0 {
		t.Fatalf("no network interfaces: %v", err)
	}
	selected, err := querySnapshot(ctx, MonitorRequest{Source: "network", Mode: "snapshot", Name: interfaces[0].Name})
	if err != nil || len(selected.Network) != 1 || selected.Network[0].Name != interfaces[0].Name {
		t.Fatalf("interface filter: %+v, %v", selected, err)
	}
	missing, err := querySnapshot(ctx, MonitorRequest{Source: "sensors", Mode: "snapshot", Name: "no-such-sensor-key-xyz"})
	if err != nil || len(missing.Sensors) != 0 {
		t.Fatalf("sensor filter: %+v, %v", missing, err)
	}
	for _, tc := range []struct {
		snapshot Snapshot
		field    string
	}{
		{missing, "sensors"}, {Snapshot{Source: "network", Network: []InterfaceInfo{}}, "network"},
		{Snapshot{Source: "process", Processes: []ProcessInfo{}}, "processes"},
		{Snapshot{Source: "containers", Containers: []ContainerInfo{}}, "containers"},
		{Snapshot{Source: "gpu", GPUs: []GPUInfo{}}, "gpus"},
	} {
		encoded, err := json.Marshal(tc.snapshot)
		if err != nil || !strings.Contains(string(encoded), `"`+tc.field+`":[]`) {
			t.Fatalf("empty %s collection not encoded: %s, %v", tc.field, encoded, err)
		}
	}
}
