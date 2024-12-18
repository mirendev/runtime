package rpc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"net"
	"time"
)

// generateSelfSignedCert creates an in-memory self-signed certificate
// and returns a tls.Certificate ready to be used in a server.
func generateSelfSignedCert() (tls.Certificate, error) {
	// Generate a private key.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate private key: %v", err)
	}

	// Create a random serial number.
	serialNumber, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate serial number: %v", err)
	}

	// Create a certificate template.
	// Adjust fields (Subject, NotAfter, etc.) as needed.
	certTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Example Org"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour), // valid for 24 hours
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	// Self-sign the certificate using the same private key for both
	// the “parent” and the “template”.
	derBytes, err := x509.CreateCertificate(
		rand.Reader,
		&certTemplate,
		&certTemplate,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create certificate: %v", err)
	}

	// Encode the private key into PEM format.
	privateKeyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
		},
	)

	// Encode the certificate into PEM format.
	certPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: derBytes,
		},
	)

	// Create the tls.Certificate from the PEM-encoded key pair.
	tlsCert, err := tls.X509KeyPair(certPEM, privateKeyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create x509 key pair: %v", err)
	}

	return tlsCert, nil
}
