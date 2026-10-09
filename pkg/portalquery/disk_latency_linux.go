//go:build linux

package query

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

const diskPendingMax = 16384

type diskBTFLayout struct {
	issueArg, completeArg            int
	rqQ, rqSector, rqBytes, rqFlags  int16
	qDisk, diskMajor, diskFirstMinor int16
	flagBits                         requestFlagBits
	ioCgroupOffsets                  []int16
}

type requestFlagBits struct {
	valid                             bool
	sync, meta, fua, preflush, rahead uint8
}

func loadRequestFlagBits(spec *btf.Spec) (requestFlagBits, error) {
	b := requestFlagBits{}
	want := map[string]*uint8{"__REQ_SYNC": &b.sync, "__REQ_META": &b.meta,
		"__REQ_FUA": &b.fua, "__REQ_PREFLUSH": &b.preflush, "__REQ_RAHEAD": &b.rahead}
	found := make(map[string]bool)
	for typ, err := range spec.All() {
		if err != nil {
			return b, fmt.Errorf("iterate kernel BTF request flags: %w", err)
		}
		e, ok := typ.(*btf.Enum)
		if !ok {
			continue
		}
		for _, value := range e.Values {
			if dst := want[value.Name]; dst != nil {
				if value.Value >= 32 {
					return b, fmt.Errorf("unsupported kernel BTF request flag %s=%d", value.Name, value.Value)
				}
				*dst, found[value.Name] = uint8(value.Value), true
			}
		}
	}
	for name := range want {
		if !found[name] {
			return b, fmt.Errorf("kernel BTF lacks request flag enum %s", name)
		}
	}
	b.valid = true
	return b, nil
}

func memberOffset(s *btf.Struct, name string) (int16, btf.Type, error) {
	for _, m := range s.Members {
		if m.Name == name {
			if m.BitfieldSize != 0 || m.Offset%8 != 0 || m.Offset/8 > 4095 {
				return 0, nil, fmt.Errorf("unsupported bitfield/offset for %s.%s", s.Name, name)
			}
			return int16(m.Offset / 8), m.Type, nil
		}
		if m.Name == "" && m.Offset%8 == 0 {
			var members []btf.Member
			switch t := btf.UnderlyingType(m.Type).(type) {
			case *btf.Struct:
				members = t.Members
			case *btf.Union:
				members = t.Members
			}
			if members != nil {
				offset, typ, err := memberOffset(&btf.Struct{Name: s.Name, Members: members}, name)
				if err == nil && int64(offset)+int64(m.Offset/8) <= 4095 {
					return offset + int16(m.Offset/8), typ, nil
				}
			}
		}
	}
	return 0, nil, fmt.Errorf("kernel BTF %s lacks %s", s.Name, name)
}

func pointerStruct(t btf.Type, want string) (*btf.Struct, error) {
	p, ok := btf.UnderlyingType(t).(*btf.Pointer)
	if !ok {
		return nil, fmt.Errorf("%s is not a pointer", want)
	}
	s, ok := btf.UnderlyingType(p.Target).(*btf.Struct)
	if !ok || s.Name != want {
		return nil, fmt.Errorf("expected pointer to %s", want)
	}
	return s, nil
}

func rawRequestArg(spec *btf.Spec, name string, trailing int) (int, error) {
	types, err := spec.AnyTypesByName("btf_trace_" + name)
	if err != nil {
		return 0, fmt.Errorf("kernel BTF lacks raw tracepoint prototype btf_trace_%s: %w", name, err)
	}
	var proto *btf.FuncProto
	for _, typ := range types {
		underlying := btf.UnderlyingType(typ)
		if pointer, ok := underlying.(*btf.Pointer); ok {
			underlying = btf.UnderlyingType(pointer.Target)
		}
		if p, ok := underlying.(*btf.FuncProto); ok {
			proto = p
			break
		}
	}
	if proto == nil {
		return 0, fmt.Errorf("invalid btf_trace_%s prototype", name)
	}
	found := -1
	for i, p := range proto.Params {
		if _, err := pointerStruct(p.Type, "request"); err == nil {
			if found >= 0 {
				return 0, fmt.Errorf("ambiguous request argument in btf_trace_%s", name)
			}
			found = i
		}
	}
	if found < 0 || len(proto.Params)-found-1 != trailing || found > 1 {
		return 0, fmt.Errorf("unsupported btf_trace_%s signature (%d arguments, request at %d)", name, len(proto.Params), found)
	}
	// BTF trace typedefs may include their internal void *__data first. It is
	// not present in the raw tracepoint argument array.
	if found == 1 {
		p, ok := btf.UnderlyingType(proto.Params[0].Type).(*btf.Pointer)
		if !ok {
			return 0, fmt.Errorf("unsupported leading argument in btf_trace_%s", name)
		}
		if _, ok := btf.UnderlyingType(p.Target).(*btf.Void); !ok {
			return 0, fmt.Errorf("unsupported leading argument in btf_trace_%s", name)
		}
		return 0, nil
	}
	return found, nil
}

