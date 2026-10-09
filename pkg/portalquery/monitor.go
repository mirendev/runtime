package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// MonitorRequest selects a source and its server-side filters. Source is
// "syscalls", "packets", "process", "disk", "tracepoint", or a snapshot-only source;
// unrelated filters must be omitted. "script" selects concurrent named aggregates
// in Probes, with shared timing in Aggregation and no root-level filters.
type MonitorRequest struct {
	Source       string               `json:"source"`
	Mode         string               `json:"mode,omitempty"` // empty for events, "snapshot" or "aggregate" for queries
	Aggregation  *AggregationRequest  `json:"aggregation,omitempty"`
	Probes       []MonitorRequest     `json:"probes,omitempty"`  // script: independent selections sharing aggregation timing
	Reports      []AggregateReport    `json:"reports,omitempty"` // script: post-aggregation emits, including joins
	PID          uint32               `json:"pid,omitempty"`
	Syscalls     []int                `json:"syscalls,omitempty"`
	SyscallNames []string             `json:"syscall_names,omitempty"` // resolved against the server's native Linux ABI
	Phase        string               `json:"phase,omitempty"`         // syscalls/disk: entry (default) or completion
	Paths        bool                 `json:"paths,omitempty"`         // best-effort syscall FD path resolution
	Packet       *PacketFilter        `json:"packet,omitempty"`
	Process      *ProcessFilter       `json:"process,omitempty"`
	Disk         *DiskFilter          `json:"disk,omitempty"`
	Tracepoint   *TracepointFilter    `json:"tracepoint,omitempty"`
	Symbols      *SymbolRequest       `json:"symbols,omitempty"`
	Stacks       *StackCapture        `json:"stacks,omitempty"`
	Name         string               `json:"name,omitempty"`          // network interface or sensor key (exact or edge glob)
	Path         string               `json:"path,omitempty"`          // cgroup path relative to the visible v2 mount (exact or edge glob)
	EventFilters map[string]string    `json:"event_filters,omitempty"` // task-context event strings; AND, exact or one edge glob
	Comparisons  []NumericComparison  `json:"comparisons,omitempty"`   // event numeric predicates; AND, applied before delivery/reduction
	FileDepth    int                  `json:"file_depth,omitempty"`    // aggregate file.dir prefix depth; zero retains full directory
	Filters      []SourceFilter       `json:"filters,omitempty"`       // custom-source predicates
	Using        *SnapshotCorrelation `json:"using,omitempty"`         // frozen inventory selection before snapshot collection
	customSource *CustomSource
}

// NumericComparison compares an event field to an exact numeric threshold.
// Value is textual to preserve full-width integer precision in signed requests.
type NumericComparison struct {
	Field string `json:"field"`
	Op    string `json:"op"` // >, >=, <, <=
	Value string `json:"value"`
}

var comparisonNumber = regexp.MustCompile(`^-?(?:[0-9]+(?:\.[0-9]+)?|0[xX][0-9a-fA-F]+)$`)

func comparisonValue(text string) (*big.Rat, bool) {
	if len(text) > 128 || !comparisonNumber.MatchString(text) {
		return nil, false
	}
	if !strings.Contains(text, ".") && !strings.ContainsAny(text, "xX") {
		n, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return nil, false
		}
		return new(big.Rat).SetInt(n), true
	}
	return new(big.Rat).SetString(text)
}

// TracepointFilter selects scalar fields from a Linux tracepoint. Equals values
// use decimal or 0x notation; every filtered field must also be selected.
// An empty selection captures task identity/stacks without reading payload fields.
type TracepointFilter struct {
	Event  string            `json:"event"` // category:name
	Fields []string          `json:"fields"`
	Equals map[string]string `json:"equals,omitempty"`
}

// TracepointEvent contains values interpreted using the server kernel's format.
// json.Number preserves full-width signed and unsigned 64-bit values on clients.
type TracepointEvent struct {
	Event  string                 `json:"event"`
	Fields map[string]json.Number `json:"fields"`
}

var tracepointIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*$`)

func (f TracepointFilter) validateTracepoint() error {
	parts := strings.Split(f.Event, ":")
	if len(parts) != 2 || !tracepointIdentifier.MatchString(parts[0]) || !tracepointIdentifier.MatchString(parts[1]) || len(f.Event) > 128 {
		return errors.New("tracepoint event must be category:name")
	}
	if len(f.Fields) > 16 {
		return errors.New("tracepoint allows at most 16 fields")
	}
	seen := make(map[string]bool, len(f.Fields))
	for _, name := range f.Fields {
		if len(name) > 64 || !tracepointIdentifier.MatchString(name) || seen[name] {
			return fmt.Errorf("invalid or duplicate tracepoint field %q", name)
		}
		seen[name] = true
	}
	for name, value := range f.Equals {
		if !seen[name] {
			return fmt.Errorf("tracepoint filter field %q must be selected", name)
		}
		if _, err := parseTracepointNumber(value); err != nil {
			return fmt.Errorf("invalid tracepoint value %q: %w", value, err)
		}
	}
	return nil
}

func parseTracepointNumber(value string) (uint64, error) {
	if strings.HasPrefix(value, "-") {
		n, err := strconv.ParseInt(value, 10, 64)
		return uint64(n), err
	}
	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 0
	}
	return strconv.ParseUint(value, base, 64)
}

// DiskFilter selects block requests by kernel device ID and operation.
type DiskFilter struct {
	Device    uint32 `json:"device,omitempty"`
	Operation string `json:"operation,omitempty"` // read, write, discard, or flush
}

// DiskEvent describes a block request issued to a device. Sector units are 512 bytes.
type DiskEvent struct {
	Device        uint32  `json:"device"`
	Sector        uint64  `json:"sector"`
	Sectors       uint32  `json:"sectors"`
	Operation     string  `json:"operation"`
	RWBS          string  `json:"rwbs,omitempty"`
	DeviceName    string  `json:"device_name,omitempty"`
	RequestFlags  *uint32 `json:"request_flags,omitempty"`
	DurationNS    *uint64 `json:"duration_ns,omitempty"`
	Status        *uint32 `json:"status,omitempty"`       // completion blk_status_t; zero is success, not errno
	IOCgroupID    *uint64 `json:"io_cgroup_id,omitempty"` // kernel charged blkcg kernfs ID, not issuing task membership
	IOCgroupPath  string  `json:"io_cgroup_path,omitempty"`
	IOCgroupError string  `json:"io_cgroup_error,omitempty"`
}

// SyscallFile is a best-effort kernel path snapshot captured at syscall entry
// and retained through completion. Concurrent rename/descriptor changes may race.
type SyscallFile struct {
	FD    int32  `json:"fd"`
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

// ProcessFilter selects process lifecycle events. Empty fields match all.
type ProcessFilter struct {
	Name   string `json:"name,omitempty"`   // exact, prefix*, or *suffix
	Action string `json:"action,omitempty"` // start or exit
}

// ProcessEvent describes a start or exit observed by the process poller.
type ProcessEvent struct {
	Name   string `json:"name"`
	Action string `json:"action"`
}

// ProcessInfo is a process visible to the server at snapshot time.
type ProcessInfo struct {
	PID         uint32    `json:"pid"`
	Name        string    `json:"name"`
	Started     time.Time `json:"started"`
	CPUSeconds  *float64  `json:"cpu_seconds,omitempty"` // Cumulative user + system CPU time, excluding children.
	RSSBytes    *uint64   `json:"rss_bytes,omitempty"`
	VMSBytes    *uint64   `json:"vms_bytes,omitempty"`
	User        string    `json:"user,omitempty"`
	State       string    `json:"state,omitempty"`
	Threads     *int32    `json:"threads,omitempty"`
	CommandLine string    `json:"command_line,omitempty"`
}

// Snapshot is a one-shot view, not a stream of lifecycle events.
type Snapshot struct {
	Source       string               `json:"source"`
	Time         time.Time            `json:"time"`
	Processes    []ProcessInfo        `json:"processes,omitempty"`
	CPU          []CPUInfo            `json:"cpu,omitempty"`
	Memory       *MemoryInfo          `json:"memory,omitempty"`
	Network      []InterfaceInfo      `json:"network,omitempty"`
	Kernel       *KernelInfo          `json:"kernel,omitempty"`
	Sensors      []SensorInfo         `json:"sensors,omitempty"`
	Containers   []ContainerInfo      `json:"containers,omitempty"`
	Cgroups      []CgroupInfo         `json:"cgroups,omitempty"`
	GPUs         []GPUInfo            `json:"gpus,omitempty"`
	Aggregation  *AggregationResult   `json:"aggregation,omitempty"`
	Windows      []*AggregationResult `json:"windows,omitempty"` // finite periodic reports, returned together at query completion
	Tables       []Snapshot           `json:"tables,omitempty"`  // multi-selector results in selector order
	Capabilities *Capabilities        `json:"capabilities,omitempty"`
	Symbols      *SymbolResult        `json:"symbols,omitempty"`
	Data         any                  `json:"data,omitempty"` // integration-specific snapshot payload
	sampleRows   []map[string]any     // engine-owned enriched sampling records
}

// MarshalJSON keeps empty collections visible for the selected source while
// excluding fields from unrelated sources.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	type fields Snapshot
	encoded, err := json.Marshal(fields(s))
	if err != nil {
		return nil, err
	}
	if s.Aggregation != nil || s.Windows != nil || s.Data != nil {
		return encoded, nil
	}
	var collection string
	switch s.Source {
	case "process":
		collection = "processes"
	case "cpu":
		collection = "cpu"
	case "network":
		collection = "network"
	case "sensors":
		collection = "sensors"
	case "containers":
		collection = "containers"
	case "cgroups":
		collection = "cgroups"
	case "gpu":
		collection = "gpus"
	}
	if collection == "" {
		return encoded, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, err
	}
	if _, ok := object[collection]; !ok {
		object[collection] = json.RawMessage(`[]`)
	}
	return json.Marshal(object)
}

// PacketFilter matches packet headers. Empty fields match any value. Direction
// is "incoming" or "outgoing" relative to the server's network interface.
// Protocol is "tcp" or "udp"; port filters only match those protocols.
type PacketFilter struct {
	Protocol        string `json:"protocol,omitempty"`
	Direction       string `json:"direction,omitempty"`
	SourceIP        string `json:"source_ip,omitempty"`
	DestinationIP   string `json:"destination_ip,omitempty"`
	SourcePort      uint16 `json:"source_port,omitempty"`
	DestinationPort uint16 `json:"destination_port,omitempty"`
}

// PacketEvent contains packet endpoints and up to 2048 raw link-layer bytes.
// Data is base64-encoded in JSON. No TCP stream reassembly is performed.
type PacketEvent struct {
	Protocol        string `json:"protocol"`
	Direction       string `json:"direction"`
	SourceIP        string `json:"source_ip"`
	DestinationIP   string `json:"destination_ip"`
	SourcePort      uint16 `json:"source_port,omitempty"`
	DestinationPort uint16 `json:"destination_port,omitempty"`
	Length          int    `json:"length"`
	Data            []byte `json:"data"`
}

// CollectionStats are cumulative subscription counters, not per-event deltas.
// StackCaptureFailures includes StackCollisions. Kernel drops are not per-group.
type CollectionStats struct {
	RingBufferDropped       uint64 `json:"ring_buffer_dropped"`
	StackCaptureFailures    uint64 `json:"stack_capture_failures"`
	StackCollisions         uint64 `json:"stack_collisions"`
	PairingFailures         uint64 `json:"pairing_failures"`
	UnmatchedExits          uint64 `json:"unmatched_exits"`
	BlockIssues             uint64 `json:"block_issues,omitempty"`
	BlockCompletions        uint64 `json:"block_completions,omitempty"`
	BlockReissues           uint64 `json:"block_reissues,omitempty"`
	BlockPartialCompletions uint64 `json:"block_partial_completions,omitempty"`
	BlockFinalCompletions   uint64 `json:"block_final_completions,omitempty"`
}

// Event is a source data record or a collection_stats diagnostic.
type Event struct {
	Time        time.Time        `json:"time"`
	TAI64N      string           `json:"tai64n"`
	PID         uint32           `json:"pid,omitempty"`
	TID         uint32           `json:"tid,omitempty"`
	Name        string           `json:"name,omitempty"`         // current task comm, up to 15 bytes; may differ between threads
	ProcessName string           `json:"process_name,omitempty"` // best-effort full executable basename, resolved on receipt
	NameGroup   string           `json:"name_group,omitempty"`   // kernel workers normalized for grouping; otherwise task name
	CgroupPath  string           `json:"cgroup_path,omitempty"`  // best-effort unified cgroup path of the event thread at receipt
	Phase       string           `json:"phase,omitempty"`
	DurationNS  *uint64          `json:"duration_ns,omitempty"`
	ReturnValue *int64           `json:"return_value,omitempty"`
	File        *SyscallFile     `json:"file,omitempty"`
	Collection  *CollectionStats `json:"collection,omitempty"` // cumulative subscription counters; not additive
	Kind        string           `json:"kind,omitempty"`       // collection_stats for diagnostic-only records
	Syscall     int              `json:"syscall,omitempty"`
	Packet      *PacketEvent     `json:"packet,omitempty"`
	Process     *ProcessEvent    `json:"process,omitempty"`
	Disk        *DiskEvent       `json:"disk,omitempty"`
	Tracepoint  *TracepointEvent `json:"tracepoint,omitempty"`
	UserStack   *CapturedStack   `json:"user_stack,omitempty"`
	KernelStack *CapturedStack   `json:"kernel_stack,omitempty"`
	Data        any              `json:"data,omitempty"`   // integration-specific typed payload
	Fields      map[string]any   `json:"fields,omitempty"` // flat custom-source query fields
}

// MarshalJSON keeps syscall number zero visible without putting a spurious
// syscall field on non-syscall events.
func (e Event) MarshalJSON() ([]byte, error) {
	type fields Event
	if e.Disk != nil || e.Tracepoint != nil {
		return json.Marshal(struct {
			fields
			PID  uint32 `json:"pid"`
			TID  uint32 `json:"tid"`
			Name string `json:"name"`
		}{fields(e), e.PID, e.TID, e.Name})
	}
	if e.Packet != nil || e.Process != nil || e.Kind == "collection_stats" || e.Data != nil || e.Fields != nil {
		return json.Marshal(fields(e))
	}
	return json.Marshal(struct {
		fields
		PID     uint32 `json:"pid"`
		TID     uint32 `json:"tid"`
		Syscall int    `json:"syscall"`
	}{fields(e), e.PID, e.TID, e.Syscall})
}

const maxScriptProbes = 8

func validateScript(r MonitorRequest) error {
	if r.Mode != "aggregate" || r.Aggregation == nil || len(r.Probes) == 0 || len(r.Probes) > maxScriptProbes {
		return errors.New("script requires aggregate mode, shared timing and 1–8 probes")
	}
	selection := r
	selection.Source, selection.Mode, selection.Aggregation, selection.Probes, selection.Reports = "", "", nil, nil, nil
	if !reflect.DeepEqual(selection, MonitorRequest{}) || !reflect.DeepEqual(*r.Aggregation, AggregationRequest{Window: r.Aggregation.Window, ReportEvery: r.Aggregation.ReportEvery}) {
		return errors.New("script filters and result controls belong to individual probes")
	}
	tables := make(map[string]bool)
	for _, probe := range r.Probes {
		if probe.Source == "script" || len(probe.Probes) != 0 || probe.Mode != "aggregate" || probe.Aggregation == nil {
			return errors.New("script probes must be non-nested aggregates")
		}
		if err := probe.Validate(); err != nil {
			return fmt.Errorf("selector %s: %w", probe.Source, err)
		}
		a := probe.Aggregation
		if a.Table == "" || tables[a.Table] {
			return errors.New("script tables must have unique nonempty names")
		}
		tables[a.Table] = true
		if a.Window != r.Aggregation.Window || a.ReportEvery != r.Aggregation.ReportEvery {
			return errors.New("script tables must share the same window and reporting interval")
		}
	}
	return validateAggregateReports(r)
}

// Validate checks that the request selects a supported source and that all
// source-specific filters and controls are internally consistent.
func (r MonitorRequest) Validate() error {
	if r.Using != nil {
		if err := r.Using.validate(r); err != nil {
			return err
		}
	}
	if r.customSource == nil && len(r.Filters) != 0 {
		return errors.New("custom filters require a registered custom source")
	}
	if r.Source == "script" {
		return validateScript(r)
	}
	if len(r.Probes) != 0 || len(r.Reports) != 0 {
		return errors.New("probes and reports require a script query")
	}
	if len(r.Comparisons) != 0 {
		if r.Mode == "snapshot" || sampledAggregation(r) || len(r.Comparisons) > 16 {
			return errors.New("numeric comparisons require event sources and at most 16 predicates")
		}
		_, numeric, err := aggregateFields(r)
		if err != nil {
			return err
		}
		seen := make(map[string]bool)
		for _, c := range r.Comparisons {
			if !slices.Contains(numeric, c.Field) {
				return fmt.Errorf("comparison requires an available numeric event field, got %q (duration_ns requires completion)", c.Field)
			}
			if c.Op != ">" && c.Op != ">=" && c.Op != "<" && c.Op != "<=" {
				return fmt.Errorf("unsupported comparison operator %q; use >, >=, < or <=", c.Op)
			}
			if _, ok := comparisonValue(c.Value); !ok {
				return fmt.Errorf("comparison field %q requires a decimal or hexadecimal number up to 128 bytes", c.Field)
			}
			key := c.Field + c.Op
			if seen[key] {
				return fmt.Errorf("duplicate comparison for %s %s", c.Field, c.Op)
			}
			seen[key] = true
		}
	}
	if r.FileDepth < 0 || r.FileDepth > 32 || (r.FileDepth != 0 && (r.Source != "syscalls" || !r.Paths || r.Mode != "aggregate")) {
		return errors.New("file.depth requires syscall path aggregation and must be between 0 and 32")
	}
	for field, pattern := range r.EventFilters {
		if r.Source != "syscalls" && r.Source != "disk" && r.Source != "tracepoint" {
			return errors.New("event filters require syscalls, disk or tracepoint")
		}
		switch field {
		case "name", "process_name", "name_group", "cgroup.path":
		case "device_name", "rwbs", "io.cgroup.path":
			if r.Source != "disk" {
				return fmt.Errorf("%s requires disk source", field)
			}
		default:
			return fmt.Errorf("unknown event filter %q", field)
		}
		if pattern == "" || len(pattern) > 4096 || !validEdgeGlob(pattern) {
			return fmt.Errorf("%s requires a nonempty exact value or one edge glob", field)
		}
	}
	if len(r.SyscallNames) != 0 && r.Source != "syscalls" {
		return errors.New("syscall names require syscalls source")
	}
	for _, name := range r.SyscallNames {
		if !validSyscallName(name) {
			return fmt.Errorf("invalid syscall name %q", name)
		}
	}
	if r.Phase != "" && ((r.Source != "syscalls" && r.Source != "disk") || (r.Phase != "entry" && r.Phase != "completion")) {
		return errors.New("phase is only for syscalls/disk: entry or completion")
	}
	if r.Paths && r.Source != "syscalls" {
		return errors.New("paths requires syscalls source")
	}
	if r.Symbols != nil && r.Source != "symbols" {
		return errors.New("symbols spec requires symbols source")
	}
	if r.Stacks != nil {
		if r.Source != "syscalls" && r.Source != "tracepoint" {
			return errors.New("stack capture requires syscalls or tracepoint")
		}
		if err := r.Stacks.Validate(); err != nil {
			return err
		}
		if r.Mode != "aggregate" && (r.Stacks.UserShape != nil || r.Stacks.KernelShape != nil) {
			return errors.New("stack shaping requires aggregate mode")
		}
	}
	if r.Mode != "" && r.Mode != "snapshot" && r.Mode != "aggregate" {
		return errors.New("unsupported monitor mode")
	}
	if r.Mode == "aggregate" {
		if r.Aggregation == nil {
			return errors.New("aggregation spec required")
		}
		if err := r.Aggregation.validate(r); err != nil {
			return err
		}
	} else if r.Aggregation != nil {
		return errors.New("aggregation requires aggregate mode")
	}
	if r.customSource != nil {
		return r.validateCustomSource()
	}
	if r.Mode == "snapshot" && r.Source != "process" && r.Source != "cpu" && r.Source != "memory" && r.Source != "network" && r.Source != "kernel" && r.Source != "sensors" && r.Source != "containers" && r.Source != "cgroups" && r.Source != "gpu" && r.Source != "capabilities" && r.Source != "symbols" {
		return errors.New("unsupported snapshot source")
	}
	if r.Path != "" && (r.Source != "cgroups" || !validEdgeGlob(r.Path)) {
		return errors.New("path is a cgroups filter and supports only a single leading or trailing *")
	}
	if r.Name != "" && !validEdgeGlob(r.Name) {
		return errors.New("name supports only a single leading or trailing *")
	}
	switch r.Source {
	case "symbols":
		if r.Mode != "snapshot" || r.Symbols == nil || r.PID != 0 || len(r.Syscalls) != 0 || r.Packet != nil || r.Process != nil || r.Disk != nil || r.Tracepoint != nil || r.Name != "" || r.Path != "" {
			return errors.New("symbols requires snapshot mode, symbols spec and no unrelated filters")
		}
		return r.Symbols.Validate()
	case "tracepoint":
		if r.PID != 0 || len(r.Syscalls) != 0 || r.Packet != nil || r.Process != nil || r.Disk != nil || r.Name != "" || r.Tracepoint == nil {
			return errors.New("tracepoint source requires a tracepoint spec and no unrelated filters")
		}
		return r.Tracepoint.validateTracepoint()
	case "syscalls":
		if r.Packet != nil || r.Process != nil || r.Disk != nil || r.Tracepoint != nil || r.Name != "" {
			return errors.New("unrelated filter for syscalls source")
		}
	case "process":
		if len(r.Syscalls) != 0 || r.Packet != nil || r.Disk != nil || r.Tracepoint != nil || r.Name != "" {
			return errors.New("unrelated filter for process source")
		}
		if r.Process != nil && !validEdgeGlob(r.Process.Name) {
			return errors.New("process name supports only a single leading or trailing *")
		}
		if r.Process != nil && r.Process.Action != "" && r.Process.Action != "start" && r.Process.Action != "exit" {
			return errors.New("process action must be start or exit")
		}
		if (r.Mode == "snapshot" || sampledAggregation(r)) && r.Process != nil && r.Process.Action != "" {
			return errors.New("process action is not a snapshot filter")
		}
	case "packets":
		if r.PID != 0 || len(r.Syscalls) != 0 || r.Process != nil || r.Disk != nil || r.Tracepoint != nil || r.Name != "" {
			return errors.New("unrelated filter for packets source")
		}
		if r.Packet != nil {
			p := r.Packet
			if p.Protocol != "" && p.Protocol != "tcp" && p.Protocol != "udp" {
				return errors.New("packet protocol must be tcp or udp")
			}
			if p.Direction != "" && p.Direction != "incoming" && p.Direction != "outgoing" {
				return errors.New("packet direction must be incoming or outgoing")
			}
			for _, ip := range []string{p.SourceIP, p.DestinationIP} {
				if ip != "" {
					if _, err := netip.ParseAddr(ip); err != nil {
						return fmt.Errorf("invalid packet IP address %q: %w", ip, err)
					}
				}
			}
			if (p.SourcePort != 0 || p.DestinationPort != 0) && p.Protocol == "" {
				return errors.New("port filters require tcp or udp protocol")
			}
		}
	case "disk":
		if r.PID != 0 || len(r.Syscalls) != 0 || r.Packet != nil || r.Process != nil || r.Tracepoint != nil || r.Name != "" {
			return errors.New("unrelated filter for disk source")
		}
		if r.Disk != nil && r.Disk.Operation != "" && r.Disk.Operation != "read" && r.Disk.Operation != "write" && r.Disk.Operation != "discard" && r.Disk.Operation != "flush" {
			return errors.New("disk operation must be read, write, discard or flush")
		}
	case "cpu", "memory", "network", "kernel", "sensors", "containers", "cgroups", "gpu", "capabilities":
		if (r.Mode != "snapshot" && !(r.Mode == "aggregate" && sampledAggregation(r))) || r.PID != 0 || len(r.Syscalls) != 0 || r.Packet != nil || r.Process != nil || r.Disk != nil || r.Tracepoint != nil ||
			(r.Name != "" && r.Source != "network" && r.Source != "sensors" && r.Source != "containers" && r.Source != "gpu") {
			return errors.New("invalid snapshot source filters")
		}
	default:
		return errors.New("unsupported monitor source")
	}
	if len(r.Syscalls)+len(r.SyscallNames) > 256 {
		return errors.New("too many syscall filters")
	}
	for _, id := range r.Syscalls {
		if id < 0 || id > 65535 {
			return errors.New("invalid syscall number")
		}
	}
	return nil
}

// Matches reports whether an event satisfies the request's user-space filters.
func (r MonitorRequest) Matches(event Event) bool {
	if event.Kind == "collection_stats" {
		return true
	}
	if len(r.Comparisons) != 0 {
		fields := eventGroupFields(event, nil)
		for _, c := range r.Comparisons {
			value, exists := fields[c.Field]
			if !exists || value == nil {
				return false
			}
			data, err := json.Marshal(value)
			actual, ok := new(big.Rat).SetString(string(data))
			threshold, valid := comparisonValue(c.Value)
			if err != nil || !ok || !valid {
				return false
			}
			order := actual.Cmp(threshold)
			switch c.Op {
			case ">":
				if order <= 0 {
					return false
				}
			case ">=":
				if order < 0 {
					return false
				}
			case "<":
				if order >= 0 {
					return false
				}
			case "<=":
				if order > 0 {
					return false
				}
			default:
				return false
			}
		}
	}
	if len(r.EventFilters) != 0 {
		for field, pattern := range r.EventFilters {
			var value string
			switch field {
			case "name":
				value = event.Name
			case "process_name":
				value = event.ProcessName
			case "name_group":
				value = event.NameGroup
				if value == "" {
					value = event.Name
				}
			case "cgroup.path":
				value = event.CgroupPath
			case "device_name":
				if event.Disk != nil {
					value = event.Disk.DeviceName
				}
			case "rwbs":
				if event.Disk != nil {
					value = event.Disk.RWBS
				}
			case "io.cgroup.path":
				if event.Disk != nil {
					value = event.Disk.IOCgroupPath
				}
			}
			if value == "" || !processNameMatches(pattern, value) {
				return false
			}
		}
	}
	if r.customSource != nil {
		for _, filter := range r.Filters {
			value, ok := event.Fields[filter.Field]
			if !ok || !slices.Contains(filter.Values, fmt.Sprint(value)) {
				return false
			}
		}
		return true
	}
	if r.Source == "tracepoint" {
		if event.Tracepoint == nil || r.Tracepoint == nil || event.Tracepoint.Event != r.Tracepoint.Event {
			return false
		}
		for name, expected := range r.Tracepoint.Equals {
			value, ok := event.Tracepoint.Fields[name]
			if !ok {
				return false
			}
			a, errA := parseTracepointNumber(value.String())
			b, errB := parseTracepointNumber(expected)
			if errA != nil || errB != nil || a != b {
				return false
			}
		}
		return true
	}
	if r.Source == "disk" {
		if event.Disk == nil {
			return false
		}
		return r.Disk == nil || ((r.Disk.Device == 0 || r.Disk.Device == event.Disk.Device) &&
			(r.Disk.Operation == "" || r.Disk.Operation == event.Disk.Operation))
	}
	if r.Source == "process" {
		if event.Process == nil || (r.PID != 0 && r.PID != event.PID) {
			return false
		}
		return r.Process == nil ||
			(processNameMatches(r.Process.Name, event.Process.Name) &&
				(r.Process.Action == "" || r.Process.Action == event.Process.Action))
	}
	if r.Source == "packets" {
		if event.Packet == nil {
			return false
		}
		if r.Packet == nil {
			return true
		}
		p, f := event.Packet, r.Packet
		return (f.Protocol == "" || f.Protocol == p.Protocol) &&
			(f.Direction == "" || f.Direction == p.Direction) &&
			packetIPMatches(f.SourceIP, p.SourceIP) &&
			packetIPMatches(f.DestinationIP, p.DestinationIP) &&
			(f.SourcePort == 0 || f.SourcePort == p.SourcePort) &&
			(f.DestinationPort == 0 || f.DestinationPort == p.DestinationPort)
	}
	if event.Packet != nil || event.Process != nil || event.Disk != nil || event.Tracepoint != nil || event.Data != nil || event.Fields != nil {
		return false
	}
	if r.PID != 0 && r.PID != event.PID {
		return false
	}
	if len(r.Syscalls) == 0 {
		return true
	}
	for _, id := range r.Syscalls {
		if id == event.Syscall {
			return true
		}
	}
	return false
}

func processNameMatches(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(name, pattern[1:])
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(name, pattern[:len(pattern)-1])
	}
	return pattern == name
}

func validEdgeGlob(pattern string) bool {
	return !strings.Contains(pattern, "*") ||
		(strings.Count(pattern, "*") == 1 && len(pattern) > 1 &&
			(pattern[0] == '*' || pattern[len(pattern)-1] == '*'))
}

func packetIPMatches(filter, actual string) bool {
	if filter == "" {
		return true
	}
	ip, err := netip.ParseAddr(filter)
	return err == nil && ip.String() == actual
}
