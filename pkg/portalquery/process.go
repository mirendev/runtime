package query

import (
	"context"
	"errors"
	"os"
	"os/user"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// CollectEvents is the built-in EventSource. It accepts an already validated
// event selection; use Engine.Monitor for validation, filtering and timestamps.
func CollectEvents(ctx context.Context, request MonitorRequest, emit func(Event) error) error {
	if request.Source == "syscalls" || request.Source == "disk" || request.Source == "tracepoint" {
		selection := request
		selection.EventFilters = nil // enrichment must precede string matching
		filtered := emit
		original := request
		emit = enrichEventNames(func(event Event) error {
			if original.Matches(event) {
				return filtered(event)
			}
			return nil
		})
		request = selection
	}
	switch request.Source {
	case "process":
		return processEvents(ctx, request, emit)
	case "packets":
		return packetEvents(ctx, request, emit)
	case "disk":
		return diskEvents(ctx, request, emit)
	case "tracepoint":
		return tracepointEvents(ctx, request, emit)
	default:
		return syscallEvents(ctx, request, emit)
	}
}

type processState struct {
	started int64 // milliseconds since epoch; distinguishes PID reuse
	name    string
}

// snapshotProcesses uses gopsutil's platform implementations rather than
// assuming /proc exists. Inaccessible/disappearing processes are skipped.
func snapshotProcesses(ctx context.Context) (map[uint32]processState, error) {
	all, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	var currentUser string
	restricted := os.Geteuid() != 0
	if restricted {
		account, err := user.Current()
		if err != nil {
			return nil, err
		}
		currentUser = account.Username
		if currentUser == "" {
			return nil, errors.New("cannot determine server account for process monitoring")
		}
	}
	result := make(map[uint32]processState, len(all))
	for _, p := range all {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if p.Pid <= 0 {
			continue
		}
		if restricted {
			uids, err := p.UidsWithContext(ctx)
			if err == nil && len(uids) != 0 {
				uid := uids[0] // macOS reports the effective UID only.
				if len(uids) > 1 {
					uid = uids[1] // Linux reports real UID, then effective UID.
				}
				if uid != uint32(os.Geteuid()) {
					continue
				}
			} else {
				// Windows has no Unix UID; compare account identities instead.
				owner, err := p.UsernameWithContext(ctx)
				if err != nil || !strings.EqualFold(owner, currentUser) {
					continue
				}
			}
		}
		started, err := p.CreateTimeWithContext(ctx)
		if err != nil || started <= 0 {
			continue
		}
		name, err := p.NameWithContext(ctx)
		if err != nil || name == "" {
			continue
		}
		result[uint32(p.Pid)] = processState{started: started, name: name}
	}
	return result, nil
}

func processSnapshot(ctx context.Context, request MonitorRequest) (Snapshot, error) {
	current, err := snapshotProcesses(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Source: "process", Time: time.Now().UTC(), Processes: make([]ProcessInfo, 0)}
	for pid, state := range current {
		if request.PID != 0 && request.PID != pid {
			continue
		}
		if request.Process != nil && !processNameMatches(request.Process.Name, state.name) {
			continue
		}
		if ctx.Err() != nil {
			return Snapshot{}, ctx.Err()
		}
		p, err := process.NewProcessWithContext(ctx, int32(pid))
		if err != nil {
			continue
		}
		info := ProcessInfo{
			PID: pid, Name: state.name, Started: time.UnixMilli(state.started).UTC(),
		}
		// Metrics are best-effort: unsupported or inaccessible values are
		// omitted rather than hiding the process or fabricating zeros.
		if times, err := p.TimesWithContext(ctx); err == nil {
			seconds := times.User + times.System
			info.CPUSeconds = &seconds
		}
		if memory, err := p.MemoryInfoWithContext(ctx); err == nil {
			info.RSSBytes, info.VMSBytes = &memory.RSS, &memory.VMS
		}
		info.User, _ = p.UsernameWithContext(ctx)
		if states, err := p.StatusWithContext(ctx); err == nil {
			info.State = strings.Join(states, ",")
		}
		if threads, err := p.NumThreadsWithContext(ctx); err == nil {
			info.Threads = &threads
		}
		info.CommandLine, _ = p.CmdlineWithContext(ctx)
		// Use a fresh handle to avoid a cached start time. If the PID was
		// reused while collecting metrics, discard the mixed observation.
		check, err := process.NewProcessWithContext(ctx, int32(pid))
		if err != nil {
			continue
		}
		started, err := check.CreateTimeWithContext(ctx)
		if err != nil || started != state.started {
			continue
		}
		snapshot.Processes = append(snapshot.Processes, info)
	}
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	sort.Slice(snapshot.Processes, func(i, j int) bool { return snapshot.Processes[i].PID < snapshot.Processes[j].PID })
	return snapshot, nil
}

func processTransitions(before, after map[uint32]processState) []Event {
	var events []Event
	now := time.Now().UTC()
	for pid, old := range before {
		if next, ok := after[pid]; !ok || next.started != old.started {
			events = append(events, Event{Time: now, PID: pid, Process: &ProcessEvent{Name: old.name, Action: "exit"}})
		}
	}
	for pid, next := range after {
		if old, ok := before[pid]; !ok || old.started != next.started {
			events = append(events, Event{Time: now, PID: pid, Process: &ProcessEvent{Name: next.name, Action: "start"}})
		}
	}
	return events
}

func processEvents(ctx context.Context, request MonitorRequest, emit func(Event) error) error {
	previous, err := snapshotProcesses(ctx) // Baseline: existing processes are not start events.
	if err != nil {
		return err
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			current, err := snapshotProcesses(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			for _, event := range processTransitions(previous, current) {
				if request.Matches(event) {
					if err := emit(event); err != nil {
						return err
					}
				}
			}
			previous = current
		}
	}
}
