package rpc

import "testing"

func TestWSRemoteURL(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		// A bare authority means the peer is one of ours, so it gets our route.
		{"localhost:8443", "wss://localhost:8443" + wsMessagePath},
		{"[::1]:8443", "wss://[::1]:8443" + wsMessagePath},

		// A path names somebody else's endpoint and is taken verbatim.
		{"api.miren.cloud/api/v1/clusters/cluster-abc/rpc", "wss://api.miren.cloud/api/v1/clusters/cluster-abc/rpc"},
		{"localhost:3001/relay", "wss://localhost:3001/relay"},

		// A trailing slash names no path, so it is not one.
		{"localhost:8443/", "wss://localhost:8443" + wsMessagePath},

		// The scheme decides transport security rather than being decoration.
		{"wss://localhost:8443", "wss://localhost:8443" + wsMessagePath},
		{"ws://localhost:3001/relay", "ws://localhost:3001/relay"},
		{"ws://localhost:3001", "ws://localhost:3001" + wsMessagePath},
	}

	for _, tt := range tests {
		if got := wsRemoteURL(tt.remote); got != tt.want {
			t.Errorf("wsRemoteURL(%q) = %q, want %q", tt.remote, got, tt.want)
		}
	}
}

// The handshake bearer must never cross an unencrypted hop to somewhere else.
// Plaintext exists for a development endpoint on this machine; anywhere else it
// would put a reusable credential on the wire, and nothing about the connection
// would look wrong afterwards.
func TestBearerTravelsSafely(t *testing.T) {
	safe := []string{
		"wss://api.miren.cloud/api/v1/clusters/c/rpc",
		"wss://anything.example",
		"ws://localhost:3001/relay",
		"ws://LocalHost:3001/relay",
		"ws://127.0.0.1:18080/relay",
		"ws://[::1]:18080/relay",
	}
	for _, url := range safe {
		if !bearerTravelsSafely(url) {
			t.Errorf("%q should carry a bearer", url)
		}
	}

	unsafe := []string{
		"ws://relay.example/relay",
		"ws://miren.host:3001/relay",
		"ws://10.0.0.1:8080/relay",
	}
	for _, url := range unsafe {
		if bearerTravelsSafely(url) {
			t.Errorf("%q must not carry a bearer", url)
		}
	}
}
