package coordinate

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"miren.dev/runtime/components/buildkit"
	"miren.dev/runtime/components/netresolve"
	"miren.dev/runtime/metrics"
	"miren.dev/runtime/observability"
	"miren.dev/runtime/pkg/caauth"
	"miren.dev/runtime/pkg/secret"
	"miren.dev/runtime/pkg/workloadidentity"
)

// EtcdTLSConfig holds TLS configuration for connecting to etcd with mTLS.
type EtcdTLSConfig struct {
	CertPEM []byte // Client certificate PEM
	KeyPEM  []byte // Client private key PEM
	CACert  []byte // CA certificate PEM for verifying server
}

type CoordinatorConfig struct {
	Address         string              `json:"address" yaml:"address"`
	EtcdEndpoints   []string            `json:"etcd_endpoints" yaml:"etcd_endpoints"`
	Prefix          string              `json:"prefix" yaml:"prefix"`
	Resolver        netresolve.Resolver `json:"resolver" yaml:"resolver"`
	TempDir         string              `json:"temp_dir" yaml:"temp_dir"`
	DataPath        string              `json:"data_path" yaml:"data_path"`
	AdditionalNames []string            `json:"additional_names" yaml:"additional_names"`
	IPs             *IPSet              `json:"ips" yaml:"ips"`

	// ACME certificate configuration
	AcmeEmail       string `json:"acme_email" yaml:"acme_email"`
	AcmeDNSProvider string `json:"acme_dns_provider" yaml:"acme_dns_provider"`

	// Cloud authentication configuration
	CloudAuth CloudAuthConfig `json:"cloud_auth" yaml:"cloud_auth"`

	// NoAuth disables authentication entirely (for testing only)
	NoAuth bool `json:"no_auth" yaml:"no_auth"`

	// EtcdTLS holds mTLS configuration for etcd connections (optional).
	// When set, the coordinator will use mTLS to connect to etcd.
	EtcdTLS *EtcdTLSConfig `json:"etcd_tls" yaml:"etcd_tls"`

	Mem       *metrics.MemoryUsage
	Cpu       *metrics.CPUUsage
	HTTP      *metrics.HTTPMetrics
	Logs      *observability.LogReader
	LogWriter observability.LogWriter

	// Observability addresses for distributed runners
	VictoriametricsAddress string
	VictorialogsAddress    string

	// BuildKit is the persistent BuildKit component for container image builds
	BuildKit *buildkit.Component

	// AppVersionRetentionCount and AppVersionRetentionPeriod tune the version
	// retention GC. Values <= 0 fall back to the controller defaults.
	AppVersionRetentionCount  int
	AppVersionRetentionPeriod time.Duration

	// SecretKeyRotationPeriod is how old the cluster key may get before it
	// rotates on its own. Zero means rotate only when asked; negative means the
	// operator's value did not parse, so fall back to the default rather than
	// reading a typo as "never rotate".
	SecretKeyRotationPeriod time.Duration

	// SagaRetentionPeriod is how long a finished saga execution is kept.
	// Unlike the app-version knobs above, zero is meaningful here: it keeps
	// executions indefinitely, which is the escape hatch for an operator who
	// wants saga history frozen during an investigation. A negative value
	// falls back to the controller default.
	SagaRetentionPeriod time.Duration

	// WorkloadIssuer signs workload identity tokens for sandbox containers
	WorkloadIssuer *workloadidentity.Issuer

	// Secrets holds the registered secret backends. The caller builds it so the
	// runner sharing this process materializes through the same registry the
	// coordinator pins and serves with. Nil leaves the cluster without a secret
	// store, in which case a config referencing one fails rather than deploying
	// without it.
	Secrets *secret.Registry
}

// CloudAuthConfig contains cloud authentication settings
type CloudAuthConfig struct {
	Enabled     bool              `json:"enabled" yaml:"enabled"`
	CloudURL    string            `json:"cloud_url" yaml:"cloud_url"`       // URL of miren.cloud (default: https://api.miren.cloud)
	PrivateKey  string            `json:"private_key" yaml:"private_key"`   // Required: Path to service account private key when enabled
	Tags        map[string]string `json:"tags" yaml:"tags"`                 // Tags from registration for RBAC evaluation
	ClusterID   string            `json:"cluster_id" yaml:"cluster_id"`     // Cluster ID for status reporting
	DNSHostname string            `json:"dns_hostname" yaml:"dns_hostname"` // Cloud-provisioned DNS hostname for the cluster

	// IdentityIssuerURL is the workload identity anchor cloud assigned this
	// cluster, when it has one. Empty means the cluster anchors identity at its
	// own hostname and serves its own discovery.
	IdentityIssuerURL string `json:"identity_issuer_url" yaml:"identity_issuer_url"`
}

const (
	DefaultProjectOwner = "miren.system@miren.dev"
	DefaultCloudURL     = "https://api.miren.cloud"
)

// EtcdTLSSetupResult contains the results of setting up etcd TLS.
type EtcdTLSSetupResult struct {
	// CertsDir is the directory containing etcd server certs (ca.crt, server.crt, server.key)
	CertsDir string
	// ClientTLS is the TLS config for clients connecting to etcd
	ClientTLS *EtcdTLSConfig
	// ClientCertFile is the path to the client certificate on disk
	ClientCertFile string
	// ClientKeyFile is the path to the client private key on disk
	ClientKeyFile string
	// CAFile is the path to the CA certificate on disk
	CAFile string
}