func loadDiskBTFLayout() (diskBTFLayout, error) {
	if strconv.IntSize != 64 {
		return diskBTFLayout{}, errors.New("disk completion requires a native 64-bit server/kernel ABI")
	}
	spec, err := btf.LoadKernelSpec()
	if err != nil {
		return diskBTFLayout{}, fmt.Errorf("load kernel BTF for disk completion: %w", err)
	}
	issue, err := rawRequestArg(spec, "block_rq_issue", 0)
	if err != nil {
		return diskBTFLayout{}, err
	}
	complete, err := rawRequestArg(spec, "block_rq_complete", 2)
	if err != nil {
		return diskBTFLayout{}, err
	}
	var rq *btf.Struct
	if err := spec.TypeByName("request", &rq); err != nil {
		return diskBTFLayout{}, err
	}
	l := diskBTFLayout{issueArg: issue, completeArg: complete}
	l.ioCgroupOffsets, _ = requestCgroupOffsets(spec)
	if l.flagBits, err = loadRequestFlagBits(spec); err != nil {
		return l, err
	}
	var qt btf.Type
	if l.rqQ, qt, err = memberOffset(rq, "q"); err != nil {
		return l, err
	}
	if l.rqSector, _, err = memberOffset(rq, "__sector"); err != nil {
		return l, err
	}
	if l.rqBytes, _, err = memberOffset(rq, "__data_len"); err != nil {
		return l, err
	}
	if l.rqFlags, _, err = memberOffset(rq, "cmd_flags"); err != nil {
		return l, err
	}
	q, err := pointerStruct(qt, "request_queue")
	if err != nil {
		return l, err
	}
	var dt btf.Type
	if l.qDisk, dt, err = memberOffset(q, "disk"); err != nil {
		return l, fmt.Errorf("unsupported request_queue layout: %w", err)
	}
	d, err := pointerStruct(dt, "gendisk")
	if err != nil {
		return l, err
	}
	if l.diskMajor, _, err = memberOffset(d, "major"); err != nil {
		return l, err
	}
	if l.diskFirstMinor, _, err = memberOffset(d, "first_minor"); err != nil {
		return l, err
	}
	for _, field := range []struct {
		s    *btf.Struct
		name string
		size int
	}{
		{rq, "__sector", 8}, {rq, "__data_len", 4}, {rq, "cmd_flags", 4}, {d, "major", 4}, {d, "first_minor", 4},
	} {
		_, typ, err := memberOffset(field.s, field.name)
		if err != nil {
			return l, err
		}
		size, err := btf.Sizeof(typ)
		if err != nil || size != field.size {
			return l, fmt.Errorf("unsupported kernel BTF %s.%s size", field.s.Name, field.name)
		}
	}
	return l, nil
}

func probeRead(dst int16, base asm.Register, off int16, size int32, fail string) asm.Instructions {
	return asm.Instructions{asm.Mov.Reg(asm.R1, asm.RFP), asm.Add.Imm(asm.R1, int32(dst)), asm.Mov.Imm(asm.R2, size), asm.Mov.Reg(asm.R3, base), asm.Add.Imm(asm.R3, int32(off)), asm.FnProbeReadKernel.Call(), asm.JNE.Imm(asm.R0, 0, fail)}
}

