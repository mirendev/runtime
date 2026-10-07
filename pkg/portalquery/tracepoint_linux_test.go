//go:build linux

package query

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"golang.org/x/sys/unix"
)

const schedWakeupFormat = `name: sched_wakeup
format:
	field:unsigned short common_type; offset:0; size:2; signed:0;
	field:unsigned char common_flags; offset:2; size:1; signed:0;
	field:unsigned char common_preempt_count; offset:3; size:1; signed:0;
	field:int common_pid; offset:4; size:4; signed:1;
	field:char comm[16]; offset:8; size:16; signed:0;
	field:pid_t pid; offset:24; size:4; signed:1;
	field:int prio; offset:28; size:4; signed:1;
	field:int target_cpu; offset:32; size:4; signed:1;
	field:unsigned long long timestamp; offset:40; size:8; signed:0;
	field:__data_loc char[] filename; offset:48; size:4; signed:1;
	field:void *ptr; offset:56; size:8; signed:0;
`

func TestProbeTracepointFieldInferenceLive(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root tracepoints")
	}
	for _, query := range []string{
		`tracepoint:sched:sched_switch { @switches[] = count() } after 400ms { emit @switches }`,
		`tracepoint:sched:sched_switch { @switches[previous: field.prev_pid] = count() } after 400ms { emit @switches order by value asc limit 2 }`,
	} {
		r, err := ParseMonitorQuery(query)
		if err != nil {
			t.Fatal(err)
		}
		result, err := aggregateEvents(context.Background(), r, tracepointEvents)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Aggregation.Rows) == 0 || string(result.Aggregation.Rows[0].Values[0]) == "0" {
			t.Fatalf("no live tracepoint events: %+v", result)
		}
		if len(r.Tracepoint.Fields) == 0 {
			if len(result.Aggregation.Rows[0].Group) != 0 {
				t.Fatal("count-only probe captured unexpected fields")
			}
		} else if _, ok := result.Aggregation.Rows[0].Group["previous"]; !ok {
			t.Fatal("missing inferred aliased field")
		}
		t.Logf("fields=%v groups=%d first_count=%s", r.Tracepoint.Fields, len(result.Aggregation.Rows), result.Aggregation.Rows[0].Values[0])
	}
}

func TestProbeScriptSourcesLive(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root tracepoints")
	}
	r, err := ParseMonitorQuery(`tracepoint:sched:sched_switch { @switches[] = count() }
tracepoint:sched:sched_wakeup { @wakeups[] = count() }
memory { @memory[] = avg(used) }
after 1200ms { emit @switches; emit @wakeups; emit @memory }`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := aggregateScript(context.Background(), r, CollectEvents, querySnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tables) != 3 {
		t.Fatalf("missing live sources: %+v", result)
	}
	first := result.Tables[0].Aggregation
	for _, table := range result.Tables {
		a := table.Aggregation
		if a == nil || !a.Start.Equal(first.Start) || !a.End.Equal(first.End) || len(a.Rows) != 1 {
			t.Fatalf("incorrect live table/timing: %+v", table)
		}
		value, err := strconv.ParseFloat(string(a.Rows[0].Values[0]), 64)
		if err != nil || value <= 0 {
			t.Fatalf("no live data for @%s: %+v", a.Table, a)
		}
		t.Logf("source=%s table=%s value=%s start=%s end=%s", table.Source, a.Table, a.Rows[0].Values[0], a.Start, a.End)
	}
}

