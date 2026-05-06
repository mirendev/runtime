package autotls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	fallbackCertFile = "fallback-cert.pem"
	fallbackKeyFile  = "fallback-key.pem"
	// Regenerate the fallback cert if it expires within this duration
	fallbackCertRenewalWindow = 30 * 24 * time.Hour
)

// ServeTLSSelfSigned serves HTTPS using a self-signed certificate.
// This is intended for development and testing only.
func ServeTLSSelfSigned(ctx context.Context, log *slog.Logger, h http.Handler) error {
	log = log.With("module", "autotls", "mode", "self-signed")
	log.Info("serving TLS with self-signed certificate (dev mode)")

	cert, err := generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	server := &http.Server{
		Addr:              ":443",
		Handler:           h,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("starting HTTPS server with self-signed cert", "addr", ":443")
		err := server.ListenAndServeTLS("", "")
		if err != nil && err != http.ErrServerClosed {
			log.Error("error serving HTTPS", "error", err)
		}
	}()

	// Also start HTTP server on port 80 that redirects to HTTPS
	httpServer := &http.Server{
		Addr: ":80",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if hostWithoutPort, _, err := net.SplitHostPort(host); err == nil {
				host = hostWithoutPort
			}
			target := "https://" + host + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusFound)
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("starting HTTP server for HTTPS redirect", "addr", ":80")
		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Error("error serving HTTP", "error", err)
		}
	}()

	// Monitor for context cancellation
	go func() {
		<-ctx.Done()
		log.Info("shutting down HTTPS and HTTP servers")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTPS server shutdown error", "error", err)
		}
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTP server shutdown error", "error", err)
		}
		log.Info("HTTPS and HTTP servers shutdown complete")
	}()

	return nil
}

// ServeTLSSelfSignedOnAddr serves HTTPS on addr using a self-signed certificate.
// Unlike ServeTLSSelfSigned, it does not also start a port-80 redirect server,
// so it can bind to a non-standard address (for example, behind a TLS-terminating
// proxy that already owns 80/443 on the host).
func ServeTLSSelfSignedOnAddr(ctx context.Context, log *slog.Logger, h http.Handler, addr string) error {
	log = log.With("module", "autotls", "mode", "self-signed", "addr", addr)
	log.Info("serving TLS with self-signed certificate on custom address (dev / behind-proxy mode)")

	cert, err := generateSelfSignedCert()
	if err != nil {
		return fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	server := &http.Server{
		Handler:           h,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		err := server.ServeTLS(ln, "", "")
		if err != nil && err != http.ErrServerClosed {
			log.Error("error serving HTTPS", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		log.Info("shutting down HTTPS server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTPS server shutdown error", "error", err)
		}
	}()

	return nil
}

// generateSelfSignedCert creates an in-memory self-signed certificate
func generateSelfSignedCert() (tls.Certificate, error) {
	cert, _, _, err := generateSelfSignedCertWithPEM()
	return cert, err
}

// loadOrGenerateFallbackCert loads a cached fallback certificate from disk,
// or generates a new one if it doesn't exist or is expiring soon.
// This ensures users who accept the browser warning don't have to re-accept
// on every server restart.
func LoadOrGenerateFallbackCert(certsDir string) (tls.Certificate, error) {
	certPath := filepath.Join(certsDir, fallbackCertFile)
	keyPath := filepath.Join(certsDir, fallbackKeyFile)

	// Try to load existing cert
	cert, err := loadFallbackCert(certPath, keyPath)
	if err == nil {
		// Check if cert is expiring soon
		if !certExpiringSoon(cert) {
			return cert, nil
		}
		// Cert is expiring, fall through to regenerate
	}

	// Generate new cert
	cert, certPEM, keyPEM, err := generateSelfSignedCertWithPEM()
	if err != nil {
		return tls.Certificate{}, err
	}

	// Ensure certs directory exists
	if err := os.MkdirAll(certsDir, 0700); err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create certs directory: %w", err)
	}

	// Save cert and key to disk
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to write fallback cert: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to write fallback key: %w", err)
	}

	return cert, nil
}

// loadFallbackCert loads a certificate from disk.
func loadFallbackCert(certPath, keyPath string) (tls.Certificate, error) {
	return tls.LoadX509KeyPair(certPath, keyPath)
}

// certExpiringSoon checks if a certificate is expiring within the renewal window.
func certExpiringSoon(cert tls.Certificate) bool {
	if len(cert.Certificate) == 0 {
		return true
	}

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return true
	}

	return time.Until(x509Cert.NotAfter) < fallbackCertRenewalWindow
}

// generateSelfSignedCertWithPEM creates a self-signed certificate and returns
// both the tls.Certificate and the PEM-encoded cert and key for persistence.
// Uses ECDSA P-256 for broad browser compatibility.
func generateSelfSignedCertWithPEM() (tls.Certificate, []byte, []byte, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	certTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Miren Dev"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
		DNSNames:              []string{"localhost"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &certTemplate, &certTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	pkbytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkbytes,
	})

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: derBytes,
	})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, nil, nil, fmt.Errorf("failed to create x509 key pair: %w", err)
	}

	return tlsCert, certPEM, keyPEM, nil
}
