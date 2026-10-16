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
	"errors"
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
	"miren.dev/runtime/pkg/clusterstate"
	"miren.dev/runtime/pkg/entity"
)

// Controller provisions and manages TLS certificates for http_route entities using DNS-01 ACME challenges
type Controller struct {
	Log              *slog.Logger
	dataPath         string
	email            string
	dnsProvider      string
	clusterHostnames []string

	client     *lego.Client
	user       *legoUser
	initOnce   sync.Once
	initErr    error
	certCache  map[string]*cachedCert
	certMisses map[string]time.Time
	cacheMutex sync.RWMutex
	failures   sync.Map // domain -> time.Time
	shared     *clusterstate.Store
}

func (c *Controller) SetSharedStore(store *clusterstate.Store) { c.shared = store }

const dnsFailureCooldown = 5 * time.Minute

type acmeObtainError struct {
	err error
}

func (e *acmeObtainError) Error() string {
	return fmt.Sprintf("failed to obtain certificate: %v", e.err)
}

func (e *acmeObtainError) Unwrap() error {
	return e.err
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
func NewController(log *slog.Logger, dataPath string, email string, dnsProvider string, clusterHostnames []string) *Controller {
	return &Controller{
		Log:              log.With("module", "certificate-controller"),
		dataPath:         dataPath,
		email:            email,
		dnsProvider:      dnsProvider,
		clusterHostnames: clusterHostnames,
		certCache:        make(map[string]*cachedCert),
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
	var user *legoUser
	var err error
	if c.shared != nil {
		user, err = c.ensureSharedAccount(ctx, certsDir)
	} else {
		user, err = c.loadOrCreateAccount(certsDir)
	}
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
	var accountLock *clusterstate.Lock
	if user.Registration == nil {
		if c.shared != nil {
			accountLock, err = c.shared.Lock(ctx, "acme/account")
			if err != nil {
				return err
			}
			defer accountLock.Unlock()
			data, getErr := c.shared.Get(ctx, "acme/dns-account")
			if getErr == nil {
				current, decodeErr := decodeAccount(data)
				if decodeErr != nil {
					return decodeErr
				}
				user.Registration = current.Registration
			}
		}
	}
	if user.Registration == nil {
		c.Log.Info("registering new ACME account", "email", user.Email)
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return fmt.Errorf("failed to register ACME account: %w", err)
		}
		user.Registration = reg
		if accountLock != nil {
			data, encodeErr := encodeAccount(user)
			if encodeErr != nil {
				return encodeErr
			}
			if err := accountLock.Put(ctx, "acme/dns-account", data); err != nil {
				return err
			}
		}
		if err := c.saveAccount(certsDir, user); err != nil {
			c.Log.Error("failed to save account", "error", err)
		}
	}

	c.client = client

	// Load existing certificates from disk into cache
	if err := c.loadExistingCerts(ctx, certsDir); err != nil {
		if c.shared != nil {
			return fmt.Errorf("importing existing certificates: %w", err)
		}
		c.Log.Warn("failed to load existing certificates", "error", err)
	}

	if len(c.clusterHostnames) > 0 {
		go c.provisionClusterHostnames(ctx)
	}

	c.Log.Info("ACME DNS challenge client initialized")
	return nil
}

const clusterHostnameRenewInterval = 12 * time.Hour

func (c *Controller) provisionClusterHostnames(ctx context.Context) {
	c.renewClusterHostnames(ctx)

	ticker := time.NewTicker(clusterHostnameRenewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.renewClusterHostnames(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (c *Controller) renewClusterHostnames(ctx context.Context) {
	for _, hostname := range c.clusterHostnames {
		if ctx.Err() != nil {
			return
		}
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		if err := c.provisionCertificate(ctx, hostname); err != nil {
			c.Log.Warn("failed to provision cluster hostname certificate (will retry next cycle)", "hostname", hostname, "error", err)
		} else {
			c.Log.Info("cluster hostname certificate provisioned", "hostname", hostname)
		}
	}
}

// loadExistingCerts scans the certs directory and loads all existing certificates into cache
func (c *Controller) loadExistingCerts(ctx context.Context, certsDir string) error {
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
		if c.shared != nil {
			bundle, marshalErr := json.Marshal(certificateBundle{Certificate: certPEM, PrivateKey: keyPEM})
			if marshalErr != nil {
				return marshalErr
			}
			authoritative, ensureErr := c.shared.Ensure(ctx, "acme/certificates/"+domain, bundle)
			if ensureErr != nil {
				return ensureErr
			}
			var value certificateBundle
			if json.Unmarshal(authoritative, &value) == nil {
				certPEM, keyPEM = value.Certificate, value.PrivateKey
			}
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
	return c.provisionRouteCertificate(ctx, log, domain, c.provisionCertificate)
}

func (c *Controller) provisionRouteCertificate(
	ctx context.Context,
	log *slog.Logger,
	domain string,
	provision func(context.Context, string) error,
) error {
	if c.inFailureCooldown(domain) {
		log.Debug("skipping DNS certificate provisioning during failure cooldown")
		return nil
	}

	log.Info("provisioning certificate via DNS challenge")
	if err := provision(ctx, domain); err != nil {
		var obtainErr *acmeObtainError
		if !errors.As(err, &obtainErr) {
			return err
		}
		c.failures.Store(domain, time.Now())
		// ReconcileController retries ordinary state-machine failures quickly.
		// ACME is an external rate-limited system, so keep its pacing here and
		// let a later route event or the hourly resync make the next attempt.
		log.Warn("failed to provision certificate; suppressing retries during cooldown",
			"cooldown", dnsFailureCooldown, "error", err)
		return nil
	}
	c.failures.Delete(domain)

	log.Info("certificate provisioned successfully")
	return nil
}

func (c *Controller) inFailureCooldown(domain string) bool {
	value, ok := c.failures.Load(domain)
	if !ok {
		return false
	}
	if time.Since(value.(time.Time)) < dnsFailureCooldown {
		return true
	}
	c.failures.Delete(domain)
	return false
}

// Delete handles http_route deletion - we keep the cert in cache/disk for potential reuse
func (c *Controller) Delete(ctx context.Context, id entity.Id, obj *ingress_v1alpha.HttpRoute) error {
	c.Log.Debug("http_route deleted, keeping certificate in cache", "route", id)
	return nil
}

func (c *Controller) provisionCertificate(ctx context.Context, domain string) error {
	var sharedLock *clusterstate.Lock
	if c.shared != nil {
		var err error
		sharedLock, err = c.shared.Lock(ctx, "acme/"+domain)
		if err != nil {
			return err
		}
		defer sharedLock.Unlock()
		if bundle, err := c.shared.Get(ctx, "acme/certificates/"+domain); err == nil {
			var value certificateBundle
			if json.Unmarshal(bundle, &value) == nil {
				if cert, loadErr := c.loadCertIntoCache(domain, value.Certificate, value.PrivateKey); loadErr == nil && time.Until(cert.notAfter) > 30*24*time.Hour {
					return nil
				}
			}
		}
	}
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
		return &acmeObtainError{err: err}
	}
	if c.shared != nil {
		bundle, err := json.Marshal(certificateBundle{Certificate: cert.Certificate, PrivateKey: cert.PrivateKey})
		if err != nil {
			return err
		}
		if err := sharedLock.Put(ctx, "acme/certificates/"+domain, bundle); err != nil {
			return fmt.Errorf("saving shared certificate: %w", err)
		}
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

type certificateBundle struct {
	Certificate []byte `json:"certificate"`
	PrivateKey  []byte `json:"private_key"`
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
	delete(c.certMisses, domain)
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
	c.cacheMutex.RLock()
	lastMiss := c.certMisses[domain]
	c.cacheMutex.RUnlock()
	if cached == nil && c.shared != nil && time.Since(lastMiss) >= time.Minute {
		ctx, cancel := context.WithTimeout(hello.Context(), 2*time.Second)
		defer cancel()
		if bundle, err := c.shared.Get(ctx, "acme/certificates/"+domain); err == nil {
			var value certificateBundle
			if json.Unmarshal(bundle, &value) == nil {
				cached, _ = c.loadCertIntoCache(domain, value.Certificate, value.PrivateKey)
			}
		} else if errors.Is(err, clusterstate.ErrNotFound) {
			c.cacheMutex.Lock()
			if c.certMisses == nil {
				c.certMisses = make(map[string]time.Time)
			}
			// SNI is attacker-controlled: bound both retention and cache size.
			for name, missedAt := range c.certMisses {
				if time.Since(missedAt) >= time.Minute {
					delete(c.certMisses, name)
				}
			}
			if len(c.certMisses) < 256 {
				c.certMisses[domain] = time.Now()
			}
			c.cacheMutex.Unlock()
		}
	}
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

type sharedAccount struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration,omitempty"`
	Key          []byte                 `json:"key"`
}

func encodeAccount(user *legoUser) ([]byte, error) {
	key, err := x509.MarshalECPrivateKey(user.key.(*ecdsa.PrivateKey))
	if err != nil {
		return nil, err
	}
	return json.Marshal(sharedAccount{Email: user.Email, Registration: user.Registration, Key: key})
}

func decodeAccount(data []byte) (*legoUser, error) {
	var value sharedAccount
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	key, err := x509.ParseECPrivateKey(value.Key)
	if err != nil {
		return nil, err
	}
	return &legoUser{Email: value.Email, Registration: value.Registration, key: key}, nil
}

func (c *Controller) ensureSharedAccount(ctx context.Context, certsDir string) (*legoUser, error) {
	data, err := c.shared.Get(ctx, "acme/dns-account")
	if err == nil {
		return decodeAccount(data)
	}
	if !errors.Is(err, clusterstate.ErrNotFound) {
		return nil, err
	}
	local, err := c.loadOrCreateAccount(certsDir)
	if err != nil {
		return nil, err
	}
	bootstrap, err := encodeAccount(local)
	if err != nil {
		return nil, err
	}
	data, err = c.shared.Ensure(ctx, "acme/dns-account", bootstrap)
	if err != nil {
		return nil, err
	}
	return decodeAccount(data)
}
