package netutil

import (
	"testing"
)

func TestParseNetworkAddress_CIDR(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "IPv4 CIDR",
			input:    "10.0.0.1/24",
			expected: "10.0.0.1",
		},
		{
			name:     "IPv4 CIDR /32",
			input:    "192.168.1.100/32",
			expected: "192.168.1.100",
		},
		{
			name:     "IPv6 CIDR",
			input:    "2001:db8::1/64",
			expected: "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseNetworkAddress(tt.input)
			if err != nil {
				t.Errorf("ParseNetworkAddress(%q) returned error: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("ParseNetworkAddress(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseNetworkAddress_PlainIP(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "IPv4 address",
			input:    "10.0.0.1",
			expected: "10.0.0.1",
		},
		{
			name:     "IPv4 loopback",
			input:    "127.0.0.1",
			expected: "127.0.0.1",
		},
		{
			name:     "IPv6 address",
			input:    "2001:db8::1",
			expected: "2001:db8::1",
		},
		{
			name:     "IPv6 loopback",
			input:    "::1",
			expected: "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseNetworkAddress(tt.input)
			if err != nil {
				t.Errorf("ParseNetworkAddress(%q) returned error: %v", tt.input, err)
			}
			if result != tt.expected {
				t.Errorf("ParseNetworkAddress(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseNetworkAddress_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "Invalid format",
			input: "not-an-ip",
		},
		{
			name:  "Empty string",
			input: "",
		},
		{
			name:  "Invalid CIDR",
			input: "10.0.0.1/",
		},
		{
			name:  "Invalid IPv4",
			input: "999.999.999.999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseNetworkAddress(tt.input)
			if err == nil {
				t.Errorf("ParseNetworkAddress(%q) = %q, expected error", tt.input, result)
			}
			if result != "" {
				t.Errorf("ParseNetworkAddress(%q) returned non-empty result on error: %q", tt.input, result)
			}
		})
	}
}

func TestBuildHTTPURL(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		port     int64
		expected string
		wantErr  bool
	}{
		{
			name:     "IPv4 with standard port",
			addr:     "10.0.0.1",
			port:     3000,
			expected: "http://10.0.0.1:3000",
		},
		{
			name:     "IPv4 with port 80",
			addr:     "192.168.1.1",
			port:     80,
			expected: "http://192.168.1.1:80",
		},
		{
			name:     "IPv4 with high port",
			addr:     "172.16.0.1",
			port:     8080,
			expected: "http://172.16.0.1:8080",
		},
		{
			name:     "IPv6 with port",
			addr:     "2001:db8::1",
			port:     3000,
			expected: "http://[2001:db8::1]:3000",
		},
		{
			name:     "Loopback with port",
			addr:     "127.0.0.1",
			port:     5000,
			expected: "http://127.0.0.1:5000",
		},
		{
			name:     "IPv4 CIDR with port",
			addr:     "10.0.0.1/24",
			port:     8080,
			expected: "http://10.0.0.1:8080",
		},
		{
			name:     "IPv6 CIDR with port",
			addr:     "2001:db8::1/64",
			port:     3000,
			expected: "http://[2001:db8::1]:3000",
		},
		{
			name:    "Invalid address",
			addr:    "not-an-ip",
			port:    3000,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildHTTPURL(tt.addr, tt.port)
			if tt.wantErr {
				if err == nil {
					t.Errorf("BuildHTTPURL(%q, %d) expected error, got nil", tt.addr, tt.port)
				}
				return
			}
			if err != nil {
				t.Errorf("BuildHTTPURL(%q, %d) returned error: %v", tt.addr, tt.port, err)
			}
			if result != tt.expected {
				t.Errorf("BuildHTTPURL(%q, %d) = %q, want %q", tt.addr, tt.port, result, tt.expected)
			}
		})
	}
}
