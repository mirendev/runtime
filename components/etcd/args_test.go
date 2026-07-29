package etcd

import (
	"slices"
	"testing"
)

// flagValue returns the value following the named flag, or "" if the flag isn't
// present or has no value after it.
func flagValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i == -1 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

// TestEtcdArgsListeners pins down which etcd listeners are reachable off-box.
//
// Only the gRPC client port may bind to all interfaces, and only when TLS and
// --client-cert-auth are covering it. The JSON gateway shares the same keyspace
// but is not covered by --client-cert-auth, so exposing it bypasses the mTLS
// that distributed runners set up (MIR-1481); the peer port carries plaintext
// raft. Both stay on loopback.
func TestEtcdArgsListeners(t *testing.T) {
	baseConfig := EtcdConfig{
		Name:           "miren-etcd",
		DataDir:        etcdDataDir,
		ClientPort:     defaultEtcdPort,
		HTTPClientPort: defaultEtcdHTTPPort,
		PeerPort:       defaultPeerPort,
		ClusterState:   "new",
	}

	tests := []struct {
		name           string
		tls            *TLSConfig
		wantClientURL  string
		wantClientAuth bool
	}{
		{
			name:           "plaintext",
			tls:            nil,
			wantClientURL:  "http://127.0.0.1:12379",
			wantClientAuth: false,
		},
		{
			name:           "mtls",
			tls:            &TLSConfig{CertsDir: "/var/lib/miren/etcd-certs"},
			wantClientURL:  "https://0.0.0.0:12379",
			wantClientAuth: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := baseConfig
			config.TLS = tt.tls

			args := etcdArgs(config, computeTuning(8*gib, 0))

			if got := flagValue(args, "--listen-client-urls"); got != tt.wantClientURL {
				t.Errorf("--listen-client-urls = %q, want %q", got, tt.wantClientURL)
			}

			// These two must never leave the host, in either TLS mode.
			if got := flagValue(args, "--listen-client-http-urls"); got != "http://127.0.0.1:12381" {
				t.Errorf("--listen-client-http-urls = %q, want loopback: the JSON gateway serves the same keyspace and --client-cert-auth does not apply to it", got)
			}
			if got := flagValue(args, "--listen-peer-urls"); got != "http://127.0.0.1:12380" {
				t.Errorf("--listen-peer-urls = %q, want loopback: raft peer traffic is plaintext", got)
			}

			if got := slices.Contains(args, "--client-cert-auth"); got != tt.wantClientAuth {
				t.Errorf("--client-cert-auth present = %v, want %v", got, tt.wantClientAuth)
			}

			// Nothing in Miren speaks the JSON API, so it stays off entirely
			// rather than merely being confined to loopback.
			if !slices.Contains(args, "--enable-grpc-gateway=false") {
				t.Error("--enable-grpc-gateway=false missing: the JSON gateway should be disabled, not just loopback-bound")
			}
		})
	}
}

// TestEtcdArgsTLSFlags checks that TLS material is wired to the paths the certs
// directory is mounted at in createContainer.
func TestEtcdArgsTLSFlags(t *testing.T) {
	config := EtcdConfig{
		Name:           "miren-etcd",
		DataDir:        etcdDataDir,
		ClientPort:     defaultEtcdPort,
		HTTPClientPort: defaultEtcdHTTPPort,
		PeerPort:       defaultPeerPort,
		ClusterState:   "new",
		TLS:            &TLSConfig{CertsDir: "/var/lib/miren/etcd-certs"},
	}

	args := etcdArgs(config, computeTuning(8*gib, 0))

	for flag, want := range map[string]string{
		"--cert-file":       "/certs/server.crt",
		"--key-file":        "/certs/server.key",
		"--trusted-ca-file": "/certs/ca.crt",
	} {
		if got := flagValue(args, flag); got != want {
			t.Errorf("%s = %q, want %q", flag, got, want)
		}
	}
}

// TestEtcdArgsInitialToken covers the optional flag so the append order in
// etcdArgs can't silently drop it.
func TestEtcdArgsInitialToken(t *testing.T) {
	config := EtcdConfig{
		Name:           "miren-etcd",
		DataDir:        etcdDataDir,
		ClientPort:     defaultEtcdPort,
		HTTPClientPort: defaultEtcdHTTPPort,
		PeerPort:       defaultPeerPort,
		ClusterState:   "new",
	}

	if got := flagValue(etcdArgs(config, computeTuning(8*gib, 0)), "--initial-cluster-token"); got != "" {
		t.Errorf("--initial-cluster-token = %q, want it absent when unset", got)
	}

	config.InitialToken = "miren-token"
	if got := flagValue(etcdArgs(config, computeTuning(8*gib, 0)), "--initial-cluster-token"); got != "miren-token" {
		t.Errorf("--initial-cluster-token = %q, want %q", got, "miren-token")
	}
}
