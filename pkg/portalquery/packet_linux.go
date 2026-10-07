//go:build linux

package query

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"golang.org/x/sys/unix"
)

func packetFilterInstructions() asm.Instructions {
	// The socket filter admits IP frames (including up to two VLAN tags) and
	// caps copies to 2048 bytes. Endpoint and direction filters are evaluated
	// on the server after parsing, before any event is sent to the client.
	return asm.Instructions{
		// Legacy packet loads implicitly read the skb context from R6.
		asm.Mov.Reg(asm.R6, asm.R1),
		asm.LoadAbs(12, asm.Half),
		asm.JEq.Imm(asm.R0, 0x0800, "accept"),
		asm.JEq.Imm(asm.R0, 0x86dd, "accept"),
		asm.JEq.Imm(asm.R0, 0x8100, "vlan"),
		asm.JNE.Imm(asm.R0, 0x88a8, "reject"),
		asm.LoadAbs(16, asm.Half).WithSymbol("vlan"),
		asm.JEq.Imm(asm.R0, 0x0800, "accept"),
		asm.JEq.Imm(asm.R0, 0x86dd, "accept"),
		asm.JEq.Imm(asm.R0, 0x8100, "second"),
		asm.JNE.Imm(asm.R0, 0x88a8, "reject"),
		asm.LoadAbs(20, asm.Half).WithSymbol("second"),
		asm.JEq.Imm(asm.R0, 0x0800, "accept"),
		asm.JEq.Imm(asm.R0, 0x86dd, "accept"),
		asm.Mov.Imm(asm.R0, 0).WithSymbol("reject"),
		asm.Return(),
		asm.Mov.Imm(asm.R0, 2048).WithSymbol("accept"),
		asm.Return(),
	}
}

func packetEvents(ctx context.Context, request MonitorRequest, emit func(Event) error) error {
	program, err := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "portal_packets", Type: ebpf.SocketFilter, License: "MIT", Instructions: packetFilterInstructions()})
	if err != nil {
		return fmt.Errorf("load eBPF packet filter: %w", err)
	}
	defer program.Close()
	// AF_PACKET sees both incoming and outgoing packets on all interfaces.
	protocol := int(binary.NativeEndian.Uint16([]byte{0, unix.ETH_P_ALL}))
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, protocol)
	if err != nil {
		return fmt.Errorf("open packet socket: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, program.FD()); err != nil {
		return fmt.Errorf("attach eBPF packet filter: %w", err)
	}
	buf := make([]byte, 2048)
	for ctx.Err() == nil {
		_, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 200)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		n, addr, err := unix.Recvfrom(fd, buf, unix.MSG_DONTWAIT)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		direction := "incoming"
		if link, ok := addr.(*unix.SockaddrLinklayer); ok && link.Pkttype == unix.PACKET_OUTGOING {
			direction = "outgoing"
		}
		packet := parsePacket(buf[:n], direction)
		if packet == nil {
			continue
		}
		event := Event{Time: time.Now().UTC(), Packet: packet}
		if request.Matches(event) {
			if err := emit(event); err != nil {
				return err
			}
		}
	}
	return nil
}
