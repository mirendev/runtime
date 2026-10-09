//go:build linux

package query

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

type diskField struct {
	offset int16
	size   int
}

var traceField = regexp.MustCompile(`field:[^;]*\b(dev|sector|nr_sector|rwbs)(?:\[(\d+)\])?;\s*offset:(\d+);\s*size:(\d+);`)

// Tracepoint context offsets are kernel-specific; reject formats we cannot read.
func parseDiskFormat(format string) (map[string]diskField, error) {
	fields := make(map[string]diskField)
	for _, line := range strings.Split(format, "\n") {
		match := traceField.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		offset, err := strconv.Atoi(match[3])
		if err != nil || offset < 0 || offset > 4095 {
			return nil, fmt.Errorf("invalid %s offset", match[1])
		}
		size, err := strconv.Atoi(match[4])
		if err != nil || (match[1] == "sector" && size != 8) ||
			((match[1] == "dev" || match[1] == "nr_sector") && size != 4) ||
			(match[1] == "rwbs" && (size < 1 || size > 32 || match[2] != strconv.Itoa(size))) {
			return nil, fmt.Errorf("unsupported %s size", match[1])
		}
		if _, exists := fields[match[1]]; exists {
			return nil, fmt.Errorf("duplicate %s field", match[1])
		}
		fields[match[1]] = diskField{offset: int16(offset), size: size}
	}
	for _, name := range []string{"dev", "sector", "nr_sector", "rwbs"} {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("block_rq_issue lacks %s field", name)
		}
	}
	return fields, nil
}

func readDiskFormat() (map[string]diskField, error) {
	var err error
	for _, path := range []string{
		"/sys/kernel/tracing/events/block/block_rq_issue/format",
		"/sys/kernel/debug/tracing/events/block/block_rq_issue/format",
	} {
		var data []byte
		data, err = os.ReadFile(path)
		if err == nil {
			return parseDiskFormat(string(data))
		}
	}
	return nil, fmt.Errorf("read block_rq_issue tracepoint format: %w", err)
}

