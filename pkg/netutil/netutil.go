package netutil

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// ParseNetworkAddress parses CIDR notation or plain IP, returns IP string
func ParseNetworkAddress(addr string) (string, error) {
	// Try parsing as CIDR first
	if prefix, err := netip.ParsePrefix(addr); err == nil {
		return prefix.Addr().String(), nil
	}

	// Try parsing as plain IP
	if ip, err := netip.ParseAddr(addr); err == nil {
		return ip.String(), nil
	}

	return "", fmt.Errorf("invalid address format: %s", addr)
}

// BuildHTTPURL parses a network address (CIDR or plain IP) and builds an HTTP URL with the given port.
// Handles both IPv4 and IPv6 addresses correctly (IPv6 addresses are wrapped in brackets).
func BuildHTTPURL(addr string, port int64) (string, error) {
	// Parse to get the IP
	ipStr, err := ParseNetworkAddress(addr)
	if err != nil {
		return "", err
	}

	// Use net.JoinHostPort which handles IPv6 brackets automatically
	hostPort := net.JoinHostPort(ipStr, strconv.FormatInt(port, 10))
	return fmt.Sprintf("http://%s", hostPort), nil
}
