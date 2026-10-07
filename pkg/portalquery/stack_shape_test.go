package query

import (
	"context"
	"reflect"
	"testing"
	"testing/synctest"
)

func TestStackShapeKeys(t *testing.T) {
	stack := CapturedStack{Frames: []SymbolFrame{
		{Module: "app", Name: "fsync", Offset: 0x23},
		{Module: "app", Name: "logstorage.merge", Offset: 0x37},
		{Address: "0x987"},
		{Module: "app", Name: "runtime.start", Offset: 2},
	}}
	before := append([]SymbolFrame{}, stack.Frames...)
	for _, tc := range []struct {
		shape *StackShape
		want  string
	}{
		{nil, "app:fsync+0x23;app:logstorage.merge+0x37;0x987;app:runtime.start+0x2"},
		{&StackShape{DropOffsets: true}, "app:fsync;app:logstorage.merge;0x987;app:runtime.start"},
		{&StackShape{DropBottom: 1, Top: 2}, "app:fsync+0x23;app:logstorage.merge+0x37"},
		{&StackShape{Until: "logstorage.*", DropOffsets: true}, "app:fsync;app:logstorage.merge"},
		{&StackShape{Until: "*start", DropBottom: 1}, "app:fsync+0x23;app:logstorage.merge+0x37;0x987"},
		{&StackShape{Until: "missing", Top: 1}, "app:fsync+0x23"},
		{&StackShape{DropBottom: 64}, ""},
		{&StackShape{From: "logstorage.*", DropOffsets: true}, "app:logstorage.merge;0x987;app:runtime.start"},
		{&StackShape{DropTop: 1, From: "logstorage.*", Until: "*start", Top: 2}, "app:logstorage.merge+0x37;0x987"},
		{&StackShape{From: "missing", DropTop: 2}, "0x987;app:runtime.start+0x2"},
		{&StackShape{DropTop: 64}, ""},
	} {
		if got := stack.key(tc.shape); got != tc.want {
			t.Fatalf("%+v: %q, want %q", tc.shape, got, tc.want)
		}
	}
	if !reflect.DeepEqual(stack.Frames, before) {
		t.Fatal("shaping changed raw frames")
	}
	stack.Error = "capture failed;\n-17%"
	if got := stack.key(&StackShape{DropBottom: 64}); got != "[capture failed%3B%0A-17%25]" {
		t.Fatalf("error lost or injected a frame: %q", got)
	}
	if (CapturedStack{Frames: []SymbolFrame{{Name: "a;b", Module: "m"}}}).key(nil) == (CapturedStack{Frames: []SymbolFrame{{Name: "a%3Bb", Module: "m"}}}).key(nil) {
		t.Fatal("escaping collided")
	}
	offset := uint64(0x823)
	fileStack := CapturedStack{Frames: []SymbolFrame{{Module: "/usr/bin/qemu", Address: "0x100823", FileOffset: &offset}}}
	if got := fileStack.key(&StackShape{DropOffsets: true}); got != "/usr/bin/qemu@file+0x823" {
		t.Fatalf("stripped frame identity erased: %s", got)
	}
}

func TestStackShapeQueriesAndProof(t *testing.T) {
	for _, text := range []string{
		"syscalls where stacks = user and user.stack.offsets = false and user.stack.until = logstorage.* and user.stack.top = 8 count over 1s by user.stack",
		"syscalls where kernel.stack.drop_bottom = 3 and stack.depth = 64 and stacks = both count over 1s by kernel.stack",
		"syscalls where stacks = both and kernel.stack.from = submit_bio* and user.stack.drop_top = 5 count over 1s by kernel.stack",
		`syscalls where stacks = user and user.stack.from = "os.(*File).Sync" count over 1s by user.stack`,
		`syscalls where stacks = user and user.stack.from in ("os.(*File).Sync", '*Fdatasync') count over 1s by user.stack`,
	} {
		r, err := ParseMonitorQuery(text)
		if err != nil {
			t.Fatalf("%s: %v", text, err)
		}
		if r.Stacks.UserShape == nil && r.Stacks.KernelShape == nil {
			t.Fatal("lost shape filters")
		}
	}
	for _, text := range []string{
		"syscalls where stacks = user and user.stack.offsets = false",
		"syscalls where stacks = user and kernel.stack.top = 1 count over 1s",
		"syscalls where user.stack.top = 1 count over 1s",
		"syscalls where stacks = both and user.stack.top = -1 count over 1s",
		"syscalls where stacks = both and user.stack.top = 65 count over 1s",
		"syscalls where stacks = both and kernel.stack.drop_bottom = nope count over 1s",
		"syscalls where stacks = both and user.stack.until = '*' count over 1s",
		"syscalls where stacks = both and user.stack.until = '*foo*' count over 1s",
		"syscalls where stacks = both and user.stack.from in (ok, '') count over 1s",
		"syscalls where stacks = both and user.stack.drop_top = 65 count over 1s",
		"syscalls where stacks = both and user.stack.until = '' count over 1s",
		"syscalls where stacks = both and user.stack.offsets = nope count over 1s",
		"syscalls where stacks = both and kernel.stack.bogus = 2 count over 1s",
	} {
		if _, err := ParseMonitorQuery(text); err == nil {
			t.Fatalf("invalid shape accepted: %s", text)
		}
	}
	if err := (StackCapture{User: true, UserShape: &StackShape{Until: "fsync"}}).Validate(); err == nil {
		t.Fatal("until accepted without symbols")
	}
}

func TestStackShapesMergeBeforeSharedReductions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, err := ParseMonitorQuery("syscalls where phase = completion and stacks = both and user.stack.offsets = false and kernel.stack.top = 1 count, sum(duration_ns) over 1s by user.stack, kernel.stack")
		if err != nil {
			t.Fatal(err)
		}
		got, err := aggregateEvents(context.Background(), r, func(ctx context.Context, _ MonitorRequest, emit func(Event) error) error {
			for i, duration := range []uint64{7, 11} {
				user := &CapturedStack{Frames: []SymbolFrame{{Module: "app", Name: "fsync", Offset: uint64(0x23 + i)}}}
				kernel := &CapturedStack{Frames: []SymbolFrame{{Name: "vfs_fsync", Offset: 1}, {Name: "caller", Offset: uint64(i)}}}
				if err := emit(Event{Phase: "completion", DurationNS: &duration, UserStack: user, KernelStack: kernel}); err != nil {
					return err
				}
				if user.Frames[0].Offset != uint64(0x23+i) || len(kernel.Frames) != 2 {
					t.Fatal("raw event mutated")
				}
			}
			<-ctx.Done()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		metrics := got.Aggregation.Metrics
		if len(metrics[0].Counts) != 1 || metrics[0].Counts[0].Count != 2 || string(metrics[1].Values[0].Value) != "18" {
			t.Fatalf("shape applied after reduction: %+v", metrics)
		}
		if string(metrics[0].Counts[0].Group["user.stack"]) != `"app:fsync"` || string(metrics[0].Counts[0].Group["kernel.stack"]) != `":vfs_fsync+0x1"` {
			t.Fatalf("user/kernel shapes mixed: %+v", metrics[0].Counts)
		}
	})
}
