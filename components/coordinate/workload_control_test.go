package coordinate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestACMEClusterHostnames(t *testing.T) {
	tests := []struct {
		name       string
		cloud      string
		additional []string
		want       []string
	}{
		{name: "none"},
		{
			name:  "cloud hostname only",
			cloud: "cluster-abc.miren.systems",
			want:  []string{"cluster-abc.miren.systems"},
		},
		{
			// The v0.16 regression: these were left out, so a server named
			// by --dns-names needed a vouch to get a cert for its own name.
			name:       "dns-names only",
			additional: []string{"srv.example.com"},
			want:       []string{"srv.example.com"},
		},
		{
			name:       "both, cloud first",
			cloud:      "cluster-abc.miren.systems",
			additional: []string{"srv.example.com", "api.example.com"},
			want:       []string{"cluster-abc.miren.systems", "srv.example.com", "api.example.com"},
		},
		{
			name:       "normalized and deduped",
			cloud:      "Cluster.Miren.Systems",
			additional: []string{" cluster.miren.systems ", "SRV.example.com.", "srv.example.com"},
			want:       []string{"cluster.miren.systems", "srv.example.com"},
		},
		{
			name: "names ACME cannot issue for are dropped",
			additional: []string{
				"", "localhost", "api.localhost", "miren-server",
				"*.example.com", "10.0.0.5", "::1", "2001:db8::1",
				"nas.local", "api.corp.internal", "box.lan", "router.home.arpa",
				"svc.test", "host.example", "x.invalid",
				"ok.example.com",
			},
			want: []string{"ok.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, acmeClusterHostnames(tt.cloud, tt.additional))
		})
	}
}

func TestSelfIssuerHostnames(t *testing.T) {
	tests := []struct {
		name       string
		issuer     string
		additional []string
		want       []string
	}{
		{
			name:       "issuer on a dns-names entry",
			issuer:     "https://srv.example.com",
			additional: []string{"SRV.example.com.", "internal.example.com"},
			want:       []string{"srv.example.com"},
		},
		{
			name:       "cloud-anchored issuer is not ours",
			issuer:     "https://id.cloud.example.net/clusters/abc",
			additional: []string{"srv.example.com"},
		},
		{
			name:       "cluster-local issuer",
			issuer:     "https://cluster.local",
			additional: []string{"srv.example.com"},
		},
		{name: "no issuer", additional: []string{"srv.example.com"}},
		{name: "not https", issuer: "http://srv.example.com", additional: []string{"srv.example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, selfIssuerHostnames(tt.issuer, tt.additional))
		})
	}
}
