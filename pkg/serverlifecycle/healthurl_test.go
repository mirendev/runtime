package serverlifecycle

import "testing"

func TestHealthURL(t *testing.T) {
	tests := []struct{ mode, addr, want string }{
		{"", "", DefaultHealthURL},
		{"tls-autoprovision", "", DefaultHealthURL},
		{"behind-proxy-http", "", "http://127.0.0.1:80/.well-known/miren/health"},
		{"behind-proxy-http", ":8080", "http://127.0.0.1:8080/.well-known/miren/health"},
		{"behind-proxy-http", "0.0.0.0:8080", "http://127.0.0.1:8080/.well-known/miren/health"},
		{"behind-proxy-https", "[::]:8443", "https://127.0.0.1:8443/.well-known/miren/health"},
		{"behind-proxy-https", "10.0.0.5:8443", "https://10.0.0.5:8443/.well-known/miren/health"},
	}
	for _, tt := range tests {
		if got := HealthURL(tt.mode, tt.addr); got != tt.want {
			t.Errorf("HealthURL(%q, %q) = %q, want %q", tt.mode, tt.addr, got, tt.want)
		}
	}
}