func TestGenericTracepointFormatAndRecord(t *testing.T) {
	spec := TracepointFilter{Event: "sched:sched_wakeup", Fields: []string{"target_cpu", "pid", "timestamp", "prio"}, Equals: map[string]string{"target_cpu": "3", "prio": "-2"}}
	fields, err := parseTracepointFormat(schedWakeupFormat, spec.Fields)
	if err != nil {
		t.Fatal(err)
	}
	if fields[0].offset != 32 || fields[1].offset != 24 || fields[2].offset != 40 || !fields[3].signed {
		t.Fatalf("fields not ordered by request: %+v", fields)
	}
	insns := tracepointInstructions(fields, 42, nil)
	if err := insns.Marshal(&bytes.Buffer{}, binary.LittleEndian); err != nil {
		t.Fatalf("invalid eBPF instructions: %v", err)
	}
	for i, field := range fields {
		start := 10 + i*9
		if insns[start] != asm.Mov.Imm(asm.R7, 0) || insns[start+1] != asm.StoreMem(asm.RFP, int16(-8*(len(fields)-i)), asm.R7, asm.DWord) ||
			insns[start+4] != asm.Mov.Imm(asm.R2, int32(field.size)) ||
			insns[start+6] != asm.Add.Imm(asm.R3, int32(field.offset)) ||
			insns[start+7] != asm.FnProbeReadKernel.Call() {
			t.Fatalf("wrong eBPF field read for %+v: %v", field, insns)
		}
	}
	raw := make([]byte, 32)
	binary.NativeEndian.PutUint64(raw[0:8], 3)
	binary.NativeEndian.PutUint64(raw[8:16], 4312)
	binary.NativeEndian.PutUint64(raw[16:24], ^uint64(0))
	binary.NativeEndian.PutUint64(raw[24:32], 0xfffffffe)
	event, err := decodeTracepointRecord(raw, spec, fields)
	if err != nil || event.Tracepoint == nil || event.Tracepoint.Fields["prio"] != "-2" || event.Tracepoint.Fields["timestamp"] != "18446744073709551615" || event.Time.IsZero() {
		t.Fatalf("decoded %+v: %v", event, err)
	}
	request := MonitorRequest{Source: "tracepoint", Tracepoint: &spec}
	if err := request.Validate(); err != nil || !request.Matches(event) || (MonitorRequest{Source: "syscalls"}).Matches(event) {
		t.Fatalf("invalid match or request: %v", err)
	}
	encoded, err := json.Marshal(event)
	var decoded Event
	if err == nil {
		err = json.Unmarshal(encoded, &decoded)
	}
	if err != nil || decoded.Tracepoint.Fields["timestamp"] != "18446744073709551615" || strings.Contains(string(encoded), `"syscall"`) {
		t.Fatalf("lost precision or emitted unrelated field: %s: %v", encoded, err)
	}
	for _, changed := range []Event{
		{Tracepoint: &TracepointEvent{Event: "sched:sched_switch", Fields: event.Tracepoint.Fields}},
		{Tracepoint: &TracepointEvent{Event: spec.Event, Fields: map[string]json.Number{"target_cpu": "4", "prio": "-2"}}},
		{Tracepoint: &TracepointEvent{Event: spec.Event, Fields: map[string]json.Number{"target_cpu": "3"}}},
	} {
		if request.Matches(changed) {
			t.Fatalf("accepted mismatched tracepoint event: %+v", changed)
		}
	}
	if _, err := decodeTracepointRecord(raw[:31], spec, fields); err == nil {
		t.Fatal("accepted truncated ringbuf record")
	}
}

func TestGenericTracepointKernelCommonFields(t *testing.T) {
	fields, err := parseTracepointFormat(schedWakeupFormat, []string{"pid", "common_pid", "timestamp"})
	if err != nil {
		t.Fatal(err)
	}
	events, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.RingBuf, MaxEntries: 1 << 16})
	if err != nil {
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
			t.Skipf("kernel eBPF loading unavailable: %v", err)
		}
		t.Fatal(err)
	}
	defer events.Close()
	program, err := ebpf.NewProgram(&ebpf.ProgramSpec{Type: ebpf.TracePoint, License: "GPL", Instructions: tracepointInstructions(fields, events.FD(), nil)})
	if err != nil {
		t.Fatalf("load tracepoint with common_pid at offset 4: %+v", err)
	}
	program.Close()
}

func TestGenericTracepointCommonPIDHelper(t *testing.T) {
	fields, err := parseTracepointFormat(schedWakeupFormat, []string{"pid", "common_pid", "prio"})
	if err != nil {
		t.Fatal(err)
	}
	if !fields[1].commonPID || fields[0].commonPID || fields[2].commonPID {
		t.Fatalf("incorrect current-task field selection: %+v", fields)
	}
	insns := tracepointInstructions(fields, 42, nil)
	if insns[19] != asm.FnGetCurrentPidTgid.Call() || insns[20] != asm.Mov.Reg32(asm.R7, asm.R0) || insns[21] != asm.StoreMem(asm.RFP, -16, asm.R7, asm.DWord) {
		t.Fatalf("common_pid must store the helper's low 32-bit TID in its selected slot: %v", insns)
	}
	if err := insns.Marshal(&bytes.Buffer{}, binary.LittleEndian); err != nil {
		t.Fatal(err)
	}
}

