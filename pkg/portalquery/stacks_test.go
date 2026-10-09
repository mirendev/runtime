package query

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSymbolAndStackQueries(t *testing.T) {
	r, err := ParseMonitorQuery("symbols where target = process and pid = 123 and addresses in (0xffffffffffffffff, 42)")
	if err != nil || r.Mode != "snapshot" || r.Symbols.PID != 123 || r.Symbols.Addresses[0] != ^uint64(0) || r.Symbols.Addresses[1] != 42 {
		t.Fatalf("address query: %+v, %v", r, err)
	}
	r, err = ParseMonitorQuery("symbols where target = process and pid = 123 and name = handle* and limit = 17")
	if err != nil || r.Mode != "snapshot" || r.Symbols.PID != 123 || r.Symbols.Name != "handle*" || r.Symbols.Limit != 17 {
		t.Fatalf("process name query: %+v, %v", r, err)
	}
	r, err = ParseMonitorQuery("syscalls where stacks = both count over 1s by pid, user.stack, kernel.stack")
	if err != nil || !r.Stacks.User || !r.Stacks.Kernel || !r.Stacks.Symbolize {
		t.Fatalf("stack query: %+v, %v", r, err)
	}
	for _, query := range []string{
		"process where stacks = user", "syscalls count over 1s by user.stack",
		"syscalls where stacks = kernel count over 1s by user.stack",
		"symbols where target = kernel and addresses in (-1)",
		"symbols where target = kernel and name = vfs_* and addresses in (42)",
		"symbols where target = kernel and name = vfs_* count over 1s",
		"symbols where target = process and name = handle*",
		"symbols where target = process and pid = 123 and name = handle* and addresses in (42)",
	} {
		if _, err := ParseMonitorQuery(query); err == nil {
			t.Fatalf("accepted invalid query: %s", query)
		}
	}
	frames := &CapturedStack{Frames: []SymbolFrame{{Address: "0x123", Name: "run", Module: "app", Offset: 7}, {Address: "0x456", Error: "unresolved"}}}
	event := Event{PID: 123, UserStack: frames}
	if got := eventGroupFields(event, nil)["user.stack"]; got != "app:run+0x7;0x456" {
		t.Fatalf("wrong collapsed stack: %v", got)
	}
	data, err := json.Marshal(event)
	if err != nil || !strings.Contains(string(data), `"user_stack"`) || !strings.Contains(string(data), `"syscall":0`) {
		t.Fatalf("lost syscall stack: %s, %v", data, err)
	}
}

func TestStackCoverageDistinguishesCaptureAndSymbolization(t *testing.T) {
	var coverage StackCoverage
	for _, stack := range []*CapturedStack{
		nil, {},
		{CaptureErrorCode: -14, Error: "bpf_get_stackid failed: -14"},
		{CaptureErrorCode: -14, Error: "bpf_get_stackid failed: -14"},
		{CaptureErrorCode: -17, Error: "bpf_get_stackid failed: -17"},
		{Error: "read captured stack: missing key"},
		{Frames: []SymbolFrame{{Name: "a"}, {Name: "b"}}, DepthLimitReached: true},
		{Frames: []SymbolFrame{{Name: "a"}, {Address: "0x12", Error: "no symbol"}}},
		{Frames: []SymbolFrame{{Address: "0x1"}, {Address: "0x2"}, {Address: "0x3"}}, Error: "process exited before symbolization"},
		{Frames: []SymbolFrame{{Address: "0x4", Module: "libc.so", FileOffset: new(uint64)}}},
	} {
		coverage.observe(stack, true)
	}
	want := StackCoverage{Events: 10, Missing: 1, Empty: 1, CaptureFailures: 4, LookupFailures: 1,
		HelperErrors: map[string]uint64{"-14": 2, "-17": 1}, Captured: 4, DepthLimitReached: 1,
		Frames: 8, NamedFrames: 3, UnresolvedFrames: 5, FullySymbolized: 1, PartiallySymbolized: 1, Unsymbolized: 2, SymbolizationFailures: 1}
	if !reflect.DeepEqual(coverage, want) {
		t.Fatalf("capture failures must not include successfully captured unnamed frames: %+v", coverage)
	}
	var disabled StackCoverage
	disabled.observe(&CapturedStack{Frames: []SymbolFrame{{Address: "0x1"}, {Address: "0x2"}}}, false)
	if disabled.Captured != 1 || disabled.Frames != 2 || disabled.SymbolizationDisabled != 1 || disabled.UnresolvedFrames != 0 || disabled.Unsymbolized != 0 {
		t.Fatalf("disabled symbolization reported as failed resolution: %+v", disabled)
	}
}