// Pending value: issuing identity (24), ktime, device, bytes remaining,
// sector, sectors, operation, status, original cmd_flags and charged cgroup ID
// (72 bytes). A nonzero eventsFD emits issues instead of storing pending values.
func diskIssueCompletionInstructions(l diskBTFLayout, pendingFD, eventsFD int, collection *collectionState) asm.Instructions {
	// BlockIssues counts raw issue callbacks before pointer/layout/map handling.
	// BlockReissues counts issue callbacks whose request pointer is already
	// pending; it is callback context, not a count of distinct requests.
	i := asm.Instructions{asm.Mov.Reg(asm.R6, asm.R1)}
	i = appendCollectionCounter(i, collection, collectionBlockIssues, "issue_counted")
	i = append(i, asm.LoadMem(asm.R6, asm.R6, int16(l.issueArg*8), asm.DWord), asm.JEq.Imm(asm.R6, 0, "pairing_failed"))
	i = appendTaskIdentity(i, -88)
	i = append(i, asm.FnKtimeGetNs.Call(), asm.StoreMem(asm.RFP, -64, asm.R0, asm.DWord))
	i = append(i, probeRead(-52, asm.R6, l.rqBytes, 4, "pairing_failed")...)
	i = append(i, probeRead(-48, asm.R6, l.rqSector, 8, "pairing_failed")...)
	i = append(i, probeRead(-36, asm.R6, l.rqFlags, 4, "pairing_failed")...)
	i = append(i, probeRead(-32, asm.R6, l.rqQ, 8, "pairing_failed")...)
	i = append(i, asm.LoadMem(asm.R7, asm.RFP, -32, asm.DWord), asm.JEq.Imm(asm.R7, 0, "pairing_failed"))
	i = append(i, probeRead(-32, asm.R7, l.qDisk, 8, "pairing_failed")...)
	i = append(i, asm.LoadMem(asm.R7, asm.RFP, -32, asm.DWord), asm.JEq.Imm(asm.R7, 0, "pairing_failed"))
	i = append(i, probeRead(-56, asm.R7, l.diskMajor, 4, "pairing_failed")...)
	i = append(i, probeRead(-32, asm.R7, l.diskFirstMinor, 4, "pairing_failed")...)
	// Kernel tracepoint dev_t uses MKDEV (major << 20 | minor), not
	// userspace new_encode_dev, which moves the high minor bits.
	i = append(i, asm.LoadMem(asm.R7, asm.RFP, -56, asm.Word), asm.LSh.Imm(asm.R7, 20), asm.LoadMem(asm.R8, asm.RFP, -32, asm.Word), asm.Or.Reg(asm.R7, asm.R8), asm.StoreMem(asm.RFP, -56, asm.R7, asm.Word), asm.Mov.Imm(asm.R0, 0), asm.StoreMem(asm.RFP, -32, asm.R0, asm.DWord))
	// Save original byte count as sectors and the low REQ_OP_BITS byte.
	i = append(i, asm.LoadMem(asm.R7, asm.RFP, -52, asm.Word), asm.RSh.Imm(asm.R7, 9), asm.StoreMem(asm.RFP, -40, asm.R7, asm.Word), asm.LoadMem(asm.R7, asm.RFP, -36, asm.Word), asm.StoreMem(asm.RFP, -28, asm.R7, asm.Word), asm.And.Imm(asm.R7, 0xff), asm.StoreMem(asm.RFP, -36, asm.R7, asm.Byte), asm.StoreMem(asm.RFP, -96, asm.R6, asm.DWord))
	i = appendRequestCgroup(i, l.ioCgroupOffsets)
	if eventsFD != 0 {
		i = append(i, asm.Mov.Imm(asm.R0, 0), asm.StoreMem(asm.RFP, -16, asm.R0, asm.DWord), asm.LoadMapPtr(asm.R1, eventsFD), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, -88), asm.Mov.Imm(asm.R3, 80), asm.Mov.Imm(asm.R4, 0), asm.FnRingbufOutput.Call())
		i = appendRingLoss(i, collection)
		i = append(i, asm.Ja.Label("exit"))
	} else {
		if collection != nil {
			i = append(i, asm.LoadMapPtr(asm.R1, pendingFD), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, -96), asm.FnMapLookupElem.Call(), asm.JEq.Imm(asm.R0, 0, "not_reissue"))
			i = appendCollectionCounter(i, collection, collectionBlockReissues, "not_reissue")
		}
		i = append(i, asm.LoadMapPtr(asm.R1, pendingFD), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, -96), asm.Mov.Reg(asm.R3, asm.RFP), asm.Add.Imm(asm.R3, -88), asm.Mov.Imm(asm.R4, 0), asm.FnMapUpdateElem.Call(), asm.JEq.Imm(asm.R0, 0, "exit"))
	}
	i = append(i, asm.Mov.Imm(asm.R0, 0).WithSymbol("pairing_failed"))
	i = appendCollectionCounter(i, collection, collectionPairingFailed, "pairing_done")
	i = append(i, asm.Ja.Label("exit"))
	return append(i, asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"), asm.Return())
}

