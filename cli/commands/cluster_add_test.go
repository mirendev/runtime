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
