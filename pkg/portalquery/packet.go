package query

import (
	"encoding/binary"
	"net/netip"
)

// parsePacket accepts Ethernet IPv4/IPv6 TCP and UDP frames. It ignores
// noninitial IPv4 fragments and IPv6 extension headers: neither has transport
// ports at the fixed offset. VLAN-tagged Ethernet frames are supported.
func parsePacket(frame []byte, direction string) *PacketEvent {
	if len(frame) < 14 {
		return nil
	}
	offset := 14
	etherType := binary.BigEndian.Uint16(frame[12:14])
	for tags := 0; tags < 2 && (etherType == 0x8100 || etherType == 0x88a8); tags++ {
		if len(frame) < offset+4 {
			return nil
		}
		etherType = binary.BigEndian.Uint16(frame[offset+2 : offset+4])
		offset += 4
	}
	var src, dst netip.Addr
	var protocol byte
	var transport int
	var length int
	switch etherType {
	case 0x0800:
		if len(frame) < offset+20 || frame[offset]>>4 != 4 {
			return nil
		}
		headerLen := int(frame[offset]&15) * 4
		length = int(binary.BigEndian.Uint16(frame[offset+2 : offset+4]))
		if headerLen < 20 || length < headerLen+4 || len(frame) < offset+headerLen+4 || binary.BigEndian.Uint16(frame[offset+6:offset+8])&0x1fff != 0 {
			return nil
		}
		src = netip.AddrFrom4([4]byte(frame[offset+12 : offset+16]))
		dst = netip.AddrFrom4([4]byte(frame[offset+16 : offset+20]))
		protocol = frame[offset+9]
		transport = offset + headerLen
	case 0x86dd:
		if len(frame) < offset+40+4 || frame[offset]>>4 != 6 {
			return nil
		}
		length = 40 + int(binary.BigEndian.Uint16(frame[offset+4:offset+6]))
		if length < 44 {
			return nil
		}
		src = netip.AddrFrom16([16]byte(frame[offset+8 : offset+24]))
		dst = netip.AddrFrom16([16]byte(frame[offset+24 : offset+40]))
		protocol = frame[offset+6]
		transport = offset + 40
	default:
		return nil
	}
	minimum := 8 // UDP header
	if protocol == 6 {
		minimum = 20 // TCP header
	} else if protocol != 17 {
		return nil
	}
	if length < transport-offset+minimum || len(frame) < transport+minimum || (len(frame) < offset+length && len(frame) < 2048) {
		return nil
	}
	if protocol == 6 {
		tcpHeader := int(frame[transport+12]>>4) * 4
		if tcpHeader < 20 || length < transport-offset+tcpHeader || len(frame) < transport+tcpHeader {
			return nil
		}
	}
	packet := &PacketEvent{Direction: direction, SourceIP: src.String(), DestinationIP: dst.String(),
		SourcePort: binary.BigEndian.Uint16(frame[transport : transport+2]), DestinationPort: binary.BigEndian.Uint16(frame[transport+2 : transport+4]), Length: offset + length}
	switch protocol {
	case 6:
		packet.Protocol = "tcp"
	case 17:
		packet.Protocol = "udp"
	}
	// Include only the IP frame, not trailing Ethernet padding. Length still
	// reports the full frame size when the socket's capture limit truncated it.
	end := min(len(frame), packet.Length)
	packet.Data = append([]byte(nil), frame[:end]...)
	return packet
}