func diskCompleteInstructions(l diskBTFLayout, pendingFD, eventsFD int, collection *collectionState) asm.Instructions {
	// Completion prototype is (rq, error, nr_bytes). Partial completions retain
	// the pointer-keyed entry with decremented remaining bytes. BlockCompletions
	// counts every callback. UnmatchedExits therefore also counts callbacks
	// without a pending pointer, including repeated/partial callbacks; it is not
	// a count of distinct requests.
	i := asm.Instructions{asm.LoadMem(asm.R6, asm.R1, int16(l.completeArg*8), asm.DWord), asm.LoadMem(asm.R7, asm.R1, int16((l.completeArg+2)*8), asm.DWord), asm.LoadMem(asm.R8, asm.R1, int16((l.completeArg+1)*8), asm.DWord), asm.StoreMem(asm.RFP, -100, asm.R8, asm.Word), asm.StoreMem(asm.RFP, -96, asm.R6, asm.DWord)}
	i = appendCollectionCounter(i, collection, collectionBlockCompletions, "completion_counted")
	i = append(i, asm.LoadMapPtr(asm.R1, pendingFD), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, -96), asm.FnMapLookupElem.Call(), asm.JEq.Imm(asm.R0, 0, "unmatched"), asm.Mov.Reg(asm.R9, asm.R0), asm.LoadMem(asm.R8, asm.R9, 36, asm.Word), asm.JLT.Reg(asm.R7, asm.R8, "partial"))
	// Copy metadata before deleting; map-value pointers are invalid afterwards.
	for off := int16(0); off < 72; off += 8 {
		load := asm.LoadMem(asm.R8, asm.R9, off, asm.DWord)
		if off == 0 {
			load = load.WithSymbol("full")
		}
		i = append(i, load, asm.StoreMem(asm.RFP, -88+off, asm.R8, asm.DWord))
	}
	i = append(i, asm.LoadMem(asm.R8, asm.RFP, -100, asm.Word), asm.JEq.Imm(asm.R8, 0, "status_saved"), asm.StoreMem(asm.RFP, -32, asm.R8, asm.Word))
	i = append(i, asm.LoadMapPtr(asm.R1, pendingFD).WithSymbol("status_saved"), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, -96), asm.FnMapDeleteElem.Call())
	if collection != nil {
		i = append(i, asm.JEq.Imm(asm.R0, 0, "delete_done"))
		i = appendCollectionCounter(i, collection, collectionPairingFailed, "delete_done")
	}
	i = appendCollectionCounter(i, collection, collectionBlockFinalCompletions, "final_counted")
	i = append(i, asm.FnKtimeGetNs.Call(), asm.LoadMem(asm.R7, asm.RFP, -64, asm.DWord), asm.Sub.Reg(asm.R0, asm.R7), asm.StoreMem(asm.RFP, -16, asm.R0, asm.DWord), asm.LoadMapPtr(asm.R1, eventsFD), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, -88), asm.Mov.Imm(asm.R3, 80), asm.Mov.Imm(asm.R4, 0), asm.FnRingbufOutput.Call())
	i = appendRingLoss(i, collection)
	i = append(i, asm.Ja.Label("exit"), asm.Sub.Reg(asm.R8, asm.R7).WithSymbol("partial"), asm.StoreMem(asm.R9, 36, asm.R8, asm.Word))
	i = appendCollectionCounter(i, collection, collectionBlockPartialCompletions, "partial_counted")
	i = append(i, asm.LoadMem(asm.R8, asm.RFP, -100, asm.Word), asm.JEq.Imm(asm.R8, 0, "exit"), asm.StoreMem(asm.R9, 56, asm.R8, asm.Word), asm.Ja.Label("exit"))
	i = append(i, asm.Mov.Imm(asm.R0, 0).WithSymbol("unmatched"))
	i = appendCollectionCounter(i, collection, collectionUnmatchedExit, "unmatched_done")
	return append(i, asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"), asm.Return())
}

func decodeDiskCompletionRecord(raw []byte) (Event, error) {
	return decodeDiskCompletionRecordWithFlags(raw, requestFlagBits{})
}

func requestRWBS(operation byte, flags uint32, b requestFlagBits) string {
	// Match Linux blk_fill_rwbs: preflush precedes the operation; FUA,
	// readahead, sync and metadata follow it. Other request flags are not RWBS.
	rwbs := map[byte]string{0: "R", 1: "W", 2: "F", 3: "D", 5: "DE"}[operation]
	if rwbs == "" {
		rwbs = "N"
	}
	if !b.valid {
		return rwbs
	}
	has := func(bit uint8) bool { return flags&(uint32(1)<<bit) != 0 }
	if has(b.preflush) {
		rwbs = "F" + rwbs
	}
	if has(b.fua) {
		rwbs += "F"
	}
	if has(b.rahead) {
		rwbs += "A"
	}
	if has(b.sync) {
		rwbs += "S"
	}
	if has(b.meta) {
		rwbs += "M"
	}
	return rwbs
}

