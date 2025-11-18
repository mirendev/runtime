package certificate

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns"
	"github.com/go-acme/lego/v4/registration"
	"miren.dev/runtime/api/ingress/ingress_v1alpha"
	"miren.dev/runtime/pkg/entity"
)

// Controller provisions and manages TLS certificates for http_route entities using DNS-01 ACME challenges
type Controller struct {
	Log         *slog.Logger
	dataPath    string
	email       string
	dnsProvider string

	client     *lego.Client
	user       *legoUser
	initOnce   sync.Once
	initErr    error
	certCache  map[string]*cachedCert
	cacheMutex sync.RWMutex
}

type cachedCert struct {
	cert      *tls.Certificate
	notAfter  time.Time
	notBefore time.Time
}

// legoUser implements the acme.User interface required by lego
type legoUser struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

func (u *legoUser) GetEmail() string {
	return u.Email
}

func (u *legoUser) GetRegistration() *registration.Resource {
	return u.Registration
}

func (u *legoUser) GetPrivateKey() crypto.PrivateKey {
	return u.key
}

// NewController creates a new certificate controller
func NewController(log *slog.Logger, dataPath string, email string, dnsProvider string) *Controller {
	return &Controller{
		Log:         log.With("module", "certificate-controller"),
		dataPath:    dataPath,
		email:       email,
		dnsProvider: dnsProvider,
		certCache:   make(map[string]*cachedCert),
	}
}

// Init implements ReconcileControllerI - called once at startup
func (c *Controller) Init(ctx context.Context) error {
	c.initOnce.Do(func() {
		c.initErr = c.initializeLego(ctx)
	})
	return c.initErr
}

func (c *Controller) initializeLego(ctx context.Context) error {
	c.Log.Info("initializing ACME DNS challenge client", "provider", c.dnsProvider)

	if c.email == "" {
		c.Log.Warn("ACME email not configured - account recovery and notifications will not be available")
	}

	certsDir := filepath.Join(c.dataPath, "certs")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	// Load or create account
	user, err := c.loadOrCreateAccount(certsDir)
	if err != nil {
		return fmt.Errorf("failed to load/create ACME account: %w", err)
	}
	c.user = user

	// Create lego client
	config := lego.NewConfig(user)
	config.Certificate.KeyType = certcrypto.RSA2048

	client, err := lego.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create lego client: %w", err)
	}

	// Set up DNS provider
	provider, err := dns.NewDNSChallengeProviderByName(c.dnsProvider)
	if err != nil {
		return fmt.Errorf("failed to create DNS provider %q: %w", c.dnsProvider, err)
	}

	if err := client.Challenge.SetDNS01Provider(provider); err != nil {
		return fmt.Errorf("failed to set DNS provider: %w", err)
	}

	// Register account if needed
	if user.Registration == nil {
		c.Log.Info("registering new ACME account", "email", user.Email)
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return fmt.Errorf("failed to register ACME account: %w", err)
		}
		user.Registration = reg
		if err := c.saveAccount(certsDir, user); err != nil {
			c.Log.Error("failed to save account", "error", err)
		}
	}

	c.client = client

	// Load existing certificates from disk into cache
	if err := c.loadExistingCerts(certsDir); err != nil {
		c.Log.Warn("failed to load existing certificates", "error", err)
		// Don't fail initialization if we can't load existing certs
	}

	c.Log.Info("ACME DNS challenge client initialized")
	return nil
}

// loadExistingCerts scans the certs directory and loads all existing certificates into cache
func (c *Controller) loadExistingCerts(certsDir string) error {
	entries, err := os.ReadDir(certsDir)
	if err != nil {
		return fmt.Errorf("failed to read certs directory: %w", err)
	}

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".crt") {
			continue
		}

		// Extract domain from filename (e.g., "example.com.crt" -> "example.com")
		domain := strings.TrimSuffix(entry.Name(), ".crt")
		certFile := filepath.Join(certsDir, entry.Name())
		keyFile := filepath.Join(certsDir, domain+".key")

		// Check if key file exists
		if _, err := os.Stat(keyFile); err != nil {
			c.Log.Debug("skipping cert without key file", "domain", domain)
			continue
		}

		// Load cert into cache
		certPEM, err := os.ReadFile(certFile)
		if err != nil {
			c.Log.Warn("failed to read certificate file", "domain", domain, "error", err)
			continue
		}

		keyPEM, err := os.ReadFile(keyFile)
		if err != nil {
			c.Log.Warn("failed to read key file", "domain", domain, "error", err)
			continue
		}

		if _, err := c.loadCertIntoCache(domain, certPEM, keyPEM); err != nil {
			c.Log.Warn("failed to load certificate into cache", "domain", domain, "error", err)
			continue
		}

		loaded++
		c.Log.Debug("loaded existing certificate", "domain", domain)
	}

	if loaded > 0 {
		c.Log.Info("loaded existing certificates from disk", "count", loaded)
	}

	return nil
}

