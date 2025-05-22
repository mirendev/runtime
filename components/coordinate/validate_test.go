package coordinate

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func createTestCertificate(
	t *testing.T,
	privateKey *rsa.PrivateKey,
	notAfter time.Time,
	dnsNames []string,
	ipAddresses []net.IP,
) *x509.Certificate {
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Test Organization"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"Test City"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     notAfter,
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(certDER)
	require.NoError(t, err)

	return cert
}

func TestValidateAPICertificate(t *testing.T) {
	expectedNames := []string{"localhost", "example.com"}
	expectedIPs := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}

	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	require.NoError(t, err)

	t.Run("valid certificate", func(t *testing.T) {
		cert := createTestCertificate(t, privateKey, time.Now().Add(96*time.Hour), expectedNames, expectedIPs)
		require.NoError(t, validateAPICertificate(cert, expectedNames, expectedIPs))
	})

	t.Run("expired certificate", func(t *testing.T) {
		cert := createTestCertificate(t, privateKey, time.Now().Add(-72*time.Hour), expectedNames, expectedIPs)
		err := validateAPICertificate(cert, expectedNames, expectedIPs)
		require.Error(t, err)
		require.Contains(t, err.Error(), "certificate expired")
	})

	t.Run("expiring soon certificate", func(t *testing.T) {
		cert := createTestCertificate(t, privateKey, time.Now().Add(24*time.Hour), expectedNames, expectedIPs)
		err := validateAPICertificate(cert, expectedNames, expectedIPs)
		require.Error(t, err)
		require.Contains(t, err.Error(), "certificate expired")
	})

	t.Run("mismatched DNS names", func(t *testing.T) {
		cert := createTestCertificate(t, privateKey, time.Now().Add(96*time.Hour), []string{"different.com"}, expectedIPs)
		err := validateAPICertificate(cert, expectedNames, expectedIPs)
		require.Error(t, err)
		require.Contains(t, err.Error(), "DNS names")
		require.Contains(t, err.Error(), "do not match")
	})

	t.Run("mismatched IP addresses", func(t *testing.T) {
		cert := createTestCertificate(t, privateKey, time.Now().Add(96*time.Hour), expectedNames, []net.IP{net.ParseIP("192.168.1.1")})
		err := validateAPICertificate(cert, expectedNames, expectedIPs)
		require.Error(t, err)
		require.Contains(t, err.Error(), "IP addresses")
		require.Contains(t, err.Error(), "do not match")
	})

	t.Run("empty DNS names and IPs", func(t *testing.T) {
		cert := createTestCertificate(t, privateKey, time.Now().Add(96*time.Hour), []string{}, []net.IP{})
		require.NoError(t, validateAPICertificate(cert, []string{}, []net.IP{}))
	})
}
