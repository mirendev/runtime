package commands

import (
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/caauth"
)

// apiCert issues a certificate shaped like the one a coordinator serves for its
// API, so the SAN set under test is the real one.
func apiCert(t *testing.T, dnsNames []string, ips []net.IP) *x509.Certificate {
	t.Helper()

	ca, err := caauth.New(caauth.Options{
		CommonName:   "miren-test-ca",
		Organization: "miren",
		ValidFor:     time.Hour,
	})
	require.NoError(t, err)

	cc, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "miren-api",
		Organization: "miren",
		ValidFor:     time.Hour,
		DNSNames:     dnsNames,
		IPs:          ips,
	})
	require.NoError(t, err)

	cert, err := caauth.LoadCertificate(cc.CertPEM)
	require.NoError(t, err)

	return cert
}

func TestVerificationName(t *testing.T) {
	// What a coordinator issues by default: loopback, the stable in-cluster
	// name, and every address it discovered.
	standard := func(t *testing.T) *x509.Certificate {
		return apiCert(t,
			[]string{"localhost", clientconfig.APIServerName},
			[]net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("100.64.0.10")},
		)
	}

	t.Run("an advertised IP needs no override", func(t *testing.T) {
		r := require.New(t)
		name, verifiable := verificationName("100.64.0.10", standard(t))
		r.True(verifiable)
		r.Empty(name)
	})

	t.Run("a hostname borrows the in-cluster name", func(t *testing.T) {
		r := require.New(t)
		name, verifiable := verificationName("homelab", standard(t))
		r.True(verifiable)
		r.Equal(clientconfig.APIServerName, name)
	})

	t.Run("an IP the server doesn't know about borrows it too", func(t *testing.T) {
		// A host behind static NAT: reachable on an address that was never in
		// its own interface list, so never a SAN.
		r := require.New(t)
		name, verifiable := verificationName("203.0.113.7", standard(t))
		r.True(verifiable)
		r.Equal(clientconfig.APIServerName, name)
	})

	t.Run("a name the certificate actually covers is used directly", func(t *testing.T) {
		r := require.New(t)
		cert := apiCert(t,
			[]string{"localhost", clientconfig.APIServerName, "homelab.example.com"},
			[]net.IP{net.ParseIP("127.0.0.1")},
		)
		name, verifiable := verificationName("homelab.example.com", cert)
		r.True(verifiable)
		r.Empty(name)
	})

	t.Run("nothing to fall back on is reported as unverifiable", func(t *testing.T) {
		// A cluster old enough to predate the api.miren SAN, reached by a name.
		r := require.New(t)
		cert := apiCert(t, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
		name, verifiable := verificationName("homelab", cert)
		r.False(verifiable)
		r.Empty(name)
	})

	t.Run("no certificate is not a verification problem", func(t *testing.T) {
		r := require.New(t)
		name, verifiable := verificationName("homelab", nil)
		r.True(verifiable)
		r.Empty(name)
	})
}

func TestNormalizeAddress(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		addr    string
		sniHost string
	}{
		{"hostname gets the default port", "homelab", "homelab:8443", "homelab"},
		{"hostname with port is left alone", "homelab:9443", "homelab:9443", "homelab"},
		{"scheme is stripped", "https://homelab:8443", "homelab:8443", "homelab"},
		{"ipv4 gets the default port", "100.64.0.10", "100.64.0.10:8443", "100.64.0.10"},
		{"ipv4 with port is left alone", "100.64.0.10:8443", "100.64.0.10:8443", "100.64.0.10"},
		{"bare ipv6 is bracketed", "fd7a:115c:a1e0::1", "[fd7a:115c:a1e0::1]:8443", "fd7a:115c:a1e0::1"},
		{"bracketed ipv6 gets the default port", "[fd7a:115c:a1e0::1]", "[fd7a:115c:a1e0::1]:8443", "fd7a:115c:a1e0::1"},
		{"bracketed ipv6 with port is left alone", "[fd7a:115c:a1e0::1]:8443", "[fd7a:115c:a1e0::1]:8443", "fd7a:115c:a1e0::1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			addr, sniHost, err := normalizeAddress(tc.in)
			r.NoError(err)
			r.Equal(tc.addr, addr)
			r.Equal(tc.sniHost, sniHost)
		})
	}
}