func TestGenericTracepointKernelCommonPIDValues(t *testing.T) {
	if _, err := readTracepointFormat("sched", "sched_switch", []string{"common_pid", "prev_pid"}); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			t.Skipf("tracefs unavailable: %v", err)
		}
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	complete := errors.New("captured distinct scheduling tasks")
	done := make(chan error, 1)
	go func() {
		seen := make(map[json.Number]bool)
		done <- tracepointEvents(ctx, MonitorRequest{Source: "tracepoint", Tracepoint: &TracepointFilter{
			Event: "sched:sched_switch", Fields: []string{"common_pid", "prev_pid"},
		}}, func(event Event) error {
			fields := event.Tracepoint.Fields
			if fields["common_pid"] != fields["prev_pid"] {
				return fmt.Errorf("common_pid %s != outgoing task prev_pid %s", fields["common_pid"], fields["prev_pid"])
			}
			if fields["common_pid"] != json.Number(strconv.FormatUint(uint64(event.TID), 10)) {
				return fmt.Errorf("common_pid %s != identity TID %d", fields["common_pid"], event.TID)
			}
			if fields["prev_pid"] != "0" && (event.PID == 0 || event.Name == "") {
				return fmt.Errorf("missing task identity: pid=%d name=%q", event.PID, event.Name)
			}
			if fields["prev_pid"] != "0" {
				seen[fields["prev_pid"]] = true
			}
			if len(seen) >= 2 {
				return complete
			}
			return nil
		})
	}()
	// Force scheduling activity on multiple Go threads while capturing.
	for {
		select {
		case err := <-done:
			if !errors.Is(err, complete) {
				var verifier *ebpf.VerifierError
				if !errors.As(err, &verifier) && (errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES)) {
					t.Skipf("kernel tracepoint capture unavailable: %v", err)
				}
				t.Fatalf("capture common_pid values: %v", err)
			}
			return
		default:
			runtime.Gosched()
			time.Sleep(time.Millisecond)
		}
	}
}

func TestGenericTracepointFieldBounds(t *testing.T) {
	for _, tc := range []struct {
		offset, size string
		valid        bool
	}{
		{"4092", "4", true},
		{"4093", "4", false},
		{"32768", "4", false},
		{"9223372036854775806", "4", false},
		{"24", "2147483648", false},
		{"24", "9223372036854775807", false},
	} {
		t.Run(tc.offset+"/"+tc.size, func(t *testing.T) {
			format := fmt.Sprintf("field:pid_t pid; offset:%s; size:%s; signed:1;", tc.offset, tc.size)
			fields, err := parseTracepointFormat(format, []string{"pid"})
			if (err == nil) != tc.valid {
				t.Fatalf("fields=%+v error=%v; valid=%v", fields, err, tc.valid)
			}
			if tc.valid && (fields[0].offset != 4092 || fields[0].size != 4) {
				t.Fatalf("unexpected boundary field: %+v", fields[0])
			}
		})
	}
}

func TestGenericTracepointRejectsUnsupportedFields(t *testing.T) {
	for _, name := range []string{"comm", "filename", "ptr", "missing", "common_type", "common_flags", "common_preempt_count"} {
		if _, err := parseTracepointFormat(schedWakeupFormat, []string{name}); err == nil {
			t.Fatalf("accepted unsupported or missing field %s", name)
		}
	}
	for _, bad := range []string{
		strings.Replace(schedWakeupFormat, "common_pid; offset:4; size:4; signed:1", "common_pid; offset:8; size:4; signed:1", 1),
		strings.Replace(schedWakeupFormat, "common_pid; offset:4; size:4; signed:1", "common_pid; offset:4; size:8; signed:1", 1),
		strings.Replace(schedWakeupFormat, "common_pid; offset:4; size:4; signed:1", "common_pid; offset:4; size:4; signed:0", 1),
	} {
		if _, err := parseTracepointFormat(bad, []string{"common_pid"}); err == nil {
			t.Fatal("accepted invalid common_pid format")
		}
	}
	for _, bad := range []string{
		strings.Replace(schedWakeupFormat, "offset:24;", "offset:4094;", 1),
		strings.Replace(schedWakeupFormat, "size:4; signed:1;\n\tfield:int prio", "size:3; signed:1;\n\tfield:int prio", 1),
		strings.Replace(schedWakeupFormat, "field:pid_t pid; offset:24", "field:pid_t pid; offset:24; size:4; signed:1;\n\tfield:pid_t pid; offset:24", 1),
	} {
		if _, err := parseTracepointFormat(bad, []string{"pid"}); err == nil {
			t.Fatalf("accepted invalid format: %s", bad)
		}
	}
}
