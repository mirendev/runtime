//go:build linux

package query

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
)

const blockIssueFormat = `name: block_rq_issue
format:
	field:unsigned short common_type; offset:0; size:2; signed:0;
	field:dev_t dev; offset:8; size:4; signed:0;
	field:sector_t sector; offset:16; size:8; signed:0;
	field:unsigned int nr_sector; offset:24; size:4; signed:0;
	field:char rwbs[8]; offset:28; size:8; signed:0;
`

func TestDiskTracepointFormatAndRecord(t *testing.T) {
	fields, err := parseDiskFormat(blockIssueFormat)
	if err != nil {
		t.Fatal(err)
	}
	if fields["rwbs"].offset != 28 || fields["sector"].offset != 16 {
		t.Fatalf("wrong tracepoint offsets: %+v", fields)
	}
	insns := diskInstructions(fields, 42, &DiskFilter{Device: 17, Operation: "write"})
	if err := insns.Marshal(&bytes.Buffer{}, binary.LittleEndian); err != nil {
		t.Fatalf("invalid eBPF instructions: %v", err)
	}
	if len(insns) < 10 || insns[1].OpCode != asm.LoadMem(asm.R7, asm.R6, 8, asm.Word).OpCode || insns[1].Offset != 8 {
		t.Fatal("device offset not loaded from format")
	}
	raw := make([]byte, taskIdentitySize+48)
	binary.NativeEndian.PutUint64(raw[:8], uint64(123)<<32|456)
	copy(raw[8:24], "disk-worker")
	payload := raw[taskIdentitySize:]
	binary.NativeEndian.PutUint32(payload[:4], 17)
	binary.NativeEndian.PutUint32(payload[4:8], 8)
	binary.NativeEndian.PutUint64(payload[8:16], 12345)
	copy(payload[16:48], "WSMF")
	event, err := decodeDiskRecord(raw)
	if err != nil || event.PID != 123 || event.TID != 456 || event.Name != "disk-worker" || event.Disk.Device != 17 || event.Disk.Sector != 12345 || event.Disk.Sectors != 8 || event.Disk.Operation != "write" || event.Disk.RWBS != "WSMF" || event.Time.IsZero() {
		t.Fatalf("decoded %+v, %v", event, err)
	}
	if !(&MonitorRequest{Source: "disk", Disk: &DiskFilter{Device: 17, Operation: "write"}}).Matches(event) {
		t.Fatal("matching disk request rejected")
	}
	for _, request := range []MonitorRequest{
		{Source: "disk", Disk: &DiskFilter{Device: 18}},
		{Source: "disk", Disk: &DiskFilter{Operation: "read"}},
		{Source: "syscalls"},
	} {
		if request.Matches(event) {
			t.Fatalf("unrelated filter matched disk event: %+v", request)
		}
	}
	for _, request := range []MonitorRequest{
		{Source: "disk", PID: 1},
		{Source: "disk", Packet: &PacketFilter{}},
		{Source: "disk", Disk: &DiskFilter{Operation: "unknown"}},
	} {
		if err := request.Validate(); err == nil {
			t.Fatalf("invalid disk request accepted: %+v", request)
		}
	}
	if _, err := decodeDiskRecord(raw[:23]); err == nil {
		t.Fatal("truncated record accepted")
	}
	for _, bad := range []string{
		strings.Replace(blockIssueFormat, "nr_sector;", "wrong;", 1),
		strings.Replace(blockIssueFormat, "rwbs[8]", "rwbs[0]", 1),
		strings.Replace(blockIssueFormat, "sector; offset:16; size:8", "sector; offset:16; size:4", 1),
	} {
		if _, err := parseDiskFormat(bad); err == nil {
			t.Fatalf("invalid format accepted: %s", bad)
		}
	}
}

func TestDiskDeviceNameCacheIsBounded(t *testing.T) {
	c := newDiskDeviceNames()
	for i := uint32(0); i < diskDeviceCacheMax+20; i++ {
		c.lookup(i)
	}
	if len(c.names) != diskDeviceCacheMax {
		t.Fatalf("device cache grew to %d", len(c.names))
	}
}

func TestDiskPreflushOperationAndPrograms(t *testing.T) {
	for _, tc := range []struct{ rwbs, operation string }{
		{"FWFSM", "write"}, {"FSM", "flush"}, {"FF", "flush"}, {"FRA", "read"}, {"DE", "discard"},
	} {
		raw := make([]byte, 72)
		copy(raw[40:], tc.rwbs)
		e, err := decodeDiskRecord(raw)
		if err != nil || e.Disk.Operation != tc.operation || e.Disk.RWBS != tc.rwbs {
			t.Fatalf("rwbs=%s: %+v, %v", tc.rwbs, e.Disk, err)
		}
	}
	raw := make([]byte, 72)
	copy(raw[40:], "WS\x00M\x00F")
	e, err := decodeDiskRecord(raw)
	if err != nil || e.Disk.RWBS != "WS" || e.Disk.Operation != "write" {
		t.Fatalf("bytes after C terminator treated as flags: %+v, %v", e.Disk, err)
	}
	if os.Geteuid() != 0 {
		return // decoder checks above run unprivileged too
	}
	fields, err := readDiskFormat()
	if err != nil {
		t.Fatal(err)
	}
	events, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.RingBuf, MaxEntries: 4096})
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	for _, operation := range []string{"", "write", "flush"} {
		p, err := ebpf.NewProgram(&ebpf.ProgramSpec{Type: ebpf.TracePoint, License: "GPL", Instructions: diskInstructions(fields, events.FD(), &DiskFilter{Operation: operation})})
		if err != nil {
			t.Fatalf("load operation=%s: %v", operation, err)
		}
		p.Close()
	}
}
