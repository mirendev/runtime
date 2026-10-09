//go:build linux

package query

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
)

const (
	stackMapEntries = 16384
	stackFlagUser   = 1 << 8
)

type stackCaptureState struct {
	collection *collectionState
	spec       StackCapture
	stackMap   *ebpf.Map
	kernelSyms []inspectedSymbol
	kernelErr  error
}

func newStackCaptureState(spec *StackCapture) (*stackCaptureState, error) {
	if spec == nil {
		return nil, nil
	}
	m, err := ebpf.NewMap(&ebpf.MapSpec{Name: "portal_stacks", Type: ebpf.StackTrace, KeySize: 4, ValueSize: uint32(spec.depth() * 8), MaxEntries: stackMapEntries})
	if err != nil {
		return nil, fmt.Errorf("create eBPF stack map: %w", err)
	}
	s := &stackCaptureState{spec: *spec, stackMap: m}
	if spec.Symbolize && spec.Kernel {
		s.kernelSyms, s.kernelErr = readKernelSymbols(context.Background())
	}
	return s, nil
}

func (s *stackCaptureState) close() {
	if s != nil {
		s.stackMap.Close()
	}
}

// appendStackCapture stores pid/tid followed by signed user and kernel stack
// IDs. A negative helper return value is deliberately preserved in the record.
func appendStackCapture(insns asm.Instructions, ctxReg asm.Register, stackOffset int16, s *stackCaptureState) asm.Instructions {
	if s == nil {
		return insns
	}
	insns = append(insns,
		asm.FnGetCurrentPidTgid.Call(),
		asm.StoreMem(asm.RFP, stackOffset, asm.R0, asm.DWord),
	)
	off := stackOffset + 8
	for _, enabled := range []struct {
		on    bool
		flags int32
	}{{s.spec.User, stackFlagUser}, {s.spec.Kernel, 0}} {
		if enabled.on {
			insns = append(insns,
				asm.Mov.Reg(asm.R1, ctxReg),
				asm.LoadMapPtr(asm.R2, s.stackMap.FD()),
				asm.Mov.Imm(asm.R3, enabled.flags),
				asm.FnGetStackid.Call(),
				asm.StoreMem(asm.RFP, off, asm.R0, asm.DWord),
			)
			if s.collection != nil {
				label := fmt.Sprintf("stack_%d_done", off)
				collision := fmt.Sprintf("stack_%d_collision_done", off)
				// Preserve the signed helper result in the event before counters call helpers.
				insns = append(insns, asm.JSGE.Imm(asm.R0, 0, label), asm.JNE.Imm(asm.R0, -17, collision))
				insns = appendCollectionCounter(insns, s.collection, collectionStackCollision, collision)
				insns = appendCollectionCounter(insns, s.collection, collectionStackFailed, label)
			}
			off += 8
		}
	}
	return insns
}

func (s *stackCaptureState) recordSize() int {
	if s == nil {
		return 0
	}
	n := 8
	if s.spec.User {
		n += 8
	}
	if s.spec.Kernel {
		n += 8
	}
	return n
}

func (s *stackCaptureState) decode(ctx context.Context, raw []byte, event *Event) error {
	if s == nil {
		return nil
	}
	if len(raw) != s.recordSize() {
		return errors.New("invalid eBPF stack record")
	}
	pidTID := binary.NativeEndian.Uint64(raw[:8])
	event.PID, event.TID = uint32(pidTID>>32), uint32(pidTID)
	off := 8
	if s.spec.User {
		event.UserStack = s.stack(ctx, int64(binary.NativeEndian.Uint64(raw[off:off+8])), true, event.PID)
		off += 8
	}
	if s.spec.Kernel {
		event.KernelStack = s.stack(ctx, int64(binary.NativeEndian.Uint64(raw[off:off+8])), false, event.PID)
	}
	return nil
}

func (s *stackCaptureState) stack(ctx context.Context, id int64, user bool, pid uint32) *CapturedStack {
	if id < 0 {
		return &CapturedStack{Error: fmt.Sprintf("bpf_get_stackid failed: %d", id), CaptureErrorCode: id}
	}
	addresses := make([]uint64, s.spec.depth())
	key := uint32(id)
	if err := s.stackMap.Lookup(&key, &addresses); err != nil {
		return &CapturedStack{Error: "read captured stack: " + err.Error()}
	}
	for len(addresses) > 0 && addresses[len(addresses)-1] == 0 {
		addresses = addresses[:len(addresses)-1]
	}
	frames := make([]SymbolFrame, len(addresses))
	for i, address := range addresses {
		frames[i].Address = symbolHex(address)
	}
	result := &CapturedStack{Frames: frames, DepthLimitReached: len(addresses) == s.spec.depth()}
	if !s.spec.Symbolize || len(addresses) == 0 {
		return result
	}
	if user {
		if err := verifyHostPID(pid); err != nil {
			result.Error = err.Error()
			return result
		}
		r, err := InspectSymbols(ctx, SymbolRequest{Target: "process", PID: pid, Addresses: addresses})
		if err != nil {
			result.Error = "symbolize user stack: " + err.Error()
			return result
		}
		result.Frames = r.Frames
		return result
	}
	if s.kernelErr != nil {
		result.Error = "symbolize kernel stack: " + s.kernelErr.Error()
		return result
	}
	result.Frames = resultFromSymbols(ctx, SymbolRequest{Addresses: addresses}, s.kernelSyms).Frames
	return result
}

func verifyHostPID(pid uint32) error {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return fmt.Errorf("cannot map host PID %d into visible /proc (PID namespace mismatch): %w", pid, err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "NSpid:") {
			ids := strings.Fields(strings.TrimPrefix(line, "NSpid:"))
			if len(ids) == 0 || ids[0] != strconv.FormatUint(uint64(pid), 10) {
				return fmt.Errorf("host PID %d does not identify the same process in visible /proc", pid)
			}
			return nil
		}
	}
	return errors.New("cannot verify process PID namespace: NSpid is unavailable")
}