// SetupEtcdTLS loads the existing CA and ensures valid etcd mTLS certificates.
// Existing certificates are reused if their SANs match and they aren't near
// expiry; otherwise they are regenerated.
// The dataPath should be the same path used for CoordinatorConfig.DataPath.
// The CA must already exist (created by the coordinator's LoadCA).
// Additional DNS names and IPs are included in the server certificate SANs
// so that distributed runners can connect to etcd over the network.
func SetupEtcdTLS(log *slog.Logger, dataPath string, extraDNSNames []string, extraIPs []net.IP) (*EtcdTLSSetupResult, error) {
	certPath := filepath.Join(dataPath, "server", "ca.crt")
	keyPath := filepath.Join(dataPath, "server", "ca.key")

	data, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("CA certificate not found at %s: %w (CA must be created before setting up etcd TLS)", certPath, err)
	}

	log.Info("loading existing CA for etcd TLS", "path", certPath)

	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("missing key for CA: %w", err)
	}

	ca, err := caauth.LoadFromPEM(data, key)
	if err != nil {
		return nil, fmt.Errorf("failed to load CA: %w", err)
	}

	// Create etcd certs directory
	certsDir := filepath.Join(dataPath, "etcd-certs")
	if err := os.MkdirAll(certsDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create etcd certs directory: %w", err)
	}

	// Build expected server cert SANs
	dnsNames := []string{"localhost"}
	dnsNames = append(dnsNames, extraDNSNames...)

	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	ips = append(ips, extraIPs...)

	serverCertFile := filepath.Join(certsDir, "server.crt")
	serverKeyFile := filepath.Join(certsDir, "server.key")
	clientCertFile := filepath.Join(certsDir, "client.crt")
	clientKeyFile := filepath.Join(certsDir, "client.key")
	caFile := filepath.Join(certsDir, "ca.crt")

	// Check if existing certs are still valid
	if existing, err := loadX509Cert(serverCertFile); err == nil {
		if err := validateAPICertificate(existing, dnsNames, ips); err == nil {
			// Also check client cert exists and isn't expired
			if clientExisting, err := loadX509Cert(clientCertFile); err == nil {
				horizon := time.Now().Add(48 * time.Hour)
				if clientExisting.NotAfter.After(horizon) {
					log.Info("etcd TLS certificates valid, reusing", "certs_dir", certsDir,
						"server_expires", existing.NotAfter.Format(time.RFC3339),
						"sans_ips", existing.IPAddresses)

					clientPEM, err := os.ReadFile(clientCertFile)
					if err != nil {
						log.Info("etcd client cert unreadable, regenerating", "error", err)
						goto regenerate
					}
					clientKey, err := os.ReadFile(clientKeyFile)
					if err != nil {
						log.Info("etcd client key unreadable, regenerating", "error", err)
						goto regenerate
					}
					caCert := ca.GetCACertificate()

					return &EtcdTLSSetupResult{
						CertsDir: certsDir,
						ClientTLS: &EtcdTLSConfig{
							CertPEM: clientPEM,
							KeyPEM:  clientKey,
							CACert:  caCert,
						},
						ClientCertFile: clientCertFile,
						ClientKeyFile:  clientKeyFile,
						CAFile:         caFile,
					}, nil
				}
			}
		} else {
			log.Info("etcd server cert needs regeneration", "reason", err)
		}
	}

regenerate:
	log.Info("generating etcd TLS certificates", "dns_names", dnsNames, "ips", ips)

	// Issue etcd server certificate
	serverCert, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "etcd-server",
		Organization: "miren",
		ValidFor:     1 * year,
		DNSNames:     dnsNames,
		IPs:          ips,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to issue etcd server certificate: %w", err)
	}

	// Write etcd server certs
	if err := os.WriteFile(caFile, ca.GetCACertificate(), 0644); err != nil {
		return nil, fmt.Errorf("failed to write etcd CA cert: %w", err)
	}
	if err := os.WriteFile(serverCertFile, serverCert.CertPEM, 0644); err != nil {
		return nil, fmt.Errorf("failed to write etcd server cert: %w", err)
	}
	if err := os.WriteFile(serverKeyFile, serverCert.KeyPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to write etcd server key: %w", err)
	}

	// Issue coordinator client certificate
	clientCert, err := ca.IssueCertificate(caauth.Options{
		CommonName:   "etcd-client-coordinator",
		Organization: "miren",
		ValidFor:     1 * year,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to issue etcd client certificate: %w", err)
	}

	if err := os.WriteFile(clientCertFile, clientCert.CertPEM, 0644); err != nil {
		return nil, fmt.Errorf("failed to write etcd client cert: %w", err)
	}
	if err := os.WriteFile(clientKeyFile, clientCert.KeyPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to write etcd client key: %w", err)
	}

	log.Info("etcd TLS certificates generated", "certs_dir", certsDir)

	return &EtcdTLSSetupResult{
		CertsDir: certsDir,
		ClientTLS: &EtcdTLSConfig{
			CertPEM: clientCert.CertPEM,
			KeyPEM:  clientCert.KeyPEM,
			CACert:  ca.GetCACertificate(),
		},
		ClientCertFile: clientCertFile,
		ClientKeyFile:  clientKeyFile,
		CAFile:         caFile,
	}, nil
}

func loadX509Cert(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in %s", path)
	}
	return x509.ParseCertificate(block.Bytes)
}
