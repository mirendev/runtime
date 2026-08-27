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
		{name: "explicit port", input: "127.0.0.1:9443", address: "127.0.0.1:9443", port: 9443},
		{name: "all interfaces", input: ":7443", address: ":7443", port: 7443},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address := normalizedServerAddress(testLogger(), test.input)
			if address != test.address {
				t.Fatalf("normalizedServerAddress() = %q, want %q", address, test.address)
			}
			if port := serverPort(testLogger(), address); port != test.port {
				t.Fatalf("serverPort() = %d, want %d", port, test.port)
			}
		})
	}
}
