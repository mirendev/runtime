package caauth

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	r := require.New(t)

	opts := Options{
		CommonName:   "Test CA",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     24 * time.Hour,
	}

	ca, err := New(opts)
	r.NoError(err)

	// Verify CA certificate fields
	r.Equal(opts.CommonName, ca.cert.Subject.CommonName)
	r.True(ca.cert.IsCA, "Certificate should be marked as CA")
	r.Equal(1, ca.cert.MaxPathLen)
}

func TestExportAndLoadPEM(t *testing.T) {
	r := require.New(t)

	// Create a new CA
	ca, err := New(Options{
		CommonName:   "Test CA",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     24 * time.Hour,
	})
	r.NoError(err)

	// Export to PEM
	certPEM, keyPEM, err := ca.ExportPEM()
	r.NoError(err)

	// Verify PEM format
	r.Contains(string(certPEM), "-----BEGIN CERTIFICATE-----")
	r.Contains(string(keyPEM), "-----BEGIN PRIVATE KEY-----")

	// Load from PEM
	loadedCA, err := LoadFromPEM(certPEM, keyPEM)
	r.NoError(err)

	// Verify loaded CA matches original
	r.Equal(ca.cert.Subject.CommonName, loadedCA.cert.Subject.CommonName)
}

func TestIssueCertificate(t *testing.T) {
	r := require.New(t)

	// Create a CA
	ca, err := New(Options{
		CommonName:   "Test CA",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     24 * time.Hour,
	})
	r.NoError(err)

	// Issue a certificate
	certOpts := Options{
		CommonName:   "test.example.com",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     time.Hour,
	}

	ccert, err := ca.IssueCertificate(certOpts)
	r.NoError(err)

	// Decode and verify the issued certificate
	block, _ := pem.Decode(ccert.CertPEM)
	r.NotNil(block, "Should decode certificate PEM")

	cert, err := x509.ParseCertificate(block.Bytes)
	r.NoError(err)

	r.Equal(certOpts.CommonName, cert.Subject.CommonName)
	r.False(cert.IsCA, "Issued certificate should not be a CA")
}

func TestVerifyCertificate(t *testing.T) {
	r := require.New(t)

	// Create a CA
	ca, err := New(Options{
		CommonName:   "Test CA",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     24 * time.Hour,
	})
	r.NoError(err)

	// Issue a valid certificate
	ccert, err := ca.IssueCertificate(Options{
		CommonName:   "test.example.com",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     time.Hour,
	})
	r.NoError(err)

	// Verify the valid certificate
	err = ca.VerifyCertificate(ccert.CertPEM)
	r.NoError(err)

	// Create another CA
	otherCA, err := New(Options{
		CommonName:   "Other CA",
		Organization: "Other Org",
		Country:      "US",
		ValidFor:     24 * time.Hour,
	})
	r.NoError(err)

	// Issue certificate from other CA
	oc, err := otherCA.IssueCertificate(Options{
		CommonName:   "other.example.com",
		Organization: "Other Org",
		Country:      "US",
		ValidFor:     time.Hour,
	})
	r.NoError(err)

	// Verify should fail for certificate from other CA
	err = ca.VerifyCertificate(oc.CertPEM)
	r.Error(err, "Should fail to verify certificate from different CA")
}

func TestGetCACertificate(t *testing.T) {
	r := require.New(t)

	ca, err := New(Options{
		CommonName:   "Test CA",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     24 * time.Hour,
	})
	r.NoError(err)

	certPEM := ca.GetCACertificate()

	// Verify PEM format
	r.Contains(string(certPEM), "-----BEGIN CERTIFICATE-----")

	// Decode and verify the certificate
	block, _ := pem.Decode(certPEM)
	r.NotNil(block, "Should decode CA certificate PEM")

	cert, err := x509.ParseCertificate(block.Bytes)
	r.NoError(err)
	r.Equal("Test CA", cert.Subject.CommonName)
}

func TestClientCertificateMarshalUnmarshal(t *testing.T) {
	r := require.New(t)

	// Create a CA and issue a certificate
	ca, err := New(Options{
		CommonName:   "Test CA",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     24 * time.Hour,
	})
	r.NoError(err)

	originalCert, err := ca.IssueCertificate(Options{
		CommonName:   "test.example.com",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     time.Hour,
	})
	r.NoError(err)

	// Marshal the certificate
	marshaledData, err := originalCert.MarshalText()
	r.NoError(err)
	r.NotEmpty(marshaledData)

	// Unmarshal into a new certificate
	var unmarshaledCert ClientCertificate
	err = unmarshaledCert.UnmarshalText(marshaledData)
	r.NoError(err)

	// Verify the certificates match
	r.Equal(originalCert.CertPEM, unmarshaledCert.CertPEM)
	r.Equal(originalCert.KeyPEM, unmarshaledCert.KeyPEM)

	// Verify the unmarshaled certificate is still valid
	err = ca.VerifyCertificate(unmarshaledCert.CertPEM)
	r.NoError(err)

	// Test unmarshaling invalid data
	err = unmarshaledCert.UnmarshalText([]byte("invalid json"))
	r.Error(err)
}

func TestClientCertificateValidation(t *testing.T) {
	r := require.New(t)

	// Create a CA
	ca, err := New(Options{
		CommonName:   "Test CA",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     24 * time.Hour,
	})
	r.NoError(err)

	// Issue a certificate
	cert, err := ca.IssueCertificate(Options{
		CommonName:   "test.example.com",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     time.Hour,
	})
	r.NoError(err)

	// Verify certificate structure
	certBlock, _ := pem.Decode(cert.CertPEM)
	r.NotNil(certBlock)
	r.Equal("CERTIFICATE", certBlock.Type)

	keyBlock, _ := pem.Decode(cert.KeyPEM)
	r.NotNil(keyBlock)
	r.Equal("PRIVATE KEY", keyBlock.Type)

	// Parse certificate to verify contents
	x509Cert, err := x509.ParseCertificate(certBlock.Bytes)
	r.NoError(err)
	r.Equal("test.example.com", x509Cert.Subject.CommonName)
	r.Equal("Test Org", x509Cert.Subject.Organization[0])
	r.Equal("US", x509Cert.Subject.Country[0])
}

func TestInvalidPEM(t *testing.T) {
	r := require.New(t)

	// Test loading invalid certificate PEM
	_, err := LoadFromPEM([]byte("invalid"), []byte("invalid"))
	r.Error(err, "Should error when loading invalid PEM")

	// Test verifying invalid certificate PEM
	ca, err := New(Options{
		CommonName:   "Test CA",
		Organization: "Test Org",
		Country:      "US",
		ValidFor:     24 * time.Hour,
	})
	r.NoError(err)

	err = ca.VerifyCertificate([]byte("invalid"))
	r.Error(err, "Should error when verifying invalid PEM")
}
