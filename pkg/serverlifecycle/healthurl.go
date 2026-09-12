package serverlifecycle

import (
	"net"
	"strings"

	"miren.dev/runtime/pkg/serverconfig"
)

// HealthURL is where a server with this ingress mode and address answers
// /.well-known/miren/health from its own host.
func HealthURL(mode, address string) string {
	const path = "/.well-known/miren/health"
	switch mode {
	case serverconfig.IngressModeBehindProxyHTTP:
		return "http://" + localAddr(address, "80") + path
	case serverconfig.IngressModeBehindProxyHTTPS:
		return "https://" + localAddr(address, "443") + path
	}
	return DefaultHealthURL
}

// localAddr makes a listen address dialable from the same host.
func localAddr(address, defaultPort string) string {
	if address == "" {
		return "127.0.0.1:" + defaultPort
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		if strings.HasPrefix(address, ":") {
			return "127.0.0.1" + address
		}
		return address + ":" + defaultPort
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
