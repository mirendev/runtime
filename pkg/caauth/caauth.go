package caauth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"regexp"
	"time"
)

// Authority represents a Certificate Authority
type Authority struct {
	cert *x509.Certificate
	key  ed25519.PrivateKey

	certPEM []byte // Cached PEM encoding of the certificate
}

// Options for certificate generation
type Options struct {
	CommonName   string
	Organization string
	Country      string
	ValidFor     time.Duration

	DNSNames []string
	IPs      []net.IP
}

// New creates a new CA with a self-signed certificate
func New(opts Options) (*Authority, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key pair: %w", err)
	}

	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   opts.CommonName,
			Organization: []string{opts.Organization},
			Country:      []string{opts.Country},
		},
		DNSNames:              opts.DNSNames,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(opts.ValidFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, pub, priv)
	if err != nil {
		return nil, fmt.Errorf("creating CA certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing CA certificate: %w", err)
	}

	return &Authority{
		cert: cert,
		key:  priv,
	}, nil
}

func LoadCertificate(certPEM []byte) (*x509.Certificate, error) {
	// Decode certificate
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("failed to decode PEM certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate: %w", err)
	}

	return cert, nil
}

// LoadFromPEM loads an existing CA from PEM-encoded certificate and key
func LoadFromPEM(certPEM, keyPEM []byte) (*Authority, error) {
	// Decode certificate
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("failed to decode PEM certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing certificate: %w", err)
	}

	// Decode private key
	block, _ = pem.Decode(keyPEM)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("failed to decode PEM private key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}

	privKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ed25519")
	}

	return &Authority{
		cert:    cert,
		certPEM: certPEM,
		key:     privKey,
	}, nil
}

func (ca *Authority) Fingerprint() string {
	hash := sha1.Sum(ca.cert.Raw)
	return hex.EncodeToString(hash[:])
}

// ExportPEM exports the CA certificate and private key in PEM format
func (ca *Authority) ExportPEM() (certPEM, keyPEM []byte, err error) {
	// Export certificate
	certPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ca.cert.Raw,
	})

	// Export private key
	keyBytes, err := x509.MarshalPKCS8PrivateKey(ca.key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling private key: %w", err)
	}

	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	})

	return certPEM, keyPEM, nil
}

type ClientCertificate struct {
	CertPEM []byte
	KeyPEM  []byte
	CACert  []byte
}

var (
	dnsName    = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)
	simpleName = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)
)

// validateCommonName validates that the provided name is a valid DNS-1123 name
// or a valid domain name suitable for use as a certificate CommonName.
func validateCommonName(name string) error {
	// For domain names like "test.example.com", we allow dots
	if dnsName.MatchString(name) {
		return nil // Valid domain name
	}

	// For simple names, RFC 1123 compliant hostname pattern
	// - Must start with a letter or number
	// - Can contain letters, numbers, and hyphens
	// - Cannot end with a hyphen
	// - Max length is 63 characters
	if !simpleName.MatchString(name) {
		return fmt.Errorf("invalid name format: %q does not conform to DNS-1123 naming conventions", name)
	}

	// List of reserved names that should not be used as certificate names
	reservedNames := map[string]bool{
		"admin":       true,
		"root":        true,
		"system":      true,
		"kubernetes":  true,
		"kube-system": true,
		"runtime":     true,
	}

	if reservedNames[name] {
		return fmt.Errorf("name %q is reserved and cannot be used", name)
	}

	return nil
}

// IssueCertificate creates a new certificate signed by the CA
func (ca *Authority) IssueCertificate(opts Options) (*ClientCertificate, error) {
	// Validate the common name before proceeding
	if err := validateCommonName(opts.CommonName); err != nil {
		return nil, err
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating key pair: %w", err)
	}

	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   opts.CommonName,
			Organization: []string{opts.Organization},
			Country:      []string{opts.Country},
		},
		DNSNames:              opts.DNSNames,
		IPAddresses:           opts.IPs,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(opts.ValidFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, ca.cert, pub, ca.key)
	if err != nil {
		return nil, fmt.Errorf("creating certificate: %w", err)
	}

	// Export certificate and include CA cert so that it's advertised as a chain
	var buf bytes.Buffer
	pem.Encode(&buf, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})
	buf.Write(ca.certPEM)

	certPEM := buf.Bytes()

	// Export private key
	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshaling private key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	})

	return &ClientCertificate{
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
		CACert:  ca.GetCACertificate(),
	}, nil
}

// VerifyCertificate verifies if a certificate was signed by this CA
func (ca *Authority) VerifyCertificate(certPEM []byte) error {
	// Decode certificate
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return fmt.Errorf("failed to decode PEM certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parsing certificate: %w", err)
	}

	// Create verification pool with CA cert
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)

	// Verify the certificate
	_, err = cert.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: time.Now(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	})

	if err != nil {
		return fmt.Errorf("certificate verification failed: %w", err)
	}

	return nil
}

// GetCACertificate returns the CA certificate in PEM format
func (ca *Authority) GetCACertificate() []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ca.cert.Raw,
	})
}

// Helper function to generate a random serial number
func generateSerialNumber() (*big.Int, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("generating serial number: %w", err)
	}
	return serialNumber, nil
}

var _ encoding.TextMarshaler = &ClientCertificate{}
var _ encoding.TextUnmarshaler = &ClientCertificate{}

type marshaledCert struct {
	Cert []byte `json:"cert"`
	Key  []byte `json:"key"`
}

func (c *ClientCertificate) MarshalText() ([]byte, error) {
	cert, _ := pem.Decode(c.CertPEM)
	key, _ := pem.Decode(c.KeyPEM)

	return json.Marshal(marshaledCert{
		Cert: cert.Bytes,
		Key:  key.Bytes,
	})
}

func (cert *ClientCertificate) UnmarshalText(data []byte) error {
	var mc marshaledCert

	err := json.Unmarshal(data, &mc)
	if err != nil {
		return err
	}

	cert.CertPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: mc.Cert,
	})

	cert.KeyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: mc.Key,
	})

	return nil
}