func decodeDiskCompletionRecordWithFlags(raw []byte, bits requestFlagBits) (Event, error) {
	if len(raw) != 80 {
		return Event{}, errors.New("invalid eBPF disk completion record")
	}
	e := Event{Time: time.Now().UTC(), Phase: "completion"}
	decodeTaskIdentity(raw[:24], &e)
	op := map[byte]string{0: "read", 1: "write", 2: "flush", 3: "discard"}[raw[52]]
	if op == "" {
		op = "other"
	}
	flags := binary.NativeEndian.Uint32(raw[60:64])
	e.Disk = &DiskEvent{Device: binary.NativeEndian.Uint32(raw[32:36]), Sectors: binary.NativeEndian.Uint32(raw[48:52]), Sector: binary.NativeEndian.Uint64(raw[40:48]), Operation: op, RWBS: requestRWBS(raw[52], flags, bits), RequestFlags: &flags}
	if id := binary.NativeEndian.Uint64(raw[64:72]); id != 0 {
		e.Disk.IOCgroupID = &id
	}
	d := binary.NativeEndian.Uint64(raw[72:80])
	e.Disk.DurationNS = &d
	status := binary.NativeEndian.Uint32(raw[56:60])
	e.Disk.Status = &status
	return e, nil
}

func diskRequestEvents(ctx context.Context, request MonitorRequest, emit func(Event) error, l diskBTFLayout) error {
	events, err := ebpf.NewMap(&ebpf.MapSpec{Name: "portal_disk_done", Type: ebpf.RingBuf, MaxEntries: 1 << 16})
	if err != nil {
		return err
	}
	defer events.Close()
	c, err := newCollectionState()
	if err != nil {
		return err
	}
	defer c.close()
	var cl link.Link
	pendingFD, issueEventsFD := 0, events.FD()
	if request.Phase == "completion" {
		pending, err := ebpf.NewMap(&ebpf.MapSpec{Name: "portal_disk_pending", Type: ebpf.Hash, KeySize: 8, ValueSize: 72, MaxEntries: diskPendingMax})
		if err != nil {
			return fmt.Errorf("create disk pairing map: %w", err)
		}
		defer pending.Close()
		pendingFD, issueEventsFD = pending.FD(), 0
		complete, err := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "portal_disk_complete", Type: ebpf.RawTracepoint, License: "GPL", Instructions: diskCompleteInstructions(l, pendingFD, events.FD(), c)})
		if err != nil {
			return fmt.Errorf("load block completion program: %w", err)
		}
		defer complete.Close()
		cl, err = link.AttachRawTracepoint(link.RawTracepointOptions{Name: "block_rq_complete", Program: complete})
		if err != nil {
			return err
		}
		defer cl.Close()
	}
	issue, err := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "portal_disk_pair", Type: ebpf.RawTracepoint, License: "GPL", Instructions: diskIssueCompletionInstructions(l, pendingFD, issueEventsFD, c)})
	if err != nil {
		return fmt.Errorf("load block issue pairing program: %w", err)
	}
	defer issue.Close()
	il, err := link.AttachRawTracepoint(link.RawTracepointOptions{Name: "block_rq_issue", Program: issue})
	if err != nil {
		return err
	}
	defer il.Close()
	r, err := ringbuf.NewReader(events)
	if err != nil {
		return err
	}
	defer r.Close()
	stop := context.AfterFunc(ctx, func() { r.Close() })
	defer stop()
	deviceNames := newDiskDeviceNames()
	cgroupPaths := &diskCgroupPaths{mount: "/sys/fs/cgroup"}
	for {
		rec, err := r.Read()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, ringbuf.ErrClosed) {
				if err := il.Close(); err != nil {
					return err
				}
				if cl != nil {
					if err := cl.Close(); err != nil {
						return err
					}
				}
				final := Event{Kind: "collection_stats", Time: time.Now().UTC()}
				if err = c.report(&final); err != nil {
					return err
				}
				return emit(final)
			}
			return err
		}
		e, err := decodeDiskCompletionRecordWithFlags(rec.RawSample, l.flagBits)
		if err != nil {
			return err
		}
		if request.Phase != "completion" {
			e.Phase = ""
			e.Disk.DurationNS, e.Disk.Status = nil, nil
		}
		e.Disk.DeviceName = deviceNames.lookup(e.Disk.Device)
		cgroupPaths.enrich(e.Disk)
		if err = c.report(&e); err != nil {
			return err
		}
		if request.Matches(e) {
			if err = emit(e); err != nil {
				return err
			}
		}
	}
}
