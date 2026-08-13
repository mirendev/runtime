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
