package query

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"
)

// CPUInfo contains cumulative seconds, not instantaneous utilization percentages.
type CPUInfo struct {
	Name   string  `json:"name"`
	User   float64 `json:"user"`
	System float64 `json:"system"`
	Idle   float64 `json:"idle"`
	IOWait float64 `json:"iowait"`
	Steal  float64 `json:"steal"`
	Total  float64 `json:"total"` // Includes nice/IRQ/softIRQ accounting, without double-counting guest time.
}

type MemoryInfo struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"`
	Used      uint64 `json:"used"`
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`
}

// InterfaceInfo includes cumulative traffic counters, not per-second rates.
type InterfaceInfo struct {
	Name        string   `json:"name"`
	Index       int      `json:"index"`
	MTU         int      `json:"mtu"`
	Flags       []string `json:"flags"`
	Addresses   []string `json:"addresses"`
	BytesSent   uint64   `json:"bytes_sent"`
	BytesRecv   uint64   `json:"bytes_recv"`
	PacketsSent uint64   `json:"packets_sent"`
	PacketsRecv uint64   `json:"packets_recv"`
}

type KernelInfo struct {
	BootTime      time.Time       `json:"boot_time"`
	UptimeSeconds uint64          `json:"uptime_seconds"`
	Load1         float64         `json:"load_1"`
	Load5         float64         `json:"load_5"`
	Load15        float64         `json:"load_15"`
	Counters      *KernelCounters `json:"counters,omitempty"`
}

type KernelCounters struct {
	ContextSwitches  int `json:"context_switches"`
	ProcessesRunning int `json:"processes_running"`
	ProcessesBlocked int `json:"processes_blocked"`
}

type SensorInfo struct {
	Name        string  `json:"name"`
	Temperature float64 `json:"temperature_celsius"`
	High        float64 `json:"high_celsius"`
	Critical    float64 `json:"critical_celsius"`
}

type ContainerInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	State string `json:"state"`
}

// CgroupInfo describes a visible Linux cgroup v2 directory. Usage includes
// descendants; limits are configured locally, not effective ancestor limits.
type CgroupInfo struct {
	Path             string    `json:"path"`
	ID               string    `json:"id"` // Filesystem device/inode identity, used to detect recreated groups.
	CPUSeconds       *float64  `json:"cpu_seconds,omitempty"`
	CPULimitCores    *float64  `json:"cpu_limit_cores,omitempty"`
	MemoryBytes      *uint64   `json:"memory_bytes,omitempty"`
	MemoryLimitBytes *uint64   `json:"memory_limit_bytes,omitempty"`
	MemoryAnonBytes  *uint64   `json:"memory_anon_bytes,omitempty"`
	MemoryFileBytes  *uint64   `json:"memory_file_bytes,omitempty"`
	PIDsCurrent      *uint64   `json:"pids_current,omitempty"` // Counts tasks, including threads.
	IO               *CgroupIO `json:"io,omitempty"`
}

type CgroupIO struct {
	ReadBytes    *uint64          `json:"read_bytes,omitempty"`
	WriteBytes   *uint64          `json:"write_bytes,omitempty"`
	DiscardBytes *uint64          `json:"discard_bytes,omitempty"`
	ReadIOs      *uint64          `json:"read_ios,omitempty"`
	WriteIOs     *uint64          `json:"write_ios,omitempty"`
	DiscardIOs   *uint64          `json:"discard_ios,omitempty"`
	Devices      []CgroupIODevice `json:"devices"`
}

type CgroupIODevice struct {
	Device   string            `json:"device"`
	Counters map[string]uint64 `json:"counters"`
}

type GPUInfo struct {
	Index       int      `json:"index"`
	UUID        string   `json:"uuid"`
	Name        string   `json:"name"`
	Temperature *float64 `json:"temperature_celsius,omitempty"`
	Utilization *float64 `json:"utilization_percent,omitempty"`
	MemoryUsed  *uint64  `json:"memory_used_mib,omitempty"`
	MemoryTotal *uint64  `json:"memory_total_mib,omitempty"`
	Power       *float64 `json:"power_watts,omitempty"`
}

func querySnapshot(ctx context.Context, request MonitorRequest) (Snapshot, error) {
	if request.Source == "process" {
		return processSnapshot(ctx, request)
	}
	snapshot := Snapshot{Source: request.Source}
	switch request.Source {
	case "symbols":
		result, err := InspectSymbols(ctx, *request.Symbols)
		if err != nil {
			return Snapshot{}, err
		}
		snapshot.Symbols = &result
	case "cpu":
		times, err := cpu.TimesWithContext(ctx, true)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read CPU times: %w", err)
		}
		snapshot.CPU = make([]CPUInfo, 0, len(times))
		for _, t := range times {
			snapshot.CPU = append(snapshot.CPU, CPUInfo{Name: t.CPU, User: t.User, System: t.System, Idle: t.Idle, IOWait: t.Iowait, Steal: t.Steal, Total: t.Total() - t.Guest - t.GuestNice})
		}
	case "memory":
		ram, err := mem.VirtualMemoryWithContext(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read memory: %w", err)
		}
		swap, err := mem.SwapMemoryWithContext(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read swap: %w", err)
		}
		snapshot.Memory = &MemoryInfo{Total: ram.Total, Available: ram.Available, Used: ram.Used, SwapTotal: swap.Total, SwapUsed: swap.Used}
	case "network":
		interfaces, err := net.InterfacesWithContext(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read network interfaces: %w", err)
		}
		counters, err := net.IOCountersWithContext(ctx, true)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read network counters: %w", err)
		}
		byName := make(map[string]net.IOCountersStat, len(counters))
		for _, counter := range counters {
			byName[counter.Name] = counter
		}
		snapshot.Network = make([]InterfaceInfo, 0)
		for _, iface := range interfaces {
			if !processNameMatches(request.Name, iface.Name) {
				continue
			}
			entry := InterfaceInfo{Name: iface.Name, Index: iface.Index, MTU: iface.MTU, Flags: iface.Flags, Addresses: make([]string, 0, len(iface.Addrs))}
			for _, addr := range iface.Addrs {
				entry.Addresses = append(entry.Addresses, addr.Addr)
			}
			stat := byName[iface.Name]
			entry.BytesSent, entry.BytesRecv = stat.BytesSent, stat.BytesRecv
			entry.PacketsSent, entry.PacketsRecv = stat.PacketsSent, stat.PacketsRecv
			snapshot.Network = append(snapshot.Network, entry)
		}
		sort.Slice(snapshot.Network, func(i, j int) bool { return snapshot.Network[i].Name < snapshot.Network[j].Name })
	case "kernel":
		boot, err := host.BootTimeWithContext(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read boot time: %w", err)
		}
		uptime, err := host.UptimeWithContext(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read uptime: %w", err)
		}
		avg, err := load.AvgWithContext(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read load averages: %w", err)
		}
		snapshot.Kernel = &KernelInfo{BootTime: time.Unix(int64(boot), 0).UTC(), UptimeSeconds: uptime, Load1: avg.Load1, Load5: avg.Load5, Load15: avg.Load15}
		misc, err := load.MiscWithContext(ctx)
		if err != nil && runtime.GOOS == "linux" {
			return Snapshot{}, fmt.Errorf("read kernel counters: %w", err)
		}
		if err == nil {
			snapshot.Kernel.Counters = &KernelCounters{ContextSwitches: misc.Ctxt, ProcessesRunning: misc.ProcsRunning, ProcessesBlocked: misc.ProcsBlocked}
		}
	case "sensors":
		temperatures, err := sensors.TemperaturesWithContext(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read sensors: %w", err)
		}
		snapshot.Sensors = make([]SensorInfo, 0)
		for _, t := range temperatures {
			if processNameMatches(request.Name, t.SensorKey) {
				snapshot.Sensors = append(snapshot.Sensors, SensorInfo{Name: t.SensorKey, Temperature: t.Temperature, High: t.High, Critical: t.Critical})
			}
		}
		sort.Slice(snapshot.Sensors, func(i, j int) bool { return snapshot.Sensors[i].Name < snapshot.Sensors[j].Name })
	case "containers":
		var err error
		snapshot.Containers, err = readContainers(ctx, request.Name)
		if err != nil {
			return Snapshot{}, err
		}
	case "cgroups":
		var err error
		snapshot.Cgroups, err = readCgroups(ctx, request.Path)
		if err != nil {
			return Snapshot{}, err
		}
	case "gpu":
		var err error
		snapshot.GPUs, err = readGPUs(ctx, request.Name)
		if err != nil {
			return Snapshot{}, err
		}
	}
	snapshot.Time = time.Now().UTC()
	return snapshot, nil
}
