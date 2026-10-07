//go:build linux

package query

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"golang.org/x/sys/unix"
)

func TestPacketFilterContext(t *testing.T) {
	insns := packetFilterInstructions()
	if insns[0] != asm.Mov.Reg(asm.R6, asm.R1) {
		t.Fatal("packet loads require the skb context in R6")
	}
	if err := insns.Marshal(&bytes.Buffer{}, binary.LittleEndian); err != nil {
		t.Fatal(err)
	}
}

func TestPacketFilterKernel(t *testing.T) {
	program, err := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "portal_packets", Type: ebpf.SocketFilter, License: "MIT", Instructions: packetFilterInstructions()})
	if err != nil {
		var verifier *ebpf.VerifierError
		if !errors.As(err, &verifier) && (errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES)) {
			t.Skipf("kernel eBPF loading unavailable: %v", err)
		}
		t.Fatalf("load packet filter: %+v", err)
	}
	defer program.Close()
	loopback, err := net.InterfaceByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	protocol := int(binary.NativeEndian.Uint16([]byte{0, unix.ETH_P_ALL}))
	sender, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, protocol)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sender)
	addr := &unix.SockaddrLinklayer{Ifindex: loopback.Index, Protocol: uint16(protocol)}
	for _, tc := range []struct {
		name  string
		types []uint16
		want  uint32
	}{
		{"ipv4", []uint16{0x0800}, 2048},
		{"ipv6", []uint16{0x86dd}, 2048},
		{"arp", []uint16{0x0806}, 0},
		{"vlan-ipv4", []uint16{0x8100, 0x0800}, 2048},
		{"double-vlan-ipv6", []uint16{0x88a8, 0x8100, 0x86dd}, 2048},
		{"vlan-arp", []uint16{0x8100, 0x0806}, 0},
		{"triple-vlan", []uint16{0x8100, 0x8100, 0x8100, 0x0800}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Test via AF_PACKET: BPF_PROG_TEST_RUN instead presents an skb
			// with the Ethernet header pulled, unlike our capture socket.
			fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, protocol)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(fd)
			if err := unix.Bind(fd, addr); err != nil {
				t.Fatal(err)
			}
			if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, program.FD()); err != nil {
				t.Fatal(err)
			}
			frame := make([]byte, 64)
			copy(frame, []byte{2, 0, 0, 0, 0, 1, 2, 0, 0, 0, 0, 2})
			for i, typ := range tc.types {
				binary.BigEndian.PutUint16(frame[12+4*i:], typ)
			}
			if err := unix.Sendto(sender, frame, 0, addr); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 2048)
			found := false
			for deadline := time.Now().Add(200 * time.Millisecond); time.Now().Before(deadline); {
				if _, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 20); err != nil {
					t.Fatal(err)
				}
				n, _, err := unix.Recvfrom(fd, buf, unix.MSG_DONTWAIT)
				if errors.Is(err, unix.EAGAIN) {
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Equal(buf[:n], frame) {
					found = true
					break
				}
			}
			if found != (tc.want != 0) {
				t.Fatalf("captured frame = %v, want %v", found, tc.want != 0)
			}
		})
	}
}