// The cluster list an account with two organizations gets back, including the
// name collision that makes --organization necessary.
func namedClusters() []ClusterResponse {
	return []ClusterResponse{
		{Name: "prod", XID: "cluster-acme-prod", OrganizationName: "Acme", OrganizationXID: "org-acme"},
		{Name: "prod", XID: "cluster-globex-prod", OrganizationName: "Globex", OrganizationXID: "org-globex"},
		{Name: "staging", XID: "cluster-acme-staging", OrganizationName: "Acme", OrganizationXID: "org-acme"},
	}
}

func TestFindClusterByName(t *testing.T) {
	tests := []struct {
		name         string
		cluster      string
		organization string
		wantXID      string
		wantErr      []string
	}{
		{
			name:    "exact match",
			cluster: "staging",
			wantXID: "cluster-acme-staging",
		},
		{
			// Convenience for anyone typing a name back from a listing, but
			// only once nothing matched exactly.
			name:    "case insensitive match",
			cluster: "STAGING",
			wantXID: "cluster-acme-staging",
		},
		{
			name:    "no such cluster names the ones that exist",
			cluster: "nope",
			wantErr: []string{`no cluster named "nope"`, "prod (Acme)", "prod (Globex)", "staging (Acme)"},
		},
		{
			// Guessing here would bind the wrong production cluster under the
			// right local name.
			name:    "ambiguous name is refused",
			cluster: "prod",
			wantErr: []string{`2 clusters are named "prod"`, "prod (Acme)", "prod (Globex)", "--organization"},
		},
		{
			name:         "organization resolves the ambiguity",
			cluster:      "prod",
			organization: "Globex",
			wantXID:      "cluster-globex-prod",
		},
		{
			name:         "organization matches case insensitively",
			cluster:      "prod",
			organization: "acme",
			wantXID:      "cluster-acme-prod",
		},
		{
			// The XID is what a script has on hand, and it is unambiguous in a
			// way the display name is not.
			name:         "organization matches by xid",
			cluster:      "prod",
			organization: "org-globex",
			wantXID:      "cluster-globex-prod",
		},
		{
			name:         "unknown organization names the real ones",
			cluster:      "prod",
			organization: "Initech",
			wantErr:      []string{`no clusters in organization "Initech"`, "Acme, Globex"},
		},
		{
			name:         "organization narrows before the name is matched",
			cluster:      "staging",
			organization: "Globex",
			wantErr:      []string{`no cluster named "staging"`, "prod (Globex)"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			found, err := findClusterByName(namedClusters(), test.cluster, test.organization)

			if len(test.wantErr) > 0 {
				r.Error(err)
				for _, want := range test.wantErr {
					r.Contains(err.Error(), want)
				}
				r.Nil(found)
				return
			}

			r.NoError(err)
			r.Equal(test.wantXID, found.XID)
		})
	}
}

// A name that matches exactly must win over one that only matches case
// insensitively, or two clusters differing in case would read as ambiguous.
func TestFindClusterByNamePrefersTheExactMatch(t *testing.T) {
	clusters := []ClusterResponse{
		{Name: "Prod", XID: "cluster-upper", OrganizationName: "Acme"},
		{Name: "prod", XID: "cluster-lower", OrganizationName: "Acme"},
	}

	found, err := findClusterByName(clusters, "prod", "")
	require.NoError(t, err)
	require.Equal(t, "cluster-lower", found.XID)
}

// The flag combinations that are refused before anything is fetched or dialed,
// which is what makes them testable without a cloud or a cluster.
func TestClusterAddFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		opts    addClusterOptions
		wantErr string
	}{
		{
			name:    "as without cluster",
			opts:    addClusterOptions{localName: "local-prod"},
			wantErr: "--as needs --cluster",
		},
		{
			name:    "as with address",
			opts:    addClusterOptions{clusterName: "prod", address: "10.0.0.1:8443", localName: "local-prod"},
			wantErr: "--as and --address are mutually exclusive",
		},
		{
			name:    "organization with address",
			opts:    addClusterOptions{clusterName: "prod", address: "10.0.0.1:8443", organization: "Acme"},
			wantErr: "--organization and --address are mutually exclusive",
		},
		{
			name:    "organization without cluster",
			opts:    addClusterOptions{organization: "Acme"},
			wantErr: "--organization only narrows the lookup for --cluster",
		},
		{
			name:    "via-cloud with address",
			opts:    addClusterOptions{clusterName: "prod", address: "10.0.0.1:8443", viaCloud: true},
			wantErr: "--via-cloud and --address are mutually exclusive",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := addCluster(presenceContext(t), test.opts)
			require.Error(t, err)
			require.Contains(t, err.Error(), test.wantErr)
		})
	}
}
