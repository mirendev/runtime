package query

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestPacketParsingAndFiltering(t *testing.T) {
	// Ethernet + IPv4 with a 24-byte IP header; src port 80 is deliberately
	// different from dst port 8080 so a reversed port filter cannot pass.
	frame := make([]byte, 14+24+20)
	binary.BigEndian.PutUint16(frame[12:], 0x0800)
	ip := frame[14:]
	ip[0], ip[9] = 0x46, 6
	binary.BigEndian.PutUint16(ip[2:], 44)
	copy(ip[12:], []byte{192, 0, 2, 10})
	copy(ip[16:], []byte{198, 51, 100, 20})
	binary.BigEndian.PutUint16(ip[24:], 80)
	binary.BigEndian.PutUint16(ip[26:], 8080)
	ip[24+12] = 0x50
	packet := parsePacket(frame, "outgoing")
	if packet == nil || packet.SourceIP != "192.0.2.10" || packet.DestinationIP != "198.51.100.20" || packet.SourcePort != 80 || packet.DestinationPort != 8080 || packet.Protocol != "tcp" || packet.Length != len(frame) || !bytes.Equal(packet.Data, frame) {
		t.Fatalf("IPv4 packet: %+v", packet)
	}
	request := MonitorRequest{Source: "packets", Packet: &PacketFilter{Protocol: "tcp", Direction: "outgoing", DestinationIP: "198.51.100.20", DestinationPort: 80}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if request.Matches(Event{Packet: packet}) {
		t.Fatal("matched source port 80 as destination port 80")
	}
	request.Packet.DestinationPort = 8080
	if !request.Matches(Event{Packet: packet}) {
		t.Fatal("matching outgoing TCP packet excluded")
	}
	// The stated use case must match packets sent to port 80, not merely
	// packets whose source is port 80.
	binary.BigEndian.PutUint16(ip[24:], 8080)
	binary.BigEndian.PutUint16(ip[26:], 80)
	toHTTP := parsePacket(frame, "outgoing")
	if !(MonitorRequest{Source: "packets", Packet: &PacketFilter{Protocol: "tcp", Direction: "outgoing", DestinationPort: 80}}).Matches(Event{Packet: toHTTP}) {
		t.Fatalf("outgoing TCP port 80 packet excluded: %+v", toHTTP)
	}
	for _, change := range []func(*PacketFilter){
		func(p *PacketFilter) { p.Protocol = "udp" },
		func(p *PacketFilter) { p.Direction = "incoming" },
		func(p *PacketFilter) { p.SourceIP = "192.0.2.11" },
		func(p *PacketFilter) { p.DestinationIP = "198.51.100.21" },
		func(p *PacketFilter) { p.SourcePort = 81 },
	} {
		filter := *request.Packet
		change(&filter)
		if (MonitorRequest{Source: "packets", Packet: &filter}).Matches(Event{Packet: packet}) {
			t.Fatalf("mismatched filter accepted: %+v", filter)
		}
	}
	if request.Matches(Event{PID: 80}) || (MonitorRequest{Source: "syscalls"}).Matches(Event{Packet: packet}) {
		t.Fatal("event from the wrong source matched")
	}

	// One VLAN tag and IPv6 UDP with both ports and the full address width.
	v6 := make([]byte, 18+40+8)
	binary.BigEndian.PutUint16(v6[12:], 0x8100)
	binary.BigEndian.PutUint16(v6[16:], 0x86dd)
	ip6 := v6[18:]
	ip6[0], ip6[6] = 0x60, 17
	binary.BigEndian.PutUint16(ip6[4:], 8)
	copy(ip6[8:], []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	copy(ip6[24:], []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2})
	binary.BigEndian.PutUint16(ip6[40:], 53)
	binary.BigEndian.PutUint16(ip6[42:], 53000)
	packet = parsePacket(v6, "incoming")
	if packet == nil || packet.SourceIP != "2001:db8::1" || packet.DestinationIP != "2001:db8::2" || packet.Protocol != "udp" || packet.SourcePort != 53 || packet.DestinationPort != 53000 || packet.Length != len(v6) {
		t.Fatalf("VLAN IPv6 UDP packet: %+v", packet)
	}
	if !(MonitorRequest{Source: "packets", Packet: &PacketFilter{Protocol: "udp", DestinationPort: 53000}}).Matches(Event{Packet: packet}) {
		t.Fatal("valid IPv6 UDP filter excluded")
	}
	if !(MonitorRequest{Source: "packets", Packet: &PacketFilter{SourceIP: "2001:0db8:0:0:0:0:0:1"}}).Matches(Event{Packet: packet}) {
		t.Fatal("equivalent expanded IPv6 address excluded")
	}
	encoded, err := json.Marshal(Event{Packet: packet})
	if err != nil || !strings.Contains(string(encoded), `"packet":`) || strings.Contains(string(encoded), `"syscall":`) {
		t.Fatalf("packet event JSON: %s, %v", encoded, err)
	}
	encoded, err = json.Marshal(Event{Syscall: 0})
	if err != nil || !strings.Contains(string(encoded), `"syscall":0`) {
		t.Fatalf("syscall zero JSON: %s, %v", encoded, err)
	}
	// Fragment continuation has no transport header, and truncated headers
	// must never be interpreted as ports from arbitrary bytes.
	binary.BigEndian.PutUint16(ip[6:], 1)
	if parsePacket(frame, "incoming") != nil || parsePacket(v6[:len(v6)-5], "incoming") != nil {
		t.Fatal("fragment or truncated packet accepted")
	}
}

func TestPacketFilterValidationAndSignature(t *testing.T) {
	for _, bad := range []MonitorRequest{
		{Source: "syscalls", Packet: &PacketFilter{Protocol: "tcp"}},
		{Source: "packets", PID: 1},
		{Source: "packets", Packet: &PacketFilter{Protocol: "icmp"}},
		{Source: "packets", Packet: &PacketFilter{DestinationPort: 80}},
		{Source: "packets", Packet: &PacketFilter{Protocol: "tcp", Direction: "sideways"}},
		{Source: "packets", Packet: &PacketFilter{Protocol: "tcp", DestinationIP: "not-an-ip"}},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("invalid request accepted: %+v", bad)
		}
	}

}
