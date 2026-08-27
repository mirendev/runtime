//go:build linux

package server

import "testing"

func TestNormalizedServerAddressAndPort(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		address string
		port    int
	}{
		{name: "bare host", input: "127.0.0.1", address: "127.0.0.1:8443", port: 8443},
		{name: "bare wildcard", input: "0.0.0.0", address: "0.0.0.0:8443", port: 8443},
		{name: "explicit port", input: "127.0.0.1:9443", address: "127.0.0.1:9443", port: 9443},
		{name: "all interfaces", input: ":7443", address: ":7443", port: 7443},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address := NormalizeServerAddress(testLogger(), test.input)
			if address != test.address {
				t.Fatalf("normalizedServerAddress() = %q, want %q", address, test.address)
			}
			if port := serverPort(testLogger(), address); port != test.port {
				t.Fatalf("serverPort() = %d, want %d", port, test.port)
			}
		})
	}
}

func TestLocalClientAddress(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "portless wildcard", in: "0.0.0.0", want: "127.0.0.1:8443"},
		{name: "wildcard with port", in: "0.0.0.0:9443", want: "127.0.0.1:9443"},
		{name: "empty wildcard", in: ":7443", want: "127.0.0.1:7443"},
		{name: "IPv6 wildcard", in: "[::]:6443", want: "127.0.0.1:6443"},
		{name: "qualified address", in: "10.0.0.4:9443", want: "10.0.0.4:9443"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := LocalClientAddress(testLogger(), test.in); got != test.want {
				t.Fatalf("LocalClientAddress() = %q, want %q", got, test.want)
			}
		})
	}
}