func diskInstructions(fields map[string]diskField, eventsFD int, filter *DiskFilter, collections ...*collectionState) asm.Instructions {
	var collection *collectionState
	if len(collections) != 0 {
		collection = collections[0]
	}
	insns := asm.Instructions{asm.Mov.Reg(asm.R6, asm.R1),
		asm.LoadMem(asm.R7, asm.R6, fields["dev"].offset, asm.Word),
	}
	if filter != nil && filter.Device != 0 {
		insns = append(insns, asm.JNE.Imm32(asm.R7, int32(filter.Device), "exit"))
	}
	insns = append(insns, asm.LoadMem(asm.R8, asm.R6, fields["rwbs"].offset, asm.Byte))
	if filter != nil && filter.Operation != "" {
		// A leading F is PREFLUSH when followed by an operation byte.
		if fields["rwbs"].size > 1 {
			insns = append(insns, asm.JNE.Imm(asm.R8, 'F', "operation_ready"), asm.LoadMem(asm.R0, asm.R6, fields["rwbs"].offset+1, asm.Byte))
			for _, op := range []int32{'R', 'W', 'D', 'F', 'N'} {
				insns = append(insns, asm.JEq.Imm(asm.R0, op, "after_preflush"))
			}
			insns = append(insns, asm.Ja.Label("operation_ready"), asm.Mov.Reg(asm.R8, asm.R0).WithSymbol("after_preflush"), asm.Mov.Reg(asm.R8, asm.R8).WithSymbol("operation_ready"))
		}
		ops := map[string]int32{"read": 'R', "write": 'W', "discard": 'D', "flush": 'F'}
		insns = append(insns, asm.JNE.Imm(asm.R8, ops[filter.Operation], "exit"))
	}
	// The record retains the complete tracefs rwbs array (up to the validated
	// 32-byte maximum), rather than just its operation byte.
	insns = appendTaskIdentity(insns, -72)
	insns = append(insns,
		asm.LoadMem(asm.R9, asm.R6, fields["sector"].offset, asm.DWord),
		asm.LoadMem(asm.R0, asm.R6, fields["nr_sector"].offset, asm.Word),
		asm.StoreMem(asm.RFP, -48, asm.R7, asm.Word),
		asm.StoreMem(asm.RFP, -44, asm.R0, asm.Word),
		asm.StoreMem(asm.RFP, -40, asm.R9, asm.DWord),
		asm.Mov.Imm(asm.R0, 0),
		asm.StoreMem(asm.RFP, -32, asm.R0, asm.DWord), asm.StoreMem(asm.RFP, -24, asm.R0, asm.DWord),
		asm.StoreMem(asm.RFP, -16, asm.R0, asm.DWord), asm.StoreMem(asm.RFP, -8, asm.R0, asm.DWord),
	)
	// Direct tracepoint context accesses are verifier-checked constant loads.
	for off := 0; off < fields["rwbs"].size; off++ {
		insns = append(insns, asm.LoadMem(asm.R0, asm.R6, fields["rwbs"].offset+int16(off), asm.Byte), asm.StoreMem(asm.RFP, -32+int16(off), asm.R0, asm.Byte))
	}
	insns = append(insns,
		asm.LoadMapPtr(asm.R1, eventsFD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, -72),
		asm.Mov.Imm(asm.R3, 72),
		asm.Mov.Imm(asm.R4, 0),
		asm.FnRingbufOutput.Call(),
	)
	insns = appendRingLoss(insns, collection)
	insns = append(insns, asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"), asm.Return())
	return insns
}

func decodeDiskRecord(raw []byte) (Event, error) {
	if len(raw) != taskIdentitySize+48 {
		return Event{}, errors.New("invalid eBPF disk record")
	}
	event := Event{Time: time.Now().UTC()}
	decodeTaskIdentity(raw[:taskIdentitySize], &event)
	raw = raw[taskIdentitySize:]
	flags := raw[16:48]
	// Tracefs exposes a C string, not a zero-padded array. Bytes after
	// the first terminator may retain flags from an earlier ring-buffer use.
	if end := bytes.IndexByte(flags, 0); end >= 0 {
		flags = flags[:end]
	}
	rwbs := string(flags)
	opByte := raw[16]
	if len(rwbs) > 1 && rwbs[0] == 'F' && strings.ContainsRune("RWDFN", rune(rwbs[1])) {
		opByte = rwbs[1]
	}
	op := map[byte]string{'R': "read", 'W': "write", 'D': "discard", 'F': "flush"}[opByte]
	if op == "" {
		op = "other"
	}
	event.Disk = &DiskEvent{
		Device: binary.NativeEndian.Uint32(raw[:4]), Sectors: binary.NativeEndian.Uint32(raw[4:8]),
		Sector: binary.NativeEndian.Uint64(raw[8:16]), Operation: op, RWBS: rwbs,
	}
	return event, nil
}

const diskDeviceCacheMax = 256

type diskDeviceNames struct{ names map[uint32]string }

func newDiskDeviceNames() *diskDeviceNames { return &diskDeviceNames{names: make(map[uint32]string)} }

func (d *diskDeviceNames) lookup(device uint32) string {
	if name, ok := d.names[device]; ok {
		return name
	}
	major, minor := device>>20, device&0xfffff
	target, err := filepath.EvalSymlinks(fmt.Sprintf("/sys/dev/block/%d:%d", major, minor))
	name := ""
	if err == nil {
		name = filepath.Base(target)
	}
	if len(d.names) < diskDeviceCacheMax {
		d.names[device] = name
	}
	return name
}

func diskEvents(ctx context.Context, request MonitorRequest, emit func(Event) error) error {
	// BTF exposes the request/bio association and original flags for both
	// phases. Retain the tracefs entry collector on kernels without it.
	l, layoutErr := loadDiskBTFLayout()
	if layoutErr == nil {
		return diskRequestEvents(ctx, request, emit, l)
	}
	if request.Phase == "completion" {
		return layoutErr
	}
	fields, err := readDiskFormat()
	if err != nil {
		return err
	}
	events, err := ebpf.NewMap(&ebpf.MapSpec{Name: "portal_disk", Type: ebpf.RingBuf, MaxEntries: 1 << 16})
	if err != nil {
		return fmt.Errorf("create eBPF disk ring buffer: %w", err)
	}
	defer events.Close()
	collection, err := newCollectionState()
	if err != nil {
		return err
	}
	defer collection.close()
	program, err := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "portal_disk_issue", Type: ebpf.TracePoint, License: "GPL", Instructions: diskInstructions(fields, events.FD(), request.Disk, collection)})
	if err != nil {
		return fmt.Errorf("load eBPF disk monitor: %w", err)
	}
	defer program.Close()
	reader, err := ringbuf.NewReader(events)
	if err != nil {
		return err
	}
	defer reader.Close()
	attached, err := link.Tracepoint("block", "block_rq_issue", program, nil)
	if err != nil {
		return fmt.Errorf("attach eBPF disk monitor: %w", err)
	}
	defer attached.Close()
	stop := context.AfterFunc(ctx, func() { reader.Close() })
	defer stop()
	deviceNames := newDiskDeviceNames()
	for {
		record, err := reader.Read()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, ringbuf.ErrClosed) {
				if err := attached.Close(); err != nil {
					return err
				}
				final := Event{Kind: "collection_stats", Time: time.Now().UTC()}
				if err := collection.report(&final); err != nil {
					return err
				}
				return emit(final)
			}
			return err
		}
		event, err := decodeDiskRecord(record.RawSample)
		if err != nil {
			return err
		}
		event.Disk.DeviceName = deviceNames.lookup(event.Disk.Device)
		event.Disk.IOCgroupError = "request ownership requires supported runtime kernel BTF"
		if err := collection.report(&event); err != nil {
			return err
		}
		if request.Matches(event) {
			if err := emit(event); err != nil {
				return err
			}
		}
	}
}
