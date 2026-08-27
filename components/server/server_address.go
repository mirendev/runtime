//go:build linux

package server

import (
	"log/slog"
	"net"
	"strconv"
	"strings"
)

// defaultServerPort mirrors pkg/serverconfig's default for --address.
const defaultServerPort = 8443

// NormalizeServerAddress adds the default port when an address does not name
// one explicitly. Server binding and local client configuration both use this
// path so they cannot disagree about where the API is listening.
func NormalizeServerAddress(log *slog.Logger, address string) string {
	if strings.HasPrefix(address, ":") {
		return address
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, strconv.Itoa(defaultServerPort))
		log.Debug("no port specified in server address, using default 8443", "address", address)
	}
	return address
}

// LocalClientAddress returns a loopback address for a client connecting to a
// server that listens on all interfaces.
func LocalClientAddress(log *slog.Logger, address string) string {
	address = NormalizeServerAddress(log, address)
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	ip := net.ParseIP(host)
	if host == "" || ip != nil && ip.IsUnspecified() {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return address
}

// serverPort returns the port the API listens on. Both the bridge firewall rule
// and the address injected into sandboxes derive from this value.
func serverPort(log *slog.Logger, address string) int {
	_, portValue, err := net.SplitHostPort(address)
	if err != nil {
		log.Warn("could not parse server address, assuming default API port",
			"address", address, "port", defaultServerPort, "error", err)
		return defaultServerPort
	}

	port, err := strconv.Atoi(portValue)
	if err != nil || port < 1 || port > 65535 {
		log.Warn("server address has no usable port, assuming default API port",
			"address", address, "port", defaultServerPort)
		return defaultServerPort
	}
	return port
}
