//go:build linux

package query

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

const syscallMapKeyOffset = -504

func syscallFilter(insns asm.Instructions, request MonitorRequest, loadID asm.Instruction, miss string) asm.Instructions {
	insns = append(insns, asm.FnGetCurrentPidTgid.Call(), asm.Mov.Reg(asm.R7, asm.R0))
	if request.PID != 0 {
		insns = append(insns, asm.Mov.Reg(asm.R1, asm.R0), asm.RSh.Imm(asm.R1, 32), asm.JNE.Imm32(asm.R1, int32(request.PID), miss))
	}
	if os.Geteuid() != 0 {
		insns = append(insns, asm.FnGetCurrentUidGid.Call(), asm.JNE.Imm32(asm.R0, int32(os.Geteuid()), miss))
	}
	insns = append(insns, loadID)
	if len(request.Syscalls) != 0 {
		for _, id := range request.Syscalls {
			insns = append(insns, asm.JEq.Imm32(asm.R8, int32(id), "selected"))
		}
		insns = append(insns, asm.Ja.Label(miss), asm.Mov.Imm(asm.R0, 0).WithSymbol("selected"))
	}
	return insns
}

func syscallEvents(ctx context.Context, request MonitorRequest, emit func(Event) error) error {
	events, err := ebpf.NewMap(&ebpf.MapSpec{Name: "portal_events", Type: ebpf.RingBuf, MaxEntries: 1 << 16})
	if err != nil {
		return fmt.Errorf("create eBPF ring buffer: %w", err)
	}
	defer events.Close()
	collection, err := newCollectionState()
	if err != nil {
		return err
	}
	defer collection.close()
	stacks, err := newStackCaptureState(request.Stacks)
	if err != nil {
		return err
	}
	defer stacks.close()
	if stacks != nil {
		stacks.collection = collection
	}

	completion := request.Phase == "completion"
	recordSize := taskIdentitySize + 8
	entryType := ebpf.RawTracepoint
	loadID := asm.LoadMem(asm.R8, asm.R6, 8, asm.DWord)
	var idOffset, argsOffset int16
	var fileIDs []int
	var pathLayout syscallPathBTFLayout
	var pathScratch *ebpf.Map
	if request.Paths {
		idOffset, argsOffset, fileIDs, pathLayout, err = syscallPathLayout()
		if err != nil {
			return err
		}
		pathScratch, err = ebpf.NewMap(&ebpf.MapSpec{Name: "portal_path_scratch", Type: ebpf.PerCPUArray, KeySize: 4, ValueSize: syscallPathScratchSize, MaxEntries: 1})
		if err != nil {
			return fmt.Errorf("create syscall path scratch map: %w", err)
		}
		defer pathScratch.Close()
		entryType = ebpf.TracePoint
		loadID = asm.LoadMem(asm.R8, asm.RFP, -472, asm.DWord)
		recordSize += syscallPathRecordSize
	}
	entryStart := func() asm.Instructions {
		i := asm.Instructions{asm.Mov.Reg(asm.R6, asm.R1)}
		if request.Paths {
			i = append(i, probeRead(-472, asm.R6, idOffset, 8, "exit")...)
		}
		return syscallFilter(i, request, loadID, "exit")
	}
	attachEntry := func(program *ebpf.Program) (link.Link, error) {
		if request.Paths {
			return link.Tracepoint("raw_syscalls", "sys_enter", program, nil)
		}
		return link.AttachRawTracepoint(link.RawTracepointOptions{Name: "sys_enter", Program: program})
	}
	var exitFields []tracepointField
	if completion {
		exitFields, err = readTracepointFormat("raw_syscalls", "sys_exit", []string{"id", "ret"})
		if err != nil {
			return err
		}
		if exitFields[0].size != 8 || exitFields[1].size != 8 {
			return errors.New("syscall completion requires 64-bit syscall id/return fields")
		}
		recordSize += 16
	}
	if stacks != nil {
		recordSize += stacks.recordSize()
	}

	var programs []*ebpf.Program
	var links []link.Link
	closeAttached := func() {
		for _, l := range links {
			l.Close()
		}
		links = nil
		for _, p := range programs {
			p.Close()
		}
		programs = nil
	}
	defer closeAttached()

	if completion {
		pendingSize := 40
		if request.Paths {
			pendingSize += syscallPathRecordSize
		}
		if stacks != nil {
			pendingSize += stacks.recordSize()
		}
		pending, mapErr := ebpf.NewMap(&ebpf.MapSpec{Name: "portal_pending", Type: ebpf.Hash, KeySize: 8, ValueSize: uint32(pendingSize), MaxEntries: 16384})
		if mapErr != nil {
			return fmt.Errorf("create syscall pairing map: %w", mapErr)
		}
		defer pending.Close()

		// Attach exits first so an entry can never be inserted without a consumer.
		exit := asm.Instructions{asm.Mov.Reg(asm.R6, asm.R1)}
		// Unlike sys_enter, raw sys_exit has only regs and return value, not
		// a syscall ID. Use the formatted tracepoint payload for ID filtering.
		for i, field := range exitFields {
			exit = append(exit, asm.Mov.Reg(asm.R1, asm.RFP), asm.Add.Imm(asm.R1, int32(-496+i*8)), asm.Mov.Imm(asm.R2, 8), asm.Mov.Reg(asm.R3, asm.R6), asm.Add.Imm(asm.R3, int32(field.offset)), asm.FnProbeReadKernel.Call(), asm.JNE.Imm(asm.R0, 0, "exit"))
		}
		exit = syscallFilter(exit, request, asm.LoadMem(asm.R8, asm.RFP, -496, asm.DWord), "exit")
		exit = append(exit, asm.StoreMem(asm.RFP, syscallMapKeyOffset, asm.R7, asm.DWord), asm.LoadMapPtr(asm.R1, pending.FD()), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, syscallMapKeyOffset), asm.FnMapLookupElem.Call(), asm.JEq.Imm(asm.R0, 0, "unmatched"), asm.Mov.Reg(asm.R9, asm.R0))
		base := int16(-recordSize)
		// Identity and syscall were captured at entry.
		for off := int16(0); off < taskIdentitySize; off += 8 {
			exit = append(exit, asm.LoadMem(asm.R1, asm.R9, 8+off, asm.DWord), asm.StoreMem(asm.RFP, base+off, asm.R1, asm.DWord))
		}
		exit = append(exit, asm.LoadMem(asm.R1, asm.R9, 32, asm.DWord), asm.StoreMem(asm.RFP, base+24, asm.R1, asm.DWord), asm.FnKtimeGetNs.Call(), asm.LoadMem(asm.R1, asm.R9, 0, asm.DWord), asm.Sub.Reg(asm.R0, asm.R1), asm.StoreMem(asm.RFP, base+32, asm.R0, asm.DWord), asm.LoadMem(asm.R1, asm.RFP, -488, asm.DWord), asm.StoreMem(asm.RFP, base+40, asm.R1, asm.DWord))
		if stacks != nil {
			for off := int16(0); off < int16(stacks.recordSize()); off += 8 {
				exit = append(exit, asm.LoadMem(asm.R1, asm.R9, 40+off, asm.DWord), asm.StoreMem(asm.RFP, base+48+off, asm.R1, asm.DWord))
			}
		}
		if request.Paths {
			pathBase := int16(recordSize - syscallPathRecordSize)
			pendingBase := int16(pendingSize - syscallPathRecordSize)
			for off := int16(0); off < syscallPathRecordSize; off += 8 {
				exit = append(exit, asm.LoadMem(asm.R1, asm.R9, pendingBase+off, asm.DWord), asm.StoreMem(asm.RFP, base+pathBase+off, asm.R1, asm.DWord))
			}
		}
		exit = append(exit, asm.LoadMapPtr(asm.R1, pending.FD()), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, syscallMapKeyOffset), asm.FnMapDeleteElem.Call(), asm.LoadMapPtr(asm.R1, events.FD()), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, int32(base)), asm.Mov.Imm(asm.R3, int32(recordSize)), asm.Mov.Imm(asm.R4, 0), asm.FnRingbufOutput.Call())
		exit = appendRingLoss(exit, collection)
		exit = append(exit, asm.Ja.Label("exit"))
		unmatchedStart := len(exit)
		exit = appendCollectionCounter(exit, collection, collectionUnmatchedExit, "unmatched_done")
		exit[unmatchedStart] = exit[unmatchedStart].WithSymbol("unmatched")
		exit = append(exit, asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"), asm.Return())
		exitProgram, loadErr := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "portal_sys_exit", Type: ebpf.TracePoint, License: "GPL", Instructions: exit})
		if loadErr != nil {
			return fmt.Errorf("load eBPF syscall exit monitor: %w", loadErr)
		}
		programs = append(programs, exitProgram)
		exitLink, attachErr := link.Tracepoint("raw_syscalls", "sys_exit", exitProgram, nil)
		if attachErr != nil {
			return fmt.Errorf("attach eBPF syscall exit monitor: %w", attachErr)
		}
		links = append(links, exitLink)

		cleanup := asm.Instructions{asm.FnGetCurrentPidTgid.Call(), asm.StoreMem(asm.RFP, syscallMapKeyOffset, asm.R0, asm.DWord), asm.LoadMapPtr(asm.R1, pending.FD()), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, syscallMapKeyOffset), asm.FnMapDeleteElem.Call(), asm.Mov.Imm(asm.R0, 0), asm.Return()}
		cleanupProgram, loadErr := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "portal_task_exit", Type: ebpf.RawTracepoint, License: "GPL", Instructions: cleanup})
		if loadErr != nil {
			return fmt.Errorf("load eBPF task cleanup: %w", loadErr)
		}
		programs = append(programs, cleanupProgram)
		cleanupLink, attachErr := link.AttachRawTracepoint(link.RawTracepointOptions{Name: "sched_process_exit", Program: cleanupProgram})
		if attachErr != nil {
			return fmt.Errorf("attach eBPF task cleanup: %w", attachErr)
		}
		links = append(links, cleanupLink)

		entry := entryStart()
		valueBase := int16(-pendingSize)
		entry = append(entry, asm.StoreMem(asm.RFP, syscallMapKeyOffset, asm.R7, asm.DWord), asm.FnKtimeGetNs.Call(), asm.StoreMem(asm.RFP, valueBase, asm.R0, asm.DWord))
		entry = appendTaskIdentity(entry, valueBase+8)
		entry = append(entry, asm.StoreMem(asm.RFP, valueBase+32, asm.R8, asm.DWord))
		if stacks != nil {
			entry = appendStackCapture(entry, asm.R6, valueBase+40, stacks)
		}
		if request.Paths {
			entry = appendSyscallPath(entry, pathScratch.FD(), valueBase+int16(pendingSize-syscallPathRecordSize), argsOffset, fileIDs, pathLayout)
		}
		entry = append(entry, asm.LoadMapPtr(asm.R1, pending.FD()), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, syscallMapKeyOffset), asm.Mov.Reg(asm.R3, asm.RFP), asm.Add.Imm(asm.R3, int32(valueBase)), asm.Mov.Imm(asm.R4, 1), asm.FnMapUpdateElem.Call(), asm.JEq.Imm(asm.R0, 0, "exit"))
		entry = appendCollectionCounter(entry, collection, collectionPairingFailed, "pairing_failed_done")
		entry = append(entry, asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"), asm.Return())
		entryProgram, loadErr := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "portal_sys_enter", Type: entryType, License: "GPL", Instructions: entry})
		if loadErr != nil {
			return fmt.Errorf("load eBPF syscall entry monitor: %w", loadErr)
		}
		programs = append(programs, entryProgram)
		entryLink, attachErr := attachEntry(entryProgram)
		if attachErr != nil {
			return fmt.Errorf("attach eBPF syscall entry monitor: %w", attachErr)
		}
		links = append(links, entryLink)
	} else {
		insns := entryStart()
		base := int16(-recordSize)
		insns = appendTaskIdentity(insns, base)
		insns = append(insns, asm.StoreMem(asm.RFP, base+24, asm.R8, asm.DWord))
		if stacks != nil {
			insns = appendStackCapture(insns, asm.R6, base+32, stacks)
		}
		if request.Paths {
			insns = appendSyscallPath(insns, pathScratch.FD(), base+int16(recordSize-syscallPathRecordSize), argsOffset, fileIDs, pathLayout)
		}
		insns = append(insns, asm.LoadMapPtr(asm.R1, events.FD()), asm.Mov.Reg(asm.R2, asm.RFP), asm.Add.Imm(asm.R2, int32(base)), asm.Mov.Imm(asm.R3, int32(recordSize)), asm.Mov.Imm(asm.R4, 0), asm.FnRingbufOutput.Call())
		insns = appendRingLoss(insns, collection)
		insns = append(insns, asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"), asm.Return())
		program, loadErr := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "portal_sys_enter", Type: entryType, License: "GPL", Instructions: insns})
		if loadErr != nil {
			return fmt.Errorf("load eBPF syscall monitor: %w", loadErr)
		}
		programs = append(programs, program)
		attached, attachErr := attachEntry(program)
		if attachErr != nil {
			return fmt.Errorf("attach eBPF syscall monitor: %w", attachErr)
		}
		links = append(links, attached)
	}

	reader, err := ringbuf.NewReader(events)
	if err != nil {
		return err
	}
	defer reader.Close()
	stop := context.AfterFunc(ctx, func() { reader.Close() })
	defer stop()
	for {
		record, readErr := reader.Read()
		if readErr != nil {
			if ctx.Err() != nil || errors.Is(readErr, ringbuf.ErrClosed) {
				closeAttached()
				final := Event{Kind: "collection_stats", Time: time.Now().UTC()}
				if err := collection.report(&final); err != nil {
					return err
				}
				return emit(final)
			}
			return readErr
		}
		if len(record.RawSample) != recordSize {
			return errors.New("invalid eBPF syscall record")
		}
		event := Event{Time: time.Now().UTC()}
		decodeTaskIdentity(record.RawSample[:taskIdentitySize], &event)
		off := taskIdentitySize
		event.Syscall = int(binary.NativeEndian.Uint64(record.RawSample[off : off+8]))
		off += 8
		if completion {
			event.Phase = "completion"
			duration := binary.NativeEndian.Uint64(record.RawSample[off : off+8])
			off += 8
			ret := int64(binary.NativeEndian.Uint64(record.RawSample[off : off+8]))
			off += 8
			event.DurationNS, event.ReturnValue = &duration, &ret
		} else {
			event.Phase = "entry"
		}
		if stacks != nil {
			if err := stacks.decode(ctx, record.RawSample[off:off+stacks.recordSize()], &event); err != nil {
				return err
			}
		}
		if request.Paths {
			resolveSyscallFile(record.RawSample[recordSize-syscallPathRecordSize:], &event)
		}
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