// Reconcile implements ReconcileControllerI - called for each http_route add/update and periodic resyncs
func (c *Controller) Reconcile(ctx context.Context, route *ingress_v1alpha.HttpRoute, meta *entity.Meta) error {
	domain := strings.TrimSpace(route.Host)
	if domain == "" {
		c.Log.Warn("http_route has empty host, skipping certificate provisioning", "route", meta.Id)
		return nil
	}
	log := c.Log.With("domain", domain, "route", route.ID)

	// Check if cert exists in cache and is valid
	if cert := c.getCachedCert(domain); cert != nil {
		timeUntilExpiry := time.Until(cert.notAfter)
		if timeUntilExpiry > 30*24*time.Hour {
			log.Debug("certificate valid", "expires", cert.notAfter, "remaining", timeUntilExpiry)
			return nil
		}
		log.Info("certificate needs renewal", "expires", cert.notAfter, "remaining", timeUntilExpiry)
	}

	// Provision or renew certificate
	log.Info("provisioning certificate via DNS challenge")
	if err := c.provisionCertificate(ctx, domain); err != nil {
		return fmt.Errorf("failed to provision certificate for %s: %w", domain, err)
	}

	log.Info("certificate provisioned successfully")
	return nil
}

// Delete handles http_route deletion - we keep the cert in cache/disk for potential reuse
func (c *Controller) Delete(ctx context.Context, id entity.Id) error {
	c.Log.Debug("http_route deleted, keeping certificate in cache", "route", id)
	return nil
}

func (c *Controller) provisionCertificate(ctx context.Context, domain string) error {
	certsDir := filepath.Join(c.dataPath, "certs")
	certFile := filepath.Join(certsDir, domain+".crt")
	keyFile := filepath.Join(certsDir, domain+".key")

	// Try to load from disk first (might have been provisioned before cache was loaded)
	if _, err := os.Stat(certFile); err == nil {
		certPEM, err := os.ReadFile(certFile)
		if err == nil {
			keyPEM, err := os.ReadFile(keyFile)
			if err == nil {
				if cert, err := c.loadCertIntoCache(domain, certPEM, keyPEM); err == nil {
					// Check if it's still valid
					if time.Until(cert.notAfter) > 30*24*time.Hour {
						return nil
					}
				}
			}
		}
	}

	// Obtain new certificate
	request := certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	}

	cert, err := c.client.Certificate.Obtain(request)
	if err != nil {
		return fmt.Errorf("failed to obtain certificate: %w", err)
	}

	// Save to disk
	if err := os.WriteFile(certFile, cert.Certificate, 0644); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	if err := os.WriteFile(keyFile, cert.PrivateKey, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Load into cache
	_, err = c.loadCertIntoCache(domain, cert.Certificate, cert.PrivateKey)
	return err
}

func (c *Controller) loadCertIntoCache(domain string, certPEM, keyPEM []byte) (*cachedCert, error) {
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Parse to get expiry info
	if len(tlsCert.Certificate) == 0 {
		return nil, fmt.Errorf("certificate has no data")
	}

	x509Cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse x509 certificate: %w", err)
	}

	cached := &cachedCert{
		cert:      &tlsCert,
		notAfter:  x509Cert.NotAfter,
		notBefore: x509Cert.NotBefore,
	}

	c.cacheMutex.Lock()
	c.certCache[domain] = cached
	c.cacheMutex.Unlock()

	return cached, nil
}

func (c *Controller) getCachedCert(domain string) *cachedCert {
	c.cacheMutex.RLock()
	defer c.cacheMutex.RUnlock()
	return c.certCache[domain]
}

// GetCertificate implements tls.Config.GetCertificate - returns cached certs for TLS handshakes
func (c *Controller) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	domain := hello.ServerName
	if domain == "" {
		return nil, fmt.Errorf("no SNI provided")
	}

	cached := c.getCachedCert(domain)
	if cached == nil {
		return nil, fmt.Errorf("no certificate available for %s", domain)
	}

	// Check if expired
	if time.Now().After(cached.notAfter) {
		return nil, fmt.Errorf("certificate for %s has expired", domain)
	}

	return cached.cert, nil
}

func (c *Controller) loadOrCreateAccount(certsDir string) (*legoUser, error) {
	accountFile := filepath.Join(certsDir, "account.json")
	keyFile := filepath.Join(certsDir, "account.key")

	// Try to load existing account
	if _, err := os.Stat(accountFile); err == nil {
		data, err := os.ReadFile(accountFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read account file: %w", err)
		}

		var user legoUser
		if err := json.Unmarshal(data, &user); err != nil {
			return nil, fmt.Errorf("failed to parse account file: %w", err)
		}

		// Load private key
		keyData, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read account key: %w", err)
		}

		block, _ := pem.Decode(keyData)
		if block == nil {
			return nil, fmt.Errorf("failed to decode PEM block from account key")
		}

		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}

		user.key = key
		c.Log.Info("loaded existing ACME account", "email", user.Email)
		return &user, nil
	}

	// Create new account
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	user := &legoUser{
		Email: c.email,
		key:   privateKey,
	}

	if err := c.saveAccount(certsDir, user); err != nil {
		return nil, err
	}

	c.Log.Info("created new ACME account", "email", user.Email)
	return user, nil
}

func (c *Controller) saveAccount(certsDir string, user *legoUser) error {
	accountFile := filepath.Join(certsDir, "account.json")
	keyFile := filepath.Join(certsDir, "account.key")

	// Save account info
	data, err := json.MarshalIndent(user, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	if err := os.WriteFile(accountFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write account file: %w", err)
	}

	// Save private key
	keyBytes, err := x509.MarshalECPrivateKey(user.key.(*ecdsa.PrivateKey))
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	pemBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}

	if err := os.WriteFile(keyFile, pem.EncodeToMemory(pemBlock), 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	return nil
}
